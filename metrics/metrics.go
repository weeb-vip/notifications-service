package metrics

import (
	metricsLib "github.com/weeb-vip/go-metrics-lib"
	"github.com/weeb-vip/go-metrics-lib/clients/prometheus"
	"github.com/weeb-vip/notifications-service/config"
)

var metricsInstance metricsLib.MetricsImpl

var prometheusInstance *prometheus.PrometheusClient

func NewMetricsInstance() metricsLib.MetricsImpl {
	if metricsInstance == nil {
		prometheusInstance = NewPrometheusInstance()
		initMetrics(prometheusInstance)
		metricsInstance = metricsLib.NewMetrics(prometheusInstance, 1)
	}
	return metricsInstance
}

func NewPrometheusInstance() *prometheus.PrometheusClient {
	if prometheusInstance == nil {
		prometheusInstance = prometheus.NewPrometheusClient()
		initMetrics(prometheusInstance)
	}
	return prometheusInstance
}

func initMetrics(prometheusInstance *prometheus.PrometheusClient) {
	prometheusInstance.CreateHistogramVec("resolver_request_duration_histogram_milliseconds", "graphql resolver millisecond", []string{"service", "protocol", "resolver", "result", "env"}, []float64{
		// create buckets 10000 split into 10 buckets
		100,
		200,
		300,
		400,
		500,
		600,
		700,
		800,
		900,
		1000,
	})

	prometheusInstance.CreateHistogramVec("database_query_duration_histogram_milliseconds", "database calls millisecond", []string{"service", "table", "method", "result", "env"}, []float64{
		// create buckets 10000 split into 10 buckets
		100,
		200,
		300,
		400,
		500,
		600,
		700,
		800,
		900,
		1000,
	})
}

func GetCurrentEnv() string {
	cfg := config.LoadConfigOrPanic()
	return cfg.AppConfig.Env
}

func GetServiceName() string {
	cfg := config.LoadConfigOrPanic()
	return cfg.AppConfig.APPName
}
