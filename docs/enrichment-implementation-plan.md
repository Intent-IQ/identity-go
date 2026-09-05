# Enrichment implementation plan

Implement the blocks in order. Each block must compile, pass its tests, and be reviewed before the next block starts. `docs/design.md` is authoritative for architecture and public contracts; `docs/enirchment-implementation.md` is authoritative for compatibility details.

## Block 1: shared contracts

Scope:

- [x] Finalize `logging.Logger` and its no-op implementation.
- [x] Finalize `clock.Clock` and `clock.RealClock`.
- [x] Finalize shared `iiqapi.Error`, error kinds, unwrapping, and label conversion.
- [x] Finalize `enrichment.Enricher`, `Request`, `Result`, and `Outcome`.
- [x] Finalize `enrichment.Cache`, `enrichment.Metrics`, and their models.
- [x] Finalize `cache.Store`, `cache.Metrics`, `cache.Config`, and dependency types.
- [x] Add compile-time interface assertions for no-op implementations.

Tests and completion gate:

- [x] Test unknown, wrapped, timeout, and HTTP-status error classification.
- [x] Test all no-op implementations.
- [x] Test cache configuration JSON field names.
- [x] `go test ./...` passes with no provider or Prometheus dependency in the core module.
- [x] No runtime workflow or placeholder implementation is introduced.

## Block 2: S2S HTTP client

Depends on Block 1.

Scope:

- [x] Implement `s2s.API.Resolve(ctx, requestURL, consent)`.
- [x] Send a GET to the supplied URL without rebuilding or reordering it.
- [x] Add `gdpr-consent` only when consent is nonempty.
- [x] Parse `data`, `cttl`, `abTestUuid`, `tc`, and HTTP status.
- [x] Preserve request, transport, timeout, status, body-read, and parse errors.
- [x] Capture a single-line error snippet from at most 1024 response bytes.
- [x] Drain and close bodies so connections remain reusable.

Tests and completion gate:

- [x] Port all `prebid-go-module/enrichment/client_test.go` cases without weaker assertions.
- [x] Cover successful EIDs, empty-string `data`, absent TTL, and invalid top-level JSON.
- [x] Cover consent presence/absence, non-2xx, timeout, unreachable server, and body-read failure.
- [x] Verify error snippet normalization/capping and connection reuse.
- [x] S2S package tests pass and the client has no Prebid Server dependency.

## Block 3: S2S parameter builder

Depends on Block 1. Uses no network or cache.

Scope:

- [x] Implement deterministic URL construction in `enrichment/params.go`.
- [x] Preserve fixed order: `at`, `mi`, `dpi`, `pt`, `dpn`, `srvrReq`, `source`.
- [x] Preserve existing endpoint query parameters.
- [x] Encode spaces as `%20` and omit whitespace-only values.
- [x] Map IP, IPv6, raw UA, site/app reference, and existing `intentiq.com` UID.
- [x] Map IFA to `pcid`/`idtype`, suppress for LMT, and uppercase CTV types `3` and `7`.
- [x] Map GDPR, TCF consent, US Privacy, GPP, and GPP SID, including JSON `ext` fallbacks.

Tests and completion gate:

- [x] Port the applicable exact URL and consent tests from `params_test.go`.
- [x] Assert complete URLs where ordering matters, not only individual query values.
- [x] Cover nil request, nil device/user/regs, malformed extensions, and blank values.
- [x] Parameter tests pass without importing Prebid Server wrappers or utilities.

## Block 4: structured UA hints

Depends on Block 3.

Scope:

- [ ] Implement the high-entropy `device.sua` mapping used by `uh`.
- [ ] Preserve numeric keys `0` through `8` and current quoting.
- [ ] Preserve major and full browser versions.
- [ ] Sort brands deterministically.
- [ ] Omit empty and low-entropy hints.

Tests and completion gate:

- [ ] Port every existing UA-hint test unchanged in behavior.
- [ ] Cover browser, platform, mobile, architecture, bitness, model, nil, and empty cases.
- [ ] Re-run all Block 3 exact URL tests.

## Block 5: cache models, TTL, and entry codec

Depends on Block 1. Uses a fake clock and no Store.

Scope:

- [ ] Implement cache states, layers, key types, and stable label tokens.
- [ ] Implement the existing default and ceiling TTL policy.
- [ ] Implement positive, negative, and in-progress entry encoding/decoding.
- [ ] Use absolute expiry in Unix milliseconds and the injected clock.
- [ ] Preserve JSON names, casing, and `omitempty` behavior.

