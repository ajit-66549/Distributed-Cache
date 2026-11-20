# Phase 8 measurement contract

Status: Phase 8 Task 1 definitions, Task 3 metrics package, Task 4 GET hit/miss
instrumentation, Task 5 removal/size instrumentation, and Task 6 HTTP
instrumentation are implemented. Task 7 exposes metrics, health, and readiness.

This contract describes the current single-node cache. It preserves HTTP
responses, TTL behavior, LRU behavior, and the rule that mutations are synced to
the AOF before memory changes and success responses.

## 1. Ownership and lifetime

The HTTP layer owns request counts, response statuses, and request durations.
The cache engine owns hits, misses, removal events, and stored-size accounting.
Application lifecycle state owns readiness. Do not count the same event in both
the handler and the engine.

Counters start at zero for each server process and are not persisted. Snapshot
restore, AOF replay, and startup expiration cleanup do not increment runtime
event counters. Gauges reflect the recovered store when serving begins. Normal
engine calls after startup, including calls outside HTTP, follow the same engine
counter rules; engine counters need not equal HTTP request counts.

Metric collection is read-only: it must not call GET, change LRU order, clean up
expired entries, append records, or save snapshots. Concurrent collection and
updates must be race-free. The cache owner captures and publishes stored gauges
as one snapshot while holding its cache lock. Collection reads the published
snapshot without acquiring cache/AOF locks, scanning entries, or holding a lock
while formatting the HTTP response. Preserve cache-before-AOF lock order and
avoid introducing a metrics lock that reverses that order.

## 2. Required measurements

Names and units below are the contract for later instrumentation. The metrics
package uses the Prometheus Go client with a private registry per instance.
HTTP exporter wiring belongs to a later task.

| Metric | Type | Labels | Definition |
| --- | --- | --- | --- |
| `cache_http_requests_total` | Counter | `method`, `route`, `status` | One event per eligible HTTP request when its handler returns or unwinds. |
| `cache_http_request_duration_seconds` | Histogram | `method`, `route` | One duration observation for each request counted above, in seconds. |
| `cache_hits_total` | Counter | None | One GET that returns a present, unexpired entry, including an empty value. |
| `cache_misses_total` | Counter | None | One GET that finds no entry or finds an expired entry. |
| `cache_evictions_total` | Counter | None | Number of entries actually removed by a successful SET's LRU eviction plan. |
| `cache_expirations_total` | Counter | `source` | Number of entries actually removed for TTL expiration; `source` is `lazy` or `cleanup`. |
| `cache_used_bytes` | Gauge | None | Sum of `len(key) + len(value)` for physically stored entries. |
| `cache_max_bytes` | Gauge | None | Configured key/value byte budget. |
| `cache_items` | Gauge | None | Number of physically stored entries, matching `Store.Len()`. |
| `cache_go_heap_alloc_bytes` | Gauge | None | Go runtime's currently allocated heap bytes for the whole process, sampled at collection. |

`cache_used_bytes` excludes map buckets, list elements, entry metadata, allocator
overhead, HTTP buffers, and persistence buffers. `cache_go_heap_alloc_bytes`
includes heap allocations beyond the cache and is neither a resident-memory
measurement nor the cache budget. Do not compare either value to the other as
though they had the same scope. Process resident-memory collection may be added
separately with an explicit name and platform-support policy.

Expired entries continue to contribute to `cache_items` and `cache_used_bytes`
until an operation physically removes or replaces them. Passing a TTL deadline
alone does not change these gauges or increment an expiration counter. The first
version does not expose a separate exact live-item gauge, which would require
additional expiration bookkeeping or a scan.

## 3. Lookup and removal rules

Each completed runtime `Store.Get` increments exactly one of hits or misses.
HTTP validation or routing failures never reach GET and increment neither. A
DELETE of a missing key does not increment misses. Successful SET and DELETE
operations do not increment hits.

Removal counters describe the operation that actually removed the entry:

