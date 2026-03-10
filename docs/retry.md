# Retry

Automatic retry with exponential backoff and jitter for transient failures.

## Quick Start

```go
client, _ := httpclient.New(
    httpclient.DefaultConfig(),
    httpclient.WithRetry(3, 100*time.Millisecond, 5*time.Second),
)
```

**Disabled by default** - must be explicitly enabled.

## How It Works

```
Request fails (5xx or network error)
  ↓
Wait: minBackoff * 2^attempt + jitter
  ↓
Retry request
  ↓
Success? → Return response
Still failing? → Retry again (up to maxRetries)
  ↓
All retries exhausted → Return last response/error
```

## Configuration

| Parameter | Description | Example |
|---|---|---|
| `maxRetries` | Maximum number of retry attempts | `3` |
| `minBackoff` | Minimum delay before first retry | `100ms` |
| `maxBackoff` | Maximum delay cap | `5s` |

```go
httpclient.WithRetry(
    3,                      // maxRetries
    100 * time.Millisecond, // minBackoff
    5 * time.Second,        // maxBackoff
)
```

## Retry Conditions

The client retries automatically on:

| Condition | Retried | Reason |
|---|---|---|
| Network error | Yes | Transient connectivity issue |
| Timeout | Yes | Temporary overload |
| `429 Too Many Requests` | Yes | Rate limiting |
| `500 Internal Server Error` | Yes | Server-side failure |
| `502 Bad Gateway` | Yes | Proxy failure |
| `503 Service Unavailable` | Yes | Service overloaded |
| `504 Gateway Timeout` | Yes | Upstream timeout |
| `400 Bad Request` | No | Client error, won't change |
| `401 Unauthorized` | No | Auth issue, won't change |
| `403 Forbidden` | No | Permission issue |
| `404 Not Found` | No | Resource doesn't exist |

## Backoff Strategy

Exponential backoff with jitter prevents thundering herd:

```
Attempt 0: minBackoff * 2^0 + jitter  →  ~100ms
Attempt 1: minBackoff * 2^1 + jitter  →  ~200ms
Attempt 2: minBackoff * 2^2 + jitter  →  ~400ms
...capped at maxBackoff
```

Jitter adds randomness between 50%-100% of the calculated delay to spread out retries.

## Context Cancellation

Retries respect context cancellation:

```go
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
defer cancel()

// If context expires during backoff wait, returns immediately
resp, err := client.Get(ctx, url)
```

## Logging

When a logger is configured, retry attempts are logged at `Warn` level:

```json
{
  "level": "warn",
  "msg": "HTTP request retrying",
  "method": "GET",
  "url": "https://api.example.com/users",
  "attempt": 1,
  "backoff": "150ms"
}
```

Use `WithSkipLog()` per-request to suppress retry logs.

## Exhausted Retries

When all retries are exhausted:
- **Server responded**: Returns the last `*http.Response` (e.g., 503)
- **Network error**: Returns a `*RequestError` wrapping the last error

```go
resp, err := client.Get(ctx, url)
if err != nil {
    // Network-level failure after all retries
    var reqErr *httpclient.RequestError
    errors.As(err, &reqErr)
}

if resp.StatusCode >= 500 {
    // Server returned 5xx on every attempt
}
```

## Common Configurations

### Conservative (API calls)

```go
httpclient.WithRetry(3, 500*time.Millisecond, 10*time.Second)
```

Use when: Calling external APIs where failures are expected.

### Aggressive (Internal services)

```go
httpclient.WithRetry(5, 50*time.Millisecond, 2*time.Second)
```

Use when: Calling internal microservices with fast recovery.

### Minimal (Health checks)

```go
httpclient.WithRetry(1, 100*time.Millisecond, 100*time.Millisecond)
```

Use when: Simple retry for transient glitches.

## Validation Rules

| Rule | Error |
|---|---|
| `maxRetries < 0` | `retry.maxRetries must be non-negative` |
| `minBackoff <= 0` | `retry.minBackoff must be positive` |
| `maxBackoff <= 0` | `retry.maxBackoff must be positive` |
| `minBackoff > maxBackoff` | `retry.minBackoff must be less than or equal to maxBackoff` |
