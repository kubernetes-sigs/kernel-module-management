package pod

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/config"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/meta"
	"github.com/kubernetes-sigs/kernel-module-management/internal/worker"
	"github.com/mitchellh/hashstructure/v2"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/kubectl/pkg/cmd/util/podcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/yaml"
)

//go:generate mockgen -source=workerpodmanager.go -package=pod -destination=mock_workerpodmanager.go

type WorkerPodManager interface {
	CreateLoaderPod(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleSpec) error
	CreateUnloaderPod(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleStatus) error
	DeletePod(ctx context.Context, pod *v1.Pod) error
	GetWorkerPod(ctx context.Context, podName, namespace string) (*v1.Pod, error)
	ListWorkerPodsOnNode(ctx context.Context, nodeName string) ([]v1.Pod, error)
	LoaderPodTemplate(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleSpec) (*v1.Pod, error)
	UnloaderPodTemplate(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleStatus) (*v1.Pod, error)
	IsLoaderPod(p *v1.Pod) bool
	IsUnloaderPod(p *v1.Pod) bool
	GetConfigAnnotation(p *v1.Pod) string
	HashAnnotationDiffer(p1, p2 *v1.Pod) bool
	GetTolerationsAnnotation(p *v1.Pod) string
	GetModuleVersionAnnotation(p *v1.Pod) string
}

const (
	NodeModulesConfigFinalizer = "kmm.node.kubernetes.io/nodemodulesconfig-reconciler"
	WorkerContainerName        = "worker"

	volMountPointConfig        = "/etc/kmm-worker"
	configFileName             = "config.yaml"
	configFullPath             = volMountPointConfig + "/" + configFileName
	volNameConfig              = "config"
	sharedFilesDir             = "/tmp"
	volNameTmp                 = "tmp"
	volumeNameConfig           = "config"
	initContainerName          = "image-extractor"
	modulesOrderKey            = "kmm.node.kubernetes.io/modules-order"
	workerActionLoad           = "Load"
	workerActionUnload         = "Unload"
	actionLabelKey             = "kmm.node.kubernetes.io/worker-action"
	configAnnotationKey        = "kmm.node.kubernetes.io/worker-config"
	hashAnnotationKey          = "kmm.node.kubernetes.io/worker-hash"
	tolerationsAnnotationKey   = "kmm.node.kubernetes.io/worker-tolerations"
	moduleVersionAnnotationKey = "kmm.node.kubernetes.io/worker-module-version"
)

var (
	requests = v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("0.5"),
		v1.ResourceMemory: resource.MustParse("64Mi"),
	}
	limits = v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse("1"),
		v1.ResourceMemory: resource.MustParse("128Mi"),
	}
)

type workerPodManagerImpl struct {
	client      client.Client
	scheme      *runtime.Scheme
	workerCfg   *config.Worker
	workerImage string
}

func NewWorkerPodManager(client client.Client, workerImage string, scheme *runtime.Scheme, workerCfg *config.Worker) WorkerPodManager {
	return &workerPodManagerImpl{
		client:      client,
		scheme:      scheme,
		workerCfg:   workerCfg,
		workerImage: workerImage,
	}
}

func (wpmi *workerPodManagerImpl) CreateLoaderPod(ctx context.Context, nmcObj client.Object, nms *kmmv1beta1.NodeModuleSpec) error {
	pod, err := wpmi.LoaderPodTemplate(ctx, nmcObj, nms)
	if err != nil {
		return fmt.Errorf("could not get loader Pod template: %v", err)
	}

	return wpmi.client.Create(ctx, pod)
}

func (wpmi *workerPodManagerImpl) CreateUnloaderPod(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleStatus) error {
	pod, err := wpmi.UnloaderPodTemplate(ctx, nmc, nms)
	if err != nil {
		return fmt.Errorf("could not create the Pod template: %v", err)
	}

	return wpmi.client.Create(ctx, pod)
}

func (wpmi *workerPodManagerImpl) DeletePod(ctx context.Context, pod *v1.Pod) error {
	logger := ctrl.LoggerFrom(ctx)

	logger.Info("Removing Pod finalizer")

	podPatch := client.MergeFrom(pod.DeepCopy())

	controllerutil.RemoveFinalizer(pod, NodeModulesConfigFinalizer)

	if err := wpmi.client.Patch(ctx, pod, podPatch); err != nil {
		return fmt.Errorf("could not patch Pod %s/%s: %v", pod.Namespace, pod.Name, err)
	}

	return deletePod(wpmi.client, ctx, pod)
}

