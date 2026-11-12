# Distributed Cache Design

## 1. Project Overview

This project is a distributed in-memory cache written in Go.

The cache will allow clients to store, retrieve, and delete key-value data. It will begin as a single-node cache and gradually support persistence, multiple nodes, sharding, replication, failure handling, monitoring, Docker, and Kubernetes.

## 2. Project Goals

The main goals are to:

* Provide fast in-memory data access.
* Handle multiple client requests concurrently.
* Prevent race conditions during concurrent operations.
* Support key expiration using TTL.
* Limit memory usage using an eviction policy.
* Recover data after a restart.
* Distribute data across multiple cache nodes.
* Replicate data for improved availability.
* Collect meaningful performance metrics.
* Run locally and in Kubernetes.

## 3. Initial Data Model

The first version will use:

* String keys
* String values
* An optional TTL in seconds

Example:

```text
Key: user:10
Value: Ajit
TTL: 60 seconds
```

The first version will store data in an in-memory Go map.

## 4. Initial Features

The first usable version will support:

* `SET`
* `GET`
* `DELETE`
* `PING`
* Optional TTL
* Thread-safe concurrent access

## 5. Cache Behavior

### Cache hit

A cache hit occurs when the requested key exists and has not expired.

```text
GET name
Result: Ajit
```

### Cache miss

A cache miss occurs when:

* The key does not exist.
* The key was deleted.
* The key has expired.

```text
GET age
Result: null
```

A cache miss is normal not as an error.

## 6. Expiration Behavior

A client may provide an optional TTL when storing a value.

```text
SET session abc123 TTL 60
```

The key will expire 60 seconds after it is stored.

After expiration:

* `GET` returns `null`.
* The expired value is treated as missing.
* The expired entry will eventually be removed from memory.

The cache will use:

* Lazy expiration when a key is accessed.
* Active expiration through a background cleanup process.

## 7. Memory Management

The cache will have a configurable memory limit.

When the cache reaches that limit, it will remove entries using the Least Recently Used, or LRU, eviction policy.

LRU removes the value that has gone unused for the longest time.

LFU may be added later as an optional eviction policy.

## 8. Concurrency

The cache must handle multiple requests at the same time.

The implementation will use:

* Goroutines for concurrent request handling.
* `sync.RWMutex` to protect shared cache data.
* Go's race detector to detect unsafe concurrent access.

No operation should read or modify the shared map without the required lock.

## 9. Persistence

This section defines the Phase 7 persistence contract. The server now writes an AOF, repairs incomplete final records, and restores unrotated logs before serving requests. Snapshots and log rotation remain to implement.

The in-memory store remains the normal read path. An append-only file (AOF) records mutations, periodic snapshots capture the live cache, and startup recovery loads a snapshot followed by newer log records. Persistence preserves the outcome of cache operations; later updates, deletes, expiration, and eviction can still remove previously acknowledged values.

### 9.1 Scope, configuration, and file ownership

The first persistence implementation supports one process and one local data directory on macOS or Linux. It requires file synchronization, directory synchronization, and atomic replacement within the same directory. Unsupported filesystem behavior must produce an error rather than silently weakening the contract.

| Setting | Initial policy |
| --- | --- |
| `CACHE_DATA_DIR` | Defaults to `./data`, relative to the working directory. |
| `CACHE_SNAPSHOT_INTERVAL` | Defaults to `1m`; accepts a positive Go duration. |
| Write synchronization | Fixed to `always`: sync each mutation before acknowledging it. |
| `CACHE_MAX_BYTES` | Keeps its existing meaning: the budget counts key and value bytes. |

The data directory contains:

```text
data/
  LOCK             # Exclusive process lock, held until persistence closes.
  append.aof       # Active mutation log.
  snapshot.bin     # Latest published snapshot; absent before the first snapshot.
```

Temporary snapshot and replacement-log files are created in this directory. They are never recovery inputs and are never automatically promoted at startup. The `data/` directory is already gitignored.

Only the process holding the exclusive lock may inspect for recovery or modify these files. A second server using the same directory must fail startup. Newly created directories, file contents, and directory entries must be synchronized before the server accepts durable writes.

The persisted memory budget must match `CACHE_MAX_BYTES` on restart. Version 1 rejects a mismatch instead of silently evicting recovered data or migrating the files. Changing the budget of an existing data directory requires a future migration mechanism.

