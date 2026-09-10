package cache

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Intent-IQ/identity-go/enrichment"
	"github.com/prebid/openrtb/v20/openrtb2"
)

var errTestStore = errors.New("store unavailable")

type storedValue struct {
	value []byte
	ttl   time.Duration
}

type recordingStore struct {
	mu        sync.Mutex
	values    map[string]storedValue
	getErr    error
	getErrFor map[string]error
	putErr    error
	gets      []string
	puts      []string
	onGet     func()
	onPut     func()
	closed    bool
}

func newRecordingStore() *recordingStore {
	return &recordingStore{
		values:    make(map[string]storedValue),
		getErrFor: make(map[string]error),
	}
}

func (store *recordingStore) Get(_ context.Context, key string) ([]byte, error) {
	store.mu.Lock()
	store.gets = append(store.gets, key)
	value, found := store.values[key]
	err := store.getErr
	if keyError := store.getErrFor[key]; keyError != nil {
		err = keyError
	}
	hook := store.onGet
	store.mu.Unlock()
	if hook != nil {
		hook()
	}
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	return append([]byte(nil), value.value...), nil
}

func (store *recordingStore) Put(_ context.Context, key string, value []byte, ttl time.Duration) error {
	store.mu.Lock()
	store.puts = append(store.puts, key)
	err := store.putErr
	if err == nil {
		store.values[key] = storedValue{value: append([]byte(nil), value...), ttl: ttl}
	}
	hook := store.onPut
	store.mu.Unlock()
	if hook != nil {
		hook()
	}
	return err
}

func (store *recordingStore) Close() error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.closed = true
	return nil
}

func (store *recordingStore) stored(key string) (storedValue, bool) {
	store.mu.Lock()
	defer store.mu.Unlock()
	value, found := store.values[key]
	return value, found
}

func (store *recordingStore) getCount() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return len(store.gets)
}

type metricEvent struct{ operation, result string }

type recordingCacheMetrics struct {
	mu           sync.Mutex
	events       []metricEvent
	getLatencies []time.Duration
	putLatencies []time.Duration
}

func (metrics *recordingCacheMetrics) L2Request(operation, result string) {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	metrics.events = append(metrics.events, metricEvent{operation, result})
}

func (metrics *recordingCacheMetrics) L2GetLatency(duration time.Duration) {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	metrics.getLatencies = append(metrics.getLatencies, duration)
}

func (metrics *recordingCacheMetrics) L2PutLatency(duration time.Duration) {
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	metrics.putLatencies = append(metrics.putLatencies, duration)
}

type recordingLogger struct {
	mu       sync.Mutex
	warnings []string
}

func (*recordingLogger) Debug(string) {}
func (*recordingLogger) Error(string) {}
func (logger *recordingLogger) Warn(message string) {
	logger.mu.Lock()
	defer logger.mu.Unlock()
	logger.warnings = append(logger.warnings, message)
}

func testCacheConfig() Config {
	return Config{
		Enabled: true, MaxSize: 1024 * 1024, TTLSeconds: 1800,
		TTLCeilingFirstPartySeconds: 3600,
		TTLCeilingThirdPartySeconds: 600,
		TTLCeilingDeviceSeconds:     300,
		NegativeTTLSeconds:          120,
	}
}

func newTestIdentityCache(t *testing.T) (*identityCache, *recordingStore, *recordingCacheMetrics, *recordingLogger, *mutableClock) {
	t.Helper()
	store := newRecordingStore()
	metrics := &recordingCacheMetrics{}
	logger := &recordingLogger{}
	clock := &mutableClock{now: time.UnixMilli(1_700_000_000_000)}
	created, err := New(Dependencies{Store: store, Metrics: metrics, Logger: logger, Clock: clock}, testCacheConfig())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	cache, ok := created.(*identityCache)
	if !ok {
		t.Fatalf("New() returned %T, want *identityCache", created)
	}
	return cache, store, metrics, logger, clock
}

