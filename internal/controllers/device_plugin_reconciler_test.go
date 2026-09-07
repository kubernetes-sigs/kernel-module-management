package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/kubernetes-sigs/kernel-module-management/internal/node"
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/metrics"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("DevicePluginReconciler_Reconcile", func() {
	var (
		ctrl            *gomock.Controller
		clnt            *client.MockClient
		mockReconHelper *MockdevicePluginReconcilerHelperAPI
		mod             *kmmv1beta1.Module
		dpr             *DevicePluginReconciler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockReconHelper = NewMockdevicePluginReconcilerHelperAPI(ctrl)

		// The finalizer is already there, so these specs exercise the handlers rather than the
		// pass that registers it. Registration has its own specs below.
		mod = &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       namespace,
				Name:            moduleName,
				ResourceVersion: "1",
				Finalizers:      []string{constants.DevicePluginFinalizer},
			},
			Spec: kmmv1beta1.ModuleSpec{
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{},
			},
		}

		dpr = &DevicePluginReconciler{
			client:         clnt,
			reconHelperAPI: mockReconHelper,
		}
	})

	ctx := context.Background()

	DescribeTable("check error flows", func(getDSError, handleTargetLabelsError, handlePluginError, gcError bool) {
		devicePluginDS := []appsv1.DaemonSet{appsv1.DaemonSet{}}
		returnedError := fmt.Errorf("some error")
		mockReconHelper.EXPECT().setKMMOMetrics(ctx)
		if getDSError {
			mockReconHelper.EXPECT().getModuleDevicePluginDaemonSets(ctx, mod.Name, mod.Namespace).Return(nil, returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().getModuleDevicePluginDaemonSets(ctx, mod.Name, mod.Namespace).Return(devicePluginDS, nil)
		if handleTargetLabelsError {
			mockReconHelper.EXPECT().handleDevicePluginTargetLabels(ctx, mod).Return(returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().handleDevicePluginTargetLabels(ctx, mod).Return(nil)
		if handlePluginError {
			mockReconHelper.EXPECT().handleDevicePlugin(ctx, mod, devicePluginDS).Return(returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().handleDevicePlugin(ctx, mod, devicePluginDS).Return(nil)
		if gcError {
			mockReconHelper.EXPECT().garbageCollect(ctx, mod, devicePluginDS).Return(returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().garbageCollect(ctx, mod, devicePluginDS).Return(nil)
		mockReconHelper.EXPECT().moduleUpdateDevicePluginStatus(ctx, mod, devicePluginDS).Return(returnedError)

	executeTestFunction:
		res, err := dpr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).To(HaveOccurred())

	},
		Entry("getDevicePluginDaemonSets failed", true, false, false, false),
		Entry("handleDevicePluginTargetLabels failed", false, true, false, false),
		Entry("handleDevicePlugin failed", false, false, true, false),
		Entry("garbageCollect failed", false, false, false, true),
		Entry("devicePluginUpdateStatus failed", false, false, false, false),
	)

	It("Good flow", func() {
		devicePluginDS := []appsv1.DaemonSet{appsv1.DaemonSet{}}
		gomock.InOrder(
			mockReconHelper.EXPECT().setKMMOMetrics(ctx),
			mockReconHelper.EXPECT().getModuleDevicePluginDaemonSets(ctx, mod.Name, mod.Namespace).Return(devicePluginDS, nil),
			mockReconHelper.EXPECT().handleDevicePluginTargetLabels(ctx, mod).Return(nil),
			mockReconHelper.EXPECT().handleDevicePlugin(ctx, mod, devicePluginDS).Return(nil),
			mockReconHelper.EXPECT().garbageCollect(ctx, mod, devicePluginDS).Return(nil),
			mockReconHelper.EXPECT().moduleUpdateDevicePluginStatus(ctx, mod, devicePluginDS).Return(nil),
		)

		res, err := dpr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("module deletion flow", func() {
		mod.SetDeletionTimestamp(&metav1.Time{})

		By("good flow")
		gomock.InOrder(
			mockReconHelper.EXPECT().deleteDevicePluginResources(ctx, mod).Return(true, nil),
			clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Return(nil),
		)

		res, err := dpr.Reconcile(ctx, mod)
		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())

		By("error flow - cleanup fails")
		mockReconHelper.EXPECT().deleteDevicePluginResources(ctx, mod).Return(false, fmt.Errorf("some error"))

		res, err = dpr.Reconcile(ctx, mod)
		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).To(HaveOccurred())
	})

	It("keeps the finalizer while the device plugin resources are still there", func() {
		mod.SetDeletionTimestamp(&metav1.Time{Time: time.Now()})

		// No Patch: nothing releases the Module until the cleanup says it is done.
		mockReconHelper.EXPECT().deleteDevicePluginResources(ctx, mod).Return(false, nil)

		res, err := dpr.Reconcile(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(pluginCleanupRequeue))
	})

	It("registers its finalizer before it writes anything the finalizer protects", func() {
		mod.Finalizers = nil
		devicePluginDS := []appsv1.DaemonSet{{}}

		gomock.InOrder(
			clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ context.Context, o ctrlclient.Object, p ctrlclient.Patch, _ ...ctrlclient.PatchOption) error {
					data, err := p.Data(o)
					Expect(err).NotTo(HaveOccurred())
					Expect(string(data)).To(ContainSubstring(constants.DevicePluginFinalizer))
					Expect(string(data)).To(ContainSubstring(`"resourceVersion":"1"`))
					return nil
				},
			),
			mockReconHelper.EXPECT().setKMMOMetrics(ctx),
			mockReconHelper.EXPECT().getModuleDevicePluginDaemonSets(ctx, mod.Name, mod.Namespace).Return(devicePluginDS, nil),
			mockReconHelper.EXPECT().handleDevicePluginTargetLabels(ctx, mod).Return(nil),
			mockReconHelper.EXPECT().handleDevicePlugin(ctx, mod, devicePluginDS).Return(nil),
			mockReconHelper.EXPECT().garbageCollect(ctx, mod, devicePluginDS).Return(nil),
			mockReconHelper.EXPECT().moduleUpdateDevicePluginStatus(ctx, mod, devicePluginDS).Return(nil),
		)

		_, err := dpr.Reconcile(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
	})

	It("clears a leftover status in its own pass before releasing the Module", func() {
		mod.SetDeletionTimestamp(&metav1.Time{Time: time.Now()})
		mod.Status.DevicePlugin = kmmv1beta1.DaemonSetStatus{NodesMatchingSelectorNumber: 1}

		// No Patch: a status write moves the resourceVersion, so the release waits for the
		// next pass, which decides again from the spec.
		gomock.InOrder(
			mockReconHelper.EXPECT().deleteDevicePluginResources(ctx, mod).Return(true, nil),
			mockReconHelper.EXPECT().clearDevicePluginStatus(ctx, gomock.Any()).Return(nil),
		)

		res, err := dpr.Reconcile(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.RequeueAfter).To(Equal(pluginCleanupRequeue))
	})

	It("does not touch the resources when its finalizer cannot be written", func() {
		mod.Finalizers = nil

		clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("conflict"))

		_, err := dpr.Reconcile(ctx, mod)
		Expect(err).To(HaveOccurred())
	})

	It("writes nothing when the Module goes before its finalizer lands", func() {
		mod.Finalizers = nil

		// No helper expectations: a Module that has gone cannot be cleaned up later, so
		// nothing may be written for it. Any call below is an unexpected call here.
		clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).
			Return(apierrors.NewNotFound(schema.GroupResource{}, mod.Name))

		_, err := dpr.Reconcile(ctx, mod)
		Expect(err).To(HaveOccurred())
	})

	It("keeps hold of the Module when the release cannot be written", func() {
		mod.SetDeletionTimestamp(&metav1.Time{Time: time.Now()})

		gomock.InOrder(
			mockReconHelper.EXPECT().deleteDevicePluginResources(ctx, mod).Return(true, nil),
			clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("conflict")),
		)

		_, err := dpr.Reconcile(ctx, mod)
		Expect(err).To(HaveOccurred())
	})

	It("no-op when spec.devicePlugin is nil and no existing DaemonSets", func() {
		mod.Spec.DevicePlugin = nil
		gomock.InOrder(
			mockReconHelper.EXPECT().setKMMOMetrics(ctx),
			mockReconHelper.EXPECT().deleteDevicePluginResources(ctx, mod).Return(true, nil),
			clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Return(nil),
		)

		res, err := dpr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("cleanup when spec.devicePlugin is nil but existing DaemonSets present", func() {
		mod.Spec.DevicePlugin = nil

		gomock.InOrder(
			mockReconHelper.EXPECT().setKMMOMetrics(ctx),
			mockReconHelper.EXPECT().deleteDevicePluginResources(ctx, mod).Return(true, nil),
			clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Return(nil),
		)

		res, err := dpr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("cleanup when spec.devicePlugin is nil and removeDevicePluginTargetLabels fails", func() {
		mod.Spec.DevicePlugin = nil

		gomock.InOrder(
			mockReconHelper.EXPECT().setKMMOMetrics(ctx),
			mockReconHelper.EXPECT().deleteDevicePluginResources(ctx, mod).Return(false, fmt.Errorf("some error")),
		)

		res, err := dpr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).To(HaveOccurred())
	})

	It("cleanup when spec.devicePlugin is nil and clearDevicePluginStatus fails", func() {
		mod.Spec.DevicePlugin = nil

		gomock.InOrder(
			mockReconHelper.EXPECT().setKMMOMetrics(ctx),
			mockReconHelper.EXPECT().deleteDevicePluginResources(ctx, mod).Return(true, nil),
			mockReconHelper.EXPECT().clearDevicePluginStatus(ctx, gomock.Any()).Return(fmt.Errorf("some error")),
		)
		mod.Status.DevicePlugin = kmmv1beta1.DaemonSetStatus{NodesMatchingSelectorNumber: 1}

		res, err := dpr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).To(HaveOccurred())
	})

})

var _ = Describe("DevicePluginReconciler_handleDevicePlugin", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		mockDSHelper *MockdaemonSetCreator
		dprh         devicePluginReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockDSHelper = NewMockdaemonSetCreator(ctrl)
		dprh = devicePluginReconcilerHelper{
			client:          clnt,
			daemonSetHelper: mockDSHelper,
		}
	})

	It("device plugin not defined", func() {
		ctx := context.Background()
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "namespace",
			},
		}

		err := dprh.handleDevicePlugin(ctx, &mod, nil)

		Expect(err).NotTo(HaveOccurred())
	})

	It("new daemonset", func() {
		ctx := context.Background()
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "namespace",
			},
			Spec: kmmv1beta1.ModuleSpec{
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{},
			},
		}

		newDS := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: mod.Namespace, GenerateName: mod.Name + "-device-plugin-"},
		}
		gomock.InOrder(
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(apierrors.NewNotFound(schema.GroupResource{}, "whatever")),
			mockDSHelper.EXPECT().setDevicePluginAsDesired(ctx, newDS, &mod).Return(nil),
			clnt.EXPECT().Create(ctx, gomock.Any()).Return(nil),
		)

		err := dprh.handleDevicePlugin(ctx, &mod, nil)

		Expect(err).NotTo(HaveOccurred())
	})

	It("existing daemonset", func() {
		ctx := context.Background()
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "namespace",
			},
			Spec: kmmv1beta1.ModuleSpec{
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{},
			},
		}

		const name = "some name"
		existingDS := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: mod.Namespace, Name: name},
		}
		gomock.InOrder(
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, ds *appsv1.DaemonSet, _ ...ctrlclient.GetOption) error {
					ds.SetName(name)
					ds.SetNamespace(mod.Namespace)
					return nil
				},
			),
			mockDSHelper.EXPECT().setDevicePluginAsDesired(ctx, &existingDS, &mod).Return(nil),
		)

		err := dprh.handleDevicePlugin(ctx, &mod, []appsv1.DaemonSet{existingDS})

		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("DevicePluginReconciler_garbageCollect", func() {
	const currentModuleVersion = "current label"

	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		dprh devicePluginReconcilerHelperAPI
		mn   node.Node
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mn = node.NewMockNode(ctrl)
		dprh = newDevicePluginReconcilerHelper(clnt, clnt, nil, mn, nil)
	})

	mod := &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "moduleName",
			Namespace: "namespace",
		},
		Spec: kmmv1beta1.ModuleSpec{
			ModuleLoader: &kmmv1beta1.ModuleLoaderSpec{
				Container: kmmv1beta1.ModuleLoaderContainerSpec{
					Version: currentModuleVersion,
				},
			},
		},
	}
	schedulePodVersionLabel := utils.GetSchedulePodVersionLabelName(mod.Namespace, mod.Name)

	DescribeTable("device-plugin GC", func(devicePluginFormerLabel bool, devicePluginFormerDesired int) {
		devicePluginDS := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "devicePlugin",
				Namespace: "namespace",
				Labels: map[string]string{
					schedulePodVersionLabel:   currentModuleVersion,
					constants.ModuleNameLabel: mod.Name,
				},
			},
		}
		devicePluginFormerVersionDS := &appsv1.DaemonSet{}

		existingDS := []appsv1.DaemonSet{devicePluginDS}
		if devicePluginFormerLabel {
			devicePluginFormerVersionDS = devicePluginDS.DeepCopy()
			devicePluginFormerVersionDS.SetName("devicePluginFormer")
			devicePluginFormerVersionDS.Labels[schedulePodVersionLabel] = "former label"
			devicePluginFormerVersionDS.Status.DesiredNumberScheduled = int32(devicePluginFormerDesired)
			existingDS = append(existingDS, *devicePluginFormerVersionDS)
		}
		if devicePluginFormerLabel && devicePluginFormerDesired == 0 {
			clnt.EXPECT().Delete(context.Background(), devicePluginFormerVersionDS).Return(nil)
		}

		err := dprh.garbageCollect(context.Background(), mod, existingDS)

		Expect(err).NotTo(HaveOccurred())
	},
		Entry("no deletes", false, 0),
		Entry("former device plugin", true, 0),
		Entry("former device plugin has desired", true, 1),
	)

	It("should return an error if a deletion failed", func() {
		deleteDS := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "devicePlugin",
				Namespace: "namespace",
				Labels:    map[string]string{constants.ModuleNameLabel: mod.Name, schedulePodVersionLabel: "formerVersion"},
			},
		}
		clnt.EXPECT().Delete(context.Background(), &deleteDS).Return(fmt.Errorf("some error"))

		existingDS := []appsv1.DaemonSet{deleteDS}

		err := dprh.garbageCollect(context.Background(), mod, existingDS)
		Expect(err).To(HaveOccurred())
	})

	It("should pass if moduleLoader is not defined", func() {
		modWithoutModuleLoader := mod
		modWithoutModuleLoader.Spec.ModuleLoader = nil
		deleteDS := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "devicePlugin",
				Namespace: "namespace",
				Labels:    map[string]string{constants.ModuleNameLabel: mod.Name, schedulePodVersionLabel: "formerVersion"},
			},
		}

		existingDS := []appsv1.DaemonSet{deleteDS}

		err := dprh.garbageCollect(context.Background(), modWithoutModuleLoader, existingDS)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("DevicePluginReconciler_deleteDevicePluginResources", func() {
	const (
		modName = "my-mod"
		modNS   = "my-ns"
	)

	var (
		ctrl   *gomock.Controller
		clnt   *client.MockClient
		reader *client.MockClient
		dprh   devicePluginReconcilerHelperAPI
		mod    *kmmv1beta1.Module
	)

	// A separate mock for the API reader, so a read that went to the cached client instead shows
	// up here as an unexpected call rather than passing.
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		reader = client.NewMockClient(ctrl)
		dprh = newDevicePluginReconcilerHelper(clnt, reader, nil, nil, nil)
		mod = &kmmv1beta1.Module{ObjectMeta: metav1.ObjectMeta{Namespace: modNS, Name: modName}}
	})

	ctx := context.Background()

	listing := func(dss []appsv1.DaemonSet, pods []v1.Pod, nodes []v1.Node) {
		reader.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, l ctrlclient.ObjectList, _ ...ctrlclient.ListOption) error {
				switch typed := l.(type) {
				case *appsv1.DaemonSetList:
					typed.Items = dss
				case *v1.PodList:
					typed.Items = pods
				case *v1.NodeList:
					typed.Items = nodes
				}
				return nil
			},
		).AnyTimes()
	}

	It("is not done while a DaemonSet it asked for is still finalizing", func() {
		now := metav1.Now()
		listing([]appsv1.DaemonSet{{ObjectMeta: metav1.ObjectMeta{
			Namespace: modNS, Name: "ds", DeletionTimestamp: &now,
			Finalizers: []string{"tests.kmm.sigs.x-k8s.io/hold"},
		}}}, nil, nil)

		// No Delete: it has been asked for, and asking again would wake this controller each pass.
		done, err := dprh.deleteDevicePluginResources(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(done).To(BeFalse())
	})

	It("is done once nothing is left", func() {
		listing(nil, nil, nil)

		done, err := dprh.deleteDevicePluginResources(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(done).To(BeTrue())
	})

	It("still cleans up a DaemonSet written before the role label existed", func() {
		legacy := appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{
			Namespace: modNS, Name: "legacy-dp", UID: "uid", ResourceVersion: "3",
		}}
		listing([]appsv1.DaemonSet{legacy}, nil, nil)

		clnt.EXPECT().Delete(ctx, gomock.Any(), gomock.Any()).Return(nil)

		done, err := dprh.deleteDevicePluginResources(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(done).To(BeFalse())
	})

	It("leaves the other plugins alone", func() {
		other := []appsv1.DaemonSet{
			{ObjectMeta: metav1.ObjectMeta{Namespace: modNS, Name: "loader",
				Labels: map[string]string{constants.DaemonSetRole: constants.ModuleLoaderRoleLabelValue}}},
			{ObjectMeta: metav1.ObjectMeta{Namespace: modNS, Name: "dra",
				Labels: map[string]string{constants.DaemonSetRole: constants.DRARoleLabelValue}}},
		}
		listing(other, nil, nil)

		// No Delete: neither belongs to the device plugin.
		done, err := dprh.deleteDevicePluginResources(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(done).To(BeTrue())
	})

	It("waits for a device plugin Pod that is still an object", func() {
		pod := v1.Pod{ObjectMeta: metav1.ObjectMeta{
			Namespace: modNS,
			Name:      "dp-pod",
			Labels: map[string]string{
				constants.ModuleNameLabel: modName,
				constants.DaemonSetRole:   constants.DevicePluginRoleLabelValue,
			},
			OwnerReferences: []metav1.OwnerReference{{Kind: "DaemonSet", Name: "ds", Controller: ptr.To(true)}},
		}}
		listing(nil, []v1.Pod{pod}, nil)

		done, err := dprh.deleteDevicePluginResources(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(done).To(BeFalse())
	})

	It("takes the target label off a node whose value was edited", func() {
		targetLabel := utils.GetDevicePluginTargetNodeLabel(modNS, modName)
		node := v1.Node{ObjectMeta: metav1.ObjectMeta{
			Name:   "node1",
			Labels: map[string]string{targetLabel: "not-empty", "keep": "me"},
		}}
		// The node is only returned for a key-existence selector, so a value match would find
		// nothing here and the label would survive the Module.
		reader.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, l ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
				switch typed := l.(type) {
				case *v1.NodeList:
					Expect(opts).To(HaveLen(1))
					Expect(opts[0]).To(Equal(ctrlclient.HasLabels{targetLabel}))
					typed.Items = []v1.Node{node}
				}
				return nil
			},
		).AnyTimes()

		clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, o ctrlclient.Object, p ctrlclient.Patch, _ ...ctrlclient.PatchOption) error {
				data, err := p.Data(o)
				Expect(err).NotTo(HaveOccurred())
				Expect(string(data)).To(ContainSubstring(`"` + targetLabel + `":null`))
				Expect(string(data)).NotTo(ContainSubstring("keep"))
				return nil
			},
		)

		done, err := dprh.deleteDevicePluginResources(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(done).To(BeTrue())
	})

	It("is not done when a read fails", func() {
		reader.EXPECT().List(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		done, err := dprh.deleteDevicePluginResources(ctx, mod)
		Expect(err).To(HaveOccurred())
		Expect(done).To(BeFalse())
	})
	It("does not wait for another namespace's Module of the same name", func() {
		// Same module.name label, different namespace: the list is namespaced, so nothing here
		// belongs to this Module.
		reader.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, l ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
				switch l.(type) {
				case *appsv1.DaemonSetList, *v1.PodList:
					Expect(opts).To(ContainElement(ctrlclient.InNamespace(modNS)))
				}
				return nil
			},
		).AnyTimes()

		done, err := dprh.deleteDevicePluginResources(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(done).To(BeTrue())
	})
})
var _ = Describe("DevicePluginReconciler_setKMMOMetrics", func() {
	var (
		ctrl        *gomock.Controller
		clnt        *client.MockClient
		mockMetrics *metrics.MockMetrics
		dprh        devicePluginReconcilerHelperAPI
		mn          node.Node
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockMetrics = metrics.NewMockMetrics(ctrl)
		mn = node.NewMockNode(ctrl)
		dprh = newDevicePluginReconcilerHelper(clnt, clnt, mockMetrics, mn, nil)
	})

	ctx := context.Background()

	It("failed to list Modules", func() {
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		dprh.setKMMOMetrics(ctx)
	})

	DescribeTable("getting metrics data", func(buildInContainer, buildInKM, signInContainer, signInKM, devicePlugin bool, modprobeArg, modprobeRawArg []string) {
		km := kmmv1beta1.KernelMapping{}
		mod1 := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "namespace",
			},
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: &kmmv1beta1.ModuleLoaderSpec{},
			},
		}
		mod2 := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: &kmmv1beta1.ModuleLoaderSpec{},
			},
		}
		mod3 := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: &kmmv1beta1.ModuleLoaderSpec{},
			},
		}
		numBuild := 0
		numSign := 0
		numDevicePlugin := 0
		if buildInContainer {
			mod1.Spec.ModuleLoader.Container.Build = &kmmv1beta1.Build{}
			numBuild = 1
		}
		if buildInKM {
			km.Build = &kmmv1beta1.Build{}
			numBuild = 1
		}
		if signInContainer {
			mod1.Spec.ModuleLoader.Container.Sign = &kmmv1beta1.Sign{}
			numSign = 1
		}
		if signInKM {
			km.Sign = &kmmv1beta1.Sign{}
			numSign = 1
		}
		if devicePlugin {
			mod1.Spec.DevicePlugin = &kmmv1beta1.DevicePluginSpec{}
			numDevicePlugin = 1
		}
		if modprobeArg != nil {
			mod1.Spec.ModuleLoader.Container.Modprobe.Args = &kmmv1beta1.ModprobeArgs{Load: modprobeArg}
		}
		if modprobeRawArg != nil {
			mod1.Spec.ModuleLoader.Container.Modprobe.RawArgs = &kmmv1beta1.ModprobeArgs{Load: modprobeRawArg}
		}
		mod1.Spec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{km}

		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
				list.Items = []kmmv1beta1.Module{mod1, mod2, mod3}
				return nil
			},
		)

		if modprobeArg != nil {
			mockMetrics.EXPECT().SetKMMModprobeArgs(mod1.Name, mod1.Namespace, strings.Join(modprobeArg, ","))
		}
		if modprobeRawArg != nil {
			mockMetrics.EXPECT().SetKMMModprobeRawArgs(mod1.Name, mod1.Namespace, strings.Join(modprobeRawArg, ","))
		}

		mockMetrics.EXPECT().SetKMMModulesNum(3)
		mockMetrics.EXPECT().SetKMMInClusterBuildNum(numBuild)
		mockMetrics.EXPECT().SetKMMInClusterSignNum(numSign)
		mockMetrics.EXPECT().SetKMMDevicePluginNum(numDevicePlugin)

		dprh.setKMMOMetrics(ctx)
	},
		Entry("build in container", true, false, false, false, false, nil, nil),
		Entry("build in KM", false, true, false, false, false, nil, nil),
		Entry("build in container and KM", true, true, false, false, false, nil, nil),
		Entry("sign in container", false, false, true, false, false, nil, nil),
		Entry("sign in KM", false, false, false, true, false, nil, nil),
		Entry("sign in container and KM", false, false, true, true, false, nil, nil),
		Entry("device plugin", false, false, false, false, true, nil, nil),
		Entry("modprobe args", false, false, false, false, false, []string{"param1", "param2"}, nil),
		Entry("modprobe raw args", false, false, false, false, false, nil, []string{"rawparam1", "rawparam2"}),
		Entry("altogether", true, true, true, true, true, []string{"param1", "param2"}, []string{"rawparam1", "rawparam2"}),
	)
})

