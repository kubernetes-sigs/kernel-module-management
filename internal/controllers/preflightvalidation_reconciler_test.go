package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta2"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/metrics"
	"github.com/kubernetes-sigs/kernel-module-management/internal/preflight"
	"github.com/kubernetes-sigs/kernel-module-management/internal/preflight/test"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		mockMetrics   *metrics.MockMetrics
		mockSU        *preflight.MockStatusUpdater
		mockPreflight *preflight.MockPreflightAPI
		ctx           context.Context
		nsn           types.NamespacedName
		pr            *PreflightValidationReconciler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockMetrics = metrics.NewMockMetrics(ctrl)
		mockSU = preflight.NewMockStatusUpdater(ctrl)
		mockPreflight = preflight.NewMockPreflightAPI(ctrl)
		nsn = types.NamespacedName{Name: preflightName}
		ctx = context.Background()
		pr = NewPreflightValidationReconciler(clnt, nil, mockMetrics, mockSU, mockPreflight)
	})

	It("good flow, all verified", func() {
		mod := v1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
		}
		pv := v1beta2.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{Name: nsn.Name},
			Spec:       v1beta2.PreflightValidationSpec{KernelVersion: "some kernel version"},
			Status: v1beta2.PreflightValidationStatus{
				Modules: []v1beta2.PreflightValidationModuleStatus{
					{
						Name:      mod.Name,
						Namespace: mod.Namespace,
					},
				},
			},
		}

		modNSN := types.NamespacedName{Name: moduleName, Namespace: namespace}

		gomock.InOrder(
			mockMetrics.EXPECT().SetKMMPreflightsNum(1),
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *v1beta1.ModuleList, _ ...interface{}) error {
					list.Items = []v1beta1.Module{mod}
					return nil
				},
			),
			mockSU.EXPECT().PresetStatuses(ctx, &pv, sets.New(modNSN), []types.NamespacedName{}).Return(nil),
			mockPreflight.EXPECT().PreflightUpgradeCheck(ctx, &pv, &mod).Return(true, "some message"),
			mockSU.EXPECT().SetVerificationStatus(ctx, &pv, modNSN, v1beta1.VerificationTrue, "some message").Return(nil),
			mockSU.EXPECT().SetVerificationStage(ctx, &pv, modNSN, v1beta1.VerificationStageDone).Return(nil),
			clnt.EXPECT().Get(ctx, nsn, &v1beta2.PreflightValidation{}).DoAndReturn(
				func(_ interface{}, _ interface{}, m *v1beta2.PreflightValidation, _ ...ctrlclient.GetOption) error {
					m.Status.Modules = []v1beta2.PreflightValidationModuleStatus{
						{
							Name:         mod.Name,
							Namespace:    mod.Namespace,
							CRBaseStatus: v1beta2.CRBaseStatus{VerificationStatus: "True"},
						},
					}

					return nil
				},
			),
		)

		res, err := pr.Reconcile(ctx, &pv)

		Expect(err).To(BeNil())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("good flow, some not verified", func() {
		mod := v1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: moduleName, Namespace: namespace},
		}

		pv := v1beta2.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{Name: nsn.Name},
			Spec:       v1beta2.PreflightValidationSpec{KernelVersion: "some kernel version"},
			Status: v1beta2.PreflightValidationStatus{
				Modules: []v1beta2.PreflightValidationModuleStatus{
					{
						Name:      mod.Name,
						Namespace: mod.Namespace,
					},
				},
			},
		}

		modNSN := types.NamespacedName{Name: moduleName, Namespace: namespace}

		gomock.InOrder(
			mockMetrics.EXPECT().SetKMMPreflightsNum(1),
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *v1beta1.ModuleList, _ ...interface{}) error {
					list.Items = []v1beta1.Module{mod}
					return nil
				},
			),
			mockSU.EXPECT().PresetStatuses(ctx, &pv, sets.New(modNSN), []types.NamespacedName{}).Return(nil),
			mockPreflight.EXPECT().PreflightUpgradeCheck(ctx, &pv, &mod).Return(true, "some message"),
			mockSU.EXPECT().SetVerificationStatus(ctx, &pv, modNSN, v1beta1.VerificationTrue, "some message").Return(nil),
			mockSU.EXPECT().SetVerificationStage(ctx, &pv, modNSN, v1beta1.VerificationStageDone).Return(nil),
			clnt.EXPECT().Get(context.Background(), nsn, &v1beta2.PreflightValidation{}).DoAndReturn(
				func(_ interface{}, _ interface{}, m *v1beta2.PreflightValidation, _ ...ctrlclient.GetOption) error {
					m.Status.Modules = []v1beta2.PreflightValidationModuleStatus{
						{
							Name:         mod.Name,
							Namespace:    mod.Namespace,
							CRBaseStatus: v1beta2.CRBaseStatus{VerificationStatus: "False"},
						},
					}

					return nil
				},
			),
		)

		res, err := pr.Reconcile(ctx, &pv)

		Expect(err).To(BeNil())
		Expect(res).To(Equal(reconcile.Result{RequeueAfter: time.Second * reconcileRequeueInSeconds}))
	})

})

