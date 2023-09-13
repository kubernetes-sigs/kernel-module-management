package daemonset

import (
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

const (
	kernelVersion     = "1.2.3"
	moduleName        = "module-name"
	namespace         = "namespace"
	kernelLabel       = "kernel-label"
	devicePluginImage = "device-plugin-image"
)

var (
	ctrl *gomock.Controller
	clnt *client.MockClient
	dc   DaemonSetCreator
)

var _ = Describe("SetDevicePluginAsDesired", func() {

	BeforeEach(func() {
		dc = NewCreator(nil, kernelLabel, scheme)
	})

	It("should return an error if the DaemonSet is nil", func() {
		Expect(
			dc.SetDevicePluginAsDesired(context.Background(), nil, &kmmv1beta1.Module{}),
		).To(
			HaveOccurred(),
		)
	})

	It("should return an error if DevicePlugin not set in the Spec", func() {
		ds := appsv1.DaemonSet{}
		Expect(
			dc.SetDevicePluginAsDesired(context.Background(), &ds, &kmmv1beta1.Module{}),
		).To(
			HaveOccurred(),
		)
	})

	It("should add additional volumes if there are any", func() {
		vol := v1.Volume{Name: "test-volume"}

		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{
					Container: kmmv1beta1.DevicePluginContainerSpec{Image: devicePluginImage},
					Volumes:   []v1.Volume{vol},
				},
			},
		}

		ds := appsv1.DaemonSet{}

		err := dc.SetDevicePluginAsDesired(context.Background(), &ds, &mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(2))
		Expect(ds.Spec.Template.Spec.Volumes[1]).To(Equal(vol))
	})

	It("should add module version if it was defined in the Module", func() {
		vol := v1.Volume{Name: "test-volume"}

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						Version: "some version",
					},
				},
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{
					Container: kmmv1beta1.DevicePluginContainerSpec{Image: devicePluginImage},
					Volumes:   []v1.Volume{vol},
				},
			},
		}

		ds := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
		}

		err := dc.SetDevicePluginAsDesired(context.Background(), &ds, &mod)

		Expect(err).NotTo(HaveOccurred())
		versionLabel := utils.GetDevicePluginVersionLabelName(namespace, moduleName)
		Expect(ds.GetLabels()).Should(HaveKeyWithValue(versionLabel, "some version"))
	})

	It("should work as expected", func() {
		const (
			dsName             = "ds-name"
			serviceAccountName = "some-service-account"
		)

		dpVol := v1.Volume{
			Name:         "test-volume",
			VolumeSource: v1.VolumeSource{},
		}

		dpVolMount := v1.VolumeMount{
			Name:      "some-dp-volume-mount",
			MountPath: "/some/path",
		}

		repoSecret := v1.LocalObjectReference{Name: "pull-secret-name"}

		env := []v1.EnvVar{
			{
				Name:  "ENV_KEY",
				Value: "ENV_VALUE",
			},
		}

		resources := v1.ResourceRequirements{
			Limits: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU:    resource.MustParse("200m"),
				v1.ResourceMemory: resource.MustParse("4G"),
			},
			Requests: map[v1.ResourceName]resource.Quantity{
				v1.ResourceCPU:    resource.MustParse("100m"),
				v1.ResourceMemory: resource.MustParse("2G"),
			},
		}

		args := []string{"some", "args"}
		command := []string{"some", "command"}

		const ipp = v1.PullIfNotPresent

		mod := kmmv1beta1.Module{
			TypeMeta: metav1.TypeMeta{
				APIVersion: kmmv1beta1.GroupVersion.String(),
				Kind:       "Module",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ModuleSpec{
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{
					Container: kmmv1beta1.DevicePluginContainerSpec{
						Args:            args,
						Command:         command,
						Env:             env,
						Image:           devicePluginImage,
						ImagePullPolicy: ipp,
						Resources:       resources,
						VolumeMounts:    []v1.VolumeMount{dpVolMount},
					},
					ServiceAccountName: serviceAccountName,
					Volumes:            []v1.Volume{dpVol},
				},
				ImageRepoSecret: &repoSecret,
				Selector:        map[string]string{"has-feature-x": "true"},
			},
		}

		ds := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dsName,
				Namespace: namespace,
			},
		}

		err := dc.SetDevicePluginAsDesired(context.Background(), &ds, &mod)
		Expect(err).NotTo(HaveOccurred())

		podLabels := map[string]string{
			constants.ModuleNameLabel: moduleName,
			constants.DaemonSetRole:   "device-plugin",
		}

		directory := v1.HostPathDirectory

		expected := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dsName,
				Namespace: namespace,
				Labels:    podLabels,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         mod.APIVersion,
						BlockOwnerDeletion: pointer.Bool(true),
						Controller:         pointer.Bool(true),
						Kind:               mod.Kind,
						Name:               moduleName,
						UID:                mod.UID,
					},
				},
			},
			Spec: appsv1.DaemonSetSpec{
				Selector: &metav1.LabelSelector{MatchLabels: podLabels},
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels:     podLabels,
						Finalizers: []string{constants.NodeLabelerFinalizer},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Args:            args,
								Command:         command,
								Env:             env,
								Image:           devicePluginImage,
								ImagePullPolicy: ipp,
								Name:            "device-plugin",
								Resources:       resources,
								SecurityContext: &v1.SecurityContext{
									Privileged: pointer.Bool(true),
								},
								VolumeMounts: []v1.VolumeMount{
									dpVolMount,
									{
										Name:      "kubelet-device-plugins",
										MountPath: "/var/lib/kubelet/device-plugins",
									},
								},
							},
						},
						ImagePullSecrets: []v1.LocalObjectReference{repoSecret},
						NodeSelector: map[string]string{
							getDriverContainerNodeLabel(mod.Namespace, mod.Name, true): "",
						},
						PriorityClassName:  "system-node-critical",
						ServiceAccountName: serviceAccountName,
						Volumes: []v1.Volume{
							{
								Name: "kubelet-device-plugins",
								VolumeSource: v1.VolumeSource{
									HostPath: &v1.HostPathVolumeSource{
										Path: "/var/lib/kubelet/device-plugins",
										Type: &directory,
									},
								},
							},
							dpVol,
						},
					},
				},
			},
		}

		Expect(
			cmp.Equal(expected, ds),
		).To(
			BeTrue(), cmp.Diff(expected, ds),
		)
	})
})

