package httpclient

import "context"

// Logger is the interface that any logger implementation must satisfy.
// This allows the client to be agnostic about the logging implementation.
//
// The client does NOT call Close(). The caller who creates the logger
// is responsible for its lifecycle.
//
// Example adapter for loki-logger-go:
//
//	// loki.Logger already satisfies this interface, pass it directly:
//	httpClient, _ := httpclient.New(
//	    httpclient.DefaultConfig(),
//	    httpclient.WithLogger(lokiLogger),
//	)
type Logger interface {
	// Info logs an informational message with optional fields
	Info(ctx context.Context, msg string, fields map[string]any)

	// Error logs an error message with optional fields
	Error(ctx context.Context, msg string, fields map[string]any)

	// Warn logs a warning message with optional fields
	Warn(ctx context.Context, msg string, fields map[string]any)

	// Debug logs a debug message with optional fields
	Debug(ctx context.Context, msg string, fields map[string]any)

	// Close closes the logger and flushes any pending logs.
	// Note: This should be called by the logger creator (usually in main()),
	// not by the platform. Use defer logger.Close() after creating the logger.
	// Returns an error if the logger fails to close or flush properly.
	Close() error
}