func TestNewCacheConstruction(t *testing.T) {
	disabled, err := New(Dependencies{}, Config{})
	if err != nil || disabled != nil {
		t.Fatalf("disabled New() = (%T, %v), want (nil, nil)", disabled, err)
	}
	if _, err := New(Dependencies{}, Config{Enabled: true}); !errors.Is(err, errStoreRequired) {
		t.Fatalf("enabled New() error = %v, want %v", err, errStoreRequired)
	}
	created, err := New(Dependencies{Store: newRecordingStore()}, testCacheConfig())
	if err != nil || created == nil {
		t.Fatalf("New() with default dependencies = (%T, %v)", created, err)
	}
}

func TestIdentityCacheEmptyKeysMiss(t *testing.T) {
	cache, store, _, _, _ := newTestIdentityCache(t)
	for _, keys := range [][]enrichment.CacheKey{nil, {}} {
		result, err := cache.Get(t.Context(), keys)
		if err != nil || result.State != enrichment.CacheMiss || result.Layer != enrichment.CacheLayerNone {
			t.Fatalf("Get() = (%#v, %v), want miss", result, err)
		}
	}
	if store.getCount() != 0 {
		t.Fatalf("empty lookup made %d Store.Get calls", store.getCount())
	}
}

func TestIdentityCacheResolvedL1HitPreservesMetadata(t *testing.T) {
	cache, store, metrics, _, _ := newTestIdentityCache(t)
	terminationCause := int64(120088)
	keys := []enrichment.CacheKey{{Value: "fp", Type: enrichment.CacheKeyFirstParty}}
	want := enrichment.Result{
		EIDs: []openrtb2.EID{{Source: "a.com"}}, CacheTTL: 3 * time.Minute,
		ABTestUUID: "ab-1", TerminationCause: &terminationCause,
	}
	if err := cache.PutResolved(t.Context(), keys, want); err != nil {
		t.Fatalf("PutResolved() error = %v", err)
	}
	stored, found := store.stored("fp")
	if !found || stored.ttl != 3*time.Minute {
		t.Fatalf("stored value = (%#v, %t), want TTL 3m", stored, found)
	}

	result, err := cache.Get(t.Context(), keys)
	if err != nil || result.State != enrichment.CacheHit || result.Layer != enrichment.CacheLayerL1 || result.KeyType != enrichment.CacheKeyFirstParty {
		t.Fatalf("Get() = (%#v, %v)", result, err)
	}
	if !reflect.DeepEqual(result.Result.EIDs, want.EIDs) || result.Result.ABTestUUID != "ab-1" || result.Result.TerminationCause == nil || *result.Result.TerminationCause != terminationCause {
		t.Fatalf("cached result = %#v, want metadata and EIDs preserved", result.Result)
	}
	if store.getCount() != 0 {
		t.Fatal("L1 hit queried L2")
	}
	assertMetricEvents(t, metrics.events, []metricEvent{{OperationPut, ResultStored}})
}

func TestIdentityCacheL2HitPromotesToL1(t *testing.T) {
	cache, store, metrics, _, _ := newTestIdentityCache(t)
	key := enrichment.CacheKey{Value: "fp", Type: enrichment.CacheKeyFirstParty}
	value := cache.codec.resolved(enrichment.Result{EIDs: []openrtb2.EID{{Source: "b.com"}}}, 20*time.Minute)
	encoded, _ := cache.codec.encode(value)
	if err := store.Put(t.Context(), key.Value, encoded, time.Hour); err != nil {
		t.Fatalf("seed Put() error = %v", err)
	}

	result, err := cache.Get(t.Context(), []enrichment.CacheKey{key})
	if err != nil || result.State != enrichment.CacheHit || result.Layer != enrichment.CacheLayerL2 {
		t.Fatalf("first Get() = (%#v, %v)", result, err)
	}
	firstGetCount := store.getCount()
	result, err = cache.Get(t.Context(), []enrichment.CacheKey{key})
	if err != nil || result.Layer != enrichment.CacheLayerL1 || store.getCount() != firstGetCount {
		t.Fatalf("promoted Get() = (%#v, %v), L2 gets=%d", result, err, store.getCount())
	}
	assertMetricEvents(t, metrics.events, []metricEvent{{OperationGet, ResultHit}})
}

