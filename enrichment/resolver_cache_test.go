package enrichment

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Intent-IQ/identity-go/iiqapi"
	"github.com/Intent-IQ/identity-go/iiqapi/s2s"
	"github.com/prebid/openrtb/v20/openrtb2"
)

type recordingCache struct {
	result CacheResult
	getErr error
	putErr error

	gets           [][]CacheKey
	resolved       []cacheResolvedCall
	negative       []cacheNegativeCall
	inProgress     [][]CacheKey
	inProgressTTLs []time.Duration
	resolvedCtxErr error
	negativeCtxErr error
}

type cacheResolvedCall struct {
	keys   []CacheKey
	result Result
}

type cacheNegativeCall struct {
	keys     []CacheKey
	metadata ResultMetadata
}

type expiringMarkerCache struct {
	recordingCache
	now       time.Time
	expiresAt time.Time
}

type lifecycleCache struct {
	mu       sync.Mutex
	state    CacheState
	result   Result
	resolved chan struct{}
	negative chan struct{}
}

type signalingMetrics struct {
	NoopMetrics
	apiError chan struct{}
}

func (metrics *signalingMetrics) APIError(string, string, int) {
	close(metrics.apiError)
}

func (cache *lifecycleCache) Get(context.Context, []CacheKey) (CacheResult, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return CacheResult{State: cache.state, Layer: CacheLayerL1, Result: cache.result}, nil
}

func (cache *lifecycleCache) PutResolved(_ context.Context, _ []CacheKey, result Result) error {
	cache.mu.Lock()
	cache.state = CacheHit
	cache.result = result
	cache.mu.Unlock()
	close(cache.resolved)
	return nil
}

func (cache *lifecycleCache) PutNegative(context.Context, []CacheKey, ResultMetadata) error {
	cache.mu.Lock()
	cache.state = CacheNegative
	cache.mu.Unlock()
	if cache.negative != nil {
		close(cache.negative)
	}
	return nil
}

func (cache *lifecycleCache) PutInProgress(context.Context, []CacheKey, time.Duration) error {
	cache.mu.Lock()
	cache.state = CacheInProgress
	cache.mu.Unlock()
	return nil
}

func (cache *expiringMarkerCache) Get(_ context.Context, keys []CacheKey) (CacheResult, error) {
	cache.gets = append(cache.gets, cloneCacheKeys(keys))
	if cache.now.Before(cache.expiresAt) {
		return CacheResult{State: CacheInProgress, Layer: CacheLayerL2}, nil
	}
	return CacheResult{State: CacheMiss, Layer: CacheLayerNone}, nil
}

func (cache *expiringMarkerCache) PutInProgress(ctx context.Context, keys []CacheKey, ttl time.Duration) error {
	cache.expiresAt = cache.now.Add(ttl)
	return cache.recordingCache.PutInProgress(ctx, keys, ttl)
}

func (cache *recordingCache) Get(_ context.Context, keys []CacheKey) (CacheResult, error) {
	cache.gets = append(cache.gets, cloneCacheKeys(keys))
	return cache.result, cache.getErr
}

func (cache *recordingCache) PutResolved(ctx context.Context, keys []CacheKey, result Result) error {
	cache.resolvedCtxErr = ctx.Err()
	cache.resolved = append(cache.resolved, cacheResolvedCall{keys: cloneCacheKeys(keys), result: result})
	return cache.putErr
}

func (cache *recordingCache) PutNegative(ctx context.Context, keys []CacheKey, metadata ResultMetadata) error {
	cache.negativeCtxErr = ctx.Err()
	cache.negative = append(cache.negative, cacheNegativeCall{keys: cloneCacheKeys(keys), metadata: metadata})
	return cache.putErr
}