| Removal path | Metric change |
| --- | --- |
| GET discovers and removes an expired entry | One miss and one expiration with `source="lazy"`. |
| Background cleanup removes N expired entries | N expirations with `source="cleanup"`; no hits or misses. |
| A successful SET removes N planned LRU victims | N evictions, counted after the durable append succeeds and the removals apply. |
| Explicit DELETE removes an entry | No eviction or expiration event, even if its deadline has passed. |
| SET replaces an existing entry | No eviction or expiration event for the replaced entry. |
| Recovery restores, skips, or removes an entry | No runtime hit, miss, eviction, or expiration event. |
| Snapshot capture omits an expired entry | No removal event: capture does not remove it from the running store. |

An expired entry selected as an LRU victim counts as an eviction, because memory
pressure caused that removal. It is not also counted as an expiration. Once an
expired entry is removed by one path, another path must not count it again.

Rejected writes and failed AOF appends leave planned victims, item count, and
stored bytes unchanged. The HTTP response can still count as a rejection or
failure. A later recovery may apply a complete record left by a failed or
unanswered write; that does not retroactively change the previous process's
metrics or establish exactly-once request handling.

## 4. HTTP scope, errors, and label limits

Include cache requests, `/v1/ping`, validation failures, and router-generated
responses. Exclude `/metrics`, `/healthz`, `/readyz`, and future pprof endpoints
from both application request counters and duration histograms so scrapes and
probes do not inflate application traffic.

Use a bounded set of label values:

* `method`: `GET`, `PUT`, `DELETE`, `HEAD`, `OPTIONS`, or `OTHER`.
* `route`: the matched path template `/v1/cache/{key}` or `/v1/ping`; use
  `unmatched` when no application handler matches, including unsupported methods.
* `status`: the first final HTTP response status; an ordinary implicit response
  is `200`. Use `aborted` if the handler panics or aborts before a final status.
* `source`: only `lazy` or `cleanup` for expiration events.

Do not label metrics with actual keys, raw URLs, query strings, values, client
addresses, or error messages. The router's automatic HEAD handling can invoke a
GET handler; count the HTTP method as HEAD and any engine lookup by the normal
GET rules.

Requests rejected before reaching application middleware, such as malformed
HTTP or a header timeout, are outside these application metrics. A response
status measures what the server emitted, not proof that the client received the
whole response. A disconnect after headers retains the recorded status.

Use status-labelled request counts to measure errors rather than adding another
counter for the same HTTP outcome:

* `5xx` responses and `aborted` are server failures.
* `400` is invalid input; `413` is a capacity or record-size rejection. Neither
  alone means persistence is unhealthy.
* GET `404` on the cache route is a normal cache miss. DELETE `404` is a normal
  absent-key outcome. Unmatched-route `404` is a routing rejection.
* Other `4xx` responses are client/routing rejections, reported separately from
  server failures.

Server failure rate is failures divided by all eligible requests over the same
interval. Cache hit ratio is `hits / (hits + misses)` over the same interval;
with no lookups it is undefined, not 100%. Throughput is the change in request
count divided by elapsed seconds. Filter to the cache route when reporting cache
API throughput so ping requests do not contribute. Total throughput includes
rejections and failures; report statuses alongside it and identify successful
throughput separately.

## 5. Request-duration histogram

Measure from application middleware entry until the handler returns or unwinds,
including validation, lock waits, engine work, durable writes, and JSON encoding.
Time spent waiting for a snapshot-held cache lock is included. Network connection
setup, pre-handler HTTP parsing, and final delivery to the client are outside
this timer; HTTP load tools measure their own client-side latency separately.

Initial finite bucket upper bounds, in seconds:

```text
0.0001, 0.0005, 0.001, 0.005, 0.01, 0.025, 0.05,
0.1, 0.25, 0.5, 1, 2.5, 5, 10
```

