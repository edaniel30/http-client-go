# Hooks

Lifecycle callbacks for HTTP requests, enabling custom logic at key points in the request lifecycle.

## Quick Start

```go
type MetricsHook struct{}

func (h *MetricsHook) OnRequestStart(req *http.Request) {
    metrics.IncrementCounter("http_requests_total")
}

func (h *MetricsHook) OnRequestEnd(req *http.Request, resp *http.Response) {
    metrics.RecordStatus(resp.StatusCode)
}

func (h *MetricsHook) OnError(req *http.Request, err error) {
    metrics.IncrementCounter("http_errors_total")
}

client, _ := httpclient.New(
    httpclient.DefaultConfig(),
    httpclient.WithHooks(&MetricsHook{}),
)
```

## Hook Interface

```go
type Hook interface {
    OnRequestStart(req *http.Request)
    OnRequestEnd(req *http.Request, resp *http.Response)
    OnError(req *http.Request, err error)
}
```

| Method | When | Available Data |
|---|---|---|
| `OnRequestStart` | Before HTTP request is sent | Request method, URL, headers |
| `OnRequestEnd` | After successful response | Request + response with status code |
| `OnError` | After request fails | Request + error |

## Execution Order

```
Default headers applied
  ↓
Per-request options applied (trace ID, extra headers)
  ↓
Hooks: OnRequestStart (in registration order)
  ↓
Logging: request started
  ↓
HTTP request (with retry if configured)
  ↓
Success?
  ├─ Yes → Hooks: OnRequestEnd → Logging: completed
  └─ No  → Hooks: OnError → Logging: failed
```

## Multiple Hooks

Register multiple hooks - they execute in order:

```go
client, _ := httpclient.New(
    httpclient.DefaultConfig(),
    httpclient.WithHooks(&MetricsHook{}, &AuditHook{}, &AlertHook{}),
)
```

```
OnRequestStart:  MetricsHook → AuditHook → AlertHook
OnRequestEnd:    MetricsHook → AuditHook → AlertHook
OnError:         MetricsHook → AuditHook → AlertHook
```

## Use Cases

### Metrics Collection

```go
type MetricsHook struct {
    requestDuration *prometheus.HistogramVec
}

func (h *MetricsHook) OnRequestStart(req *http.Request) {}

func (h *MetricsHook) OnRequestEnd(req *http.Request, resp *http.Response) {
    h.requestDuration.WithLabelValues(
        req.Method,
        strconv.Itoa(resp.StatusCode),
    ).Observe(time.Since(startTime).Seconds())
}

func (h *MetricsHook) OnError(req *http.Request, err error) {
    h.requestDuration.WithLabelValues(req.Method, "error").Observe(0)
}
```

### Request Auditing

```go
type AuditHook struct {
    logger Logger
}

func (h *AuditHook) OnRequestStart(req *http.Request) {
    h.logger.Info("Outgoing request", map[string]any{
        "method": req.Method,
        "url":    req.URL.String(),
    })
}

func (h *AuditHook) OnRequestEnd(req *http.Request, resp *http.Response) {}
func (h *AuditHook) OnError(req *http.Request, err error) {}
```

### Circuit Breaker

```go
type CircuitBreakerHook struct {
    failures  atomic.Int32
    threshold int32
    open      atomic.Bool
}

func (h *CircuitBreakerHook) OnRequestStart(req *http.Request) {}

func (h *CircuitBreakerHook) OnRequestEnd(req *http.Request, resp *http.Response) {
    if resp.StatusCode < 500 {
        h.failures.Store(0)
    }
}

func (h *CircuitBreakerHook) OnError(req *http.Request, err error) {
    if h.failures.Add(1) >= h.threshold {
        h.open.Store(true)
    }
}
```

## Built-in Hook: Telemetry

When `WithTelemetry` is configured, an OpenTelemetry hook is automatically registered. It creates spans, injects trace context, and records status codes. See [Telemetry Documentation](TELEMETRY.md).

## Important Notes

- All three methods must be implemented (use empty bodies for unused callbacks)
- Hooks should be **fast and non-blocking** — avoid heavy computation or I/O
- Hooks are called **once per `Do()` call**, not per retry attempt
- Hooks receive the final request (after headers, trace ID, and per-request options are applied)
- `OnError` receives the raw transport error, not the wrapped `*RequestError`
