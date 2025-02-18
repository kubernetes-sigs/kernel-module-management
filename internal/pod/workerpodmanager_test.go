package pod

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/budougumi0617/cmpmock"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	testclient "github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/config"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/mitchellh/hashstructure/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomock "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/kubectl/pkg/cmd/util/podcmd"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	serviceAccountName = "some-sa"
	workerImage        = "worker-image"
	nmcName            = "nmc"
	moduleName         = "my-module"
	namespace          = "namespace"
)

var (
	workerCfg = &config.Worker{
		RunAsUser:   ptr.To[int64](1234),
		SELinuxType: "someType",
	}

	moduleConfig = kmmv1beta1.ModuleConfig{
		KernelVersion:         "kernel-version",
		ContainerImage:        "container image",
		ImagePullPolicy:       v1.PullIfNotPresent,
		InsecurePull:          true,
		InTreeModulesToRemove: []string{"intree1", "intree2"},
		Modprobe: kmmv1beta1.ModprobeSpec{
			ModuleName:          "test",
			Parameters:          []string{"a", "b"},
			DirName:             "/dir",
			Args:                nil,
			RawArgs:             nil,
			ModulesLoadingOrder: []string{"a", "b", "c"},
		},
	}
)

var _ = Describe("CreateLoaderPod", func() {

	const irsName = "some-secret"

	var (
		ctrl   *gomock.Controller
		client *testclient.MockClient

		nmc *kmmv1beta1.NodeModulesConfig
		mi  kmmv1beta1.ModuleItem

		ctx               context.Context
		moduleConfigToUse kmmv1beta1.ModuleConfig
	)

	BeforeEach(func() {

		ctrl = gomock.NewController(GinkgoT())
		client = testclient.NewMockClient(ctrl)

		nmc = &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}

		mi = kmmv1beta1.ModuleItem{
			ImageRepoSecret:    &v1.LocalObjectReference{Name: irsName},
			Name:               moduleName,
			Namespace:          namespace,
			ServiceAccountName: serviceAccountName,
			Tolerations: []v1.Toleration{
				{
					Key:    "test-key",
					Value:  "test-value",
					Effect: v1.TaintEffectNoExecute,
				},
			},
		}

		moduleConfigToUse = moduleConfig
		ctx = context.TODO()
	})

	It("it should fail if firmwareHostPath was not set but firmware loading was", func() {

		moduleConfigToUse.Modprobe.FirmwarePath = "/firmware-path"

		nms := &kmmv1beta1.NodeModuleSpec{
			ModuleItem: mi,
			Config:     moduleConfigToUse,
		}

		kli := &workerPodManagerImpl{
			client:      client,
			scheme:      scheme,
			workerImage: workerImage,
			workerCfg:   workerCfg,
		}

		Expect(
			kli.CreateLoaderPod(ctx, nmc, nms),
		).To(
			HaveOccurred(),
		)
	})

	It("imagePullSecret should not be defined, if missing from ModuleItem", func() {
		mi.ImageRepoSecret = nil
		nms := &kmmv1beta1.NodeModuleSpec{
			ModuleItem: mi,
			Config:     moduleConfigToUse,
		}

		expected := getBaseWorkerPod("load", nmc, nil, false, true, nil)

		Expect(
			controllerutil.SetControllerReference(nmc, expected, scheme),
		).NotTo(
			HaveOccurred(),
		)

		controllerutil.AddFinalizer(expected, NodeModulesConfigFinalizer)

		container, _ := podcmd.FindContainerByName(expected, "worker")
		Expect(container).NotTo(BeNil())

		container.SecurityContext = &v1.SecurityContext{
			Capabilities: &v1.Capabilities{
				Add: []v1.Capability{"SYS_MODULE"},
			},
			RunAsUser:      workerCfg.RunAsUser,
			SELinuxOptions: &v1.SELinuxOptions{Type: workerCfg.SELinuxType},
		}

		hash, err := hashstructure.Hash(expected, hashstructure.FormatV2, nil)
		Expect(err).NotTo(HaveOccurred())

		expected.Annotations[HashAnnotationKey] = fmt.Sprintf("%d", hash)

		gomock.InOrder(
			client.EXPECT().Create(ctx, cmpmock.DiffEq(expected)),
		)

		workerCfg := *workerCfg

		kli := &workerPodManagerImpl{
			client:      client,
			scheme:      scheme,
			workerImage: workerImage,
			workerCfg:   &workerCfg,
		}

		Expect(
			kli.CreateLoaderPod(ctx, nmc, nms),
		).NotTo(
			HaveOccurred(),
		)
	})

	DescribeTable(
		"should work as expected",
		func(firmwareHostPath *string, withFirmwareLoading bool) {

			if withFirmwareLoading {
				moduleConfigToUse.Modprobe.FirmwarePath = "/firmware-path"
			}

			nms := &kmmv1beta1.NodeModuleSpec{
				ModuleItem: mi,
				Config:     moduleConfigToUse,
			}

			expected := getBaseWorkerPod("load", nmc, firmwareHostPath, withFirmwareLoading, true, mi.ImageRepoSecret)

			Expect(
				controllerutil.SetControllerReference(nmc, expected, scheme),
			).NotTo(
				HaveOccurred(),
			)

			controllerutil.AddFinalizer(expected, NodeModulesConfigFinalizer)

			container, _ := podcmd.FindContainerByName(expected, "worker")
			Expect(container).NotTo(BeNil())

			if withFirmwareLoading && firmwareHostPath != nil {
				container.SecurityContext = &v1.SecurityContext{
					Privileged: ptr.To(true),
				}
			} else {
				container.SecurityContext = &v1.SecurityContext{
					Capabilities: &v1.Capabilities{
						Add: []v1.Capability{"SYS_MODULE"},
					},
					RunAsUser:      workerCfg.RunAsUser,
					SELinuxOptions: &v1.SELinuxOptions{Type: workerCfg.SELinuxType},
				}
			}

			hash, err := hashstructure.Hash(expected, hashstructure.FormatV2, nil)
			Expect(err).NotTo(HaveOccurred())

			expected.Annotations[HashAnnotationKey] = fmt.Sprintf("%d", hash)

			gomock.InOrder(
				client.EXPECT().Create(ctx, cmpmock.DiffEq(expected)),
			)

			workerCfg := *workerCfg
			workerCfg.FirmwareHostPath = firmwareHostPath

			kli := &workerPodManagerImpl{
				client:      client,
				scheme:      scheme,
				workerImage: workerImage,
				workerCfg:   &workerCfg,
			}

			Expect(
				kli.CreateLoaderPod(ctx, nmc, nms),
			).NotTo(
				HaveOccurred(),
			)
		},
		Entry("firmwareHostPath not set, firmware loading not requested", nil, false),
		Entry("firmwareHostPath set to empty string, firmware loading not requested", ptr.To(""), false),
		Entry("firmwareHostPath set, firmware loading not requested", ptr.To("some-path"), false),
		Entry("firmwareHostPath set , firmware loading requested", ptr.To("some-path"), true),
	)
})

