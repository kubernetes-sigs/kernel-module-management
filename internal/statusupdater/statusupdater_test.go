package statusupdater

import (
	"context"
	"fmt"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kmmv1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	"github.com/qbarrand/oot-operator/internal/client"
	"github.com/qbarrand/oot-operator/internal/daemonset"
	"github.com/qbarrand/oot-operator/internal/metrics"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		mockDC      *daemonset.MockDaemonSetCreator
		mockMetrics *metrics.MockMetrics
		mod         *kmmv1beta1.Module
		su          ModuleStatusUpdater
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockDC = daemonset.NewMockDaemonSetCreator(ctrl)
		mockMetrics = metrics.NewMockMetrics(ctrl)
		mod = &kmmv1beta1.Module{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
		su = NewModuleStatusUpdater(clnt, mockDC, mockMetrics)
	})

	DescribeTable("checking status updater based on module",
		func(mappingsNodes []v1.Node, targetedNodes []v1.Node, dsMap map[string]*appsv1.DaemonSet, devicePluginPresent bool) {
			if devicePluginPresent {
				mod.Spec.DevicePlugin = &kmmv1beta1.DevicePluginSpec{}
			}
			var driverContainerAvailable int32
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
					driverContainerAvailable += ds.Status.NumberAvailable
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
			Expect(mod.Status.ModuleLoader.AvailableNumber).To(Equal(driverContainerAvailable))
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
		name      = "preflight-name"
		namespace = "preflight-namespace"
	)

	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		pv   *kmmv1beta1.PreflightValidation
		ps   *kmmv1beta1.CRStatus
		su   PreflightStatusUpdater
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		pv = &kmmv1beta1.PreflightValidation{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
		ps = &kmmv1beta1.CRStatus{Name: "cr-name"}
		su = NewPreflightStatusUpdater(clnt)
	})

	It("preset preflight status", func() {
		statusWrite := client.NewMockStatusWriter(ctrl)
		clnt.EXPECT().Status().Return(statusWrite)
		statusWrite.EXPECT().Update(context.Background(), pv).Return(nil)

		res := su.PreflightPresetVerificationStatus(context.Background(), pv, ps)
		Expect(res).To(BeNil())
		Expect(ps.VerificationStatus).To(Equal(kmmv1beta1.VerificationFalse))
		Expect(ps.VerificationStage).To(Equal(kmmv1beta1.VerificationStageImage))
	})

	It("set preflight verification status", func() {
		statusWrite := client.NewMockStatusWriter(ctrl)
		clnt.EXPECT().Status().Return(statusWrite)
		statusWrite.EXPECT().Patch(context.Background(), pv, gomock.Any()).Return(nil)

		res := su.PreflightSetVerificationStatus(context.Background(), pv, ps, "verificationStatus", "verificationReason")
		Expect(res).To(BeNil())
		Expect(ps.VerificationStatus).To(Equal("verificationStatus"))
		Expect(ps.StatusReason).To(Equal("verificationReason"))
	})

	It("set preflight verification stage", func() {
		statusWrite := client.NewMockStatusWriter(ctrl)
		clnt.EXPECT().Status().Return(statusWrite)
		statusWrite.EXPECT().Patch(context.Background(), pv, gomock.Any()).Return(nil)

		res := su.PreflightSetVerificationStage(context.Background(), pv, ps, "verificationStage")
		Expect(res).To(BeNil())
		Expect(ps.VerificationStage).To(Equal("verificationStage"))
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
