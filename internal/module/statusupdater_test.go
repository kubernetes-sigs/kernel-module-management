package module

import (
	"context"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/internal/client"
	"github.com/qbarrand/oot-operator/internal/daemonset"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("status update", func() {
	const (
		name      = "sr-name"
		namespace = "sr-namespace"
	)

	var (
		ctrl   *gomock.Controller
		clnt   *client.MockClient
		mockDC *daemonset.MockDaemonSetCreator
		mod    *ootov1alpha1.Module
		su     StatusUpdater
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockDC = daemonset.NewMockDaemonSetCreator(ctrl)
		mod = &ootov1alpha1.Module{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}
		su = NewStatusUpdater(clnt, mockDC)
	})

	DescribeTable("checking status updater based on module",
		func(mappingsNodes []v1.Node, targetedNodes []v1.Node, ds []appsv1.DaemonSet, moduleAvailable, pluginAvailable int, devicePluginPresent bool) {
			if devicePluginPresent {
				mod.Spec.DevicePlugin = &v1.Container{}
			}
			mockDC.EXPECT().ModuleDaemonSets(context.Background(), name, namespace).Return(ds, nil)
			mockDC.EXPECT().IsDevicePluginDaemonSet(gomock.Any()).
				DoAndReturn(func(ds appsv1.DaemonSet) bool {
					if ds.Name == "device-plugin" {
						return true
					} else {
						return false
					}
				}).Times(len(ds))

			statusWrite := client.NewMockStatusWriter(ctrl)
			clnt.EXPECT().Status().Return(statusWrite)
			statusWrite.EXPECT().Patch(context.Background(), mod, gomock.Any()).Return(nil)

			res := su.UpdateModuleStatus(context.Background(), mod, mappingsNodes, targetedNodes)

			Expect(res).To(BeNil())
			Expect(mod.Status.DriverContainer.NodesMatchingSelectorNumber).To(Equal(int32(len(targetedNodes))))
			Expect(mod.Status.DriverContainer.DesiredNumber).To(Equal(int32(len(mappingsNodes))))
			Expect(mod.Status.DriverContainer.AvailableNumber).To(Equal(int32(moduleAvailable)))
			Expect(mod.Status.DevicePlugin.AvailableNumber).To(Equal(int32(pluginAvailable)))
		},
		Entry("0 nodes, 0 driver-containers, 0 device plugins",
			[]v1.Node{}, []v1.Node{}, []appsv1.DaemonSet{}, 0, 0, false,
		),
		Entry("2 nodes, 2 driver-containers, 0 device plugins",
			[]v1.Node{v1.Node{}, v1.Node{}}, []v1.Node{v1.Node{}, v1.Node{}}, []appsv1.DaemonSet{getDaemonSet(2, false), getDaemonSet(3, false)}, 5, 0, false,
		),
		Entry("2 nodes, 1 driver-containers, 1 device plugins",
			[]v1.Node{v1.Node{}, v1.Node{}}, []v1.Node{v1.Node{}, v1.Node{}}, []appsv1.DaemonSet{getDaemonSet(2, false), getDaemonSet(3, true)}, 2, 3, true,
		),
		Entry("2 nodes, 3 targeted nodes, 1 driver-containers, 1 device plugins",
			[]v1.Node{v1.Node{}, v1.Node{}}, []v1.Node{v1.Node{}, v1.Node{}, v1.Node{}}, []appsv1.DaemonSet{getDaemonSet(2, false), getDaemonSet(3, true)}, 2, 3, true,
		),
	)
})

func getDaemonSet(numberAvailable int, devicePluginFlag bool) appsv1.DaemonSet {
	name := "driver-container"
	if devicePluginFlag {
		name = "device-plugin"
	}
	ds := appsv1.DaemonSet{
		Status: appsv1.DaemonSetStatus{NumberAvailable: int32(numberAvailable)},
	}
	ds.Name = name
	return ds
}
