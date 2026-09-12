# Distributed Cache Protocol

## 1. Protocol Overview

The first version of the cache will use an HTTP API with JSON request and response bodies.

A custom TCP protocol may be added later.

## 2. Supported Operations

The initial API supports:

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

* A value must initially be a string.
* A value cannot be larger than 1 MB.
* An empty string is allowed as a value.

### TTL

* TTL is optional.
* TTL is measured in seconds.
* TTL must be a positive integer.
* A key without a TTL does not automatically expire.
* Updating a key replaces its previous value and TTL.

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
200 OK
```

Body:

```json
{
  "status": "ok"
}
```

### Behavior

* If the key does not exist, a new entry is created.
* If the key already exists, its value is replaced.
* If a TTL is provided, the key expires after that number of seconds.
* Updating a key also replaces its old TTL.
* If no TTL is provided during an update, the new entry has no expiration.

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
  "key": "name",
  "value": "Ajit"
}
```

### Cache-miss response

Status:

```text
200 OK
```

Body:

```json
{
  "key": "name",
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
  "deleted": true
}
```

### Missing-key response

Status:

```text
200 OK
```

Body:

```json
{
  "deleted": false
}
```

Deleting a missing or expired key is not considered a server error.

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
  "message": "PONG"
}
```

## 8. Validation Errors

Validation errors return:

```text
400 Bad Request
```

### Empty key

```json
{
  "error": {
    "code": "INVALID_KEY",
    "message": "Key cannot be empty."
  }
}
```

### Key is too long

```json
{
  "error": {
    "code": "KEY_TOO_LARGE",
    "message": "Key cannot exceed 256 bytes."
  }
}
```

### Missing value

```json
{
  "error": {
    "code": "VALUE_REQUIRED",
    "message": "A value is required."
  }
}
```

### Value is too large

```json
{
  "error": {
    "code": "VALUE_TOO_LARGE",
    "message": "Value cannot exceed 1 MB."
  }
}
```

### Invalid TTL

```json
{
  "error": {
    "code": "INVALID_TTL",
    "message": "TTL must be a positive integer."
  }
}
```

### Invalid JSON

```json
{
  "error": {
    "code": "INVALID_REQUEST",
    "message": "The request body contains invalid JSON."
  }
}
```

## 9. Server Errors

Unexpected server errors return:

```text
500 Internal Server Error
```

Body:

```json
{
  "error": {
    "code": "INTERNAL_ERROR",
    "message": "The cache could not process the request."
  }
}
```

Internal details must be written to server logs and should not be exposed to the client.

## 10. Request and Response Summary

| Operation             | Success response                |
| --------------------- | ------------------------------- |
| `SET key value`       | `{"status":"ok"}`               |
| `GET existing-key`    | `{"key":"key","value":"value"}` |
| `GET missing-key`     | `{"key":"key","value":null}`    |
| `DELETE existing-key` | `{"deleted":true}`              |
| `DELETE missing-key`  | `{"deleted":false}`             |
| `PING`                | `{"message":"PONG"}`            |

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
