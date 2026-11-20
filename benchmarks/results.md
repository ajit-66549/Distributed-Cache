# Performance evidence

## Phase 8: uninstrumented engine baseline

The existing memory-only engine benchmarks are the baseline for future Phase 8
instrumentation. This run completed successfully with 60 samples across 12
benchmark cases. No instrumentation or engine changes were made for this run.

Raw evidence: [baseline-2026-11-13.txt](baseline-2026-11-13.txt).
Measurement definitions: [observability contract](../docs/observability.md).

### Source and environment

| Setting | Recorded value |
| --- | --- |
| Date | 2026-11-13, America/Chicago; raw output completed at approximately 03:28 CST. |
| Source commit | `27066ef17c62c303f86087c79bb3afc755aeae63` |
| Source state | Clean before starting this benchmark run; only documentation and benchmark evidence changed during the run. Go sources and benchmarks were unchanged. |
| Module | `github.com/ajit-66549/distributed-cache` |
| Go version | `go1.27.1 darwin/arm64` |
| Operating system | macOS 15.7.3, build 24G419 |
| Machine | Apple M3, model `Mac15,12` |
| CPU cores | 8 physical, 8 logical |
| RAM | 8,589,934,592 bytes (8 GiB) |
| Benchmark parallelism | `GOMAXPROCS=1`, `-cpu=1`; benchmark loops are serial. |
| Sampling | `-benchtime=1s`, `-count=5`; Go calibrates the iteration count for each sample. |
| Allocation reporting | `-benchmem`; existing benchmarks also call `b.ReportAllocs()`. |
| Build cache | `GOCACHE=/private/tmp/distributed-cache-phase8-go-build`, warmed by the preflight tests. |
| Other Go settings | `GOFLAGS`, `GOEXPERIMENT`, `GODEBUG`, `GOGC`, and `GOMEMLIMIT` unset; `GOTOOLCHAIN` resolves to `auto`; `CGO_ENABLED=1`. |
| Server configuration | `CACHE_*` settings are not loaded by these benchmarks. Each benchmark constructs a store directly. |
| Persistence and workers | No attached mutation log, HTTP server, snapshot worker, or expiration-cleanup worker in the measured benchmarks. |
| Host controls | No CPU affinity; background load, power mode, and thermal state were not controlled. |

### Reproduction commands

Run from the repository directory using the source revision and toolchain above.
The temporary build cache keeps generated build files outside the source tree.

Preflight command, completed successfully before benchmarking:

```sh
GOCACHE=/private/tmp/distributed-cache-phase8-go-build go test ./...
```

All five packages passed: `cmd/cache-server`, `internal/cache`,
`internal/config`, `internal/persistence`, and `internal/server`.

Exact benchmark command used, under zsh with pipeline failure propagation:

```sh
set -o pipefail
GOCACHE=/private/tmp/distributed-cache-phase8-go-build GOMAXPROCS=1 \
  go test ./internal/cache \
  -run '^$' \
  -bench '^Benchmark(Get|Set|SetEvictionCycle)$' \
  -benchmem -benchtime=1s -count=5 -cpu=1 \
  | tee benchmarks/baseline-2026-11-13.txt
```

`-run '^$'` skips correctness tests during measurement. The benchmark process
exited successfully and reported `PASS` in 82.681 seconds. Use a new output
filename for subsequent runs to retain this original evidence.

Environment inspection commands:

```sh
git rev-parse HEAD
git status --short
go version
go env GOOS GOARCH GOFLAGS GOTOOLCHAIN CGO_ENABLED
sw_vers
sysctl -n machdep.cpu.brand_string hw.model hw.physicalcpu hw.logicalcpu hw.memsize
```

### Workload configuration

The unchanged benchmark source is
[`internal/cache/lru_benchmark_test.go`](../internal/cache/lru_benchmark_test.go).
Its `newLimitedStore` helper uses `NewStoreWithLimit`, without a mutation log.

