# Cache

A single-node, in-memory Go cache with an HTTP API, TTL expiration, LRU eviction,
an append-only log (AOF), and periodic snapshots for restart recovery.

Run from the repository directory:

```sh
make run
```

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `CACHE_PORT` | `8080` | HTTP port. |
| `CACHE_MAX_BYTES` | `67108864` (64 MiB) | Budget for stored key and value bytes. |
| `CACHE_DATA_DIR` | `./data` | Persistence directory, relative to the working directory. |
| `CACHE_SNAPSHOT_INTERVAL` | `1m` | Positive Go duration between snapshot ticks, such as `10s` or `5m`. |

For example, save snapshots every ten seconds in another data directory:

```sh
CACHE_DATA_DIR=./cache-data CACHE_SNAPSHOT_INTERVAL=10s make run
```

Invalid, zero, or negative snapshot intervals stop startup. Keep the same data
directory and memory budget when restarting. The default `data/` directory is
gitignored.

The HTTP endpoints are:

| Request | Result |
| --- | --- |
| `GET /v1/ping` | `200`, `{"status":"ok"}`. |
| `PUT /v1/cache/{key}` | `201`, `{"status":"stored"}`. |
| `GET /v1/cache/{key}` | `200` with the value, or `404` with `{"value":null}`. |
| `DELETE /v1/cache/{key}` | `200`, `{"status":"deleted"}`, or `404` if missing. |

Store and read an entry:

```sh
curl -i -X PUT http://localhost:8080/v1/cache/name \
  -H 'Content-Type: application/json' \
  -d '{"value":"Ajit"}'
curl -i http://localhost:8080/v1/cache/name
```

Startup locks the data directory and validates the published snapshot, if one
exists, plus the AOF. It restores live snapshot entries and applies AOF records
after the snapshot checkpoint before serving requests. Without a snapshot, it
replays the full log. Original expiration deadlines and saved LRU order are
preserved; reads after the snapshot are not logged.

SET, DELETE, and eviction mutations are written and synced before changing cache
memory and returning success. An incomplete final AOF record is removed at
startup, and the repair offset and discarded byte count are logged. Corrupt
records, invalid or incomplete snapshots, missing history, and budget mismatches
stop startup.

The snapshot worker starts after recovery. Its first save follows one configured
interval, and saves run one at a time. Each save captures live entries, publishes
a synced temporary file as `snapshot.bin`, and replaces `append.aof` with a synced
header-only log at the same checkpoint. The next mutation keeps the sequence
going. Cache operations wait while capture, publication, and rotation hold the
cache lock.

The console logs the configured interval and each completed snapshot/rotation.
A snapshot-worker failure is logged and triggers application shutdown.

Stop with `Ctrl+C` or `SIGTERM`. Shutdown cancels background work, drains HTTP
requests, waits for active snapshot work, then syncs and closes the AOF and
releases the directory lock. HTTP draining has a ten-second timeout; an active
snapshot is still awaited before persistence closes. No final snapshot is
required because the published snapshot and synced AOF already cover
acknowledged mutations.

See the [API protocol](docs/protocol.md) for request and response details and the
[persistence design](docs/design.md#9-persistence) for file formats, durability
guarantees, and limits.
