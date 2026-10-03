package logger

import (
	"context"
	"sync"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"go.opentelemetry.io/otel/trace"
)

type ctxKey struct{}

var once sync.Once
var globalLogger zerolog.Logger

// Logger initializes and configures the global logger with options
func Logger(opts ...Option) {
	once.Do(func() {
		config := &Config{
			ServiceName:    "unknown-service",
			ServiceVersion: "unknown-version",
		}

		for _, opt := range opts {
			opt(config)
		}

		globalLogger = log.With().
			Str("service", config.ServiceName).
			Str("version", config.ServiceVersion).
			Str("environment", config.Environment).
			Logger()
	})
}

// Get returns the global logger instance
func Get() zerolog.Logger {
	return globalLogger
}

// FromCtx returns the Logger associated with the ctx. If no logger
// is associated, the global logger is returned. Automatically includes trace context.
func FromCtx(ctx context.Context) zerolog.Logger {
	var logger zerolog.Logger
	if l, ok := ctx.Value(ctxKey{}).(zerolog.Logger); ok {
		logger = l
	} else {
		logger = globalLogger
	}

	// Add trace context if available
	return withTraceContext(ctx, logger)
}

// withTraceContext adds trace and span IDs to the logger if available in context
func withTraceContext(ctx context.Context, logger zerolog.Logger) zerolog.Logger {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return logger
	}

	spanContext := span.SpanContext()
	return logger.With().
		Str("trace_id", spanContext.TraceID().String()).
		Str("span_id", spanContext.SpanID().String()).
		Logger()
}

// FromContext is an alias for FromCtx for backward compatibility
func FromContext(ctx context.Context) zerolog.Logger {
	return FromCtx(ctx)
}

// WithCtx returns a copy of ctx with the Logger attached.
func WithCtx(ctx context.Context, l zerolog.Logger) context.Context {
	return context.WithValue(ctx, ctxKey{}, l)
}

// WithContext returns a copy of ctx with the Logger attached.
func WithContext(logger zerolog.Logger, ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKey{}, logger)
}

// Config holds logger configuration
type Config struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
}

// Option configures the logger
type Option func(*Config)

// WithServerName sets the service name
func WithServerName(name string) Option {
	return func(c *Config) {
		c.ServiceName = name
	}
}

// WithVersion sets the service version
func WithVersion(version string) Option {
	return func(c *Config) {
		c.ServiceVersion = version
	}
}

// WithEnvironment sets the environment
func WithEnvironment(env string) Option {
	return func(c *Config) {
		c.Environment = env
	}
}
