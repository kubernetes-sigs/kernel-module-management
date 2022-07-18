package module

import (
	"context"

	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/internal/daemonset"
	"github.com/qbarrand/oot-operator/internal/metrics"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen -source=statusupdater.go -package=module -destination=mock_statusupdater.go

type StatusUpdater interface {
	UpdateModuleStatus(ctx context.Context,
		mod *ootov1alpha1.Module,
		kernelMappingNodes []v1.Node,
		targetedNodes []v1.Node,
		dsByKernelVersion map[string]*appsv1.DaemonSet) error
}

type statusUpdater struct {
	client     client.Client
	daemonAPI  daemonset.DaemonSetCreator
	metricsAPI metrics.Metrics
}

func NewStatusUpdater(client client.Client, daemonAPI daemonset.DaemonSetCreator, metricsAPI metrics.Metrics) StatusUpdater {
	return &statusUpdater{
		client:     client,
		daemonAPI:  daemonAPI,
		metricsAPI: metricsAPI,
	}
}

func (s *statusUpdater) UpdateModuleStatus(ctx context.Context,
	mod *ootov1alpha1.Module,
	kernelMappingNodes []v1.Node,
	targetedNodes []v1.Node,
	dsByKernelVersion map[string]*appsv1.DaemonSet) error {

	nodesMatchingSelectorNumber := int32(len(targetedNodes))
	numDesired := int32(len(kernelMappingNodes))
	var numAvailableDevicePlugin int32
	var numAvailableKernelModule int32
	for kernelVersion, ds := range dsByKernelVersion {
		if daemonset.IsDevicePluginKernelVersion(kernelVersion) {
			numAvailableDevicePlugin += ds.Status.NumberAvailable
		} else {
			numAvailableKernelModule += ds.Status.NumberAvailable
		}
	}
	mod.Status.DriverContainer.NodesMatchingSelectorNumber = nodesMatchingSelectorNumber
	mod.Status.DriverContainer.DesiredNumber = numDesired
	mod.Status.DriverContainer.AvailableNumber = numAvailableKernelModule
	if mod.Spec.DevicePlugin != nil {
		mod.Status.DevicePlugin.NodesMatchingSelectorNumber = nodesMatchingSelectorNumber
		mod.Status.DevicePlugin.DesiredNumber = numDesired
		mod.Status.DevicePlugin.AvailableNumber = numAvailableDevicePlugin
	}
	s.updateMetrics(ctx, mod, dsByKernelVersion)
	return s.client.Status().Update(ctx, mod)
}

func (s *statusUpdater) updateMetrics(ctx context.Context, mod *ootov1alpha1.Module, dsByKernelVersion map[string]*appsv1.DaemonSet) {
	for kernelVersion, ds := range dsByKernelVersion {
		stage := metrics.DriverContainerStage
		if daemonset.IsDevicePluginKernelVersion(kernelVersion) {
			stage = metrics.DevicePluginStage
		}
		s.metricsAPI.SetCompletedStage(mod.Name,
			mod.Namespace,
			kernelVersion,
			stage,
			ds.Status.DesiredNumberScheduled == ds.Status.NumberAvailable)
	}
}