var _ = Describe("DevicePluginReconciler_moduleUpdateDevicePluginStatus", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		statusWriter *client.MockStatusWriter
		dprh         devicePluginReconcilerHelperAPI
		mn           node.Node
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		statusWriter = client.NewMockStatusWriter(ctrl)
		mn = node.NewNode(clnt)
		dprh = newDevicePluginReconcilerHelper(clnt, clnt, nil, mn, nil)
	})

	ctx := context.Background()

	It("device plugin not defined in the module", func() {
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: &kmmv1beta1.ModuleLoaderSpec{},
			},
		}
		err := dprh.moduleUpdateDevicePluginStatus(ctx, &mod, nil)
		Expect(err).NotTo(HaveOccurred())
	})

	DescribeTable("device-plugin status update",
		func(numTargetedNodes int, numAvailableInDaemonSets []int, nodesMatchingNumber, availableNumber int) {
			mod := kmmv1beta1.Module{
				Spec: kmmv1beta1.ModuleSpec{
					DevicePlugin: &kmmv1beta1.DevicePluginSpec{},
				},
			}
			expectedMod := mod.DeepCopy()
			expectedMod.Status.DevicePlugin.NodesMatchingSelectorNumber = int32(nodesMatchingNumber)
			expectedMod.Status.DevicePlugin.DesiredNumber = int32(nodesMatchingNumber)
			expectedMod.Status.DevicePlugin.AvailableNumber = int32(availableNumber)

			nodesList := []v1.Node{}
			for i := 0; i < numTargetedNodes; i++ {
				nodesList = append(nodesList, v1.Node{})
			}
			daemonSetsList := []appsv1.DaemonSet{}
			for _, numAvailable := range numAvailableInDaemonSets {
				ds := appsv1.DaemonSet{
					Status: appsv1.DaemonSetStatus{
						NumberAvailable: int32(numAvailable),
					},
				}
				daemonSetsList = append(daemonSetsList, ds)
			}

			clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *v1.NodeList, _ ...interface{}) error {
					list.Items = nodesList
					return nil
				},
			)
			clnt.EXPECT().Status().Return(statusWriter)
			statusWriter.EXPECT().Patch(ctx, expectedMod, gomock.Any())

			err := dprh.moduleUpdateDevicePluginStatus(ctx, &mod, daemonSetsList)
			Expect(err).NotTo(HaveOccurred())
		},
		Entry("0 target node, 0 ds", 0, nil, 0, 0),
		Entry("0 target node, 1 ds", 0, []int{1}, 0, 1),
		Entry("0 target node, 2 ds", 0, []int{3, 6}, 0, 9),
		Entry("3 target node, 0 ds", 3, nil, 3, 0),
		Entry("2 target node, 3 ds", 2, []int{3, 6, 8}, 2, 17),
	)
})

