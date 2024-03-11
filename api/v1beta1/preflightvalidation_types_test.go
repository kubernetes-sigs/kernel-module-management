package v1beta1

import (
	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("PreflightValidation_ConvertFrom", func() {
	It("should work as expected", func() {
		baseStatus1 := v1beta2.CRBaseStatus{
			VerificationStatus: "True",
			StatusReason:       "some-reason",
			VerificationStage:  "some-stage",
			LastTransitionTime: metav1.Now(),
		}

		baseStatus2 := v1beta2.CRBaseStatus{
			VerificationStatus: "False",
			StatusReason:       "some-other-reason",
			VerificationStage:  "some-other-stage",
			LastTransitionTime: metav1.Now(),
		}

		meta := metav1.ObjectMeta{Name: "pfv-name"}
		spec := v1beta2.PreflightValidationSpec{
			KernelVersion:  "1.2.3",
			PushBuiltImage: true,
		}

		pfvV1beta2 := v1beta2.PreflightValidation{
			ObjectMeta: meta,
			Spec:       spec,
			Status: v1beta2.PreflightValidationStatus{
				Modules: []v1beta2.PreflightValidationModuleStatus{
					{
						Name:         "module-1",
						Namespace:    "namespace-1",
						CRBaseStatus: baseStatus1,
					},
					{
						Name:         "module-2",
						Namespace:    "namespace-2",
						CRBaseStatus: baseStatus2,
					},
				},
			},
		}

		expected := PreflightValidation{
			ObjectMeta: meta,
			Spec:       spec,
			Status: PreflightValidationStatus{
				CRStatuses: map[string]*CRStatus{
					"namespace-1/module-1": &baseStatus1,
					"namespace-2/module-2": &baseStatus2,
				},
			},
		}

		pfvV1beta1 := &PreflightValidation{}

		Expect(
			pfvV1beta1.ConvertFrom(&pfvV1beta2),
		).NotTo(
			HaveOccurred(),
		)

		Expect(*pfvV1beta1).To(BeComparableTo(expected))
	})
})

var _ = Describe("PreflightValidation_ConvertTo", func() {
	It("should work as expected", func() {
		baseStatus1 := v1beta2.CRBaseStatus{
			VerificationStatus: "True",
			StatusReason:       "some-reason",
			VerificationStage:  "some-stage",
			LastTransitionTime: metav1.Now(),
		}

		baseStatus2 := v1beta2.CRBaseStatus{
			VerificationStatus: "False",
			StatusReason:       "some-other-reason",
			VerificationStage:  "some-other-stage",
			LastTransitionTime: metav1.Now(),
		}

		meta := metav1.ObjectMeta{Name: "name"}
		spec := v1beta2.PreflightValidationSpec{
			KernelVersion:  "1.2.3",
			PushBuiltImage: true,
		}

		pfvV1beta1 := PreflightValidation{
			ObjectMeta: meta,
			Spec:       spec,
			Status: PreflightValidationStatus{
				CRStatuses: map[string]*CRStatus{
					"namespace-1/module-1": &baseStatus1,
					"namespace-2/module-2": &baseStatus2,
				},
			},
		}

		pfvV1beta2 := v1beta2.PreflightValidation{}

		Expect(
			pfvV1beta1.ConvertTo(&pfvV1beta2),
		).NotTo(
			HaveOccurred(),
		)

		Expect(pfvV1beta2.ObjectMeta).To(Equal(meta))
		Expect(pfvV1beta2.Spec).To(Equal(spec))

		modStatus1 := v1beta2.PreflightValidationModuleStatus{
			Name:         "module-1",
			Namespace:    "namespace-1",
			CRBaseStatus: baseStatus1,
		}

		modStatus2 := v1beta2.PreflightValidationModuleStatus{
			Name:         "module-2",
			Namespace:    "namespace-2",
			CRBaseStatus: baseStatus2,
		}

		Expect(pfvV1beta2.Status.Modules).To(ConsistOf(modStatus1, modStatus2))
	})
})
