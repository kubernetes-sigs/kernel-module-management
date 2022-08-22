package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	runtimemetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// When adding metric names, see https://prometheus.io/docs/practices/naming/#metric-names
const (
	existingKMMOModulesQuery = "kmmo_module_total"
	completedKMMOStageQuery  = "kmmo_completed_stage"
	BuildStage               = "build"
	ModuleLoaderStage        = "module-loader"
	DevicePluginStage        = "device-plugin"
)

//go:generate mockgen -source=metrics.go -package=metrics -destination=mock_metrics_api.go

// Metrics is an interface representing a prometheus client for the Special Resource Operator
type Metrics interface {
	Register()
	SetExistingKMMOModules(value int)
	SetCompletedStage(kmmoName, kmmoNamespace, kernelVersion, stage string, completed bool)
}

type metrics struct {
	kmmoResourcesNum   prometheus.Gauge
	kmmoCompletedStage *prometheus.GaugeVec
}

func New() Metrics {

	kmmoResourcesNum := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: existingKMMOModulesQuery,
			Help: "Number of existing KMMO Modules",
		},
	)
	completedStages := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: completedKMMOStageQuery,
			Help: "For a given kmmo,namespace, kernel version, stage(device-plugin, module-loader, build), 1 if the stage is completed, 0 if it is not.",
		},
		[]string{"kmmo", "namespace", "kernel", "stage"},
	)

	return &metrics{
		kmmoResourcesNum:   kmmoResourcesNum,
		kmmoCompletedStage: completedStages,
	}
}

func (m *metrics) Register() {
	runtimemetrics.Registry.MustRegister(
		m.kmmoResourcesNum,
		m.kmmoCompletedStage,
	)
}

func (m *metrics) SetExistingKMMOModules(value int) {
	m.kmmoResourcesNum.Set(float64(value))
}

func (m *metrics) SetCompletedStage(kmmoName, kmmoNamespace, kernelVersion, stage string, completed bool) {
	var value float64
	if completed {
		value = 1
	}
	m.kmmoCompletedStage.WithLabelValues(kmmoName, kmmoNamespace, kernelVersion, stage).Set(value)
}
