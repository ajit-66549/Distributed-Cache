# Phase 8 measurement contract

Status: definitions for Phase 8 Task 1. Metrics, instrumentation, and the new
endpoints are planned; this document does not mean they are implemented.

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
updates must be race-free. Read stored gauges as a consistent snapshot under a
brief cache lock; avoid scanning all entries or holding the lock while formatting
the HTTP response. Preserve cache-before-AOF lock order and avoid introducing a
metrics lock that reverses that order.

## 2. Required measurements

Names and units below are the contract for later instrumentation. The metrics
library and exporter will be selected in a later task.

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

The planned endpoints use GET and return a small JSON response:

| Endpoint | Successful response | Unready response |
| --- | --- | --- |
| `/healthz` | `200`, `{"status":"ok"}` while the HTTP service can respond. | An unavailable or stopped listener cannot respond. |
| `/readyz` | `200`, `{"status":"ready"}` when normal durable operations are available. | `503`, `{"status":"not_ready"}` if reachable while unready. |
| `/metrics` | `200` with exported measurements. | Exporter behavior will be specified when the exporter is implemented. |

Readiness is true only after configuration, persistence validation, recovery,
and worker initialization succeed, and before shutdown begins. It becomes false
on a terminal persistence condition that disables durable writes (including I/O
failure or sequence exhaustion), snapshot-worker failure, or shutdown initiation.
A terminal persistence condition remains unready until
restart and successful recovery; a successful GET does not restore readiness.

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
