package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/golang/mock/gomock"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/preflight"
	"github.com/kubernetes-sigs/kernel-module-management/internal/statusupdater"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	preflightName = "test-preflight"
)

var _ = Describe("PreflightValidationReconciler_Reconcile", func() {
	var (
		ctrl          *gomock.Controller
		clnt          *client.MockClient
		mockSU        *statusupdater.MockPreflightStatusUpdater
		mockPreflight *preflight.MockPreflightAPI
		req           reconcile.Request
		ctx           context.Context
		nsn           types.NamespacedName
		pr            *PreflightValidationReconciler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockSU = statusupdater.NewMockPreflightStatusUpdater(ctrl)
		mockPreflight = preflight.NewMockPreflightAPI(ctrl)
		nsn = types.NamespacedName{
			Name:      preflightName,
			Namespace: namespace,
		}
		req = reconcile.Request{NamespacedName: nsn}
		ctx = context.Background()
		pr = NewPreflightValidationReconciler(clnt, nil, mockSU, mockPreflight)
	})

	It("should do nothing if the Preflight is not available anymore", func() {
		clnt.EXPECT().Get(ctx, nsn, &kmmv1beta1.PreflightValidation{}).Return(apierrors.NewNotFound(schema.GroupResource{}, preflightName))

		res, err := pr.Reconcile(ctx, req)

		Expect(err).To(BeNil())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should return error when failed to get preflight", func() {
		clnt.EXPECT().Get(ctx, nsn, &kmmv1beta1.PreflightValidation{}).Return(fmt.Errorf("some error"))

		res, err := pr.Reconcile(ctx, req)

		Expect(err).To(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("good flow, all verified", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name: "moduleName",
			},
		}
		pv := kmmv1beta1.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nsn.Name,
				Namespace: nsn.Namespace,
			},
			Spec: kmmv1beta1.PreflightValidationSpec{
				KernelVersion: "some kernel version",
			},
			Status: kmmv1beta1.PreflightValidationStatus{
				CRStatuses: map[string]*kmmv1beta1.CRStatus{mod.Name: &kmmv1beta1.CRStatus{}},
			},
		}
		gomock.InOrder(
			clnt.EXPECT().Get(context.Background(), nsn, &kmmv1beta1.PreflightValidation{}).DoAndReturn(
				func(_ interface{}, _ interface{}, m *kmmv1beta1.PreflightValidation, _ ...ctrlclient.GetOption) error {
					m.ObjectMeta = pv.ObjectMeta
					m.Spec.KernelVersion = pv.Spec.KernelVersion
					m.Status = pv.Status
					return nil
				},
			),
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.Module{mod}
					return nil
				},
			),
			mockSU.EXPECT().PreflightPresetStatuses(ctx, &pv, sets.NewString(mod.Name), []string{}).Return(nil),
			mockPreflight.EXPECT().PreflightUpgradeCheck(ctx, &pv, &mod).Return(true, "some message"),
			mockSU.EXPECT().PreflightSetVerificationStatus(ctx, &pv, mod.Name, kmmv1beta1.VerificationTrue, "some message").Return(nil),
			mockSU.EXPECT().PreflightSetVerificationStage(ctx, &pv, mod.Name, kmmv1beta1.VerificationStageDone).Return(nil),
			clnt.EXPECT().Get(context.Background(), nsn, &kmmv1beta1.PreflightValidation{}).DoAndReturn(
				func(_ interface{}, _ interface{}, m *kmmv1beta1.PreflightValidation, _ ...ctrlclient.GetOption) error {
					m.Status.CRStatuses = map[string]*kmmv1beta1.CRStatus{mod.Name: &kmmv1beta1.CRStatus{VerificationStatus: "True"}}
					return nil
				},
			),
		)

		res, err := pr.Reconcile(ctx, req)

		Expect(err).To(BeNil())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("good flow, some not verified", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name: "moduleName",
			},
		}
		pv := kmmv1beta1.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      nsn.Name,
				Namespace: nsn.Namespace,
			},
			Spec: kmmv1beta1.PreflightValidationSpec{
				KernelVersion: "some kernel version",
			},
			Status: kmmv1beta1.PreflightValidationStatus{
				CRStatuses: map[string]*kmmv1beta1.CRStatus{mod.Name: &kmmv1beta1.CRStatus{}},
			},
		}
		gomock.InOrder(
			clnt.EXPECT().Get(context.Background(), nsn, &kmmv1beta1.PreflightValidation{}).DoAndReturn(
				func(_ interface{}, _ interface{}, m *kmmv1beta1.PreflightValidation, _ ...ctrlclient.GetOption) error {
					m.ObjectMeta = pv.ObjectMeta
					m.Spec.KernelVersion = pv.Spec.KernelVersion
					m.Status = pv.Status
					return nil
				},
			),
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.Module{mod}
					return nil
				},
			),
			mockSU.EXPECT().PreflightPresetStatuses(ctx, &pv, sets.NewString(mod.Name), []string{}).Return(nil),
			mockPreflight.EXPECT().PreflightUpgradeCheck(ctx, &pv, &mod).Return(true, "some message"),
			mockSU.EXPECT().PreflightSetVerificationStatus(ctx, &pv, mod.Name, kmmv1beta1.VerificationTrue, "some message").Return(nil),
			mockSU.EXPECT().PreflightSetVerificationStage(ctx, &pv, mod.Name, kmmv1beta1.VerificationStageDone).Return(nil),
			clnt.EXPECT().Get(context.Background(), nsn, &kmmv1beta1.PreflightValidation{}).DoAndReturn(
				func(_ interface{}, _ interface{}, m *kmmv1beta1.PreflightValidation, _ ...ctrlclient.GetOption) error {
					m.Status.CRStatuses = map[string]*kmmv1beta1.CRStatus{mod.Name: &kmmv1beta1.CRStatus{VerificationStatus: "False"}}
					return nil
				},
			),
		)

		res, err := pr.Reconcile(ctx, req)

		Expect(err).To(BeNil())
		Expect(res).To(Equal(reconcile.Result{RequeueAfter: time.Second * reconcileRequeueInSeconds}))
	})

})

