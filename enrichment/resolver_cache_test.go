package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Intent-IQ/identity-go/iiqapi/s2s"
	"github.com/prebid/openrtb/v20/openrtb2"
)

type recordingCache struct {
	result CacheResult
	getErr error
	putErr error

	gets       [][]CacheKey
	resolved   []cacheResolvedCall
	negative   []cacheNegativeCall
	inProgress [][]CacheKey
}

type cacheResolvedCall struct {
	keys   []CacheKey
	result Result
}

type cacheNegativeCall struct {
	keys     []CacheKey
	metadata ResultMetadata
}

func (cache *recordingCache) Get(_ context.Context, keys []CacheKey) (CacheResult, error) {
	cache.gets = append(cache.gets, cloneCacheKeys(keys))
	return cache.result, cache.getErr
}

func (cache *recordingCache) PutResolved(_ context.Context, keys []CacheKey, result Result) error {
	cache.resolved = append(cache.resolved, cacheResolvedCall{keys: cloneCacheKeys(keys), result: result})
	return cache.putErr
}

func (cache *recordingCache) PutNegative(_ context.Context, keys []CacheKey, metadata ResultMetadata) error {
	cache.negative = append(cache.negative, cacheNegativeCall{keys: cloneCacheKeys(keys), metadata: metadata})
	return cache.putErr
}

func (cache *recordingCache) PutInProgress(_ context.Context, keys []CacheKey) error {
	cache.inProgress = append(cache.inProgress, cloneCacheKeys(keys))
	return cache.putErr
}

func cloneCacheKeys(keys []CacheKey) []CacheKey {
	return append([]CacheKey(nil), keys...)
}

func newCachedTestEnricher(
	t *testing.T,
	api *recordingS2S,
	cache Cache,
	metrics *recordingEnrichmentMetrics,
	logger *recordingEnrichmentLogger,
	maxKeys int,
) Enricher {
	t.Helper()
	created, err := New(Dependencies{S2S: api, Cache: cache, Metrics: metrics, Logger: logger}, maxKeys)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return created
}

func cacheableRequest() Request {
	return Request{
		PartnerID: "partner",
		Endpoint:  "https://example.test/resolve",
		Timeout:   time.Second,
		Auction: &openrtb2.BidRequest{
			User: &openrtb2.User{EIDs: []openrtb2.EID{
				{Source: "pubcid.org", UIDs: []openrtb2.UID{{ID: "shared"}}},
			}},
			Device: &openrtb2.Device{IP: "192.0.2.1", UA: "test-agent"},
		},
		CacheEnabled: true,
	}
}

func TestEnrichBypassesCacheWhenDisabledOrWithoutKeys(t *testing.T) {
	tests := []struct {
		name    string
		request Request
	}{
		{name: "disabled", request: func() Request { request := cacheableRequest(); request.CacheEnabled = false; return request }()},
		{name: "no keys", request: Request{PartnerID: "partner", Endpoint: "https://example.test", Timeout: time.Second, Auction: &openrtb2.BidRequest{}, CacheEnabled: true}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api := &recordingS2S{response: s2s.Response{Data: json.RawMessage(`{"eids":[]}`)}}
			cache := &recordingCache{}
			metrics := &recordingEnrichmentMetrics{}
			result, err := newCachedTestEnricher(t, api, cache, metrics, &recordingEnrichmentLogger{}, 10).Enrich(t.Context(), test.request)
			if err != nil || result.Outcome != OutcomeNoIDs || len(api.calls) != 1 || len(cache.gets) != 0 {
				t.Fatalf("Enrich() = (%#v, %v), API calls=%d cache gets=%d", result, err, len(api.calls), len(cache.gets))
			}
			assertEnrichmentMetricNames(t, metrics.events, "request", "api_duration", "api_success", "not_enriched")
		})
	}
}

func TestEnrichTreatsNilCacheAsDisabled(t *testing.T) {
	api := &recordingS2S{response: s2s.Response{Data: json.RawMessage(`{"eids":[]}`)}}
	metrics := &recordingEnrichmentMetrics{}
	result, err := newCachedTestEnricher(t, api, nil, metrics, &recordingEnrichmentLogger{}, 10).Enrich(t.Context(), cacheableRequest())
	if err != nil || result.Outcome != OutcomeNoIDs || len(api.calls) != 1 {
		t.Fatalf("Enrich() = (%#v, %v), API calls=%d", result, err, len(api.calls))
	}
	assertEnrichmentMetricNames(t, metrics.events, "request", "api_duration", "api_success", "not_enriched")
}

