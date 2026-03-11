package httpclient

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockLogger struct {
	debugCalls []logCall
	infoCalls  []logCall
	warnCalls  []logCall
	errorCalls []logCall
}

type logCall struct {
	msg    string
	fields map[string]any
}

func (m *mockLogger) Debug(_ context.Context, msg string, fields map[string]any) {
	m.debugCalls = append(m.debugCalls, logCall{msg: msg, fields: fields})
}

func (m *mockLogger) Info(_ context.Context, msg string, fields map[string]any) {
	m.infoCalls = append(m.infoCalls, logCall{msg: msg, fields: fields})
}

func (m *mockLogger) Warn(_ context.Context, msg string, fields map[string]any) {
	m.warnCalls = append(m.warnCalls, logCall{msg: msg, fields: fields})
}

func (m *mockLogger) Error(_ context.Context, msg string, fields map[string]any) {
	m.errorCalls = append(m.errorCalls, logCall{msg: msg, fields: fields})
}

func (m *mockLogger) Close() error { return nil }

type mockHook struct {
	startCalls []hookStartCall
	endCalls   []hookEndCall
	errorCalls []hookErrorCall
}

type hookStartCall struct {
	method string
	url    string
}

type hookEndCall struct {
	method string
	url    string
	status int
}

type hookErrorCall struct {
	method string
	url    string
	err    error
}

func (h *mockHook) OnRequestStart(req *http.Request) {
	h.startCalls = append(h.startCalls, hookStartCall{method: req.Method, url: req.URL.String()})
}

func (h *mockHook) OnRequestEnd(req *http.Request, resp *http.Response) {
	h.endCalls = append(h.endCalls, hookEndCall{method: req.Method, url: req.URL.String(), status: resp.StatusCode})
}

func (h *mockHook) OnError(req *http.Request, err error) {
	h.errorCalls = append(h.errorCalls, hookErrorCall{method: req.Method, url: req.URL.String(), err: err})
}

// --- New ---

func TestNew(t *testing.T) {
	tests := []struct {
		name        string
		cfg         *Config
		opts        []Option
		expectError bool
	}{
		{
			name:        "default config",
			cfg:         DefaultConfig(),
			expectError: false,
		},
		{
			name: "with options",
			cfg:  DefaultConfig(),
			opts: []Option{
				WithTimeout(10 * time.Second),
				WithHeaders(map[string]string{"X-Custom": "value"}),
				WithLogger(&mockLogger{}),
			},
			expectError: false,
		},
		{
			name: "negative timeout fails validation",
			cfg:  DefaultConfig(),
			opts: []Option{
				WithTimeout(-1 * time.Second),
			},
			expectError: true,
		},
		{
			name: "zero timeout fails validation",
			cfg: &Config{
				Timeout: 0,
				Headers: make(map[string]string),
			},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := New(tt.cfg, tt.opts...)
			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, client)

				var configErr *ConfigError
				assert.ErrorAs(t, err, &configErr)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, client)
			}
		})
	}
}

// --- Close ---

func TestClient_Close(t *testing.T) {
	client, err := New(DefaultConfig())
	require.NoError(t, err)

	client.Close()

	_, err = client.Get(context.Background(), "http://example.com")
	assert.ErrorIs(t, err, ErrClientClosed)
}

func TestClient_Close_Idempotent(t *testing.T) {
	client, err := New(DefaultConfig())
	require.NoError(t, err)

	client.Close()
	client.Close()

	assert.True(t, client.closed)
}

// --- Do ---

func TestClient_Do_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	logger := &mockLogger{}
	client, err := New(DefaultConfig(), WithLogger(logger))
	require.NoError(t, err)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/test", nil)
	resp, err := client.Do(context.Background(), req)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	assert.Len(t, logger.debugCalls, 1)
	assert.Equal(t, "HTTP request started", logger.debugCalls[0].msg)
	assert.Len(t, logger.infoCalls, 1)
	assert.Equal(t, "HTTP request completed", logger.infoCalls[0].msg)
	assert.Equal(t, http.StatusOK, logger.infoCalls[0].fields["status"])
}