Include the histogram's overflow (`+Inf`) bucket, count, and sum. Derive p50,
p95, and p99 over a stated interval. Histogram percentiles are bucket-based
estimates; preserve bucket configuration when comparing runs. Do not average
percentiles across routes or benchmark runs. Report GET and PUT separately when
examining read and durable-write latency.

## 6. Health and readiness

The implemented probe endpoints use GET and return a small JSON response.
The metrics endpoint serves Prometheus-compatible measurements:

| Endpoint | Successful response | Unready response |
| --- | --- | --- |
| `/healthz` | `200`, `{"status":"ok"}` while the HTTP service can respond. | An unavailable or stopped listener cannot respond. |
| `/readyz` | `200`, `{"status":"ready"}` when normal durable operations are available. | `503`, `{"status":"not_ready"}` if reachable while unready. |
| `/metrics` | `200` with exported measurements from the application's registry. | Collection failure returns `500`; unready state alone does not prevent scraping. |

Readiness is true only after configuration, persistence validation, recovery,
and worker initialization succeed, and before shutdown begins. It becomes false
on a terminal persistence condition that disables durable writes (including I/O
failure or sequence exhaustion), snapshot-worker failure, or shutdown initiation.
I/O failure requires restart and successful recovery; a successful GET does not
restore readiness. Exhausted sequence history remains unwritable when reopened.

Startup currently binds the HTTP listener after recovery, so probes cannot
connect during recovery. Do not describe that period as an actual HTTP 503 or
move listener startup earlier just to implement probes. Once shutdown closes the
listener, probes likewise receive no HTTP response. If reachable during draining,
readiness is false and health remains a liveness check.

Normal misses, rejected input, a full cache, routine expiration cleanup, and an
in-progress successful snapshot do not change readiness. Health and readiness
must not perform disk writes, acquire the cache/AOF locks, or wait for snapshot
completion; use lifecycle/persistence state published by the relevant owner.
Readiness is advisory and does not change existing cache endpoint responses.

## 7. Verification examples for later instrumentation

Run each scenario from a known starting state and compare metric deltas, not
unrelated process totals. Prevent unrelated cleanup and snapshot activity during
deterministic assertions.

| Scenario | Expected result |
| --- | --- |
| PUT a key, GET it twice, GET a missing key once | Four HTTP requests, two hits, one miss; no removals. |
| PUT `a=b`, then replace with `a=long` | One item; stored bytes change from 2 to 5. |
| GET an expired, physically present key twice | Two misses; exactly one lazy expiration; one physical removal. |
| Cleanup removes two expired keys, then runs again | Two cleanup expirations total; second pass adds zero. |
| Successful SET evicts three victims | Three evictions; gauges reflect the final applied batch. |
| SET plans victims but AOF append fails | HTTP 500; zero evictions; stored bytes and items unchanged; readiness false. |
| PUT exceeds the budget | HTTP 413; existing entries unchanged; readiness remains true. |
| DELETE an expired, physically present entry | HTTP 200; item/byte gauges fall; no hit, miss, eviction, or expiration event. |
| Restart with a snapshot and AOF | Runtime counters begin at zero; gauges match the recovered physical store. |
| Scrape metrics and probe health/readiness | No application request, lookup, removal, or LRU changes. |
| Save a snapshot while requests wait | Successful snapshot keeps readiness true; waiting request durations include the pause. |

Run race checks once instrumentation exists. Keep the uninstrumented engine
baseline and its limitations in [the benchmark record](../benchmarks/results.md).

## 8. Metrics package (Task 3)

[`internal/metrics`](../internal/metrics/metrics.go) provides the measurements
without importing the cache or server packages. `New()` creates an independent
instance; it does not modify Prometheus's global registry. Its methods support
concurrent updates and collection. The zero value is not usable.