var _ = Describe("GarbageCollect", func() {
	const (
		legitKernelVersion    = "legit-kernel-version"
		notLegitKernelVersion = "not-legit-kernel-version"
		currentModuleVersion  = "current label"
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		dc = NewCreator(clnt, kernelLabel, scheme)
	})

	mod := &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "moduleName",
			Namespace: "namespace",
		},
		Spec: kmmv1beta1.ModuleSpec{
			ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
				Container: kmmv1beta1.ModuleLoaderContainerSpec{
					Version: currentModuleVersion,
				},
			},
		},
	}
	devicePluginVersionLabel := utils.GetDevicePluginVersionLabelName(mod.Namespace, mod.Name)

	DescribeTable("device-plugin GC", func(devicePluginFormerLabel bool, devicePluginFormerDesired int) {
		devicePluginDS := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "devicePlugin",
				Namespace: "namespace",
				Labels: map[string]string{
					constants.DaemonSetRole:   constants.DevicePluginRoleLabelValue,
					devicePluginVersionLabel:  currentModuleVersion,
					constants.ModuleNameLabel: mod.Name,
				},
			},
		}
		devicePluginFormerVersionDS := &appsv1.DaemonSet{}

		existingDS := []appsv1.DaemonSet{devicePluginDS}
		expectedDeleteNames := []string{}
		if devicePluginFormerLabel {
			devicePluginFormerVersionDS = devicePluginDS.DeepCopy()
			devicePluginFormerVersionDS.SetName("devicePluginFormer")
			devicePluginFormerVersionDS.Labels[devicePluginVersionLabel] = "former label"
			devicePluginFormerVersionDS.Status.DesiredNumberScheduled = int32(devicePluginFormerDesired)
			existingDS = append(existingDS, *devicePluginFormerVersionDS)
		}
		if devicePluginFormerLabel && devicePluginFormerDesired == 0 {
			expectedDeleteNames = append(expectedDeleteNames, "devicePluginFormer")
			clnt.EXPECT().Delete(context.Background(), devicePluginFormerVersionDS).Return(nil)
		}

		res, err := dc.GarbageCollect(context.Background(), mod, existingDS)

		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(expectedDeleteNames))
	},
		Entry("no deletes", false, 0),
		Entry("former device plugin", true, 0),
		Entry("former device plugin has desired", true, 1),
	)

	It("should return an error if a deletion failed", func() {
		deleteDS := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "devicePlugin",
				Namespace: "namespace",
				Labels:    map[string]string{constants.DaemonSetRole: constants.DevicePluginRoleLabelValue, devicePluginVersionLabel: "formerVersion"},
			},
		}
		clnt.EXPECT().Delete(context.Background(), &deleteDS).Return(fmt.Errorf("some error"))

		existingDS := []appsv1.DaemonSet{deleteDS}

		_, err := dc.GarbageCollect(context.Background(), mod, existingDS)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("GetModuleDaemonSets", func() {
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
	})

	It("should return an empty map if no DaemonSets are present", func() {
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any())

		dc := NewCreator(clnt, kernelLabel, scheme)

		s, err := dc.GetModuleDaemonSets(context.Background(), moduleName, namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(s).To(BeEmpty())
	})

	It("should return all daemonsets return by client", func() {
		const otherKernelVersion = "4.5.6"

		ds1 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ds1",
				Namespace: namespace,
				Labels: map[string]string{
					"kmm.node.kubernetes.io/module.name": moduleName,
					kernelLabel:                          kernelVersion,
				},
			},
		}

		ds2 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ds2",
				Namespace: namespace,
				Labels: map[string]string{
					"kmm.node.kubernetes.io/module.name": moduleName,
					kernelLabel:                          otherKernelVersion,
				},
			},
		}

		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *appsv1.DaemonSetList, _ ...interface{}) error {
				list.Items = append(list.Items, ds1)
				list.Items = append(list.Items, ds2)
				return nil
			},
		)
		expectSlice := []appsv1.DaemonSet{ds1, ds2}

		dc := NewCreator(clnt, kernelLabel, scheme)

		s, err := dc.GetModuleDaemonSets(context.Background(), moduleName, namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(s).To(HaveLen(2))
		Expect(s).To(Equal(expectSlice))
	})

})

