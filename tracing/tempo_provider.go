package tracing

import (
	"context"
	"fmt"

	"github.com/weeb-vip/go-tracing-lib/providers"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
)

// NewTempoProvider creates a new OpenTelemetry provider configured for Tempo
func NewTempoProvider(ctx context.Context, config providers.ProviderConfig, endpoint string, insecure bool, environment string) (*trace.TracerProvider, func(ctx context.Context) error, error) {
	// Configure OTLP exporter options
	opts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
	}

	if insecure {
		opts = append(opts, otlptracegrpc.WithInsecure())
	}

	// Create OTLP trace exporter
	traceExporter, err := otlptracegrpc.New(ctx, opts...)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(config.ServiceName),
			semconv.ServiceVersionKey.String(config.ServiceVersion),
			semconv.DeploymentEnvironmentKey.String(environment),
		),
		resource.WithSchemaURL(semconv.SchemaURL),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create trace provider
	traceProvider := trace.NewTracerProvider(
		trace.WithBatcher(traceExporter),
		trace.WithResource(res),
		trace.WithSampler(trace.AlwaysSample()), // Sample all traces for development
	)

	return traceProvider, func(ctx context.Context) error {
		return traceProvider.Shutdown(ctx)
	}, nil
}