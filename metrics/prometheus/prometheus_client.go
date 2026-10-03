package prometheus

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
)

type MetricWithLabelNames[T any] struct {
	Labels []string
	Metric T
}

type PrometheusClient struct {
	HistogramVecs map[string]MetricWithLabelNames[*prometheus.HistogramVec]
	CounterVecs   map[string]MetricWithLabelNames[*prometheus.CounterVec]
	GaugeVecs     map[string]MetricWithLabelNames[*prometheus.GaugeVec]
	SummaryVecs   map[string]MetricWithLabelNames[*prometheus.SummaryVec]
}

func NewPrometheusClient() *PrometheusClient {
	return &PrometheusClient{}
}

func (p *PrometheusClient) ServerHandler() {
	http.Handle("/metrics", promhttp.Handler())
	_ = http.ListenAndServe(":2112", nil)
}

func (p *PrometheusClient) Handler() http.Handler {
	return promhttp.Handler()
}

func (p *PrometheusClient) CreateHistogramVec(name string, help string, labelNames []string, buckets []float64) error {
	if p.HistogramVecs == nil {
		p.HistogramVecs = make(map[string]MetricWithLabelNames[*prometheus.HistogramVec])
	}

	if _, ok := p.HistogramVecs[name]; ok {
		return nil
	}
	p.HistogramVecs[name] = MetricWithLabelNames[*prometheus.HistogramVec]{
		Metric: promauto.NewHistogramVec(prometheus.HistogramOpts{
			Name:    name,
			Help:    help,
			Buckets: buckets,
		}, labelNames),
		Labels: labelNames,
	}
	return nil
}

func (p *PrometheusClient) CreateCounterVec(name string, help string, labelNames []string) error {
	if p.CounterVecs == nil {
		p.CounterVecs = make(map[string]MetricWithLabelNames[*prometheus.CounterVec])
	}

	if _, ok := p.CounterVecs[name]; ok {
		return nil
	}
	p.CounterVecs[name] = MetricWithLabelNames[*prometheus.CounterVec]{
		Metric: promauto.NewCounterVec(prometheus.CounterOpts{
			Name: name,
			Help: help,
		}, labelNames),
		Labels: labelNames,
	}
	return nil
}

func (p *PrometheusClient) CreateGaugeVec(name string, help string, labelNames []string) error {
	if p.GaugeVecs == nil {
		p.GaugeVecs = make(map[string]MetricWithLabelNames[*prometheus.GaugeVec])
	}

	if _, ok := p.GaugeVecs[name]; ok {
		return nil
	}
	p.GaugeVecs[name] = MetricWithLabelNames[*prometheus.GaugeVec]{
		Metric: promauto.NewGaugeVec(prometheus.GaugeOpts{
			Name: name,
			Help: help,
		}, labelNames),
		Labels: labelNames,
	}
	return nil
}

func (p *PrometheusClient) CreateSummaryVec(name string, help string, labelNames []string) error {
	if p.SummaryVecs == nil {
		p.SummaryVecs = make(map[string]MetricWithLabelNames[*prometheus.SummaryVec])
	}

	if _, ok := p.SummaryVecs[name]; ok {
		return nil
	}
	p.SummaryVecs[name] = MetricWithLabelNames[*prometheus.SummaryVec]{
		Metric: promauto.NewSummaryVec(prometheus.SummaryOpts{
			Name: name,
			Help: help,
		}, labelNames),
		Labels: labelNames,
	}
	return nil
}

func (p *PrometheusClient) Histogram(name string, value float64, labels map[string]string, rate float64) error {
	labelNames := make([]string, 0, len(labels))
	labelValues := make([]string, 0, len(labels))
	for k := range labels {
		labelNames = append(labelNames, k)
	}

	if _, ok := p.HistogramVecs[name]; !ok {
		_ = p.CreateHistogramVec(name, "", labelNames, nil)
	}

	for _, labelName := range p.HistogramVecs[name].Labels {
		labelValues = append(labelValues, labels[labelName])
	}

	p.HistogramVecs[name].Metric.WithLabelValues(labelValues...).Observe(value)
	return nil
}

func (p *PrometheusClient) Count(name string, labels map[string]string, rate float64) error {
	labelNames := make([]string, 0, len(labels))
	labelValues := make([]string, 0, len(labels))
	for k := range labels {
		labelNames = append(labelNames, k)
	}
	if _, ok := p.CounterVecs[name]; !ok {
		_ = p.CreateCounterVec(name, "", labelNames)
	}

	for _, labelName := range p.CounterVecs[name].Labels {
		labelValues = append(labelValues, labels[labelName])
	}

	p.CounterVecs[name].Metric.WithLabelValues(labelValues...).Inc()
	return nil
}

func (p *PrometheusClient) Gauge(name string, value float64, labels map[string]string, rate float64) error {
	labelNames := make([]string, 0, len(labels))
	labelValues := make([]string, 0, len(labels))
	for k := range labels {
		labelNames = append(labelNames, k)
	}
	if _, ok := p.GaugeVecs[name]; !ok {
		_ = p.CreateGaugeVec(name, "", labelNames)
	}

	for _, labelName := range p.GaugeVecs[name].Labels {
		labelValues = append(labelValues, labels[labelName])
	}

	p.GaugeVecs[name].Metric.WithLabelValues(labelValues...).Set(value)
	return nil
}

func (p *PrometheusClient) Summary(name string, value float64, labels map[string]string, rate float64) error {
	labelNames := make([]string, 0, len(labels))
	labelValues := make([]string, 0, len(labels))
	for k := range labels {
		labelNames = append(labelNames, k)
	}
	if _, ok := p.SummaryVecs[name]; !ok {
		_ = p.CreateSummaryVec(name, "", labelNames)
	}

	for _, labelName := range p.SummaryVecs[name].Labels {
		labelValues = append(labelValues, labels[labelName])
	}

	p.SummaryVecs[name].Metric.WithLabelValues(labelValues...).Observe(value)
	return nil
}
