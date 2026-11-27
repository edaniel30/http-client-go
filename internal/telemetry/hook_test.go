package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func setupTestTracer(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()

	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
	})

	return exporter
}

func TestHook_OnRequestStart_CreatesSpan(t *testing.T) {
	exporter := setupTestTracer(t)
	hook := NewHook()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/test", nil)
	require.NoError(t, err)

	hook.OnRequestStart(req)

	assert.NotEmpty(t, req.Header.Get("Traceparent"), "trace context should be injected")

	hook.mu.Lock()
	assert.Len(t, hook.spans, 1)
	hook.mu.Unlock()

	resp := &http.Response{StatusCode: http.StatusOK}
	hook.OnRequestEnd(req, resp)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "GET 127.0.0.1", spans[0].Name)
}

func TestHook_OnRequestEnd_RecordsStatus(t *testing.T) {
	exporter := setupTestTracer(t)
	hook := NewHook()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://example.com/api", nil)
	hook.OnRequestStart(req)

	resp := &http.Response{StatusCode: http.StatusCreated}
	hook.OnRequestEnd(req, resp)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	hasStatusCode := false
	for _, attr := range spans[0].Attributes {
		if string(attr.Key) == "http.response.status_code" {
			hasStatusCode = true
			assert.Equal(t, int64(201), attr.Value.AsInt64())
		}
	}

	assert.True(t, hasStatusCode)
}

func TestHook_OnRequestEnd_MarksServerError(t *testing.T) {
	exporter := setupTestTracer(t)
	hook := NewHook()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", nil)
	hook.OnRequestStart(req)

	resp := &http.Response{StatusCode: http.StatusInternalServerError}
	hook.OnRequestEnd(req, resp)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "Error", spans[0].Status.Code.String())
}

func TestHook_OnError_RecordsError(t *testing.T) {
	exporter := setupTestTracer(t)
	hook := NewHook()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", nil)
	hook.OnRequestStart(req)

	hook.OnError(req, context.DeadlineExceeded)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "Error", spans[0].Status.Code.String())
	assert.NotEmpty(t, spans[0].Events)
}

func TestHook_OnRequestEnd_NoSpan_NoOp(t *testing.T) {
	hook := NewHook()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", nil)
	resp := &http.Response{StatusCode: http.StatusOK}

	// Should not panic when no span exists
	hook.OnRequestEnd(req, resp)
}

func TestHook_OnError_NoSpan_NoOp(t *testing.T) {
	hook := NewHook()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", nil)

	// Should not panic when no span exists
	hook.OnError(req, context.DeadlineExceeded)
}

func TestHook_SpanAttributes(t *testing.T) {
	exporter := setupTestTracer(t)
	hook := NewHook()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://example.com:8080/api/v1/users", nil)
	hook.OnRequestStart(req)

	resp := &http.Response{StatusCode: http.StatusOK}
	hook.OnRequestEnd(req, resp)

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "POST example.com", spans[0].Name)

	attrs := spanAttributes(spans[0])
	assert.Equal(t, "POST", attrs["http.request.method"])
	assert.Equal(t, "http://example.com:8080/api/v1/users", attrs["url.full"])
	assert.Equal(t, "example.com", attrs["server.address"])
	assert.Equal(t, int64(8080), attrs["server.port"])
	assert.Equal(t, int64(200), attrs["http.response.status_code"])
	assert.NotContains(t, attrs, "error.type")
	assert.Equal(t, "Unset", spans[0].Status.Code.String())
}

func TestHook_DefaultPortFromScheme(t *testing.T) {
	exporter := setupTestTracer(t)
	hook := NewHook()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://generativelanguage.googleapis.com/v1beta/interactions", nil)
	hook.OnRequestStart(req)
	hook.OnRequestEnd(req, &http.Response{StatusCode: http.StatusOK})

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "POST generativelanguage.googleapis.com", spans[0].Name)

	attrs := spanAttributes(spans[0])
	assert.Equal(t, "generativelanguage.googleapis.com", attrs["server.address"])
	assert.Equal(t, int64(443), attrs["server.port"])
}

func TestHook_OnRequestEnd_MarksClientError(t *testing.T) {
	exporter := setupTestTracer(t)
	hook := NewHook()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://api.example.com/v1", nil)
	hook.OnRequestStart(req)
	hook.OnRequestEnd(req, &http.Response{StatusCode: http.StatusTooManyRequests})

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "Error", spans[0].Status.Code.String())

	attrs := spanAttributes(spans[0])
	assert.Equal(t, int64(429), attrs["http.response.status_code"])
	assert.Equal(t, "429", attrs["error.type"])
}

func TestHook_OnError_SetsErrorType(t *testing.T) {
	exporter := setupTestTracer(t)
	hook := NewHook()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.com", nil)
	hook.OnRequestStart(req)
	hook.OnError(req, &url.Error{Op: "Get", URL: "http://example.com", Err: context.DeadlineExceeded})

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)
	assert.Equal(t, "*url.Error", spanAttributes(spans[0])["error.type"])
}

func TestHook_RedactsURL(t *testing.T) {
	exporter := setupTestTracer(t)
	hook := NewHook()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet,
		"https://user:pass@bucket.example.com/file.pdf?X-Amz-Signature=abc&key=secret#frag", nil)
	hook.OnRequestStart(req)
	hook.OnRequestEnd(req, &http.Response{StatusCode: http.StatusOK})

	spans := exporter.GetSpans()
	require.Len(t, spans, 1)

	full, _ := spanAttributes(spans[0])["url.full"].(string)
	assert.Equal(t, "https://REDACTED:REDACTED@bucket.example.com/file.pdf?X-Amz-Signature=REDACTED&key=REDACTED", full)
	assert.NotContains(t, full, "secret")
	assert.NotContains(t, full, "pass")
	// The request itself is untouched
	assert.Equal(t, "secret", req.URL.Query().Get("key"))
}

func spanAttributes(span tracetest.SpanStub) map[string]any {
	attrs := make(map[string]any)
	for _, attr := range span.Attributes {
		attrs[string(attr.Key)] = attr.Value.AsInterface()
	}

	return attrs
}
