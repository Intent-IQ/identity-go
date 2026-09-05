// Package enrichment defines the host-facing identity enrichment contract.
package enrichment

import (
	"context"
	"time"

	"github.com/prebid/openrtb/v20/openrtb2"
)

// Enricher enriches an auction with resolved user identifiers.
type Enricher interface {
	Enrich(context.Context, Request) (Result, error)
}

type Request struct {
	PartnerID string
	Endpoint  string
	Auction   *openrtb2.BidRequest
	Consent   Consent
}

type Consent struct {
	GDPR       *int8
	TCF        string
	USPrivacy  string
	GPP        string
	GPPSection []int8
}

type Result struct {
	EIDs             []openrtb2.EID
	CacheTTL         time.Duration
	ABTestUUID       string
	TerminationCause *int64
}