| API | Purpose |
| --- | --- |
| `RecordHit()`, `RecordMiss()` | Record engine lookup outcomes. |
| `AddEvictions(count)` | Record victims after a successful batch applies. |
| `RecordLazyExpiration()`, `AddCleanupExpirations(count)` | Record TTL removals with the two allowed sources. The caller separately records a lazy-expiration GET miss. |
| `SetCacheStats(CacheStats)` | Publish used bytes, maximum bytes, and physical item count as one snapshot. Invalid negative values or used bytes above the maximum return an error without changing gauges. |
| `ObserveRequest(method, route, status, elapsed)` | Record one request count and duration observation, excluding probe/profiling routes. |
| `Gatherer()` | Supply collection access for tests and the future exporter. |

Counters and published cache gauges start at zero. HTTP label combinations are
created on first observation, so their metric families are absent until a request
is observed. Both expiration sources are present at zero from construction. Heap
usage is sampled from the Go runtime on each collection and covers the process
shared by all instances.

`ObserveRequest` accepts a path template without a method prefix. Unknown methods
map to `OTHER`; unknown routes map to `unmatched`. Status zero represents an abort
before a final response, and statuses outside 200 through 599 also map to
`aborted`. Negative elapsed durations are clamped to zero. The HTTP middleware
uses the router's selected pattern and handles first-final-status detection and timing;
passing a raw key path never exposes that key as a metric label.

`SetCacheStats` copies values into a snapshot protected by a short metrics lock.
The cache integration captures and publishes the snapshot while holding the
cache lock, preserving mutation order. Collection copies the entire snapshot
and releases the metrics lock before emitting measurements; it never calls back
into the cache. Cache/HTTP integration, lifecycle readiness, and route installation
are not part of Task 3.

Package tests cover counter totals, metric definitions, histogram boundaries and
overflow, bounded labels, probe exclusions, rejected snapshots, independent
instances, and concurrent updates with consistent gauge collection.

## 9. GET hit and miss instrumentation (Task 4)

`cache.WithMetrics(measurements)` attaches an existing metrics instance when
constructing a store. `NewStore`, `NewStoreWithLimit`, and `NewStoreWithLog` accept
this optional setting; callers that omit it have cache instrumentation disabled.
The server creates one instance at startup and passes it through recovery to the
store before serving requests. Snapshot restoration and AOF replay do not call
GET and therefore do not add lookup events.

With metrics attached, each `Store.Get` increments exactly one lookup counter:

* A present, unexpired entry increments hits, including an empty value.
* An absent key increments misses.
* An expired entry is removed and increments misses. Another GET of that key
  increments misses again because the entry is now absent.

These updates happen under the existing cache lock. SET, DELETE, cleanup, and
recovery do not increment lookup counters. Two successful GETs followed by one
absent-key GET produce two hits and one miss on a fresh instance.

Task 4 connects lookup counters; Task 5 adds removal counters and size gauges as
described below. Task 6 connects HTTP measurements; Task 7 exposes the monitoring
endpoints. Measurements can be collected through the supplied instance's
`Gatherer()` or `/metrics`.

## 10. Removal and size instrumentation (Task 5)

Stores with metrics attached publish the configured byte budget, zero used
bytes, and zero items during construction. Size updates use the existing byte
accounting and map length; no entry scan is needed to collect the gauges.

| Operation | Measurement update |
| --- | --- |
| Successful SET or replacement | Publish final stored bytes and item count after the full mutation batch. Count the actual LRU victims as evictions. |
| Failed or rejected SET | No size or eviction measurement changes; planned victims were not removed. |
| Successful DELETE | Publish the remaining bytes and items, without incrementing removal counters. |
| Missing or failed DELETE | No size or removal measurement changes. |
| GET removes an expired entry | Record one miss and one `lazy` expiration, then publish the remaining size. |
| Runtime cleanup removes N expired entries | Add N `cleanup` expirations and publish size once after the batch. An empty pass adds nothing. |
| Snapshot restore or AOF replay | Publish the recovered physical size without incrementing runtime lookup or removal counters. |
| Snapshot capture | No size or removal changes; skipping expired entries in a snapshot does not remove them from the running store. |