Small-entry keys are `key-%06d` (10 bytes for these workloads); values are the
fixed 32-byte string `0123456789abcdef0123456789abcdef`. Each small entry consumes
42 budgeted bytes. Entries have no TTL. Setup and prepopulation occur before
`b.ResetTimer()` and are excluded from the reported operation measurements.

| Workload | Cases | Byte budget | Measured operation |
| --- | --- | --- | --- |
| GET hit | 16, 1,024, or 65,536 entries | 672; 43,008; or 2,752,512 bytes | Read one existing key, cycling through keys; includes TTL check, LRU update, locking, and the benchmark's hit/value assertion. |
| SET update | Same three entry counts | Same three budgets | Replace one existing value, cycling through keys; includes locking and checking the returned error. |
| SET with eviction | Same three entry counts, plus one rotating extra key | Same three budgets | Insert one missing key and evict one LRU entry; includes locking and checking the returned error. |
| Eviction cycle | 8, 64, or 512 small entries | 336; 2,688; or 21,504 bytes | Write all N small entries, then write `large` with a value sized to fill the entire budget. One operation is N + 1 SET calls. |

The cycle benchmark starts each measured cycle with the budget-filling `large`
entry already present. The first small write removes that entry; the final
large write removes all N small entries. Large values are 331, 2,683, or 21,499
bytes respectively, plus the five-byte key `large`. Their preparation is outside
the timer.

### Results

Each row summarizes five samples from the raw file. Time is the median and
observed minimum–maximum of the reported `ns/op` values. Allocation columns are
medians of reported per-operation values. No samples were discarded.

| Benchmark | Median ns/op | Min–max ns/op | Median B/op | Median allocs/op | Writes/op |
| --- | ---: | ---: | ---: | ---: | ---: |
| `BenchmarkGet/entries=16` | 45.23 | 41.01–46.57 | 0 | 0 | — |
| `BenchmarkGet/entries=1024` | 45.13 | 42.91–49.61 | 0 | 0 | — |
| `BenchmarkGet/entries=65536` | 50.58 | 48.81–52.19 | 0 | 0 | — |
| `BenchmarkSet/update/entries=16` | 27.53 | 27.35–29.21 | 0 | 0 | 1 |
| `BenchmarkSet/update/entries=1024` | 31.81 | 31.52–34.29 | 0 | 0 | 1 |
| `BenchmarkSet/update/entries=65536` | 38.28 | 37.46–39.82 | 0 | 0 | 1 |
| `BenchmarkSet/evict/entries=16` | 124.9 | 124.1–159.3 | 128 | 3 | 1 |
| `BenchmarkSet/evict/entries=1024` | 140.3 | 139.1–145.8 | 128 | 3 | 1 |
| `BenchmarkSet/evict/entries=65536` | 190 | 186–190.5 | 128 | 3 | 1 |
| `BenchmarkSetEvictionCycle/victims=8` | 1055 | 1047–1083 | 1216 | 21 | 9 |
| `BenchmarkSetEvictionCycle/victims=64` | 8683 | 8581–10393 | 9408 | 136 | 65 |
| `BenchmarkSetEvictionCycle/victims=512` | 68388 | 67982–69302 | 76224 | 1035 | 513 |

In this run, successful reads and existing-key updates reported zero allocations
per operation. Single-entry eviction writes reported 128 B/op and three
allocations per operation. Cycle results describe the full group of writes,
not a single SET. The median across five samples is not a request-latency p50,
and these measurements do not yield request p95 or p99.

### Scope and future comparison

These are direct engine measurements on one developer laptop. They exclude HTTP
routing, validation, JSON, network costs, AOF encoding and sync, snapshots, TTL
cleanup, and concurrent request contention. They do not establish durable-write
latency, HTTP requests per second, or server capacity.

There is no optimized result yet. After instrumentation is implemented, repeat
the same workloads on the same machine with the same Go version, parallelism,
sampling, and benchmark code, saving the new source revision and raw output.
Retain the range of repeated samples when assessing changes; this run includes
visible variability and does not establish statistical significance.

