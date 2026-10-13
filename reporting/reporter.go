package reporting

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	iiqreporting "github.com/Intent-IQ/identity-go/iiqapi/reporting"
	"github.com/Intent-IQ/identity-go/logging"
)

var errAPIRequired = errors.New("reporting API is required")

type reportJob struct {
	partnerID  string
	requestURL string
	timeout    time.Duration
}

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

	job := newReportJob(request)
	reporter.metrics.ReportingStarted()
	go reporter.run(job)

	return ReportAccepted
}

func newReportJob(request Request) reportJob {
	requestURL := ""
	if request.Endpoint != "" {
		requestURL = buildReportURL(request)
	}

	return reportJob{
		partnerID:  request.PartnerID,
		requestURL: requestURL,
		timeout:    request.Timeout,
	}
}

func (reporter *reporter) run(job reportJob) {
	defer func() {
		if recovered := recover(); recovered != nil {
			reporter.recordPanic(job.partnerID, recovered)
		}
	}()
	defer reporter.limiter.Release()
	defer reporter.metrics.ReportingFinished()

	_ = reporter.report(reporter.reportContext, job)
}

func (reporter *reporter) recordPanic(partnerID string, recovered any) {
	// Host-provided observability implementations must not be able to re-panic
	// an owned background goroutine after the original panic was contained.
	func() {
		defer func() { _ = recover() }()
		reporter.metrics.ImpressionError(partnerID)
	}()

	defer func() { _ = recover() }()
	reporter.logger.Error(fmt.Sprintf(
		"intentiq-identity: panic in impression report: %v\n%s",
		recovered,
		debug.Stack(),
	))
}

func (reporter *reporter) reject(partnerID string, result ReportResult) {
	reporter.metrics.ImpressionRejected(partnerID, result)
	reporter.logger.Warn(fmt.Sprintf(
		"intentiq-identity: impression report rejected (dpi=%s): %s",
		partnerID, result,
	))
}

func (reporter *reporter) report(ctx context.Context, job reportJob) error {
	if job.requestURL == "" {
		return nil
	}

	reportContext, cancel := context.WithTimeout(ctx, job.timeout)
	defer cancel()

	started := time.Now()
	defer func() {
		reporter.metrics.ImpressionAPIRequestDuration(job.partnerID, time.Since(started))
	}()

	if err := reporter.api.ReportImpression(reportContext, job.requestURL); err != nil {
		reporter.metrics.ImpressionError(job.partnerID)
		reporter.logger.Warn(fmt.Sprintf("intentiq-identity: impression report failed (dpi=%s): %v", job.partnerID, err))
		return err
	}

	reporter.metrics.ImpressionReported(job.partnerID)
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
