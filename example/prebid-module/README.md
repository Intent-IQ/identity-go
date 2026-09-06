# Prebid Server module example

This example uses the real Prebid Server hook contracts and delegates reusable identity work to `identity-go`.

- `module.go` constructs and reuses the Enricher and Reporter.
- `processedauctionrequest.go` calls enrichment, applies EIDs through a hook mutation, and remains fail-open.
- `auctioncontext.go` carries request and enrichment metadata between hook stages.
- `auctionresponse.go` leaves the response unchanged and starts one detached report per bid.

The example wires Prometheus metrics and supports Valkey, Redis, and Aerospike through the generic two-layer cache. `config.yaml` shows all provider blocks; only the provider selected by `cache.provider` is validated and connected. When `metrics_port` is positive, the module serves its registry on that port. `Shutdown` closes the metrics listener and selected store.

The YAML file includes the module configuration and both hook stages in Prebid Server's host execution plan. The runnable Valkey flow remains available in `../basic`.

Run its tests with:

```bash
go test ./...
```
