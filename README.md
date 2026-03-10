# http-client-go

A production-ready HTTP client library for Go with retry, telemetry, lifecycle hooks, structured logging, and per-request options.

## Installation

```bash
go get github.com/edaniel30/http-client-go
```

## Quick Start

```go
package main

import (
    "context"
    "fmt"

    httpclient "github.com/edaniel30/http-client-go"
)

func main() {
    client, err := httpclient.New(httpclient.DefaultConfig())
    if err != nil {
        panic(err)
    }
    defer client.Close()

    resp, err := client.Get(context.Background(), "https://api.example.com/users")
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    fmt.Println("Status:", resp.StatusCode)
}
```

## Configuration

### Default Configuration

```go
httpclient.DefaultConfig()
// Returns:
// - Timeout: 30s
// - Headers: empty
// - Logger: nil (disabled)
// - Retry: nil (disabled)
// - Transport: nil (Go default)
// - Hooks: nil
// - Telemetry: nil (disabled)
```

### Configuration Options

All options use the functional options pattern:

#### Client Options

```go
httpclient.WithTimeout(10 * time.Second)                          // Request timeout
httpclient.WithHeaders(map[string]string{"Authorization": "Bearer token"})  // Default headers
httpclient.WithLogger(myLogger)                                   // Structured logger
httpclient.WithTransport(&http.Transport{MaxIdleConns: 100})      // Connection pooling
httpclient.WithRetry(3, 100*time.Millisecond, 5*time.Second)      // Retry with backoff
httpclient.WithHooks(myHook)                                      // Lifecycle hooks
httpclient.WithTelemetry(&httpclient.TelemetryConfig{...})        // OpenTelemetry tracing
```

See detailed documentation for each feature:

- [Retry](docs/retry.md) - Exponential backoff with jitter
- [Telemetry](docs/telemetry.md) - OpenTelemetry distributed tracing
- [Hooks](docs/hooks.md) - Lifecycle callbacks for requests
- [Logging](docs/logging.md) - Structured logging with obfuscation
- [Request Options](docs/request_options.md) - Per-request configuration

## HTTP Methods

```go
client.Get(ctx, url, opts...)
client.Post(ctx, url, contentType, body, opts...)
client.Put(ctx, url, contentType, body, opts...)
client.Patch(ctx, url, contentType, body, opts...)
client.Delete(ctx, url, opts...)
client.Head(ctx, url, opts...)
client.Options(ctx, url, opts...)
client.Do(ctx, req, opts...)  // Custom *http.Request
```

All methods accept optional per-request options (`...RequestOption`).

## JSON Decoding

```go
type User struct {
    Name string `json:"name"`
    Age  int    `json:"age"`
}

resp, _ := client.Get(ctx, "https://api.example.com/users/1")
user, err := httpclient.DecodeJSON[User](resp)
```

`DecodeJSON` closes the response body automatically.

## Per-Request Options

Override client-level behavior for individual requests:

```go
resp, _ := client.Get(ctx, url,
    httpclient.WithTraceID("trace-abc-123"),
    httpclient.WithSkipLog(),
    httpclient.WithTags(map[string]string{"operation": "charge"}),
    httpclient.WithObfuscatedHeaders("Authorization", "X-Api-Key"),
    httpclient.WithRequestHeaders(extraHeaders),
)
```

See [Request Options Documentation](docs/REQUEST_OPTIONS.md) for details.

## Error Handling

The library provides typed errors for precise handling:

```go
resp, err := client.Get(ctx, url)
if err != nil {
    var reqErr *httpclient.RequestError
    if errors.As(err, &reqErr) {
        fmt.Println(reqErr.Method, reqErr.URL, reqErr.Err)
    }

    if errors.Is(err, httpclient.ErrClientClosed) {
        fmt.Println("Client was closed")
    }
}

user, err := httpclient.DecodeJSON[User](resp)
if err != nil {
    var decodeErr *httpclient.ResponseDecodeError
    if errors.As(err, &decodeErr) {
        fmt.Println("Status:", decodeErr.StatusCode)
    }
}
```

| Error Type | When |
|---|---|
| `*ConfigError` | Invalid configuration (timeout, retry, telemetry) |
| `*RequestError` | HTTP request failure (network, timeout) |
| `*ResponseDecodeError` | JSON decode failure |
| `ErrClientClosed` | Client used after `Close()` |

## Lifecycle

```go
client, err := httpclient.New(httpclient.DefaultConfig(), ...)
if err != nil {
    log.Fatal(err)
}
defer client.Close()  // Closes idle connections + flushes telemetry
```

`Close()` is idempotent and safe to call multiple times.

## Full Example

```go
client, err := httpclient.New(
    httpclient.DefaultConfig(),
    httpclient.WithTimeout(10*time.Second),
    httpclient.WithHeaders(map[string]string{
        "Authorization": "Bearer " + token,
    }),
    httpclient.WithRetry(3, 100*time.Millisecond, 5*time.Second),
    httpclient.WithLogger(myLogger),
    httpclient.WithTelemetry(&httpclient.TelemetryConfig{
        ServiceName:  "payment-service",
        Version:      "1.0.0",
        Environment:  "production",
        OTLPEndpoint: "localhost:4318",
        SampleAll:    false,
    }),
)
defer client.Close()

resp, err := client.Post(
    ctx,
    "https://api.example.com/payments",
    "application/json",
    strings.NewReader(`{"amount": 100}`),
    httpclient.WithTraceID("tx-abc-123"),
    httpclient.WithObfuscatedHeaders("Authorization"),
    httpclient.WithTags(map[string]string{"provider": "stripe"}),
)
```

## Dependencies

### Required
- [stretchr/testify](https://github.com/stretchr/testify) - Testing assertions

### Optional (only with telemetry enabled)
- [go.opentelemetry.io/otel](https://github.com/open-telemetry/opentelemetry-go) - OpenTelemetry tracing
- [go.opentelemetry.io/otel/exporters/otlp](https://github.com/open-telemetry/opentelemetry-go) - OTLP exporter

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
