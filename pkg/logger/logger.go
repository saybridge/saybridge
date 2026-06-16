// Package logger provides a centralized, structured logging solution using zerolog.
// This replaces scattered log.Printf calls with structured, leveled logging
// that includes component context and optional request correlation IDs.
package logger

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/rs/zerolog"
)

// ctxKey is the context key type for storing the logger.
type ctxKey struct{}

// requestIDKey is the context key for request IDs.
type requestIDKey struct{}

// Config holds logger configuration.
type Config struct {
	Level  string // "debug", "info", "warn", "error" (default: "info")
	Pretty bool   // Use human-readable console output (default: false = JSON)
}

// New creates a new zerolog.Logger with the given configuration.
func New(cfg Config) zerolog.Logger {
	var output io.Writer = os.Stdout

	if cfg.Pretty {
		output = zerolog.ConsoleWriter{
			Out:        os.Stdout,
			TimeFormat: time.RFC3339,
		}
	}

	level := zerolog.InfoLevel
	switch cfg.Level {
	case "debug":
		level = zerolog.DebugLevel
	case "warn":
		level = zerolog.WarnLevel
	case "error":
		level = zerolog.ErrorLevel
	case "trace":
		level = zerolog.TraceLevel
	}

	return zerolog.New(output).
		With().
		Timestamp().
		Caller().
		Logger().
		Level(level)
}

// Default returns a default logger suitable for development.
func Default() zerolog.Logger {
	return New(Config{Level: "info", Pretty: true})
}

// Component returns a sub-logger with a component field.
// Usage: logger.Component("Hub").Info().Msg("Client connected")
func Component(base zerolog.Logger, name string) zerolog.Logger {
	return base.With().Str("component", name).Logger()
}

// WithContext stores the logger in the context.
func WithContext(ctx context.Context, logger zerolog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, logger)
}

// FromContext retrieves the logger from context, or returns a default.
func FromContext(ctx context.Context) zerolog.Logger {
	if l, ok := ctx.Value(ctxKey{}).(zerolog.Logger); ok {
		return l
	}
	return Default()
}

// WithRequestID stores a request ID in the context for correlation.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, requestID)
}

// RequestID retrieves the request ID from context.
func RequestID(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey{}).(string); ok {
		return id
	}
	return ""
}
