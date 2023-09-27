package statusupdater

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	workv1 "open-cluster-management.io/api/work/v1"

	hubv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
)

var _ = Describe("module status update", func() {
	const (
		name      = "sr-name"
		namespace = "sr-namespace"
	)

	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		mod  *kmmv1beta1.Module
		su   ModuleStatusUpdater
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mod = &kmmv1beta1.Module{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
		su = NewModuleStatusUpdater(clnt)
	})

	DescribeTable("checking status updater based on module",
		func(mappingsNodes []v1.Node, targetedNodes []v1.Node, devicePluginDS *appsv1.DaemonSet) {
			existingDS := []appsv1.DaemonSet{}
			var devicePluginAvailable int32
			if devicePluginDS != nil {
				mod.Spec.DevicePlugin = &kmmv1beta1.DevicePluginSpec{}
				devicePluginAvailable = devicePluginDS.Status.NumberAvailable
				existingDS = append(existingDS, *devicePluginDS)
			}

			statusWrite := client.NewMockStatusWriter(ctrl)
			clnt.EXPECT().Status().Return(statusWrite)
			statusWrite.EXPECT().Patch(context.Background(), mod, gomock.Any()).Return(nil)

			res := su.ModuleUpdateStatus(context.Background(), mod, mappingsNodes, targetedNodes, existingDS)

			Expect(res).To(BeNil())
			Expect(mod.Status.ModuleLoader.NodesMatchingSelectorNumber).To(Equal(int32(len(targetedNodes))))
			Expect(mod.Status.ModuleLoader.DesiredNumber).To(Equal(int32(len(mappingsNodes))))
			Expect(mod.Status.ModuleLoader.AvailableNumber).To(Equal(int32(0)))
			Expect(mod.Status.DevicePlugin.AvailableNumber).To(Equal(devicePluginAvailable))
		},
		Entry("0 nodes, 0 device plugins",
			[]v1.Node{},
			[]v1.Node{},
			nil,
		),
		Entry("2 nodes, 0 device plugins",
			[]v1.Node{v1.Node{}, v1.Node{}},
			[]v1.Node{v1.Node{}, v1.Node{}},
			nil,
		),
		Entry("2 nodes, 1 device plugins",
			[]v1.Node{v1.Node{}, v1.Node{}},
			[]v1.Node{v1.Node{}, v1.Node{}},
			prepareDaemonSet(4, 2),
		),
		Entry("2 nodes, 3 targeted nodes, 1 device plugins",
			[]v1.Node{v1.Node{}, v1.Node{}},
			[]v1.Node{v1.Node{}, v1.Node{}, v1.Node{}},
			prepareDaemonSet(4, 4),
		),
	)
})

var _ = Describe("ManagedClusterModule status update", func() {
	const (
		name = "mcm-name"
	)

	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		mcm  *hubv1beta1.ManagedClusterModule
		su   ManagedClusterModuleStatusUpdater
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mcm = &hubv1beta1.ManagedClusterModule{ObjectMeta: metav1.ObjectMeta{Name: name}}
		su = NewManagedClusterModuleStatusUpdater(clnt)
	})

	It("", func() {
		statusWrite := client.NewMockStatusWriter(ctrl)
		clnt.EXPECT().Status().Return(statusWrite)
		statusWrite.EXPECT().Patch(context.Background(), mcm, gomock.Any()).Return(nil)

		mw := workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test",
				Namespace: "a-namespace",
				Labels: map[string]string{
					constants.ManagedClusterModuleNameLabel: mcm.Name,
				},
			},
			Status: workv1.ManifestWorkStatus{
				Conditions: []metav1.Condition{
					{
						Type:   workv1.WorkApplied,
						Status: metav1.ConditionTrue,
					},
					{
						Type:   workv1.WorkDegraded,
						Status: metav1.ConditionFalse,
					},
				},
			},
		}
		degradedMW := workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-degraded",
				Namespace: "another-namespace",
				Labels: map[string]string{
					constants.ManagedClusterModuleNameLabel: mcm.Name,
				},
			},
			Status: workv1.ManifestWorkStatus{
				Conditions: []metav1.Condition{
					{
						Type:   workv1.WorkApplied,
						Status: metav1.ConditionFalse,
					},
					{
						Type:   workv1.WorkDegraded,
						Status: metav1.ConditionTrue,
					},
				},
			},
		}
		manifestWorkList := workv1.ManifestWorkList{
			Items: []workv1.ManifestWork{mw, degradedMW},
		}

		res := su.ManagedClusterModuleUpdateStatus(context.Background(), mcm, manifestWorkList.Items)

		Expect(res).To(BeNil())
		Expect(mcm.Status.NumberDesired).To(BeEquivalentTo(len(manifestWorkList.Items)))
		Expect(mcm.Status.NumberApplied).To(BeEquivalentTo(1))
		Expect(mcm.Status.NumberDegraded).To(BeEquivalentTo(1))
	})
})

