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
| `GET /healthz` | `200`, `{"status":"ok"}` while HTTP can respond. |
| `GET /readyz` | `200`, `{"status":"ready"}`, or `503`, `{"status":"not_ready"}`. |
| `GET /metrics` | `200` with Prometheus metrics; `500` if collection fails. |
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

Phase 8 starts with the [planned observability contract](docs/observability.md)
and [engine benchmark baseline](benchmarks/results.md). The
[metrics package](internal/metrics/metrics.go) provides counters, gauges, and
request-duration histograms. Server operations record cache hits, misses,
evictions, TTL removals, stored key/value bytes, the byte budget, and item count.
Recovery populates size gauges without replaying runtime removal counters.
HTTP middleware records request counts by method, route pattern, and response
status, plus duration histograms spanning validation, cache work, and response
generation. `/metrics` exposes the shared registry; scrapes and probes are
excluded from application request metrics.

Check a running server:

```sh
curl -i http://localhost:8080/healthz
curl -i http://localhost:8080/readyz
curl http://localhost:8080/metrics
```

Readiness becomes true after recovery and worker initialization. It becomes false
when persistence can no longer accept durable writes, on snapshot-worker failure,
or when shutdown starts. Health stays a lightweight liveness response. Both probes
avoid cache/persistence locks and disk I/O. Recovery runs before the listener
starts, so probes cannot connect during recovery; shutdown closes the listener
after readiness has been withdrawn. Readiness does not change cache API responses,
and reads can still work while persistence is failed. Normal misses, input
rejections, and a full cache do not make the server unready.

## View cache metrics in Prometheus

1. Install Prometheus once with `brew install prometheus` on macOS. On other
   platforms, get the [Prometheus binary](https://prometheus.io/download/) and
   put it on your `PATH`.
2. From the repository directory, run `make run` and leave it running. The cache
   exposes raw metrics at [http://localhost:8080/metrics](http://localhost:8080/metrics).
3. In a second terminal in the same directory, run `make prometheus`.
   [prometheus.yml](prometheus.yml) tells Prometheus to scrape the cache every
   five seconds. Prometheus stores its time series in `prometheus-data/`.
4. Open [http://localhost:9090](http://localhost:9090). On the
   [Targets page](http://localhost:9090/targets), look for the `cache` job and
   wait for it to show **UP**. In [Query](http://localhost:9090/query), enter
   `up{job="cache"}` and click **Execute**. A value of `1` means the last scrape
   succeeded. Use **Table** for the latest value or **Graph** for the history.

To generate a hit and a miss, run these in another terminal. The sample entry
has a 60-second TTL, and the missing-key request is expected to return `404`.

```sh
curl -i -X PUT http://localhost:8080/v1/cache/prometheus-demo \
  -H 'Content-Type: application/json' \
  -d '{"value":"sample","ttl_seconds":60}'
curl -i http://localhost:8080/v1/cache/prometheus-demo
curl -i http://localhost:8080/v1/cache/prometheus-demo-missing
```

After the next scrape, try `cache_hits_total` and `cache_misses_total` for lookup
counts, `cache_items` for the number of stored entries, or `cache_used_bytes` for
stored key and value bytes. `cache_http_requests_total` appears after the first
application request and groups counts by method, route, and status.

If the target shows **DOWN**, check that `curl -f http://localhost:8080/metrics`
returns metrics. If you set `CACHE_PORT`, update the target port in
`prometheus.yml` and restart Prometheus. Stop either process with `Ctrl+C` in
its terminal.