func TestIdentityCacheAliasBackfillUsesRemainingTTLAndDestinationCeiling(t *testing.T) {
	cache, store, _, _, clock := newTestIdentityCache(t)
	primary := enrichment.CacheKey{Value: "primary", Type: enrichment.CacheKeyFirstParty}
	secondary := enrichment.CacheKey{Value: "secondary", Type: enrichment.CacheKeyThirdParty}
	result := enrichment.Result{
		EIDs: []openrtb2.EID{{Source: "a.com"}}, CacheTTL: 30 * time.Minute,
		ABTestUUID: "ab-3", TerminationCause: int64Pointer(7),
	}
	_ = cache.PutResolved(t.Context(), []enrichment.CacheKey{primary}, result)
	clock.advance(time.Minute)

	lookup, err := cache.Get(t.Context(), []enrichment.CacheKey{primary, secondary})
	if err != nil || lookup.State != enrichment.CacheHit || lookup.KeyType != enrichment.CacheKeyFirstParty {
		t.Fatalf("Get() = (%#v, %v)", lookup, err)
	}
	alias, found := store.stored(secondary.Value)
	if !found || alias.ttl != 10*time.Minute {
		t.Fatalf("alias = (%#v, %t), want destination ceiling 10m", alias, found)
	}
	decoded, valid := cache.codec.decode(alias.value)
	if !valid || decoded.ExpiresAt != clock.Now().Add(10*time.Minute).UnixMilli() || decoded.ABTestUUID != "ab-3" || decoded.TerminationCause == nil || *decoded.TerminationCause != 7 {
		t.Fatalf("decoded alias = (%#v, %t)", decoded, valid)
	}

	aliasResult, _ := cache.Get(t.Context(), []enrichment.CacheKey{secondary})
	if aliasResult.State != enrichment.CacheHit || aliasResult.Layer != enrichment.CacheLayerL1 || aliasResult.KeyType != enrichment.CacheKeyThirdParty {
		t.Fatalf("alias Get() = %#v", aliasResult)
	}
}

func TestIdentityCacheAliasBackfillUsesUncappedRemainingTTL(t *testing.T) {
	cache, store, _, _, clock := newTestIdentityCache(t)
	primary := enrichment.CacheKey{Value: "third-party", Type: enrichment.CacheKeyThirdParty}
	alias := enrichment.CacheKey{Value: "first-party", Type: enrichment.CacheKeyFirstParty}
	_ = cache.PutResolved(t.Context(), []enrichment.CacheKey{primary}, enrichment.Result{CacheTTL: 8 * time.Minute})
	clock.advance(3 * time.Minute)

	_, _ = cache.Get(t.Context(), []enrichment.CacheKey{primary, alias})
	stored, found := store.stored(alias.Value)
	if !found || stored.ttl != 5*time.Minute {
		t.Fatalf("alias = (%#v, %t), want remaining TTL 5m", stored, found)
	}
}

