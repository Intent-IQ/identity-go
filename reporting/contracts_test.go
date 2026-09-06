package reporting

import (
	"context"
	"testing"
	"time"

	iiqreporting "github.com/Intent-IQ/identity-go/iiqapi/reporting"
	"github.com/Intent-IQ/identity-go/logging"
	"github.com/prebid/openrtb/v20/openrtb2"
)

type recordingReportingAPI struct{}

func (*recordingReportingAPI) ReportImpression(context.Context, string) error { return nil }

func TestReportingContracts(t *testing.T) {
	terminationCause := int64(0)
	request := Request{
		PartnerID:        "partner",
		Endpoint:         "https://example.test/report",
		Timeout:          time.Second,
		Bid:              openrtb2.Bid{ImpID: "imp", Price: 1.5},
		BidderCode:       "bidder",
		Currency:         "USD",
		AuctionID:        "auction",
		Reference:        "example.test",
		IP:               "192.0.2.1",
		UserAgent:        "agent",
		ABTestUUID:       "ab",
		TerminationCause: &terminationCause,
	}
	if request.Timeout != time.Second || request.Bid.ImpID != "imp" || request.TerminationCause == nil {
		t.Fatalf("Request fields not retained: %#v", request)
	}

	dependencies := Dependencies{
		API:     &recordingReportingAPI{},
		Metrics: NoopMetrics{},
		Logger:  logging.NoopLogger{},
	}
	if dependencies.API == nil || dependencies.Metrics == nil || dependencies.Logger == nil {
		t.Fatalf("Dependencies fields not retained: %#v", dependencies)
	}
}

func TestNoopReportingMetrics(t *testing.T) {
	metrics := NoopMetrics{}
	metrics.ImpressionReported("partner")
	metrics.ImpressionError("partner")
}

var _ iiqreporting.API = (*recordingReportingAPI)(nil)
