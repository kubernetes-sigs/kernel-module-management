package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	runtimemetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// When adding metric names, see https://prometheus.io/docs/practices/naming/#metric-names
const (
	existingKMMOModulesQuery = "kmmo_module_total"
)

//go:generate mockgen -source=metrics.go -package=metrics -destination=mock_metrics_api.go

// Metrics is an interface representing a prometheus client for the Special Resource Operator
type Metrics interface {
	Register()
	SetExistingKMMOModules(value int)
}

type metrics struct {
	kmmoResourcesNum prometheus.Gauge
}

func New() Metrics {

	kmmoResourcesNum := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: existingKMMOModulesQuery,
			Help: "Number of existing KMMO Modules",
		},
	)
	return &metrics{kmmoResourcesNum: kmmoResourcesNum}
}

func (m *metrics) Register() {
	runtimemetrics.Registry.MustRegister(
		m.kmmoResourcesNum,
	)
}

func (m *metrics) SetExistingKMMOModules(value int) {
	m.kmmoResourcesNum.Set(float64(value))
}
