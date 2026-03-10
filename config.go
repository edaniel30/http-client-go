package httpclient

import (
	"net/http"
	"time"
)

type RetryConfig struct {
	MaxRetries int
	MinBackoff time.Duration
	MaxBackoff time.Duration
}

// TelemetryConfig configures OpenTelemetry distributed tracing.
// When provided, the client automatically creates spans for every HTTP request
// and exports them via OTLP to the configured endpoint (Datadog Agent, Jaeger, etc).
type TelemetryConfig struct {
	ServiceName  string
	Version      string
	Environment  string
	OTLPEndpoint string
	SampleAll    bool
}

type Config struct {
	Timeout   time.Duration
	Headers   map[string]string
	Logger    Logger
	Retry     *RetryConfig
	Transport *http.Transport
	Hooks     []Hook
	Telemetry *TelemetryConfig
}

type Option func(*Config)

func DefaultConfig() *Config {
	return &Config{
		Timeout: 30 * time.Second,
		Headers: make(map[string]string),
	}
}

func (c *Config) validate() error {
	if c.Timeout <= 0 {
		return NewConfigError("timeout", "must be positive")
	}

	if c.Retry != nil {
		if c.Retry.MaxRetries < 0 {
			return NewConfigError("retry.maxRetries", "must be non-negative")
		}

		if c.Retry.MinBackoff <= 0 {
			return NewConfigError("retry.minBackoff", "must be positive")
		}

		if c.Retry.MaxBackoff <= 0 {
			return NewConfigError("retry.maxBackoff", "must be positive")
		}

		if c.Retry.MinBackoff > c.Retry.MaxBackoff {
			return NewConfigError("retry.minBackoff", "must be less than or equal to maxBackoff")
		}
	}

	if c.Telemetry != nil {
		if c.Telemetry.ServiceName == "" {
			return NewConfigError("telemetry.serviceName", "must not be empty")
		}

		if c.Telemetry.OTLPEndpoint == "" {
			return NewConfigError("telemetry.otlpEndpoint", "must not be empty")
		}
	}

	return nil
}

func WithTimeout(d time.Duration) Option {
	return func(c *Config) {
		c.Timeout = d
	}
}

func WithLogger(l Logger) Option {
	return func(c *Config) {
		c.Logger = l
	}
}

func WithHeaders(h map[string]string) Option {
	return func(c *Config) {
		for k, v := range h {
			c.Headers[k] = v
		}
	}
}

func WithRetry(maxRetries int, minBackoff, maxBackoff time.Duration) Option {
	return func(c *Config) {
		c.Retry = &RetryConfig{
			MaxRetries: maxRetries,
			MinBackoff: minBackoff,
			MaxBackoff: maxBackoff,
		}
	}
}

// WithTransport sets a custom HTTP transport for connection pooling and TLS configuration.
func WithTransport(t *http.Transport) Option {
	return func(c *Config) {
		c.Transport = t
	}
}

// WithHooks adds lifecycle hooks that are called on request start, end, and error.
func WithHooks(hooks ...Hook) Option {
	return func(c *Config) {
		c.Hooks = append(c.Hooks, hooks...)
	}
}

// WithTelemetry enables OpenTelemetry distributed tracing.
// The client initializes OTLP export and creates spans for every HTTP request.
// Pass nil to disable telemetry.
func WithTelemetry(t *TelemetryConfig) Option {
	return func(c *Config) {
		c.Telemetry = t
	}
}
