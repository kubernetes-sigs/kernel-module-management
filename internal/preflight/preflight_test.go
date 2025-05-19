package preflight

import (
	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SetModuleStatus", func() {
	var (
		preflightAPI PreflightAPI
		pv           *v1beta2.PreflightValidation
	)

	BeforeEach(func() {
		preflightAPI = NewPreflightAPI()
		pv = &v1beta2.PreflightValidation{
			Status: v1beta2.PreflightValidationStatus{
				Modules: []v1beta2.PreflightValidationModuleStatus{
					{
						Name:      "test-name",
						Namespace: "test-namespace",
						CRBaseStatus: v1beta2.CRBaseStatus{
							VerificationStatus: "original status",
							StatusReason:       "original reason",
						},
					},
				},
			},
		}
	})

	It("should set the existing module status correctly", func() {
		preflightAPI.SetModuleStatus(pv, "test-namespace", "test-name", "some status", "some reason")
		Expect(pv.Status.Modules).To(HaveLen(1))
		Expect(pv.Status.Modules[0].VerificationStatus).To(Equal("some status"))
		Expect(pv.Status.Modules[0].StatusReason).To(Equal("some reason"))
	})

	It("should set the new module status correctly", func() {
		preflightAPI.SetModuleStatus(pv, "test-namespace", "new-name", "new status", "new reason")
		Expect(pv.Status.Modules).To(HaveLen(2))
		Expect(pv.Status.Modules[1].Name).To(Equal("new-name"))
		Expect(pv.Status.Modules[1].VerificationStatus).To(Equal("new status"))
		Expect(pv.Status.Modules[1].StatusReason).To(Equal("new reason"))
	})
})

var _ = Describe("GetModuleStatus", func() {
	var (
		preflightAPI PreflightAPI
		pv           *v1beta2.PreflightValidation
	)

	BeforeEach(func() {
		preflightAPI = NewPreflightAPI()
		pv = &v1beta2.PreflightValidation{
			Status: v1beta2.PreflightValidationStatus{
				Modules: []v1beta2.PreflightValidationModuleStatus{
					{
						Name:      "test-name",
						Namespace: "test-namespace",
						CRBaseStatus: v1beta2.CRBaseStatus{
							VerificationStatus: "original status",
							StatusReason:       "original reason",
						},
					},
				},
			},
		}
	})

	It("should return the status of existing module", func() {
		res := preflightAPI.GetModuleStatus(pv, "test-namespace", "test-name")
		Expect(res).To(Equal("original status"))
	})

	It("should return empty string for non-existing module", func() {
		res := preflightAPI.GetModuleStatus(pv, "test-namespace", "non-existing-name")
		Expect(res).To(Equal(""))
	})
})

var _ = Describe("AllModulesVerified", func() {
	var (
		preflightAPI PreflightAPI
		pv           *v1beta2.PreflightValidation
	)

	BeforeEach(func() {
		preflightAPI = NewPreflightAPI()
		pv = &v1beta2.PreflightValidation{
			Status: v1beta2.PreflightValidationStatus{
				Modules: []v1beta2.PreflightValidationModuleStatus{
					{
						Name:      "test-name1",
						Namespace: "test-namespace1",
					},
					{
						Name:      "test-name2",
						Namespace: "test-namespace2",
					},
				},
			},
		}
	})

	It("should return true if all modules are verified", func() {
		pv.Status.Modules[0].VerificationStatus = v1beta2.VerificationSuccess
		pv.Status.Modules[1].VerificationStatus = v1beta2.VerificationFailure
		Expect(preflightAPI.AllModulesVerified(pv)).To(BeTrue())

	})

	It("should return false if any module is not verified", func() {
		pv.Status.Modules[0].VerificationStatus = v1beta2.VerificationInProgress
		pv.Status.Modules[1].VerificationStatus = v1beta2.VerificationFailure
		Expect(preflightAPI.AllModulesVerified(pv)).To(BeFalse())
	})
})