Runtime SET instrumentation runs only after the durable append succeeds and
the complete in-memory batch applies. Low-level insertion and removal helpers
do not publish partial sizes or guess the removal reason; this also keeps
recovered DELETE records from inflating eviction counters.

`FinishRecovery()` performs startup's final expiration pass without counting
runtime cleanup events and publishes the final size before requests are served.
The background worker continues to use `DeleteExpired()`, which records runtime
cleanup removals. Expired entries selected by LRU pressure count as evictions;
explicit deletes and replacements do not count as expiration removals.

`publishSizeMetricsLocked()` publishes used bytes, maximum bytes, and items as
one snapshot while the cache lock is held, or while a constructor owns an
unpublished store. Collection never acquires a cache lock. An invalid snapshot
indicates an internal accounting problem and is logged without changing the
result of a durable mutation that has already applied.

## 11. HTTP request instrumentation (Task 6)

[`server.InstrumentHTTP`](../internal/server/metrics.go) wraps the complete router.
Application startup passes it the same metrics instance attached to the cache.
Installing it once records one request counter increment and one duration
observation per eligible request. Passing nil metrics leaves the handler unchanged.

The timer starts before routing and stops after handler completion, including
JSON decoding and validation, time inside cache calls (lock waits and durable
persistence included), and response encoding/writing. It excludes connection
setup and final network delivery after the handler returns.

After routing, the middleware reads `Request.Pattern` and removes its method
prefix, yielding `/v1/cache/{key}` or `/v1/ping`. Raw keys and query strings are
never labels. Router-generated 404 and 405 responses use `unmatched`; unsupported
methods use `OTHER` according to the metrics package's bounded label rules.

A response-writer wrapper captures the first final status and handles implicit
200 responses. Informational 1xx responses do not replace the final status.
Deferred collection runs even if a handler panics: a panic before final headers
records `aborted`, while one after final headers retains their status. The panic
continues to the caller/HTTP server; middleware does not convert it into a 500.

Request counters distinguish client rejections such as 400/413 from server
failures such as 500 using the status label. GET 404 remains a normal miss.
Duration histograms are grouped by method and route as defined in the contract.

Metrics, health/readiness, and pprof paths bypass application instrumentation
before routing, even when those endpoints are not registered. This task does not
install those endpoints. Focused tests exercise status labels, route grouping,
implicit/informational responses, panics, exclusions, and timing across request
decoding, cache calls, and response writing.

## 12. Monitoring routes and lifecycle readiness (Task 7)

`server.WithMonitoring(gatherer, readiness)` configures `NewHandler` to register
the three monitoring endpoints on the same router as the cache API. `/metrics`
uses Prometheus's `promhttp.HandlerFor` with the supplied private registry and
returns HTTP 500 on collection failure. It remains available while unready.
The existing middleware excludes all three routes from application observations.

`server.Readiness` starts unready. Startup calls `MarkReady()` after `openStore`
has completed recovery and both workers have been started, before the listener
begins accepting requests. `Ready()` checks atomic lifecycle flags, the shutdown
context, and the persistence owner's nonblocking `Writable()` callback. Missing
readiness configuration is unready. `BeginShutdown()` permanently withdraws
readiness, and signal-context cancellation withdraws it even before the main
shutdown select loop runs. Snapshot-worker and listener results lead through the
same shutdown path before HTTP draining and persistence close.

The AOF publishes writability using an atomic boolean. It is false before open,
after close begins, after a terminal append/sync/snapshot publication or rotation
failure, and when the last sequence reaches its maximum. Validation rejections
and routine successful snapshots do not disable it. The flag describes observed
writer state; probes do not issue test writes or wait for the AOF mutex.

Readiness is advisory. Existing GET/PUT/DELETE responses retain their contract,
and successful reads after a persistence failure do not make readiness true.
Health simply returns a liveness response without consulting persistence.
Tests cover metric exposition and probe exclusions, startup/shutdown state,
collection failure, actual I/O failures, sequence exhaustion, nonblocking checks,
and readiness after snapshot/AOF recovery and a later rotation failure.
