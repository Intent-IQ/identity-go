# identity-go design

## Goal

`identity-go` provides host-independent IntentIQ identity enrichment and impression reporting for `iiq-prebid-server` and official Go Prebid Server.

The core may depend on the standard library, `github.com/prebid/openrtb/v20`, and FreeCache. It must not depend on Prebid Server hooks, Prometheus, Redis, Valkey, Aerospike, Glog, or Zerolog.

## Public contracts

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

type Result struct {
	EIDs             []openrtb2.EID
	CacheTTL         time.Duration
	ABTestUUID       string
	TerminationCause *int64
	Outcome          Outcome
}
```

`Outcome` distinguishes `enriched`, `no_ids`, `no_ids_cached`, `in_progress`, and `no_endpoint`. Structured diagnostics may be added later but are not part of the initial contract.

```go
package reporting

type Reporter interface {
	Report(context.Context, Request) error
}
```

## Dependency ports

`enrichment` defines the `Cache` and enrichment `Metrics` interfaces it consumes. `cache` defines the smaller `Store` and cache `Metrics` interfaces used by the generic cache. `clock` defines the shared time-source abstraction, and `logging` defines a message-only `Logger`. Nil metrics and logger dependencies use no-op implementations.

```go
package logging

type Logger interface {
	Debug(message string)
	Warn(message string)
	Error(message string)
}
```

The generic cache uses a required `Store` when enabled. A nil cache disables caching; the library never creates an implicit L1-only cache.

## Ownership

- `enrichment` owns OpenRTB extraction, exact S2S URL construction, key extraction, cache state handling, S2S timeout, result mapping, and enrichment metrics.
- `iiqapi/s2s` owns HTTP execution, the consent header, response parsing, status validation, and typed API errors.
- `cache` owns FreeCache-backed L1 behavior, L2 access, alias backfill, serialization, TTL policy, and L2 metrics.
- Host adapters own configuration mapping, concrete integrations, hook mutations, flow context, final fail-open conversion, debug-trace rendering, and resource shutdown.
- Core components emit messages through `logging.Logger`; hosts provide Glog, Zerolog, or other adapters.

## Construction

```go
package cache

type Dependencies struct {
	Store   Store
	Metrics Metrics
	Logger  logging.Logger
	Clock   clock.Clock
}

func New(Dependencies, Config) (enrichment.Cache, error)
```

```go
package enrichment

type Dependencies struct {
	S2S     s2s.API
	Cache   Cache
	Metrics Metrics
	Logger  logging.Logger
}

func New(Dependencies, maxCacheKeys int) (Enricher, error)
```

Nil S2S and an enabled cache with nil Store are errors. Nil cache disables caching. Nil metrics and logger use no-op implementations; a nil clock uses the real clock. `cache.Config.MaxKeys` is the single configuration source for the Enricher key limit.

## Folder structure

```text
identity-go/
  enrichment/         # business contract and resolution workflow
  reporting/          # business reporting contract and workflow
  cache/              # generic identity cache and Store contract
  clock/              # shared time-source contract
  iiqapi/
    s2s/              # identity-resolution HTTP API
    reporting/        # impression-reporting HTTP API
  logging/            # logger contract and no-op implementation
  integrations/
    prometheus/
    aerospike/
    redis/
    valkey/
```

Each integration is an optional module with its own dependencies. BEPP imports only Aerospike; official Prebid Server may select Aerospike, Redis, or Valkey. Consumers may provide their own cache, metrics, or logger implementations.

Cache keys and entries remain compatible with `prebid-go-module`. L1 counters and gauges are intentionally not migrated; all other enrichment behavior and L2/enrichment metrics are compatibility requirements.
