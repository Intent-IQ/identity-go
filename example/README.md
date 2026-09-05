# Identity enrichment example

This example shows how a host application wires the identity-go S2S client, generic cache, Valkey Store, logger, and enrichment flow. It also demonstrates host-owned EID mutation and fail-open error handling.

Docker Compose starts Valkey and the local S2S mock used by the example configuration.

## Prerequisites

- Go 1.25 or newer
- Docker with Docker Compose

## Quick start

```bash
cd example
docker compose up -d
go run . -config config.yaml
```

The first enrichment resolves through S2S and writes the result to Valkey. The second enrichment uses the cached result.

Stop the local services when finished:

```bash
docker compose down
```

Edit `config.yaml` to change the partner ID, endpoint, timeout, cache policy, or Valkey connection.
