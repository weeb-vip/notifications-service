package tracing

import "os"

// TracingConfig contains configuration for the tracing system
type TracingConfig struct {
	// Endpoint is the OTLP endpoint for Tempo (default: localhost:4317)
	Endpoint string
	// Insecure indicates whether to use an insecure connection (default: true for local development)
	Insecure bool
	// ServiceVersion is the version of the service
	ServiceVersion string
}

// GetTracingConfig returns the tracing configuration from environment variables
func GetTracingConfig() TracingConfig {
	config := TracingConfig{
		Endpoint: os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"),
		Insecure: true, // Default to insecure for local development
		ServiceVersion: os.Getenv("SERVICE_VERSION"),
	}

	// Set defaults if not provided
	if config.Endpoint == "" {
		config.Endpoint = "localhost:4317"
	}

	if config.ServiceVersion == "" {
		config.ServiceVersion = "1.0.0"
	}

	// Check if we should use secure connection
	if os.Getenv("OTEL_EXPORTER_OTLP_INSECURE") == "false" {
		config.Insecure = false
	}

	return config
}