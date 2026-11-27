# Telemetry

OpenTelemetry distributed tracing for monitoring HTTP requests across microservices.

## Quick Start

**Disabled by default** - must be explicitly enabled:

```go
client, _ := httpclient.New(
    httpclient.DefaultConfig(),
    httpclient.WithTelemetry(&httpclient.TelemetryConfig{
        ServiceName:  "payment-service",
        Version:      "1.0.0",
        Environment:  "production",
        OTLPEndpoint: "localhost:4318",
        SampleAll:    false,
    }),
)
defer client.Close()
```

## Features

- **Distributed tracing** - Track outgoing HTTP requests with spans
- **Trace propagation** - Automatically injects `Traceparent` header for downstream services
- **Error recording** - Marks network errors and any 4xx/5xx response as span errors, with `error.type`
- **Semantic conventions** - Current OpenTelemetry HTTP client attributes, so backends (Tempo span metrics, Datadog, Jaeger) group calls by dependency
- **Backend agnostic** - Works with Jaeger, Datadog, Zipkin, and any OTLP-compatible backend

## Span Attributes

Spans are named `<METHOD> <host>` (`POST api.example.com`): one name per dependency, never per URL, so span metrics stay low-cardinality. Attributes follow the current [HTTP client semantic conventions](https://opentelemetry.io/docs/specs/semconv/http/http-spans/#http-client):

| Attribute | Description | Example |
|---|---|---|
| `http.request.method` | HTTP method | `GET`, `POST` |
| `url.full` | Request URL, with user info and every query value replaced by `REDACTED` | `https://api.example.com/users?key=REDACTED` |
| `server.address` | Target host, without the port | `api.example.com` |
| `server.port` | Target port (the scheme's default when the URL has none) | `443` |
| `http.response.status_code` | Response status | `200`, `429` |
| `error.type` | Only on failure: the status code (4xx, 5xx) or the Go error type | `429`, `*url.Error` |

Up to v0.0.3 the spans were named `HTTP <METHOD>` and used the older attributes (`http.method`, `http.url`, `http.scheme`, `net.peer.name`, `http.status_code`), and only 5xx marked an error. Queries or dashboards on those names must move to the ones above.

## Configuration

| Field | Required | Description | Example |
|---|---|---|---|
| `ServiceName` | Yes | Service name in traces | `"payment-service"` |
| `Version` | No | Service version | `"1.0.0"` |
| `Environment` | No | Deployment environment | `"production"` |
| `OTLPEndpoint` | Yes | OTLP collector endpoint | `"localhost:4318"` |
| `SampleAll` | No | Sample all traces (default: false = 10%) | `true` for dev |

## How It Works

```
client.Get(ctx, url)
  ↓
Hook: OnRequestStart
  → Create span "GET api.example.com"
  → Set attributes (method, redacted url, server address and port)
  → Inject Traceparent header
  ↓
HTTP request executes
  ↓
Hook: OnRequestEnd / OnError
  → Set http.response.status_code
  → Mark error (and error.type) on 4xx, 5xx or network failure
  → End span (sent to OTLP endpoint)
```

Telemetry is implemented as a `Hook` that is automatically registered when `WithTelemetry` is configured. No manual hook registration is needed.

## Trace Propagation

The telemetry hook automatically injects W3C Trace Context headers into outgoing requests:

```
Traceparent: 00-<trace-id>-<span-id>-01
```

This enables end-to-end tracing across services:

```
[payment-service] → HTTP GET /api/users → [user-service]
       span-1    ──── Traceparent ────→      span-2
```

If the incoming `context.Context` already contains a parent span (e.g., from an HTTP server), the client span becomes a child of it.

## Backend Integration

### Jaeger

```go
httpclient.WithTelemetry(&httpclient.TelemetryConfig{
    ServiceName:  "my-service",
    OTLPEndpoint: "localhost:4318",
    SampleAll:    true,
})
```

Run Jaeger:
```bash
docker run -d --name jaeger \
  -p 4318:4318 \
  -p 16686:16686 \
  jaegertracing/all-in-one:latest
```

Access UI: http://localhost:16686

### Datadog

```go
httpclient.WithTelemetry(&httpclient.TelemetryConfig{
    ServiceName:  "my-service",
    Environment:  "production",
    OTLPEndpoint: os.Getenv("DD_OTLP_ENDPOINT"),
    SampleAll:    false,
})
```

### Zipkin

```go
httpclient.WithTelemetry(&httpclient.TelemetryConfig{
    ServiceName:  "my-service",
    OTLPEndpoint: "localhost:9411",
    SampleAll:    true,
})
```

## Sampling Strategies

### Development/Staging

```go
SampleAll: true  // Capture 100% of traces
```

Good for: Low traffic, debugging, complete visibility.

### Production

```go
SampleAll: false  // Sample 10% of traces
```

Good for: High traffic, reduced overhead, cost optimization.

## Graceful Shutdown

`client.Close()` automatically flushes pending spans and shuts down the trace provider:

```go
client, _ := httpclient.New(cfg, httpclient.WithTelemetry(&telemetryCfg))
defer client.Close()  // Flushes spans with 5s timeout
```

## Initialization Failure

If telemetry fails to initialize (e.g., invalid endpoint), the client still works normally:

```go
// If OTLP endpoint is unreachable:
// - Error is logged (if logger is configured)
// - Client works without tracing
// - No telemetry hook is registered
```

## When to Use

| Scenario | Enable Telemetry | Notes |
|---|---|---|
| Production | Yes | Essential for monitoring |
| Staging | Yes | Debug performance issues |
| Microservices | Yes | Track distributed requests |
| Development | Optional | Enable when testing tracing |
| Testing | No | Unnecessary overhead |

## Validation Rules

| Rule | Error |
|---|---|
| `ServiceName` empty | `telemetry.serviceName must not be empty` |
| `OTLPEndpoint` empty | `telemetry.otlpEndpoint must not be empty` |