func (cache *recordingCache) PutInProgress(_ context.Context, keys []CacheKey, ttl time.Duration) error {
	cache.inProgress = append(cache.inProgress, cloneCacheKeys(keys))
	cache.inProgressTTLs = append(cache.inProgressTTLs, ttl)
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
			if result.CacheLayer != test.cached.Layer {
				t.Fatalf("CacheLayer = %q for cache state %q, want %q",
					result.CacheLayer.Token(), test.cached.State.Token(), test.cached.Layer.Token())
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
	cacheTTL := int64(900_000)
	terminationCause := int64(5)
	api := &recordingS2S{response: s2s.Response{
		Data:     json.RawMessage(`{"eids":[{"source":"intentiq.com","uids":[{"id":"resolved"}]}]}`),
		CacheTTL: &cacheTTL, ABTestUUID: "ab-positive", TC: &terminationCause,
	}}
	cache := &recordingCache{result: CacheResult{State: CacheMiss, Layer: CacheLayerNone}}
	metrics := &recordingEnrichmentMetrics{}
	result, err := newCachedTestEnricher(t, api, cache, metrics, &recordingEnrichmentLogger{}, 1).Enrich(t.Context(), cacheableRequest())
	if err != nil || result.Outcome != OutcomeEnriched || result.CacheLayer != CacheLayerNone || len(api.calls) != 1 {
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
	cacheTTL := int64(300_000)
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

func TestEnrichS2SErrorUsesRequestTimeoutForInProgressMarker(t *testing.T) {
	s2sError := errors.New("upstream failed")
	cache := &recordingCache{}
	api := &recordingS2S{err: s2sError}
	request := cacheableRequest()
	request.Timeout = 750 * time.Millisecond
	result, err := newCachedTestEnricher(t, api, cache, &recordingEnrichmentMetrics{}, &recordingEnrichmentLogger{}, 10).Enrich(t.Context(), request)
	if !errors.Is(err, s2sError) || !reflect.DeepEqual(result, Result{}) || len(cache.inProgress) != 1 || len(cache.resolved) != 0 || len(cache.negative) != 0 {
		t.Fatalf("Enrich() = (%#v, %v), cache=%#v", result, err, cache)
	}
	if !reflect.DeepEqual(cache.inProgressTTLs, []time.Duration{request.Timeout}) {
		t.Fatalf("in-progress TTLs = %v, want [%v]", cache.inProgressTTLs, request.Timeout)
	}
}

func TestEnrichRetriesAfterErrorMarkerExpires(t *testing.T) {
	tests := []struct {
		name       string
		firstError error
	}{
		{name: "timeout", firstError: context.DeadlineExceeded},
		{name: "API error", firstError: &iiqapi.Error{Kind: iiqapi.ErrorStatus, Status: 503}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cache := &expiringMarkerCache{now: time.Unix(1_700_000_000, 0)}
			api := &recordingS2S{}
			api.resolve = func(context.Context) (s2s.Response, error) {
				if len(api.calls) == 1 {
					return s2s.Response{}, test.firstError
				}
				return s2s.Response{Data: json.RawMessage(`{"eids":[{"source":"intentiq.com"}]}`)}, nil
			}
			request := cacheableRequest()
			enricher := newCachedTestEnricher(t, api, cache, &recordingEnrichmentMetrics{}, &recordingEnrichmentLogger{}, 10)

			if result, err := enricher.Enrich(t.Context(), request); !errors.Is(err, test.firstError) || !reflect.DeepEqual(result, Result{}) {
				t.Fatalf("first Enrich() = (%#v, %v), want error %v", result, err, test.firstError)
			}
			if result, err := enricher.Enrich(t.Context(), request); err != nil || result.Outcome != OutcomeInProgress || len(api.calls) != 1 {
				t.Fatalf("second Enrich() = (%#v, %v), S2S calls=%d", result, err, len(api.calls))
			}

			cache.now = cache.now.Add(request.Timeout)
			result, err := enricher.Enrich(t.Context(), request)
			if err != nil || result.Outcome != OutcomeEnriched || len(api.calls) != 2 || len(cache.resolved) != 1 {
				t.Fatalf("third Enrich() = (%#v, %v), S2S calls=%d resolved writes=%d", result, err, len(api.calls), len(cache.resolved))
			}
		})
	}
}

func TestCacheWriteSurvivesACallThatSpentTheWholeTimeout(t *testing.T) {
	// The slowest calls are the ones the background modes exist for, and their
	// result still has to reach the cache.
	api := &recordingS2S{resolve: func(ctx context.Context) (s2s.Response, error) {
		<-ctx.Done()
		return s2s.Response{Data: json.RawMessage(
			`{"eids":[{"source":"intentiq.com","uids":[{"id":"resolved"}]}]}`,
		)}, nil
	}}
	cache := &recordingCache{}
	enricher := newCachedTestEnricher(t, api, cache, &recordingEnrichmentMetrics{}, &recordingEnrichmentLogger{}, 10).(*enricher)
	request := cacheableRequest()
	request.Timeout = 50 * time.Millisecond
	keys := []CacheKey{{Value: "pubcid:shared", Type: CacheKeyFirstParty}}

	if _, err := enricher.execute(t.Context(), request, keys, nil); err != nil {
		t.Fatalf("execute() error = %v", err)
	}

	if len(cache.resolved) != 1 || cache.resolvedCtxErr != nil {
		t.Fatalf("resolved writes = %d, context error = %v", len(cache.resolved), cache.resolvedCtxErr)
	}
}

func TestExecuteDetachedFromCallerThroughCacheWrite(t *testing.T) {
	tests := []struct {
		name       string
		response   s2s.Response
		wantResult Outcome
		wantWrite  string
	}{
		{
			name: "positive",
			response: s2s.Response{Data: json.RawMessage(
				`{"eids":[{"source":"intentiq.com","uids":[{"id":"resolved"}]}]}`,
			)},
			wantResult: OutcomeEnriched,
			wantWrite:  "resolved",
		},
		{
			name:       "negative",
			response:   s2s.Response{Data: json.RawMessage(`{"eids":[]}`)},
			wantResult: OutcomeNoIDs,
			wantWrite:  "negative",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			started := make(chan struct{})
			release := make(chan struct{})
			api := &recordingS2S{resolve: func(context.Context) (s2s.Response, error) {
				close(started)
				<-release
				return test.response, nil
			}}
			cache := &recordingCache{}
			enricher := newCachedTestEnricher(t, api, cache, &recordingEnrichmentMetrics{}, &recordingEnrichmentLogger{}, 10).(*enricher)
			request := cacheableRequest()
			wait := 100 * time.Millisecond
			request.WaitTimeout = &wait
			keys := []CacheKey{{Value: "pubcid:shared", Type: CacheKeyFirstParty}}
			parent, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)

			type executionResult struct {
				result Result
				err    error
			}
			finished := make(chan executionResult, 1)
			go func() {
				result, err := enricher.execute(parent, request, keys, nil)
				finished <- executionResult{result: result, err: err}
			}()

			<-started
			cancel()
			close(release)
			got := <-finished
			if got.err != nil || got.result.Outcome != test.wantResult {
				t.Fatalf("execute() = (%#v, %v), want outcome %q", got.result, got.err, test.wantResult)
			}
			switch test.wantWrite {
			case "resolved":
				if len(cache.resolved) != 1 || cache.resolvedCtxErr != nil {
					t.Fatalf("resolved writes = %d, context error = %v", len(cache.resolved), cache.resolvedCtxErr)
				}
			case "negative":
				if len(cache.negative) != 1 || cache.negativeCtxErr != nil {
					t.Fatalf("negative writes = %d, context error = %v", len(cache.negative), cache.negativeCtxErr)
				}
			}
		})
	}
}

