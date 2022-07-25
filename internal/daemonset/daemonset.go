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
	devicePluginKernelVersion      = ""
)

//go:generate mockgen -source=daemonset.go -package=daemonset -destination=mock_daemonset.go

type DaemonSetCreator interface {
	GarbageCollect(ctx context.Context, existingDS map[string]*appsv1.DaemonSet, validKernels sets.String) ([]string, error)
	ModuleDaemonSetsByKernelVersion(ctx context.Context, name, namespace string) (map[string]*appsv1.DaemonSet, error)
	SetDriverContainerAsDesired(ctx context.Context, ds *appsv1.DaemonSet, image string, mod ootov1alpha1.Module, kernelVersion string) error
	SetDevicePluginAsDesired(ctx context.Context, ds *appsv1.DaemonSet, mod *ootov1alpha1.Module) error
	GetNodeLabelFromPod(pod *v1.Pod, moduleName string) string
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
		if !dc.isDevicePluginDaemonSet(ds) && !validKernels.Has(kernelVersion) {
			if err := dc.client.Delete(ctx, ds); err != nil {
				return nil, fmt.Errorf("could not delete DaemonSet %s: %v", ds.Name, err)
			}

			deleted = append(deleted, ds.Name)
		}
	}

	return deleted, nil
}

func (dc *daemonSetGenerator) ModuleDaemonSetsByKernelVersion(ctx context.Context, name, namespace string) (map[string]*appsv1.DaemonSet, error) {
	dsList, err := dc.moduleDaemonSets(ctx, name, namespace)
	if err != nil {
		return nil, fmt.Errorf("could not get all DaemonSets: %w", err)
	}

	dsByKernelVersion := make(map[string]*appsv1.DaemonSet, len(dsList))

	for i := 0; i < len(dsList); i++ {
		ds := dsList[i]

		kernelVersion := ds.Labels[dc.kernelLabel]
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

	ds.SetLabels(
		OverrideLabels(ds.GetLabels(), standardLabels),
	)

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

	container := mod.Spec.DriverContainer
	container.Name = "driver-container"
	container.Image = image
	container.VolumeMounts = append(container.VolumeMounts, driverContainerVolumeMounts...)

	ds.Spec = appsv1.DaemonSetSpec{
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:     standardLabels,
				Finalizers: []string{constants.NodeLabelerFinalizer},
			},
			Spec: v1.PodSpec{
				Containers:         []v1.Container{container},
				ImagePullSecrets:   GetPodPullSecrets(mod),
				NodeSelector:       nodeSelector,
				ServiceAccountName: mod.Spec.ServiceAccountName,
				Volumes:            append(volumes, mod.Spec.AdditionalVolumes...),
			},
		},
		Selector: &metav1.LabelSelector{MatchLabels: standardLabels},
	}

	return controllerutil.SetControllerReference(&mod, ds, dc.scheme)
}

func (dc *daemonSetGenerator) SetDevicePluginAsDesired(ctx context.Context, ds *appsv1.DaemonSet, mod *ootov1alpha1.Module) error {
	if ds == nil {
		return errors.New("ds cannot be nil")
	}

	if mod.Spec.DevicePlugin == nil {
		return errors.New("device plugin in module should not be nil")
	}

	containerVolumeMounts := []v1.VolumeMount{
		{
			Name:      kubeletDevicePluginsVolumeName,
			MountPath: kubeletDevicePluginsPath,
		},
	}

	hostPathDirectory := v1.HostPathDirectory

	devicePluginVolume := v1.Volume{
		Name: kubeletDevicePluginsVolumeName,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: kubeletDevicePluginsPath,
				Type: &hostPathDirectory,
			},
		},
	}

	standardLabels := map[string]string{constants.ModuleNameLabel: mod.Name}

	ds.SetLabels(
		OverrideLabels(ds.GetLabels(), standardLabels),
	)

	container := *mod.Spec.DevicePlugin
	container.Name = "device-plugin"
	container.VolumeMounts = append(container.VolumeMounts, containerVolumeMounts...)

	if container.SecurityContext == nil {
		container.SecurityContext = &v1.SecurityContext{}
	}

	container.SecurityContext.Privileged = pointer.Bool(true)

	ds.Spec = appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: standardLabels},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:     standardLabels,
				Finalizers: []string{constants.NodeLabelerFinalizer},
			},
			Spec: v1.PodSpec{
				Containers:         []v1.Container{container},
				ImagePullSecrets:   GetPodPullSecrets(*mod),
				NodeSelector:       map[string]string{getDriverContainerNodeLabel(mod.Name): ""},
				ServiceAccountName: mod.Spec.ServiceAccountName,
				Volumes:            append([]v1.Volume{devicePluginVolume}, mod.Spec.AdditionalVolumes...),
			},
		},
	}

	return controllerutil.SetControllerReference(mod, ds, dc.scheme)
}

func (dc *daemonSetGenerator) GetNodeLabelFromPod(pod *v1.Pod, moduleName string) string {
	kernelVersion := pod.Labels[dc.kernelLabel]
	if kernelVersion == devicePluginKernelVersion {
		return getDevicePluginNodeLabel(moduleName)
	}
	return getDriverContainerNodeLabel(moduleName)
}

func (dc *daemonSetGenerator) moduleDaemonSets(ctx context.Context, name, namespace string) ([]appsv1.DaemonSet, error) {
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

func (dc *daemonSetGenerator) isDevicePluginDaemonSet(ds *appsv1.DaemonSet) bool {
	return ds.Labels[dc.kernelLabel] == ""
}

// CopyMapStringString returns a deep copy of m.
func CopyMapStringString(m map[string]string) map[string]string {
	n := make(map[string]string, len(m))

	for k, v := range m {
		n[k] = v
	}

	return n
}

func getDriverContainerNodeLabel(moduleName string) string {
	return fmt.Sprintf("oot.node.kubernetes.io/%s.ready", moduleName)
}

func getDevicePluginNodeLabel(moduleName string) string {
	return fmt.Sprintf("oot.node.kubernetes.io/%s.device-plugin-ready", moduleName)
}

func IsDevicePluginKernelVersion(kernelVersion string) bool {
	return kernelVersion == devicePluginKernelVersion
}

func GetDevicePluginKernelVersion() string {
	return devicePluginKernelVersion
}

func GetPodPullSecrets(mod ootov1alpha1.Module) []v1.LocalObjectReference {
	var pullSecrets []v1.LocalObjectReference

	if ips := mod.Spec.ImagePullSecret; ips != nil {
		pullSecrets = []v1.LocalObjectReference{*ips}
	}

	return pullSecrets
}

func OverrideLabels(labels, overrides map[string]string) map[string]string {
	if labels == nil {
		labels = make(map[string]string, len(overrides))
	}

	for k, v := range overrides {
		labels[k] = v
	}

	return labels
}
