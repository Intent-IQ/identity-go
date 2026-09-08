package enrichment

import "time"

type NotEnrichedReason string
type CacheLookupResult string

const (
	ReasonNoIDs       NotEnrichedReason = "no_ids"
	ReasonNoIDsCached NotEnrichedReason = "no_ids_cached"
	ReasonInProgress  NotEnrichedReason = "in_progress"
	ReasonNoEndpoint  NotEnrichedReason = "no_endpoint"
)

const (
	CacheLookupHit  CacheLookupResult = "hit"
	CacheLookupMiss CacheLookupResult = "miss"
)

type Metrics interface {
	Request(partnerID string)
	Enriched(partnerID string)
	NotEnriched(partnerID string, reason NotEnrichedReason)
	APIRequestDuration(partnerID string, duration time.Duration)
	APISuccess(partnerID string)
	APIError(partnerID string, kind string, statusCode int)
	CacheLookup(partnerID string, result CacheLookupResult, layer CacheLayer)
}

type NoopMetrics struct{}

func (NoopMetrics) Request(string)                                    {}
func (NoopMetrics) Enriched(string)                                   {}
func (NoopMetrics) NotEnriched(string, NotEnrichedReason)             {}
func (NoopMetrics) APIRequestDuration(string, time.Duration)          {}
func (NoopMetrics) APISuccess(string)                                 {}
func (NoopMetrics) APIError(string, string, int)                      {}
func (NoopMetrics) CacheLookup(string, CacheLookupResult, CacheLayer) {}

var _ Metrics = NoopMetrics{}