### 9.2 Mutation ordering and acknowledgment

Every state-changing client `SET` or `DELETE` has one sequence number and one log record. Sequences are unsigned 64-bit integers, start at 1, and increase by exactly 1 for each record. Rotation preserves the sequence; exhaustion must stop writes instead of wrapping to zero.

The operation order is:

```text
Validate input and prepare the complete mutation, including eviction victims
    -> Encode and validate the record size
    -> Append the complete record
    -> Sync the AOF
    -> Apply the prepared mutation to memory
    -> Return HTTP success
```

Eviction planning, logging, and applying the mutation must share one coordinated critical section. Concurrent requests, LRU-changing reads, expiration cleanup, and snapshot work must not invalidate the prepared mutation. No intermediate batch state may be visible to a reader.

* A successful `PUT` returns the existing `201 Created` response only after the record has been synchronized and applied.
* A successful `DELETE` returns the existing `200 OK` response only after its removal has been synchronized and applied. Its internal API must be able to return a persistence error, rather than only a boolean.
* Invalid input, an entry exceeding the memory budget, or a record exceeding the format limit is rejected before append and before any eviction or memory change. An oversized record returns `413 Request Entity Too Large`.
* A `DELETE` that finds no stored entry remains `404 Not Found` and creates no log record. An expired entry still physically present may be deleted and logged, preserving the current store's deletion behavior.
* `GET` and `PING` create no log records. `GET` still updates live LRU order.
* Expiration cleanup requires no delete records because the original absolute deadline already represents expiration durably.

An append, sync, or persistence publication failure returns a server error for the affected request where applicable, leaves its prepared mutation unapplied, and places persistence in a failed state. Further client mutations and snapshot work are rejected until restart and recovery. Reads may continue against the last applied in-memory state. Errors must be reported with their operation and file context.

A failed, interrupted, or unanswered request has an **unknown durability outcome** once append has begun: a complete record may reach storage even when the client receives no success. Recovery may apply that record. The API does not provide request deduplication or an exactly-once retry guarantee.

### 9.3 Durability guarantee and limits

An acknowledged mutation is recoverable after process termination, provided the persistence files remain available and valid. Recovery preserves its logical effect in sequence order, subject to subsequent mutations and expiration.

The implementation uses Go's [`File.Sync`](https://pkg.go.dev/os#File.Sync) to request stable storage. File creation and replacement also require synchronization of the containing directory; syncing a file alone does not make its directory entry durable, as described by the [Linux `fsync` documentation](https://man7.org/linux/man-pages/man2/fsync.2.html).

Machine-crash and power-loss durability depend on the operating system, filesystem, and storage device honoring these synchronization requests. The contract does not promise recovery from lost files, disk failure, or undetected corruption. CRC checks detect accidental corruption with high probability; they cannot repair it or authenticate data.

### 9.4 Version 1 AOF format

The format uses a fixed binary file header followed by length-framed UTF-8 JSON records. Binary integers use big-endian byte order. All checksums use the IEEE CRC-32 algorithm exposed by Go's [`crc32.ChecksumIEEE`](https://pkg.go.dev/hash/crc32#ChecksumIEEE), calculated over the exact stored bytes.

The 30-byte file header is:

| Field | Size | Meaning |
| --- | --- | --- |
| Magic | 8 bytes | ASCII `CACHEAOF`. |
| Version | `uint16` | `1`. Applies to every record in this file. |
| Base sequence | `uint64` | Last sequence retired by rotation; `0` for a new log. |
| Memory budget | `uint64` | Original positive `CACHE_MAX_BYTES`, within signed 64-bit range. |
| Header checksum | `uint32` | CRC-32 of the preceding 26 header bytes. |

Each record is framed as:

```text
payload_length: uint32
length_checksum: uint32       # CRC-32 of the 4 encoded payload_length bytes.
payload: payload_length bytes # One UTF-8 JSON object.
record_checksum: uint32       # CRC-32 of encoded payload_length followed by payload.
```

The length checksum must be verified before using the length. This prevents an accidentally changed length from being treated as an ordinary incomplete payload. Payload length must be between 1 byte and 64 MiB inclusive and must be checked before allocating a payload buffer. This limit applies to the entire encoded mutation batch, including JSON escaping and eviction removals; it is separate from the cache's key/value byte budget.