var _ = Describe("GetPodPullSecrets", func() {
	It("should return nil if the secret is nil", func() {
		Expect(
			GetPodPullSecrets(nil),
		).To(
			BeNil(),
		)
	})

	It("should a slice with the secret inside", func() {
		lor := v1.LocalObjectReference{Name: "test"}

		Expect(
			GetPodPullSecrets(&lor),
		).To(
			Equal([]v1.LocalObjectReference{lor}),
		)
	})
})

var _ = Describe("OverrideLabels", func() {
	It("should create a labels map if it was empty", func() {
		overrides := map[string]string{"a": "b"}

		Expect(
			OverrideLabels(nil, overrides),
		).To(
			Equal(overrides),
		)
	})

	It("should override existing values", func() {
		labels := map[string]string{"a": "b", "c": "d"}
		overrides := map[string]string{"a": "z"}

		Expect(
			OverrideLabels(labels, overrides),
		).To(
			Equal(map[string]string{"a": "z", "c": "d"}),
		)
	})
})

var _ = Describe("GetNodeLabelFromPod", func() {

	const namespace = "some-namespace"
	var dc DaemonSetCreator

	BeforeEach(func() {
		dc = NewCreator(clnt, kernelLabel, scheme)
	})

	It("should return a driver container label", func() {
		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					constants.ModuleNameLabel: moduleName,
					constants.DaemonSetRole:   "module-loader",
				},
				Namespace: namespace,
			},
		}
		res := dc.GetNodeLabelFromPod(&pod, "module-name", false)
		Expect(res).To(Equal(getDriverContainerNodeLabel(namespace, "module-name", false)))
		res = dc.GetNodeLabelFromPod(&pod, "module-name", true)
		Expect(res).To(Equal(getDriverContainerNodeLabel(namespace, "module-name", true)))
	})

	It("should return a device plugin label", func() {
		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					constants.ModuleNameLabel: moduleName,
					constants.DaemonSetRole:   "device-plugin",
				},
				Namespace: namespace,
			},
		}
		res := dc.GetNodeLabelFromPod(&pod, "module-name", false)
		Expect(res).To(Equal(getDevicePluginNodeLabel(namespace, "module-name", false)))
		res = dc.GetNodeLabelFromPod(&pod, "module-name", true)
		Expect(res).To(Equal(getDevicePluginNodeLabel(namespace, "module-name", true)))
	})
})