func TestPreparedResolutionDoesNotRetainAuctionOrKeySlice(t *testing.T) {
	request := cacheableRequest()
	request.Auction.User.Consent = "original-consent"
	keys := []CacheKey{{Value: "pubcid:original", Type: CacheKeyFirstParty}}
	prepared := prepareResolution(request, keys)

	request.Auction.User.Consent = "mutated-consent"
	request.Auction.User.EIDs[0].UIDs[0].ID = "mutated"
	request.Auction.Device.IP = "203.0.113.10"
	keys[0].Value = "pubcid:mutated"

	api := &recordingS2S{response: s2s.Response{Data: json.RawMessage(
		`{"eids":[{"source":"intentiq.com","uids":[{"id":"resolved"}]}]}`,
	)}}
	cache := &recordingCache{}
	enricher := newCachedTestEnricher(t, api, cache, &recordingEnrichmentMetrics{}, &recordingEnrichmentLogger{}, 10).(*enricher)
	result, err := enricher.executePrepared(t.Context(), prepared)
	if err != nil || result.Outcome != OutcomeEnriched {
		t.Fatalf("executePrepared() = (%#v, %v)", result, err)
	}
	if len(api.calls) != 1 || api.calls[0].consent != "original-consent" || strings.Contains(api.calls[0].requestURL, "mutated") || strings.Contains(api.calls[0].requestURL, "203.0.113.10") {
		t.Fatalf("prepared API call changed after auction mutation: %#v", api.calls)
	}
	if len(cache.resolved) != 1 || cache.resolved[0].keys[0].Value != "pubcid:original" {
		t.Fatalf("prepared cache keys changed after source mutation: %#v", cache.resolved)
	}
}

