# benchmark

Replays OpenRTB requests from a JSONL file through the identity enrichment flow.
It writes one result file per request and a summary for the full run.

## Run

```bash
go run ./cmd/benchmark \
  -fixture /path/to/requests.jsonl \
  -out /path/to/results \
  -endpoint https://example.com/profiles \
  -partner <partner-id> \
  -limit 1000 \
  -concurrency 16
```

`-fixture`, `-out`, `-endpoint`, and `-partner` are required. Run
`go run ./cmd/benchmark -h` for all options.

The fixture must contain one OpenRTB bid request per line. The command moves EIDs
from `user.ext.eids` to `user.eids`, matching the preprocessing done by Prebid
Server.

`-limit` defaults to 1000. Set it to `0` to process the whole file. Use `-skip`
to resume from a specific line.

## Cache

Caching is disabled by default. Pass `-cache <dir>` to enable the in-memory cache
and a file-backed cache that survives between runs:

```bash
go run ./cmd/benchmark ... -cache /path/to/cache -out /path/to/cold-results
go run ./cmd/benchmark ... -cache /path/to/cache -out /path/to/warm-results
```

`served_from_cache` indicates that no S2S request was made.

## Query parameters

- `-param key=value` adds or replaces a parameter.
- `-drop key` removes a parameter.
- `-require key` skips requests that do not contain the parameter.

These flags are repeatable and accept comma-separated values.

## Output

The output directory contains:

- `summary.json` with totals, outcomes, errors, timings, and cache statistics.
- `part-NNNNN/*.json` with the request URL, S2S response, and enrichment result
  for each fixture line.

## Sensitive data

Fixtures, results, and caches may contain IP addresses, user agents, advertising
IDs, consent data, and resolved EIDs. Keep real data outside the repository and
do not commit it.

Files under `testdata` must be small and synthetic. Never place captured traffic
or API responses there.