var _ = Describe("DevicePluginReconciler_clearDevicePluginStatus", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		statusWriter *client.MockStatusWriter
		dprh         devicePluginReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		statusWriter = client.NewMockStatusWriter(ctrl)
		dprh = devicePluginReconcilerHelper{
			client: clnt,
		}
	})

	ctx := context.Background()

	It("should be a no-op when status.devicePlugin is already empty", func() {
		mod := kmmv1beta1.Module{}
		err := dprh.clearDevicePluginStatus(ctx, &mod)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should clear status.devicePlugin when it has values", func() {
		mod := kmmv1beta1.Module{
			Status: kmmv1beta1.ModuleStatus{
				DevicePlugin: kmmv1beta1.DaemonSetStatus{
					NodesMatchingSelectorNumber: 3,
					DesiredNumber:               3,
					AvailableNumber:             2,
				},
			},
		}

		expectedMod := mod.DeepCopy()
		expectedMod.Status.DevicePlugin = kmmv1beta1.DaemonSetStatus{}

		clnt.EXPECT().Status().Return(statusWriter)
		statusWriter.EXPECT().Patch(ctx, expectedMod, gomock.Any())

		err := dprh.clearDevicePluginStatus(ctx, &mod)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("DevicePluginReconciler_setDevicePluginAsDesired", func() {
	const (
		devicePluginImage = "device-plugin-image"
	)

	var (
		dsc daemonSetCreator
	)

	BeforeEach(func() {
		dsc = newDaemonSetCreator(scheme)
	})

	It("should return an error if the DaemonSet is nil", func() {
		Expect(
			dsc.setDevicePluginAsDesired(context.Background(), nil, &kmmv1beta1.Module{}),
		).To(
			HaveOccurred(),
		)
	})

	It("should return an error if DevicePlugin not set in the Spec", func() {
		ds := appsv1.DaemonSet{}
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: &kmmv1beta1.ModuleLoaderSpec{},
			},
		}
		Expect(
			dsc.setDevicePluginAsDesired(context.Background(), &ds, &mod),
		).To(
			HaveOccurred(),
		)
	})

	It("should add additional volumes if there are any", func() {
		vol := v1.Volume{Name: "test-volume"}

		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{
					Container: kmmv1beta1.DevicePluginContainerSpec{Image: devicePluginImage},
					Volumes:   []v1.Volume{vol},
				},
			},
		}

		ds := appsv1.DaemonSet{}

		err := dsc.setDevicePluginAsDesired(context.Background(), &ds, &mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(2))
		Expect(ds.Spec.Template.Spec.Volumes[1]).To(Equal(vol))
	})

	It("should add module version if it was defined in the Module", func() {
		vol := v1.Volume{Name: "test-volume"}

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: &kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						Version: "some version",
					},
				},
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{
					Container: kmmv1beta1.DevicePluginContainerSpec{Image: devicePluginImage},
					Volumes:   []v1.Volume{vol},
				},
			},
		}

		ds := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
		}

		err := dsc.setDevicePluginAsDesired(context.Background(), &ds, &mod)

		Expect(err).NotTo(HaveOccurred())
		versionLabel := utils.GetSchedulePodVersionLabelName(namespace, moduleName)
		Expect(ds.GetLabels()).Should(HaveKeyWithValue(versionLabel, "some version"))
	})

	DescribeTable("should work as expected",
		func(moduleLoader *kmmv1beta1.ModuleLoaderSpec, expectedNodeSelector map[string]string, withInitContainer bool, customLiveness *v1.Probe, customStartup *v1.Probe) {
			const (
				dsName             = "ds-name"
				serviceAccountName = "some-service-account"
			)

			dpVol := v1.Volume{
				Name:         "test-volume",
				VolumeSource: v1.VolumeSource{},
			}

			dpVolMount := v1.VolumeMount{
				Name:      "some-dp-volume-mount",
				MountPath: "/some/path",
			}

			repoSecret := v1.LocalObjectReference{Name: "pull-secret-name"}

			env := []v1.EnvVar{
				{
					Name:  "ENV_KEY",
					Value: "ENV_VALUE",
				},
			}

			resources := v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU:    resource.MustParse("200m"),
					v1.ResourceMemory: resource.MustParse("4G"),
				},
				Requests: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU:    resource.MustParse("100m"),
					v1.ResourceMemory: resource.MustParse("2G"),
				},
			}

			args := []string{"some", "args"}
			command := []string{"some", "command"}

			testToleration := v1.Toleration{
				Key:    "test-key",
				Value:  "test-value",
				Effect: v1.TaintEffectNoExecute,
			}

			const ipp = v1.PullIfNotPresent

			initContainer := &kmmv1beta1.DevicePluginContainerSpec{
				Args:         args,
				Command:      command,
				Env:          env,
				Image:        devicePluginImage,
				Resources:    resources,
				VolumeMounts: []v1.VolumeMount{dpVolMount},
			}
			if !withInitContainer {
				initContainer = nil
			}

			mod := kmmv1beta1.Module{
				TypeMeta: metav1.TypeMeta{
					APIVersion: kmmv1beta1.GroupVersion.String(),
					Kind:       "Module",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: namespace,
				},
				Spec: kmmv1beta1.ModuleSpec{
					ModuleLoader: moduleLoader,
					DevicePlugin: &kmmv1beta1.DevicePluginSpec{
						InitContainer: initContainer,
						Container: kmmv1beta1.DevicePluginContainerSpec{
							Args:            args,
							Command:         command,
							Env:             env,
							Image:           devicePluginImage,
							ImagePullPolicy: ipp,
							Resources:       resources,
							VolumeMounts:    []v1.VolumeMount{dpVolMount},
							LivenessProbe:   customLiveness,
							StartupProbe:    customStartup,
						},
						ServiceAccountName:           serviceAccountName,
						Volumes:                      []v1.Volume{dpVol},
						AutomountServiceAccountToken: ptr.To(false),
					},
					ImageRepoSecret: &repoSecret,
					Selector:        map[string]string{"has-feature-x": "true"},
					Tolerations:     []v1.Toleration{testToleration},
				},
			}
			ds := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dsName,
					Namespace: namespace,
				},
			}

			err := dsc.setDevicePluginAsDesired(context.Background(), &ds, &mod)
			Expect(err).NotTo(HaveOccurred())

			podLabels := map[string]string{
				constants.ModuleNameLabel: moduleName,
				constants.DaemonSetRole:   constants.DevicePluginRoleLabelValue,
			}

			expectedInitContainer := []v1.Container{
				{
					Args:      args,
					Command:   command,
					Env:       env,
					Image:     devicePluginImage,
					Name:      "device-plugin-init",
					Resources: resources,
					SecurityContext: &v1.SecurityContext{
						Privileged: ptr.To(true),
					},
					VolumeMounts: []v1.VolumeMount{
						dpVolMount,
					},
				},
			}

			if !withInitContainer {
				expectedInitContainer = nil
			}
			directory := v1.HostPathDirectory
			expected := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dsName,
					Namespace: namespace,
					Labels:    podLabels,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         mod.APIVersion,
							BlockOwnerDeletion: ptr.To(true),
							Controller:         ptr.To(true),
							Kind:               mod.Kind,
							Name:               moduleName,
							UID:                mod.UID,
						},
					},
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{MatchLabels: podLabels},
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:     podLabels,
							Finalizers: []string{constants.NodeLabelerFinalizer},
						},
						Spec: v1.PodSpec{
							InitContainers: expectedInitContainer,
							Containers: []v1.Container{
								{
									Args:            args,
									Command:         command,
									Env:             env,
									Image:           devicePluginImage,
									ImagePullPolicy: ipp,
									Name:            "device-plugin",
									Resources:       resources,
									SecurityContext: &v1.SecurityContext{
										Privileged: ptr.To(true),
									},
									VolumeMounts: []v1.VolumeMount{
										{
											Name:      "kubelet-device-plugins",
											MountPath: "/var/lib/kubelet/device-plugins",
										},
										dpVolMount,
									},
									LivenessProbe: customLiveness,
									StartupProbe:  customStartup,
								},
							},
							ImagePullSecrets:   []v1.LocalObjectReference{repoSecret},
							NodeSelector:       expectedNodeSelector,
							PriorityClassName:  "system-node-critical",
							ServiceAccountName: serviceAccountName,
							Volumes: []v1.Volume{
								{
									Name: "kubelet-device-plugins",
									VolumeSource: v1.VolumeSource{
										HostPath: &v1.HostPathVolumeSource{
											Path: "/var/lib/kubelet/device-plugins",
											Type: &directory,
										},
									},
								},
								dpVol,
							},
							Tolerations:                  []v1.Toleration{testToleration},
							AutomountServiceAccountToken: ptr.To(false),
						},
					},
				},
			}
			Expect(
				cmp.Equal(expected, ds),
			).To(
				BeTrue(), cmp.Diff(expected, ds),
			)
		},
		Entry("moduleLoader is nil",
			nil,
			map[string]string{"has-feature-x": "true"},
			false,
			nil, nil,
		),
		Entry("moduleLoader is defined",
			&kmmv1beta1.ModuleLoaderSpec{},
			map[string]string{
				utils.GetKernelModuleReadyNodeLabel(namespace, moduleName):  "",
				utils.GetDevicePluginTargetNodeLabel(namespace, moduleName): "",
			},
			true,
			nil, nil,
		),
		Entry("with custom liveness probe",
			nil,
			map[string]string{"has-feature-x": "true"},
			false,
			&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt32(8080)},
				},
				PeriodSeconds: 30,
			}, nil,
		),
		Entry("with custom startup probe",
			nil,
			map[string]string{"has-feature-x": "true"},
			false,
			nil,
			&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{Path: "/ready", Port: intstr.FromInt32(8080)},
				},
				InitialDelaySeconds: 10,
				PeriodSeconds:       5,
				FailureThreshold:    30,
			},
		),
		Entry("with custom liveness and startup probes",
			nil,
			map[string]string{"has-feature-x": "true"},
			false,
			&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{Path: "/healthz", Port: intstr.FromInt32(8080)},
				},
				PeriodSeconds: 30,
			},
			&v1.Probe{
				ProbeHandler: v1.ProbeHandler{
					HTTPGet: &v1.HTTPGetAction{Path: "/ready", Port: intstr.FromInt32(8080)},
				},
				InitialDelaySeconds: 10,
				PeriodSeconds:       5,
				FailureThreshold:    30,
			},
		),
	)
})