func TestClient_Do_Error(t *testing.T) {
	logger := &mockLogger{}
	client, err := New(DefaultConfig(), WithLogger(logger), WithTimeout(1*time.Millisecond))
	require.NoError(t, err)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://192.0.2.1/timeout", nil)
	resp, err := client.Do(context.Background(), req)

	assert.Error(t, err)
	assert.Nil(t, resp)

	var reqErr *RequestError
	assert.ErrorAs(t, err, &reqErr)
	assert.Equal(t, http.MethodGet, reqErr.Method)

	assert.Len(t, logger.errorCalls, 1)
	assert.Equal(t, "HTTP request failed", logger.errorCalls[0].msg)
}

func TestClient_Do_WithoutLogger(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(DefaultConfig())
	require.NoError(t, err)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	resp, err := client.Do(context.Background(), req)

	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

// --- Default Headers ---

func TestClient_DefaultHeaders(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(
		DefaultConfig(),
		WithHeaders(map[string]string{"Authorization": "Bearer token123"}),
	)
	require.NoError(t, err)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	resp, err := client.Do(context.Background(), req)

	assert.NoError(t, err)
	assert.Equal(t, "Bearer token123", receivedAuth)
	_ = resp.Body.Close()
}

func TestClient_DefaultHeaders_NoOverride(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(
		DefaultConfig(),
		WithHeaders(map[string]string{"Authorization": "Bearer default"}),
	)
	require.NoError(t, err)

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, nil)
	req.Header.Set("Authorization", "Bearer explicit")
	resp, err := client.Do(context.Background(), req)

	assert.NoError(t, err)
	assert.Equal(t, "Bearer explicit", receivedAuth)
	_ = resp.Body.Close()
}

// --- HTTP Methods ---

