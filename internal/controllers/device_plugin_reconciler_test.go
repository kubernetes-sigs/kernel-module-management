package controllers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/go-cmp/cmp"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/metrics"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/node"
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
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("DevicePluginReconciler finalizer lifecycle", func() {
	It("keeps a device-plugin-only Module available when label cleanup must be retried", func() {
		ctx := context.Background()
		mod := &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name: moduleName, Namespace: namespace,
				Finalizers: []string{constants.ModuleFinalizer},
			},
			Spec: kmmv1beta1.ModuleSpec{DevicePlugin: &kmmv1beta1.DevicePluginSpec{}},
		}
		failNodeList := true
		clnt := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mod).WithInterceptorFuncs(interceptor.Funcs{
			List: func(ctx context.Context, c ctrlclient.WithWatch, list ctrlclient.ObjectList, opts ...ctrlclient.ListOption) error {
				if _, ok := list.(*v1.NodeList); ok && failNodeList {
					return fmt.Errorf("node list failed")
				}
				return c.List(ctx, list, opts...)
			},
		}).Build()
		r := NewDevicePluginReconciler(clnt, metrics.New(), nil, node.NewNode(clnt), scheme)
		key := ctrlclient.ObjectKeyFromObject(mod)

		// Stop the first pass before resources are created, then start deletion.
		_, err := r.Reconcile(ctx, mod)
		Expect(err).To(HaveOccurred())
		Expect(clnt.Get(ctx, key, mod)).To(Succeed())
		Expect(mod.Finalizers).To(ContainElement(constants.DevicePluginFinalizer))

		Expect(clnt.Delete(ctx, mod)).To(Succeed())
		Expect(clnt.Get(ctx, key, mod)).To(Succeed())
		original := mod.DeepCopy()
		mod.Finalizers = []string{constants.DevicePluginFinalizer}
		Expect(clnt.Patch(ctx, mod, ctrlclient.MergeFrom(original))).To(Succeed())

		_, err = r.Reconcile(ctx, mod)
		Expect(err).To(HaveOccurred())
		Expect(clnt.Get(ctx, key, mod)).To(Succeed())

		failNodeList = false
		_, err = r.Reconcile(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(apierrors.IsNotFound(clnt.Get(ctx, key, mod))).To(BeTrue())
	})

	DescribeTable("waits for DaemonSets before releasing its finalizer", func(deleting bool) {
		ctx := context.Background()
		targetLabel := utils.GetDevicePluginTargetNodeLabel(namespace, moduleName)
		mod := &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name: moduleName, Namespace: namespace,
				Finalizers: []string{constants.ModuleFinalizer, constants.DevicePluginFinalizer},
			},
		}
		if deleting {
			mod.Spec.DevicePlugin = &kmmv1beta1.DevicePluginSpec{}
		}
		ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{
			Name: "device-plugin", Namespace: namespace, UID: "device-plugin-uid",
			Labels: map[string]string{
				constants.ModuleNameLabel: moduleName,
				constants.DaemonSetRole:   constants.DevicePluginRoleLabelValue,
			},
			Finalizers: []string{"example.com/hold"},
		}}
		n := &v1.Node{ObjectMeta: metav1.ObjectMeta{
			Name: "labelled-node", Labels: map[string]string{targetLabel: "", "unrelated": "keep"},
		}}
		clnt := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mod, ds, n).Build()
		r := NewDevicePluginReconciler(clnt, metrics.New(), nil, node.NewNode(clnt), scheme)
		if deleting {
			Expect(clnt.Delete(ctx, mod)).To(Succeed())
		}
		Expect(clnt.Get(ctx, ctrlclient.ObjectKeyFromObject(mod), mod)).To(Succeed())

		_, err := r.Reconcile(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(clnt.Get(ctx, ctrlclient.ObjectKeyFromObject(mod), mod)).To(Succeed())
		Expect(mod.Finalizers).To(ContainElement(constants.DevicePluginFinalizer))
		Expect(clnt.Get(ctx, ctrlclient.ObjectKeyFromObject(n), n)).To(Succeed())
		Expect(n.Labels).To(Equal(map[string]string{"unrelated": "keep"}))
		Expect(clnt.Get(ctx, ctrlclient.ObjectKeyFromObject(ds), ds)).To(Succeed())
		Expect(ds.DeletionTimestamp).NotTo(BeNil())

		// Repeated reconciliations must keep waiting for an already terminating DaemonSet.
		_, err = r.Reconcile(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(clnt.Get(ctx, ctrlclient.ObjectKeyFromObject(mod), mod)).To(Succeed())
		Expect(mod.Finalizers).To(ContainElement(constants.DevicePluginFinalizer))

		original := ds.DeepCopy()
		ds.Finalizers = nil
		Expect(clnt.Patch(ctx, ds, ctrlclient.MergeFrom(original))).To(Succeed())
		_, err = r.Reconcile(ctx, mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(clnt.Get(ctx, ctrlclient.ObjectKeyFromObject(mod), mod)).To(Succeed())
		Expect(mod.Finalizers).To(Equal([]string{constants.ModuleFinalizer}))
	},
		Entry("Module deletion", true),
		Entry("device-plugin spec removal", false),
	)
})

