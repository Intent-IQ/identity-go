# Enrichment benchmark

This package provides k6 benchmarks for the identity-go enrichment flow and
exports Prometheus metrics.

## Start the server

From the repository root:

```bash
cd cmd/benchmark
go run . \
  -listen :8081 \
  -endpoint https://s2s.example.test/profiles \
  -partner example-partner \
  -timeout 400ms \
  -cache /tmp/identity-benchmark-cache \
  -max-concurrent-calls 2000
```

The cache is optional for sync mode and required for async and hybrid modes.
Use a new cache directory for a cold-cache test. Reuse the directory for a
warm-cache test.

## Endpoints

### `POST /enrich`

The request body must contain one OpenRTB bid request.

Supported modes:

```text
/enrich?mode=sync
/enrich?mode=async
/enrich?mode=hybrid&wait_ms=250
```

The response includes the enrichment outcome, returned EIDs, request duration,
and whether the result came from cache.

### `GET /health`

Returns `200 OK` when the server is running.

### `GET /metrics`

Returns the `iiq_identity_*` Prometheus metrics used by the production
integration.

## Run the benchmark

The root Makefile runs the benchmark server and k6 cases:

```bash
make benchmark \
  ENDPOINT=https://s2s.example.test/profiles \
  PARTNER=example-partner
```

The default workload uses 1,000 iterations, 16 virtual users, and all six test
cases. Override settings as needed:

```bash
make benchmark \
  ENDPOINT=https://s2s.example.test/profiles \
  PARTNER=example-partner \
  ITERATIONS=100 \
  VUS=10 \
  CASES="1 4"
```

Common variables:

| Variable | Default | Description |
|---|---:|---|
| `ITERATIONS` | `1000` | Total requests per case |
| `VUS` | `16` | k6 virtual users |
| `CASES` | `1 2 3 4 5 6` | Cases to run |
| `TIMEOUT` | `400ms` | S2S call timeout |
| `HOOK_TIMEOUT_MS` | `300` | Simulated hook timeout |
| `MAX_CONCURRENT_CALLS` | `2000` | Async/hybrid S2S concurrency limit |
| `FIXTURE` | `testdata/sample.jsonl` | JSONL fixture path, relative to `cmd/benchmark` |
| `OUT` | `./results` | Result directory, relative to `cmd/benchmark` |

The suite runs these cases:

| Case | Mode | Wait | Hook timeout |
|---|---|---:|---:|
| `1` | sync | full call | none |
| `2` | sync | full call | `HOOK_TIMEOUT_MS` |
| `3` | async | `0` | `HOOK_TIMEOUT_MS` |
| `4` | hybrid | `250ms` | `HOOK_TIMEOUT_MS` |
| `5` | hybrid | `100ms` | `HOOK_TIMEOUT_MS` |
| `6` | hybrid | `50ms` | `HOOK_TIMEOUT_MS` |

Results include the k6 summary, Prometheus scrape, server log, and cache for
each case. A new server and cache are used for every case.

## Run k6 directly

Start the server, then run a test from `cmd/benchmark/k6`:

```bash
MODE=sync ITERATIONS=1000 VUS=16 \
  FIXTURE=../testdata/sample.jsonl \
  k6 run enrichment.js

MODE=hybrid WAIT_MS=280 ITERATIONS=1000 VUS=16 \
  FIXTURE=../testdata/sample.jsonl \
  k6 run enrichment.js

MODE=async ITERATIONS=1000 VUS=16 \
  FIXTURE=../testdata/sample.jsonl \
  k6 run enrichment.js
```

The fixture is read once and shared across virtual users. If there are fewer
fixture entries than iterations, the entries are reused.

`HOOK_TIMEOUT_MS` simulates the Prebid Server hook timeout. `TMAX_DEFAULT_MS`
provides an auction timeout for fixture entries that do not contain `tmax`.

## Sensitive data

Fixtures, caches, logs, and results may contain IP addresses, user agents,
advertising IDs, consent data, and resolved EIDs. Do not commit production data.