Measure HTTP workloads and persistence-enabled workloads separately.
Preserve the current sync-before-success durability contract in
server comparisons and run HTTP tests long enough to include snapshot activity.

## Phase 8: expanded engine workloads

The original benchmark cases and raw baseline above are preserved. New workloads
live in [`engine_benchmark_test.go`](../internal/cache/engine_benchmark_test.go)
and [`persistence_benchmark_test.go`](../internal/cache/persistence_benchmark_test.go).
They exercise the real store; durable cases attach a real AOF rather than a test
log. The AOF benchmarks run on macOS and Linux, where directory locking is
supported. These additions change benchmark coverage and documentation.

### Workloads and measurement units

All cases use precomputed 10-byte keys and the existing 32-byte benchmark value.
Stores have a budget of 42 bytes per entry. Metrics, HTTP, snapshot workers, and
background cleanup workers are disabled so the measured scope is explicit.
Each benchmark reports `ns/op`, `B/op`, and `allocs/op` through Go's benchmark
harness and `b.ReportAllocs()`.

| Benchmark | Configuration | One measured operation |
| --- | --- | --- |
| `BenchmarkGetMissMemory` | 16, 1,024, or 65,536 stored entries | GET the same absent key; assert a miss. This is a repeated missing-key lookup, not random-key traffic. |
| `BenchmarkDeleteMemory/hit` | Batches of 1,024 or 65,536 keys | DELETE one present key; assert successful removal. A batch drains from full to empty, so its result averages changing occupancy. |
| `BenchmarkDeleteMemory/miss` | 16, 1,024, or 65,536 stored entries | DELETE the same absent key; assert no removal and no error. |
| `BenchmarkTTLCleanupMemory` | 1,024 or 65,536 entries; 0%, 50%, or 100% expired | One complete `DeleteExpired()` scan; assert the exact removal count. `scanned/op` and `removed/op` identify the batch size, not individual-operation latency. |
| `BenchmarkParallelMemory/get-hit` | Shared store with 1,024 or 65,536 entries | One successful GET, distributed across workers. |
| `BenchmarkParallelMemory/get-miss` | Same shared-store sizes | One GET of a fixed absent key. |
| `BenchmarkParallelMemory/set-update` | Same shared-store sizes | One SET replacing an existing value, without eviction. |
| `BenchmarkParallelMemory/mixed-90get-10set` | Same shared-store sizes | One GET or SET; each worker repeats nine successful reads and one update per ten operations. A final partial group can slightly change the ratio. |
| `BenchmarkPersistence/{memory,aof-sync}/set-update` | Capacity 16 entries (672 bytes) | One existing-key SET, with or without an AOF append and sync. |
| `BenchmarkPersistence/{memory,aof-sync}/set-evict` | Same capacity; 17 rotating keys | One SET inserting a missing key and evicting one victim; the durable record includes both mutations. |
| `BenchmarkPersistence/{memory,aof-sync}/set-delete-cycle` | Same capacity; 15 resident entries and one reusable slot | One SET inserting the extra key, then one successful DELETE of that key. **Two writes per op**; durable cases perform two syncs. |
| `BenchmarkPersistence/{memory,aof-sync}/parallel-set-update` | Same capacity; shared store | One existing-key SET, distributed across workers, with or without append and sync. |

Store construction, key preparation, initial population, and AOF open/close are
outside measurement. Successful DELETE benchmarks refill between batches with
the timer stopped; refill time and allocations are excluded. TTL benchmarks
use negative TTLs to establish expired entries without sleeps, and refill only
the expired subset between scans with the timer stopped. The 0%-expired case
repeatedly scans an unchanged store. These fixtures describe warm, controlled
workloads; untimed setup still affects cache locality and GC behavior.

For parallel workloads, `b.RunParallel` shares the store across the default
number of workers: one per `GOMAXPROCS`. Workers cycle precomputed keys locally.
Every operation still takes the store's exclusive mutex, including GETs that
maintain LRU order. Reported parallel `ns/op` is total elapsed time divided by
the combined operation count; it is an aggregate throughput cost, not a request
latency percentile or the time each worker waits. Worker startup and benchmark
checks are included and amortized over the run.