var _ = Describe("DevicePluginReconciler_setFinalizer", func() {
	It("does not write when its finalizer is already registered", func() {
		ctrl := gomock.NewController(GinkgoT())
		helper := devicePluginReconcilerHelper{client: client.NewMockClient(ctrl)}
		mod := &kmmv1beta1.Module{ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{constants.DevicePluginFinalizer},
		}}

		Expect(helper.setFinalizer(context.Background(), mod)).To(Succeed())
	})

	It("preserves existing finalizers when registering its own", func() {
		ctx := context.Background()
		mod := &kmmv1beta1.Module{ObjectMeta: metav1.ObjectMeta{
			Name: moduleName, Namespace: namespace, Finalizers: []string{constants.ModuleFinalizer},
		}}
		clnt := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mod).Build()
		helper := devicePluginReconcilerHelper{client: clnt}
		Expect(clnt.Get(ctx, ctrlclient.ObjectKeyFromObject(mod), mod)).To(Succeed())

		Expect(helper.setFinalizer(ctx, mod)).To(Succeed())
		Expect(clnt.Get(ctx, ctrlclient.ObjectKeyFromObject(mod), mod)).To(Succeed())
		Expect(mod.Finalizers).To(ConsistOf(constants.ModuleFinalizer, constants.DevicePluginFinalizer))
	})
})

var _ = Describe("DevicePluginReconciler finalizer conflicts", func() {
	DescribeTable("preserves a concurrent finalizer update", func(removing bool) {
		ctx := context.Background()
		mod := &kmmv1beta1.Module{ObjectMeta: metav1.ObjectMeta{
			Name: moduleName, Namespace: namespace,
			Finalizers: []string{constants.ModuleFinalizer},
		}}
		if removing {
			mod.Finalizers = append(mod.Finalizers, constants.DevicePluginFinalizer)
		}
		clnt := fake.NewClientBuilder().WithScheme(scheme).WithObjects(mod).Build()
		helper := devicePluginReconcilerHelper{client: clnt, nodeAPI: node.NewNode(clnt)}
		key := ctrlclient.ObjectKeyFromObject(mod)
		Expect(clnt.Get(ctx, key, mod)).To(Succeed())
		updated := mod.DeepCopy()
		updated.Finalizers = append(updated.Finalizers, "example.com/concurrent")
		Expect(clnt.Update(ctx, updated)).To(Succeed())

		var err error
		if removing {
			err = helper.finalizeModule(ctx, mod)
		} else {
			err = helper.setFinalizer(ctx, mod)
		}
		Expect(apierrors.IsConflict(err)).To(BeTrue())
		Expect(clnt.Get(ctx, key, mod)).To(Succeed())
		Expect(mod.Finalizers).To(Equal(updated.Finalizers))
	},
		Entry("registration", false),
		Entry("removal", true),
	)
})

