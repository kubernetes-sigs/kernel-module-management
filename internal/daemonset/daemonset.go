package daemonset

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-openapi/swag"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
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
	nodeLibModulesVolumeName       = "node-lib-modules"
	nodeVarLibFirmwarePath         = "/var/lib/firmware"
	nodeVarLibFirmwareVolumeName   = "node-var-lib-firmware"
)

//go:generate mockgen -source=daemonset.go -package=daemonset -destination=mock_daemonset.go

type DaemonSetCreator interface {
	GarbageCollect(ctx context.Context, mod *kmmv1beta1.Module, existingDS []appsv1.DaemonSet, validKernels sets.Set[string]) ([]string, error)
	GetModuleDaemonSets(ctx context.Context, name, namespace string) ([]appsv1.DaemonSet, error)
	SetDriverContainerAsDesired(ctx context.Context, ds *appsv1.DaemonSet, mld *api.ModuleLoaderData) error
	SetDevicePluginAsDesired(ctx context.Context, ds *appsv1.DaemonSet, mod *kmmv1beta1.Module) error
	GetNodeLabelFromPod(pod *v1.Pod, moduleName string, useDeprecatedLabel bool) string
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

func (dc *daemonSetGenerator) GarbageCollect(ctx context.Context, mod *kmmv1beta1.Module, existingDS []appsv1.DaemonSet, validKernels sets.Set[string]) ([]string, error) {
	deleted := make([]string, 0)

	for _, ds := range existingDS {
		if isOlderVersionUnusedDaemonset(&ds, mod.Spec.ModuleLoader.Container.Version) ||
			isModuleLoaderDaemonsetWithInvalidKernel(&ds, dc.kernelLabel, validKernels) {
			deleted = append(deleted, ds.Name)
			if err := dc.client.Delete(ctx, &ds); err != nil {
				return nil, fmt.Errorf("could not delete DaemonSet %s: %v", ds.Name, err)
			}
		}
	}

	return deleted, nil
}

func (dc *daemonSetGenerator) SetDriverContainerAsDesired(
	ctx context.Context,
	ds *appsv1.DaemonSet,
	mld *api.ModuleLoaderData,
) error {
	if ds == nil {
		return errors.New("ds cannot be nil")
	}

	if mld.ContainerImage == "" {
		return errors.New("container image cannot be empty")
	}

	kernelVersion := mld.KernelVersion
	if kernelVersion == "" {
		return errors.New("kernelVersion cannot be empty")
	}

	standardLabels := map[string]string{
		constants.ModuleNameLabel: mld.Name,
		dc.kernelLabel:            kernelVersion,
		constants.DaemonSetRole:   constants.ModuleLoaderRoleLabelValue,
	}
	nodeSelector := CopyMapStringString(mld.Selector)
	nodeSelector[dc.kernelLabel] = kernelVersion

	if mld.ModuleVersion != "" {
		versionLabel := utils.GetModuleLoaderVersionLabelName(mld.Namespace, mld.Name)
		standardLabels[versionLabel] = mld.ModuleVersion
		nodeSelector[versionLabel] = mld.ModuleVersion
	}

	ds.SetLabels(
		OverrideLabels(ds.GetLabels(), standardLabels),
	)

	nodeLibModulesPath := "/lib/modules/" + kernelVersion

	hostPathDirectory := v1.HostPathDirectory
	hostPathDirectoryOrCreate := v1.HostPathDirectoryOrCreate

	container := v1.Container{
		Command:         []string{"sleep", "infinity"},
		Name:            "module-loader",
		Image:           mld.ContainerImage,
		ImagePullPolicy: mld.ImagePullPolicy,
		Lifecycle: &v1.Lifecycle{
			PostStart: &v1.LifecycleHandler{
				Exec: &v1.ExecAction{
					Command: makeLoadCommand(mld.InTreeModuleToRemove, mld.Modprobe, mld.Name),
				},
			},
			PreStop: &v1.LifecycleHandler{
				Exec: &v1.ExecAction{
					Command: makeUnloadCommand(mld.Modprobe, mld.Name),
				},
			},
		},
		SecurityContext: &v1.SecurityContext{
			AllowPrivilegeEscalation: pointer.Bool(false),
			Capabilities: &v1.Capabilities{
				Add: []v1.Capability{"SYS_MODULE"},
			},
			RunAsUser: pointer.Int64(0),
			SELinuxOptions: &v1.SELinuxOptions{
				Type: "spc_t",
			},
		},
		VolumeMounts: []v1.VolumeMount{
			{
				Name:      nodeLibModulesVolumeName,
				ReadOnly:  true,
				MountPath: nodeLibModulesPath,
			},
		},
	}

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
	}

	if fw := mld.Modprobe.FirmwarePath; fw != "" {
		firmwareVolume := v1.Volume{
			Name: nodeVarLibFirmwareVolumeName,
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: nodeVarLibFirmwarePath,
					Type: &hostPathDirectoryOrCreate,
				},
			},
		}
		volumes = append(volumes, firmwareVolume)

		firmwareVolumeMount := v1.VolumeMount{
			Name:      nodeVarLibFirmwareVolumeName,
			MountPath: nodeVarLibFirmwarePath,
		}

		container.VolumeMounts = append(container.VolumeMounts, firmwareVolumeMount)
	}

	var modulesOrderAnnotations map[string]string
	if mld.Modprobe.ModulesLoadingOrder != nil {
		modulesOrderAnnotations = map[string]string{
			"modules-order": getModulesOrderAnnotationValue(mld),
		}
		softdepVolume := v1.Volume{
			Name: "modules-order",
			VolumeSource: v1.VolumeSource{
				DownwardAPI: &v1.DownwardAPIVolumeSource{
					Items: []v1.DownwardAPIVolumeFile{
						{
							Path:     "softdep.conf",
							FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.annotations['modules-order']"},
						},
					},
				},
			},
		}
		volumes = append(volumes, softdepVolume)

		softDepVolumeMount := v1.VolumeMount{
			Name:      "modules-order",
			ReadOnly:  true,
			MountPath: "/etc/modprobe.d",
		}
		container.VolumeMounts = append(container.VolumeMounts, softDepVolumeMount)
	}

	ds.Spec = appsv1.DaemonSetSpec{
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      standardLabels,
				Finalizers:  []string{constants.NodeLabelerFinalizer},
				Annotations: modulesOrderAnnotations,
			},
			Spec: v1.PodSpec{
				ShareProcessNamespace: swag.Bool(true),
				Containers:            []v1.Container{container},
				ImagePullSecrets:      GetPodPullSecrets(mld.ImageRepoSecret),
				NodeSelector:          nodeSelector,
				PriorityClassName:     "system-node-critical",
				ServiceAccountName:    mld.ServiceAccountName,
				Volumes:               volumes,
			},
		},
		Selector: &metav1.LabelSelector{MatchLabels: standardLabels},
	}

	return controllerutil.SetControllerReference(mld.Owner, ds, dc.scheme)
}

