package preflight

import (
	"context"

	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

func getCRStatus(statuses []v1beta2.PreflightValidationModuleStatus, nsn types.NamespacedName) *v1beta2.PreflightValidationModuleStatus {
	GinkgoHelper()

	s, ok := FindModuleStatus(statuses, nsn)

	Expect(ok).To(BeTrue())

	return s
}

var _ = Describe("preflight status updates", func() {
	const (
		name       = "preflight-name"
		namespace  = "module-namespace"
		moduleName = "moduleName"
	)

	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		nsn  = types.NamespacedName{Name: moduleName, Namespace: namespace}
		pv   *v1beta2.PreflightValidation
		su   StatusUpdater
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		pv = &v1beta2.PreflightValidation{ObjectMeta: metav1.ObjectMeta{Name: name}}
		su = NewStatusUpdater(clnt)
	})

	It("preset preflight statuses", func() {
		const (
			moduleName1 = "moduleName1"
			moduleName2 = "moduleName2"
			moduleName3 = "moduleName3"
			moduleName4 = "moduleName4"
		)

		pv.Status.Modules = []v1beta2.PreflightValidationModuleStatus{
			{
				Name:         moduleName1,
				Namespace:    namespace,
				CRBaseStatus: v1beta2.CRBaseStatus{VerificationStage: kmmv1beta1.VerificationStageBuild},
			},
			{
				Name:         moduleName2,
				Namespace:    namespace,
				CRBaseStatus: v1beta2.CRBaseStatus{VerificationStage: kmmv1beta1.VerificationStageBuild},
			},
			{
				Name:         moduleName3,
				Namespace:    namespace,
				CRBaseStatus: v1beta2.CRBaseStatus{VerificationStage: kmmv1beta1.VerificationStageImage},
			},
		}

		existingModules := sets.New(
			types.NamespacedName{Name: moduleName1, Namespace: namespace},
			types.NamespacedName{Name: moduleName2, Namespace: namespace},
		)

		newModules := []types.NamespacedName{
			{Name: moduleName4, Namespace: namespace},
		}

		statusWrite := client.NewMockStatusWriter(ctrl)
		clnt.EXPECT().Status().Return(statusWrite)
		statusWrite.EXPECT().Update(context.Background(), pv).Return(nil)

		res := su.PresetStatuses(context.Background(), pv, existingModules, newModules)
		Expect(res).To(BeNil())

		Expect(getCRStatus(pv.Status.Modules, types.NamespacedName{Name: moduleName1, Namespace: namespace}).VerificationStage).To(Equal(kmmv1beta1.VerificationStageBuild))
		Expect(getCRStatus(pv.Status.Modules, types.NamespacedName{Name: moduleName2, Namespace: namespace}).VerificationStage).To(Equal(kmmv1beta1.VerificationStageBuild))
		Expect(getCRStatus(pv.Status.Modules, types.NamespacedName{Name: moduleName4, Namespace: namespace}).VerificationStage).To(Equal(kmmv1beta1.VerificationStageImage))
		Expect(getCRStatus(pv.Status.Modules, types.NamespacedName{Name: moduleName4, Namespace: namespace}).VerificationStatus).To(Equal(kmmv1beta1.VerificationFalse))

		_, ok := FindModuleStatus(pv.Status.Modules, types.NamespacedName{Name: moduleName3, Namespace: namespace})
		Expect(ok).To(BeFalse())
	})

	It("set preflight verification status", func() {
		pv.Status.Modules = []v1beta2.PreflightValidationModuleStatus{
			{
				Name:      moduleName,
				Namespace: namespace,
			},
		}
		statusWrite := client.NewMockStatusWriter(ctrl)
		clnt.EXPECT().Status().Return(statusWrite)
		statusWrite.EXPECT().Update(context.Background(), pv).Return(nil)

		res := su.SetVerificationStatus(context.Background(), pv, nsn, "verificationStatus", "verificationReason")
		Expect(res).To(BeNil())
		Expect(getCRStatus(pv.Status.Modules, nsn).VerificationStatus).To(Equal("verificationStatus"))
		Expect(getCRStatus(pv.Status.Modules, nsn).StatusReason).To(Equal("verificationReason"))
	})

	It("set preflight verification stage", func() {
		pv.Status.Modules = []v1beta2.PreflightValidationModuleStatus{
			{
				Name:      moduleName,
				Namespace: namespace,
			},
		}
		statusWrite := client.NewMockStatusWriter(ctrl)
		clnt.EXPECT().Status().Return(statusWrite)
		statusWrite.EXPECT().Update(context.Background(), pv).Return(nil)

		res := su.SetVerificationStage(context.Background(), pv, nsn, "verificationStage")
		Expect(res).To(BeNil())
		Expect(getCRStatus(pv.Status.Modules, nsn).VerificationStage).To(Equal("verificationStage"))
	})
})