var _ = Describe("CreateUnloaderPod", func() {

	const irsName = "some-secret"

	var (
		ctrl   *gomock.Controller
		client *testclient.MockClient
		nmc    *kmmv1beta1.NodeModulesConfig
		mi     kmmv1beta1.ModuleItem

		ctx               context.Context
		moduleConfigToUse kmmv1beta1.ModuleConfig
		status            *kmmv1beta1.NodeModuleStatus
	)

	BeforeEach(func() {

		ctrl = gomock.NewController(GinkgoT())
		client = testclient.NewMockClient(ctrl)

		nmc = &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}

		mi = kmmv1beta1.ModuleItem{
			ImageRepoSecret:    &v1.LocalObjectReference{Name: irsName},
			Name:               moduleName,
			Namespace:          namespace,
			ServiceAccountName: serviceAccountName,
			Tolerations: []v1.Toleration{
				{
					Key:    "test-key",
					Value:  "test-value",
					Effect: v1.TaintEffectNoExecute,
				},
			},
		}

		moduleConfigToUse = moduleConfig
		moduleConfigToUse.Modprobe.FirmwarePath = "/firmware-path"
		status = &kmmv1beta1.NodeModuleStatus{
			ModuleItem: mi,
			Config:     moduleConfigToUse,
		}
		ctx = context.TODO()
	})

	It("it should fail if firmwareClassPath was not set but firmware loading was", func() {

		wpm := NewWorkerPodManager(client, workerImage, scheme, workerCfg)

		Expect(
			wpm.CreateUnloaderPod(ctx, nmc, status),
		).To(
			HaveOccurred(),
		)
	})

	It("should work as expected", func() {

		expected := getBaseWorkerPod("unload", nmc, ptr.To("/lib/firmware"), true, false, mi.ImageRepoSecret)

		container, _ := podcmd.FindContainerByName(expected, "worker")
		Expect(container).NotTo(BeNil())

		container.SecurityContext = &v1.SecurityContext{
			Capabilities: &v1.Capabilities{
				Add: []v1.Capability{"SYS_MODULE"},
			},
			RunAsUser:      workerCfg.RunAsUser,
			SELinuxOptions: &v1.SELinuxOptions{Type: workerCfg.SELinuxType},
		}

		hash, err := hashstructure.Hash(expected, hashstructure.FormatV2, nil)
		Expect(err).NotTo(HaveOccurred())

		expected.Annotations[HashAnnotationKey] = fmt.Sprintf("%d", hash)

		client.EXPECT().Create(ctx, cmpmock.DiffEq(expected))

		workerCfg := *workerCfg
		workerCfg.FirmwareHostPath = ptr.To("/lib/firmware")

		wpm := NewWorkerPodManager(client, workerImage, scheme, &workerCfg)

		Expect(
			wpm.CreateUnloaderPod(ctx, nmc, status),
		).NotTo(
			HaveOccurred(),
		)
	})
})

