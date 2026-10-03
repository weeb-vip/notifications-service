package middleware

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"

	"github.com/weeb-vip/notifications-service/internal/logger"
	"github.com/weeb-vip/notifications-service/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// SelectiveGzipMiddleware handles both request decompression and response compression
// but excludes specific paths like /metrics from compression
func SelectiveGzipMiddleware(excludePaths ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check if this path should be excluded from compression
			for _, path := range excludePaths {
				if r.URL.Path == path {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Apply regular gzip middleware
			gzipHandler := GzipMiddleware()
			gzipHandler(next).ServeHTTP(w, r)
		})
	}
}

// GzipMiddleware handles both request decompression and response compression
func GzipMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			log := logger.FromCtx(ctx)

			// Add tracing for compression operations
			tracer := tracing.GetTracer(ctx)
			ctx, span := tracer.Start(ctx, "GzipMiddleware",
				trace.WithAttributes(
					attribute.String("http.middleware", "gzip"),
					attribute.String("request.content_encoding", r.Header.Get("Content-Encoding")),
					attribute.String("request.accept_encoding", r.Header.Get("Accept-Encoding")),
				),
				trace.WithSpanKind(trace.SpanKindInternal),
				tracing.GetEnvironmentAttribute(),
			)
			defer span.End()

			requestCompressed := false
			responseCompressed := false

			// Handle compressed request bodies
			if r.Header.Get("Content-Encoding") == "gzip" {
				gzipReader, err := gzip.NewReader(r.Body)
				if err != nil {
					span.RecordError(err)
					log.Error().Err(err).Msg("Failed to decompress gzip request")
					http.Error(w, "Invalid gzip data", http.StatusBadRequest)
					return
				}
				defer gzipReader.Close()
				r.Body = gzipReader
				requestCompressed = true
				log.Debug().Msg("Decompressed gzip request body")
			}

			// Check if client accepts gzip encoding for response
			if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				span.SetAttributes(
					attribute.Bool("compression.request_compressed", requestCompressed),
					attribute.Bool("compression.response_compressed", false),
				)
				r = r.WithContext(ctx)
				next.ServeHTTP(w, r)
				return
			}

			// Wrap response writer with gzip compression
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Vary", "Accept-Encoding")
			responseCompressed = true

			gzipWriter := gzip.NewWriter(w)
			defer gzipWriter.Close()

			gzipResponseWriter := &gzipResponseWriter{
				ResponseWriter: w,
				Writer:         gzipWriter,
			}

			span.SetAttributes(
				attribute.Bool("compression.request_compressed", requestCompressed),
				attribute.Bool("compression.response_compressed", responseCompressed),
			)

			log.Debug().
				Bool("request_compressed", requestCompressed).
				Bool("response_compressed", responseCompressed).
				Msg("Gzip compression applied")

			r = r.WithContext(ctx)
			next.ServeHTTP(gzipResponseWriter, r)
		})
	}
}

// gzipResponseWriter wraps http.ResponseWriter to provide gzip compression
type gzipResponseWriter struct {
	http.ResponseWriter
	Writer io.Writer
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.Writer.Write(b)
}

func (w *gzipResponseWriter) Flush() {
	if flusher, ok := w.Writer.(*gzip.Writer); ok {
		flusher.Flush()
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
