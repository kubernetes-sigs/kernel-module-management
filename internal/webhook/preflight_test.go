package webhook

import (
	"context"

	kmmv1beta2 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("validatePreflight", func() {
	It("should fail with invalid kernel version", func() {
		pv := &kmmv1beta2.PreflightValidation{
			Spec: kmmv1beta2.PreflightValidationSpec{
				KernelVersion: "test",
			},
		}
		_, err := validatePreflight(pv)
		Expect(err).To(HaveOccurred())
	})

	It("should pass with valid kernel version", func() {
		pv := &kmmv1beta2.PreflightValidation{
			Spec: kmmv1beta2.PreflightValidationSpec{
				KernelVersion: "6.0.15-300.fc37.x86_64",
			},
		}
		_, err := validatePreflight(pv)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("PreflightValidationValidator", func() {
	v := NewPreflightValidationValidator(GinkgoLogr)
	ctx := context.TODO()

	It("ValidateDelete should return not implemented", func() {
		_, err := v.ValidateDelete(ctx, nil)
		Expect(err).To(Equal(NotImplemented))
	})
})