var _ = Describe("PreflightValidationReconciler_getModulesCheck", func() {
	var (
		ctrl          *gomock.Controller
		clnt          *client.MockClient
		mockSU        *statusupdater.MockPreflightStatusUpdater
		mockPreflight *preflight.MockPreflightAPI
		ctx           context.Context
		pr            *PreflightValidationReconciler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockSU = statusupdater.NewMockPreflightStatusUpdater(ctrl)
		mockPreflight = preflight.NewMockPreflightAPI(ctrl)
		ctx = context.Background()
		pr = NewPreflightValidationReconciler(clnt, nil, mockSU, mockPreflight)
	})

	It("multiple modules, statuses exist, none deleted", func() {
		pv := kmmv1beta1.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      preflightName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.PreflightValidationSpec{
				KernelVersion: "some kernel version",
			},
			Status: kmmv1beta1.PreflightValidationStatus{
				CRStatuses: map[string]*kmmv1beta1.CRStatus{
					"moduleName1": &kmmv1beta1.CRStatus{},
					"moduleName2": &kmmv1beta1.CRStatus{}},
			},
		}
		mod1 := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name: "moduleName1",
			},
		}

		mod2 := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name: "moduleName2",
			},
		}

		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
				list.Items = []kmmv1beta1.Module{mod1, mod2}
				return nil
			},
		)
		mockSU.EXPECT().PreflightPresetStatuses(ctx, &pv, sets.NewString("moduleName1", "moduleName2"), []string{})

		modulesToCheck, err := pr.getModulesToCheck(ctx, &pv)

		Expect(err).To(BeNil())
		Expect(modulesToCheck).To(Equal([]kmmv1beta1.Module{mod1, mod2}))

	})

	It("multiple modules, one status missing, none deleted", func() {
		pv := kmmv1beta1.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      preflightName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.PreflightValidationSpec{
				KernelVersion: "some kernel version",
			},
			Status: kmmv1beta1.PreflightValidationStatus{
				CRStatuses: map[string]*kmmv1beta1.CRStatus{"moduleName1": &kmmv1beta1.CRStatus{}},
			},
		}
		mod1 := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name: "moduleName1",
			},
		}

		mod2 := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name: "moduleName2",
			},
		}
		gomock.InOrder(
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.Module{mod1, mod2}
					return nil
				},
			),
			mockSU.EXPECT().PreflightPresetStatuses(ctx, &pv, sets.NewString("moduleName1", "moduleName2"), []string{"moduleName2"}).DoAndReturn(
				func(_ interface{}, pv *kmmv1beta1.PreflightValidation, existingModules sets.String, newModules []string) error {
					pv.Status.CRStatuses[newModules[0]] = &kmmv1beta1.CRStatus{}
					return nil
				}),
		)

		modulesToCheck, err := pr.getModulesToCheck(ctx, &pv)

		Expect(err).To(BeNil())
		Expect(modulesToCheck).To(Equal([]kmmv1beta1.Module{mod1, mod2}))
	})

	It("multiple modules, one status missing, one deleted", func() {
		pv := kmmv1beta1.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      preflightName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.PreflightValidationSpec{
				KernelVersion: "some kernel version",
			},
			Status: kmmv1beta1.PreflightValidationStatus{
				CRStatuses: map[string]*kmmv1beta1.CRStatus{"moduleName1": &kmmv1beta1.CRStatus{}},
			},
		}
		mod1 := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name: "moduleName1",
			},
		}

		mod2 := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name: "moduleName2",
			},
		}

		mod3 := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name: "moduleName3",
			},
		}
		timestamp := metav1.Now()
		mod3.SetDeletionTimestamp(&timestamp)

		gomock.InOrder(
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.Module{mod1, mod2, mod3}
					return nil
				},
			),
			mockSU.EXPECT().PreflightPresetStatuses(ctx, &pv, sets.NewString("moduleName1", "moduleName2"), []string{"moduleName2"}).DoAndReturn(
				func(_ interface{}, pv *kmmv1beta1.PreflightValidation, existingModules sets.String, newModules []string) error {
					pv.Status.CRStatuses[newModules[0]] = &kmmv1beta1.CRStatus{}
					delete(pv.Status.CRStatuses, "moduleName3")
					return nil
				}),
		)
		modulesToCheck, err := pr.getModulesToCheck(ctx, &pv)

		Expect(err).To(BeNil())
		Expect(modulesToCheck).To(Equal([]kmmv1beta1.Module{mod1, mod2}))
	})
})