var _ = Describe("DevicePluginReconciler_getExistingDSFromVersion", func() {
	const (
		moduleName      = "moduleName"
		moduleNamespace = "moduleNamespace"
		kernelVersion   = "kernelVersion"
		moduleVersion   = "moduleVersion"
	)

	devicePluginLabels := map[string]string{
		utils.GetSchedulePodVersionLabelName(moduleNamespace, moduleName): moduleVersion,
	}

	ds := appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: moduleNamespace, Name: moduleName},
	}

	It("various scenarios", func() {
		By("empty daemonset list")
		res, version := getExistingDSFromVersion(nil, moduleNamespace, moduleName, &kmmv1beta1.ModuleLoaderSpec{Container: kmmv1beta1.ModuleLoaderContainerSpec{Version: moduleVersion}})
		Expect(res).To(BeNil())
		Expect(version).To(Equal(moduleVersion))

		By("device plugin, module version equal")
		ds.SetLabels(devicePluginLabels)
		res, version = getExistingDSFromVersion([]appsv1.DaemonSet{ds}, moduleNamespace, moduleName, &kmmv1beta1.ModuleLoaderSpec{Container: kmmv1beta1.ModuleLoaderContainerSpec{Version: moduleVersion}})
		Expect(res).To(Equal(&ds))
		Expect(version).To(Equal(moduleVersion))

		By("device plugin, module version not equal")
		res, version = getExistingDSFromVersion([]appsv1.DaemonSet{ds}, moduleNamespace, moduleName, &kmmv1beta1.ModuleLoaderSpec{Container: kmmv1beta1.ModuleLoaderContainerSpec{Version: "some-version"}})
		Expect(res).To(BeNil())
		Expect(version).To(Equal("some-version"))

		By("device plugin, module version label missing, and module version parameter is empty")
		ds.SetLabels(map[string]string{})
		res, version = getExistingDSFromVersion([]appsv1.DaemonSet{ds}, moduleNamespace, moduleName, &kmmv1beta1.ModuleLoaderSpec{Container: kmmv1beta1.ModuleLoaderContainerSpec{Version: ""}})
		Expect(res).To(Equal(&ds))
		Expect(version).To(Equal(""))
	})
})

