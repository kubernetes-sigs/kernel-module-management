package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	runtimemetrics "sigs.k8s.io/controller-runtime/pkg/metrics"
)

// When adding metric names, see https://prometheus.io/docs/practices/naming/#metric-names
const (
	kmmModulesQuery         = "kmm_module_num"
	kmmInClusterBuildQuery  = "kmm_in_cluster_build_num"
	kmmInClusterSignQuery   = "kmm_in_cluster_sign_num"
	kmmDevicePluginQuery    = "kmm_device_plugin_num"
	kmmPreflightQuery       = "kmm_preflight_num"
	kmmModprobeArgsQuery    = "kmm_modprobe_args"
	kmmModprobeRawArgsQuery = "kmm_modprobe_raw_args"
)

//go:generate mockgen -source=metrics.go -package=metrics -destination=mock_metrics_api.go

// Metrics is an interface representing a prometheus client for the Kernel Module Management Operator
type Metrics interface {
	Register()
	SetKMMModulesNum(value int)
	SetKMMInClusterBuildNum(value int)
	SetKMMInClusterSignNum(value int)
	SetKMMDevicePluginNum(value int)
	SetKMMPreflightsNum(value int)
	SetKMMModprobeArgs(modName, namespace, modprobeArgs string)
	SetKMMModprobeRawArgs(modName, namespace, modprobeArgs string)
}

type metrics struct {
	kmmModuleResourcesNum       prometheus.Gauge
	kmmInClusterBuildNum        prometheus.Gauge
	kmmInClusterSignNum         prometheus.Gauge
	kmmDevicePluginResourcesNum prometheus.Gauge
	kmmPreflightResourceNum     prometheus.Gauge
	kmmModprobeArgs             *prometheus.GaugeVec
	kmmModprobeRawArgs          *prometheus.GaugeVec
}

func New() Metrics {

	kmmModuleResourcesNum := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: kmmModulesQuery,
			Help: "Number of existing KMMO Modules",
		},
	)

	kmmInClusterBuildNum := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: kmmInClusterBuildQuery,
			Help: "Number of existing KMMO Modules with in-cluster Build defined",
		},
	)

	kmmInClusterSignNum := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: kmmInClusterSignQuery,
			Help: "Number of existing KMMO Modules with in-cluster Sign defined",
		},
	)

	kmmDevicePluginResourcesNum := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: kmmDevicePluginQuery,
			Help: "Number of existing KMMO Modules with DevicePlugin defined",
		},
	)

	kmmPreflightResourceNum := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: kmmPreflightQuery,
			Help: "Number of existing KMMO Preflights",
		},
	)

	kmmModprobeArgs := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: kmmModprobeArgsQuery,
			Help: "for a given kernel version, describe which modprobe args used (if at all)",
		},
		[]string{"name", "namespace", "modprobeArgs"},
	)

	kmmModprobeRawArgs := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: kmmModprobeRawArgsQuery,
			Help: "for a given kernel version, describe which modprobe raw args used (if at all)",
		},
		[]string{"name", "namespace", "modprobeRawArgs"},
	)

	return &metrics{
		kmmModuleResourcesNum:       kmmModuleResourcesNum,
		kmmInClusterBuildNum:        kmmInClusterBuildNum,
		kmmInClusterSignNum:         kmmInClusterSignNum,
		kmmDevicePluginResourcesNum: kmmDevicePluginResourcesNum,
		kmmPreflightResourceNum:     kmmPreflightResourceNum,
		kmmModprobeArgs:             kmmModprobeArgs,
		kmmModprobeRawArgs:          kmmModprobeRawArgs,
	}
}

func (m *metrics) Register() {
	runtimemetrics.Registry.MustRegister(
		m.kmmModuleResourcesNum,
		m.kmmInClusterBuildNum,
		m.kmmInClusterSignNum,
		m.kmmDevicePluginResourcesNum,
		m.kmmPreflightResourceNum,
		m.kmmModprobeArgs,
	)
}

func (m *metrics) SetKMMModulesNum(value int) {
	m.kmmModuleResourcesNum.Set(float64(value))
}

func (m *metrics) SetKMMInClusterBuildNum(value int) {
	m.kmmInClusterBuildNum.Set(float64(value))
}

func (m *metrics) SetKMMInClusterSignNum(value int) {
	m.kmmInClusterSignNum.Set(float64(value))
}

func (m *metrics) SetKMMDevicePluginNum(value int) {
	m.kmmDevicePluginResourcesNum.Set(float64(value))
}

func (m *metrics) SetKMMPreflightsNum(value int) {
	m.kmmPreflightResourceNum.Set(float64(value))
}

func (m *metrics) SetKMMModprobeArgs(modName, namespace, modprobeArgs string) {
	m.kmmModprobeArgs.WithLabelValues(modName, namespace, modprobeArgs).Set(float64(1))
}

func (m *metrics) SetKMMModprobeRawArgs(modName, namespace, modprobeRawArgs string) {
	m.kmmModprobeRawArgs.WithLabelValues(modName, namespace, modprobeRawArgs).Set(float64(1))
}
