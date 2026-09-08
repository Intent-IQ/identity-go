package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Intent-IQ/identity-go/clock"
	"github.com/Intent-IQ/identity-go/enrichment"
	"github.com/Intent-IQ/identity-go/logging"
)

var errStoreRequired = errors.New("cache store is required when caching is enabled")

type identityCache struct {
	local   *localCache
	store   Store
	policy  TTLPolicy
	codec   entryCodec
	metrics Metrics
	logger  logging.Logger
	clock   clock.Clock
}

// New constructs the generic two-layer cache. A disabled configuration returns
// a nil cache so callers cannot accidentally get an implicit L1-only cache.
func New(dependencies Dependencies, config Config) (enrichment.Cache, error) {
	if !config.Enabled {
		return nil, nil
	}
	if dependencies.Store == nil {
		return nil, errStoreRequired
	}
	if dependencies.Metrics == nil {
		dependencies.Metrics = NoopMetrics{}
	}
	if dependencies.Logger == nil {
		dependencies.Logger = logging.NoopLogger{}
	}
	if dependencies.Clock == nil {
		dependencies.Clock = clock.RealClock{}
	}

	codec := newEntryCodec(dependencies.Clock)
	return &identityCache{
		local:   newLocalCache(config.MaxSize, codec),
		store:   dependencies.Store,
		policy:  config.TTLPolicy(),
		codec:   codec,
		metrics: dependencies.Metrics,
		logger:  dependencies.Logger,
		clock:   dependencies.Clock,
	}, nil
}

func (cache *identityCache) Get(ctx context.Context, keys []enrichment.CacheKey) (enrichment.CacheResult, error) {
	if len(keys) == 0 {
		return missResult(), nil
	}

	var inProgressKeyType enrichment.CacheKeyType
	inProgressFound := false
	for index, key := range keys {
		value, found := cache.local.get(key.Value)
		if !found {
			continue
		}
		if value.InProgress {
			if !inProgressFound {
				inProgressKeyType = key.Type
				inProgressFound = true
			}
			continue
		}
		cache.backfill(ctx, keys, index, value)
		return cacheResultFromEntry(value, key.Type, enrichment.CacheLayerL1), nil
	}
	if inProgressFound {
		return inProgressResult(inProgressKeyType, enrichment.CacheLayerL1), nil
	}

	var l2InProgressKeyType enrichment.CacheKeyType
	l2InProgressFound := false
	for index, key := range keys {
		value, found := cache.getFromStore(ctx, key.Value)
		if !found {
			continue
		}
		if value.InProgress {
			if !l2InProgressFound {
				cache.promote(key.Value, value)
				l2InProgressKeyType = key.Type
				l2InProgressFound = true
			}
			continue
		}
		cache.promote(key.Value, value)
		cache.backfill(ctx, keys, index, value)
		return cacheResultFromEntry(value, key.Type, enrichment.CacheLayerL2), nil
	}
	if l2InProgressFound {
		return inProgressResult(l2InProgressKeyType, enrichment.CacheLayerL2), nil
	}
	return missResult(), nil
}

func (cache *identityCache) PutResolved(ctx context.Context, keys []enrichment.CacheKey, result enrichment.Result) error {
	for _, key := range keys {
		ttl := cache.policy.EffectiveTTL(key.Type, result.CacheTTL)
		cache.writeBoth(ctx, key.Value, cache.codec.resolved(result, ttl), ttl)
	}
	return nil
}

func (cache *identityCache) PutNegative(ctx context.Context, keys []enrichment.CacheKey, metadata enrichment.ResultMetadata) error {
	ttl := cache.policy.NegativeTTLFor(metadata.CacheTTL)
	for _, key := range keys {
		cache.writeBoth(ctx, key.Value, cache.codec.negative(metadata, ttl), ttl)
	}
	return nil
}

func (cache *identityCache) PutInProgress(ctx context.Context, keys []enrichment.CacheKey) error {
	ttl := cache.policy.InProgressTTL
	for _, key := range keys {
		cache.writeBoth(ctx, key.Value, cache.codec.inProgress(ttl), ttl)
	}
	return nil
}

func (cache *identityCache) backfill(ctx context.Context, keys []enrichment.CacheKey, hitIndex int, hit entry) {
	remainingMilliseconds := hit.ExpiresAt - cache.clock.Now().UnixMilli()
	if remainingMilliseconds <= 0 {
		return
	}

	for index, key := range keys {
		if index == hitIndex {
			continue
		}
		if _, found := cache.local.get(key.Value); found {
			continue
		}

		ttlMilliseconds := remainingMilliseconds
		if ceiling := cache.policy.CeilingFor(key.Type).Milliseconds(); ceiling < ttlMilliseconds {
			ttlMilliseconds = ceiling
		}
		ttl := time.Duration(ttlMilliseconds) * time.Millisecond
		alias := entry{
			EIDs:             hit.EIDs,
			ABTestUUID:       hit.ABTestUUID,
			TerminationCause: hit.TerminationCause,
			Negative:         hit.Negative,
			InProgress:       hit.InProgress,
			ExpiresAt:        cache.clock.Now().UnixMilli() + ttlMilliseconds,
		}
		cache.writeBoth(ctx, key.Value, alias, ttl)
	}
}

func (cache *identityCache) promote(key string, value entry) {
	remainingMilliseconds := value.ExpiresAt - cache.clock.Now().UnixMilli()
	if remainingMilliseconds <= 0 {
		return
	}
	encoded, err := cache.codec.encode(value)
	if err != nil {
		return
	}
	_ = cache.local.set(key, encoded, time.Duration(remainingMilliseconds)*time.Millisecond)
}

func (cache *identityCache) writeBoth(ctx context.Context, key string, value entry, ttl time.Duration) {
	encoded, err := cache.codec.encode(value)
	if err != nil {
		return
	}
	_ = cache.local.set(key, encoded, ttl)

	started := cache.clock.Now()
	err = cache.store.Put(ctx, key, encoded, ttl)
	cache.metrics.L2PutLatency(cache.clock.Now().Sub(started))
	if err != nil {
		cache.metrics.L2Request(OperationPut, ResultError)
		cache.logger.Warn(fmt.Sprintf("identity cache L2 put failed: %v", err))
		return
	}
	cache.metrics.L2Request(OperationPut, ResultStored)
}

func (cache *identityCache) getFromStore(ctx context.Context, key string) (entry, bool) {
	started := cache.clock.Now()
	encoded, err := cache.store.Get(ctx, key)
	cache.metrics.L2GetLatency(cache.clock.Now().Sub(started))
	if err != nil {
		cache.metrics.L2Request(OperationGet, ResultError)
		cache.logger.Warn(fmt.Sprintf("identity cache L2 get failed: %v", err))
		return entry{}, false
	}
	value, valid := cache.codec.decode(encoded)
	if !valid {
		cache.metrics.L2Request(OperationGet, ResultMiss)
		return entry{}, false
	}
	cache.metrics.L2Request(OperationGet, ResultHit)
	return value, true
}

func missResult() enrichment.CacheResult {
	return enrichment.CacheResult{State: enrichment.CacheMiss, Layer: enrichment.CacheLayerNone}
}

func inProgressResult(keyType enrichment.CacheKeyType, layer enrichment.CacheLayer) enrichment.CacheResult {
	return enrichment.CacheResult{State: enrichment.CacheInProgress, KeyType: keyType, Layer: layer}
}

var _ enrichment.Cache = (*identityCache)(nil)