var _ = Describe("DevicePluginReconciler_getModuleDevicePluginDaemonSets", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		dprh devicePluginReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		dprh = devicePluginReconcilerHelper{
			client: clnt,
		}
	})

	ctx := context.Background()

	It("list failed", func() {
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		dsList, err := dprh.getModuleDevicePluginDaemonSets(ctx, "name", "namespace")

		Expect(err).ToNot(BeNil())
		Expect(dsList).To(BeNil())
	})

	It("good flow, return only device plugin DSs, excluding module-loader and DRA roles", func() {
		ds1 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					constants.ModuleNameLabel: "some name",
				},
			},
		}
		ds2 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					constants.ModuleNameLabel: "some name",
					constants.DaemonSetRole:   constants.ModuleLoaderRoleLabelValue,
				},
			},
		}
		ds3 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					constants.ModuleNameLabel: "some name",
					constants.DaemonSetRole:   "some role",
				},
			},
		}
		ds4 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					constants.ModuleNameLabel: "some name",
					constants.DaemonSetRole:   constants.DRARoleLabelValue,
				},
			},
		}
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *appsv1.DaemonSetList, _ ...interface{}) error {
				list.Items = []appsv1.DaemonSet{ds1, ds2, ds3, ds4}
				return nil
			},
		)

		dsList, err := dprh.getModuleDevicePluginDaemonSets(ctx, "name", "namespace")

		Expect(err).NotTo(HaveOccurred())
		Expect(dsList).To(Equal([]appsv1.DaemonSet{ds1, ds3}))
	})
})