func (wpmi *workerPodManagerImpl) GetWorkerPod(ctx context.Context, podName, namespace string) (*v1.Pod, error) {
	pod := v1.Pod{}
	nsn := types.NamespacedName{Namespace: namespace, Name: podName}

	if err := wpmi.client.Get(ctx, nsn, &pod); err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		} else {
			return nil, fmt.Errorf("could not get pod %s: %v", nsn, err)
		}
	}

	return &pod, nil
}

func (wpmi *workerPodManagerImpl) ListWorkerPodsOnNode(ctx context.Context, nodeName string) ([]v1.Pod, error) {
	logger := ctrl.LoggerFrom(ctx).WithValues("node name", nodeName)

	pl := v1.PodList{}

	hl := client.HasLabels{actionLabelKey}
	mf := client.MatchingFields{".spec.nodeName": nodeName}

	logger.V(1).Info("Listing worker Pods")

	if err := wpmi.client.List(ctx, &pl, hl, mf); err != nil {
		return nil, fmt.Errorf("could not list worker pods for node %s: %v", nodeName, err)
	}

	return pl.Items, nil
}

func (wpmi *workerPodManagerImpl) LoaderPodTemplate(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleSpec) (*v1.Pod, error) {
	pod, err := wpmi.baseWorkerPod(ctx, nmc, &nms.ModuleItem, &nms.Config)
	if err != nil {
		return nil, fmt.Errorf("could not create the base Pod: %v", err)
	}

	if nms.Config.Modprobe.ModulesLoadingOrder != nil {
		if err = setWorkerSofdepConfig(pod, nms.Config.Modprobe.ModulesLoadingOrder); err != nil {
			return nil, fmt.Errorf("could not set software dependency for mulitple modules: %v", err)
		}
	}

	args := []string{"kmod", "load", configFullPath}

	privileged := false
	if nms.Config.Modprobe.FirmwarePath != "" {

		firmwareHostPath := wpmi.workerCfg.FirmwareHostPath
		if firmwareHostPath == nil {
			return nil, fmt.Errorf("firmwareHostPath wasn't set, while the Module requires firmware loading")
		}

		args = append(args, "--"+worker.FlagFirmwarePath, *firmwareHostPath)

		firmwarePathContainerImg := filepath.Join(nms.Config.Modprobe.FirmwarePath, "*")
		firmwarePathWorkerImg := filepath.Join(sharedFilesDir, nms.Config.Modprobe.FirmwarePath)
		if err = addCopyCommand(pod, firmwarePathContainerImg, firmwarePathWorkerImg); err != nil {
			return nil, fmt.Errorf("could not add the copy command to the init container: %v", err)
		}

		if err = setFirmwareVolume(pod, firmwareHostPath); err != nil {
			return nil, fmt.Errorf("could not map host volume needed for firmware loading: %v", err)
		}

		privileged = true
	}

	if err = setWorkerConfigAnnotation(pod, nms.Config); err != nil {
		return nil, fmt.Errorf("could not set worker config: %v", err)
	}
	if err = setWorkerTolerationsAnnotation(pod, nms.Tolerations); err != nil {
		return nil, fmt.Errorf("could not set worker tolerations: %v", err)
	}

	setWorkerModuleVersionAnnotation(pod, nms.Version)

	if err = setWorkerSecurityContext(pod, wpmi.workerCfg, privileged); err != nil {
		return nil, fmt.Errorf("could not set the worker Pod as privileged: %v", err)
	}

	if err = setWorkerContainerArgs(pod, args); err != nil {
		return nil, fmt.Errorf("could not set worker container args: %v", err)
	}

	meta.SetLabel(pod, actionLabelKey, workerActionLoad)

	return pod, setHashAnnotation(pod)
}

