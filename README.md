# identity-go

`identity-go` provides host-independent IntentIQ identity enrichment and impression reporting for Go applications, including Prebid Server modules.

The library contains the reusable business flow and HTTP clients. Host applications remain responsible for configuration mapping, OpenRTB mutations, asynchronous reporting, final fail-open policy, and dependency lifecycle.

## Packages

- `enrichment` resolves EIDs through the IntentIQ S2S API, optionally using the two-layer cache.
- `reporting` builds and submits one impression report synchronously. The host may run it asynchronously.
- `cache` provides the generic FreeCache L1 plus a shared L2 `Store` abstraction.
- `iiqapi/s2s` and `iiqapi/reporting` provide the low-level HTTP clients.
- `logging` and `clock` provide host-independent dependency interfaces.
- `integrations/prometheus` provides enrichment, cache, and reporting metrics.
- `integrations/aerospike`, `integrations/redis`, and `integrations/valkey` provide optional L2 stores.

The integrations are separate Go modules, so applications only import the external dependencies they use.

## Quick start

Install the core module:

```bash
go get github.com/Intent-IQ/identity-go
```

Construct the S2S client and Enricher once during application startup:

```go
s2sClient := s2s.NewClient(http.DefaultClient)

enricher, err := enrichment.New(enrichment.Dependencies{
	S2S: s2sClient,
}, 0)
if err != nil {
	return err
}

result, err := enricher.Enrich(ctx, enrichment.Request{
	PartnerID: "your-partner-id",
	Endpoint:  "https://your-s2s-endpoint",
	Auction:   bidRequest,
	Timeout:   time.Second,
})
```

On success, the host applies `result.EIDs` to the auction. On failure, a Prebid Server integration normally leaves the auction unchanged.

Reporting is a separate flow:

```go
reporter, err := reporting.New(reporting.Dependencies{
	API: reportingapi.NewClient(http.DefaultClient),
})
if err != nil {
	return err
}

go func() {
	_ = reporter.Report(context.Background(), reporting.Request{
		PartnerID: "your-partner-id",
		Endpoint:  "https://your-reporting-endpoint",
		Timeout:   time.Second,
		Bid:       bid,
		Currency:  currency,
	})
}()
```

The host owns bid iteration, goroutine creation, background context selection, and panic recovery. See the examples for complete wiring.

## Examples

- [`example/basic`](example/basic/) is a runnable end-to-end flow using Valkey, a local S2S mock, enrichment, and impression reporting.
- [`example/prebid-module`](example/prebid-module/) demonstrates real Prebid Server hook contracts, Prometheus metrics, all supported cache providers, fail-open enrichment, auction-context transfer, and asynchronous reporting.

Run the basic example:

```bash
cd example/basic
docker compose up -d
go run . -config config.yaml
docker compose down
```

Run the Prebid module example tests:

```bash
cd example/prebid-module
go test ./...
```

## Design documents

Architecture and compatibility requirements are documented in [`docs`](docs/), starting with [`docs/design.md`](docs/design.md).

## Development

Run the core checks from the repository root:

```bash
go test ./...
go vet ./...
```

Each directory under `integrations/` and `example/` with its own `go.mod` is tested independently.
