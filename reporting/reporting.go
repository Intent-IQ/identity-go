// Package reporting defines the host-facing impression reporting contract.
package reporting

import (
	"context"
	"time"

	"github.com/prebid/openrtb/v20/openrtb2"
)

// Reporter asynchronously reports impressions with bounded concurrency.
type Reporter interface {
	// TryReport attempts to start a report without blocking and returns its
	// admission outcome. ReportAccepted does not indicate successful delivery.
	TryReport(Request) ReportResult
	// Shutdown stops accepting reports and drains accepted work until ctx
	// expires. When ctx expires, in-flight report contexts are cancelled.
	Shutdown(context.Context) error
}

// ReportResult describes whether an asynchronous report was accepted.
// ReportAccepted does not indicate successful delivery.
type ReportResult string

const (
	ReportAccepted     ReportResult = "accepted"
	ReportAtCapacity   ReportResult = "at_capacity"
	ReportShuttingDown ReportResult = "shutting_down"
)

type Request struct {
	PartnerID        string
	Endpoint         string
	Timeout          time.Duration
	Bid              openrtb2.Bid
	BidderCode       string
	Currency         string
	AuctionID        string
	Reference        string
	IP               string
	UserAgent        string
	ABTestUUID       string
	TerminationCause *int64
}