var _ = Describe("PreflightValidationReconciler_getModulesCheck", func() {
	const (
		moduleName1 = "moduleName1"
		moduleName2 = "moduleName2"
	)

	var (
		ctrl          *gomock.Controller
		clnt          *client.MockClient
		mockSU        *preflight.MockStatusUpdater
		mockPreflight *preflight.MockPreflightAPI
		ctx           context.Context
		pr            *PreflightValidationReconciler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockSU = preflight.NewMockStatusUpdater(ctrl)
		mockPreflight = preflight.NewMockPreflightAPI(ctrl)
		ctx = context.Background()
		pr = NewPreflightValidationReconciler(clnt, nil, nil, mockSU, mockPreflight)
	})

	It("multiple modules, statuses exist, none deleted", func() {
		pv := v1beta2.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      preflightName,
				Namespace: namespace,
			},
			Spec: v1beta2.PreflightValidationSpec{
				KernelVersion: "some kernel version",
			},
			Status: v1beta2.PreflightValidationStatus{
				Modules: []v1beta2.PreflightValidationModuleStatus{
					{Name: moduleName1, Namespace: namespace},
					{Name: moduleName2, Namespace: namespace},
				},
			},
		}
		mod1 := v1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: moduleName1, Namespace: namespace},
		}

		mod2 := v1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: moduleName2, Namespace: namespace},
		}

		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *v1beta1.ModuleList, _ ...interface{}) error {
				list.Items = []v1beta1.Module{mod1, mod2}
				return nil
			},
		)

		existingModules := sets.New(
			types.NamespacedName{Name: moduleName1, Namespace: namespace},
			types.NamespacedName{Name: moduleName2, Namespace: namespace},
		)

		mockSU.EXPECT().PresetStatuses(ctx, &pv, existingModules, []types.NamespacedName{})

		modulesToCheck, err := pr.getModulesToCheck(ctx, &pv)

		Expect(err).To(BeNil())
		Expect(modulesToCheck).To(Equal([]v1beta1.Module{mod1, mod2}))

	})

	It("multiple modules, one status missing, none deleted", func() {
		pv := v1beta2.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      preflightName,
				Namespace: namespace,
			},
			Spec: v1beta2.PreflightValidationSpec{
				KernelVersion: "some kernel version",
			},
			Status: v1beta2.PreflightValidationStatus{
				Modules: []v1beta2.PreflightValidationModuleStatus{
					{Name: moduleName1, Namespace: namespace},
				},
			},
		}
		mod1 := v1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: moduleName1, Namespace: namespace},
		}

		mod2 := v1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: moduleName2, Namespace: namespace},
		}

		existingModules := sets.New(
			types.NamespacedName{Name: moduleName1, Namespace: namespace},
			types.NamespacedName{Name: moduleName2, Namespace: namespace},
		)

		newModules := []types.NamespacedName{
			{Name: moduleName2, Namespace: namespace},
		}

		gomock.InOrder(
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *v1beta1.ModuleList, _ ...interface{}) error {
					list.Items = []v1beta1.Module{mod1, mod2}
					return nil
				},
			),

			mockSU.EXPECT().PresetStatuses(ctx, &pv, existingModules, newModules).DoAndReturn(
				func(_ interface{}, pv *v1beta2.PreflightValidation, _ sets.Set[types.NamespacedName], newModules []types.NamespacedName) error {
					status := v1beta2.PreflightValidationModuleStatus{
						Name:      newModules[0].Name,
						Namespace: newModules[0].Namespace,
					}

					Expect(
						test.UpsertModuleStatus(&pv.Status.Modules, status),
					).NotTo(
						HaveOccurred(),
					)

					return nil
				}),
		)

		modulesToCheck, err := pr.getModulesToCheck(ctx, &pv)

		Expect(err).To(BeNil())
		Expect(modulesToCheck).To(Equal([]v1beta1.Module{mod1, mod2}))
	})

	It("multiple modules, one status missing, one deleted", func() {
		pv := v1beta2.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      preflightName,
				Namespace: namespace,
			},
			Spec: v1beta2.PreflightValidationSpec{
				KernelVersion: "some kernel version",
			},
			Status: v1beta2.PreflightValidationStatus{
				Modules: []v1beta2.PreflightValidationModuleStatus{
					{Name: moduleName1, Namespace: namespace},
				},
			},
		}
		mod1 := v1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: moduleName1, Namespace: namespace},
		}

		mod2 := v1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: moduleName2, Namespace: namespace},
		}

		mod3 := v1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName3", Namespace: namespace},
		}
		timestamp := metav1.Now()
		mod3.SetDeletionTimestamp(&timestamp)

		existingModules := sets.New(
			types.NamespacedName{Name: moduleName1, Namespace: namespace},
			types.NamespacedName{Name: moduleName2, Namespace: namespace},
		)

		newModules := []types.NamespacedName{
			{Name: moduleName2, Namespace: namespace},
		}

		gomock.InOrder(
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *v1beta1.ModuleList, _ ...interface{}) error {
					list.Items = []v1beta1.Module{mod1, mod2, mod3}
					return nil
				},
			),
			mockSU.EXPECT().PresetStatuses(ctx, &pv, existingModules, newModules).DoAndReturn(
				func(_ interface{}, pv *v1beta2.PreflightValidation, _ sets.Set[types.NamespacedName], newModules []types.NamespacedName) error {
					s := v1beta2.PreflightValidationModuleStatus{
						Name:      newModules[0].Name,
						Namespace: newModules[0].Namespace,
					}

					Expect(
						test.UpsertModuleStatus(&pv.Status.Modules, s),
					).NotTo(
						HaveOccurred(),
					)

					test.DeleteModuleStatus(&pv.Status.Modules, types.NamespacedName{Name: "moduleName3", Namespace: namespace})
					return nil
				}),
		)
		modulesToCheck, err := pr.getModulesToCheck(ctx, &pv)

		Expect(err).To(BeNil())
		Expect(modulesToCheck).To(Equal([]v1beta1.Module{mod1, mod2}))
	})
})