func (wpmi *workerPodManagerImpl) UnloaderPodTemplate(ctx context.Context, nmc client.Object, nms *kmmv1beta1.NodeModuleStatus) (*v1.Pod, error) {
	pod, err := wpmi.baseWorkerPod(ctx, nmc, &nms.ModuleItem, &nms.Config)
	if err != nil {
		return nil, fmt.Errorf("could not create the base Pod: %v", err)
	}

	args := []string{"kmod", "unload", configFullPath}

	if err = setWorkerConfigAnnotation(pod, nms.Config); err != nil {
		return nil, fmt.Errorf("could not set worker config: %v", err)
	}
	if err = setWorkerTolerationsAnnotation(pod, nms.Tolerations); err != nil {
		return nil, fmt.Errorf("could not set worker tolerations: %v", err)
	}

	if err = setWorkerSecurityContext(pod, wpmi.workerCfg, false); err != nil {
		return nil, fmt.Errorf("could not set the worker Pod's security context: %v", err)
	}

	if nms.Config.Modprobe.ModulesLoadingOrder != nil {
		if err = setWorkerSofdepConfig(pod, nms.Config.Modprobe.ModulesLoadingOrder); err != nil {
			return nil, fmt.Errorf("could not set software dependency for mulitple modules: %v", err)
		}
	}

	if nms.Config.Modprobe.FirmwarePath != "" {
		firmwareHostPath := wpmi.workerCfg.FirmwareHostPath
		if firmwareHostPath == nil {
			return nil, fmt.Errorf("firmwareHostPath was not set while the Module requires firmware unloading")
		}
		args = append(args, "--"+worker.FlagFirmwarePath, *firmwareHostPath)

		firmwarePathContainerImg := filepath.Join(nms.Config.Modprobe.FirmwarePath, "*")
		firmwarePathWorkerImg := filepath.Join(sharedFilesDir, nms.Config.Modprobe.FirmwarePath)
		if err = addCopyCommand(pod, firmwarePathContainerImg, firmwarePathWorkerImg); err != nil {
			return nil, fmt.Errorf("could not add the copy command to the init container: %v", err)
		}

		if err = setFirmwareVolume(pod, firmwareHostPath); err != nil {
			return nil, fmt.Errorf("could not map host volume needed for firmware unloading: %v", err)
		}
	}

	if err = setWorkerContainerArgs(pod, args); err != nil {
		return nil, fmt.Errorf("could not set worker container args: %v", err)
	}

	meta.SetLabel(pod, actionLabelKey, workerActionUnload)

	return pod, setHashAnnotation(pod)
}

func (wpmi *workerPodManagerImpl) IsLoaderPod(p *v1.Pod) bool {

	if p == nil {
		return false
	}

	return p.Labels[actionLabelKey] == workerActionLoad
}

func (wpmi *workerPodManagerImpl) IsUnloaderPod(p *v1.Pod) bool {

	if p == nil {
		return false
	}

	return p.Labels[actionLabelKey] == workerActionUnload
}

func (wpmi *workerPodManagerImpl) GetConfigAnnotation(p *v1.Pod) string {

	if p == nil {
		return ""
	}

	return p.Annotations[configAnnotationKey]
}

func (wpmi *workerPodManagerImpl) GetTolerationsAnnotation(p *v1.Pod) string {

	if p == nil {
		return ""
	}

	return p.Annotations[tolerationsAnnotationKey]
}

func (wpmi *workerPodManagerImpl) GetModuleVersionAnnotation(p *v1.Pod) string {

	if p == nil {
		return ""
	}

	return p.Annotations[moduleVersionAnnotationKey]
}

func (wpmi *workerPodManagerImpl) HashAnnotationDiffer(p1, p2 *v1.Pod) bool {

	if p1 == nil && p2 == nil {
		return false
	}

	if (p1 == nil && p2 != nil) || (p1 != nil) && (p2 == nil) {
		return true
	}

	return p1.Annotations[hashAnnotationKey] != p2.Annotations[hashAnnotationKey]
}