func TestIdentityCacheNegativeAndInProgress(t *testing.T) {
	cache, store, _, _, _ := newTestIdentityCache(t)
	negativeKey := enrichment.CacheKey{Value: "negative", Type: enrichment.CacheKeyThirdParty}
	metadata := enrichment.ResultMetadata{CacheTTL: 30 * time.Second, ABTestUUID: "ab-2", TerminationCause: int64Pointer(8)}
	_ = cache.PutNegative(t.Context(), []enrichment.CacheKey{negativeKey}, metadata)
	stored, _ := store.stored(negativeKey.Value)
	if stored.ttl != 30*time.Second {
		t.Fatalf("negative TTL = %v, want 30s", stored.ttl)
	}
	negative, _ := cache.Get(t.Context(), []enrichment.CacheKey{negativeKey})
	if negative.State != enrichment.CacheNegative || negative.Result.ABTestUUID != "ab-2" || negative.Result.TerminationCause == nil || *negative.Result.TerminationCause != 8 {
		t.Fatalf("negative result = %#v", negative)
	}

	inProgressKey := enrichment.CacheKey{Value: "in-progress", Type: enrichment.CacheKeyDevice}
	_ = cache.PutInProgress(t.Context(), []enrichment.CacheKey{inProgressKey}, 15*time.Second)
	stored, _ = store.stored(inProgressKey.Value)
	if stored.ttl != 15*time.Second {
		t.Fatalf("in-progress TTL = %v, want 15s", stored.ttl)
	}
	inProgress, _ := cache.Get(t.Context(), []enrichment.CacheKey{inProgressKey})
	if inProgress.State != enrichment.CacheInProgress || inProgress.Layer != enrichment.CacheLayerL1 || inProgress.KeyType != enrichment.CacheKeyDevice {
		t.Fatalf("in-progress result = %#v", inProgress)
	}
}

func TestIdentityCacheResolvedWinsOverEarlierInProgressWithinLayer(t *testing.T) {
	cache, _, _, _, _ := newTestIdentityCache(t)
	inProgress := enrichment.CacheKey{Value: "ip", Type: enrichment.CacheKeyFirstParty}
	resolved := enrichment.CacheKey{Value: "resolved", Type: enrichment.CacheKeyThirdParty}
	_ = cache.PutInProgress(t.Context(), []enrichment.CacheKey{inProgress}, 15*time.Second)
	_ = cache.PutResolved(t.Context(), []enrichment.CacheKey{resolved}, enrichment.Result{EIDs: []openrtb2.EID{{Source: "a.com"}}})

	result, _ := cache.Get(t.Context(), []enrichment.CacheKey{inProgress, resolved})
	if result.State != enrichment.CacheHit || result.KeyType != enrichment.CacheKeyThirdParty {
		t.Fatalf("Get() = %#v, want resolved second key", result)
	}
}

func TestIdentityCacheResolvedL2EntryWinsOverEarlierL2InProgress(t *testing.T) {
	cache, store, metrics, _, _ := newTestIdentityCache(t)
	inProgress := enrichment.CacheKey{Value: "ip", Type: enrichment.CacheKeyFirstParty}
	resolved := enrichment.CacheKey{Value: "resolved", Type: enrichment.CacheKeyThirdParty}
	for key, value := range map[string]entry{
		inProgress.Value: cache.codec.inProgress(time.Minute),
		resolved.Value:   cache.codec.resolved(enrichment.Result{EIDs: []openrtb2.EID{{Source: "a.com"}}}, time.Minute),
	} {
		encoded, _ := cache.codec.encode(value)
		_ = store.Put(t.Context(), key, encoded, time.Minute)
	}

	result, _ := cache.Get(t.Context(), []enrichment.CacheKey{inProgress, resolved})
	if result.State != enrichment.CacheHit || result.Layer != enrichment.CacheLayerL2 || result.KeyType != enrichment.CacheKeyThirdParty {
		t.Fatalf("Get() = %#v, want resolved L2 second key", result)
	}
	assertMetricEvents(t, metrics.events, []metricEvent{{OperationGet, ResultHit}, {OperationGet, ResultHit}})
}

