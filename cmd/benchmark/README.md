# benchmark

Replays OpenRTB bid requests from a JSONL fixture through the `enrichment` flow and
the S2S client, writing one JSON file per request.

```bash
go run ./cmd/benchmark \
  -fixture /secure/benchmark-data/requests.jsonl \
  -out /secure/benchmark-data/results/run-001 \
  -endpoint https://be-api-s2s.intentiq.com/profiles_engine/ProfilesEngineServlet \
  -partner <partner-id> \
  -limit 1000 -concurrency 16
```

Every fixture line is one bid request. Each replay parses it into an
`openrtb2.BidRequest`, hands it to `enrichment.Enricher`, and records the URL the
enrichment flow built, the raw S2S response, and the resolved EIDs.

`-limit` defaults to 1000; `0` replays the whole fixture, which is over a million
live requests — raise it deliberately. `-skip` resumes at a fixture offset.

## Fixture shape

The fixture holds requests as an exchange receives them, so EIDs sit under
`user.ext.eids`. Prebid Server moves them to `user.eids` before modules run, and
the replay performs the same migration; without it `resolveIIQUID` never finds the
IntentIQ EID and the `iiquid` parameter is never sent.

Only `device`, `user`, `regs`, `site`/`app` feed the S2S request. `imp`, `bcat` and
the rest of the auction are ignored by enrichment.

## Cache

Without `-cache` the cache is disabled and every line reaches the S2S API.

`-cache <dir>` enables both layers: the in-process FreeCache L1, plus an L2 kept in
that directory. The L2 is a plain file store (`filestore.go`), so entries survive
between runs and a re-run over the same fixture can hit them — which an in-process
L1 alone cannot do.

```bash
go run ./cmd/benchmark ... -cache /secure/benchmark-data/cache -out /secure/benchmark-data/results/cold
go run ./cmd/benchmark ... -cache /secure/benchmark-data/cache -out /secure/benchmark-data/results/warm
```

`-cache-ttl` sets the TTL used when the API returns none (default 1h);
`-cache-max-keys` caps aliases per auction (default 10).

A record is marked `served_from_cache` when the S2S client was never reached.
Prefer it over `cache_lookups` for measuring API avoidance: a negative-sentinel hit
is reported by the library as `miss` even though it skipped the network.

Cache contents are as sensitive as the responses — keep the directory outside the
repository.

## Varying the request

`-param k=v` adds or replaces a query parameter, `-drop k` removes one, and
`-require k` replays only requests whose built URL carries that parameter. All
three are repeatable and accept comma-separated lists. Useful for isolating which
parameter a server-side rejection depends on:

```bash
go run ./cmd/benchmark ... -drop srvrReq          # which param causes the rejection?
go run ./cmd/benchmark ... -require iiquid,pcid   # only well-identified requests
```

## Output

`-out` holds `summary.json` plus `part-NNNNN/` shards of `-shard-size` files, each
named by fixture index:

```json
{
  "index": 0,
  "auction_id": "<auction id>",
  "reference": "<site domain or app bundle>",
  "s2s_request_url": "https://...?at=39&mi=10&dpi=...&ip=...&uas=...&ref=...&gdpr=0",
  "duration_ms": 7.9,
  "outcome": "enriched",
  "served_from_cache": false,
  "s2s_response": { "status": 200, "data": { "eids": [] }, "cttl": 3600000 },
  "enrichment_result": { "eids": [], "cache_ttl_ms": 3600000 }
}
```

Failures write an `error` object with the `iiqapi` error kind and HTTP status
instead of a response. `summary.json` aggregates outcomes, error kinds, cache
lookups and `filtered_out` counts.

## Output is personal data

Stored request URLs carry `ip`, `uas`, `pcid` and `iiquid`; consent headers may
contain consent data; and results carry resolved cross-partner identity graphs.
Real fixture data, output, and caches belong outside the repository and must not
be committed. The command requires explicit `-fixture` and `-out` paths so their
location is always deliberate. Output and cache files are created with owner-only
permissions, but they still require the same retention and handling controls as
the source fixture.

The `testdata` directory contains only small, synthetic fixtures suitable for
tests and examples. It must never contain captured traffic or API responses.