Persistence comparisons use identical workloads in `memory` and `aof-sync`
modes. Every timed durable mutation includes validation, record encoding, file
append, and the engine's `os.File.Sync()` before memory is changed. The
`writes/op` and `syncs/op` columns make cycle costs explicit. AOF directories are
isolated with `b.TempDir()` and removed after the benchmark; they grow during
each run without snapshotting or rotation. Concurrent durable writes serialize
on the store mutex, including their syncs.

`B/op` measures newly allocated heap bytes per measured operation, not retained
cache size, the configured byte budget, process RSS, or on-disk AOF bytes.
Allocation averages are rounded by Go's output, so a reported zero does not
prove that no allocation happened anywhere in the benchmark process.

### Reproduction

Run from the repository directory. The first command checks correctness; the
second measures without the race detector and skips correctness test execution.
Keep performance runs separate from other tests or builds.

```sh
GOCACHE=/private/tmp/distributed-cache-phase8-go-build \
GOMODCACHE=/private/tmp/distributed-cache-phase8-go-mod \
  go test -race ./...

set -o pipefail
TMPDIR=/private/tmp \
GOCACHE=/private/tmp/distributed-cache-phase8-go-build \
GOMODCACHE=/private/tmp/distributed-cache-phase8-go-mod \
GOMAXPROCS=1 \
  go test ./internal/cache -run '^$' \
  -bench '^Benchmark(GetMissMemory|DeleteMemory|TTLCleanupMemory|ParallelMemory|Persistence)$' \
  -benchmem -benchtime=300ms -count=3 -cpu=1,4 \
  | tee benchmarks/expanded-engine-2026-11-19.txt
```

Use a new output filename when repeating the run. The `-cpu` values select
`GOMAXPROCS=1` and `4`; serial cases keep serial loops under both settings.
Benchmark names with a `-4` suffix are the four-processor setting; names without
a suffix are the one-processor setting. All reported timing includes the
benchmark's operation-result checks. These runs do not measure HTTP latency,
instrumentation overhead, snapshot pauses, or maximum server capacity.

### Recorded run

Raw evidence: [expanded-engine-2026-11-19.txt](expanded-engine-2026-11-19.txt).

| Setting | Recorded value |
| --- | --- |
| Date | 2026-11-19, America/Chicago |
| Source base | `5eeaedb6de151181e849888ccc23247507251ddd`, plus the two new benchmark files in this change |
| Other working-tree changes | Pre-existing changes to `docs/observability.md`, `internal/cache/metrics_test.go`, and `internal/server/metrics_test.go`; correctness tests are excluded by `-run '^$'` during measurement. |
| Go version | `go1.27.1 darwin/arm64` |
| Operating system | macOS 15.7.3, build 24G419 |
| Machine | Apple M3, `Mac15,12`, 8 physical/logical cores, 8 GiB RAM |
| Sampling | 300 ms target per sample, three samples per workload/processor setting; Go calibrates iteration counts. |
| Other Go settings | `GOFLAGS`, `GOEXPERIMENT`, `GODEBUG`, `GOGC`, and `GOMEMLIMIT` unset; `GOTOOLCHAIN=auto`, `CGO_ENABLED=1`. |
| Persistence storage | Fresh temporary directories under `/private/tmp`, on `/dev/disk3s5`, mounted at `/System/Volumes/Data`; real file syncs. |
| Host controls | No CPU affinity; background load, power mode, and thermal state were not controlled. |

SHA-256 fingerprints of the benchmark sources used for this run:

```text
aa22ad0414242b189ae946721a64551d9e1bf7ee70bd0df398775049bf5296ca  internal/cache/lru_benchmark_test.go
c42fb035a05c8ef5cdfdc8537aadf88b325d9853934e245ec8782c49976c0851  internal/cache/engine_benchmark_test.go
7ead0309dcfbd338949322fc22d4ccd4cc5bd6e2761caa5b28ce24a3d0e2f6b4  internal/cache/persistence_benchmark_test.go
```