var _ = Describe("DeletePod", func() {
	ctx := context.TODO()
	now := metav1.Now()

	DescribeTable(
		"should work as expected",
		func(deletionTimestamp *metav1.Time) {
			ctrl := gomock.NewController(GinkgoT())
			kubeclient := testclient.NewMockClient(ctrl)

			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: deletionTimestamp,
					Finalizers:        []string{NodeModulesConfigFinalizer},
				},
			}

			patchedPod := pod
			patchedPod.Finalizers = nil

			patch := kubeclient.EXPECT().Patch(ctx, patchedPod, gomock.Any())

			if deletionTimestamp == nil {
				kubeclient.EXPECT().Delete(ctx, patchedPod).After(patch)
			}

			Expect(
				NewWorkerPodManager(kubeclient, workerImage, scheme, workerCfg).DeletePod(ctx, patchedPod),
			).NotTo(
				HaveOccurred(),
			)
		},
		Entry("deletionTimestamp not set", nil),
		Entry("deletionTimestamp set", &now),
	)
})

var _ = Describe("ListWorkerPodsOnNode", func() {
	const nodeName = "some-node"

	var (
		ctx = context.TODO()

		kubeClient *testclient.MockClient
		wpm        WorkerPodManager
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = testclient.NewMockClient(ctrl)
		wpm = NewWorkerPodManager(kubeClient, workerImage, scheme, nil)
	})

	opts := []interface{}{
		ctrlclient.HasLabels{ActionLabelKey},
		ctrlclient.MatchingFields{".spec.nodeName": nodeName},
	}

	It("should return an error if the kube client encountered one", func() {
		kubeClient.EXPECT().List(ctx, &v1.PodList{}, opts...).Return(errors.New("random error"))

		_, err := wpm.ListWorkerPodsOnNode(ctx, nodeName)
		Expect(err).To(HaveOccurred())
	})

	It("should the list of pods", func() {
		pods := []v1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-0"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
			},
		}

		kubeClient.
			EXPECT().
			List(ctx, &v1.PodList{}, opts...).
			Do(func(_ context.Context, pl *v1.PodList, _ ...ctrlclient.ListOption) {
				pl.Items = pods
			})

		Expect(
			wpm.ListWorkerPodsOnNode(ctx, nodeName),
		).To(
			Equal(pods),
		)
	})
})

