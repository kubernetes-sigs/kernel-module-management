package daemonset

import (
	"context"
	"errors"

	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/internal/client"
	"github.com/qbarrand/oot-operator/internal/constants"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
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
)

var _ = Describe("SetDriverContainerAsDesired", func() {
	dg := NewCreator(nil, kernelLabel, scheme)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
	})

	It("should return an error if the DaemonSet is nil", func() {
		Expect(
			dg.SetDriverContainerAsDesired(context.Background(), nil, "", ootov1alpha1.Module{}, ""),
		).To(
			HaveOccurred(),
		)
	})

	It("should return an error if the image is empty", func() {
		Expect(
			dg.SetDriverContainerAsDesired(context.Background(), &appsv1.DaemonSet{}, "", ootov1alpha1.Module{}, ""),
		).To(
			HaveOccurred(),
		)
	})

	It("should return an error if the kernel version is empty", func() {
		Expect(
			dg.SetDriverContainerAsDesired(context.Background(), &appsv1.DaemonSet{}, "", ootov1alpha1.Module{}, ""),
		).To(
			HaveOccurred(),
		)
	})

	It("should not add a device-plugin container if it is not set in the spec", func() {
		mod := ootov1alpha1.Module{
			Spec: ootov1alpha1.ModuleSpec{
				Selector: map[string]string{"has-feature-x": "true"},
			},
		}

		ds := appsv1.DaemonSet{}

		err := dg.SetDriverContainerAsDesired(context.Background(), &ds, "test-image", mod, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(ds.Spec.Template.Spec.Containers).To(HaveLen(1))
		Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(2))
	})

	It("should add additional volumes if there are any", func() {
		vol := v1.Volume{Name: "test-volume"}

		mod := ootov1alpha1.Module{
			Spec: ootov1alpha1.ModuleSpec{
				AdditionalVolumes: []v1.Volume{vol},
			},
		}

		ds := appsv1.DaemonSet{}

		err := dg.SetDriverContainerAsDesired(context.Background(), &ds, "test-image", mod, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(3))
		Expect(ds.Spec.Template.Spec.Volumes[2]).To(Equal(vol))
	})

	It("should work as expected", func() {
		const (
			driverContainerImage = "driver-image"
			dsName               = "ds-name"
			serviceAccountName   = "some-service-account"
		)

		dcVolMount := v1.VolumeMount{
			Name:      "some-dc-volume-mount",
			ReadOnly:  true,
			MountPath: "/some/path",
		}

		dpVolMount := v1.VolumeMount{
			Name:      "some-dp-volume-mount",
			MountPath: "/some/path",
		}

		mod := ootov1alpha1.Module{
			TypeMeta: metav1.TypeMeta{
				APIVersion: ootov1alpha1.GroupVersion.String(),
				Kind:       "Module",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
			Spec: ootov1alpha1.ModuleSpec{
				DriverContainer: v1.Container{
					VolumeMounts: []v1.VolumeMount{dcVolMount},
				},
				DevicePlugin: &v1.Container{
					Image:        devicePluginImage,
					VolumeMounts: []v1.VolumeMount{dpVolMount},
				},
				Selector:           map[string]string{"has-feature-x": "true"},
				ServiceAccountName: serviceAccountName,
				ImagePullSecret: &v1.LocalObjectReference{
					Name: "pull-push-secret",
				},
			},
		}

		ds := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dsName,
				Namespace: namespace,
			},
		}

		err := dg.SetDriverContainerAsDesired(context.Background(), &ds, driverContainerImage, mod, kernelVersion)
		Expect(err).NotTo(HaveOccurred())

		podLabels := map[string]string{
			constants.ModuleNameLabel: moduleName,
			kernelLabel:               kernelVersion,
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
						Finalizers: []string{constants.NodeLabelerFinalizer},
						Labels:     podLabels,
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "driver-container",
								Image: driverContainerImage,
								VolumeMounts: []v1.VolumeMount{
									dcVolMount,
									{
										Name:      "node-lib-modules",
										ReadOnly:  true,
										MountPath: "/lib/modules",
									},
									{
										Name:      "node-usr-lib-modules",
										ReadOnly:  true,
										MountPath: "/usr/lib/modules",
									},
								},
							},
						},
						NodeSelector: map[string]string{
							"has-feature-x": "true",
							kernelLabel:     kernelVersion,
						},
						ServiceAccountName: serviceAccountName,
						Volumes: []v1.Volume{
							{
								Name: "node-lib-modules",
								VolumeSource: v1.VolumeSource{
									HostPath: &v1.HostPathVolumeSource{
										Path: "/lib/modules",
										Type: &directory,
									},
								},
							},
							{
								Name: "node-usr-lib-modules",
								VolumeSource: v1.VolumeSource{
									HostPath: &v1.HostPathVolumeSource{
										Path: "/usr/lib/modules",
										Type: &directory,
									},
								},
							},
						},
						ImagePullSecrets: []v1.LocalObjectReference{
							{
								Name: "pull-push-secret",
							},
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

	Describe("ModuleDaemonSetsByKernelVersion", func() {
		It("should return an empty map if no DaemonSets are present", func() {
			clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any())

			dc := NewCreator(clnt, kernelLabel, scheme)

			mod := ootov1alpha1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: namespace,
				},
			}

			m, err := dc.ModuleDaemonSetsByKernelVersion(context.Background(), mod.Name, mod.Namespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(BeEmpty())
		})

		It("should return an error if two DaemonSets are present for the same kernel", func() {
			clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).Return(errors.New("some error"))

			dc := NewCreator(clnt, kernelLabel, scheme)
			mod := ootov1alpha1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: namespace,
				},
			}

			_, err := dc.ModuleDaemonSetsByKernelVersion(context.Background(), mod.Name, mod.Namespace)
			Expect(err).To(HaveOccurred())
		})

		It("should return a map if two DaemonSets are present for different kernels", func() {
			const otherKernelVersion = "4.5.6"

			ds1 := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ds1",
					Namespace: namespace,
					Labels: map[string]string{
						"oot.node.kubernetes.io/module.name": moduleName,
						kernelLabel:                          kernelVersion,
					},
				},
			}

			ds2 := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ds2",
					Namespace: namespace,
					Labels: map[string]string{
						"oot.node.kubernetes.io/module.name": moduleName,
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

			dc := NewCreator(clnt, kernelLabel, scheme)
			mod := ootov1alpha1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: namespace,
				},
			}

			m, err := dc.ModuleDaemonSetsByKernelVersion(context.Background(), mod.Name, mod.Namespace)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(HaveLen(2))
			Expect(m).To(HaveKeyWithValue(kernelVersion, &ds1))
			Expect(m).To(HaveKeyWithValue(otherKernelVersion, &ds2))
		})
	})
})