### Results and validation

The recorded command passed in 124.551 seconds: 30 workloads, two processor
settings, and three samples each, for **180 samples**. Every sample reports time
and allocations. The table below selects representative workloads; the raw file
contains every size, expiry fraction, mode, and processor setting. Values are
medians of three samples, with the observed time range retained. No samples
were discarded. These medians are not request-latency percentiles.

| Benchmark | Median ns/op | Min–max ns/op | Median B/op | Median allocs/op |
| --- | ---: | ---: | ---: | ---: |
| `BenchmarkGetMissMemory/entries=1024` | 11.88 | 10.18–12.28 | 0 | 0 |
| `BenchmarkDeleteMemory/hit/batch=1024` | 65.77 | 65.67–70.90 | 64 | 1 |
| `BenchmarkDeleteMemory/miss/entries=1024` | 10.64 | 10.62–12.32 | 0 | 0 |
| `BenchmarkTTLCleanupMemory/entries=1024/expired=0pct` | 7,358 | 7,310–7,417 | 0 | 0 |
| `BenchmarkTTLCleanupMemory/entries=1024/expired=50pct` | 37,018 | 36,899–39,932 | 0 | 0 |
| `BenchmarkTTLCleanupMemory/entries=1024/expired=100pct` | 52,659 | 52,431–53,584 | 0 | 0 |
| `BenchmarkParallelMemory/get-hit/entries=1024` | 43.76 | 43.07–48.26 | 0 | 0 |
| `BenchmarkParallelMemory/get-hit/entries=1024-4` | 181.8 | 177.7–188.2 | 0 | 0 |
| `BenchmarkParallelMemory/mixed-90get-10set/entries=1024-4` | 177.2 | 169.5–178.4 | 0 | 0 |
| `BenchmarkPersistence/memory/set-update/capacity=16` | 29.20 | 28.11–29.33 | 0 | 0 |
| `BenchmarkPersistence/aof-sync/set-update/capacity=16` | 3,824,091 | 3,519,845–5,784,942 | 648 | 10 |
| `BenchmarkPersistence/memory/set-evict/capacity=16` | 128.1 | 124.7–130.4 | 128 | 3 |
| `BenchmarkPersistence/aof-sync/set-evict/capacity=16` | 3,140,941 | 2,934,792–4,911,114 | 1,160 | 18 |
| `BenchmarkPersistence/memory/set-delete-cycle/capacity=16` | 134.9 | 126.1–139.9 | 176 | 3 |
| `BenchmarkPersistence/aof-sync/set-delete-cycle/capacity=16` | 7,415,563 | 6,519,112–7,726,988 | 1,283 | 21 |
| `BenchmarkPersistence/aof-sync/parallel-set-update/capacity=16-4` | 5,587,090 | 3,106,995–6,056,666 | 720 | 10 |

This run makes the additional removal work in TTL scans visible, and separates
nanosecond-scale memory writes from millisecond-scale append-and-sync writes on
this machine. Parallel workloads show contention rather than automatic scaling:
four GET workers share the same exclusive LRU lock. Hardware and sample
variability limit these observations; no optimization or speedup is claimed
against the earlier baseline, whose workload coverage and sampling differ.

Validation completed successfully before measurement:

* `gofmt -l .` and `git diff --check` were clean.
* `go vet ./...`, `go test -race ./...`, and the application build passed.
* All new workloads passed a two-iteration smoke run at `-cpu=1,4`, exercising
  repeated cleanup/refill and the real AOF path.
* Successful DELETE cases passed at `-benchtime=70000x`, crossing both the
  1,024-key and 65,536-key refill boundaries.
* `BenchmarkParallelMemory` and `BenchmarkPersistence` passed with `-race`,
  `-benchtime=100x`, and `-cpu=4`. Those diagnostic timings are not included in
  the performance evidence above.