var _ = Describe("devicePluginReconcilerHelper_handleDevicePluginTargetLabels", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		nm   *node.MockNode
		dprh devicePluginReconcilerHelper
		ctx  context.Context
		mod  *kmmv1beta1.Module
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		nm = node.NewMockNode(ctrl)
		ctx = context.Background()
		dprh = devicePluginReconcilerHelper{
			client:  clnt,
			nodeAPI: nm,
		}
		mod = &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: moduleName},
			Spec: kmmv1beta1.ModuleSpec{
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{},
			},
		}
	})

	It("should return nil when DevicePlugin is nil", func() {
		mod.Spec.DevicePlugin = nil
		err := dprh.handleDevicePluginTargetLabels(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should add target label to schedulable node", func() {
		targetLabel := utils.GetDevicePluginTargetNodeLabel(namespace, moduleName)

		schedulableNode := v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "schedulable-node"},
		}

		nm.EXPECT().GetAllNodesBySelector(ctx, mod.Spec.Selector).Return([]v1.Node{schedulableNode}, nil)
		nm.EXPECT().IsNodeSchedulable(&schedulableNode, mod.Spec.Tolerations).Return(true)
		nm.EXPECT().UpdateLabels(ctx, &schedulableNode, map[string]string{targetLabel: ""}, nil).Return(nil)

		err := dprh.handleDevicePluginTargetLabels(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should remove target label from unschedulable node", func() {
		targetLabel := utils.GetDevicePluginTargetNodeLabel(namespace, moduleName)

		unschedulableNode := v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "unschedulable-node"},
		}

		nm.EXPECT().GetAllNodesBySelector(ctx, mod.Spec.Selector).Return([]v1.Node{unschedulableNode}, nil)
		nm.EXPECT().IsNodeSchedulable(&unschedulableNode, mod.Spec.Tolerations).Return(false)
		nm.EXPECT().UpdateLabels(ctx, &unschedulableNode, nil, map[string]string{targetLabel: ""}).Return(nil)

		err := dprh.handleDevicePluginTargetLabels(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should continue processing nodes if one fails and return combined error", func() {
		targetLabel := utils.GetDevicePluginTargetNodeLabel(namespace, moduleName)

		node1 := v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node1"},
		}
		node2 := v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "node2"},
		}

		nm.EXPECT().GetAllNodesBySelector(ctx, mod.Spec.Selector).Return([]v1.Node{node1, node2}, nil)
		nm.EXPECT().IsNodeSchedulable(&node1, mod.Spec.Tolerations).Return(true)
		nm.EXPECT().UpdateLabels(ctx, &node1, map[string]string{targetLabel: ""}, nil).Return(fmt.Errorf("conflict"))
		nm.EXPECT().IsNodeSchedulable(&node2, mod.Spec.Tolerations).Return(true)
		nm.EXPECT().UpdateLabels(ctx, &node2, map[string]string{targetLabel: ""}, nil).Return(nil)

		err := dprh.handleDevicePluginTargetLabels(ctx, mod)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("node1"))
		Expect(err.Error()).NotTo(ContainSubstring("node2"))
	})
})
var _ = Describe("DevicePluginSpec backward compatibility", func() {
	It("should serialize JSON with the same field names as before the type refactoring", func() {
		spec := kmmv1beta1.DevicePluginSpec{
			Container: kmmv1beta1.DevicePluginContainerSpec{
				Image:   "quay.io/example/plugin:v1",
				Command: []string{"/bin/plugin"},
			},
			ServiceAccountName:           "dp-sa",
			Volumes:                      []v1.Volume{{Name: "sock", VolumeSource: v1.VolumeSource{EmptyDir: &v1.EmptyDirVolumeSource{}}}},
			AutomountServiceAccountToken: ptr.To(false),
		}

		data, err := json.Marshal(spec)
		Expect(err).NotTo(HaveOccurred())

		var raw map[string]interface{}
		Expect(json.Unmarshal(data, &raw)).To(Succeed())

		Expect(raw).To(HaveKey("container"))
		Expect(raw).To(HaveKey("serviceAccountName"))
		Expect(raw).To(HaveKey("volumes"))
		Expect(raw).To(HaveKey("automountServiceAccountToken"))
		Expect(raw).NotTo(HaveKey("CommonSpec"))
	})

	It("should allow accessing CommonSpec fields directly on DevicePluginSpec", func() {
		spec := kmmv1beta1.DevicePluginSpec{
			Container: kmmv1beta1.DevicePluginContainerSpec{
				Image:   "quay.io/example/plugin:v1",
				Command: []string{"/bin/plugin"},
			},
			ServiceAccountName:           "dp-sa",
			Volumes:                      []v1.Volume{{Name: "sock", VolumeSource: v1.VolumeSource{EmptyDir: &v1.EmptyDirVolumeSource{}}}},
			AutomountServiceAccountToken: ptr.To(false),
		}

		Expect(spec.Container.Image).To(Equal("quay.io/example/plugin:v1"))
		Expect(spec.ServiceAccountName).To(Equal("dp-sa"))
		Expect(spec.Volumes).To(HaveLen(1))
		Expect(*spec.AutomountServiceAccountToken).To(BeFalse())
	})
})