var _ = Describe("preflight status updates", func() {
	const (
		name       = "preflight-name"
		namespace  = "preflight-namespace"
		moduleName = "moduleName"
	)

	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		pv   *kmmv1beta1.PreflightValidation
		su   PreflightStatusUpdater
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		pv = &kmmv1beta1.PreflightValidation{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
		pv.Status.CRStatuses = make(map[string]*kmmv1beta1.CRStatus, 1)
		su = NewPreflightStatusUpdater(clnt)
	})

	It("preset preflight statuses", func() {
		pv.Status.CRStatuses["moduleName1"] = &kmmv1beta1.CRStatus{VerificationStage: kmmv1beta1.VerificationStageBuild}
		pv.Status.CRStatuses["moduleName2"] = &kmmv1beta1.CRStatus{VerificationStage: kmmv1beta1.VerificationStageBuild}
		pv.Status.CRStatuses["moduleName3"] = &kmmv1beta1.CRStatus{VerificationStage: kmmv1beta1.VerificationStageImage}
		existingModules := sets.New[string]("moduleName1", "moduleName2")
		newModules := []string{"moduleName4"}

		statusWrite := client.NewMockStatusWriter(ctrl)
		clnt.EXPECT().Status().Return(statusWrite)
		statusWrite.EXPECT().Update(context.Background(), pv).Return(nil)

		res := su.PreflightPresetStatuses(context.Background(), pv, existingModules, newModules)
		Expect(res).To(BeNil())
		Expect(pv.Status.CRStatuses["moduleName1"].VerificationStage).To(Equal(kmmv1beta1.VerificationStageBuild))
		Expect(pv.Status.CRStatuses["moduleName2"].VerificationStage).To(Equal(kmmv1beta1.VerificationStageBuild))
		Expect(pv.Status.CRStatuses["moduleName4"].VerificationStage).To(Equal(kmmv1beta1.VerificationStageImage))
		Expect(pv.Status.CRStatuses["moduleName4"].VerificationStatus).To(Equal(kmmv1beta1.VerificationFalse))
		_, ok := pv.Status.CRStatuses["moduleName3"]
		Expect(ok).To(BeFalse())
	})

	It("set preflight verification status", func() {
		pv.Status.CRStatuses[moduleName] = &kmmv1beta1.CRStatus{}
		statusWrite := client.NewMockStatusWriter(ctrl)
		clnt.EXPECT().Status().Return(statusWrite)
		statusWrite.EXPECT().Update(context.Background(), pv).Return(nil)

		res := su.PreflightSetVerificationStatus(context.Background(), pv, moduleName, "verificationStatus", "verificationReason")
		Expect(res).To(BeNil())
		Expect(pv.Status.CRStatuses[moduleName].VerificationStatus).To(Equal("verificationStatus"))
		Expect(pv.Status.CRStatuses[moduleName].StatusReason).To(Equal("verificationReason"))
	})

	It("set preflight verification stage", func() {
		pv.Status.CRStatuses[moduleName] = &kmmv1beta1.CRStatus{}
		statusWrite := client.NewMockStatusWriter(ctrl)
		clnt.EXPECT().Status().Return(statusWrite)
		statusWrite.EXPECT().Update(context.Background(), pv).Return(nil)

		res := su.PreflightSetVerificationStage(context.Background(), pv, moduleName, "verificationStage")
		Expect(res).To(BeNil())
		Expect(pv.Status.CRStatuses[moduleName].VerificationStage).To(Equal("verificationStage"))
	})
})

func prepareDaemonSet(desiredNumber, numberAvailable int) *appsv1.DaemonSet {
	ds := appsv1.DaemonSet{
		Status: appsv1.DaemonSetStatus{
			NumberAvailable:        int32(numberAvailable),
			DesiredNumberScheduled: int32(numberAvailable),
		},
	}
	ds.SetLabels(map[string]string{constants.ModuleNameLabel: "moduleName"})
	return &ds
}