func (wpmi *workerPodManagerImpl) baseWorkerPod(ctx context.Context, nmc client.Object, item *kmmv1beta1.ModuleItem,
	moduleConfig *kmmv1beta1.ModuleConfig) (*v1.Pod, error) {

	const (
		volNameLibModules    = "lib-modules"
		volNameUsrLibModules = "usr-lib-modules"
	)

	hostPathDirectory := v1.HostPathDirectory

	volumes := []v1.Volume{
		{
			Name: volumeNameConfig,
			VolumeSource: v1.VolumeSource{
				DownwardAPI: &v1.DownwardAPIVolumeSource{
					Items: []v1.DownwardAPIVolumeFile{
						{
							Path: configFileName,
							FieldRef: &v1.ObjectFieldSelector{
								FieldPath: fmt.Sprintf("metadata.annotations['%s']", configAnnotationKey),
							},
						},
					},
				},
			},
		},
		{
			Name: volNameLibModules,
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/lib/modules",
					Type: &hostPathDirectory,
				},
			},
		},
		{
			Name: volNameUsrLibModules,
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: "/usr/lib/modules",
					Type: &hostPathDirectory,
				},
			},
		},
		{
			Name: volNameTmp,
			VolumeSource: v1.VolumeSource{
				EmptyDir: &v1.EmptyDirVolumeSource{},
			},
		},
	}

	volumeMounts := []v1.VolumeMount{
		{
			Name:      volNameConfig,
			MountPath: volMountPointConfig,
			ReadOnly:  true,
		},
		{
			Name:      volNameLibModules,
			MountPath: "/lib/modules",
			ReadOnly:  true,
		},
		{
			Name:      volNameUsrLibModules,
			MountPath: "/usr/lib/modules",
			ReadOnly:  true,
		},
		{
			Name:      volNameTmp,
			MountPath: sharedFilesDir,
			ReadOnly:  true,
		},
	}

	var imagePullSecrets []v1.LocalObjectReference
	if item.ImageRepoSecret != nil {
		imagePullSecrets = append(imagePullSecrets, *item.ImageRepoSecret)
	}

	nodeName := nmc.GetName()
	pod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: item.Namespace,
			Name:      WorkerPodName(nodeName, item.Name),
			Labels: map[string]string{
				"app.kubernetes.io/name":      "kmm",
				"app.kubernetes.io/component": "worker",
				"app.kubernetes.io/part-of":   "kmm",
				constants.ModuleNameLabel:     item.Name,
			},
		},
		Spec: v1.PodSpec{
			InitContainers: []v1.Container{
				{
					Name:            initContainerName,
					Image:           moduleConfig.ContainerImage,
					ImagePullPolicy: moduleConfig.ImagePullPolicy,
					Command:         []string{"/bin/sh", "-c"},
					Args:            []string{""},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      volNameTmp,
							MountPath: sharedFilesDir,
						},
					},
					Resources: v1.ResourceRequirements{
						Requests: requests,
						Limits:   limits,
					},
				},
			},
			Containers: []v1.Container{
				{
					Name:         WorkerContainerName,
					Image:        wpmi.workerImage,
					VolumeMounts: volumeMounts,
					Resources: v1.ResourceRequirements{
						Requests: requests,
						Limits:   limits,
					},
				},
			},
			NodeName:           nodeName,
			RestartPolicy:      v1.RestartPolicyOnFailure,
			ServiceAccountName: item.ServiceAccountName,
			ImagePullSecrets:   imagePullSecrets,
			Volumes:            volumes,
			Tolerations:        item.Tolerations,
		},
	}

	if err := ctrl.SetControllerReference(nmc, &pod, wpmi.scheme); err != nil {
		return nil, fmt.Errorf("could not set the owner as controller: %v", err)
	}

	kmodsPathContainerImg := filepath.Join(moduleConfig.Modprobe.DirName, "lib", "modules", moduleConfig.KernelVersion)
	kmodsPathWorkerImg := filepath.Join(sharedFilesDir, moduleConfig.Modprobe.DirName, "lib", "modules")
	if err := addCopyCommand(&pod, kmodsPathContainerImg, kmodsPathWorkerImg); err != nil {
		return nil, fmt.Errorf("could not add the copy command to the init container: %v", err)
	}

	controllerutil.AddFinalizer(&pod, NodeModulesConfigFinalizer)

	return &pod, nil
}

func setWorkerConfigAnnotation(pod *v1.Pod, cfg kmmv1beta1.ModuleConfig) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("could not marshal the ModuleConfig to YAML: %v", err)
	}
	meta.SetAnnotation(pod, configAnnotationKey, string(b))

	return nil
}

func setWorkerTolerationsAnnotation(pod *v1.Pod, tolerations []v1.Toleration) error {
	if len(tolerations) != 0 {
		b, err := yaml.Marshal(tolerations)
		if err != nil {
			return fmt.Errorf("could not marshal the Tolerations to YAML: %v", err)
		}
		meta.SetAnnotation(pod, tolerationsAnnotationKey, string(b))
	}

	return nil
}

func setWorkerModuleVersionAnnotation(pod *v1.Pod, moduleVersion string) {
	if moduleVersion != "" {
		meta.SetAnnotation(pod, moduleVersionAnnotationKey, moduleVersion)
	}
}

