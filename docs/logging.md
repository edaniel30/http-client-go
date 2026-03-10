# Logging

Structured request/response logging with header obfuscation.

## Quick Start

```go
client, _ := httpclient.New(
    httpclient.DefaultConfig(),
    httpclient.WithLogger(myLogger),
)
```

**Disabled by default** - requires a logger implementation.

## Logger Interface

```go
type Logger interface {
    Info(ctx context.Context, msg string, fields map[string]any)
    Error(ctx context.Context, msg string, fields map[string]any)
    Warn(ctx context.Context, msg string, fields map[string]any)
    Debug(ctx context.Context, msg string, fields map[string]any)
    Close() error
}
```

Compatible with [loki-logger-go](https://github.com/edaniel30/loki-logger-go) out of the box.

## What Gets Logged

### Request Start (Debug level)

```json
{
  "msg": "HTTP request started",
  "method": "POST",
  "url": "https://api.example.com/payments",
  "request_headers": {
    "Content-Type": "application/json",
    "Authorization": "********"
  },
  "request_body": "{\"amount\":100}"
}
```

### Request Completed (Info level)

```json
{
  "msg": "HTTP request completed",
  "method": "POST",
  "url": "https://api.example.com/payments",
  "status": 200,
  "duration_ms": 45,
  "response_headers": {
    "Content-Type": "application/json"
  },
  "response_body": "{\"id\":\"pay_123\",\"status\":\"success\"}"
}
```

### Request Failed (Error level)

```json
{
  "msg": "HTTP request failed",
  "method": "POST",
  "url": "https://api.example.com/payments",
  "duration_ms": 5003,
  "error": "context deadline exceeded"
}
```

### Retry Attempt (Warn level)

```json
{
  "msg": "HTTP request retrying",
  "method": "GET",
  "url": "https://api.example.com/users",
  "attempt": 1,
  "backoff": "150ms"
}
```

## Header Obfuscation

Sensitive headers are logged as `********` while sent with real values:

```go
resp, _ := client.Get(ctx, url,
    httpclient.WithObfuscatedHeaders("Authorization", "X-Api-Key"),
)
```

```json
{
  "request_headers": {
    "Authorization": "********",
    "X-Api-Key": "********",
    "Content-Type": "application/json"
  }
}
```

Header name matching is **case-insensitive**: `"authorization"` matches `Authorization`.

## Per-Request Tags

Add custom fields to log entries for a specific request:

```go
resp, _ := client.Get(ctx, url,
    httpclient.WithTags(map[string]string{
        "service":   "stripe",
        "operation": "charge",
        "customer":  "cust_123",
    }),
)
```

Tags appear in both the `Debug` (request start) and `Info` (request completed) log entries:

```json
{
  "msg": "HTTP request completed",
  "method": "POST",
  "url": "https://api.stripe.com/v1/charges",
  "status": 200,
  "duration_ms": 320,
  "service": "stripe",
  "operation": "charge",
  "customer": "cust_123"
}
```

## Skip Logging

Disable logging for specific requests (e.g., health checks):

```go
resp, _ := client.Get(ctx, healthCheckURL, httpclient.WithSkipLog())
```

This suppresses all log output including retry warnings.

## Body Logging

Request and response bodies are read for logging and **reconstructed** so the caller can still consume them:

```go
resp, _ := client.Post(ctx, url, "application/json", body)

// Body is still readable after logging
data, _ := io.ReadAll(resp.Body)
```

Empty bodies are logged as `""`.

## Logger Lifecycle

The client does **not** call `Close()` on the logger. The caller who creates the logger is responsible for its lifecycle:

```go
logger, _ := loki.New(loki.DefaultConfig(), ...)
defer logger.Close()  // Caller manages lifecycle

client, _ := httpclient.New(
    httpclient.DefaultConfig(),
    httpclient.WithLogger(logger),
)
defer client.Close()
```

## Custom Logger Adapter

Create adapters for any logger:

```go
type ZapAdapter struct {
    logger *zap.Logger
}

func (a *ZapAdapter) Info(_ context.Context, msg string, fields map[string]any) {
    a.logger.Info(msg, toZapFields(fields)...)
}

func (a *ZapAdapter) Error(_ context.Context, msg string, fields map[string]any) {
    a.logger.Error(msg, toZapFields(fields)...)
}

func (a *ZapAdapter) Warn(_ context.Context, msg string, fields map[string]any) {
    a.logger.Warn(msg, toZapFields(fields)...)
}

func (a *ZapAdapter) Debug(_ context.Context, msg string, fields map[string]any) {
    a.logger.Debug(msg, toZapFields(fields)...)
}

func (a *ZapAdapter) Close() error {
    return a.logger.Sync()
}
```