var _ = Describe("PreflightValidationReconciler_updatePreflightStatus", func() {
	var (
		ctrl          *gomock.Controller
		clnt          *client.MockClient
		mockSU        *statusupdater.MockPreflightStatusUpdater
		mockPreflight *preflight.MockPreflightAPI
		ctx           context.Context
		pr            *PreflightValidationReconciler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockSU = statusupdater.NewMockPreflightStatusUpdater(ctrl)
		mockPreflight = preflight.NewMockPreflightAPI(ctrl)
		ctx = context.Background()
		pr = NewPreflightValidationReconciler(clnt, nil, mockSU, mockPreflight)
	})

	It("status verified", func() {
		pv := kmmv1beta1.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      preflightName,
				Namespace: namespace,
			},
		}

		gomock.InOrder(
			mockSU.EXPECT().PreflightSetVerificationStatus(ctx, &pv, "moduleName", kmmv1beta1.VerificationTrue, "some message").Return(nil),
			mockSU.EXPECT().PreflightSetVerificationStage(ctx, &pv, "moduleName", kmmv1beta1.VerificationStageDone).Return(nil),
		)
		pr.updatePreflightStatus(context.Background(), &pv, "moduleName", "some message", true)
	})

	It("status not verified", func() {
		pv := kmmv1beta1.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      preflightName,
				Namespace: namespace,
			},
		}

		gomock.InOrder(
			mockSU.EXPECT().PreflightSetVerificationStatus(ctx, &pv, "moduleName", kmmv1beta1.VerificationFalse, "some message").Return(nil),
			mockSU.EXPECT().PreflightSetVerificationStage(ctx, &pv, "moduleName", kmmv1beta1.VerificationStageRequeued).Return(nil),
		)
		pr.updatePreflightStatus(context.Background(), &pv, "moduleName", "some message", false)
	})
})

var _ = Describe("PreflightValidationReconciler_checkPreflightCompletion", func() {
	var (
		ctrl          *gomock.Controller
		clnt          *client.MockClient
		mockSU        *statusupdater.MockPreflightStatusUpdater
		mockPreflight *preflight.MockPreflightAPI
		pr            *PreflightValidationReconciler
		nsn           types.NamespacedName
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockSU = statusupdater.NewMockPreflightStatusUpdater(ctrl)
		mockPreflight = preflight.NewMockPreflightAPI(ctrl)
		nsn = types.NamespacedName{
			Name:      preflightName,
			Namespace: namespace,
		}
		pr = NewPreflightValidationReconciler(clnt, nil, mockSU, mockPreflight)
	})

	It("Get preflight failed", func() {
		clnt.EXPECT().Get(context.Background(), nsn, &kmmv1beta1.PreflightValidation{}).Return(fmt.Errorf("some error"))

		res, err := pr.checkPreflightCompletion(context.Background(), nsn.Name, nsn.Namespace)
		Expect(err).To(HaveOccurred())
		Expect(res).To(BeFalse())
	})

	It("All modules have been verified", func() {
		crStatuses := map[string]*kmmv1beta1.CRStatus{
			"module1": &kmmv1beta1.CRStatus{VerificationStatus: "True"},
			"module2": &kmmv1beta1.CRStatus{VerificationStatus: "True"},
		}

		clnt.EXPECT().Get(context.Background(), nsn, &kmmv1beta1.PreflightValidation{}).DoAndReturn(
			func(_ interface{}, _ interface{}, m *kmmv1beta1.PreflightValidation, _ ...ctrlclient.GetOption) error {
				m.Status.CRStatuses = crStatuses
				return nil
			},
		)
		res, err := pr.checkPreflightCompletion(context.Background(), nsn.Name, nsn.Namespace)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(BeTrue())

	})

	It("Some modules have not been verified", func() {
		crStatuses := map[string]*kmmv1beta1.CRStatus{
			"module1": &kmmv1beta1.CRStatus{VerificationStatus: "True"},
			"module2": &kmmv1beta1.CRStatus{VerificationStatus: "False"},
		}

		clnt.EXPECT().Get(context.Background(), nsn, &kmmv1beta1.PreflightValidation{}).DoAndReturn(
			func(_ interface{}, _ interface{}, m *kmmv1beta1.PreflightValidation, _ ...ctrlclient.GetOption) error {
				m.Status.CRStatuses = crStatuses
				return nil
			},
		)
		res, err := pr.checkPreflightCompletion(context.Background(), nsn.Name, nsn.Namespace)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(BeFalse())

	})
})
