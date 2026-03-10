package httpclient

import (
	"errors"
	"fmt"
)

// ConfigError represents a validation error in the client configuration.
type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("httpclient config error: field %q %s", e.Field, e.Message)
}

func NewConfigError(field, message string) *ConfigError {
	return &ConfigError{Field: field, Message: message}
}

// RequestError represents a failure executing an HTTP request.
type RequestError struct {
	Method string
	URL    string
	Err    error
}

func (e *RequestError) Error() string {
	return fmt.Sprintf("httpclient request error: %s %s: %v", e.Method, e.URL, e.Err)
}

func (e *RequestError) Unwrap() error {
	return e.Err
}

var ErrClientClosed = errors.New("http client is closed")

// ResponseDecodeError represents a failure decoding an HTTP response body.
type ResponseDecodeError struct {
	StatusCode int
	Err        error
}

func (e *ResponseDecodeError) Error() string {
	return fmt.Sprintf("httpclient decode error: failed to decode response (status %d): %v", e.StatusCode, e.Err)
}

func (e *ResponseDecodeError) Unwrap() error {
	return e.Err
}
