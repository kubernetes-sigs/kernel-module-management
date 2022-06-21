package controllers_test

import (
	"context"

	"github.com/go-openapi/swag"
	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/controllers"
	"github.com/qbarrand/oot-operator/controllers/constants"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	kernelVersion     = "1.2.3"
	moduleName        = "module-name"
	namespace         = "namespace"
	kernelLabel       = "kernel-label"
	devicePluginImage = "device-plugin-image"
)

var _ = Describe("SetDriverContainerAsDesired", func() {
	dg := controllers.NewDaemonSetCreator(nil, kernelLabel, scheme)

	It("should return an error if the DaemonSet is nil", func() {
		Expect(
			dg.SetDriverContainerAsDesired(context.TODO(), nil, "", ootov1alpha1.Module{}, ""),
		).To(
			HaveOccurred(),
		)
	})

	It("should return an error if the image is empty", func() {
		Expect(
			dg.SetDriverContainerAsDesired(context.TODO(), &appsv1.DaemonSet{}, "", ootov1alpha1.Module{}, ""),
		).To(
			HaveOccurred(),
		)
	})

	It("should return an error if the kernel version is empty", func() {
		Expect(
			dg.SetDriverContainerAsDesired(context.TODO(), &appsv1.DaemonSet{}, "", ootov1alpha1.Module{}, ""),
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

		err := dg.SetDriverContainerAsDesired(context.TODO(), &ds, "test-image", mod, kernelVersion)
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

		err := dg.SetDriverContainerAsDesired(context.TODO(), &ds, "test-image", mod, kernelVersion)
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
			},
		}

		ds := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dsName,
				Namespace: namespace,
			},
		}

		err := dg.SetDriverContainerAsDesired(context.TODO(), &ds, driverContainerImage, mod, kernelVersion)
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
						BlockOwnerDeletion: swag.Bool(true),
						Controller:         swag.Bool(true),
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

var _ = Describe("SetDevicePluginAsDesired", func() {
	dg := controllers.NewDaemonSetCreator(nil, kernelLabel, scheme)

	It("should return an error if the DaemonSet is nil", func() {
		Expect(
			dg.SetDevicePluginAsDesired(context.TODO(), nil, &ootov1alpha1.Module{}),
		).To(
			HaveOccurred(),
		)
	})

	It("should return an error if DevicePlugin not set in the Spec", func() {
		ds := appsv1.DaemonSet{}
		Expect(
			dg.SetDevicePluginAsDesired(context.TODO(), &ds, &ootov1alpha1.Module{}),
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

		err := dg.SetDevicePluginAsDesired(context.TODO(), &ds, &mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(2))
		Expect(ds.Spec.Template.Spec.Volumes[1]).To(Equal(vol))
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
			},
		}

		ds := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dsName,
				Namespace: namespace,
			},
		}

		err := dg.SetDevicePluginAsDesired(context.TODO(), &ds, &mod)
		Expect(err).NotTo(HaveOccurred())

		podLabels := map[string]string{
			constants.ModuleNameLabel: moduleName,
		}

		directory := v1.HostPathDirectory
		trueVar := true

		expected := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dsName,
				Namespace: namespace,
				Labels:    podLabels,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         mod.APIVersion,
						BlockOwnerDeletion: &trueVar,
						Controller:         &trueVar,
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
									Privileged: swag.Bool(true),
								},
							},
						},
						NodeSelector: map[string]string{
							"has-feature-x": "true",
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
	It("should only delete one of the two DaemonSets if only one is not used", func() {
		const (
			legitKernelVersion    = "legit-kernel-version"
			legitName             = "legit"
			notLegitKernelVersion = "not-legit-kernel-version"
			notLegitName          = "not-legit"
		)

		dsLegit := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: legitName, Namespace: namespace},
		}

		dsNotLegit := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: notLegitName, Namespace: namespace},
		}

		client := fake.NewClientBuilder().WithObjects(&dsLegit, &dsNotLegit).Build()

		dc := controllers.NewDaemonSetCreator(client, "", scheme)

		existingDS := map[string]*appsv1.DaemonSet{
			legitKernelVersion:    &dsLegit,
			notLegitKernelVersion: &dsNotLegit,
		}

		validKernels := sets.NewString(legitKernelVersion)

		Expect(
			dc.GarbageCollect(context.TODO(), existingDS, validKernels),
		).To(
			Equal([]string{notLegitName}),
		)

		afterDeleteDaemonSetList := appsv1.DaemonSetList{}

		Expect(
			client.List(context.TODO(), &afterDeleteDaemonSetList),
		).To(
			Succeed(),
		)

		Expect(afterDeleteDaemonSetList.Items).To(HaveLen(1))
		Expect(afterDeleteDaemonSetList.Items[0].Name).To(Equal(legitName))
	})

	It("should return an error if a deletion failed", func() {
		dc := controllers.NewDaemonSetCreator(
			fake.NewClientBuilder().Build(),
			"",
			scheme)

		existingDS := map[string]*appsv1.DaemonSet{
			"some-kernel-version": {},
		}

		_, err := dc.GarbageCollect(context.TODO(), existingDS, sets.NewString())
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("ModuleDaemonSetsByKernelVersion", func() {
	It("should return an empty map if no DaemonSets are present", func() {
		dc := controllers.NewDaemonSetCreator(
			fake.NewClientBuilder().WithScheme(scheme).Build(),
			kernelLabel,
			scheme)

		m, err := dc.ModuleDaemonSetsByKernelVersion(context.TODO(), moduleName, namespace)
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

		dc := controllers.NewDaemonSetCreator(
			fake.NewClientBuilder().WithScheme(scheme).WithObjects(&ds1, &ds2).Build(),
			kernelLabel,
			scheme)

		_, err := dc.ModuleDaemonSetsByKernelVersion(context.TODO(), moduleName, namespace)
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

		dc := controllers.NewDaemonSetCreator(
			fake.NewClientBuilder().WithScheme(scheme).WithObjects(&ds1, &ds2).Build(),
			kernelLabel,
			scheme)

		m, err := dc.ModuleDaemonSetsByKernelVersion(context.TODO(), moduleName, namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(m).To(HaveLen(2))
		Expect(m).To(HaveKeyWithValue(kernelVersion, &ds1))
		Expect(m).To(HaveKeyWithValue(otherKernelVersion, &ds2))
	})

	It("should skip a map entry for device plugin", func() {
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

		dc := controllers.NewDaemonSetCreator(
			fake.NewClientBuilder().WithScheme(scheme).WithObjects(&ds1, &ds2).Build(),
			kernelLabel,
			scheme)

		m, err := dc.ModuleDaemonSetsByKernelVersion(context.TODO(), moduleName, namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(m).To(HaveLen(1))
		Expect(m).To(HaveKeyWithValue(kernelVersion, &ds1))
	})

})
