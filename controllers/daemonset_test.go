package controllers_test

import (
	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	"github.com/qbarrand/oot-operator/controllers"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("daemonSetGenerator", func() {
	const kernelLabel = "kernel-label"

	Describe("SetAsDesired", func() {
		dg := controllers.NewDaemonSetCreator(kernelLabel, scheme)

		It("should return an error if the DaemonSet is nil", func() {
			Expect(
				dg.SetAsDesired(nil, "", nil, ""),
			).To(
				HaveOccurred(),
			)
		})

		It("should return an error if the image is empty", func() {
			Expect(
				dg.SetAsDesired(&appsv1.DaemonSet{}, "", nil, ""),
			).To(
				HaveOccurred(),
			)
		})

		It("should return an error if the module is nil", func() {
			Expect(
				dg.SetAsDesired(&appsv1.DaemonSet{}, "test", nil, ""),
			).To(
				HaveOccurred(),
			)
		})

		It("should return an error if the kernel version is empty", func() {
			Expect(
				dg.SetAsDesired(&appsv1.DaemonSet{}, "", &ootov1beta1.Module{}, ""),
			).To(
				HaveOccurred(),
			)
		})

		It("should work as expected", func() {
			const (
				dsName        = "ds-name"
				dsNamespace   = "ds-namespace"
				image         = "test-image"
				kernelVersion = "1.2.3"
				moduleName    = "module-name"
			)

			mod := ootov1beta1.Module{
				TypeMeta: metav1.TypeMeta{
					APIVersion: ootov1beta1.GroupVersion.String(),
					Kind:       "Module",
				},
				ObjectMeta: metav1.ObjectMeta{Name: moduleName},
				Spec: ootov1beta1.ModuleSpec{
					Selector: map[string]string{"has-feature-x": "true"},
				},
				Status: ootov1beta1.ModuleStatus{},
			}

			ds := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dsName,
					Namespace: dsNamespace,
				},
			}

			err := dg.SetAsDesired(&ds, image, &mod, kernelVersion)
			Expect(err).NotTo(HaveOccurred())

			podLabels := map[string]string{
				"oot.node.kubernetes.io/module.name":         moduleName,
				"oot.node.kubernetes.io/kernel-version.full": kernelVersion,
			}

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
									Image: image,
								},
							},
							NodeSelector: map[string]string{
								"has-feature-x": "true",
								kernelLabel:     kernelVersion,
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
})
