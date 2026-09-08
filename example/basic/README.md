# Identity enrichment and reporting example

This example shows how a host application wires identity enrichment, Valkey caching, and impression reporting. It demonstrates host-owned EID mutation, fail-open handling, bid iteration, asynchronous reporting, and panic recovery.

Docker Compose starts Valkey and a local mock that accepts both S2S identity resolution and impression reports.

## Prerequisites

- Go 1.25 or newer
- Docker with Docker Compose

## Quick start

```bash
cd example/basic
docker compose up -d
go run . -config config.yaml
```

The first enrichment resolves through S2S and writes the result to Valkey. The second enrichment uses the cached result. The example then queues one impression report and waits for it only so the short-lived process does not exit first.

To inspect the received report:

```bash
docker compose logs iiq-s2s-api-mock
```

Stop the local services when finished:

```bash
docker compose down
```

Edit `config.yaml` to change the partner ID, S2S or reporting endpoint, timeout, cache policy, or Valkey connection.