Tests and completion gate:

- [ ] Port key, result, config, TTL, and entry tests.
- [ ] Add fixed old-format fixtures for every entry type.
- [ ] Verify old entries decode and new entries retain the old canonical format.
- [ ] Test exact-expiry boundaries with a fake clock and no sleeps.

## Block 6: cache-key extraction

Depends on Blocks 4 and 5.

Scope:

- [ ] Port ordered candidate-key extraction.
- [ ] Preserve IIQ, shared/pubcid, MAID, other EID, and device-composite namespaces.
- [ ] Preserve source casing rules, CTV IFA normalization, LMT behavior, and normalized UA.
- [ ] Preserve first-occurrence deduplication and `Config.MaxKeys` capping.

Tests and completion gate:

- [ ] Port all key-extractor and user-agent normalization tests.
- [ ] Verify exact key order, value, and type.
- [ ] Verify `Config.MaxKeys` is the only key-limit source.

## Block 7: FreeCache L1

Depends on Block 5.

Scope:

- [ ] Wrap FreeCache as the internal L1 implementation.
- [ ] Preserve the 512 KiB minimum byte capacity.
- [ ] Preserve concurrency safety and byte-bounded eviction.
- [ ] Round TTL upward to whole seconds with a one-second floor.
- [ ] Validate absolute expiry using `clock.Clock` after every read.
- [ ] Do not expose or emit L1 metrics.

Tests and completion gate:

- [ ] Test get/set, miss, expiry, malformed entry, and concurrent access.
- [ ] Test capacity floor and TTL rounding.
- [ ] Confirm removal of L1 counters and gauges is the only compatibility exception.

## Block 8: generic two-layer cache

Depends on Blocks 5–7.

Scope:

- [ ] Implement `enrichment.Cache` over FreeCache L1 and `cache.Store` L2.
- [ ] Preserve ordered lookup and resolved-over-in-progress precedence.
- [ ] Preserve L2 promotion and alias backfill using remaining TTL and destination ceilings.
- [ ] Preserve positive, negative, and best-effort in-progress writes.
- [ ] Keep `Get` then `PutInProgress` non-atomic.
- [ ] Treat L2 read errors as misses and retain L1 success after L2 write errors.
- [ ] Measure L2 get/put result and latency in the generic cache.
- [ ] Use `clock.Clock` for every cache time calculation.
- [ ] Reject enabled-cache construction with nil Store; never create implicit L1-only cache.

Tests and completion gate:

- [ ] Port all generic identity-cache tests except approved L1 metric assertions.
- [ ] Test L1/L2 hit, miss, promotion, alias backfill, negative, in-progress, and expiry paths.
- [ ] Test TTL ceilings and backend-provided `cttl` behavior.
- [ ] Test L2 read/write errors, logger messages, and exact L2 metric events.
- [ ] Test concurrent misses retain best-effort behavior without atomic guarantees.
- [ ] Run a reusable Store contract against an in-memory fake.

## Block 9: Enricher without cache

Depends on Blocks 2–4 and 6.

Scope:

- [ ] Implement `enrichment.New` validation and nil metrics/logger defaults.
- [ ] Implement no-endpoint and nil-auction behavior.
- [ ] Build the exact URL and consent, then call the injected S2S API.
- [ ] Apply `Request.Timeout` only around the S2S call.
- [ ] Map EIDs, TTL, A/B UUID, termination cause, and outcomes.
- [ ] Return classified S2S errors for host-level fail-open handling.
- [ ] Emit request, API duration, API success/error, enriched, and not-enriched events at existing points.

Tests and completion gate:

- [ ] Test with a recording fake S2S API, metrics implementation, and logger.
- [ ] Cover enriched, no IDs, no endpoint, nil auction, timeout, and every S2S error category.
- [ ] Verify exact metric ordering/labels where observable.
- [ ] Verify EID order and all reporting metadata.

## Block 10: Enricher cache orchestration

Depends on Blocks 8 and 9.

Scope:

- [ ] Bypass cache when disabled or no candidate keys exist.
- [ ] Return cached EIDs and metadata on a hit without S2S.
- [ ] Return `OutcomeCachedNoIDs` for a negative hit.
- [ ] Return `OutcomeInProgress` for an in-progress hit.
- [ ] On miss, write in-progress markers, call S2S, then store positive or negative results.
- [ ] Preserve cache lookup metric result/layer semantics.
- [ ] Ignore cache write errors after a successful S2S result.
- [ ] Fall through to S2S on cache read errors.

Tests and completion gate:

- [ ] Port the cache-related processed-auction-request scenarios as Enricher unit tests.
- [ ] Assert whether S2S and each cache method were called for every state.
- [ ] Assert A/B UUID and termination cause survive positive and negative cache paths.
- [ ] Verify all outcomes and not-enriched reasons.
- [ ] Re-run Blocks 2–9 tests.

## Block 11: Prometheus integration

Depends on Blocks 8–10. Resolve ordinary-package versus separate-module packaging before starting this block.

Scope:

- [ ] Implement both `enrichment.Metrics` and `cache.Metrics`.
- [ ] Preserve existing metric names and the `iiq_identity_` prefix.
- [ ] Preserve labels and emission values for enrichment, API, cache lookup, and L2 operations.
- [ ] Convert absent API status `0` to an empty label.
- [ ] Preserve L2 operations `get`/`put` and results `hit`/`miss`/`stored`/`error`.
- [ ] Exclude L1 counters and gauges intentionally.
- [ ] Keep metrics server lifecycle outside the core implementation.

Tests and completion gate:

- [ ] Port Prometheus tests except explicit L1 metric cases.
- [ ] Assert collector names, label values, histogram observations, and no duplicate registration.
- [ ] Verify core packages remain free of Prometheus dependencies.

## Block 12: Store integrations

Depends on Block 8. Implement each provider as an independently reviewable sub-block.

### Block 12A: Aerospike

- [ ] Implement `cache.Store` with existing key, value, TTL, miss, and error behavior.
- [ ] Port Aerospike configuration and tests.
- [ ] Run the generic Store contract.
- [ ] Verify BEPP can wire Aerospike without Redis or Valkey imports.

### Block 12B: Redis

- [ ] Implement `cache.Store` with existing key, value, TTL, miss, and error behavior.
- [ ] Port Redis configuration and tests.
- [ ] Run the generic Store contract.

### Block 12C: Valkey

- [ ] Implement `cache.Store` with existing key, value, TTL, miss, and error behavior.
- [ ] Port Valkey configuration and tests.
- [ ] Run the generic Store contract.

Completion gate:

- [ ] Provider configuration and connection lifecycle remain in host wiring.
- [ ] Official Prebid Server can select all three providers.
- [ ] Core packages remain free of provider dependencies.

## Block 13: `prebid-go-module` migration

Depends on Blocks 1–12.

Scope:

- [ ] Replace local S2S, parameter, key extraction, cache orchestration, and enrichment-related metrics implementations with `identity-go` components.
- [ ] Add thin adapters for hook payloads, mutations, flow context, Glog, configuration, and shutdown.
- [ ] Preserve final fail-open behavior and append resolved EIDs after existing EIDs.
- [ ] Preserve reporting metadata passed through module context.
- [ ] Do not weaken existing behavioral assertions.

Tests and completion gate:

- [ ] Run `go test ./modules/intentiq/identity/...`.
- [ ] Verify exact S2S URLs and consent headers.
- [ ] Verify auction mutations, cache states, TTLs, metadata, metrics, tracing, and error behavior.
- [ ] Record L1 metrics and their tests as the sole intentional compatibility removal.

## Block 14: second-consumer validation

Depends on Block 13.

- [ ] Wire Enricher into `iiq-prebid-server`/BEPP with Aerospike only.
- [ ] Prototype the official Prebid Server adapter.
- [ ] Verify neither adapter duplicates URL, key extraction, cache, or S2S business logic.
- [ ] Run both adapters against shared OpenRTB, S2S response, and cache fixtures.
- [ ] Confirm public contracts are sufficient before declaring the implementation complete.

## Final completion gate

- [ ] Every block is checked and reviewed.
- [ ] Core and selected integration tests pass.
- [ ] `prebid-go-module` passes all tests except the explicitly removed L1 metric tests.
- [ ] Cache data remains compatible during a mixed-version deployment.
- [ ] No unapproved external dependency exists in the core module.
- [ ] `docs/design.md` and `docs/enirchment-implementation.md` still match the implemented contracts.
