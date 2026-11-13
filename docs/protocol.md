# Distributed Cache Protocol

## 1. Protocol Overview

The cache uses an HTTP API with JSON request and response bodies.

A custom TCP protocol may be added later.

## 2. Supported Operations

The API supports:

* `SET`
* `GET`
* `DELETE`
* `PING`

## 3. Data Rules

### Keys

* A key must be a string.
* A key cannot be empty.
* A key cannot be longer than 256 bytes.
* Keys are case-sensitive.

Therefore, these are different keys:

```text
name
Name
NAME
```

### Values

* Values are strings.
* A value cannot be larger than 1 MiB (1,048,576 bytes).
* An empty string is allowed as a value.
* The current JSON decoder treats an omitted or `null` value as an empty string.

### TTL

* TTL is optional.
* TTL is measured in seconds.
* TTL must be a positive integer.
* A key without a TTL does not automatically expire.
* Updating a key replaces its previous value and TTL.
* Expiration deadlines are preserved across snapshots and restarts; downtime does not restart a TTL.

The current handler validates positive `int64` seconds but does not explicitly check overflow when converting seconds to Go's `time.Duration`. Upper-bound validation for that conversion remains outstanding.

## 4. SET Operation

### Purpose

Store a new key-value pair or update an existing key.

### Request

```http
PUT /v1/cache/{key}
Content-Type: application/json
```

Request body without TTL:

```json
{
  "value": "Ajit"
}
```

Request body with TTL:

```json
{
  "value": "Ajit",
  "ttl_seconds": 60
}
```

### Successful response

Status:

```text
201 Created
```

Body:

```json
{
  "status": "stored"
}
```

### Behavior

* If the key does not exist, a new entry is created.
* If the key already exists, its value is replaced.
* If a TTL is provided, the key expires after that number of seconds.
* Updating a key also replaces its old TTL.
* If no TTL is provided during an update, the new entry has no expiration.
* The cache counts the bytes in each key and value toward its memory budget.
* When a write needs space, the cache evicts the least recently used entries until it fits. Successful reads and writes make an entry most recently used.
* If the key and value together exceed the entire budget, the write returns `413 Request Entity Too Large`. The cache preserves existing entries, including the old value and TTL when an update is rejected.
* The complete mutation batch, including eviction DELETEs, is written and synced to the AOF before cache memory changes and success is returned.
* Snapshot capture, publication, and AOF rotation pause cache operations. A write is acknowledged after its own AOF sync and memory update; it does not wait for the next scheduled snapshot.

### Entry exceeds the cache memory budget

```json
{
  "error": "entry exceeds cache memory budget"
}
```

## 5. GET Operation

### Purpose

Retrieve the value stored under a key.

### Request

```http
GET /v1/cache/{key}
```

### Cache-hit response

Status:

```text
200 OK
```

Body:

```json
{
  "value": "Ajit"
}
```

### Cache-miss response

Status:

```text
404 Not Found
```

Body:

```json
{
  "value": null
}
```

The cache returns `null` when:

* The key does not exist.
* The key was deleted.
* The key has expired.

A missing key is a normal cache miss, not a server error.

## 6. DELETE Operation

### Purpose

Remove a key and its value.

### Request

```http
DELETE /v1/cache/{key}
```

### Existing-key response

Status:

```text
200 OK
```

Body:

```json
{
  "status": "deleted"
}
```

### Missing-key response

Status:

```text
404 Not Found
```

Body:

```json
{
  "error": "key not found"
}
```

A missing key returns `404` and creates no AOF record. If an expired entry is still physically present before lazy or background cleanup removes it, DELETE can remove and log it, returning `200`. A successful deletion is synced to the AOF before removal from memory and acknowledgment.

## 7. PING Operation

### Purpose

Check whether the cache server is running.

### Request

```http
GET /v1/ping
```

### Response

Status:

```text
200 OK
```

Body:

```json
{
  "status": "ok"
}
```

## 8. Validation Errors

Validation errors return:

```text
400 Bad Request
```

The response uses a string-valued `error` field, for example:

```json
{
  "error": "invalid JSON"
}
```

| Condition | Error message |
| --- | --- |
| Key exceeds 256 bytes | `key exceeds maximum length` |
| Value exceeds 1 MiB | `value exceeds maximum size` |
| TTL is zero or negative | `ttl must be greater than zero` |
| Invalid JSON or incompatible field types | `invalid JSON` |

A URL without the required key segment may be rejected by the HTTP router before reaching a cache handler. Missing or `null` values currently become empty strings instead of producing a validation error.

## 9. Server Errors

Cache-operation failures return:

```text
500 Internal Server Error
```

DELETE logs the underlying error and returns:

```json
{
  "error": "failed to delete key"
}
```

SET currently returns the underlying cache or persistence error text in the same string-valued `error` field. A key/value pair exceeding the cache budget returns `413` instead; exceeding the persistence record-size limit currently returns `500`.

After an append or sync failure, further mutations are rejected until restart. A failed or unanswered write can still leave a complete record on storage, so its final outcome is unknown; clients do not receive an exactly-once retry guarantee. See the [persistence design](design.md#9-persistence) for details.

Periodic snapshots run at `CACHE_SNAPSHOT_INTERVAL`, defaulting to `1m`. Snapshot publication or rotation failure stops the worker and initiates application shutdown. There is no separate HTTP endpoint for saving a snapshot.

## 10. Request and Response Summary

| Operation | HTTP status | Response |
| --- | --- | --- |
| `SET key value` | `201 Created` | `{"status":"stored"}` |
| `GET existing-key` | `200 OK` | `{"value":"value"}` |
| `GET missing-key` | `404 Not Found` | `{"value":null}` |
| `DELETE existing-key` | `200 OK` | `{"status":"deleted"}` |
| `DELETE missing-key` | `404 Not Found` | `{"error":"key not found"}` |
| `PING` | `200 OK` | `{"status":"ok"}` |

## 11. Future Protocol Features

Later versions may add:

* A custom TCP protocol
* Batch `GET` and `SET`
* Compare-and-set
* Numeric increment and decrement
* Authentication
* Compression
* Client-side consistent hashing
* Cluster redirection
* Request IDs
* Entry version numbers

These features are not part of the initial protocol.
