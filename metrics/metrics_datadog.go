package metrics

import (
	metricsLib "github.com/weeb-vip/go-metrics-lib"
	"github.com/weeb-vip/go-metrics-lib/clients/datadog"
	"github.com/weeb-vip/notifications-service/config"
)

var datadogInstance *datadog.DataDogClient

func NewMetricsInstanceD() metricsLib.MetricsImpl {
	cfg := config.LoadConfigOrPanic()
	if metricsInstance == nil {
		datadogInstance = datadog.NewDatadogClient(datadog.DataDogConfig{
			DD_AGENT_HOST: cfg.DataDogConfig.DD_AGENT_HOST,
			DD_AGENT_PORT: cfg.DataDogConfig.DD_AGENT_PORT,
		})
		initMetricsD(datadogInstance)
		metricsInstance = metricsLib.NewMetrics(datadogInstance, 1)
	}
	return metricsInstance
}

func NewDatadogInstance() *datadog.DataDogClient {
	cfg := config.LoadConfigOrPanic()
	if datadogInstance == nil {
		datadogInstance = datadog.NewDatadogClient(datadog.DataDogConfig{
			DD_AGENT_HOST: cfg.DataDogConfig.DD_AGENT_HOST,
			DD_AGENT_PORT: cfg.DataDogConfig.DD_AGENT_PORT,
		})
		initMetricsD(datadogInstance)
	}
	return datadogInstance
}

func initMetricsD(datadogInstance *datadog.DataDogClient) {
	cfg := config.LoadConfigOrPanic()
	datadogInstance.CreateHistogram("resolver_request_duration_histogram_milliseconds", []float64{
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
	},
		map[string]string{
			"service":  cfg.AppConfig.APPName,
			"protocol": "",
			"resolver": "",
			"result":   metricsLib.Success,
		},
		1)

	datadogInstance.CreateHistogram("database_query_duration_histogram_milliseconds", []float64{
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
	},
		map[string]string{
			"service": cfg.AppConfig.APPName,
			"table":   "",
			"method":  "",
			"result":  "",
		},
		1)
}