func TestEnrichHandlesEveryCacheState(t *testing.T) {
	terminationCause := int64(120088)
	tests := []struct {
		name          string
		cached        CacheResult
		wantOutcome   Outcome
		wantEIDs      int
		wantReason    NotEnrichedReason
		wantLookup    CacheLookupResult
		wantMetricEnd string
	}{
		{
			name: "positive hit",
			cached: CacheResult{State: CacheHit, Layer: CacheLayerL2, KeyType: CacheKeyFirstParty, Result: Result{
				EIDs:       []openrtb2.EID{{Source: "intentiq.com", UIDs: []openrtb2.UID{{ID: "cached"}}}},
				ABTestUUID: "ab-positive", TerminationCause: &terminationCause,
			}},
			wantOutcome: OutcomeEnriched, wantEIDs: 1, wantLookup: CacheLookupHit, wantMetricEnd: "enriched",
		},
		{
			name: "negative hit",
			cached: CacheResult{State: CacheNegative, Layer: CacheLayerL1, KeyType: CacheKeyDevice, Result: Result{
				ABTestUUID: "ab-negative", TerminationCause: &terminationCause,
			}},
			wantOutcome: OutcomeCachedNoIDs, wantReason: ReasonNoIDsCached, wantLookup: CacheLookupMiss, wantMetricEnd: "not_enriched",
		},
		{
			name:        "in progress",
			cached:      CacheResult{State: CacheInProgress, Layer: CacheLayerL2, KeyType: CacheKeyThirdParty},
			wantOutcome: OutcomeInProgress, wantReason: ReasonInProgress, wantLookup: CacheLookupMiss, wantMetricEnd: "not_enriched",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api := &recordingS2S{}
			cache := &recordingCache{result: test.cached}
			metrics := &recordingEnrichmentMetrics{}
			result, err := newCachedTestEnricher(t, api, cache, metrics, &recordingEnrichmentLogger{}, 10).Enrich(t.Context(), cacheableRequest())
			if err != nil || result.Outcome != test.wantOutcome || len(result.EIDs) != test.wantEIDs || len(api.calls) != 0 {
				t.Fatalf("Enrich() = (%#v, %v), API calls=%d", result, err, len(api.calls))
			}
			if test.cached.State != CacheInProgress && (result.ABTestUUID != test.cached.Result.ABTestUUID || result.TerminationCause != test.cached.Result.TerminationCause) {
				t.Fatalf("cached metadata not preserved: %#v", result)
			}
			assertEnrichmentMetricNames(t, metrics.events, "request", "cache_lookup", test.wantMetricEnd)
			if metrics.events[1].lookup != test.wantLookup || metrics.events[1].layer != test.cached.Layer {
				t.Fatalf("cache lookup metric = %#v", metrics.events[1])
			}
			if test.wantReason != "" && metrics.events[2].reason != string(test.wantReason) {
				t.Fatalf("not-enriched reason = %q", metrics.events[2].reason)
			}
		})
	}
}

func TestEnrichCacheMissStoresPositiveResult(t *testing.T) {
	cacheTTL := int64(900)
	terminationCause := int64(5)
	api := &recordingS2S{response: s2s.Response{
		Data:     json.RawMessage(`{"eids":[{"source":"intentiq.com","uids":[{"id":"resolved"}]}]}`),
		CacheTTL: &cacheTTL, ABTestUUID: "ab-positive", TC: &terminationCause,
	}}
	cache := &recordingCache{result: CacheResult{State: CacheMiss, Layer: CacheLayerNone}}
	metrics := &recordingEnrichmentMetrics{}
	result, err := newCachedTestEnricher(t, api, cache, metrics, &recordingEnrichmentLogger{}, 1).Enrich(t.Context(), cacheableRequest())
	if err != nil || result.Outcome != OutcomeEnriched || len(api.calls) != 1 {
		t.Fatalf("Enrich() = (%#v, %v), API calls=%d", result, err, len(api.calls))
	}
	wantKeys := []CacheKey{{Value: "pubcid:shared", Type: CacheKeyFirstParty}}
	if !reflect.DeepEqual(cache.gets, [][]CacheKey{wantKeys}) || !reflect.DeepEqual(cache.inProgress, [][]CacheKey{wantKeys}) {
		t.Fatalf("cache lookup/in-progress keys = %#v / %#v", cache.gets, cache.inProgress)
	}
	if len(cache.resolved) != 1 || !reflect.DeepEqual(cache.resolved[0].keys, wantKeys) || !reflect.DeepEqual(cache.resolved[0].result, result) || len(cache.negative) != 0 {
		t.Fatalf("cache writes = resolved %#v negative %#v", cache.resolved, cache.negative)
	}
	assertEnrichmentMetricNames(t, metrics.events, "request", "cache_lookup", "api_duration", "api_success", "enriched")
}

