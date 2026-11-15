# Performance evidence

## Phase 8 Task 2: uninstrumented engine baseline

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
| Source state | Clean before starting these tasks; only documentation and benchmark evidence changed during the run. Go sources and benchmarks were unchanged. |
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

Measure HTTP workloads and persistence-enabled workloads separately in their
later tasks. Preserve the current sync-before-success durability contract in
server comparisons and run HTTP tests long enough to include snapshot activity.
