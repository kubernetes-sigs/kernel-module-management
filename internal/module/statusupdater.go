package module

import (
	"context"
	"fmt"

	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/internal/daemonset"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen -source=statusupdater.go -package=module -destination=mock_statusupdater.go

type StatusUpdater interface {
	UpdateModuleStatus(ctx context.Context, mod *ootov1alpha1.Module, kernelMappingNodes []v1.Node, targetedNodes []v1.Node) error
}

type statusUpdater struct {
	client    client.Client
	daemonAPI daemonset.DaemonSetCreator
}

func NewStatusUpdater(client client.Client, daemonAPI daemonset.DaemonSetCreator) StatusUpdater {
	return &statusUpdater{
		client:    client,
		daemonAPI: daemonAPI,
	}
}

func (s *statusUpdater) UpdateModuleStatus(ctx context.Context, mod *ootov1alpha1.Module, kernelMappingNodes []v1.Node, targetedNodes []v1.Node) error {
	dsList, err := s.daemonAPI.ModuleDaemonSets(ctx, mod.Name, mod.Namespace)
	if err != nil {
		return fmt.Errorf("failed to get all module's daemonsets: %w", err)
	}
	original := mod.DeepCopy()
	nodesMatchingSelectorNumber := int32(len(targetedNodes))
	numDesired := int32(len(kernelMappingNodes))
	var numAvailableDevicePlugin int32
	var numAvailableKernelModule int32
	for _, ds := range dsList {
		if s.daemonAPI.IsDevicePluginDaemonSet(ds) {
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
	return s.client.Status().Patch(ctx, mod, client.MergeFrom(original))
}
