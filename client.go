package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/edaniel30/http-client-go/internal/telemetry"
)

const obfuscatedValue = "********"

type Client struct {
	httpClient       *http.Client
	config           *Config
	telemetryManager *telemetry.Manager
	mu               sync.RWMutex
	closed           bool
}

func New(cfg *Config, opts ...Option) (*Client, error) {
	for _, opt := range opts {
		opt(cfg)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	httpClient := &http.Client{Timeout: cfg.Timeout}
	if cfg.Transport != nil {
		httpClient.Transport = cfg.Transport
	}

	client := &Client{
		httpClient: httpClient,
		config:     cfg,
	}

	if cfg.Telemetry != nil {
		ctx := context.Background()

		tm, err := telemetry.Init(
			ctx,
			cfg.Telemetry.ServiceName,
			cfg.Telemetry.Version,
			cfg.Telemetry.Environment,
			cfg.Telemetry.OTLPEndpoint,
			cfg.Telemetry.SampleAll,
		)
		if err != nil {
			if cfg.Logger != nil {
				cfg.Logger.Error(ctx, "failed to initialize telemetry", map[string]any{"error": err.Error()})
			}
		} else {
			client.telemetryManager = tm
			cfg.Hooks = append(cfg.Hooks, telemetry.NewHook())
		}
	}

	return client, nil
}

func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return
	}

	c.closed = true
	c.httpClient.CloseIdleConnections()

	if c.telemetryManager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = c.telemetryManager.Shutdown(ctx)
	}
}

func (c *Client) checkClosed() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.closed {
		return ErrClientClosed
	}

	return nil
}

func (c *Client) Do(ctx context.Context, req *http.Request, reqOpts ...RequestOption) (*http.Response, error) {
	if err := c.checkClosed(); err != nil {
		return nil, err
	}

	ro := applyRequestOpts(reqOpts)

	for k, v := range c.config.Headers {
		if req.Header.Get(k) == "" {
			req.Header.Set(k, v)
		}
	}

	if ro.traceID != "" {
		req.Header.Set("X-Trace-Id", ro.traceID)
	}

	for k, vals := range ro.extraHeaders {
		for _, v := range vals {
			req.Header.Set(k, v)
		}
	}

	c.runOnRequestStart(req)
	c.logRequestStart(ctx, req, ro)

	start := time.Now()
	resp, err := c.doWithRetry(ctx, req, ro)
	duration := time.Since(start)

	if err != nil {
		c.runOnError(req, err)
		c.logRequestError(ctx, req, ro, duration, err)

		return nil, &RequestError{
			Method: req.Method,
			URL:    req.URL.String(),
			Err:    err,
		}
	}

	c.runOnRequestEnd(req, resp)
	c.logRequestEnd(ctx, req, resp, ro, duration)

	return resp, nil
}

func (c *Client) doWithRetry(ctx context.Context, req *http.Request, ro *requestOpts) (*http.Response, error) {
	resp, err := c.httpClient.Do(req)
	if c.config.Retry == nil || c.config.Retry.MaxRetries == 0 {
		return resp, err
	}

	for attempt := range c.config.Retry.MaxRetries {
		if !c.shouldRetry(resp, err) {
			return resp, err
		}

		if resp != nil {
			resp.Body.Close()
		}

		backoff := c.calcBackoff(attempt)

		if c.config.Logger != nil && !ro.skipLog {
			c.config.Logger.Warn(ctx, "HTTP request retrying", map[string]any{
				"method":  req.Method,
				"url":     req.URL.String(),
				"attempt": attempt + 1,
				"backoff": backoff.String(),
			})
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}

		resp, err = c.httpClient.Do(req)
	}

	return resp, err
}

func (c *Client) shouldRetry(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}

	return resp.StatusCode == http.StatusTooManyRequests ||
		resp.StatusCode >= http.StatusInternalServerError
}

func (c *Client) calcBackoff(attempt int) time.Duration {
	base := float64(c.config.Retry.MinBackoff) * math.Pow(2, float64(attempt))
	max := float64(c.config.Retry.MaxBackoff)

	if base > max {
		base = max
	}

	jitter := base * (0.5 + rand.Float64()*0.5)

	return time.Duration(jitter)
}

// --- Hooks ---

func (c *Client) runOnRequestStart(req *http.Request) {
	for _, h := range c.config.Hooks {
		h.OnRequestStart(req)
	}
}

func (c *Client) runOnRequestEnd(req *http.Request, resp *http.Response) {
	for _, h := range c.config.Hooks {
		h.OnRequestEnd(req, resp)
	}
}

func (c *Client) runOnError(req *http.Request, err error) {
	for _, h := range c.config.Hooks {
		h.OnError(req, err)
	}
}

// --- Logging ---

