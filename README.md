# cache

A single-node, in-memory Go cache with an HTTP API, TTL expiration, LRU eviction,
and an append-only log for restart recovery.

Run from the repository directory:

```sh
make run
```

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `CACHE_PORT` | `8080` | HTTP port. |
| `CACHE_MAX_BYTES` | `67108864` (64 MiB) | Budget for stored key and value bytes. |
| `CACHE_DATA_DIR` | `./data` | Persistence directory, relative to the working directory. |

For example, choose another data directory with:

```sh
CACHE_DATA_DIR=./cache-data make run
```

Startup locks the data directory, opens `append.aof`, and restores its records
before accepting HTTP requests. SET, DELETE, and eviction mutations are written
and synchronized before their changes are applied to memory. Recovery preserves
the original expiration deadlines and does not write the replayed records again.

Stop with `Ctrl+C` to drain HTTP requests, then synchronize and close the log.
Restart with the same data directory and memory budget to restore saved entries
that have not expired. The default `data/` directory is gitignored.

The current implementation recovers unrotated logs and repairs an incomplete
final record by truncating to the last complete record and syncing the file.
Startup reports the repair offset and discarded byte count. Invalid headers,
corrupt records, and sequence errors still stop startup. Snapshots and log
rotation are remaining Phase 7 work. Durability details are specified in
[the persistence contract](docs/design.md#9-persistence).
