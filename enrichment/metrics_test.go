package enrichment

import (
	"testing"
	"time"
)

func TestNoopMetrics(t *testing.T) {
	metrics := NoopMetrics{}
	metrics.Request("partner")
	metrics.Enriched("partner")
	metrics.NotEnriched("partner", ReasonNoIDs)
	metrics.APIRequestDuration("partner", time.Millisecond)
	metrics.APISuccess("partner")
	metrics.APIError("partner", "timeout", 0)
	metrics.CacheLookup("partner", CacheLookupMiss, CacheLayerNone)
}
