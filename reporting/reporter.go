package reporting

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	iiqreporting "github.com/Intent-IQ/identity-go/iiqapi/reporting"
	"github.com/Intent-IQ/identity-go/logging"
)

var errAPIRequired = errors.New("reporting API is required")

type reporter struct {
	logger logging.Logger

	api     iiqreporting.API
	limiter reportLimiter

	metrics Metrics

	reportContext context.Context
	cancelReports context.CancelFunc

	closing   atomic.Bool
	drainOnce sync.Once
	drained   chan struct{}
}

func New(dependencies Dependencies, config Config) (Reporter, error) {
	if dependencies.API == nil {
		return nil, errAPIRequired
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if dependencies.Metrics == nil {
		dependencies.Metrics = NoopMetrics{}
	}
	if dependencies.Logger == nil {
		dependencies.Logger = logging.NoopLogger{}
	}
	dependencies.Metrics.ReportingCapacity(config.MaxConcurrentCalls)

	reportContext, cancelReports := context.WithCancel(context.Background())
	return &reporter{
		api:           dependencies.API,
		metrics:       dependencies.Metrics,
		logger:        dependencies.Logger,
		limiter:       newReportLimiter(config.MaxConcurrentCalls),
		reportContext: reportContext,
		cancelReports: cancelReports,
		drained:       make(chan struct{}),
	}, nil
}

func (reporter *reporter) TryReport(request Request) ReportResult {
	if reporter.closing.Load() {
		reporter.reject(request.PartnerID, ReportShuttingDown)
		return ReportShuttingDown
	}
	if !reporter.limiter.TryAcquire() {
		if reporter.closing.Load() {
			reporter.reject(request.PartnerID, ReportShuttingDown)
			return ReportShuttingDown
		}
		reporter.reject(request.PartnerID, ReportAtCapacity)
		return ReportAtCapacity
	}
	if reporter.closing.Load() {
		reporter.limiter.Release()
		reporter.reject(request.PartnerID, ReportShuttingDown)
		return ReportShuttingDown
	}

	reporter.metrics.ReportingStarted()
	go func() {
		defer reporter.limiter.Release()
		defer reporter.metrics.ReportingFinished()
		_ = reporter.report(reporter.reportContext, request)
	}()
	return ReportAccepted
}

func (reporter *reporter) reject(partnerID string, result ReportResult) {
	reporter.metrics.ImpressionRejected(partnerID, result)
	reporter.logger.Warn(fmt.Sprintf(
		"intentiq-identity: impression report rejected (dpi=%s): %s",
		partnerID, result,
	))
}

func (reporter *reporter) report(ctx context.Context, request Request) error {
	if request.Endpoint == "" {
		return nil
	}

	requestURL := buildReportURL(request)
	reportContext, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()

	started := time.Now()
	defer func() {
		reporter.metrics.ImpressionAPIRequestDuration(request.PartnerID, time.Since(started))
	}()
	if err := reporter.api.ReportImpression(reportContext, requestURL); err != nil {
		reporter.metrics.ImpressionError(request.PartnerID)
		reporter.logger.Warn(fmt.Sprintf("intentiq-identity: impression report failed (dpi=%s): %v", request.PartnerID, err))
		return err
	}
	reporter.metrics.ImpressionReported(request.PartnerID)
	return nil
}

func (reporter *reporter) Shutdown(ctx context.Context) error {
	reporter.closing.Store(true)
	reporter.drainOnce.Do(func() {
		go func() {
			reporter.limiter.Drain()
			close(reporter.drained)
		}()
	})

	select {
	case <-reporter.drained:
		reporter.cancelReports()
		return nil
	case <-ctx.Done():
		reporter.cancelReports()
		return ctx.Err()
	}
}

var _ Reporter = (*reporter)(nil)
