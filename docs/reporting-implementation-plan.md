# Reporting implementation plan

Implement the blocks in order. Each block must compile, pass its tests, and be reviewed before the next block starts. `docs/design.md` is authoritative for architecture and public contracts; `docs/reporting-implementation.md` is authoritative for compatibility behavior.

## Block 1: reporting contracts

Scope:

- [x] Finalize `iiqapi/reporting.API` with `ReportImpression(context.Context, requestURL string) error`.
- [x] Finalize `reporting.Reporter`, `Request`, and `Dependencies`; retain the documented constructor contract for Block 4.
- [x] Add `Request.Timeout` and retain all bid and enrichment-metadata fields.
- [x] Define reporting `Metrics` and its no-op implementation.

Tests and completion gate:

- [x] Add compile-time assertions for API and no-op implementations. Add the concrete `Reporter` assertion in Block 4.
- [x] Test that request and dependency contracts retain all values and that no-op metrics are callable.
- [x] Core packages remain independent of Prebid Server and Prometheus.

## Block 2: ordered report builder

Depends on Block 1. Uses no HTTP or goroutines.

Scope:

- [ ] Build one report URL with exact parameter order: `at`, `rtype`, `source`, `dpi`, `rdata`.
- [ ] Preserve existing endpoint queries and the current `?`/`&` separator behavior.
- [ ] Encode values with `url.QueryEscape` and replace `+` with `%20`.
- [ ] Implement insertion-ordered `rdata` JSON.
- [ ] Map bid, currency, original bid fields, and enrichment metadata exactly.
- [ ] Default only an exactly empty currency to `USD`; retain whitespace currency and keep `biddingPlatformId` fixed at `"4"`.
- [ ] Preserve the existing empty-`rdata` fallback if ordered JSON marshaling fails.

Tests and completion gate:

- [ ] Port ordered-map and report URL cases from `auctionresponse_test.go`.
- [ ] Assert complete raw URLs and exact `rdata` JSON, not only parsed values.
- [ ] Cover existing endpoint queries and reserved characters.
- [ ] Cover valid, missing, blank, non-numeric, and malformed original-bid extensions.
- [ ] Cover every optional metadata field, whitespace behavior, omission rule, and a non-nil zero termination cause.
- [ ] Cover the ordered-JSON marshal-error fallback without returning a new error.

## Block 3: reporting HTTP API

Depends on Block 1.

Scope:

- [ ] Send a GET to the supplied URL without parsing or rebuilding it.
- [ ] Drain and close every received response body.
- [ ] Treat every received HTTP status, including non-2xx, as success.
- [ ] Ignore body-drain and close errors to preserve existing behavior.
- [ ] Return classified request, transport, and timeout errors.
- [ ] Keep HTTP client lifecycle outside the API client.

Tests and completion gate:

- [ ] Verify the exact request URL and GET method.
- [ ] Cover 2xx, redirect-disabled 3xx, 4xx, and 5xx responses as success.
- [ ] Cover invalid URL, timeout, and transport errors.
- [ ] Verify bodies are drained and closed and connections can be reused.
- [ ] Verify body-drain and close failures are ignored after a response is received.
- [ ] Verify concurrent calls are safe.

## Block 4: synchronous Reporter

Depends on Blocks 1–3.

Scope:

- [ ] Implement the documented `New(Dependencies) (Reporter, error)` constructor.
- [ ] Require a non-nil API dependency and default nil metrics/logger to no-op implementations.
- [ ] Implement a successful no-op for an exactly empty endpoint.
- [ ] Build the URL before starting the per-report timeout.
- [ ] Apply `Request.Timeout` only around the API call.
- [ ] Preserve immediate expiry for zero and negative timeouts.
- [ ] Call the injected API synchronously for exactly one bid.
- [ ] Emit `ImpressionReported` after success.
- [ ] Emit `ImpressionError`, log once, and return the original API error after failure.
- [ ] Keep bid iteration, goroutines, panic recovery, and final fail-open policy out of Reporter.

Tests and completion gate:

- [ ] Test constructor validation and no-op dependency defaults.
- [ ] Add a compile-time assertion for the concrete `Reporter` implementation.
- [ ] Test with recording API, metrics, and logger implementations.
- [ ] Cover empty endpoint, success, request error, transport error, positive timeout, and immediate zero/negative timeout.
- [ ] Verify exact URL forwarding and metric/log emission order.
- [ ] Verify the caller's context is not canceled.
- [ ] Verify Reporter is safe for concurrent calls.

## Block 5: Prometheus reporting metrics

Depends on Blocks 1 and 4.

Scope:

- [ ] Extend the optional Prometheus integration to implement `reporting.Metrics`.
- [ ] Add `iiq_identity_impression_reported_total{partner_id}`.
- [ ] Add `iiq_identity_impression_error_total{partner_id}`.
- [ ] Preserve idempotent collector registration.
- [ ] Keep metrics server lifecycle outside the integration.

Tests and completion gate:

- [ ] Assert exact collector names, labels, and counter values.
- [ ] Verify repeated construction does not duplicate collectors.
- [ ] Verify the core module gains no Prometheus dependency.

## Block 6: example reporting flow

Depends on Blocks 1–5.

Scope:

- [ ] Extend `example/` with reporting endpoint configuration and Reporter wiring.
- [ ] Convert an example bid and enrichment result into a reporting request.
- [ ] Demonstrate host-owned fire-and-forget execution with a fresh background context and panic recovery; Reporter owns the configured timeout.
- [ ] Keep the bid response unchanged.
- [ ] Extend the Compose S2S mock to accept impression reports.

Tests and completion gate:

- [ ] The example builds and vets independently.
- [ ] A local run resolves identity, uses Valkey on the second enrichment, and submits an impression report.
- [ ] Docker Compose configuration remains valid.

## Final compatibility review

- [ ] Compare every report field and its order with `prebid-go-module/auctionresponse.go`.
- [ ] Compare empty endpoint, currency default, HTTP status, timeout, metric, and error behavior.
- [ ] Confirm asynchronous execution remains a host concern.
- [ ] Confirm reporting never mutates or rejects an auction response.
- [ ] Run all core and optional integration tests.
- [ ] Run the unchanged `prebid-go-module` tests as the behavioral baseline.
- [ ] Ensure `docs/design.md` and `docs/reporting-implementation.md` still match the implemented contracts.
