# Enrichment implementation plan

## Goal

Move the reusable identity-resolution flow from `prebid-go-module` into `identity-go` without changing observable behavior. Existing `prebid-go-module` enrichment tests must continue to pass after integration.

## Approved interfaces

```go
package s2s

type API interface {
	Resolve(ctx context.Context, requestURL, consent string) (Response, error)
}
```

```go
package enrichment

type Enricher interface {
	Enrich(context.Context, Request) (Result, error)
}
```

## Implementation

1. Implement `iiqapi/s2s.API` as a small HTTP client. It sends the supplied URL with an optional `gdpr-consent` header, validates the status, parses the response, and returns typed errors.
2. Implement `enrichment.Enricher` using injected S2S, cache, metrics, and logger interfaces.
3. Move OpenRTB extraction and exact S2S URL construction into `enrichment`:
   - fixed `at=39`, `mi=10`, `pt=17`, `dpn=1`, `srvrReq=true`, and `source=pbsgo` parameters;
   - IP, IPv6, UA, UA hints, device ID, referrer, existing IIQ UID, GDPR, US Privacy, GPP, and TCF consent;
   - the same parameter order and encoding as `prebid-go-module`.
4. Return EIDs, cache TTL, A/B test UUID, termination cause, and outcome in `enrichment.Result`.
5. Move the reusable cache state machine, S2S timeout, and metric events into `enrichment`.
6. Keep concrete cache backends, concrete metrics exporters, logging, tracing presentation, hook mutations, and final fail-open handling in each Prebid Server adapter.
7. Replace the local resolution code in `prebid-go-module` only after compatibility tests pass.

## Dependencies

The interfaces belong in `enrichment` because they are ports consumed by the enrichment workflow:

```text
enrichment/
  enrichment.go       # Enricher, Request, and Result
  cache.go            # Cache interface and cache models
  metrics.go          # Metrics interface, labels, and no-op implementation
  resolver.go         # Enricher implementation and cache state machine
  params.go           # OpenRTB extraction and exact S2S URL construction
cache/
  cache.go            # generic identity-cache implementation
  store.go            # backend-neutral Store interface
  memory.go           # FreeCache-backed L1 cache
clock/
  clock.go            # shared Clock and real implementation
integrations/
  prometheus/         # enrichment.Metrics implementation
  aerospike/          # cache.Store implementation
  redis/              # cache.Store implementation
  valkey/              # cache.Store implementation
logging/
  logger.go            # shared Logger interface and no-op implementation
```

The core module must not import Prometheus, Redis, Valkey, or Aerospike. It may depend on `github.com/coocood/freecache` for the initial L1 implementation. Each integration is optional and should have an independent `go.mod`, so consumers acquire only the dependencies they select.

### Cache

```go
type Cache interface {
	Get(context.Context, []CacheKey) (CacheResult, error)
	PutResolved(context.Context, []CacheKey, Result) error
	PutNegative(context.Context, []CacheKey, ResultMetadata) error
	PutInProgress(context.Context, []CacheKey) error
}
```

`CacheResult` represents `miss`, `hit`, `negative`, or `in_progress` and includes the matching layer and key type. Cache read errors behave as misses; cache write errors never fail successful enrichment. A nil cache disables caching; a no-op cache may also be supplied.

### Cache storage

The reusable generic cache implements `enrichment.Cache` and internally depends on a smaller storage interface:

```go
package cache

type Store interface {
	Get(context.Context, string) ([]byte, error)
	Put(context.Context, string, []byte, time.Duration) error
	Close() error
}
```

The generic cache owns L1 memory caching, ordered alias lookup and backfill, positive and negative entries, in-progress markers, serialization, TTL policy, and fail-open storage behavior. Backend integrations only implement `cache.Store`; they must not duplicate the identity-cache state machine.

The first release retains FreeCache to match the existing byte-bounded, concurrency-safe L1 behavior. Preserve the 512 KiB minimum capacity, whole-second TTL rounding with a one-second floor, absolute-expiry validation, remaining-TTL promotion, and target-key TTL ceilings during alias backfill. FreeCache remains internal and may be replaced later without changing public interfaces.

`Get` returns `(nil, nil)` for a missing key. `Put` replaces an existing value. TTL values must be positive and use whole-second compatibility. `Close` must be idempotent.

In-progress deduplication remains best-effort: the cache performs `Get`, then writes in-progress markers with `Put`. Concurrent callers may both observe a miss and call S2S. Atomic acquisition is intentionally deferred to avoid expanding the first storage contract and changing established behavior.

Cache keys, namespaces, normalization, and serialized positive, negative, and in-progress entries must remain byte-compatible with `prebid-go-module`. This allows old and new servers to share the same backend during a rolling deployment without flushing the cache.

Provider configuration, connection creation, health checks, and shutdown wiring remain the host's responsibility. The store interface should be extended only when behavior required by all supported backends cannot be expressed by this contract.

### Cache configuration

Keep the existing `prebid-go-module` configuration fields and JSON names:

```go
type Config struct {
	Enabled                     bool   `json:"enabled"`
	Provider                    string `json:"provider"`
	TTLSeconds                  int    `json:"ttl_seconds"`
	MaxKeys                     int    `json:"max_keys"`
	MaxSize                     int    `json:"max_size"`
	TTLCeilingFirstPartySeconds int    `json:"ttl_ceiling_first_party_seconds"`
	TTLCeilingThirdPartySeconds int    `json:"ttl_ceiling_third_party_seconds"`
	TTLCeilingDeviceSeconds     int    `json:"ttl_ceiling_device_seconds"`
	NegativeTTLSeconds          int    `json:"negative_ttl_seconds"`
	InProgressTTLSeconds        int    `json:"in_progress_ttl_seconds"`
}
```

`Enabled` and `Provider` are used by host wiring. The generic cache uses the TTL and size fields. Provider connection configuration stays in its integration package.

### Clock

```go
package clock

type Clock interface {
	Now() time.Time
}
```

`Clock` is a general core abstraction rather than a cache-owned interface. The cache uses `clock.RealClock` by default and accepts a fake implementation for deterministic TTL tests. Every entry creation, expiry check, L2 promotion, alias backfill, and remaining-TTL calculation must use this clock.

### Metrics

```go
type Metrics interface {
	Request(partnerID string)
	Enriched(partnerID string)
	NotEnriched(partnerID string, reason NotEnrichedReason)
	APIRequestDuration(partnerID string, duration time.Duration)
	APISuccess(partnerID string)
	APIError(partnerID string, kind string, statusCode int)
	CacheLookup(partnerID string, result CacheLookupResult, layer CacheLayer)
}
```

Only events already produced by `prebid-go-module` should be included initially. `enrichment.NoopMetrics()` is used when metrics are not configured, so the workflow requires no external metrics dependency.

`integrations/prometheus` supplies the standard Prometheus implementation. Consumers may provide any other implementation of `enrichment.Metrics`.

The generic cache has a separate metrics interface because it owns L2 calls and their timing. L1 metrics are intentionally excluded:

```go
package cache

type Metrics interface {
	L2Request(operation, result string)
	L2GetLatency(time.Duration)
	L2PutLatency(time.Duration)
}
```

One Prometheus implementation may implement both `enrichment.Metrics` and `cache.Metrics`. Both packages provide no-op implementations.

The generic cache—not the Redis, Valkey, or Aerospike store—measures each `Store.Get` and `Store.Put` call and emits its result and duration through `cache.Metrics`. The Prometheus integration only translates these events into collectors.

Compatibility mappings are fixed:

```text
operations: get, put
results:    hit, miss, stored, error
layers:     l1, l2, none
API status: empty label when no HTTP response was received
```

If the core metrics API represents an absent HTTP status as `0`, the Prometheus integration converts it to an empty label.

### Logger

Logging is dependency-free and shared by the core components:

```go
package logging

type Logger interface {
	Debug(message string)
	Warn(message string)
	Error(message string)
}
```

The first version intentionally accepts only a message. Structured fields may be added later only when a proven use case justifies changing the contract. A no-op logger is used when none is configured. Prebid Server and `iiq-prebid-server` provide adapters for their own logging systems.

### Construction

```go
type Dependencies struct {
	S2S     s2s.API
	Cache   Cache
	Metrics Metrics
	Logger  logging.Logger
}

func New(deps Dependencies, maxCacheKeys int) (Enricher, error)
```

`S2S` is required. Nil metrics and logger dependencies are replaced with no-op implementations; nil `Cache` disables caching. `Config.MaxKeys` is the single configuration source and is passed to the Enricher as `maxCacheKeys`; the generic cache does not consume it. Account-specific execution policy remains on `Request`:

```go
type Request struct {
	PartnerID    string
	Endpoint     string
	Auction      *openrtb2.BidRequest
	Timeout      time.Duration
	CacheEnabled bool
}
```

This preserves per-account endpoint, timeout, and cache overrides.

### Wiring

Official Prebid Server selects the configured backend and passes the generic cache to the enricher:

```go
var store cache.Store
switch cfg.Provider {
case "aerospike":
	store = aerospike.New(cfg.Aerospike)
case "redis":
	store = redis.New(cfg.Redis)
case "valkey":
	store = valkey.New(cfg.Valkey)
}

identityCache, err := cache.New(cache.Dependencies{
	Store:   store,
	Metrics: metrics,
	Logger:  logger,
	Clock:   clock.RealClock{},
}, cacheConfig)
if err != nil {
	return err
}

enricher := enrichment.New(enrichment.Dependencies{
	S2S:     s2sClient,
	Cache:   identityCache,
	Metrics: metrics,
	Logger:  logger,
}, cfg.MaxKeys)
```

The BEPP module imports and wires only the Aerospike integration. A consumer that needs no cache passes nil and imports no cache integration. The host closes the concrete store during shutdown.