func TestEnrichAsyncReturnsBeforeResolutionAndWarmsCache(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	api := &recordingS2S{resolve: func(context.Context) (s2s.Response, error) {
		close(started)
		<-release
		return s2s.Response{Data: json.RawMessage(
			`{"eids":[{"source":"intentiq.com","uids":[{"id":"resolved"}]}]}`,
		)}, nil
	}}
	cache := &lifecycleCache{resolved: make(chan struct{})}
	enricher := newCachedTestEnricher(t, api, cache, &recordingEnrichmentMetrics{}, &recordingEnrichmentLogger{}, 10)
	request := cacheableRequest()
	wait := time.Duration(0)
	request.WaitTimeout = &wait

	result, err := enricher.Enrich(t.Context(), request)
	if err != nil || result.Outcome != OutcomeWaitExpired {
		t.Fatalf("Enrich() = (%#v, %v), want wait expiry", result, err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("S2S call did not start")
	}

	request.Auction.User.EIDs[0].UIDs[0].ID = "mutated"
	request.Auction.Device.IP = "203.0.113.10"
	close(release)
	select {
	case <-cache.resolved:
	case <-time.After(time.Second):
		t.Fatal("late S2S result was not cached")
	}
	if strings.Contains(api.calls[0].requestURL, "mutated") || strings.Contains(api.calls[0].requestURL, "203.0.113.10") {
		t.Fatalf("late call retained the auction: %#v", api.calls[0])
	}
}

func TestEnrichAsyncNeverUsesImmediateResultForCurrentAuction(t *testing.T) {
	api := &recordingS2S{response: s2s.Response{Data: json.RawMessage(
		`{"eids":[{"source":"intentiq.com","uids":[{"id":"resolved"}]}]}`,
	)}}
	cache := &lifecycleCache{resolved: make(chan struct{})}
	enricher := newCachedTestEnricher(t, api, cache, &recordingEnrichmentMetrics{}, &recordingEnrichmentLogger{}, 10)
	request := cacheableRequest()
	wait := time.Duration(0)
	request.WaitTimeout = &wait

	result, err := enricher.Enrich(t.Context(), request)
	if err != nil || result.Outcome != OutcomeWaitExpired || len(result.EIDs) != 0 {
		t.Fatalf("Enrich() = (%#v, %v), async mode must not enrich the current auction", result, err)
	}
	select {
	case <-cache.resolved:
	case <-time.After(time.Second):
		t.Fatal("immediate async result was not cached")
	}
}

func TestEnrichHybridCompletesOnEitherSideOfWait(t *testing.T) {
	t.Run("in time", func(t *testing.T) {
		api := &recordingS2S{response: s2s.Response{Data: json.RawMessage(
			`{"eids":[{"source":"intentiq.com","uids":[{"id":"resolved"}]}]}`,
		)}}
		enricher := newTestEnricher(t, api, &recordingEnrichmentMetrics{}, &recordingEnrichmentLogger{})
		request := cacheableRequest()
		request.CacheEnabled = false
		wait := 500 * time.Millisecond
		request.WaitTimeout = &wait
		result, err := enricher.Enrich(t.Context(), request)
		if err != nil || result.Outcome != OutcomeEnriched {
			t.Fatalf("Enrich() = (%#v, %v), want in-time enrichment", result, err)
		}
	})

	t.Run("after wait", func(t *testing.T) {
		release := make(chan struct{})
		api := &recordingS2S{resolve: func(context.Context) (s2s.Response, error) {
			<-release
			return s2s.Response{Data: json.RawMessage(`{"eids":[]}`)}, nil
		}}
		cache := &lifecycleCache{resolved: make(chan struct{}), negative: make(chan struct{})}
		enricher := newCachedTestEnricher(t, api, cache, &recordingEnrichmentMetrics{}, &recordingEnrichmentLogger{}, 10)
		request := cacheableRequest()
		wait := 10 * time.Millisecond
		request.WaitTimeout = &wait
		result, err := enricher.Enrich(t.Context(), request)
		if err != nil || result.Outcome != OutcomeWaitExpired {
			t.Fatalf("Enrich() = (%#v, %v), want wait expiry", result, err)
		}
		close(release)
		select {
		case <-cache.negative:
		case <-time.After(time.Second):
			t.Fatal("late no-ID result was not cached")
		}
	})
}

func TestEnrichAsyncSurvivesCallerCancellationButHonorsCallTimeout(t *testing.T) {
	callFinished := make(chan error, 1)
	api := &recordingS2S{resolve: func(ctx context.Context) (s2s.Response, error) {
		<-ctx.Done()
		callFinished <- ctx.Err()
		return s2s.Response{}, ctx.Err()
	}}
	metrics := &signalingMetrics{apiError: make(chan struct{})}
	created, err := New(Dependencies{S2S: api, Metrics: metrics}, 10)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	enricher := created
	request := cacheableRequest()
	request.CacheEnabled = false
	request.Timeout = 40 * time.Millisecond
	wait := time.Duration(0)
	request.WaitTimeout = &wait
	parent, cancel := context.WithCancel(t.Context())

	result, err := enricher.Enrich(parent, request)
	if err != nil || result.Outcome != OutcomeWaitExpired {
		t.Fatalf("Enrich() = (%#v, %v), want wait expiry", result, err)
	}
	cancel()
	select {
	case callErr := <-callFinished:
		if !errors.Is(callErr, context.DeadlineExceeded) {
			t.Fatalf("call error = %v, want deadline exceeded", callErr)
		}
	case <-time.After(time.Second):
		t.Fatal("S2S call did not reach its full timeout")
	}
	select {
	case <-metrics.apiError:
	case <-time.After(time.Second):
		t.Fatal("late timeout was not recorded")
	}
}

func TestEnrichReturnsInProgressThenLateCachedResult(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	api := &recordingS2S{resolve: func(context.Context) (s2s.Response, error) {
		close(started)
		<-release
		return s2s.Response{Data: json.RawMessage(
			`{"eids":[{"source":"intentiq.com","uids":[{"id":"resolved"}]}]}`,
		)}, nil
	}}
	cache := &lifecycleCache{resolved: make(chan struct{})}
	enricher := newCachedTestEnricher(t, api, cache, &recordingEnrichmentMetrics{}, &recordingEnrichmentLogger{}, 10)
	request := cacheableRequest()
	wait := time.Duration(0)
	request.WaitTimeout = &wait

	first, err := enricher.Enrich(t.Context(), request)
	if err != nil || first.Outcome != OutcomeWaitExpired {
		t.Fatalf("first Enrich() = (%#v, %v)", first, err)
	}
	<-started
	second, err := enricher.Enrich(t.Context(), request)
	if err != nil || second.Outcome != OutcomeInProgress {
		t.Fatalf("second Enrich() = (%#v, %v), want in progress", second, err)
	}
	close(release)
	<-cache.resolved
	third, err := enricher.Enrich(t.Context(), request)
	if err != nil || third.Outcome != OutcomeEnriched || len(third.EIDs) != 1 {
		t.Fatalf("third Enrich() = (%#v, %v), want cached enrichment", third, err)
	}
	if len(api.calls) != 1 {
		t.Fatalf("S2S calls = %d, want 1", len(api.calls))
	}
}

func TestBackgroundLimitRejectsBeforeCallOrInProgressMarker(t *testing.T) {
	api := &recordingS2S{}
	cache := &recordingCache{result: CacheResult{State: CacheMiss}}
	metrics := &recordingEnrichmentMetrics{}
	created, err := New(Dependencies{S2S: api, Cache: cache, Metrics: metrics}, 10)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	implementation := created.(*enricher)
	limiter := &recordingBackgroundLimiter{allow: false}
	implementation.limiter = limiter
	request := cacheableRequest()
	wait := time.Duration(0)
	request.WaitTimeout = &wait

	result, err := implementation.Enrich(t.Context(), request)
	if err != nil || result.Outcome != OutcomeBackgroundLimit {
		t.Fatalf("Enrich() = (%#v, %v), want background limit", result, err)
	}
	if len(api.calls) != 0 || len(cache.inProgress) != 0 {
		t.Fatalf("rejected request made API calls or markers: calls=%d markers=%d", len(api.calls), len(cache.inProgress))
	}
	if acquires, releases := limiter.counts(); acquires != 1 || releases != 0 {
		t.Fatalf("limiter counts = (%d, %d), want (1, 0)", acquires, releases)
	}
	events := metrics.snapshot()
	if len(events) != 3 || events[2].name != "not_enriched" || events[2].reason != string(ReasonBackgroundLimit) {
		t.Fatalf("capacity metrics = %#v", events)
	}
}

func TestConfiguredBackgroundCapacityBoundsPotentiallyDetachedCalls(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	api := &recordingS2S{resolve: func(context.Context) (s2s.Response, error) {
		close(started)
		<-release
		return s2s.Response{Data: json.RawMessage(`{"eids":[]}`)}, nil
	}}
	created, err := New(Dependencies{S2S: api, MaxBackgroundCalls: 1}, 10)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := cacheableRequest()
	request.CacheEnabled = false
	wait := time.Duration(0)
	request.WaitTimeout = &wait

	first, err := created.Enrich(t.Context(), request)
	if err != nil || first.Outcome != OutcomeWaitExpired {
		t.Fatalf("first Enrich() = (%#v, %v)", first, err)
	}
	<-started
	second, err := created.Enrich(t.Context(), request)
	if err != nil || second.Outcome != OutcomeBackgroundLimit {
		t.Fatalf("second Enrich() = (%#v, %v), want capacity rejection", second, err)
	}
	if len(api.calls) != 1 {
		t.Fatalf("S2S calls = %d, want 1", len(api.calls))
	}
	close(release)

	limiter := created.(*enricher).limiter.(boundedBackgroundLimiter)
	deadline := time.Now().Add(time.Second)
	for len(limiter) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(limiter) != 0 {
		t.Fatal("configured background permit was not released")
	}
}

func TestBackgroundLimiterSkippedForCacheResultsAndSyncCalls(t *testing.T) {
	positive := Result{EIDs: []openrtb2.EID{{Source: "intentiq.com"}}}
	tests := []struct {
		name    string
		request Request
		cached  CacheResult
	}{
		{name: "positive hit", request: cacheableRequest(), cached: CacheResult{State: CacheHit, Result: positive}},
		{name: "negative hit", request: cacheableRequest(), cached: CacheResult{State: CacheNegative}},
		{name: "in progress", request: cacheableRequest(), cached: CacheResult{State: CacheInProgress}},
		{name: "sync miss", request: cacheableRequest(), cached: CacheResult{State: CacheMiss}},
	}
	zero := time.Duration(0)
	for index := range 3 {
		tests[index].request.WaitTimeout = &zero
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api := &recordingS2S{response: s2s.Response{Data: json.RawMessage(`{"eids":[]}`)}}
			cache := &recordingCache{result: test.cached}
			created, err := New(Dependencies{S2S: api, Cache: cache}, 10)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			implementation := created.(*enricher)
			limiter := &recordingBackgroundLimiter{allow: false}
			implementation.limiter = limiter
			result, err := implementation.Enrich(t.Context(), test.request)
			if err != nil || result.Outcome == OutcomeBackgroundLimit {
				t.Fatalf("Enrich() = (%#v, %v), limiter should be skipped", result, err)
			}
			if acquires, releases := limiter.counts(); acquires != 0 || releases != 0 {
				t.Fatalf("limiter counts = (%d, %d), want (0, 0)", acquires, releases)
			}
		})
	}
}