func TestIdentityCacheL1InProgressShortCircuitsL2(t *testing.T) {
	cache, store, _, _, _ := newTestIdentityCache(t)
	inProgress := enrichment.CacheKey{Value: "ip", Type: enrichment.CacheKeyFirstParty}
	resolved := enrichment.CacheKey{Value: "resolved", Type: enrichment.CacheKeyThirdParty}
	_ = cache.PutInProgress(t.Context(), []enrichment.CacheKey{inProgress}, 15*time.Second)
	value := cache.codec.resolved(enrichment.Result{EIDs: []openrtb2.EID{{Source: "a.com"}}}, time.Minute)
	encoded, _ := cache.codec.encode(value)
	_ = store.Put(t.Context(), resolved.Value, encoded, time.Minute)
	before := store.getCount()

	result, _ := cache.Get(t.Context(), []enrichment.CacheKey{inProgress, resolved})
	if result.State != enrichment.CacheInProgress || result.Layer != enrichment.CacheLayerL1 || store.getCount() != before {
		t.Fatalf("Get() = %#v, Store.Get count=%d, want L1 in-progress without L2 lookup", result, store.getCount())
	}
}

func TestIdentityCacheFullMissAndExpiredL2Entry(t *testing.T) {
	cache, store, metrics, _, clock := newTestIdentityCache(t)
	expired, _ := cache.codec.encode(entry{ExpiresAt: clock.Now().UnixMilli()})
	_ = store.Put(t.Context(), "expired", expired, time.Hour)
	keys := []enrichment.CacheKey{
		{Value: "expired", Type: enrichment.CacheKeyFirstParty},
		{Value: "absent", Type: enrichment.CacheKeyDevice},
	}
	result, err := cache.Get(t.Context(), keys)
	if err != nil || result.State != enrichment.CacheMiss || result.Layer != enrichment.CacheLayerNone {
		t.Fatalf("Get() = (%#v, %v), want miss", result, err)
	}
	assertMetricEvents(t, metrics.events, []metricEvent{{OperationGet, ResultMiss}, {OperationGet, ResultMiss}})
}

func TestIdentityCacheTTLCoordinates(t *testing.T) {
	cache, store, _, _, _ := newTestIdentityCache(t)
	keys := []enrichment.CacheKey{
		{Value: "first", Type: enrichment.CacheKeyFirstParty},
		{Value: "third", Type: enrichment.CacheKeyThirdParty},
		{Value: "device", Type: enrichment.CacheKeyDevice},
	}
	_ = cache.PutResolved(t.Context(), keys, enrichment.Result{CacheTTL: 24 * time.Hour})
	wants := map[string]time.Duration{"first": time.Hour, "third": 10 * time.Minute, "device": 5 * time.Minute}
	for key, want := range wants {
		stored, found := store.stored(key)
		if !found || stored.ttl != want {
			t.Fatalf("%s stored TTL = %v, found=%t, want %v", key, stored.ttl, found, want)
		}
	}

	_ = cache.PutNegative(t.Context(), []enrichment.CacheKey{{Value: "negative-default"}}, enrichment.ResultMetadata{})
	_ = cache.PutNegative(t.Context(), []enrichment.CacheKey{{Value: "negative-capped"}}, enrichment.ResultMetadata{CacheTTL: 48 * time.Hour})
	if value, _ := store.stored("negative-default"); value.ttl != 2*time.Minute {
		t.Fatalf("default negative TTL = %v", value.ttl)
	}
	if value, _ := store.stored("negative-capped"); value.ttl != time.Hour {
		t.Fatalf("capped negative TTL = %v", value.ttl)
	}
}