The JSON payload has exactly two fields: `sequence` and a nonempty ordered `mutations` array. A normal `SET` record looks like:

```json
{
  "sequence": 1,
  "mutations": [
    {"op": "SET", "key": "name", "value": "Ajit", "expires_at_unix_nano": null}
  ]
}
```

Every mutation requires `op` and `key`. `SET` additionally requires `value` and `expires_at_unix_nano`; `DELETE` carries neither of those fields. Keys and values are valid UTF-8 JSON strings: keys contain 1 to 256 bytes, and values contain at most 1 MiB; an empty value is valid. An expiration is either `null` for no deadline or a signed 64-bit Unix timestamp in nanoseconds. Sequences are decoded directly as unsigned 64-bit integers, without a floating-point conversion. Field names are case-sensitive. Unknown or duplicate fields, unsupported operations, invalid field types, missing required fields, or trailing payload data are format errors.

An explicit client deletion contains one `DELETE`. A `SET` that evicts entries contains their `DELETE` mutations first, in eviction order, followed by the `SET`, all inside one checksummed record with one sequence number. Recovery validates the entire record before applying any of it. A truncated batch applies none of its mutations.

The first record follows the header's base sequence, and every subsequent record follows its predecessor. Gaps, duplicate sequences, and sequence overflow are errors. Unknown format versions must fail recovery; version 1 does not attempt automatic format conversion.

The codecs are implemented in [`internal/persistence/header.go`](../internal/persistence/header.go) and [`internal/persistence/record.go`](../internal/persistence/record.go). `EncodeHeader` and `EncodeRecord` return complete byte slices. `DecodeHeader` and `DecodeRecord` consume exactly one header or record from an `io.Reader`; they perform no file writes or cache mutations. Log writers and recovery use `Record.ValidateAfter` with the header's base sequence or the previous record's sequence to enforce continuity across records.

`DecodeRecord` returns `io.EOF` only at a clean record boundary. An incomplete frame returns `ErrTruncatedRecord`; malformed payloads, oversized records, invalid sequences, unsupported versions, and checksum mismatches have separate errors that callers can identify with `errors.Is`. A decoder returns no partial record on failure. File paths and offsets are added by the log reader during recovery.

### 9.5 TTL and eviction recovery

A TTL is converted to an absolute UTC expiration deadline once, while preparing its original write. Conversion must reject duration or timestamp overflow. Disk latency and server downtime do not extend the deadline. Replacing a value also replaces its deadline; a `SET` without TTL stores `null` and clears any earlier expiration.

For example, a value stored at `10:00:00` with a five-second TTL expires at `10:00:05`. Starting the server at `10:00:10` must leave that key absent.

An expired `SET` encountered during replay still supersedes the previous value of the same key: recovery removes that key rather than skipping the operation and exposing an older value. Any eviction removals in the same record still apply. Once replay completes, entries expired at the recovery cutoff are removed; normal access-time expiration resumes before requests are served.

Recovery applies recorded eviction decisions directly and does not call normal write methods that could evict different keys or append new records. It rebuilds the map, linked list, and key/value byte accounting. A reconstructed state exceeding the recorded budget is an error.

Snapshots preserve LRU order at their checkpoint. Reads after that checkpoint are not logged, so their exact recency order is not restored. Replay makes later `SET` entries most recently used and honors recorded removal victims. Recovered membership is preserved, while the next eviction decision may differ from the decision an uninterrupted process would have made.

### 9.6 Startup and damaged-file policy

Recovery finishes before the HTTP listener or expiration and snapshot workers start:

1. Acquire exclusive ownership of the data directory.
2. Validate published file headers, versions, checksums, and the configured memory budget.
3. Load and validate the published snapshot, if present, including its checkpoint `N`.
4. Scan the AOF in order, validating even records already covered by the snapshot. Apply only sequences greater than `N`.
5. Repair an allowed incomplete final record, remove expired entries, and rebuild cache bookkeeping.
6. Open the valid AOF for append, then start serving requests and running background workers.