func TestClient_Get(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(DefaultConfig())
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestClient_Post(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		assert.Equal(t, `{"key":"value"}`, string(body))
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client, err := New(DefaultConfig())
	require.NoError(t, err)

	resp, err := client.Post(context.Background(), server.URL, "application/json", strings.NewReader(`{"key":"value"}`))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestClient_Put(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPut, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(DefaultConfig())
	require.NoError(t, err)

	resp, err := client.Put(context.Background(), server.URL, "application/json", strings.NewReader(`{"updated":true}`))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestClient_Patch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPatch, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		assert.Equal(t, `{"field":"patched"}`, string(body))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(DefaultConfig())
	require.NoError(t, err)

	resp, err := client.Patch(context.Background(), server.URL, "application/json", strings.NewReader(`{"field":"patched"}`))
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestClient_Delete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := New(DefaultConfig())
	require.NoError(t, err)

	resp, err := client.Delete(context.Background(), server.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestClient_Head(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodHead, r.Method)
		w.Header().Set("X-Custom", "value")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(DefaultConfig())
	require.NoError(t, err)

	resp, err := client.Head(context.Background(), server.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "value", resp.Header.Get("X-Custom"))
	_ = resp.Body.Close()
}

func TestClient_Options(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodOptions, r.Method)
		w.Header().Set("Allow", "GET, POST, OPTIONS")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := New(DefaultConfig())
	require.NoError(t, err)

	resp, err := client.Options(context.Background(), server.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	assert.Equal(t, "GET, POST, OPTIONS", resp.Header.Get("Allow"))
	_ = resp.Body.Close()
}

// --- Retry ---

func TestClient_Retry_OnServerError(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempt := attempts.Add(1)
		if attempt <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client, err := New(
		DefaultConfig(),
		WithRetry(3, 10*time.Millisecond, 50*time.Millisecond),
	)
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
	assert.Equal(t, int32(3), attempts.Load())
}

func TestClient_Retry_ExhaustedReturnsLastResponse(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	client, err := New(
		DefaultConfig(),
		WithRetry(2, 10*time.Millisecond, 50*time.Millisecond),
	)
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	_ = resp.Body.Close()
	assert.Equal(t, int32(3), attempts.Load())
}

func TestClient_Retry_On429(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(
		DefaultConfig(),
		WithRetry(2, 10*time.Millisecond, 50*time.Millisecond),
	)
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestClient_Retry_NoRetryOn4xx(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer server.Close()

	client, err := New(
		DefaultConfig(),
		WithRetry(3, 10*time.Millisecond, 50*time.Millisecond),
	)
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	_ = resp.Body.Close()
	assert.Equal(t, int32(1), attempts.Load())
}

func TestClient_Retry_RespectsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := New(
		DefaultConfig(),
		WithRetry(5, 1*time.Second, 5*time.Second),
	)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err = client.Get(ctx, server.URL)
	assert.Error(t, err)
}

func TestClient_Retry_LogsRetryAttempts(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := &mockLogger{}
	client, err := New(
		DefaultConfig(),
		WithRetry(2, 10*time.Millisecond, 50*time.Millisecond),
		WithLogger(logger),
	)
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	assert.Len(t, logger.warnCalls, 1)
	assert.Equal(t, "HTTP request retrying", logger.warnCalls[0].msg)
	assert.Equal(t, 1, logger.warnCalls[0].fields["attempt"])
}

func TestClient_Retry_WithoutRetryConfig(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client, err := New(DefaultConfig())
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	_ = resp.Body.Close()
	assert.Equal(t, int32(1), attempts.Load())
}

// --- Retry Config Validation ---

func TestRetryConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		opts        []Option
		expectError bool
		errorField  string
	}{
		{
			name:        "valid retry config",
			opts:        []Option{WithRetry(3, 100*time.Millisecond, 5*time.Second)},
			expectError: false,
		},
		{
			name:        "negative max retries",
			opts:        []Option{WithRetry(-1, 100*time.Millisecond, 5*time.Second)},
			expectError: true,
			errorField:  "retry.maxRetries",
		},
		{
			name:        "zero min backoff",
			opts:        []Option{WithRetry(3, 0, 5*time.Second)},
			expectError: true,
			errorField:  "retry.minBackoff",
		},
		{
			name:        "negative max backoff",
			opts:        []Option{WithRetry(3, 100*time.Millisecond, -1*time.Second)},
			expectError: true,
			errorField:  "retry.maxBackoff",
		},
		{
			name:        "min backoff greater than max backoff",
			opts:        []Option{WithRetry(3, 10*time.Second, 1*time.Second)},
			expectError: true,
			errorField:  "retry.minBackoff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(DefaultConfig(), tt.opts...)
			if tt.expectError {
				assert.Error(t, err)

				var configErr *ConfigError
				assert.ErrorAs(t, err, &configErr)
				assert.Equal(t, tt.errorField, configErr.Field)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// --- DecodeJSON ---

func TestDecodeJSON_Success(t *testing.T) {
	type response struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"test","age":25}`))
	}))
	defer server.Close()

	client, err := New(DefaultConfig())
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL)
	require.NoError(t, err)

	result, err := DecodeJSON[response](resp)
	assert.NoError(t, err)
	assert.Equal(t, "test", result.Name)
	assert.Equal(t, 25, result.Age)
}

func TestDecodeJSON_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json`))
	}))
	defer server.Close()

	client, err := New(DefaultConfig())
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL)
	require.NoError(t, err)

	type response struct {
		Name string `json:"name"`
	}

	_, err = DecodeJSON[response](resp)
	assert.Error(t, err)

	var decodeErr *ResponseDecodeError
	assert.ErrorAs(t, err, &decodeErr)
	assert.Equal(t, http.StatusOK, decodeErr.StatusCode)
}

// --- Errors ---