func TestEnrichCacheMissStoresNegativeMetadata(t *testing.T) {
	cacheTTL := int64(300)
	terminationCause := int64(120088)
	api := &recordingS2S{response: s2s.Response{
		Data: json.RawMessage(`{"eids":[]}`), CacheTTL: &cacheTTL, ABTestUUID: "ab-negative", TC: &terminationCause,
	}}
	cache := &recordingCache{result: CacheResult{State: CacheMiss, Layer: CacheLayerNone}}
	metrics := &recordingEnrichmentMetrics{}
	result, err := newCachedTestEnricher(t, api, cache, metrics, &recordingEnrichmentLogger{}, 10).Enrich(t.Context(), cacheableRequest())
	if err != nil || result.Outcome != OutcomeNoIDs || len(cache.negative) != 1 || len(cache.resolved) != 0 {
		t.Fatalf("Enrich() = (%#v, %v), cache writes=%#v/%#v", result, err, cache.resolved, cache.negative)
	}
	want := ResultMetadata{CacheTTL: 5 * time.Minute, ABTestUUID: "ab-negative", TerminationCause: &terminationCause}
	if !reflect.DeepEqual(cache.negative[0].metadata, want) {
		t.Fatalf("negative metadata = %#v, want %#v", cache.negative[0].metadata, want)
	}
	assertEnrichmentMetricNames(t, metrics.events, "request", "cache_lookup", "api_duration", "api_success", "not_enriched")
	if metrics.events[4].reason != string(ReasonNoIDs) {
		t.Fatalf("not-enriched reason = %q", metrics.events[4].reason)
	}
}

func TestEnrichCacheFailuresRemainFailOpen(t *testing.T) {
	t.Run("read error falls through to S2S", func(t *testing.T) {
		cacheError := errors.New("cache unavailable")
		cache := &recordingCache{getErr: cacheError}
		api := &recordingS2S{response: s2s.Response{Data: json.RawMessage(`{"eids":[]}`)}}
		metrics := &recordingEnrichmentMetrics{}
		logger := &recordingEnrichmentLogger{}
		result, err := newCachedTestEnricher(t, api, cache, metrics, logger, 10).Enrich(t.Context(), cacheableRequest())
		if err != nil || result.Outcome != OutcomeNoIDs || len(api.calls) != 1 || len(cache.inProgress) != 1 || len(cache.negative) != 1 {
			t.Fatalf("Enrich() = (%#v, %v), calls=%d in-progress=%d negative=%d", result, err, len(api.calls), len(cache.inProgress), len(cache.negative))
		}
		if len(logger.warnings) != 1 || !strings.Contains(logger.warnings[0], cacheError.Error()) {
			t.Fatalf("warnings = %q", logger.warnings)
		}
		if metrics.events[1].lookup != CacheLookupMiss || metrics.events[1].layer != CacheLayerNone {
			t.Fatalf("cache metric = %#v", metrics.events[1])
		}
	})

	t.Run("write errors preserve successful result", func(t *testing.T) {
		cacheError := errors.New("cache write failed")
		cache := &recordingCache{putErr: cacheError}
		api := &recordingS2S{response: s2s.Response{Data: json.RawMessage(`{"eids":[{"source":"intentiq.com"}]}`)}}
		logger := &recordingEnrichmentLogger{}
		result, err := newCachedTestEnricher(t, api, cache, &recordingEnrichmentMetrics{}, logger, 10).Enrich(t.Context(), cacheableRequest())
		if err != nil || result.Outcome != OutcomeEnriched || len(cache.inProgress) != 1 || len(cache.resolved) != 1 {
			t.Fatalf("Enrich() = (%#v, %v), writes=%d/%d", result, err, len(cache.inProgress), len(cache.resolved))
		}
		if len(logger.warnings) != 2 {
			t.Fatalf("warnings = %q", logger.warnings)
		}
	})
}

func TestEnrichS2SErrorAfterCacheMissLeavesInProgressMarker(t *testing.T) {
	s2sError := errors.New("upstream failed")
	cache := &recordingCache{}
	api := &recordingS2S{err: s2sError}
	result, err := newCachedTestEnricher(t, api, cache, &recordingEnrichmentMetrics{}, &recordingEnrichmentLogger{}, 10).Enrich(t.Context(), cacheableRequest())
	if !errors.Is(err, s2sError) || !reflect.DeepEqual(result, Result{}) || len(cache.inProgress) != 1 || len(cache.resolved) != 0 || len(cache.negative) != 0 {
		t.Fatalf("Enrich() = (%#v, %v), cache=%#v", result, err, cache)
	}
}
