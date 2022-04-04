package controllers_test

import (
	"context"

	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	"github.com/qbarrand/oot-operator/controllers"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	dsNamespace   = "ds-namespace"
	kernelVersion = "1.2.3"
	moduleName    = "module-name"
)

var _ = Describe("daemonSetGenerator", func() {
	const kernelLabel = "kernel-label"

	Describe("SetAsDesired", func() {
		dg := controllers.NewDaemonSetCreator(nil, kernelLabel, "", scheme)

		It("should return an error if the DaemonSet is nil", func() {
			Expect(
				dg.SetAsDesired(nil, "", ootov1beta1.Module{}, ""),
			).To(
				HaveOccurred(),
			)
		})

		It("should return an error if the image is empty", func() {
			Expect(
				dg.SetAsDesired(&appsv1.DaemonSet{}, "", ootov1beta1.Module{}, ""),
			).To(
				HaveOccurred(),
			)
		})

		It("should return an error if the kernel version is empty", func() {
			Expect(
				dg.SetAsDesired(&appsv1.DaemonSet{}, "", ootov1beta1.Module{}, ""),
			).To(
				HaveOccurred(),
			)
		})

		It("should not add a device-plugin container if it is not set in the spec", func() {
			mod := ootov1beta1.Module{
				Spec: ootov1beta1.ModuleSpec{
					Selector: map[string]string{"has-feature-x": "true"},
				},
			}

			ds := appsv1.DaemonSet{}

			err := dg.SetAsDesired(&ds, "test-image", mod, kernelVersion)
			Expect(err).NotTo(HaveOccurred())
			Expect(ds.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(2))
		})

		It("should add additional volumes if there are any", func() {
			vol := v1.Volume{Name: "test-volume"}

			mod := ootov1beta1.Module{
				Spec: ootov1beta1.ModuleSpec{
					AdditionalVolumes: []v1.Volume{vol},
				},
			}

			ds := appsv1.DaemonSet{}

			err := dg.SetAsDesired(&ds, "test-image", mod, kernelVersion)
			Expect(err).NotTo(HaveOccurred())
			Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(3))
			Expect(ds.Spec.Template.Spec.Volumes[2]).To(Equal(vol))
		})

		It("should work as expected", func() {
			const (
				devicePluginImage    = "device-plugin-image"
				driverContainerImage = "driver-image"
				dsName               = "ds-name"
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

			mod := ootov1beta1.Module{
				TypeMeta: metav1.TypeMeta{
					APIVersion: ootov1beta1.GroupVersion.String(),
					Kind:       "Module",
				},
				ObjectMeta: metav1.ObjectMeta{Name: moduleName},
				Spec: ootov1beta1.ModuleSpec{
					DriverContainer: v1.Container{
						VolumeMounts: []v1.VolumeMount{dcVolMount},
					},
					DevicePlugin: &v1.Container{
						Image:        devicePluginImage,
						VolumeMounts: []v1.VolumeMount{dpVolMount},
					},
					Selector: map[string]string{"has-feature-x": "true"},
				},
			}

			ds := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dsName,
					Namespace: dsNamespace,
				},
			}

			err := dg.SetAsDesired(&ds, driverContainerImage, mod, kernelVersion)
			Expect(err).NotTo(HaveOccurred())

			podLabels := map[string]string{
				controllers.ModuleNameLabel: moduleName,
				kernelLabel:                 kernelVersion,
			}

			directory := v1.HostPathDirectory
			varTrue := true

			expected := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dsName,
					Namespace: dsNamespace,
					Labels:    podLabels,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: mod.APIVersion,
							Kind:       mod.Kind,
							Name:       moduleName,
							UID:        mod.UID,
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
								{
									Name:            "device-plugin",
									Image:           devicePluginImage,
									SecurityContext: &v1.SecurityContext{Privileged: &varTrue},
									VolumeMounts: []v1.VolumeMount{
										dpVolMount,
										{
											Name:      "kubelet-device-plugins",
											MountPath: "/var/lib/kubelet/device-plugins",
										},
									},
								},
							},
							NodeSelector: map[string]string{
								"has-feature-x": "true",
								kernelLabel:     kernelVersion,
							},
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

	Describe("ModuleDaemonSetsByKernelVersion", func() {
		It("should return an empty map if no DaemonSets are present", func() {
			dc := controllers.NewDaemonSetCreator(
				fake.NewClientBuilder().WithScheme(scheme).Build(),
				kernelLabel,
				dsNamespace,
				scheme)

			mod := ootov1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{Name: moduleName},
			}

			m, err := dc.ModuleDaemonSetsByKernelVersion(context.TODO(), mod)
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
					Namespace: dsNamespace,
					Labels:    dsLabels,
				},
			}

			ds2 := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ds2",
					Namespace: dsNamespace,
					Labels:    dsLabels,
				},
			}

			dc := controllers.NewDaemonSetCreator(
				fake.NewClientBuilder().WithScheme(scheme).WithObjects(&ds1, &ds2).Build(),
				kernelLabel,
				dsNamespace,
				scheme)

			mod := ootov1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{Name: moduleName},
			}

			_, err := dc.ModuleDaemonSetsByKernelVersion(context.TODO(), mod)
			Expect(err).To(HaveOccurred())
		})

		It("should return a map if two DaemonSets are present for different kernels", func() {
			const otherKernelVersion = "4.5.6"

			ds1 := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ds1",
					Namespace: dsNamespace,
					Labels: map[string]string{
						"oot.node.kubernetes.io/module.name": moduleName,
						kernelLabel:                          kernelVersion,
					},
				},
			}

			ds2 := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ds2",
					Namespace: dsNamespace,
					Labels: map[string]string{
						"oot.node.kubernetes.io/module.name": moduleName,
						kernelLabel:                          otherKernelVersion,
					},
				},
			}

			dc := controllers.NewDaemonSetCreator(
				fake.NewClientBuilder().WithScheme(scheme).WithObjects(&ds1, &ds2).Build(),
				kernelLabel,
				dsNamespace,
				scheme)

			mod := ootov1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{Name: moduleName},
			}

			m, err := dc.ModuleDaemonSetsByKernelVersion(context.TODO(), mod)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(HaveLen(2))
			Expect(m).To(HaveKeyWithValue(kernelVersion, &ds1))
			Expect(m).To(HaveKeyWithValue(otherKernelVersion, &ds2))
		})
	})
})