func TestIdentityCacheStoreErrorsFailOpenAndAreObservable(t *testing.T) {
	cache, store, metrics, logger, clock := newTestIdentityCache(t)
	store.getErr = errTestStore
	store.onGet = func() { clock.advance(5 * time.Millisecond) }
	result, err := cache.Get(t.Context(), []enrichment.CacheKey{{Value: "missing", Type: enrichment.CacheKeyFirstParty}})
	if err != nil || result.State != enrichment.CacheMiss {
		t.Fatalf("Get() = (%#v, %v), want fail-open miss", result, err)
	}
	assertMetricEvents(t, metrics.events, []metricEvent{{OperationGet, ResultError}})
	if !reflect.DeepEqual(metrics.getLatencies, []time.Duration{5 * time.Millisecond}) {
		t.Fatalf("get latencies = %v", metrics.getLatencies)
	}
	if len(logger.warnings) != 1 || !strings.Contains(logger.warnings[0], "identity cache L2 get failed: store unavailable") {
		t.Fatalf("warnings = %q", logger.warnings)
	}

	store.getErr = nil
	store.putErr = errTestStore
	store.onPut = func() { clock.advance(7 * time.Millisecond) }
	metrics.events = nil
	if err := cache.PutResolved(t.Context(), []enrichment.CacheKey{{Value: "local", Type: enrichment.CacheKeyFirstParty}}, enrichment.Result{EIDs: []openrtb2.EID{{Source: "a.com"}}}); err != nil {
		t.Fatalf("PutResolved() error = %v", err)
	}
	assertMetricEvents(t, metrics.events, []metricEvent{{OperationPut, ResultError}})
	if !reflect.DeepEqual(metrics.putLatencies, []time.Duration{7 * time.Millisecond}) {
		t.Fatalf("put latencies = %v", metrics.putLatencies)
	}
	if len(logger.warnings) != 2 || !strings.Contains(logger.warnings[1], "identity cache L2 put failed: store unavailable") {
		t.Fatalf("warnings = %q", logger.warnings)
	}
	result, _ = cache.Get(t.Context(), []enrichment.CacheKey{{Value: "local", Type: enrichment.CacheKeyFirstParty}})
	if result.State != enrichment.CacheHit || result.Layer != enrichment.CacheLayerL1 {
		t.Fatalf("L1 after failed L2 write = %#v", result)
	}
}

func TestIdentityCacheContinuesAfterL2ReadErrorAndFindsLaterAlias(t *testing.T) {
	cache, store, metrics, logger, _ := newTestIdentityCache(t)
	first := enrichment.CacheKey{Value: "broken", Type: enrichment.CacheKeyFirstParty}
	second := enrichment.CacheKey{Value: "resolved", Type: enrichment.CacheKeyThirdParty}
	value := cache.codec.resolved(enrichment.Result{EIDs: []openrtb2.EID{{Source: "a.com"}}}, time.Minute)
	encoded, _ := cache.codec.encode(value)
	_ = store.Put(t.Context(), second.Value, encoded, time.Minute)

	store.getErrFor[first.Value] = errTestStore

	result, err := cache.Get(t.Context(), []enrichment.CacheKey{first, second})
	if err != nil || result.State != enrichment.CacheHit || result.KeyType != enrichment.CacheKeyThirdParty {
		t.Fatalf("Get() = (%#v, %v), want later alias hit", result, err)
	}
	assertMetricEvents(t, metrics.events, []metricEvent{
		{OperationGet, ResultError},
		{OperationGet, ResultHit},
		{OperationPut, ResultStored},
	})
	if len(logger.warnings) != 1 {
		t.Fatalf("warnings = %q, want one read failure", logger.warnings)
	}
}

func TestIdentityCacheConcurrentMissesRemainBestEffort(t *testing.T) {
	cache, _, _, _, _ := newTestIdentityCache(t)
	key := []enrichment.CacheKey{{Value: "shared", Type: enrichment.CacheKeyFirstParty}}
	const callers = 8
	results := make(chan enrichment.CacheResult, callers)
	var workers sync.WaitGroup
	for range callers {
		workers.Add(1)
		go func() {
			defer workers.Done()
			result, _ := cache.Get(context.Background(), key)
			results <- result
		}()
	}
	workers.Wait()
	close(results)
	for result := range results {
		if result.State != enrichment.CacheMiss {
			t.Fatalf("concurrent Get() = %#v, want miss", result)
		}
	}
}

func assertMetricEvents(t *testing.T, got, want []metricEvent) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("metric events = %#v, want %#v", got, want)
	}
}

func int64Pointer(value int64) *int64 { return &value }
