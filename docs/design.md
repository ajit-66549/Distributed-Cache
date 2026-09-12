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

The in-memory map will remain the primary storage.

Persistence will be added later using:

* An append-only log
* Periodic snapshots
* Startup recovery

After a restart, the cache will load its latest snapshot and replay newer log entries.

Disk persistence is for recovery. It is not the normal read path.

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
