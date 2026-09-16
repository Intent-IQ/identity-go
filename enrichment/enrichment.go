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
	PartnerID    string
	Endpoint     string
	Auction      *openrtb2.BidRequest
	Timeout      time.Duration
	CacheEnabled bool
}

type Outcome string

const (
	OutcomeEnriched    Outcome = "enriched"
	OutcomeNoIDs       Outcome = "no_ids"
	OutcomeCachedNoIDs Outcome = "no_ids_cached"
	OutcomeInProgress  Outcome = "in_progress"
	OutcomeNoEndpoint  Outcome = "no_endpoint"
)

type Result struct {
	EIDs             []openrtb2.EID
	CacheTTL         time.Duration
	ABTestUUID       string
	TerminationCause *int64
	Outcome          Outcome
	// CacheLayer reports which layer served the result
	CacheLayer CacheLayer
}

// FromCache reports whether a cache layer served the result rather than the resolution API.
func (result Result) FromCache() bool {
	return result.CacheLayer != CacheLayerNone
}
