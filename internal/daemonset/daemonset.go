package daemonset

import (
	"context"
	"errors"
	"fmt"

	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/internal/constants"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	kubeletDevicePluginsVolumeName = "kubelet-device-plugins"
	kubeletDevicePluginsPath       = "/var/lib/kubelet/device-plugins"
	nodeLibModulesPath             = "/lib/modules"
	nodeLibModulesVolumeName       = "node-lib-modules"
	nodeUsrLibModulesPath          = "/usr/lib/modules"
	nodeUsrLibModulesVolumeName    = "node-usr-lib-modules"
)

//go:generate mockgen -source=daemonset.go -package=daemonset -destination=mock_daemonset.go

type DaemonSetCreator interface {
	GarbageCollect(ctx context.Context, existingDS map[string]*appsv1.DaemonSet, validKernels sets.String) ([]string, error)
	ModuleDaemonSets(ctx context.Context, name, namespace string) ([]appsv1.DaemonSet, error)
	ModuleDaemonSetsByKernelVersion(ctx context.Context, name, namespace string) (map[string]*appsv1.DaemonSet, error)
	SetDriverContainerAsDesired(ctx context.Context, ds *appsv1.DaemonSet, image string, mod ootov1alpha1.Module, kernelVersion string) error
	SetDevicePluginAsDesired(ctx context.Context, ds *appsv1.DaemonSet, mod *ootov1alpha1.Module) error
	IsDevicePluginDaemonSet(ds appsv1.DaemonSet) bool
}

type daemonSetGenerator struct {
	client      client.Client
	kernelLabel string
	scheme      *runtime.Scheme
}

func NewCreator(client client.Client, kernelLabel string, scheme *runtime.Scheme) DaemonSetCreator {
	return &daemonSetGenerator{
		client:      client,
		kernelLabel: kernelLabel,
		scheme:      scheme,
	}
}

func (dc *daemonSetGenerator) GarbageCollect(ctx context.Context, existingDS map[string]*appsv1.DaemonSet, validKernels sets.String) ([]string, error) {
	deleted := make([]string, 0)

	for kernelVersion, ds := range existingDS {
		if !validKernels.Has(kernelVersion) {
			if err := dc.client.Delete(ctx, ds); err != nil {
				return nil, fmt.Errorf("could not delete DaemonSet %s: %v", ds.Name, err)
			}

			deleted = append(deleted, ds.Name)
		}
	}

	return deleted, nil
}

func (dc *daemonSetGenerator) ModuleDaemonSets(ctx context.Context, name, namespace string) ([]appsv1.DaemonSet, error) {
	dsList := appsv1.DaemonSetList{}
	opts := []client.ListOption{
		client.MatchingLabels(map[string]string{constants.ModuleNameLabel: name}),
		client.InNamespace(namespace),
	}
	if err := dc.client.List(ctx, &dsList, opts...); err != nil {
		return nil, fmt.Errorf("could not list DaemonSets: %v", err)
	}
	return dsList.Items, nil
}

func (dc *daemonSetGenerator) ModuleDaemonSetsByKernelVersion(ctx context.Context, name, namespace string) (map[string]*appsv1.DaemonSet, error) {
	dsList, err := dc.ModuleDaemonSets(ctx, name, namespace)
	if err != nil {
		return nil, fmt.Errorf("could not get all DaemonSets: %w", err)
	}

	dsByKernelVersion := make(map[string]*appsv1.DaemonSet, len(dsList))

	for i := 0; i < len(dsList); i++ {
		ds := dsList[i]

		kernelVersion := ds.Labels[dc.kernelLabel]
		if kernelVersion == "" {
			// this is a device plugin, skipping
			continue
		}

		if dsByKernelVersion[kernelVersion] != nil {
			return nil, fmt.Errorf("multiple DaemonSets found for kernel %q", kernelVersion)
		}

		dsByKernelVersion[kernelVersion] = &ds
	}

	return dsByKernelVersion, nil
}

