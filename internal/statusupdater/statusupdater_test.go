package statusupdater

import (
	"context"
	"fmt"

	"github.com/golang/mock/gomock"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/daemonset"
	"github.com/kubernetes-sigs/kernel-module-management/internal/metrics"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
)

type daemonSetConfig struct {
	desiredNumber   int
	numberAvailable int
	isDevicePlugin  bool
}

var _ = Describe("module status update", func() {
	const (
		name      = "sr-name"
		namespace = "sr-namespace"
	)

	var (
		ctrl        *gomock.Controller
		clnt        *client.MockClient
		mockMetrics *metrics.MockMetrics
		mod         *kmmv1beta1.Module
		su          ModuleStatusUpdater
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockMetrics = metrics.NewMockMetrics(ctrl)
		mod = &kmmv1beta1.Module{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
		su = NewModuleStatusUpdater(clnt, mockMetrics)
	})

	DescribeTable("checking status updater based on module",
		func(mappingsNodes []v1.Node, targetedNodes []v1.Node, dsMap map[string]*appsv1.DaemonSet, devicePluginPresent bool) {
			if devicePluginPresent {
				mod.Spec.DevicePlugin = &kmmv1beta1.DevicePluginSpec{}
			}
			var moduleLoaderAvailable int32
			var devicePluginAvailable int32

			for kernelVersion, ds := range dsMap {
				if daemonset.IsDevicePluginKernelVersion(kernelVersion) {
					devicePluginAvailable = ds.Status.NumberAvailable
					mockMetrics.EXPECT().SetCompletedStage(name,
						namespace,
						daemonset.GetDevicePluginKernelVersion(),
						metrics.DevicePluginStage,
						ds.Status.NumberAvailable == ds.Status.DesiredNumberScheduled)
				} else {
					moduleLoaderAvailable += ds.Status.NumberAvailable
					mockMetrics.EXPECT().SetCompletedStage(name,
						namespace,
						kernelVersion,
						metrics.ModuleLoaderStage,
						ds.Status.NumberAvailable == ds.Status.DesiredNumberScheduled)
				}
			}
			statusWrite := client.NewMockStatusWriter(ctrl)
			clnt.EXPECT().Status().Return(statusWrite)
			statusWrite.EXPECT().Update(context.Background(), mod).Return(nil)

			res := su.ModuleUpdateStatus(context.Background(), mod, mappingsNodes, targetedNodes, dsMap)

			Expect(res).To(BeNil())
			Expect(mod.Status.ModuleLoader.NodesMatchingSelectorNumber).To(Equal(int32(len(targetedNodes))))
			Expect(mod.Status.ModuleLoader.DesiredNumber).To(Equal(int32(len(mappingsNodes))))
			Expect(mod.Status.ModuleLoader.AvailableNumber).To(Equal(moduleLoaderAvailable))
			Expect(mod.Status.DevicePlugin.AvailableNumber).To(Equal(devicePluginAvailable))
		},
		Entry("0 nodes, 0 module-loaders, 0 device plugins",
			[]v1.Node{},
			[]v1.Node{},
			prepareDsByKernel([]daemonSetConfig{}),
			false,
		),
		Entry("2 nodes, 2 module-loaders, 0 device plugins",
			[]v1.Node{v1.Node{}, v1.Node{}},
			[]v1.Node{v1.Node{}, v1.Node{}},
			prepareDsByKernel([]daemonSetConfig{
				daemonSetConfig{desiredNumber: 2, numberAvailable: 2, isDevicePlugin: false},
				daemonSetConfig{desiredNumber: 3, numberAvailable: 2, isDevicePlugin: false},
			}),
			false,
		),
		Entry("2 nodes, 1 module-loaders, 1 device plugins",
			[]v1.Node{v1.Node{}, v1.Node{}},
			[]v1.Node{v1.Node{}, v1.Node{}},
			prepareDsByKernel([]daemonSetConfig{
				daemonSetConfig{desiredNumber: 4, numberAvailable: 2, isDevicePlugin: true},
			}),
			true,
		),
		Entry("2 nodes, 3 targeted nodes, 1 module-loaders, 1 device plugins",
			[]v1.Node{v1.Node{}, v1.Node{}},
			[]v1.Node{v1.Node{}, v1.Node{}, v1.Node{}},
			prepareDsByKernel([]daemonSetConfig{
				daemonSetConfig{desiredNumber: 4, numberAvailable: 4, isDevicePlugin: true},
				daemonSetConfig{desiredNumber: 4, numberAvailable: 2, isDevicePlugin: false},
			}),
			true,
		),
	)
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
		existingModules := sets.NewString("moduleName1", "moduleName2")
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

func getDaemonSet(kernelNumber int, dsConfig daemonSetConfig) (string, *appsv1.DaemonSet) {
	kernelVersion := fmt.Sprintf("kernel-version-%d", kernelNumber)
	if dsConfig.isDevicePlugin {
		kernelVersion = daemonset.GetDevicePluginKernelVersion()
	}
	ds := appsv1.DaemonSet{
		Status: appsv1.DaemonSetStatus{
			NumberAvailable:        int32(dsConfig.numberAvailable),
			DesiredNumberScheduled: int32(dsConfig.numberAvailable),
		},
	}
	return kernelVersion, &ds
}

func prepareDsByKernel(dsConfigs []daemonSetConfig) map[string]*appsv1.DaemonSet {
	dsMap := make(map[string]*appsv1.DaemonSet)
	for i, dsConfig := range dsConfigs {
		kernelVersion, ds := getDaemonSet(i, dsConfig)
		dsMap[kernelVersion] = ds
	}
	return dsMap
}