func setWorkerSofdepConfig(pod *v1.Pod, modulesLoadingOrder []string) error {
	softdepAnnotationValue := getModulesOrderAnnotationValue(modulesLoadingOrder)
	meta.SetAnnotation(pod, modulesOrderKey, softdepAnnotationValue)

	softdepVolume := v1.Volume{
		Name: "modules-order",
		VolumeSource: v1.VolumeSource{
			DownwardAPI: &v1.DownwardAPIVolumeSource{
				Items: []v1.DownwardAPIVolumeFile{
					{
						Path:     "softdep.conf",
						FieldRef: &v1.ObjectFieldSelector{FieldPath: fmt.Sprintf("metadata.annotations['%s']", modulesOrderKey)},
					},
				},
			},
		},
	}
	softDepVolumeMount := v1.VolumeMount{
		Name:      "modules-order",
		ReadOnly:  true,
		MountPath: "/etc/modprobe.d",
	}
	pod.Spec.Volumes = append(pod.Spec.Volumes, softdepVolume)
	container, _ := podcmd.FindContainerByName(pod, WorkerContainerName)
	if container == nil {
		return errors.New("could not find the worker container")
	}
	container.VolumeMounts = append(container.VolumeMounts, softDepVolumeMount)
	return nil
}

func addCopyCommand(pod *v1.Pod, src, dst string) error {

	container, _ := podcmd.FindContainerByName(pod, initContainerName)
	if container == nil {
		return errors.New("could not find the init container")
	}

	const template = `
mkdir -p %s;
cp -R %s %s;
`
	copyCommand := fmt.Sprintf(template, dst, src, dst)
	container.Args[0] = strings.Join([]string{container.Args[0], copyCommand}, "")

	return nil
}

func setFirmwareVolume(pod *v1.Pod, firmwareHostPath *string) error {

	const volNameVarLibFirmware = "lib-firmware"
	container, _ := podcmd.FindContainerByName(pod, WorkerContainerName)
	if container == nil {
		return errors.New("could not find the worker container")
	}

	if firmwareHostPath == nil {
		return errors.New("hostFirmwarePath must be set")
	}

	firmwareVolumeMount := v1.VolumeMount{
		Name:      volNameVarLibFirmware,
		MountPath: *firmwareHostPath,
	}

	hostPathDirectoryOrCreate := v1.HostPathDirectoryOrCreate
	firmwareVolume := v1.Volume{
		Name: volNameVarLibFirmware,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: *firmwareHostPath,
				Type: &hostPathDirectoryOrCreate,
			},
		},
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, firmwareVolume)
	container.VolumeMounts = append(container.VolumeMounts, firmwareVolumeMount)

	return nil
}

func setWorkerSecurityContext(pod *v1.Pod, workerCfg *config.Worker, privileged bool) error {
	container, _ := podcmd.FindContainerByName(pod, WorkerContainerName)
	if container == nil {
		return errors.New("could not find the worker container")
	}

	sc := &v1.SecurityContext{}

	if privileged {
		sc.Privileged = &privileged
	} else {
		sc.Capabilities = &v1.Capabilities{
			Add: []v1.Capability{"SYS_MODULE"},
		}
		sc.RunAsUser = workerCfg.RunAsUser
		sc.SELinuxOptions = &v1.SELinuxOptions{Type: workerCfg.SELinuxType}
	}

	container.SecurityContext = sc

	return nil
}

func setWorkerContainerArgs(pod *v1.Pod, args []string) error {
	container, _ := podcmd.FindContainerByName(pod, WorkerContainerName)
	if container == nil {
		return errors.New("could not find the worker container")
	}

	container.Args = args

	return nil
}

func setHashAnnotation(pod *v1.Pod) error {
	hash, err := hashstructure.Hash(pod, hashstructure.FormatV2, nil)
	if err != nil {
		return fmt.Errorf("could not hash the pod template: %v", err)
	}

	pod.Annotations[hashAnnotationKey] = fmt.Sprintf("%d", hash)

	return nil
}

func WorkerPodName(nodeName, moduleName string) string {
	return fmt.Sprintf("kmm-worker-%s-%s", nodeName, moduleName)
}

func getModulesOrderAnnotationValue(modulesNames []string) string {
	var softDepData strings.Builder
	for i := 0; i < len(modulesNames)-1; i++ {
		fmt.Fprintf(&softDepData, "softdep %s pre: %s\n", modulesNames[i], modulesNames[i+1])
	}
	return softDepData.String()
}