var _ = Describe("DevicePluginReconciler_finalizeModule", func() {
	var (
		clnt   *client.MockClient
		nodes  *node.MockNode
		helper devicePluginReconcilerHelper
		mod    *kmmv1beta1.Module
	)
	ctx := context.Background()
	targetLabel := utils.GetDevicePluginTargetNodeLabel(namespace, moduleName)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		nodes = node.NewMockNode(ctrl)
		helper = devicePluginReconcilerHelper{client: clnt, nodeAPI: nodes}
		mod = &kmmv1beta1.Module{ObjectMeta: metav1.ObjectMeta{
			Name: moduleName, Namespace: namespace,
			Finalizers: []string{constants.ModuleFinalizer, constants.DevicePluginFinalizer},
		}}
	})

	listNodes := func(items ...v1.Node) *gomock.Call {
		return nodes.EXPECT().GetAllNodesBySelector(ctx, map[string]string{targetLabel: ""}).Return(items, nil)
	}

	listDaemonSets := func(items ...appsv1.DaemonSet) *gomock.Call {
		return clnt.EXPECT().List(ctx, gomock.AssignableToTypeOf(&appsv1.DaemonSetList{}), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, list *appsv1.DaemonSetList, _ ...ctrlclient.ListOption) error {
				list.Items = items
				return nil
			},
		)
	}

	It("keeps the finalizer when labels cannot be listed", func() {
		nodes.EXPECT().GetAllNodesBySelector(ctx, map[string]string{targetLabel: ""}).Return(nil, fmt.Errorf("list failed"))

		Expect(helper.finalizeModule(ctx, mod)).To(HaveOccurred())
		Expect(mod.Finalizers).To(ContainElement(constants.DevicePluginFinalizer))
	})

	It("keeps the finalizer when a node cannot be unlabelled", func() {
		n := v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "labelled-node"}}
		listNodes(n)
		nodes.EXPECT().UpdateLabels(ctx, &n, nil, map[string]string{targetLabel: ""}).Return(fmt.Errorf("patch failed"))

		Expect(helper.finalizeModule(ctx, mod)).To(HaveOccurred())
		Expect(mod.Finalizers).To(ContainElement(constants.DevicePluginFinalizer))
	})

	It("keeps the finalizer when a DaemonSet cannot be deleted", func() {
		ds := appsv1.DaemonSet{}
		listNodes()
		listDaemonSets(ds)
		clnt.EXPECT().Delete(ctx, &ds).Return(fmt.Errorf("delete failed"))

		Expect(helper.finalizeModule(ctx, mod)).To(HaveOccurred())
		Expect(mod.Finalizers).To(ContainElement(constants.DevicePluginFinalizer))
	})

	It("keeps the finalizer when DaemonSets cannot be listed", func() {
		listNodes()
		clnt.EXPECT().List(ctx, gomock.AssignableToTypeOf(&appsv1.DaemonSetList{}), gomock.Any(), gomock.Any()).
			Return(fmt.Errorf("list failed"))

		Expect(helper.finalizeModule(ctx, mod)).To(HaveOccurred())
		Expect(mod.Finalizers).To(ContainElement(constants.DevicePluginFinalizer))
	})

	It("cleans up legacy Modules without a device-plugin finalizer", func() {
		mod.Finalizers = []string{constants.ModuleFinalizer}
		n := v1.Node{ObjectMeta: metav1.ObjectMeta{Name: "legacy-node"}}
		ds := appsv1.DaemonSet{}
		gomock.InOrder(
			listNodes(n),
			nodes.EXPECT().UpdateLabels(ctx, &n, nil, map[string]string{targetLabel: ""}).Return(nil),
			listDaemonSets(ds),
			clnt.EXPECT().Delete(ctx, &ds).Return(nil),
		)

		Expect(helper.finalizeModule(ctx, mod)).To(Succeed())
		Expect(mod.Finalizers).To(Equal([]string{constants.ModuleFinalizer}))
	})
})