func getBaseWorkerPod(subcommand string, owner ctrlclient.Object, firmwareHostPath *string,
	withFirmware, isLoaderPod bool, imagePullSecret *v1.LocalObjectReference) *v1.Pod {
	GinkgoHelper()

	const (
		volNameLibModules     = "lib-modules"
		volNameUsrLibModules  = "usr-lib-modules"
		volNameVarLibFirmware = "lib-firmware"
	)

	action := WorkerActionLoad
	if !isLoaderPod {
		action = WorkerActionUnload
	}

	hostPathDirectory := v1.HostPathDirectory
	hostPathDirectoryOrCreate := v1.HostPathDirectoryOrCreate

	configAnnotationValue := `containerImage: container image
imagePullPolicy: IfNotPresent
inTreeModulesToRemove:
- intree1
- intree2
insecurePull: true
kernelVersion: kernel-version
modprobe:
  dirName: /dir
  firmwarePath: /firmware-path
  moduleName: test
  modulesLoadingOrder:
  - a
  - b
  - c
  parameters:
  - a
  - b
`
	modulesOrderValue := `softdep a pre: b
softdep b pre: c
`

	var initContainerArg = `
mkdir -p /tmp/dir/lib/modules;
cp -R /dir/lib/modules/kernel-version /tmp/dir/lib/modules;
`

	const initContainerArgFirmwareAddition = `
mkdir -p /tmp/firmware-path;
cp -R /firmware-path/* /tmp/firmware-path;
`

	args := []string{"kmod", subcommand, "/etc/kmm-worker/config.yaml"}
	if withFirmware {
		args = append(args, "--firmware-path", *firmwareHostPath)
		initContainerArg = strings.Join([]string{initContainerArg, initContainerArgFirmwareAddition}, "")
	} else {
		configAnnotationValue = strings.ReplaceAll(configAnnotationValue, "firmwarePath: /firmware-path\n  ", "")
	}
	pod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      WorkerPodName(nmcName, moduleName),
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/component": "worker",
				"app.kubernetes.io/name":      "kmm",
				"app.kubernetes.io/part-of":   "kmm",
				ActionLabelKey:                string(action),
				constants.ModuleNameLabel:     moduleName,
			},
			Annotations: map[string]string{
				ConfigAnnotationKey: configAnnotationValue,
				modulesOrderKey:     modulesOrderValue,
			},
		},
		Spec: v1.PodSpec{
			Tolerations: []v1.Toleration{
				{
					Key:    "test-key",
					Value:  "test-value",
					Effect: v1.TaintEffectNoExecute,
				},
			},
			InitContainers: []v1.Container{
				{
					Name:            "image-extractor",
					Image:           "container image",
					ImagePullPolicy: v1.PullIfNotPresent,
					Command:         []string{"/bin/sh", "-c"},
					Args:            []string{initContainerArg},
					Resources: v1.ResourceRequirements{
						Limits:   limits,
						Requests: requests,
					},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      volNameTmp,
							MountPath: sharedFilesDir,
						},
					},
				},
			},
			Containers: []v1.Container{
				{
					Name:  "worker",
					Image: workerImage,
					Args:  args,
					Resources: v1.ResourceRequirements{
						Limits:   limits,
						Requests: requests,
					},
					SecurityContext: &v1.SecurityContext{},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      volNameConfig,
							MountPath: "/etc/kmm-worker",
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
						{
							Name:      "modules-order",
							ReadOnly:  true,
							MountPath: "/etc/modprobe.d",
						},
					},
				},
			},
			NodeName:           nmcName,
			RestartPolicy:      v1.RestartPolicyOnFailure,
			ServiceAccountName: serviceAccountName,
			Volumes: []v1.Volume{
				{
					Name: volumeNameConfig,
					VolumeSource: v1.VolumeSource{
						DownwardAPI: &v1.DownwardAPIVolumeSource{
							Items: []v1.DownwardAPIVolumeFile{
								{
									Path: "config.yaml",
									FieldRef: &v1.ObjectFieldSelector{
										FieldPath: fmt.Sprintf("metadata.annotations['%s']", ConfigAnnotationKey),
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
				{
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
				},
			},
		},
	}

	if withFirmware {
		fwVolMount := v1.VolumeMount{
			Name:      volNameVarLibFirmware,
			MountPath: *firmwareHostPath,
		}
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, fwVolMount)
		fwVol := v1.Volume{
			Name: volNameVarLibFirmware,
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: *firmwareHostPath,
					Type: &hostPathDirectoryOrCreate,
				},
			},
		}
		pod.Spec.Volumes = append(pod.Spec.Volumes, fwVol)

	}

	if imagePullSecret != nil {
		pod.Spec.ImagePullSecrets = []v1.LocalObjectReference{*imagePullSecret}
	}

	Expect(
		controllerutil.SetControllerReference(owner, &pod, scheme),
	).NotTo(
		HaveOccurred(),
	)

	controllerutil.AddFinalizer(&pod, NodeModulesConfigFinalizer)

	return &pod
}
