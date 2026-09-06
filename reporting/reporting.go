// Package reporting defines the host-facing impression reporting contract.
package reporting

import (
	"context"
	"time"

	"github.com/prebid/openrtb/v20/openrtb2"
)

// Reporter reports one bid impression. The caller controls synchronous or
// asynchronous execution.
type Reporter interface {
	Report(context.Context, Request) error
}

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