var _ = Describe("PreflightValidationReconciler_updatePreflightStatus", func() {
	var (
		ctrl          *gomock.Controller
		clnt          *client.MockClient
		mockSU        *preflight.MockStatusUpdater
		mockPreflight *preflight.MockPreflightAPI
		ctx           context.Context
		pr            *PreflightValidationReconciler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockSU = preflight.NewMockStatusUpdater(ctrl)
		mockPreflight = preflight.NewMockPreflightAPI(ctrl)
		ctx = context.Background()
		pr = NewPreflightValidationReconciler(clnt, nil, nil, mockSU, mockPreflight)
	})

	nsn := types.NamespacedName{Name: moduleName, Namespace: namespace}

	It("status verified", func() {
		pv := v1beta2.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{Name: preflightName},
		}

		gomock.InOrder(
			mockSU.EXPECT().SetVerificationStatus(ctx, &pv, nsn, v1beta1.VerificationTrue, "some message").Return(nil),
			mockSU.EXPECT().SetVerificationStage(ctx, &pv, nsn, v1beta1.VerificationStageDone).Return(nil),
		)
		pr.updatePreflightStatus(context.Background(), &pv, nsn, "some message", true)
	})

	It("status not verified", func() {
		pv := v1beta2.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{Name: preflightName},
		}

		gomock.InOrder(
			mockSU.EXPECT().SetVerificationStatus(ctx, &pv, nsn, v1beta1.VerificationFalse, "some message").Return(nil),
			mockSU.EXPECT().SetVerificationStage(ctx, &pv, nsn, v1beta1.VerificationStageRequeued).Return(nil),
		)
		pr.updatePreflightStatus(context.Background(), &pv, nsn, "some message", false)
	})
})