func TestConfigError(t *testing.T) {
	err := NewConfigError("timeout", "must be positive")
	assert.Equal(t, `httpclient config error: field "timeout" must be positive`, err.Error())
	assert.Equal(t, "timeout", err.Field)
	assert.Equal(t, "must be positive", err.Message)
}

func TestRequestError(t *testing.T) {
	t.Run("with underlying error", func(t *testing.T) {
		inner := context.DeadlineExceeded
		err := &RequestError{Method: "GET", URL: "http://example.com", Err: inner}
		assert.Contains(t, err.Error(), "GET")
		assert.Contains(t, err.Error(), "http://example.com")
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

}

func TestResponseDecodeError(t *testing.T) {
	inner := io.ErrUnexpectedEOF
	err := &ResponseDecodeError{StatusCode: 200, Err: inner}
	assert.Contains(t, err.Error(), "200")
	assert.ErrorIs(t, err, io.ErrUnexpectedEOF)
}

// --- DefaultConfig ---

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	assert.Equal(t, 30*time.Second, cfg.Timeout)
	assert.NotNil(t, cfg.Headers)
	assert.Empty(t, cfg.Headers)
	assert.Nil(t, cfg.Logger)
	assert.Nil(t, cfg.Retry)
	assert.Nil(t, cfg.Transport)
	assert.Nil(t, cfg.Hooks)
	assert.Nil(t, cfg.Telemetry)
}

// --- Telemetry Config Validation ---

func TestTelemetryConfigValidation(t *testing.T) {
	t.Run("missing service name", func(t *testing.T) {
		_, err := New(DefaultConfig(), WithTelemetry(&TelemetryConfig{
			OTLPEndpoint: "localhost:4318",
		}))
		assert.Error(t, err)

		var configErr *ConfigError
		assert.ErrorAs(t, err, &configErr)
		assert.Equal(t, "telemetry.serviceName", configErr.Field)
	})

	t.Run("missing endpoint", func(t *testing.T) {
		_, err := New(DefaultConfig(), WithTelemetry(&TelemetryConfig{
			ServiceName: "my-service",
		}))
		assert.Error(t, err)

		var configErr *ConfigError
		assert.ErrorAs(t, err, &configErr)
		assert.Equal(t, "telemetry.otlpEndpoint", configErr.Field)
	})

	t.Run("nil telemetry is valid", func(t *testing.T) {
		client, err := New(DefaultConfig(), WithTelemetry(nil))
		assert.NoError(t, err)
		assert.NotNil(t, client)
		assert.Nil(t, client.telemetryManager)
	})
}

// --- Transport (Connection Pooling) ---

func TestClient_WithTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxConnsPerHost:     50,
		IdleConnTimeout:     90 * time.Second,
		MaxIdleConnsPerHost: 25,
	}

	client, err := New(DefaultConfig(), WithTransport(transport))
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestClient_WithoutTransport_UsesDefault(t *testing.T) {
	client, err := New(DefaultConfig())
	require.NoError(t, err)

	assert.Nil(t, client.httpClient.Transport)
}

// --- Per-Request Options ---

func TestClient_WithTraceID(t *testing.T) {
	var receivedTraceID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedTraceID = r.Header.Get("X-Trace-Id")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(DefaultConfig())
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL, WithTraceID("trace-abc-123"))
	assert.NoError(t, err)
	assert.Equal(t, "trace-abc-123", receivedTraceID)
	_ = resp.Body.Close()
}

func TestClient_WithSkipLog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := &mockLogger{}
	client, err := New(DefaultConfig(), WithLogger(logger))
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL, WithSkipLog())
	assert.NoError(t, err)
	_ = resp.Body.Close()

	assert.Empty(t, logger.debugCalls)
	assert.Empty(t, logger.infoCalls)
}

