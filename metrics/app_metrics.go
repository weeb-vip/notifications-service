package metrics

import (
	"github.com/weeb-vip/notifications-service/config"
	metricsLib "github.com/weeb-vip/go-metrics-lib"
)

// AppMetrics provides a centralized metrics interface with default tags
type AppMetrics struct {
	metricsImpl metricsLib.MetricsImpl
	defaultTags map[string]string
}

// Global instance
var appMetrics *AppMetrics

// GetAppMetrics returns the singleton metrics instance
func GetAppMetrics() *AppMetrics {
	if appMetrics == nil {
		cfg := config.LoadConfigOrPanic()

		// Initialize with Prometheus metrics
		impl := NewMetricsInstance()

		appMetrics = &AppMetrics{
			metricsImpl: impl,
			defaultTags: map[string]string{
				"service": cfg.AppConfig.APPName,
				"env":     cfg.AppConfig.Env,
				"version": cfg.AppConfig.Version,
			},
		}
	}
	return appMetrics
}

// ResolverMetric records resolver performance metrics
func (m *AppMetrics) ResolverMetric(duration float64, resolver string, result string) {
	labels := metricsLib.ResolverMetricLabels{
		Resolver: resolver,
		Service:  m.defaultTags["service"],
		Protocol: "graphql",
		Result:   result,
		Env:      m.defaultTags["env"],
	}
	m.metricsImpl.ResolverMetric(duration, labels)
}

// DatabaseMetric records database operation metrics
func (m *AppMetrics) DatabaseMetric(duration float64, table string, method string, result string) {
	labels := metricsLib.DatabaseMetricLabels{
		Service: m.defaultTags["service"],
		Table:   table,
		Method:  method,
		Result:  result,
		Env:     m.defaultTags["env"],
	}
	m.metricsImpl.DatabaseMetric(duration, labels)
}

// RepositoryMetric records repository operation metrics
func (m *AppMetrics) RepositoryMetric(duration float64, repository string, method string, result string) {
	// Use database metric with repository name as table for now
	m.DatabaseMetric(duration, repository, method, result)
}

// ServiceMetric records service operation metrics
func (m *AppMetrics) ServiceMetric(duration float64, service string, method string, result string) {
	// Use database metric with service name as table for now
	m.DatabaseMetric(duration, service, method, result)
}

// GetDefaultTags returns the default tags for this metrics instance
func (m *AppMetrics) GetDefaultTags() map[string]string {
	// Return a copy to prevent modification
	tags := make(map[string]string)
	for k, v := range m.defaultTags {
		tags[k] = v
	}
	return tags
}

// WithTags returns a new metrics instance with additional tags
func (m *AppMetrics) WithTags(additionalTags map[string]string) *AppMetrics {
	newTags := make(map[string]string)

	// Copy default tags
	for k, v := range m.defaultTags {
		newTags[k] = v
	}

	// Add additional tags (will override defaults if same key)
	for k, v := range additionalTags {
		newTags[k] = v
	}

	return &AppMetrics{
		metricsImpl: m.metricsImpl,
		defaultTags: newTags,
	}
}