# Enrichment wait-timeout rollout

Use this runbook after deploying the wait-timeout code with `wait_timeout`
omitted. Omission is synchronous mode and is the rollback setting.

## Measure the candidate wait

`cmd/enrichmentanalysis` consumes chronological JSONL observations and reports
regional p50/p70/p90/p95/p99 S2S latency plus identity recurrence inside positive
and negative cache TTLs:

```json
{"timestamp":"2026-09-19T10:00:00Z","region":"us-east-1","identity":"sha256:...","latency_ms":83.2,"cache_ttl_ms":43200000,"cache_kind":"positive"}
```

`identity` must be a non-reversible hash or another approved pseudonymous key;
never put raw device IDs, IP addresses, cookies, or EIDs in the analysis file.
`latency_ms` may be omitted for recurrence-only records. `cache_ttl_ms` and
`cache_kind` may be omitted for latency-only records. Input must be ordered by
timestamp within each region and identity.

Run:

```bash
go run ./cmd/enrichmentanalysis -input observations.jsonl > report.json
```

Choose `wait_timeout` independently per region. Start with a measured percentile
that meets the auction latency budget; p70 is only an example, not a default. Do
not enable async mode when recurrence within TTL is too low to produce useful
cache hits.

## Size and load-test the limiter

Estimate the steady-state calls that can outlive the wait:

```text
background concurrency ~= peak QPS * miss rate * max(0, timeout - wait_timeout)
```

Timeouts are expressed in seconds in this equation. Add headroom for burstiness
and a slow-tail incident, then keep the result within the HTTP transport's
connection limits and the S2S service's approved concurrency. A positive limit is
required whenever `wait_timeout` is less than `timeout`; zero is valid only for
synchronous mode.

Before production, replay representative peak traffic against a non-production
S2S endpoint and record:

- peak and sustained S2S connection/concurrency measurements from the load-test
  harness or HTTP transport;
- `background_limit` rejection rate;
- S2S connection count and API timeout rate;
- cache miss, in-progress, positive-hit, and negative-hit rates;
- auction latency with the proposed wait compared with synchronous mode.

Pass when observed peak concurrency remains below the configured limit with the
agreed headroom, no connection pool is exhausted, and sustained capacity rejection
is zero under expected peak load.

## Dashboard panels

Use `$partner_id` and the deployment's region label/filter where available.

```promql
# Total S2S API latency p95.
histogram_quantile(
  0.95,
  sum by (le) (
    rate(iiq_identity_api_latency_seconds_bucket{partner_id=~"$partner_id"}[5m])
  )
)

# Capacity rejection and wait-expiry rates.
sum by (reason) (
  rate(iiq_identity_not_enriched_total{
    partner_id=~"$partner_id",
    reason=~"wait_expired|background_limit"
  }[5m])
)

# S2S timeouts.
sum(rate(iiq_identity_api_error_total{
  partner_id=~"$partner_id",
  reason="timeout"
}[5m]))

# Cache lookup results and layers.
sum by (result, layer) (
  rate(iiq_identity_cache_lookup_total{partner_id=~"$partner_id"}[5m])
)
```

Recommended alerts:

- **Background rejection:** `background_limit` rate is non-zero for 5 minutes.
- **S2S timeout regression:** timeout ratio exceeds the region's synchronous
  baseline and error budget for 10 minutes.
- **Cache warming ineffective:** `wait_expired` remains non-zero while positive
  cache-hit rate does not improve over the agreed evaluation window.

Track limiter utilization during load tests through the harness or HTTP transport.
The library deliberately exposes only saturation (`background_limit`), not an
additional in-flight gauge.

## Staged rollout and rollback

1. Deploy with `wait_timeout` omitted and confirm all new metric series exist.
2. Select one low-risk region and configure a measured hybrid wait plus a finite
   `max_background_s2s_calls`.
3. Hold through at least one representative peak window. Compare enrichment rate,
   auction latency, late outcomes, capacity rejection, API errors, and cache hits
   with the synchronous baseline.
4. Expand one region at a time only while the load-test and alert gates remain
   green. Recalculate the wait for each region rather than copying one value.
5. Consider `wait_timeout: 0` only when measured recurrence demonstrates that
   cache warming preserves acceptable enrichment on subsequent requests.

Rollback by omitting `wait_timeout` or setting it greater than or equal to
`timeout`, then applying the host's normal restart/reload process. This restores
synchronous behavior without changing cache contents. Keep the new metrics active
to verify that `mode="sync"` is the only auction-wait mode after rollback.
