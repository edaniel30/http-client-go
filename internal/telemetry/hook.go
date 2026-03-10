package telemetry

import (
	"fmt"
	"net/http"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

const tracerName = "github.com/edaniel30/http-client-go"

// Hook implements the httpclient.Hook interface to create OpenTelemetry spans
// for every HTTP request.
type Hook struct {
	tracer trace.Tracer
	mu     sync.Mutex
	spans  map[*http.Request]trace.Span
}

// NewHook creates a telemetry hook that instruments HTTP requests with OpenTelemetry spans.
func NewHook() *Hook {
	return &Hook{
		tracer: otel.Tracer(tracerName),
		spans:  make(map[*http.Request]trace.Span),
	}
}

// OnRequestStart creates a new span and injects trace context into request headers.
func (h *Hook) OnRequestStart(req *http.Request) {
	ctx := req.Context()
	spanName := fmt.Sprintf("HTTP %s", req.Method)

	ctx, span := h.tracer.Start(ctx, spanName,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("http.method", req.Method),
			attribute.String("http.url", req.URL.String()),
			attribute.String("http.scheme", req.URL.Scheme),
			attribute.String("net.peer.name", req.URL.Host),
		),
	)

	// Inject trace context into outgoing request headers
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	// Update the request with the new context containing the span
	*req = *req.WithContext(ctx)

	h.mu.Lock()
	h.spans[req] = span
	h.mu.Unlock()
}

// OnRequestEnd records response attributes and ends the span.
func (h *Hook) OnRequestEnd(req *http.Request, resp *http.Response) {
	h.mu.Lock()
	span, ok := h.spans[req]
	if ok {
		delete(h.spans, req)
	}
	h.mu.Unlock()

	if !ok {
		return
	}

	span.SetAttributes(
		attribute.Int("http.status_code", resp.StatusCode),
	)

	if resp.StatusCode >= http.StatusInternalServerError {
		span.SetStatus(codes.Error, http.StatusText(resp.StatusCode))
	}

	span.End()
}

// OnError records the error and ends the span.
func (h *Hook) OnError(req *http.Request, err error) {
	h.mu.Lock()
	span, ok := h.spans[req]
	if ok {
		delete(h.spans, req)
	}
	h.mu.Unlock()

	if !ok {
		return
	}

	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	span.End()
}