func (dc *daemonSetGenerator) SetDriverContainerAsDesired(ctx context.Context, ds *appsv1.DaemonSet, image string, mod ootov1alpha1.Module, kernelVersion string) error {
	if ds == nil {
		return errors.New("ds cannot be nil")
	}

	if image == "" {
		return errors.New("image cannot be empty")
	}

	if kernelVersion == "" {
		return errors.New("kernelVersion cannot be empty")
	}

	standardLabels := map[string]string{
		constants.ModuleNameLabel: mod.Name,
		dc.kernelLabel:            kernelVersion,
	}

	nodeSelector := CopyMapStringString(mod.Spec.Selector)
	nodeSelector[dc.kernelLabel] = kernelVersion

	driverContainerVolumeMounts := []v1.VolumeMount{
		{
			Name:      nodeLibModulesVolumeName,
			ReadOnly:  true,
			MountPath: nodeLibModulesPath,
		},
		{
			Name:      nodeUsrLibModulesVolumeName,
			ReadOnly:  true,
			MountPath: nodeUsrLibModulesPath,
		},
	}

	hostPathDirectory := v1.HostPathDirectory
	volumes := []v1.Volume{
		{
			Name: nodeLibModulesVolumeName,
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: nodeLibModulesPath,
					Type: &hostPathDirectory,
				},
			},
		},
		{
			Name: nodeUsrLibModulesVolumeName,
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: nodeUsrLibModulesPath,
					Type: &hostPathDirectory,
				},
			},
		},
	}

	return dc.constructDaemonSet(ctx, ds, &mod, mod.Spec.DriverContainer, "driver-container", image, standardLabels,
		nodeSelector, driverContainerVolumeMounts, volumes, false)
}

func (dc *daemonSetGenerator) SetDevicePluginAsDesired(ctx context.Context, ds *appsv1.DaemonSet, mod *ootov1alpha1.Module) error {
	if ds == nil {
		return errors.New("ds cannot be nil")
	}

	if mod.Spec.DevicePlugin == nil {
		return errors.New("device plugin in module should not be nil")
	}

	standardLabels := map[string]string{
		constants.ModuleNameLabel: mod.Name,
	}

	nodeSelector := map[string]string{GetDriverContainerNodeLabel(mod.Name): ""}

	containerVolumeMounts := []v1.VolumeMount{
		{
			Name:      kubeletDevicePluginsVolumeName,
			MountPath: kubeletDevicePluginsPath,
		},
	}

	hostPathDirectory := v1.HostPathDirectory
	dsVolumes := []v1.Volume{
		{
			Name: kubeletDevicePluginsVolumeName,
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: kubeletDevicePluginsPath,
					Type: &hostPathDirectory,
				},
			},
		},
	}

	return dc.constructDaemonSet(ctx, ds, mod, *mod.Spec.DevicePlugin, "device-plugin", "", standardLabels,
		nodeSelector, containerVolumeMounts, dsVolumes, true)
}

func (dc *daemonSetGenerator) IsDevicePluginDaemonSet(ds appsv1.DaemonSet) bool {
	return ds.Labels[dc.kernelLabel] == ""
}

func (dc *daemonSetGenerator) constructDaemonSet(ctx context.Context,
	ds *appsv1.DaemonSet,
	mod *ootov1alpha1.Module,
	container v1.Container,
	containerName string,
	overrideContainerImage string,
	labels map[string]string,
	nodeSelector map[string]string,
	volumeMounts []v1.VolumeMount,
	volumes []v1.Volume,
	privilege bool) error {

	existingLabels := ds.GetLabels()

	if existingLabels == nil {
		existingLabels = make(map[string]string, len(labels))
	}

	for k, v := range labels {
		existingLabels[k] = v
	}

	ds.SetLabels(existingLabels)

	container.Name = containerName
	if overrideContainerImage != "" {
		container.Image = overrideContainerImage
	}

	container.VolumeMounts = append(container.VolumeMounts, volumeMounts...)
	if privilege {
		if container.SecurityContext == nil {
			container.SecurityContext = &v1.SecurityContext{}
		}
		container.SecurityContext.Privileged = pointer.Bool(true)
	}

	containers := []v1.Container{container}
	volumes = append(volumes, mod.Spec.AdditionalVolumes...)

	ds.Spec = appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: labels},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{Labels: labels},
			Spec: v1.PodSpec{
				NodeSelector:       nodeSelector,
				Containers:         containers,
				ServiceAccountName: mod.Spec.ServiceAccountName,
				Volumes:            volumes,
			},
		},
	}
	return controllerutil.SetControllerReference(mod, ds, dc.scheme)
}

// CopyMapStringString returns a deep copy of m.
func CopyMapStringString(m map[string]string) map[string]string {
	n := make(map[string]string, len(m))

	for k, v := range m {
		n[k] = v
	}

	return n
}

func GetDriverContainerNodeLabel(moduleName string) string {
	return fmt.Sprintf("oot.node.kubernetes.io/%s.ready", moduleName)
}