func TestBackgroundPermitReleasedForEveryTerminalPath(t *testing.T) {
	tests := []struct {
		name    string
		wait    time.Duration
		timeout time.Duration
		resolve func(context.Context) (s2s.Response, error)
	}{
		{
			name: "in-time success", wait: 500 * time.Millisecond, timeout: time.Second,
			resolve: func(context.Context) (s2s.Response, error) {
				return s2s.Response{Data: json.RawMessage(`{"eids":[]}`)}, nil
			},
		},
		{
			name: "late success", wait: 0, timeout: time.Second,
			resolve: func(context.Context) (s2s.Response, error) {
				return s2s.Response{Data: json.RawMessage(`{"eids":[]}`)}, nil
			},
		},
		{
			name: "error", wait: 500 * time.Millisecond, timeout: time.Second,
			resolve: func(context.Context) (s2s.Response, error) { return s2s.Response{}, errors.New("failed") },
		},
		{
			name: "timeout", wait: 0, timeout: 20 * time.Millisecond,
			resolve: func(ctx context.Context) (s2s.Response, error) {
				<-ctx.Done()
				return s2s.Response{}, ctx.Err()
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			api := &recordingS2S{resolve: test.resolve}
			created, err := New(Dependencies{S2S: api}, 10)
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			implementation := created.(*enricher)
			limiter := &recordingBackgroundLimiter{allow: true, released: make(chan struct{}, 1)}
			implementation.limiter = limiter
			request := cacheableRequest()
			request.CacheEnabled = false
			request.Timeout = test.timeout
			request.WaitTimeout = &test.wait
			_, _ = implementation.Enrich(t.Context(), request)
			select {
			case <-limiter.released:
			case <-time.After(time.Second):
				t.Fatal("background permit was not released")
			}
			if acquires, releases := limiter.counts(); acquires != 1 || releases != 1 {
				t.Fatalf("limiter counts = (%d, %d), want (1, 1)", acquires, releases)
			}
		})
	}
}
