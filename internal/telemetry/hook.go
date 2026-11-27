package telemetry

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	tracerName = "github.com/edaniel30/http-client-go"

	// redacted replaces credentials and query values in url.full: a URL may
	// carry an API key or a presigned signature in its query.
	redacted = "REDACTED"
)

// Hook implements the httpclient.Hook interface to create OpenTelemetry spans
// for every HTTP request.
//
// Spans follow the current HTTP client semantic conventions (server.address,
// server.port, url.full, http.request.method, http.response.status_code,
// error.type), the attributes trace backends group outgoing calls by: Tempo's
// span metrics, for one, read server.address as the called dependency.
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
//
// The span is named "<METHOD> <host>" (e.g. "POST api.example.com"): one name
// per dependency, never per URL, so span metrics stay low-cardinality.
func (h *Hook) OnRequestStart(req *http.Request) {
	ctx := req.Context()
	host, port := serverAddress(req.URL)

	attrs := []attribute.KeyValue{
		semconv.HTTPRequestMethodKey.String(req.Method),
		semconv.URLFull(redactURL(req.URL)),
		semconv.ServerAddress(host),
	}
	if port > 0 {
		attrs = append(attrs, semconv.ServerPort(port))
	}

	ctx, span := h.tracer.Start(ctx, spanName(req.Method, host),
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
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
//
// For a client, any 4xx or 5xx is a failed call (a 429 from a rate-limited
// API, a 401 from a revoked key): the span is marked as an error and
// error.type holds the status code.
func (h *Hook) OnRequestEnd(req *http.Request, resp *http.Response) {
	span, ok := h.take(req)
	if !ok {
		return
	}

	span.SetAttributes(semconv.HTTPResponseStatusCode(resp.StatusCode))

	if resp.StatusCode >= http.StatusBadRequest {
		span.SetAttributes(semconv.ErrorTypeKey.String(strconv.Itoa(resp.StatusCode)))
		span.SetStatus(codes.Error, http.StatusText(resp.StatusCode))
	}

	span.End()
}

// OnError records the error and ends the span.
func (h *Hook) OnError(req *http.Request, err error) {
	span, ok := h.take(req)
	if !ok {
		return
	}

	span.SetAttributes(semconv.ErrorTypeKey.String(fmt.Sprintf("%T", err)))
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
	span.End()
}

func (h *Hook) take(req *http.Request) (trace.Span, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	span, ok := h.spans[req]
	if ok {
		delete(h.spans, req)
	}

	return span, ok
}

func spanName(method, host string) string {
	if host == "" {
		return method
	}

	return method + " " + host
}

// serverAddress returns the host without the port, and the port: the one in
// the URL, or the scheme's default (0 when unknown).
func serverAddress(u *url.URL) (string, int) {
	host := u.Hostname()

	if p := u.Port(); p != "" {
		if port, err := strconv.Atoi(p); err == nil {
			return host, port
		}
	}

	switch u.Scheme {
	case "https":
		return host, 443
	case "http":
		return host, 80
	default:
		return host, 0
	}
}

// redactURL renders the URL for url.full with its user info and every query
// value replaced by REDACTED (the parameter names stay).
func redactURL(u *url.URL) string {
	clean := *u

	if clean.User != nil {
		clean.User = url.UserPassword(redacted, redacted)
	}

	if clean.RawQuery != "" {
		query := clean.Query()
		for key, values := range query {
			for i := range values {
				values[i] = redacted
			}
			query[key] = values
		}
		clean.RawQuery = query.Encode()
	}

	clean.Fragment = ""
	clean.RawFragment = ""

	return clean.String()
}