When caching is enabled, constructing the generic cache with a nil `Store` returns an error. When caching is disabled, the host does not construct or inject a cache. The library must not silently create an L1-only cache.

Suggested module paths:

```text
github.com/Intent-IQ/identity-go
github.com/Intent-IQ/identity-go/integrations/prometheus
github.com/Intent-IQ/identity-go/integrations/aerospike
github.com/Intent-IQ/identity-go/integrations/redis
github.com/Intent-IQ/identity-go/integrations/valkey
```

## Workflow ownership

`enrichment.Enricher` owns key extraction, cache lookup/state handling, exact URL construction, the bounded S2S call, positive/negative cache writes, result mapping, and metric events. It returns classified S2S errors rather than swallowing them.

The Prebid adapter owns config mapping, hook payload conversion, request mutation, flow-context storage, concrete dependency wiring, the concrete logger adapter, debug-trace rendering, and conversion of an enrichment error into a fail-open hook result.

### Enrichment outcome

`Result` includes an outcome so adapters can distinguish successful enrichment from valid no-EID paths without reconstructing business logic:

```go
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
}
```

A structured diagnostics result may be added later if adapters need cache layer, key type, S2S status, or timing for richer traces. It is intentionally excluded from the initial interface.

## Compatibility requirements

- Preserve existing query parameter order and existing endpoint query parameters.
- Encode spaces as `%20`, not `+`, and omit blank optional values.
- Preserve IFA/LMT and CTV device-type `3`/`7` behavior.
- Preserve site/app referrer precedence and first `intentiq.com` UID selection.
- Preserve structured UA-hint formatting and deterministic brand ordering.
- Preserve top-level privacy fields and JSON `ext` fallbacks.
- Send TCF consent only in the `gdpr-consent` header.
- Treat every non-2xx response as an error and drain/close its body.
- Treat an empty-string `data` value as a successful response with no EIDs.
- Preserve `cttl`, `abTestUuid`, `tc`, HTTP status, and stable error categories.
- Keep `identity-go` independent of Prebid Server hook and utility packages.
- Keep `identity-go` independent of Prometheus and concrete cache clients.
- Retain FreeCache as the initial internal L1 implementation.
- Importing core or the Aerospike integration must not require Redis, Valkey, or Prometheus dependencies.
- Preserve cache hit, miss, negative, in-progress, alias, TTL, and fail-open behavior.
- Preserve existing cache keys and serialized entries across mixed-version deployments.
- Reject a nil Store when constructing an enabled cache; do not create an implicit L1-only cache.
- Preserve the names, labels, and emission points of existing enrichment business metrics; the generic cache emits backend-operation events through `cache.Metrics`.
- L1 counters and gauges are intentionally removed; this is the only approved metrics compatibility exception.
- Preserve exact L2 operation/result tokens and convert an absent API status to an empty Prometheus label.
- Use the injected clock for every cache time calculation.
- Use `Config.MaxKeys` as the single source for enrichment key limits.

## Tests

Port the current tests and retain their expected values:

- Exact URL for fixed parameters and endpoints with an existing query.
- IP, IPv6, UA, and `%20` encoding.
- Mobile and CTV device IDs; blank IFA and LMT suppression.
- Site and app referrer precedence.
- Existing IIQ UID extraction.
- GDPR, TCF, US Privacy, GPP, GPP SID, and `ext` fallbacks.
- High-entropy UA hints, empty hints, and deterministic ordering.
- Successful response parsing, empty `data`, TTL, A/B UUID, and termination cause.
- Consent header present and absent.
- Non-2xx, transport, timeout, body-read, and parse errors.
- Single-line capped error snippets and connection reuse after non-2xx responses.
- Enricher mapping with a fake `s2s.API`, including zero-EID results.
- Cache hit skips S2S and returns cached metadata.
- Cache miss marks in-progress, calls S2S, and stores a positive or negative result.
- Negative and in-progress cache results skip S2S.
- Cache read failure falls back to S2S; cache write failure preserves the successful result.
- S2S timeout uses the request-level timeout and returns a classified timeout error.
- Recording metrics receive the same events and labels as the current implementation.
- Every outcome is returned correctly for enriched, no-ID, cached-negative, in-progress, and no-endpoint paths.
- Nil cache and nil metrics dependencies behave as disabled cache and no-op metrics.
- Run generic cache contract tests against an in-memory fake and every supported store.
- Verify concurrent misses preserve the current best-effort in-progress behavior without requiring atomic storage operations.
- Verify old cache fixtures decode in the new cache and new entries retain the old wire format.
- Verify enabled cache construction rejects a nil Store.
- Verify the FreeCache byte floor, TTL rounding, expiry validation, L2 promotion, and alias-backfill TTL behavior.
- Verify every integration module builds and tests independently.
- Full regression run: `go test ./modules/intentiq/identity/...` after migration.

No compatibility assertion should be weakened or rewritten merely to make the new implementation pass. The only intentional exception is removal of the existing L1 metrics and their tests.