var _ = Describe("SetDevicePluginAsDesired", func() {
	dg := NewCreator(nil, kernelLabel, scheme)

	It("should return an error if the DaemonSet is nil", func() {
		Expect(
			dg.SetDevicePluginAsDesired(context.Background(), nil, &ootov1alpha1.Module{}),
		).To(
			HaveOccurred(),
		)
	})

	It("should return an error if DevicePlugin not set in the Spec", func() {
		ds := appsv1.DaemonSet{}
		Expect(
			dg.SetDevicePluginAsDesired(context.Background(), &ds, &ootov1alpha1.Module{}),
		).To(
			HaveOccurred(),
		)
	})

	It("should add additional volumes if there are any", func() {
		vol := v1.Volume{Name: "test-volume"}

		mod := ootov1alpha1.Module{
			Spec: ootov1alpha1.ModuleSpec{
				DevicePlugin: &v1.Container{
					Image: devicePluginImage,
				},
				AdditionalVolumes: []v1.Volume{vol},
			},
		}

		ds := appsv1.DaemonSet{}

		err := dg.SetDevicePluginAsDesired(context.Background(), &ds, &mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(2))
		Expect(ds.Spec.Template.Spec.Volumes[1]).To(Equal(vol))
	})

	It("should work as expected", func() {
		const (
			dsName             = "ds-name"
			serviceAccountName = "some-service-account"
		)

		dcVolMount := v1.VolumeMount{
			Name:      "some-dc-volume-mount",
			ReadOnly:  true,
			MountPath: "/some/path",
		}

		dpVolMount := v1.VolumeMount{
			Name:      "some-dp-volume-mount",
			MountPath: "/some/path",
		}

		mod := ootov1alpha1.Module{
			TypeMeta: metav1.TypeMeta{
				APIVersion: ootov1alpha1.GroupVersion.String(),
				Kind:       "Module",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
			Spec: ootov1alpha1.ModuleSpec{
				DriverContainer: v1.Container{
					VolumeMounts: []v1.VolumeMount{dcVolMount},
				},
				DevicePlugin: &v1.Container{
					Image:        devicePluginImage,
					VolumeMounts: []v1.VolumeMount{dpVolMount},
				},
				Selector:           map[string]string{"has-feature-x": "true"},
				ServiceAccountName: serviceAccountName,
			},
		}

		ds := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dsName,
				Namespace: namespace,
			},
		}

		err := dg.SetDevicePluginAsDesired(context.Background(), &ds, &mod)
		Expect(err).NotTo(HaveOccurred())

		podLabels := map[string]string{
			constants.ModuleNameLabel: moduleName,
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
					ObjectMeta: metav1.ObjectMeta{Labels: podLabels},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "device-plugin",
								Image: devicePluginImage,
								VolumeMounts: []v1.VolumeMount{
									dpVolMount,
									{
										Name:      "kubelet-device-plugins",
										MountPath: "/var/lib/kubelet/device-plugins",
									},
								},
								SecurityContext: &v1.SecurityContext{
									Privileged: pointer.Bool(true),
								},
							},
						},
						NodeSelector: map[string]string{
							GetDriverContainerNodeLabel(mod.Name): "",
						},
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
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
	})

	It("should only delete one of the two DaemonSets if only one is not used", func() {
		const (
			legitKernelVersion    = "legit-kernel-version"
			legitName             = "legit"
			notLegitKernelVersion = "not-legit-kernel-version"
			notLegitName          = "not-legit"
		)

		dsLegit := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: legitName, Namespace: namespace, Labels: map[string]string{kernelLabel: legitKernelVersion}},
		}

		dsNotLegit := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: notLegitName, Namespace: namespace, Labels: map[string]string{kernelLabel: notLegitKernelVersion}},
		}

		clnt.EXPECT().Delete(context.Background(), &dsNotLegit).AnyTimes()

		dc := NewCreator(clnt, kernelLabel, scheme)

		existingDS := map[string]*appsv1.DaemonSet{
			legitKernelVersion:    &dsLegit,
			notLegitKernelVersion: &dsNotLegit,
		}

		validKernels := sets.NewString(legitKernelVersion)

		res, err := dc.GarbageCollect(context.Background(), existingDS, validKernels)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal([]string{notLegitName}))
	})

	It("should return an error if a deletion failed", func() {
		clnt.EXPECT().Delete(context.Background(), gomock.Any()).Return(
			errors.New("client returns some error"),
		)

		dc := NewCreator(clnt, kernelLabel, scheme)

		dsNotLegit := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "name", Namespace: "namespace", Labels: map[string]string{kernelLabel: "kernel version"}},
		}

		existingDS := map[string]*appsv1.DaemonSet{
			"some-kernel-version": &dsNotLegit,
		}

		_, err := dc.GarbageCollect(context.Background(), existingDS, sets.NewString())
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("ModuleDaemonSetsByKernelVersion", func() {
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
	})

	It("should return an empty map if no DaemonSets are present", func() {
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any())

		dc := NewCreator(clnt, kernelLabel, scheme)

		m, err := dc.ModuleDaemonSetsByKernelVersion(context.Background(), moduleName, namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(m).To(BeEmpty())
	})

	It("should return an error if two DaemonSets are present for the same kernel", func() {
		dsLabels := map[string]string{
			"oot.node.kubernetes.io/module.name": moduleName,
			kernelLabel:                          kernelVersion,
		}

		ds1 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ds1",
				Namespace: namespace,
				Labels:    dsLabels,
			},
		}

		ds2 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ds2",
				Namespace: namespace,
				Labels:    dsLabels,
			},
		}

		ctx := context.Background()
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *appsv1.DaemonSetList, _ ...interface{}) error {
				list.Items = []appsv1.DaemonSet{ds1, ds2}
				return nil
			},
		)
		dc := NewCreator(clnt, kernelLabel, scheme)

		_, err := dc.ModuleDaemonSetsByKernelVersion(ctx, moduleName, namespace)
		Expect(err).To(HaveOccurred())
	})

	It("should return a map if two DaemonSets are present for different kernels", func() {
		const otherKernelVersion = "4.5.6"

		ds1 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ds1",
				Namespace: namespace,
				Labels: map[string]string{
					"oot.node.kubernetes.io/module.name": moduleName,
					kernelLabel:                          kernelVersion,
				},
			},
		}

		ds2 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ds2",
				Namespace: namespace,
				Labels: map[string]string{
					"oot.node.kubernetes.io/module.name": moduleName,
					kernelLabel:                          otherKernelVersion,
				},
			},
		}

		ctx := context.Background()

		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *appsv1.DaemonSetList, _ ...interface{}) error {
				list.Items = []appsv1.DaemonSet{ds1, ds2}
				return nil
			},
		)

		dc := NewCreator(clnt, kernelLabel, scheme)

		m, err := dc.ModuleDaemonSetsByKernelVersion(ctx, moduleName, namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(m).To(HaveLen(2))
		Expect(m).To(HaveKeyWithValue(kernelVersion, &ds1))
		Expect(m).To(HaveKeyWithValue(otherKernelVersion, &ds2))
	})

	It("should include a map entry for device plugin", func() {
		ds1 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ds1",
				Namespace: namespace,
				Labels: map[string]string{
					"oot.node.kubernetes.io/module.name": moduleName,
					kernelLabel:                          kernelVersion,
				},
			},
		}

		ds2 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ds2",
				Namespace: namespace,
				Labels: map[string]string{
					"oot.node.kubernetes.io/module.name": moduleName,
				},
			},
		}

		ctx := context.Background()

		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *appsv1.DaemonSetList, _ ...interface{}) error {
				list.Items = []appsv1.DaemonSet{ds1, ds2}
				return nil
			},
		)

		dc := NewCreator(clnt, kernelLabel, scheme)

		m, err := dc.ModuleDaemonSetsByKernelVersion(context.Background(), moduleName, namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(m).To(HaveLen(2))
		Expect(m).To(HaveKeyWithValue(kernelVersion, &ds1))
		Expect(m).To(HaveKeyWithValue(devicePluginKernelVersion, &ds2))
	})
})

var _ = Describe("GetPodPullSecrets", func() {
	It("should return nil if the Module has no pull secret", func() {
		Expect(
			GetPodPullSecrets(ootov1alpha1.Module{}),
		).To(
			BeNil(),
		)
	})

	It("should a list with the reference nil if the Module has no pull secret", func() {
		lor := &v1.LocalObjectReference{Name: "test"}

		mod := ootov1alpha1.Module{
			Spec: ootov1alpha1.ModuleSpec{ImagePullSecret: lor},
		}

		Expect(
			GetPodPullSecrets(mod),
		).To(
			Equal([]v1.LocalObjectReference{*lor}),
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
