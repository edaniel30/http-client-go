package httpclient

import "net/http"

// RequestOption configures per-request behavior, overriding client-level defaults.
type RequestOption func(*requestOpts)

type requestOpts struct {
	skipLog            bool
	traceID            string
	tags               map[string]string
	obfuscatedHeaders  []string
	extraHeaders       http.Header
}

func defaultRequestOpts() *requestOpts {
	return &requestOpts{
		tags: make(map[string]string),
	}
}

func applyRequestOpts(opts []RequestOption) *requestOpts {
	ro := defaultRequestOpts()
	for _, opt := range opts {
		opt(ro)
	}

	return ro
}

// WithSkipLog disables logging for this specific request.
func WithSkipLog() RequestOption {
	return func(o *requestOpts) {
		o.skipLog = true
	}
}

// WithTraceID adds an X-Trace-Id header to the request for distributed tracing.
func WithTraceID(traceID string) RequestOption {
	return func(o *requestOpts) {
		o.traceID = traceID
	}
}

// WithTags adds custom key-value tags to the log entries for this request.
func WithTags(tags map[string]string) RequestOption {
	return func(o *requestOpts) {
		for k, v := range tags {
			o.tags[k] = v
		}
	}
}

// WithObfuscatedHeaders specifies header names whose values should be masked in logs.
// The headers are sent with their original values but logged as "********".
func WithObfuscatedHeaders(headers ...string) RequestOption {
	return func(o *requestOpts) {
		o.obfuscatedHeaders = append(o.obfuscatedHeaders, headers...)
	}
}

// WithRequestHeaders adds extra headers only for this specific request.
func WithRequestHeaders(headers http.Header) RequestOption {
	return func(o *requestOpts) {
		o.extraHeaders = headers
	}
}