| Condition | Required behavior |
| --- | --- |
| Neither published file exists | Initialize a new, durably created AOF with base sequence `0`. |
| AOF exists without a snapshot | Recover only if its base sequence is `0`. |
| Snapshot exists without an AOF | Fail startup; do not assume that later mutations never existed. |
| AOF base sequence exceeds the snapshot checkpoint | Fail startup because required history is missing. |
| EOF exactly between complete records | Normal end of log. |
| EOF within the final record header, payload, or trailing checksum | Report its offset and discarded byte count, recover the valid prefix, truncate to the last complete record, and sync the repair before appending. |
| Invalid header, checksum mismatch anywhere, invalid length, malformed record, or sequence error | Fail startup with file and offset context. Do not skip records or modify the published files. |
| Incomplete or corrupt published snapshot | Fail startup. Retired log history may make fallback unsafe. |
| Temporary files left by an interrupted operation | Ignore them as recovery inputs. Never substitute them for a missing published file. |

A wholly or partly missing file header is an error, including an empty existing AOF. The incomplete-tail exception applies only after a valid AOF header. A fully present but invalid final record is corruption, not an allowed truncation. No corruption policy can reconstruct bytes that have been lost from acknowledged records.

### 9.7 Snapshot format, publication, and log rotation

A snapshot contains its format version, the memory budget, a sequence checkpoint, and each live key's value and absolute deadline in least-to-most-recent LRU order. A snapshot is accepted only as a whole; it has no incomplete-tail recovery policy.

#### Version 1 snapshot format

The format constants and data types are defined in [`internal/persistence/snapshot.go`](../internal/persistence/snapshot.go). This step defines the layout; encoding, decoding, capture, publication, and startup loading remain to implement. The snapshot version is independent of the AOF version. Binary integers use big-endian byte order, and every checksum uses IEEE CRC-32.

The file contains one fixed header, exactly `EntryCount` entry frames, and one file checksum:

```text
[38-byte header][entry 1]...[entry EntryCount][4-byte file checksum][EOF]
```

The header layout is:

| Offset | Field | Size | Meaning |
| --- | --- | --- | --- |
| 0 | Magic | 8 bytes | ASCII `CACHESNP`. |
| 8 | Version | `uint16` | `1`, identified by `SnapshotFormatVersion`. |
| 10 | Checkpoint | `uint64` | All mutations through sequence `N` are reflected in the saved state. |
| 18 | Memory budget | `uint64` | Positive key/value byte budget, within signed 64-bit range. |
| 26 | Entry count | `uint64` | Number of unique saved entries, in LRU order. |
| 34 | Header checksum | `uint32` | CRC-32 of the preceding 34 header bytes. |

Checkpoint `0` is allowed for an initial empty cache and requires an entry count of `0`. A later checkpoint can also have no entries after deletions or expiration. Checkpoints count AOF records, not the number of keys. The memory budget must match both the configured cache limit and the AOF header during startup.

Each entry uses the same framing pattern as an AOF record, with its own payload:

```text
payload_length: uint32
length_checksum: uint32       # CRC-32 of the 4 encoded payload_length bytes.
payload: payload_length bytes # One UTF-8 JSON object describing one entry.
entry_checksum: uint32        # CRC-32 of encoded payload_length followed by payload.
```

An entry payload has exactly these three case-sensitive fields:

```json
{"key":"name","value":"Amit","expires_at_unix_nano":null}
```

There is no operation or per-entry sequence: entries describe the current state, and the header provides one checkpoint for the entire snapshot. The expiry field is required and contains either `null` or an integer within the signed 64-bit Unix-nanosecond range. It preserves the original deadline without restarting the TTL. Entry order is least recently used first and most recently used last; entries must not be sorted by key.

The following rules apply to both encoding and decoding:

* The magic, version, complete header, and header checksum must be valid before trusting metadata.
* Verify each length checksum before using its payload length. Payload lengths must be between 1 byte and `MaxSnapshotEntryPayloadSize` (8 MiB), inclusive, before allocating a payload buffer. This allows JSON escaping of keys up to 256 bytes and values up to 1 MiB.
* Keys must contain 1 to 256 valid UTF-8 bytes; values contain at most 1 MiB of valid UTF-8 bytes and may be empty. Duplicate keys, missing or unknown fields, duplicate fields, invalid types, and trailing JSON values are errors.
* Entry count must equal the number of encoded entries and cannot exceed the memory budget, since every key uses at least one byte. A decoder must read incrementally rather than preallocate from an untrusted entry count.
* The sum of key and value bytes across all entries must not exceed the saved budget. Check this without integer overflow and validate every entry, including entries whose deadlines have passed since capture.
* After exactly `EntryCount` complete frames, require a 4-byte file checksum equal to CRC-32 of all preceding file bytes, including the header checksum and every entry frame. This protects entry order as well as contents. No bytes may follow the file checksum, including whitespace.
* Any incomplete header, frame, or footer, checksum mismatch, invalid metadata, or other format error rejects the entire snapshot. No partially decoded state may reach the cache.

Capture includes only entries live at one cutoff time. Entries may expire after capture; restore filters them only after the complete snapshot validates. An empty snapshot is a valid 42-byte header-plus-footer file. For example, a checkpoint of `4` after setting `name` to `Ajit`, setting `city` to `Pune`, replacing `name` with `Amit`, and deleting `city` contains only `name = Amit`.

#### Publication and rotation

The first implementation pauses cache operations during capture, publication, and rotation. This keeps LRU order, expiration cleanup, and mutation boundaries consistent without a second concurrent log. The cost is a request pause during snapshot work; reducing that pause is future work.

With the cache paused at sequence `N`:

1. Capture live entries and checkpoint `N`.
2. Write a complete temporary snapshot in the data directory, sync it, and close it successfully.
3. Atomically rename it to `snapshot.bin`, then sync the directory. Do not alter the active AOF before this succeeds.
4. Write and sync a temporary replacement AOF containing a valid header with base sequence `N` and the same memory budget. Close it successfully.
5. Atomically rename it to `append.aof`, then sync the directory. Switch the append handle to the published replacement before resuming operations.
6. Resume cache operations; the next mutation uses sequence `N + 1`.

The old AOF remains available until snapshot publication is durable. A crash before publication leaves the old snapshot and full log usable. A crash after snapshot publication but before AOF replacement leaves the new snapshot and old log usable; replay skips sequences at or below `N`. After replacement, the new snapshot and AOF base agree at `N`. No mutation can arrive between capture and rotation because operations remain paused.

Do not truncate the active AOF in place as a rotation strategy. Periodic snapshots must not overlap. Publication or rotation errors enter the failed persistence state described above; do not continue appending through a handle that may refer to a replaced file.

### 9.8 Shutdown and implementation boundaries

Shutdown stops accepting requests, cancels background scheduling, waits for in-flight requests and snapshot work to finish, then syncs and closes the active log and releases the directory lock. Persistence must not close while a request or worker can still use it. Shutdown errors must be reported. A final snapshot is optional for recovery because every acknowledged mutation is already in a synchronized log or a safely published snapshot.

Persistence must provide the cache with a way to append one prepared mutation batch and propagate errors. The cache must provide snapshot capture and recovery application paths that preserve absolute deadlines and bookkeeping without recursively logging operations. `DELETE` error propagation requires updating the existing server `Cache` interface and handler.

This phase does not include replication, remote backups, request deduplication, alternative sync policies, automatic memory-budget migration, or exact restoration of read-driven LRU order. Those require separate contracts.

### 9.9 Append-only log implementation

The writer is implemented in [`internal/persistence/aof.go`](../internal/persistence/aof.go). Application startup in [`cmd/cache-server/startup.go`](../cmd/cache-server/startup.go) opens it, creates the cache with `NewStoreWithLog`, and replays the saved records before HTTP requests can reach the cache. Startup defaults `CACHE_DATA_DIR` to `./data`.