func TestClient_WithTags(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := &mockLogger{}
	client, err := New(DefaultConfig(), WithLogger(logger))
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL, WithTags(map[string]string{
		"service":   "payment",
		"operation": "charge",
	}))
	assert.NoError(t, err)
	_ = resp.Body.Close()

	require.Len(t, logger.infoCalls, 1)
	assert.Equal(t, "payment", logger.infoCalls[0].fields["service"])
	assert.Equal(t, "charge", logger.infoCalls[0].fields["operation"])
}

func TestClient_WithRequestHeaders(t *testing.T) {
	var receivedCustom string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedCustom = r.Header.Get("X-Request-Custom")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client, err := New(DefaultConfig())
	require.NoError(t, err)

	headers := http.Header{}
	headers.Set("X-Request-Custom", "per-request-value")

	resp, err := client.Get(context.Background(), server.URL, WithRequestHeaders(headers))
	assert.NoError(t, err)
	assert.Equal(t, "per-request-value", receivedCustom)
	_ = resp.Body.Close()
}

// --- Hooks ---

func TestClient_Hooks_OnRequestStart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hook := &mockHook{}
	client, err := New(DefaultConfig(), WithHooks(hook))
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL)
	assert.NoError(t, err)
	_ = resp.Body.Close()

	require.Len(t, hook.startCalls, 1)
	assert.Equal(t, http.MethodGet, hook.startCalls[0].method)
}

func TestClient_Hooks_OnRequestEnd(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	hook := &mockHook{}
	client, err := New(DefaultConfig(), WithHooks(hook))
	require.NoError(t, err)

	resp, err := client.Post(context.Background(), server.URL, "application/json", strings.NewReader(`{}`))
	assert.NoError(t, err)
	_ = resp.Body.Close()

	require.Len(t, hook.endCalls, 1)
	assert.Equal(t, http.MethodPost, hook.endCalls[0].method)
	assert.Equal(t, http.StatusCreated, hook.endCalls[0].status)
}

func TestClient_Hooks_OnError(t *testing.T) {
	hook := &mockHook{}
	client, err := New(DefaultConfig(), WithHooks(hook), WithTimeout(1*time.Millisecond))
	require.NoError(t, err)

	_, err = client.Get(context.Background(), "http://192.0.2.1/timeout")
	assert.Error(t, err)

	require.Len(t, hook.errorCalls, 1)
	assert.Equal(t, http.MethodGet, hook.errorCalls[0].method)
	assert.NotNil(t, hook.errorCalls[0].err)
}

func TestClient_Hooks_Multiple(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hook1 := &mockHook{}
	hook2 := &mockHook{}
	client, err := New(DefaultConfig(), WithHooks(hook1, hook2))
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL)
	assert.NoError(t, err)
	_ = resp.Body.Close()

	assert.Len(t, hook1.startCalls, 1)
	assert.Len(t, hook2.startCalls, 1)
	assert.Len(t, hook1.endCalls, 1)
	assert.Len(t, hook2.endCalls, 1)
}

func TestClient_Hooks_WithRequestOptions(t *testing.T) {
	var receivedTraceID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedTraceID = r.Header.Get("X-Trace-Id")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	hook := &mockHook{}
	client, err := New(DefaultConfig(), WithHooks(hook))
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL, WithTraceID("hook-trace-123"))
	assert.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, "hook-trace-123", receivedTraceID)
	require.Len(t, hook.startCalls, 1)
	require.Len(t, hook.endCalls, 1)
}

// --- Retry with SkipLog ---

func TestClient_Retry_SkipLog(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempt := attempts.Add(1)
		if attempt == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	logger := &mockLogger{}
	client, err := New(
		DefaultConfig(),
		WithRetry(2, 10*time.Millisecond, 50*time.Millisecond),
		WithLogger(logger),
	)
	require.NoError(t, err)

	resp, err := client.Get(context.Background(), server.URL, WithSkipLog())
	assert.NoError(t, err)
	_ = resp.Body.Close()

	assert.Empty(t, logger.debugCalls)
	assert.Empty(t, logger.infoCalls)
	assert.Empty(t, logger.warnCalls)
}
