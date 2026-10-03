package middleware

import (
	"net/http"

	"github.com/weeb-vip/notifications-service/internal/logger"
	"github.com/weeb-vip/notifications-service/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TracingMiddleware creates HTTP tracing middleware
func TracingMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract tracing context from headers if present
			ctx := r.Context()

			// Extract any existing trace context from headers
			propagator := propagation.TraceContext{}
			ctx = propagator.Extract(ctx, propagation.HeaderCarrier(r.Header))

			// Start a new span for this HTTP request
			tracer := tracing.GetTracer(ctx)
			ctx, span := tracer.Start(ctx, "HTTP "+r.Method+" "+r.URL.Path,
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.url", r.URL.String()),
					attribute.String("http.scheme", r.URL.Scheme),
					attribute.String("http.host", r.Host),
					attribute.String("http.target", r.URL.Path),
					attribute.String("user_agent.original", r.UserAgent()),
				),
				trace.WithSpanKind(trace.SpanKindServer),
				tracing.GetEnvironmentAttribute(),
			)
			defer span.End()

			// Add trace context to response headers for client correlation
			propagator.Inject(ctx, propagation.HeaderCarrier(w.Header()))

			// Create a response wrapper to capture status code
			wrapped := &responseWrapper{ResponseWriter: w, statusCode: 200}

			// Add span context to the request context
			r = r.WithContext(ctx)

			// Log the incoming request with trace context
			log := logger.FromCtx(ctx)
			log.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Str("remote_addr", r.RemoteAddr).
				Str("user_agent", r.UserAgent()).
				Msg("HTTP request started")

			// Process the request
			next.ServeHTTP(wrapped, r)

			// Set final span attributes
			span.SetAttributes(
				attribute.Int("http.status_code", wrapped.statusCode),
			)

			// Log the response with trace context
			log.Info().
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status_code", wrapped.statusCode).
				Msg("HTTP request completed")
		})
	}
}

// responseWrapper wraps http.ResponseWriter to capture status code
type responseWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWrapper) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWrapper) Write(b []byte) (int, error) {
	return rw.ResponseWriter.Write(b)
}