var _ = Describe("DevicePluginReconciler_Reconcile", func() {
	var (
		ctrl            *gomock.Controller
		mockReconHelper *MockdevicePluginReconcilerHelperAPI
		mod             *kmmv1beta1.Module
		dpr             *DevicePluginReconciler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockReconHelper = NewMockdevicePluginReconcilerHelperAPI(ctrl)

		mod = &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: moduleName},
			Spec: kmmv1beta1.ModuleSpec{
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{},
			},
		}

		dpr = &DevicePluginReconciler{
			reconHelperAPI: mockReconHelper,
		}
	})

	ctx := context.Background()

	DescribeTable("check error flows", func(setFinalizerError, getDSError, handleTargetLabelsError, handlePluginError, gcError bool) {
		devicePluginDS := []appsv1.DaemonSet{appsv1.DaemonSet{}}
		returnedError := fmt.Errorf("some error")
		mockReconHelper.EXPECT().setKMMOMetrics(ctx)
		if setFinalizerError {
			mockReconHelper.EXPECT().setFinalizer(ctx, mod).Return(returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().setFinalizer(ctx, mod).Return(nil)
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
		Entry("setFinalizer failed", true, false, false, false, false),
		Entry("getDevicePluginDaemonSets failed", false, true, false, false, false),
		Entry("handleDevicePluginTargetLabels failed", false, false, true, false, false),
		Entry("handleDevicePlugin failed", false, false, false, true, false),
		Entry("garbageCollect failed", false, false, false, false, true),
		Entry("devicePluginUpdateStatus failed", false, false, false, false, false),
	)

	It("Good flow", func() {
		devicePluginDS := []appsv1.DaemonSet{appsv1.DaemonSet{}}
		gomock.InOrder(
			mockReconHelper.EXPECT().setKMMOMetrics(ctx),
			mockReconHelper.EXPECT().setFinalizer(ctx, mod).Return(nil),
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

	DescribeTable("module deletion flow", func(cleanupError error) {
		mod.SetDeletionTimestamp(&metav1.Time{})
		mockReconHelper.EXPECT().finalizeModule(ctx, mod).Return(cleanupError)

		res, err := dpr.Reconcile(ctx, mod)
		Expect(res).To(Equal(reconcile.Result{}))
		if cleanupError == nil {
			Expect(err).NotTo(HaveOccurred())
		} else {
			Expect(err).To(MatchError(ContainSubstring(cleanupError.Error())))
		}
	},
		Entry("cleanup succeeds", nil),
		Entry("cleanup fails", fmt.Errorf("some error")),
	)

	It("finalizes and clears status when spec.devicePlugin is nil", func() {
		mod.Spec.DevicePlugin = nil

		gomock.InOrder(
			mockReconHelper.EXPECT().setKMMOMetrics(ctx),
			mockReconHelper.EXPECT().finalizeModule(ctx, mod).Return(nil),
			mockReconHelper.EXPECT().clearDevicePluginStatus(ctx, mod).Return(nil),
		)

		res, err := dpr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("cleanup when spec.devicePlugin is nil and finalizeModule fails", func() {
		mod.Spec.DevicePlugin = nil

		gomock.InOrder(
			mockReconHelper.EXPECT().setKMMOMetrics(ctx),
			mockReconHelper.EXPECT().finalizeModule(ctx, mod).Return(fmt.Errorf("some error")),
		)

		res, err := dpr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).To(HaveOccurred())
	})

	It("cleanup when spec.devicePlugin is nil and clearDevicePluginStatus fails", func() {
		mod.Spec.DevicePlugin = nil

		gomock.InOrder(
			mockReconHelper.EXPECT().setKMMOMetrics(ctx),
			mockReconHelper.EXPECT().finalizeModule(ctx, mod).Return(nil),
			mockReconHelper.EXPECT().clearDevicePluginStatus(ctx, mod).Return(fmt.Errorf("some error")),
		)

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
		dprh = newDevicePluginReconcilerHelper(clnt, nil, mn, nil)
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

var _ = Describe("DevicePluginReconciler_deleteDevicePluginDaemonSets", func() {
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
		dprh = newDevicePluginReconcilerHelper(clnt, nil, mn, nil)
	})

	existingDevicePluginDS := []appsv1.DaemonSet{appsv1.DaemonSet{}}
	ctx := context.Background()

	It("good flow", func() {
		clnt.EXPECT().Delete(ctx, &existingDevicePluginDS[0]).Return(nil)

		err := dprh.deleteDevicePluginDaemonSets(ctx, existingDevicePluginDS)
		Expect(err).NotTo(HaveOccurred())
	})

	It("error flow", func() {
		clnt.EXPECT().Delete(ctx, &existingDevicePluginDS[0]).Return(fmt.Errorf("some error"))

		err := dprh.deleteDevicePluginDaemonSets(ctx, existingDevicePluginDS)
		Expect(err).To(HaveOccurred())
	})

	It("ignores an already deleted DaemonSet", func() {
		clnt.EXPECT().Delete(ctx, &existingDevicePluginDS[0]).
			Return(apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "daemonsets"}, "device-plugin"))

		Expect(dprh.deleteDevicePluginDaemonSets(ctx, existingDevicePluginDS)).To(Succeed())
	})

	It("handles a terminating DaemonSet", func() {
		ds := appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: ptr.To(metav1.Now())}}
		clnt.EXPECT().Delete(ctx, &ds).Return(nil)

		Expect(dprh.deleteDevicePluginDaemonSets(ctx, []appsv1.DaemonSet{ds})).To(Succeed())
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
		dprh = newDevicePluginReconcilerHelper(clnt, mockMetrics, mn, nil)
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
		dprh = newDevicePluginReconcilerHelper(clnt, nil, mn, nil)
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
							Tolerations:                  append([]v1.Toleration{testToleration}, module.InternalTolerations...),
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

	It("should default to privileged: true when SecurityContext is nil", func() {
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
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{
					Container: kmmv1beta1.DevicePluginContainerSpec{
						Image: "device-plugin-image",
					},
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

		container := ds.Spec.Template.Spec.Containers[0]
		Expect(container.SecurityContext).To(Equal(&v1.SecurityContext{Privileged: ptr.To(true)}))
	})

	It("should use a custom SecurityContext when set in the container spec", func() {
		customSC := &v1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			Capabilities: &v1.Capabilities{
				Drop: []v1.Capability{"ALL"},
			},
			ReadOnlyRootFilesystem: ptr.To(true),
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
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{
					Container: kmmv1beta1.DevicePluginContainerSpec{
						Image:           "device-plugin-image",
						SecurityContext: customSC,
					},
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

		container := ds.Spec.Template.Spec.Containers[0]
		Expect(container.SecurityContext).To(Equal(customSC))
	})
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

	// The real check, so InternalTolerations inside IsNodeSchedulable are under test.
	realSchedulability := func() {
		nm.EXPECT().IsNodeSchedulable(gomock.Any(), gomock.Any()).
			DoAndReturn(node.NewNode(nil).IsNodeSchedulable)
	}

	It("should add target label to a node under disk, memory, or pid pressure", func() {
		targetLabel := utils.GetDevicePluginTargetNodeLabel(namespace, moduleName)

		for _, key := range []string{v1.TaintNodeDiskPressure, v1.TaintNodeMemoryPressure, v1.TaintNodePIDPressure} {
			pressuredNode := v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: "pressured-node"},
				Spec: v1.NodeSpec{
					Taints: []v1.Taint{{Key: key, Effect: v1.TaintEffectNoSchedule}},
				},
			}

			nm.EXPECT().GetAllNodesBySelector(ctx, mod.Spec.Selector).Return([]v1.Node{pressuredNode}, nil)
			realSchedulability()
			nm.EXPECT().UpdateLabels(ctx, &pressuredNode, map[string]string{targetLabel: ""}, nil).Return(nil)

			Expect(dprh.handleDevicePluginTargetLabels(ctx, mod)).NotTo(HaveOccurred(), key)
		}
	})

	It("should remove the target label from a cordoned node", func() {
		targetLabel := utils.GetDevicePluginTargetNodeLabel(namespace, moduleName)
		cordoned := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "cordoned-node",
				Labels: map[string]string{targetLabel: ""},
			},
			Spec: v1.NodeSpec{
				Taints: []v1.Taint{{Key: v1.TaintNodeUnschedulable, Effect: v1.TaintEffectNoSchedule}},
			},
		}

		nm.EXPECT().GetAllNodesBySelector(ctx, mod.Spec.Selector).Return([]v1.Node{cordoned}, nil)
		realSchedulability()
		nm.EXPECT().UpdateLabels(ctx, &cordoned, nil, map[string]string{targetLabel: ""}).Return(nil)

		Expect(dprh.handleDevicePluginTargetLabels(ctx, mod)).NotTo(HaveOccurred())
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
