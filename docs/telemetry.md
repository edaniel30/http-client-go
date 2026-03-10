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
- **Error recording** - Records errors and 5xx status codes on spans
- **Backend agnostic** - Works with Jaeger, Datadog, Zipkin, and any OTLP-compatible backend

## Span Attributes

Automatically recorded for every HTTP request:

| Attribute | Description | Example |
|---|---|---|
| `http.method` | HTTP method | `GET`, `POST` |
| `http.url` | Full request URL | `https://api.example.com/users` |
| `http.scheme` | URL scheme | `http`, `https` |
| `net.peer.name` | Target host | `api.example.com:443` |
| `http.status_code` | Response status | `200`, `500` |

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
  → Create span "HTTP GET"
  → Set attributes (method, url, host)
  → Inject Traceparent header
  ↓
HTTP request executes
  ↓
Hook: OnRequestEnd / OnError
  → Set status_code attribute
  → Mark error if 5xx or network failure
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
