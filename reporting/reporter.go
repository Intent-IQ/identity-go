package reporting

import (
	"context"
	"errors"
	"fmt"

	iiqreporting "github.com/Intent-IQ/identity-go/iiqapi/reporting"
	"github.com/Intent-IQ/identity-go/logging"
)

var errAPIRequired = errors.New("reporting API is required")

type reporter struct {
	api     iiqreporting.API
	metrics Metrics
	logger  logging.Logger
}

func New(dependencies Dependencies) (Reporter, error) {
	if dependencies.API == nil {
		return nil, errAPIRequired
	}
	if dependencies.Metrics == nil {
		dependencies.Metrics = NoopMetrics{}
	}
	if dependencies.Logger == nil {
		dependencies.Logger = logging.NoopLogger{}
	}
	return &reporter{
		api:     dependencies.API,
		metrics: dependencies.Metrics,
		logger:  dependencies.Logger,
	}, nil
}

func (reporter *reporter) Report(ctx context.Context, request Request) error {
	if request.Endpoint == "" {
		return nil
	}

	requestURL := buildReportURL(request)
	reportContext, cancel := context.WithTimeout(ctx, request.Timeout)
	defer cancel()

	if err := reporter.api.ReportImpression(reportContext, requestURL); err != nil {
		reporter.metrics.ImpressionError(request.PartnerID)
		reporter.logger.Warn(fmt.Sprintf("intentiq-identity: impression report failed (dpi=%s): %v", request.PartnerID, err))
		return err
	}
	reporter.metrics.ImpressionReported(request.PartnerID)
	return nil
}

var _ Reporter = (*reporter)(nil)