func (dc *daemonSetGenerator) SetDevicePluginAsDesired(
	ctx context.Context,
	ds *appsv1.DaemonSet,
	mod *kmmv1beta1.Module,
) error {
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

	standardLabels := map[string]string{
		constants.ModuleNameLabel: mod.Name,
		constants.DaemonSetRole:   constants.DevicePluginRoleLabelValue,
	}
	nodeSelector := map[string]string{getDriverContainerNodeLabel(mod.Namespace, mod.Name, true): ""}

	if mod.Spec.ModuleLoader.Container.Version != "" {
		versionLabel := utils.GetDevicePluginVersionLabelName(mod.Namespace, mod.Name)
		standardLabels[versionLabel] = mod.Spec.ModuleLoader.Container.Version
		nodeSelector[versionLabel] = mod.Spec.ModuleLoader.Container.Version
	}

	ds.SetLabels(
		OverrideLabels(ds.GetLabels(), standardLabels),
	)

	ds.Spec = appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: standardLabels},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:     standardLabels,
				Finalizers: []string{constants.NodeLabelerFinalizer},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Args:            mod.Spec.DevicePlugin.Container.Args,
						Command:         mod.Spec.DevicePlugin.Container.Command,
						Env:             mod.Spec.DevicePlugin.Container.Env,
						Name:            "device-plugin",
						Image:           mod.Spec.DevicePlugin.Container.Image,
						ImagePullPolicy: mod.Spec.DevicePlugin.Container.ImagePullPolicy,
						Resources:       mod.Spec.DevicePlugin.Container.Resources,
						SecurityContext: &v1.SecurityContext{Privileged: pointer.Bool(true)},
						VolumeMounts:    append(mod.Spec.DevicePlugin.Container.VolumeMounts, containerVolumeMounts...),
					},
				},
				PriorityClassName:  "system-node-critical",
				ImagePullSecrets:   GetPodPullSecrets(mod.Spec.ImageRepoSecret),
				NodeSelector:       nodeSelector,
				ServiceAccountName: mod.Spec.DevicePlugin.ServiceAccountName,
				Volumes:            append([]v1.Volume{devicePluginVolume}, mod.Spec.DevicePlugin.Volumes...),
			},
		},
	}

	return controllerutil.SetControllerReference(mod, ds, dc.scheme)
}