var _ = Describe("PreflightValidationReconciler_checkPreflightCompletion", func() {
	var (
		ctrl          *gomock.Controller
		clnt          *client.MockClient
		mockSU        *preflight.MockStatusUpdater
		mockPreflight *preflight.MockPreflightAPI
		pr            *PreflightValidationReconciler
		nsn           types.NamespacedName
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockSU = preflight.NewMockStatusUpdater(ctrl)
		mockPreflight = preflight.NewMockPreflightAPI(ctrl)
		nsn = types.NamespacedName{Name: preflightName}
		pr = NewPreflightValidationReconciler(clnt, nil, nil, mockSU, mockPreflight)
	})

	It("Get preflight failed", func() {
		clnt.EXPECT().Get(context.Background(), nsn, &v1beta2.PreflightValidation{}).Return(fmt.Errorf("some error"))

		res, err := pr.checkPreflightCompletion(context.Background(), nsn.Name)
		Expect(err).To(HaveOccurred())
		Expect(res).To(BeFalse())
	})

	It("All modules have been verified", func() {
		clnt.EXPECT().Get(context.Background(), nsn, &v1beta2.PreflightValidation{}).DoAndReturn(
			func(_ interface{}, _ interface{}, m *v1beta2.PreflightValidation, _ ...ctrlclient.GetOption) error {
				m.Status.Modules = []v1beta2.PreflightValidationModuleStatus{
					{
						Name:         "module1",
						Namespace:    namespace,
						CRBaseStatus: v1beta2.CRBaseStatus{VerificationStatus: "True"},
					},
					{
						Name:         "module2",
						Namespace:    namespace,
						CRBaseStatus: v1beta2.CRBaseStatus{VerificationStatus: "True"},
					},
				}

				return nil
			},
		)
		res, err := pr.checkPreflightCompletion(context.Background(), nsn.Name)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(BeTrue())

	})

	It("Some modules have not been verified", func() {
		clnt.EXPECT().Get(context.Background(), nsn, &v1beta2.PreflightValidation{}).DoAndReturn(
			func(_ interface{}, _ interface{}, m *v1beta2.PreflightValidation, _ ...ctrlclient.GetOption) error {
				m.Status.Modules = []v1beta2.PreflightValidationModuleStatus{
					{
						Name:         "module1",
						Namespace:    namespace,
						CRBaseStatus: v1beta2.CRBaseStatus{VerificationStatus: "True"},
					},
					{
						Name:         "module2",
						Namespace:    namespace,
						CRBaseStatus: v1beta2.CRBaseStatus{VerificationStatus: "False"},
					},
				}

				return nil
			},
		)
		res, err := pr.checkPreflightCompletion(context.Background(), nsn.Name)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(BeFalse())

	})
})
