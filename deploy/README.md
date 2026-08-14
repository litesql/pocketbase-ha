# Docker fixtures

Local 3-node PocketBase HA stacks. **Only one fixture at a time** — they share host ports.

| Directory | NATS | File cache | Files | Leader |
|-----------|------|------------|-------|--------|
| `docker-embedded-nats` | Clustered embedded | off | Local disk | Dynamic (`PB_LOCAL_TARGET`) |
| `docker-external-nats` | One external JetStream server | off | Local disk | Dynamic |
| `docker-embedded-nats-filecache` | Clustered embedded | 64 MiB | Local disk | Dynamic |
| `docker-embedded-nats-s3` | Clustered embedded | 64 MiB | RustFS S3 | Leaderless |
| `docker-external-nats-s3` | One external JetStream server | 64 MiB | RustFS S3 | Leaderless |

## Start / stop

From the repo root:

```sh
make docker-embedded-nats
make docker-embedded-nats-down

make docker-external-nats
make docker-embedded-nats-filecache
make docker-embedded-nats-s3
make docker-external-nats-s3
```

Or:

```sh
docker compose -f deploy/docker-embedded-nats/docker-compose.yml up --build
```

Images build from this repo's `Dockerfile`.

## Ports

| Service | Host |
|---------|------|
| node1 HTTP / gRPC | 8090 / 9090 |
| node2 HTTP / gRPC | 8091 / 9091 |
| node3 HTTP / gRPC | 8092 / 9092 |
| Embedded NATS client (embedded fixtures) | 4222, 4223, 4224 |
| External NATS (external fixtures) | 4222 |
| RustFS S3 API / console (S3 fixtures) | 9000 / 9001 |

## Credentials

These are **demo values** for local fixtures, not for production.

- PocketBase superuser: `test@example.com` / `1234567890` (created on node1)
- RustFS: `rustfsadmin` / `rustfsadmin`
- Bucket: `pb-files`

Upsert a different superuser:

```sh
docker compose -f deploy/docker-embedded-nats/docker-compose.yml exec \
  -e PB_NATS_CONFIG="" -e PB_LOCAL_TARGET="" \
  node1 /app/pocketbase-ha superuser upsert EMAIL PASS
```

## What init jobs do

`settings-init` waits for node1, authenticates as superuser, and `PATCH /api/settings`:

- Always: Trusted Proxy header `X-Forwarded-For` (so replica file proxy can forward the client IP).
- S3 fixtures: enable filesystem S3 at `http://rustfs:9000` with `forcePathStyle`, bucket `pb-files`.

S3 fixtures also run `bucket-init` (`aws s3 mb`) after RustFS is healthy. Settings replicate with SQLite; only node1 is patched.

RustFS console: http://localhost:9001

## Verify

With a fixture already up:

```sh
make verify-docker-embedded-nats
make verify-docker-external-nats
make verify-docker-embedded-nats-filecache
make verify-docker-embedded-nats-s3
make verify-docker-external-nats-s3
```

`verify-cluster.sh` writes a record on `:8090` and waits for `:8091` and `:8092`. `verify-files.sh` uploads a file on node1 and `GET`s it from node2.
