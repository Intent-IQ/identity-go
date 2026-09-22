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
	Shutdown(context.Context) error
}

type Request struct {
	PartnerID string
	Endpoint  string
	Auction   *openrtb2.BidRequest
	Timeout   time.Duration
	// WaitTimeout bounds how long the caller waits for enrichment. A nil value
	// preserves synchronous behavior; a non-nil zero value means do not wait.
	WaitTimeout  *time.Duration
	CacheEnabled bool
}

type Outcome string

const (
	// OutcomeEnriched means one or more resolved identity IDs are available for the auction.
	OutcomeEnriched Outcome = "enriched"

	// OutcomeNoIDs means the S2S call completed successfully but returned no identity IDs.
	OutcomeNoIDs Outcome = "no_ids"

	// OutcomeCachedNoIDs means a cached negative result was found, so no S2S call was started.
	OutcomeCachedNoIDs Outcome = "no_ids_cached"

	// OutcomeUnresolved represents a successful S2S response with an empty body.
	// It remains distinct from OutcomeNoIDs because it must not be negatively cached.
	OutcomeUnresolved Outcome = "unresolved"

	// OutcomeInProgress means another request is already resolving this identity,
	// so this request did not start another S2S call.
	OutcomeInProgress Outcome = "in_progress"

	// OutcomeNoEndpoint means enrichment was skipped because no S2S endpoint was configured.
	OutcomeNoEndpoint Outcome = "no_endpoint"

	// OutcomeWaitExpired means the auction stopped waiting, while the admitted S2S
	// call may continue in the background and warm the cache.
	OutcomeWaitExpired Outcome = "wait_expired"

	// OutcomeBackgroundLimit means all background S2S slots were occupied, so the
	// request failed open without starting or queueing another S2S call.
	OutcomeBackgroundLimit Outcome = "background_limit"
)

type WaitMode string

const (
	WaitModeSync   WaitMode = "sync"
	WaitModeAsync  WaitMode = "async"
	WaitModeHybrid WaitMode = "hybrid"
)

func normalizeWaitTimeout(timeout time.Duration, wait *time.Duration) (time.Duration, WaitMode) {
	if wait == nil || timeout <= 0 || *wait >= timeout {
		return timeout, WaitModeSync
	}
	if *wait <= 0 {
		return 0, WaitModeAsync
	}
	return *wait, WaitModeHybrid
}

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