func (c *Client) logRequestStart(ctx context.Context, req *http.Request, ro *requestOpts) {
	if c.config.Logger == nil || ro.skipLog {
		return
	}

	fields := map[string]any{
		"method":           req.Method,
		"url":              req.URL.String(),
		"request_headers":  obfuscateHeaders(req.Header, ro.obfuscatedHeaders),
		"request_body":     readBodyPreview(req.Body, &req.Body),
	}

	for k, v := range ro.tags {
		fields[k] = v
	}

	c.config.Logger.Debug(ctx, "HTTP request started", fields)
}

func (c *Client) logRequestEnd(ctx context.Context, req *http.Request, resp *http.Response, ro *requestOpts, duration time.Duration) {
	if c.config.Logger == nil || ro.skipLog {
		return
	}

	fields := map[string]any{
		"method":           req.Method,
		"url":              req.URL.String(),
		"status":           resp.StatusCode,
		"duration_ms":      duration.Milliseconds(),
		"response_headers": obfuscateHeaders(resp.Header, ro.obfuscatedHeaders),
		"response_body":    readBodyPreview(resp.Body, &resp.Body),
	}

	for k, v := range ro.tags {
		fields[k] = v
	}

	c.config.Logger.Info(ctx, "HTTP request completed", fields)
}

func (c *Client) logRequestError(ctx context.Context, req *http.Request, ro *requestOpts, duration time.Duration, err error) {
	if c.config.Logger == nil || ro.skipLog {
		return
	}

	fields := map[string]any{
		"method":      req.Method,
		"url":         req.URL.String(),
		"duration_ms": duration.Milliseconds(),
		"error":       err.Error(),
	}

	for k, v := range ro.tags {
		fields[k] = v
	}

	c.config.Logger.Error(ctx, "HTTP request failed", fields)
}

// obfuscateHeaders returns a copy of headers with sensitive values masked.
func obfuscateHeaders(h http.Header, obfuscated []string) map[string]string {
	result := make(map[string]string, len(h))

	for k, v := range h {
		result[k] = strings.Join(v, ", ")
	}

	for _, name := range obfuscated {
		for k := range result {
			if strings.EqualFold(k, name) {
				result[k] = obfuscatedValue
			}
		}
	}

	return result
}

// readBodyPreview reads the body, returns its string content, and reconstructs the ReadCloser
// so the body can still be consumed by the caller.
func readBodyPreview(body io.ReadCloser, target *io.ReadCloser) string {
	if body == nil {
		return ""
	}

	data, err := io.ReadAll(body)
	if err != nil {
		return ""
	}

	*target = io.NopCloser(bytes.NewReader(data))

	if len(data) == 0 {
		return ""
	}

	return string(data)
}

// --- HTTP Methods ---

func (c *Client) doSimple(ctx context.Context, method, url string, opts []RequestOption) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return nil, err
	}

	return c.Do(ctx, req, opts...)
}

func (c *Client) doWithBody(ctx context.Context, method, url, contentType string, body io.Reader, opts []RequestOption) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", contentType)

	return c.Do(ctx, req, opts...)
}

func (c *Client) Get(ctx context.Context, url string, opts ...RequestOption) (*http.Response, error) {
	return c.doSimple(ctx, http.MethodGet, url, opts)
}

func (c *Client) Post(ctx context.Context, url string, contentType string, body io.Reader, opts ...RequestOption) (*http.Response, error) {
	return c.doWithBody(ctx, http.MethodPost, url, contentType, body, opts)
}

func (c *Client) Put(ctx context.Context, url string, contentType string, body io.Reader, opts ...RequestOption) (*http.Response, error) {
	return c.doWithBody(ctx, http.MethodPut, url, contentType, body, opts)
}

func (c *Client) Patch(ctx context.Context, url string, contentType string, body io.Reader, opts ...RequestOption) (*http.Response, error) {
	return c.doWithBody(ctx, http.MethodPatch, url, contentType, body, opts)
}

func (c *Client) Delete(ctx context.Context, url string, opts ...RequestOption) (*http.Response, error) {
	return c.doSimple(ctx, http.MethodDelete, url, opts)
}

func (c *Client) Head(ctx context.Context, url string, opts ...RequestOption) (*http.Response, error) {
	return c.doSimple(ctx, http.MethodHead, url, opts)
}

func (c *Client) Options(ctx context.Context, url string, opts ...RequestOption) (*http.Response, error) {
	return c.doSimple(ctx, http.MethodOptions, url, opts)
}

func DecodeJSON[T any](resp *http.Response) (T, error) {
	var result T

	defer resp.Body.Close()

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return result, &ResponseDecodeError{
			StatusCode: resp.StatusCode,
			Err:        err,
		}
	}

	return result, nil
}