func (dc *daemonSetGenerator) GetNodeLabelFromPod(pod *v1.Pod, moduleName string, useDeprecatedLabel bool) string {
	podRole := pod.Labels[constants.DaemonSetRole]
	if podRole == constants.DevicePluginRoleLabelValue {
		return getDevicePluginNodeLabel(pod.Namespace, moduleName, useDeprecatedLabel)
	}
	return getDriverContainerNodeLabel(pod.Namespace, moduleName, useDeprecatedLabel)
}

func (dc *daemonSetGenerator) GetModuleDaemonSets(ctx context.Context, name, namespace string) ([]appsv1.DaemonSet, error) {
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

// CopyMapStringString returns a deep copy of m.
func CopyMapStringString(m map[string]string) map[string]string {
	n := make(map[string]string, len(m))

	for k, v := range m {
		n[k] = v
	}

	return n
}

func getDriverContainerNodeLabel(namespace, moduleName string, useDeprecatedLabel bool) string {
	// TODO: "kmm.node.kubernetes.io/<module-name>.ready" is needed for backward compatibility. Remove for 2.0
	if useDeprecatedLabel {
		return fmt.Sprintf("kmm.node.kubernetes.io/%s.ready", moduleName)
	}
	return fmt.Sprintf("kmm.node.kubernetes.io/%s.%s.ready", namespace, moduleName)
}

func getDevicePluginNodeLabel(namespace, moduleName string, useDeprecatedLabel bool) string {
	// TODO: "kmm.node.kubernetes.io/<module-name>.device-plugin-ready" is needed for backward compatibility. Remove for 2.0
	if useDeprecatedLabel {
		return fmt.Sprintf("kmm.node.kubernetes.io/%s.device-plugin-ready", moduleName)
	}
	return fmt.Sprintf("kmm.node.kubernetes.io/%s.%s.device-plugin-ready", namespace, moduleName)
}

func isOlderVersionUnusedDaemonset(ds *appsv1.DaemonSet, moduleVersion string) bool {
	moduleName := ds.Labels[constants.ModuleNameLabel]
	moduleNamespace := ds.Namespace
	versionLabel := utils.GetModuleLoaderVersionLabelName(moduleNamespace, moduleName)
	if IsDevicePluginDS(ds) {
		versionLabel = utils.GetDevicePluginVersionLabelName(moduleNamespace, moduleName)
	}
	return ds.Labels[versionLabel] != moduleVersion && ds.Status.DesiredNumberScheduled == 0
}

func isModuleLoaderDaemonsetWithInvalidKernel(ds *appsv1.DaemonSet, kernelLabel string, validKernels sets.Set[string]) bool {
	return !IsDevicePluginDS(ds) && !validKernels.Has(ds.Labels[kernelLabel])
}

func IsDevicePluginDS(ds *appsv1.DaemonSet) bool {
	dsLabels := ds.GetLabels()
	return dsLabels[constants.DaemonSetRole] == constants.DevicePluginRoleLabelValue
}

func GetPodPullSecrets(secret *v1.LocalObjectReference) []v1.LocalObjectReference {
	if secret == nil {
		return nil
	}

	return []v1.LocalObjectReference{*secret}
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

func makeLoadCommand(inTreeModuleToRemove string, spec kmmv1beta1.ModprobeSpec, modName string) []string {
	loadCommandShell := []string{
		"/bin/sh",
		"-c",
	}

	var loadCommand strings.Builder

	if inTreeModuleToRemove != "" {
		fmt.Fprintf(&loadCommand, "modprobe -r %q && ", inTreeModuleToRemove)
	}

	if fw := spec.FirmwarePath; fw != "" {
		fmt.Fprintf(&loadCommand, `cp -r "%s/*" %s && `, fw, nodeVarLibFirmwarePath)
	}

	loadCommand.WriteString("modprobe")

	if rawArgs := spec.RawArgs; rawArgs != nil && len(rawArgs.Load) > 0 {
		for _, arg := range rawArgs.Load {
			fmt.Fprintf(&loadCommand, " %q", arg)
		}
		return append(loadCommandShell, loadCommand.String())
	}

	if args := spec.Args; args != nil && len(args.Load) > 0 {
		for _, arg := range args.Load {
			fmt.Fprintf(&loadCommand, " %q", arg)
		}
	} else {
		loadCommand.WriteString(" -v")
	}

	if spec.DirName != "" {
		fmt.Fprintf(&loadCommand, " -d %q", spec.DirName)
	}

	fmt.Fprintf(&loadCommand, " %q", spec.ModuleName)

	if params := spec.Parameters; len(params) > 0 {
		for _, param := range params {
			fmt.Fprintf(&loadCommand, " %q", param)
		}
	}

	return append(loadCommandShell, loadCommand.String())
}

func makeUnloadCommand(spec kmmv1beta1.ModprobeSpec, modName string) []string {
	unloadCommandShell := []string{
		"/bin/sh",
		"-c",
	}

	var unloadCommand strings.Builder
	unloadCommand.WriteString("modprobe")

	fwUnloadCommand := ""
	if fw := spec.FirmwarePath; fw != "" {
		fwUnloadCommand = fmt.Sprintf(` && cd %q && find |sort -r |xargs -I{} rm -d "%s/{}"`, fw, nodeVarLibFirmwarePath)
	}

	if rawArgs := spec.RawArgs; rawArgs != nil && len(rawArgs.Unload) > 0 {
		for _, arg := range rawArgs.Unload {
			fmt.Fprintf(&unloadCommand, " %q", arg)
		}

		unloadCommand.WriteString(fwUnloadCommand)

		return append(unloadCommandShell, unloadCommand.String())
	}

	if args := spec.Args; args != nil && len(args.Unload) > 0 {
		for _, arg := range args.Unload {
			fmt.Fprintf(&unloadCommand, " %q", arg)
		}
	} else {
		unloadCommand.WriteString(" -rv")
	}

	if dirName := spec.DirName; dirName != "" {
		fmt.Fprintf(&unloadCommand, " -d %q", dirName)
	}

	fmt.Fprintf(&unloadCommand, " %q", spec.ModuleName)
	unloadCommand.WriteString(fwUnloadCommand)

	return append(unloadCommandShell, unloadCommand.String())
}

func getModulesOrderAnnotationValue(mld *api.ModuleLoaderData) string {
	modulesNames := mld.Modprobe.ModulesLoadingOrder

	var sb strings.Builder

	for i := 0; i < len(modulesNames)-1; i++ {
		fmt.Fprintf(&sb, "softdep %s pre: %s\n", modulesNames[i], modulesNames[i+1])
	}

	return sb.String()
}
