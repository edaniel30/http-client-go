# Request Options

Per-request configuration that overrides client-level defaults for individual HTTP requests.

## Quick Start

```go
resp, _ := client.Get(ctx, url,
    httpclient.WithTraceID("trace-abc-123"),
    httpclient.WithSkipLog(),
    httpclient.WithObfuscatedHeaders("Authorization"),
    httpclient.WithTags(map[string]string{"operation": "charge"}),
    httpclient.WithRequestHeaders(extraHeaders),
)
```

## Available Options

### WithTraceID

Adds an `X-Trace-Id` header for distributed tracing correlation:

```go
resp, _ := client.Get(ctx, url, httpclient.WithTraceID("trace-abc-123"))
```

The trace ID is sent as a header and can be correlated across services.

### WithSkipLog

Disables all logging for this request (start, end, error, retry):

```go
resp, _ := client.Get(ctx, healthCheckURL, httpclient.WithSkipLog())
```

Use for high-frequency requests like health checks that would pollute logs.

### WithTags

Adds custom key-value fields to all log entries for this request:

```go
resp, _ := client.Post(ctx, url, "application/json", body,
    httpclient.WithTags(map[string]string{
        "provider":  "stripe",
        "operation": "charge",
        "merchant":  "merch_123",
    }),
)
```

Tags appear in both request start (`Debug`) and request completed/failed (`Info`/`Error`) log entries.

### WithObfuscatedHeaders

Masks header values in logs while sending real values in the request:

```go
resp, _ := client.Get(ctx, url,
    httpclient.WithObfuscatedHeaders("Authorization", "X-Api-Key", "X-Secret"),
)
```

- Real value is **sent** to the server
- Logged value appears as `********`
- Matching is **case-insensitive**
- Applies to both request and response headers in logs

### WithRequestHeaders

Adds extra headers for this specific request only:

```go
headers := http.Header{}
headers.Set("X-Idempotency-Key", uuid.New().String())
headers.Set("X-Request-Source", "batch-job")

resp, _ := client.Post(ctx, url, "application/json", body,
    httpclient.WithRequestHeaders(headers),
)
```

These headers override any client-level default headers with the same name.

## Combining Options

Options compose naturally:

```go
resp, _ := client.Post(ctx, url, "application/json", body,
    httpclient.WithTraceID(traceID),
    httpclient.WithObfuscatedHeaders("Authorization"),
    httpclient.WithTags(map[string]string{
        "provider": "adyen",
        "flow":     "checkout",
    }),
    httpclient.WithRequestHeaders(idempotencyHeaders),
)
```

## Scope

Request options apply only to the single request they are passed to. They do not affect the client or other requests:

```go
// This request has a trace ID and skips logging
client.Get(ctx, url, httpclient.WithTraceID("abc"), httpclient.WithSkipLog())

// This request has default behavior (no trace ID, logging enabled)
client.Get(ctx, url)
```

## Application Order

```
1. Client-level default headers
2. Per-request trace ID (X-Trace-Id header)
3. Per-request extra headers (WithRequestHeaders)
4. Hooks execute (OnRequestStart)
5. Logging (with obfuscation and tags applied)
6. HTTP request sent
```