* `OpenAOF(dataDir, maxBytes)` creates missing directories, acquires the process lock, and creates a new log with a synchronized header or validates an existing log. It checks the stored budget, record checksums, and sequence continuity before permitting appends. An incomplete final record is truncated to the last valid boundary and the repair is synchronized first. Newly created directories and the log's directory entry are synchronized as well.
* `Header()` returns the log metadata. `LastSequence()` returns the last sequence confirmed by validation or a successful append and sync. The caller supplies the next sequence and coordinates cache mutation ordering.
* `Append(record)` serializes access, checks the next sequence, encodes the record, writes its complete frame, and syncs the log before returning success. Invalid records leave the writer usable. A short write or write/sync error preserves its cause, marks the writer failed, and prevents further appends.
* `Replay(apply)` reads a fixed prefix of the opened log, validates record sequences, and calls the supplied apply function for each complete record. Positional reads preserve the append handle's offset. The AOF mutex is released before callbacks acquire cache locks. Replay is a startup operation; a failed recovery cache must be discarded rather than served.
* `Sync()` explicitly synchronizes a healthy writer; each append already includes a sync. It cannot clear an earlier failure.
* `Close()` waits for active writer operations, prevents further appends, attempts sync and all cleanup steps, and releases the directory lock last. Repeated closes return the same result. Prior I/O failures and cleanup errors remain reportable through `errors.Is`.

At this implementation stage, opening supports fresh logs and unrotated logs without snapshots. `scanAOF` reports the validated header, last complete sequence, valid byte count, and any incomplete final byte count without changing the file. The repair helper in [`internal/persistence/tail.go`](../internal/persistence/tail.go) truncates only that incomplete tail, syncs the repair, and logs its offset and discarded byte count. A truncation or sync failure prevents startup. Replay then reads only the complete prefix, and the next append follows its last sequence.

An existing snapshot or a nonzero base sequence still returns `ErrRecoveryRequired`; snapshot validation remains to implement. Invalid or incomplete file headers, corrupt records, invalid lengths, sequence errors, and budget mismatches prevent opening without modifying the file. Error messages include the file and record offset where relevant.

Directory locking is implemented for macOS and Linux. Other platforms return `ErrUnsupportedPlatform`. The lock file is kept in place after close so another process cannot acquire ownership through a different lock-file inode while an existing owner still holds its lock.

`Store.ApplyRecoveredRecord` applies recorded DELETEs and SETs without appending records or selecting new eviction victims. It checks the batch's projected memory budget before changing state. An expired SET removes any previous value of the same key. Startup removes entries that expired during replay, then starts HTTP and expiration cleanup. Shutdown drains HTTP requests before closing the writer; returned startup, listener, and shutdown errors pass through cleanup before the process exits.

## 10. Distributed Architecture

Later versions will run multiple cache nodes.

The distributed version will include:

* Consistent hashing
* Virtual nodes
* Data sharding
* Replication
* Node health checks
* Failure handling
* Cluster membership
* Key redistribution when nodes join or leave

## 11. Scalability

The system will scale in two ways.

### Vertical scaling

One cache node will handle many concurrent requests using goroutines.

### Horizontal scaling

Additional cache nodes will increase:

* Total memory capacity
* Request throughput
* Availability

Consistent hashing will decide which node owns each key.

## 12. Initial Guarantees

The initial single-node version will guarantee:

* Thread-safe cache operations
* Expired keys are not returned
* Missing keys return `null`
* Basic input validation
* Bounded memory after eviction is implemented
* Predictable command responses

## 13. Initial Non-Goals

The first version will not provide:

* Permanent database storage
* Multiple cache nodes
* Data replication
* Automatic failover
* Strong consistency across nodes
* User authentication
* Data encryption
* Kubernetes deployment

These capabilities will be added in later phases.

## 14. Performance Metrics

The completed system should measure:

* Total requests
* Requests per second
* Cache hits
* Cache misses
* Cache-hit ratio
* Response latency
* Memory usage
* Number of stored keys
* Expired keys
* Evicted keys
* Failed requests
* Unavailable nodes

Latency results should include at least p50, p95, and p99 values.

## 15. Planned Development Order

1. Define the project contract.
2. Create the Go project structure.
3. Build the single-node cache engine.
4. Add the HTTP API.
5. Add concurrency safety.
6. Add TTL expiration.
7. Add memory limits and LRU eviction.
8. Add persistence and recovery.
9. Add metrics and benchmarks.
10. Run multiple cache nodes.
11. Add consistent-hash sharding.
12. Add replication and failure handling.
13. Containerize with Docker.
14. Deploy to Kubernetes.
15. Test, document, and present the system.

## 16. Definition of Success

The project is successful when another developer can:

* Understand the architecture.
* Run the system locally.
* Send cache commands.
* Run the automated tests.
* Deploy the cache.
* Observe its performance.
* Understand its guarantees and limitations.
