package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/kubernetes-sigs/kernel-module-management/internal/node"
	"github.com/kubernetes-sigs/kernel-module-management/internal/pod"
	"k8s.io/apimachinery/pkg/util/sets"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	testclient "github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/nmc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/yaml"
)

const (
	nmcName               = "nmc"
	nsFirst               = "example-ns-1"
	nsSecond              = "example-ns-2"
	nameFirst             = "example-name-1"
	nameSecond            = "example-name-2"
	imageFirst            = "example-image-1"
	imageSecond           = "example-image-2"
	kmodReadyLabel        = "kmm.node.kubernetes.io/test-ns.test-module.ready"
	kmodVersionReadyLabel = "kmm.node.kubernetes.io/test-ns.test-module.version.ready"
	labelNotToRemove      = "example.node.kubernetes.io/label-not-to-be-removed"
)

var _ = Describe("NodeModulesConfigReconciler_Reconcile", func() {
	var (
		kubeClient *testclient.MockClient
		wh         *MocknmcReconcilerHelper
		nm         *node.MockNode

		r *NMCReconciler

		ctx    = context.TODO()
		nmcNsn = types.NamespacedName{Name: nmcName}
		req    = reconcile.Request{NamespacedName: nmcNsn}
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = testclient.NewMockClient(ctrl)
		wh = NewMocknmcReconcilerHelper(ctrl)
		nm = node.NewMockNode(ctrl)
		r = &NMCReconciler{
			client:  kubeClient,
			helper:  wh,
			nodeAPI: nm,
		}
	})

	It("should clean worker Pod finalizers and return if the NMC does not exist", func() {
		gomock.InOrder(
			kubeClient.
				EXPECT().
				Get(ctx, nmcNsn, &kmmv1beta1.NodeModulesConfig{}).
				Return(k8serrors.NewNotFound(schema.GroupResource{}, nmcName)),
			wh.EXPECT().RemovePodFinalizers(ctx, nmcName),
		)

		Expect(
			r.Reconcile(ctx, req),
		).To(
			Equal(ctrl.Result{}),
		)
	})

	It("should fail if we could not synchronize the NMC status", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}
		node := v1.Node{}
		gomock.InOrder(
			kubeClient.
				EXPECT().
				Get(ctx, nmcNsn, &kmmv1beta1.NodeModulesConfig{}).
				Do(func(_ context.Context, _ types.NamespacedName, kubeNmc ctrlclient.Object, _ ...ctrlclient.Options) {
					*kubeNmc.(*kmmv1beta1.NodeModulesConfig) = *nmc
				}),
			kubeClient.EXPECT().Get(ctx, types.NamespacedName{Name: nmc.Name}, &node).Return(nil),
			wh.EXPECT().SyncStatus(ctx, nmc, &node).Return(errors.New("random error")),
		)

		_, err := r.Reconcile(ctx, req)
		Expect(err).To(HaveOccurred())
	})

	It("should fail if we could not get the node of the NMC", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}
		node := v1.Node{}
		gomock.InOrder(
			kubeClient.
				EXPECT().
				Get(ctx, nmcNsn, &kmmv1beta1.NodeModulesConfig{}).
				Do(func(_ context.Context, _ types.NamespacedName, kubeNmc ctrlclient.Object, _ ...ctrlclient.Options) {
					*kubeNmc.(*kmmv1beta1.NodeModulesConfig) = *nmc
				}),
			kubeClient.EXPECT().Get(ctx, types.NamespacedName{Name: nmc.Name}, &node).Return(fmt.Errorf("some error")),
		)

		_, err := r.Reconcile(ctx, req)
		Expect(err).To(HaveOccurred())
	})

	It("should remove kmod labels and not continue if node is not schedulable", func() {
		spec0 := kmmv1beta1.NodeModuleSpec{
			ModuleItem: kmmv1beta1.ModuleItem{
				Namespace: "test-ns",
				Name:      "test-module",
			},
		}
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
			Spec: kmmv1beta1.NodeModulesConfigSpec{
				Modules: []kmmv1beta1.NodeModuleSpec{spec0},
			},
		}
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					kmodReadyLabel:   "",
					labelNotToRemove: "",
				},
			},
		}
		expectedNode := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					labelNotToRemove: "",
				},
			},
		}
		gomock.InOrder(
			kubeClient.
				EXPECT().
				Get(ctx, nmcNsn, &kmmv1beta1.NodeModulesConfig{}).
				Do(func(_ context.Context, _ types.NamespacedName, kubeNmc ctrlclient.Object, _ ...ctrlclient.Options) {
					*kubeNmc.(*kmmv1beta1.NodeModulesConfig) = *nmc
				}),
			kubeClient.EXPECT().Get(ctx, types.NamespacedName{Name: nmc.Name}, &v1.Node{}).DoAndReturn(
				func(_ context.Context, _ types.NamespacedName, fetchedNode *v1.Node, _ ...ctrlclient.Options) error {
					*fetchedNode = node
					return nil
				},
			),
			wh.EXPECT().SyncStatus(ctx, nmc, &node),
			nm.EXPECT().IsNodeSchedulable(&node, nil).Return(false),
			nm.EXPECT().UpdateLabels(ctx, &node, nil, map[string]string{kmodReadyLabel: "", kmodVersionReadyLabel: ""}).DoAndReturn(
				func(_ context.Context, obj ctrlclient.Object, _, _ map[string]string) error {
					delete(node.ObjectMeta.Labels, kmodReadyLabel)
					delete(node.ObjectMeta.Labels, kmodVersionReadyLabel)
					return nil
				},
			),
		)

		_, err := r.Reconcile(ctx, req)
		Expect(err).To(BeNil())
		Expect(node).To(Equal(expectedNode))
	})

	It("should fail to remove kmod labels and not continue if node is not schedulable", func() {
		spec0 := kmmv1beta1.NodeModuleSpec{
			ModuleItem: kmmv1beta1.ModuleItem{
				Namespace: "test-ns",
				Name:      "test-module",
			},
		}
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
			Spec: kmmv1beta1.NodeModulesConfigSpec{
				Modules: []kmmv1beta1.NodeModuleSpec{spec0},
			},
		}
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					kmodReadyLabel:        "",
					kmodVersionReadyLabel: "2",
					labelNotToRemove:      "",
				},
			},
		}
		expectedNode := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					labelNotToRemove: "",
				},
			},
		}
		gomock.InOrder(
			kubeClient.
				EXPECT().
				Get(ctx, nmcNsn, &kmmv1beta1.NodeModulesConfig{}).
				Do(func(_ context.Context, _ types.NamespacedName, kubeNmc ctrlclient.Object, _ ...ctrlclient.Options) {
					*kubeNmc.(*kmmv1beta1.NodeModulesConfig) = *nmc
				}),
			kubeClient.EXPECT().Get(ctx, types.NamespacedName{Name: nmc.Name}, &v1.Node{}).DoAndReturn(
				func(_ context.Context, _ types.NamespacedName, fetchedNode *v1.Node, _ ...ctrlclient.Options) error {
					*fetchedNode = node
					return nil
				},
			),
			wh.EXPECT().SyncStatus(ctx, nmc, &node),
			nm.EXPECT().IsNodeSchedulable(&node, nil).Return(false),
			nm.EXPECT().UpdateLabels(ctx, &node, nil, map[string]string{kmodReadyLabel: "", kmodVersionReadyLabel: ""}).DoAndReturn(
				func(_ context.Context, obj ctrlclient.Object, _, _ map[string]string) error {
					return fmt.Errorf("some error")
				},
			),
		)

		_, err := r.Reconcile(ctx, req)
		Expect(err).To(HaveOccurred())
		Expect(node).ToNot(Equal(expectedNode))
	})

	It("should process spec entries and orphan statuses", func() {
		const (
			mod0Name = "mod0"
			mod1Name = "mod1"
			mod2Name = "mod2"
		)
		var (
			loaded   []types.NamespacedName
			unloaded []types.NamespacedName
			err      error
			node     v1.Node
		)
		spec0 := kmmv1beta1.NodeModuleSpec{
			ModuleItem: kmmv1beta1.ModuleItem{
				Namespace: namespace,
				Name:      mod0Name,
			},
		}

		spec1 := kmmv1beta1.NodeModuleSpec{
			ModuleItem: kmmv1beta1.ModuleItem{
				Namespace: namespace,
				Name:      mod1Name,
			},
		}

		status0 := kmmv1beta1.NodeModuleStatus{
			ModuleItem: kmmv1beta1.ModuleItem{
				Namespace: namespace,
				Name:      mod0Name,
			},
		}

		status2 := kmmv1beta1.NodeModuleStatus{
			ModuleItem: kmmv1beta1.ModuleItem{
				Namespace: namespace,
				Name:      mod2Name,
			},
		}

		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
			Spec: kmmv1beta1.NodeModulesConfigSpec{
				Modules: []kmmv1beta1.NodeModuleSpec{spec0, spec1},
			},
			Status: kmmv1beta1.NodeModulesConfigStatus{
				Modules: []kmmv1beta1.NodeModuleStatus{status0, status2},
			},
		}

		contextWithValueMatch := gomock.AssignableToTypeOf(
			reflect.TypeOf((*context.Context)(nil)).Elem(),
		)

		gomock.InOrder(
			kubeClient.
				EXPECT().
				Get(ctx, nmcNsn, &kmmv1beta1.NodeModulesConfig{}).
				Do(func(_ context.Context, _ types.NamespacedName, kubeNmc ctrlclient.Object, _ ...ctrlclient.Options) {
					*kubeNmc.(*kmmv1beta1.NodeModulesConfig) = *nmc
				}),
			kubeClient.EXPECT().Get(ctx, types.NamespacedName{Name: nmc.Name}, &node).Return(nil),
			wh.EXPECT().SyncStatus(ctx, nmc, &node),
			nm.EXPECT().IsNodeSchedulable(&node, nil).Return(true),
			wh.EXPECT().ProcessModuleSpec(contextWithValueMatch, nmc, &spec0, &status0, &node),
			nm.EXPECT().IsNodeSchedulable(&node, nil).Return(true),
			wh.EXPECT().ProcessModuleSpec(contextWithValueMatch, nmc, &spec1, nil, &node),
			wh.EXPECT().ProcessUnconfiguredModuleStatus(contextWithValueMatch, nmc, &status2, &node),
			wh.EXPECT().GarbageCollectInUseLabels(ctx, nmc),
			wh.EXPECT().GarbageCollectWorkerPods(ctx, nmc),
			wh.EXPECT().UpdateNodeLabels(ctx, nmc, &node).Return(loaded, unloaded, err),
			wh.EXPECT().RecordEvents(&node, loaded, unloaded),
		)

		Expect(
			r.Reconcile(ctx, req),
		).To(
			BeZero(),
		)
	})

	It("should complete all the reconcile functions and return combined error", func() {
		const (
			errorMeassge = "some error"
			mod0Name     = "mod0"
			mod1Name     = "mod1"
			mod2Name     = "mod2"
		)
		var (
			node v1.Node
			err  error
		)

		expectedErrors := []error{
			fmt.Errorf("error processing Module %s: %v", namespace+"/"+mod0Name, errorMeassge),
			fmt.Errorf("error processing orphan status for Module %s: %v", namespace+"/"+mod2Name, errorMeassge),
			fmt.Errorf("failed to GC in-use labels for NMC %s: %v", types.NamespacedName{Name: nmcName}, errorMeassge),
			fmt.Errorf("failed to GC orphan worker pods for NMC %s: %v", types.NamespacedName{Name: nmcName}, errorMeassge),
			fmt.Errorf("could not update node's labels for NMC %s: %v", types.NamespacedName{Name: nmcName}, errorMeassge),
		}

		spec0 := kmmv1beta1.NodeModuleSpec{
			ModuleItem: kmmv1beta1.ModuleItem{
				Namespace: namespace,
				Name:      mod0Name,
			},
		}

		status0 := kmmv1beta1.NodeModuleStatus{
			ModuleItem: kmmv1beta1.ModuleItem{
				Namespace: namespace,
				Name:      mod0Name,
			},
		}

		status2 := kmmv1beta1.NodeModuleStatus{
			ModuleItem: kmmv1beta1.ModuleItem{
				Namespace: namespace,
				Name:      mod2Name,
			},
		}

		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
			Spec: kmmv1beta1.NodeModulesConfigSpec{
				Modules: []kmmv1beta1.NodeModuleSpec{spec0},
			},
			Status: kmmv1beta1.NodeModulesConfigStatus{
				Modules: []kmmv1beta1.NodeModuleStatus{status0, status2},
			},
		}

		contextWithValueMatch := gomock.AssignableToTypeOf(
			reflect.TypeOf((*context.Context)(nil)).Elem(),
		)

		gomock.InOrder(
			kubeClient.
				EXPECT().
				Get(ctx, nmcNsn, &kmmv1beta1.NodeModulesConfig{}).
				Do(func(_ context.Context, _ types.NamespacedName, kubeNmc ctrlclient.Object, _ ...ctrlclient.Options) {
					*kubeNmc.(*kmmv1beta1.NodeModulesConfig) = *nmc
				}),
			kubeClient.EXPECT().Get(ctx, types.NamespacedName{Name: nmc.Name}, &node).Return(nil),
			wh.EXPECT().SyncStatus(ctx, nmc, &node).Return(nil),
			nm.EXPECT().IsNodeSchedulable(&node, nil).Return(true),
			wh.EXPECT().ProcessModuleSpec(contextWithValueMatch, nmc, &spec0, &status0, &node).Return(errors.New(errorMeassge)),
			wh.EXPECT().ProcessUnconfiguredModuleStatus(contextWithValueMatch, nmc, &status2, &node).Return(errors.New(errorMeassge)),
			wh.EXPECT().GarbageCollectInUseLabels(ctx, nmc).Return(errors.New(errorMeassge)),
			wh.EXPECT().GarbageCollectWorkerPods(ctx, nmc).Return(errors.New(errorMeassge)),
			wh.EXPECT().UpdateNodeLabels(ctx, nmc, &node).Return(nil, nil, errors.New(errorMeassge)),
		)

		_, err = r.Reconcile(ctx, req)
		Expect(err).To(Equal(errors.Join(expectedErrors...)))
	})
})

var _ = Describe("nmcReconcilerHelperImpl_GarbageCollectWorkerPods", func() {
	var (
		ctx    = context.TODO()
		client *testclient.MockClient
		pm     *pod.MockWorkerPodManager
		nrh    nmcReconcilerHelper
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		client = testclient.NewMockClient(ctrl)
		pm = pod.NewMockWorkerPodManager(ctrl)
		nrh = newNMCReconcilerHelper(client, pm, nil, nil)
	})

	It("should delete orphaned worker pod", func() {
		nmcSpec := kmmv1beta1.NodeModulesConfigSpec{
			Modules: []kmmv1beta1.NodeModuleSpec{
				{
					ModuleItem: kmmv1beta1.ModuleItem{Name: nameSecond},
				},
			},
		}
		nmcObj := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
			Spec:       nmcSpec,
		}
		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("kmm-worker-%s-%s", nmcObj.Name, nameFirst),
				Namespace: "d",
				Labels: map[string]string{
					constants.ModuleNameLabel: nameFirst,
				},
				Finalizers: []string{pod.NodeModulesConfigFinalizer},
			},
		}

		gomock.InOrder(
			pm.EXPECT().ListWorkerPodsOnNode(ctx, nmcName).Return([]v1.Pod{pod}, nil),
			client.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Return(nil),
			pm.EXPECT().DeletePod(ctx, gomock.Any()).Return(nil),
		)

		Expect(
			nrh.GarbageCollectWorkerPods(ctx, nmcObj),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should do nothing because there are not orphaned worker pods", func() {
		nmcSpec := kmmv1beta1.NodeModulesConfigSpec{
			Modules: []kmmv1beta1.NodeModuleSpec{
				{
					ModuleItem: kmmv1beta1.ModuleItem{Name: nameFirst},
				},
			},
		}
		nmcObj := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
			Spec:       nmcSpec,
		}
		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("kmm-worker-%s-%s", nmcObj.Name, nameFirst),
				Namespace: "d",
				Labels: map[string]string{
					constants.ModuleNameLabel: nameFirst,
				},
			},
		}

		gomock.InOrder(
			pm.EXPECT().ListWorkerPodsOnNode(ctx, nmcName).Return([]v1.Pod{pod}, nil),
		)

		Expect(
			nrh.GarbageCollectWorkerPods(ctx, nmcObj),
		).NotTo(
			HaveOccurred(),
		)
	})
})

var _ = Describe("nmcReconcilerHelperImpl_GarbageCollectInUseLabels", func() {
	var (
		ctx = context.TODO()

		client               *testclient.MockClient
		mockWorkerPodManager *pod.MockWorkerPodManager
		wh                   nmcReconcilerHelper
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		client = testclient.NewMockClient(ctrl)
		mockWorkerPodManager = pod.NewMockWorkerPodManager(ctrl)
		wh = newNMCReconcilerHelper(client, mockWorkerPodManager, nil, nil)
	})

	It("should do nothing if no labels should be collected", func() {
		nmcObj := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
			Status: kmmv1beta1.NodeModulesConfigStatus{
				Modules: make([]kmmv1beta1.NodeModuleStatus, 0),
			},
		}

		gomock.InOrder(
			mockWorkerPodManager.EXPECT().ListWorkerPodsOnNode(ctx, nmcName),
		)

		Expect(
			wh.GarbageCollectInUseLabels(ctx, nmcObj),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should work as expected", func() {
		bInUse := nmc.ModuleInUseLabel("b", "b")
		cInUse := nmc.ModuleInUseLabel("c", "c")
		dInUse := nmc.ModuleInUseLabel("d", "d")

		nmcObj := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name: nmcName,
				Labels: map[string]string{
					nmc.ModuleInUseLabel("a", "a"): "",
					bInUse:                         "",
					cInUse:                         "",
					dInUse:                         "",
				},
			},
			Spec: kmmv1beta1.NodeModulesConfigSpec{
				Modules: []kmmv1beta1.NodeModuleSpec{
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Namespace: "b",
							Name:      "b",
						},
					},
				},
			},
			Status: kmmv1beta1.NodeModulesConfigStatus{
				Modules: []kmmv1beta1.NodeModuleStatus{
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Namespace: "c",
							Name:      "c",
						},
					},
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Namespace: "d",
							Name:      "d",
						},
					},
				},
			},
		}

		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					constants.ModuleNameLabel: "d",
				},
				Namespace: "d",
			},
		}

		gomock.InOrder(
			mockWorkerPodManager.EXPECT().ListWorkerPodsOnNode(ctx, nmcName).Return([]v1.Pod{pod}, nil),
			client.EXPECT().Patch(ctx, nmcObj, gomock.Any()),
		)

		Expect(
			wh.GarbageCollectInUseLabels(ctx, nmcObj),
		).NotTo(
			HaveOccurred(),
		)

		Expect(nmcObj.Labels).To(
			Equal(
				map[string]string{
					bInUse: "",
					cInUse: "",
					dInUse: "",
				},
			),
		)
	})
})

var _ = Describe("nmcReconcilerHelperImpl_ProcessModuleSpec", func() {
	const (
		name      = "name"
		namespace = "namespace"
	)

	var (
		ctx     = context.TODO()
		podName = pod.WorkerPodName(nmcName, name)

		client               *testclient.MockClient
		mockWorkerPodManager *pod.MockWorkerPodManager
		wh                   nmcReconcilerHelper
		nm                   *node.MockNode

		moduleConfig = kmmv1beta1.ModuleConfig{
			KernelVersion:         "kernel-version",
			ContainerImage:        "container image",
			ImagePullPolicy:       v1.PullIfNotPresent,
			InsecurePull:          true,
			InTreeModulesToRemove: []string{"intree1", "intree2"},
			Modprobe: kmmv1beta1.ModprobeSpec{
				ModuleName:          "test",
				Parameters:          []string{"a", "b"},
				DirName:             "/dir",
				Args:                nil,
				RawArgs:             nil,
				ModulesLoadingOrder: []string{"a", "b", "c"},
			},
		}
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		client = testclient.NewMockClient(ctrl)
		mockWorkerPodManager = pod.NewMockWorkerPodManager(ctrl)
		nm = node.NewMockNode(ctrl)
		wh = newNMCReconcilerHelper(client, mockWorkerPodManager, nil, nm)
	})

	It("should create a loader Pod if there is no existing Pod and the status is missing", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}
		spec := &kmmv1beta1.NodeModuleSpec{
			ModuleItem: kmmv1beta1.ModuleItem{
				Name:      name,
				Namespace: namespace,
			},
		}

		gomock.InOrder(
			mockWorkerPodManager.EXPECT().GetWorkerPod(ctx, podName, namespace),
			mockWorkerPodManager.EXPECT().CreateLoaderPod(ctx, nmc, spec),
		)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, nil, nil),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should create an unloader Pod if the spec is different from the status and kernels are equal", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}

		spec := &kmmv1beta1.NodeModuleSpec{
			ModuleItem: kmmv1beta1.ModuleItem{
				Name:      name,
				Namespace: namespace,
			},
			Config: kmmv1beta1.ModuleConfig{ContainerImage: "old-container-image", KernelVersion: "same kernel"},
		}

		status := &kmmv1beta1.NodeModuleStatus{
			ModuleItem: kmmv1beta1.ModuleItem{
				Name:      name,
				Namespace: namespace,
			},
			Config: kmmv1beta1.ModuleConfig{ContainerImage: "new-container-image", KernelVersion: "same kernel"},
		}

		gomock.InOrder(
			mockWorkerPodManager.EXPECT().GetWorkerPod(ctx, podName, namespace),
			mockWorkerPodManager.EXPECT().CreateUnloaderPod(ctx, nmc, status),
		)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, status, nil),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should create an loader Pod if the spec is different from the status and kernels different equal", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}

		spec := &kmmv1beta1.NodeModuleSpec{
			ModuleItem: kmmv1beta1.ModuleItem{
				Name:      name,
				Namespace: namespace,
			},
			Config: kmmv1beta1.ModuleConfig{ContainerImage: "old-container-image", KernelVersion: "old kernel"},
		}

		status := &kmmv1beta1.NodeModuleStatus{
			ModuleItem: kmmv1beta1.ModuleItem{
				Name:      name,
				Namespace: namespace,
			},
			Config: kmmv1beta1.ModuleConfig{ContainerImage: "new-container-image", KernelVersion: "new kernel"},
		}

		gomock.InOrder(
			mockWorkerPodManager.EXPECT().GetWorkerPod(ctx, podName, namespace),
			mockWorkerPodManager.EXPECT().CreateLoaderPod(ctx, nmc, spec),
		)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, status, nil),
		).NotTo(
			HaveOccurred(),
		)
	})

	nmc := &kmmv1beta1.NodeModulesConfig{
		ObjectMeta: metav1.ObjectMeta{Name: nmcName},
	}

	spec := &kmmv1beta1.NodeModuleSpec{
		Config: moduleConfig,
		ModuleItem: kmmv1beta1.ModuleItem{
			Name:      name,
			Namespace: namespace,
		},
	}
	node := &v1.Node{}

	status := &kmmv1beta1.NodeModuleStatus{
		Config: moduleConfig,
		ModuleItem: kmmv1beta1.ModuleItem{
			Name:      name,
			Namespace: namespace,
		},
	}

	DescribeTable(
		"should create a loader Pod if status is older than the Ready condition",
		func(shouldCreate bool) {

			returnValue := false
			if shouldCreate {
				returnValue = true
			}

			gomock.InOrder(
				mockWorkerPodManager.EXPECT().GetWorkerPod(ctx, podName, namespace),
				nm.EXPECT().IsNodeRebooted(node, status.BootId).Return(returnValue),
			)

			if shouldCreate {
				mockWorkerPodManager.EXPECT().CreateLoaderPod(ctx, nmc, spec)
			}

			Expect(
				wh.ProcessModuleSpec(ctx, nmc, spec, status, node),
			).NotTo(
				HaveOccurred(),
			)
		},
		Entry("pod status is newer then node's Ready condition, worker pod should not be created", false),
		Entry("pod status is older then node's Ready condition, worker pod should be created", true),
	)

	It("should do nothing if the pod is not loading a kmod", func() {

		gomock.InOrder(
			mockWorkerPodManager.EXPECT().GetWorkerPod(ctx, podName, namespace).Return(&v1.Pod{}, nil),
			mockWorkerPodManager.EXPECT().IsLoaderPod(gomock.Any()).Return(true),
		)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, status, nil),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should do nothing if the worker container has not restarted", func() {

		gomock.InOrder(
			mockWorkerPodManager.EXPECT().GetWorkerPod(ctx, podName, namespace).Return(&v1.Pod{}, nil),
			mockWorkerPodManager.EXPECT().IsLoaderPod(gomock.Any()).Return(true),
		)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, status, nil),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should return an error if there was an error making the Pod template", func() {
		pod := v1.Pod{
			Status: v1.PodStatus{
				ContainerStatuses: []v1.ContainerStatus{
					{
						Name:         pod.WorkerContainerName,
						RestartCount: 1,
					},
				},
			},
		}

		gomock.InOrder(
			mockWorkerPodManager.EXPECT().GetWorkerPod(ctx, podName, namespace).Return(&pod, nil),
			mockWorkerPodManager.EXPECT().IsLoaderPod(&pod).Return(true),
			mockWorkerPodManager.EXPECT().LoaderPodTemplate(ctx, nmc, spec).Return(nil, errors.New("random error")),
		)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, status, nil),
		).To(
			HaveOccurred(),
		)
	})

	It("should delete the existing pod if its hash annotation is outdated", func() {
		p := v1.Pod{
			Status: v1.PodStatus{
				ContainerStatuses: []v1.ContainerStatus{
					{
						Name:         pod.WorkerContainerName,
						RestartCount: 1,
					},
				},
			},
		}

		podTemplate := p.DeepCopy()

		gomock.InOrder(
			mockWorkerPodManager.EXPECT().GetWorkerPod(ctx, podName, namespace).Return(&p, nil),
			mockWorkerPodManager.EXPECT().IsLoaderPod(&p).Return(true),
			mockWorkerPodManager.EXPECT().LoaderPodTemplate(ctx, nmc, spec).Return(podTemplate, nil),
			mockWorkerPodManager.EXPECT().HashAnnotationDiffer(gomock.Any(), gomock.Any()).Return(true),
			mockWorkerPodManager.EXPECT().DeletePod(ctx, &p),
		)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, status, nil),
		).NotTo(
			HaveOccurred(),
		)
	})
})

var _ = Describe("nmcReconcilerHelperImpl_ProcessUnconfiguredModuleStatus", func() {
	const name = "name"

	var (
		ctx     = context.TODO()
		podName = pod.WorkerPodName(nmcName, name)

		client *testclient.MockClient
		sw     *testclient.MockStatusWriter

		mockWorkerPodManager *pod.MockWorkerPodManager
		nm                   *node.MockNode
		helper               nmcReconcilerHelper
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		client = testclient.NewMockClient(ctrl)
		sw = testclient.NewMockStatusWriter(ctrl)
		mockWorkerPodManager = pod.NewMockWorkerPodManager(ctrl)
		nm = node.NewMockNode(ctrl)
		helper = newNMCReconcilerHelper(client, mockWorkerPodManager, nil, nm)
	})

	nmc := &kmmv1beta1.NodeModulesConfig{
		ObjectMeta: metav1.ObjectMeta{Name: nmcName},
	}

	status := &kmmv1beta1.NodeModuleStatus{
		ModuleItem: kmmv1beta1.ModuleItem{
			Name:      name,
			Namespace: namespace,
		},
	}

	node := v1.Node{}

	It("should do nothing , if the node has been rebooted/ready lately", func() {
		gomock.InOrder(
			nm.EXPECT().IsNodeRebooted(&node, status.BootId).Return(true),
			client.EXPECT().Status().Return(sw),
			sw.EXPECT().Patch(ctx, nmc, gomock.Any()),
		)

		Expect(
			helper.ProcessUnconfiguredModuleStatus(ctx, nmc, status, &node),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should create an unloader Pod if no worker Pod exists", func() {
		gomock.InOrder(
			nm.EXPECT().IsNodeRebooted(&node, status.BootId).Return(false),
			mockWorkerPodManager.EXPECT().GetWorkerPod(ctx, podName, namespace),
			mockWorkerPodManager.EXPECT().CreateUnloaderPod(ctx, nmc, status),
		)

		Expect(
			helper.ProcessUnconfiguredModuleStatus(ctx, nmc, status, &node),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should delete the current worker if it is loading a module", func() {
		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
			},
		}

		gomock.InOrder(
			nm.EXPECT().IsNodeRebooted(&node, status.BootId).Return(false),
			mockWorkerPodManager.EXPECT().GetWorkerPod(ctx, podName, namespace).Return(&pod, nil),
			mockWorkerPodManager.EXPECT().IsLoaderPod(&pod).Return(true),
			mockWorkerPodManager.EXPECT().DeletePod(ctx, &pod),
		)

		Expect(
			helper.ProcessUnconfiguredModuleStatus(ctx, nmc, status, &node),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should do nothing if the pod has not restarted yet", func() {
		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
			},
		}

		gomock.InOrder(
			nm.EXPECT().IsNodeRebooted(&node, status.BootId).Return(false),
			mockWorkerPodManager.EXPECT().GetWorkerPod(ctx, podName, namespace).Return(&pod, nil),
			mockWorkerPodManager.EXPECT().IsLoaderPod(&pod).Return(false),
		)

		Expect(
			helper.ProcessUnconfiguredModuleStatus(ctx, nmc, status, &node),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should return an error if there was an error while making the unloader Pod", func() {
		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
			},
			Status: v1.PodStatus{
				ContainerStatuses: []v1.ContainerStatus{
					{
						Name:         pod.WorkerContainerName,
						RestartCount: 1,
					},
				},
			},
		}

		gomock.InOrder(
			nm.EXPECT().IsNodeRebooted(&node, status.BootId).Return(false),
			mockWorkerPodManager.EXPECT().GetWorkerPod(ctx, podName, namespace).Return(&pod, nil),
			mockWorkerPodManager.EXPECT().IsLoaderPod(&pod).Return(false),
			mockWorkerPodManager.EXPECT().UnloaderPodTemplate(ctx, nmc, status).Return(nil, errors.New("random error")),
		)

		Expect(
			helper.ProcessUnconfiguredModuleStatus(ctx, nmc, status, &node),
		).To(
			HaveOccurred(),
		)
	})

	It("should delete the existing pod if its hash annotation is outdated", func() {
		p := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
			},
			Status: v1.PodStatus{
				ContainerStatuses: []v1.ContainerStatus{
					{
						Name:         pod.WorkerContainerName,
						RestartCount: 1,
					},
				},
			},
		}

		podTemplate := p.DeepCopy()

		gomock.InOrder(
			nm.EXPECT().IsNodeRebooted(&node, status.BootId).Return(false),
			mockWorkerPodManager.EXPECT().GetWorkerPod(ctx, podName, namespace).Return(&p, nil),
			mockWorkerPodManager.EXPECT().IsLoaderPod(&p).Return(false),
			mockWorkerPodManager.EXPECT().UnloaderPodTemplate(ctx, nmc, status).Return(podTemplate, nil),
			mockWorkerPodManager.EXPECT().HashAnnotationDiffer(gomock.Any(), gomock.Any()).Return(true),
			mockWorkerPodManager.EXPECT().DeletePod(ctx, &p),
		)

		Expect(
			helper.ProcessUnconfiguredModuleStatus(ctx, nmc, status, &node),
		).NotTo(
			HaveOccurred(),
		)
	})

})

var _ = Describe("nmcReconcilerHelperImpl_SyncStatus", func() {
	var (
		ctx = context.TODO()

		ctrl                 *gomock.Controller
		kubeClient           *testclient.MockClient
		mockWorkerPodManager *pod.MockWorkerPodManager
		sw                   *testclient.MockStatusWriter
		wh                   nmcReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		kubeClient = testclient.NewMockClient(ctrl)
		mockWorkerPodManager = pod.NewMockWorkerPodManager(ctrl)
		wh = newNMCReconcilerHelper(kubeClient, mockWorkerPodManager, nil, nil)
		sw = testclient.NewMockStatusWriter(ctrl)
	})

	const (
		podName      = "pod-name"
		podNamespace = "pod-namespace"
	)

	It("should do nothing if there are no running pods for this NMC", func() {
		mockWorkerPodManager.EXPECT().ListWorkerPodsOnNode(ctx, nmcName)

		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}
		node := v1.Node{}
		Expect(
			wh.SyncStatus(ctx, nmc, &node),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("failed pods", func() {
		podWithStatus := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: podNamespace,
				Name:      podName,
				Labels: map[string]string{
					constants.ModuleNameLabel: "mod name 1",
				},
			},
			Status: v1.PodStatus{Phase: v1.PodFailed},
		}

		podWithoutStatus := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: podNamespace,
				Name:      podName,
				Labels: map[string]string{
					constants.ModuleNameLabel: "mod name 2",
				},
			},
			Status: v1.PodStatus{Phase: v1.PodFailed},
		}

		pods := []v1.Pod{podWithStatus, podWithoutStatus}

		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
			Status: kmmv1beta1.NodeModulesConfigStatus{
				Modules: []kmmv1beta1.NodeModuleStatus{
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Name:      "mod name 1",
							Namespace: podNamespace,
						},
						Config: kmmv1beta1.ModuleConfig{ContainerImage: "some image"},
					},
				},
			},
		}

		gomock.InOrder(
			mockWorkerPodManager.EXPECT().ListWorkerPodsOnNode(ctx, nmcName).Return(pods, nil),
			kubeClient.EXPECT().Status().Return(sw),
			sw.EXPECT().Patch(ctx, nmc, gomock.Any()),
			mockWorkerPodManager.EXPECT().DeletePod(ctx, &podWithStatus),
			mockWorkerPodManager.EXPECT().DeletePod(ctx, &podWithoutStatus),
		)
		node := v1.Node{}
		Expect(
			wh.SyncStatus(ctx, nmc, &node),
		).NotTo(
			HaveOccurred(),
		)

		Expect(nmc.Status.Modules).To(HaveLen(1))
	})

	It("should remove the status and label if an unloader pod was successful", func() {
		const (
			modName      = "module"
			modNamespace = "namespace"
		)

		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
			Status: kmmv1beta1.NodeModulesConfigStatus{
				Modules: []kmmv1beta1.NodeModuleStatus{
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Name:      modName,
							Namespace: modNamespace,
						},
					},
				},
			},
		}

		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: modNamespace,
				Labels: map[string]string{
					constants.ModuleNameLabel: modName,
				},
			},
			Status: v1.PodStatus{Phase: v1.PodSucceeded},
		}

		gomock.InOrder(
			mockWorkerPodManager.EXPECT().ListWorkerPodsOnNode(ctx, nmcName).Return([]v1.Pod{pod}, nil),
			mockWorkerPodManager.EXPECT().IsUnloaderPod(&pod).Return(true),
			kubeClient.EXPECT().Status().Return(sw),
			sw.EXPECT().Patch(ctx, nmc, gomock.Any()),
			mockWorkerPodManager.EXPECT().DeletePod(ctx, &pod),
		)
		node := v1.Node{}
		Expect(
			wh.SyncStatus(ctx, nmc, &node),
		).NotTo(
			HaveOccurred(),
		)

		Expect(nmc.Status.Modules).To(BeEmpty())
	})

	It("should add the status if a loader pod was successful", func() {
		const (
			irsName            = "some-secret"
			modName            = "module"
			modNamespace       = "namespace"
			serviceAccountName = "some-sa"
		)

		testToleration := v1.Toleration{
			Key:    "test-key",
			Value:  "test-value",
			Effect: v1.TaintEffectNoExecute,
		}

		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
			Status: kmmv1beta1.NodeModulesConfigStatus{
				Modules: []kmmv1beta1.NodeModuleStatus{
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Name:      modName,
							Namespace: modNamespace,
						},
					},
				},
			},
		}

		now := metav1.Now()

		cfg := kmmv1beta1.ModuleConfig{
			KernelVersion:         "some-kernel-version",
			ContainerImage:        "some-container-image",
			InsecurePull:          true,
			InTreeModulesToRemove: []string{"intree1", "intree2"},
			Modprobe:              kmmv1beta1.ModprobeSpec{ModuleName: "test"},
		}

		p := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: modNamespace,
				Labels: map[string]string{
					constants.ModuleNameLabel: modName,
				},
			},
			Spec: v1.PodSpec{
				ServiceAccountName: serviceAccountName,
				ImagePullSecrets:   []v1.LocalObjectReference{v1.LocalObjectReference{Name: irsName}},
				Tolerations:        []v1.Toleration{testToleration},
			},
			Status: v1.PodStatus{
				Phase: v1.PodSucceeded,
				ContainerStatuses: []v1.ContainerStatus{
					{
						Name: "worker",
						State: v1.ContainerState{
							Terminated: &v1.ContainerStateTerminated{FinishedAt: now},
						},
					},
				},
			},
		}

		b, err := yaml.Marshal(cfg)
		Expect(err).NotTo(HaveOccurred())

		tolerations, err := yaml.Marshal(p.Spec.Tolerations)
		Expect(err).NotTo(HaveOccurred())

		gomock.InOrder(
			mockWorkerPodManager.EXPECT().ListWorkerPodsOnNode(ctx, nmcName).Return([]v1.Pod{p}, nil),
			mockWorkerPodManager.EXPECT().IsUnloaderPod(&p).Return(false),
			mockWorkerPodManager.EXPECT().GetConfigAnnotation(&p).Return(string(b)),
			mockWorkerPodManager.EXPECT().GetTolerationsAnnotation(&p).Return(string(tolerations)),
			mockWorkerPodManager.EXPECT().GetModuleVersionAnnotation(&p).Return("some version"),
			kubeClient.EXPECT().Status().Return(sw),
			sw.EXPECT().Patch(ctx, nmc, gomock.Any()),
			mockWorkerPodManager.EXPECT().DeletePod(ctx, &p),
		)
		node := v1.Node{}
		Expect(
			wh.SyncStatus(ctx, nmc, &node),
		).NotTo(
			HaveOccurred(),
		)

		Expect(nmc.Status.Modules).To(HaveLen(1))

		expectedStatus := kmmv1beta1.NodeModuleStatus{
			ModuleItem: kmmv1beta1.ModuleItem{
				ImageRepoSecret:    &v1.LocalObjectReference{Name: irsName},
				Name:               modName,
				Namespace:          modNamespace,
				ServiceAccountName: serviceAccountName,
				Tolerations:        []v1.Toleration{testToleration},
				Version:            "some version",
			},
			Config: cfg,
		}

		Expect(nmc.Status.Modules[0]).To(BeComparableTo(expectedStatus))
	})

	It("pod should not be deleted if NMC patch failed", func() {
		const (
			modName      = "module"
			modNamespace = "namespace"
		)

		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
			Status: kmmv1beta1.NodeModulesConfigStatus{
				Modules: []kmmv1beta1.NodeModuleStatus{
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Name:      modName,
							Namespace: modNamespace,
						},
					},
				},
			},
		}

		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: modNamespace,
				Labels: map[string]string{
					constants.ModuleNameLabel: modName,
				},
			},
			Status: v1.PodStatus{Phase: v1.PodSucceeded},
		}

		gomock.InOrder(
			mockWorkerPodManager.EXPECT().ListWorkerPodsOnNode(ctx, nmcName).Return([]v1.Pod{pod}, nil),
			mockWorkerPodManager.EXPECT().IsUnloaderPod(&pod).Return(true),
			kubeClient.EXPECT().Status().Return(sw),
			sw.EXPECT().Patch(ctx, nmc, gomock.Any()).Return(errors.New("some error")),
		)
		node := v1.Node{}
		Expect(
			wh.SyncStatus(ctx, nmc, &node),
		).To(
			HaveOccurred(),
		)
	})
})

var _ = Describe("nmcReconcilerHelperImpl_RemovePodFinalizers", func() {
	const nodeName = "node-name"

	var (
		ctx = context.TODO()

		kubeClient           *testclient.MockClient
		mockWorkerPodManager *pod.MockWorkerPodManager
		wh                   nmcReconcilerHelper
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = testclient.NewMockClient(ctrl)
		mockWorkerPodManager = pod.NewMockWorkerPodManager(ctrl)
		wh = newNMCReconcilerHelper(kubeClient, mockWorkerPodManager, nil, nil)
	})

	It("should do nothing if no pods are present", func() {
		mockWorkerPodManager.EXPECT().ListWorkerPodsOnNode(ctx, nodeName)

		Expect(
			wh.RemovePodFinalizers(ctx, nodeName),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should patch to remove the finalizer if it is set", func() {
		const (
			name      = "my-pod"
			namespace = "my-namespace"
		)

		podWithFinalizer := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:       name,
				Namespace:  namespace,
				Finalizers: []string{pod.NodeModulesConfigFinalizer},
			},
		}

		podWithoutFinalizer := podWithFinalizer
		podWithoutFinalizer.Finalizers = []string{}

		gomock.InOrder(
			mockWorkerPodManager.EXPECT().ListWorkerPodsOnNode(ctx, nodeName).Return([]v1.Pod{podWithFinalizer, {}}, nil),
			kubeClient.EXPECT().Patch(ctx, &podWithoutFinalizer, gomock.Any()),
		)

		Expect(
			wh.RemovePodFinalizers(ctx, nodeName),
		).NotTo(
			HaveOccurred(),
		)
	})
})

const (
	moduleName      = "my-module"
	moduleNamespace = "my-module-namespace"
	nodeName        = "node-name"
)

var kernelModuleLabelName = utils.GetKernelModuleReadyNodeLabel(moduleNamespace, moduleName)

var _ = Describe("nmcReconcilerHelperImpl_UpdateNodeLabels", func() {
	var (
		ctx                    context.Context
		client                 *testclient.MockClient
		fakeRecorder           *record.FakeRecorder
		n                      *node.MockNode
		nmc                    kmmv1beta1.NodeModulesConfig
		wh                     nmcReconcilerHelper
		mlph                   *MocklabelPreparationHelper
		firstLabelName         string
		secondLabelName        string
		firstVersionLabelName  string
		secondVersionLabelName string
	)

	BeforeEach(func() {
		ctx = context.TODO()
		ctrl := gomock.NewController(GinkgoT())
		client = testclient.NewMockClient(ctrl)
		nmc = kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "nmcName"},
			Spec: kmmv1beta1.NodeModulesConfigSpec{
				Modules: []kmmv1beta1.NodeModuleSpec{
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Namespace: nsFirst,
							Name:      nameFirst,
						},
						Config: kmmv1beta1.ModuleConfig{},
					},
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Namespace: nsSecond,
							Name:      nameSecond,
						},
						Config: kmmv1beta1.ModuleConfig{},
					},
				},
			},
		}
		fakeRecorder = record.NewFakeRecorder(10)
		n = node.NewMockNode(ctrl)
		wh = newNMCReconcilerHelper(client, nil, fakeRecorder, n)
		mlph = NewMocklabelPreparationHelper(ctrl)
		wh = &nmcReconcilerHelperImpl{
			client:     client,
			podManager: nil,
			recorder:   fakeRecorder,
			nodeAPI:    n,
			lph:        mlph,
		}
		firstLabelName = fmt.Sprintf("kmm.node.kubernetes.io/%s.%s.ready", nsFirst, nameFirst)
		secondLabelName = fmt.Sprintf("kmm.node.kubernetes.io/%s.%s.ready", nsSecond, nameSecond)
		firstVersionLabelName = fmt.Sprintf("kmm.node.kubernetes.io/%s.%s.version.ready", nsFirst, nameFirst)
		secondVersionLabelName = fmt.Sprintf("kmm.node.kubernetes.io/%s.%s.version.ready", nsSecond, nameSecond)
	})

	It("failed to get node", func() {
		client.EXPECT().Get(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))
		err := client.Get(ctx, types.NamespacedName{Name: nmc.Name}, &v1.Node{})
		Expect(err).To(HaveOccurred())
	})
	It("Should fail patching node after change in labels", func() {
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					firstLabelName: "",
				},
				Name: nodeName,
			},
		}
		emptySet := sets.Set[types.NamespacedName]{}
		emptyMap := map[types.NamespacedName]kmmv1beta1.ModuleConfig{}

		gomock.InOrder(
			mlph.EXPECT().getNodeKernelModuleReadyLabels(node).Return(emptySet),
			mlph.EXPECT().getDeprecatedKernelModuleReadyLabels(node).Return(map[string]string{}),
			mlph.EXPECT().getSpecLabelsAndTheirConfigs(&nmc).Return(emptyMap),
			mlph.EXPECT().getStatusLabelsAndTheirConfigs(&nmc).Return(emptyMap),
			mlph.EXPECT().getStatusVersions(&nmc).Return(map[types.NamespacedName]string{}),
			mlph.EXPECT().removeOrphanedLabels(emptySet, emptyMap, emptyMap).Return([]types.NamespacedName{{Name: nameSecond, Namespace: nsSecond}}),
			mlph.EXPECT().addEqualLabels(emptySet, emptyMap, emptyMap).Return([]types.NamespacedName{{Name: nameFirst, Namespace: nsFirst}}),
			n.EXPECT().
				UpdateLabels(
					ctx,
					&node,
					map[string]string{firstLabelName: ""},
					map[string]string{secondLabelName: "", secondVersionLabelName: ""},
				).Return(fmt.Errorf("some error")),
		)
		_, _, err := wh.UpdateNodeLabels(ctx, &nmc, &node)
		Expect(err).To(HaveOccurred())
	})
	It("Should work as expected", func() {
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					firstLabelName: "",
				},
				Name: nodeName,
			},
		}
		emptySet := sets.Set[types.NamespacedName]{}
		emptyMap := map[types.NamespacedName]kmmv1beta1.ModuleConfig{}
		firstNN := types.NamespacedName{Name: nameFirst, Namespace: nsFirst}
		secondNN := types.NamespacedName{Name: nameSecond, Namespace: nsSecond}

		gomock.InOrder(
			mlph.EXPECT().getNodeKernelModuleReadyLabels(node).Return(emptySet),
			mlph.EXPECT().getDeprecatedKernelModuleReadyLabels(node).Return(map[string]string{}),
			mlph.EXPECT().getSpecLabelsAndTheirConfigs(&nmc).Return(emptyMap),
			mlph.EXPECT().getStatusLabelsAndTheirConfigs(&nmc).Return(emptyMap),
			mlph.EXPECT().getStatusVersions(&nmc).Return(map[types.NamespacedName]string{firstNN: "some version"}),
			mlph.EXPECT().removeOrphanedLabels(emptySet, emptyMap, emptyMap).Return([]types.NamespacedName{secondNN}),
			mlph.EXPECT().addEqualLabels(emptySet, emptyMap, emptyMap).Return([]types.NamespacedName{firstNN}),
			n.EXPECT().
				UpdateLabels(
					ctx,
					&node,
					map[string]string{firstLabelName: "", firstVersionLabelName: "some version"},
					map[string]string{secondLabelName: "", secondVersionLabelName: ""},
				).Return(nil),
		)
		_, _, err := wh.UpdateNodeLabels(ctx, &nmc, &node)
		Expect(err).ToNot(HaveOccurred())
		Expect(node.Labels).To(HaveKey(firstLabelName))

	})
})

var _ = Describe("nmcReconcilerHelperImpl_RecordEvents", func() {
	var (
		client       *testclient.MockClient
		fakeRecorder *record.FakeRecorder
		//nm           *node.MockNode
		wh nmcReconcilerHelper
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		client = testclient.NewMockClient(ctrl)
		//nm = node.NewMockNode(ctrl)
		fakeRecorder = record.NewFakeRecorder(10)
		wh = newNMCReconcilerHelper(client, nil, fakeRecorder, nil)
	})

	closeAndGetAllEvents := func(events chan string) []string {
		elems := make([]string, 0)

		close(events)

		for s := range events {
			elems = append(elems, s)
		}

		return elems
	}

	_ = closeAndGetAllEvents(make(chan string))

	type testCase struct {
		nodeLabelPresent    bool
		specPresent         bool
		statusPresent       bool
		statusConfigPresent bool
		configsEqual        bool
		resultLabelPresent  bool
		addsReadyLabel      bool
		removesReadyLabel   bool
	}

	DescribeTable(
		"RecordEvents different scenarios",
		func(tc testCase, loaded []types.NamespacedName, unloaded []types.NamespacedName, node v1.Node) {

			wh.RecordEvents(&node, loaded, unloaded)
			events := closeAndGetAllEvents(fakeRecorder.Events)

			if !tc.addsReadyLabel && !tc.removesReadyLabel {
				Expect(events).To(BeEmpty())
				return
			}

			Expect(events).To(HaveLen(1))

			if tc.removesReadyLabel {
				Expect(events[0]).To(ContainSubstring("Normal ModuleUnloaded Module my-module-namespace/my-module unloaded from the kernel"))
			}

			if tc.addsReadyLabel {
				Expect(events[0]).To(ContainSubstring("Normal ModuleLoaded Module my-module-namespace/my-module loaded into the kernel"))
			}
		},
		Entry(
			"node label present, spec missing, status missing",
			testCase{
				nodeLabelPresent:  true,
				removesReadyLabel: true,
			},
			[]types.NamespacedName{},
			[]types.NamespacedName{{Namespace: moduleNamespace, Name: moduleName}},
			v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
					Name:   nodeName,
				},
			},
		),
		Entry(
			"node label present, spec present, status missing",
			testCase{
				nodeLabelPresent:   true,
				specPresent:        true,
				resultLabelPresent: true,
			},
			[]types.NamespacedName{},
			[]types.NamespacedName{},
			v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						kernelModuleLabelName: "",
					},
					Name: nodeName,
				},
			},
		),
		Entry(
			"node label present, spec present, status present, status config missing",
			testCase{
				nodeLabelPresent:   true,
				specPresent:        true,
				statusPresent:      true,
				resultLabelPresent: true,
			},
			[]types.NamespacedName{},
			[]types.NamespacedName{},
			v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						kernelModuleLabelName: "",
					},
					Name: nodeName,
				},
			},
		),
		Entry(
			"node label present, spec present, status present, status config present, configs not equal",
			testCase{
				nodeLabelPresent:    true,
				specPresent:         true,
				statusPresent:       true,
				statusConfigPresent: true,
				resultLabelPresent:  true,
			},
			[]types.NamespacedName{},
			[]types.NamespacedName{},
			v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						kernelModuleLabelName: "",
					},
					Name: nodeName,
				},
			},
		),
		Entry(
			"node label present, spec present, status present, status config present, configs equal",
			testCase{
				nodeLabelPresent:    true,
				specPresent:         true,
				statusPresent:       true,
				statusConfigPresent: true,
				configsEqual:        true,
				resultLabelPresent:  true,
			},
			[]types.NamespacedName{},
			[]types.NamespacedName{},
			v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						kernelModuleLabelName: "",
					},
					Name: nodeName,
				},
			},
		),
		Entry(
			"node label missing, spec missing, status missing",
			testCase{},
			[]types.NamespacedName{},
			[]types.NamespacedName{},
			v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
					Name:   nodeName,
				},
			},
		),
		Entry(
			"node label missing, spec present, status missing",
			testCase{specPresent: true},
			[]types.NamespacedName{},
			[]types.NamespacedName{},
			v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
					Name:   nodeName,
				},
			},
		),
		Entry(
			"node label missing, spec present, status present, status config missing",
			testCase{
				specPresent:   true,
				statusPresent: true,
			},
			[]types.NamespacedName{},
			[]types.NamespacedName{},
			v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
					Name:   nodeName,
				},
			},
		),
		Entry(
			"node label missing, spec present, status present, status config present, configs not equal",
			testCase{
				specPresent:         true,
				statusPresent:       true,
				statusConfigPresent: true,
			},
			[]types.NamespacedName{},
			[]types.NamespacedName{},
			v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{},
					Name:   nodeName,
				},
			},
		),
		Entry(
			"node label missing, spec present, status present, status config present, configs equal",
			testCase{
				specPresent:         true,
				statusPresent:       true,
				statusConfigPresent: true,
				configsEqual:        true,
				resultLabelPresent:  true,
				addsReadyLabel:      true,
			},
			[]types.NamespacedName{{Namespace: moduleNamespace, Name: moduleName}},
			[]types.NamespacedName{},
			v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						kernelModuleLabelName: "",
					},
					Name: nodeName,
				},
			},
		),
	)
})

var _ = Describe("getKernelModuleReadyLabels", func() {
	lph := newLabelPreparationHelper()

	DescribeTable("getKernelModuleReadyLabels different scenarios", func(labels map[string]string,
		nodeModuleReadyLabelsEqual sets.Set[types.NamespacedName]) {
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labels,
			},
		}

		nodeModuleReadyLabels := lph.getNodeKernelModuleReadyLabels(node)

		Expect(nodeModuleReadyLabels).To(Equal(nodeModuleReadyLabelsEqual))
	},
		Entry("Should be empty", map[string]string{},
			sets.Set[types.NamespacedName]{},
		),

		Entry("nodeModuleReadyLabels found", map[string]string{"invalid": ""},
			sets.Set[types.NamespacedName]{},
		),

		Entry("nodeModuleReadyLabels found", map[string]string{fmt.Sprintf("kmm.node.kubernetes.io/%s.%s.ready", nsFirst, nameFirst): ""},
			sets.Set[types.NamespacedName]{
				{Namespace: nsFirst, Name: nameFirst}: {},
			},
		),
	)
})

var _ = Describe("getDeprecatedKernelModuleReadyLabels", func() {

	lph := newLabelPreparationHelper()
	DescribeTable("getDeprecatedKernelModuleReadyLabels different scenarios", func(labels map[string]string,
		deprecatedNodeModuleReadyLabelsEqual map[string]string) {
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labels,
			},
		}

		deprecatedNodeModuleReadyLabels := lph.getDeprecatedKernelModuleReadyLabels(node)

		Expect(deprecatedNodeModuleReadyLabels).To(Equal(deprecatedNodeModuleReadyLabelsEqual))
	},
		Entry("Should be empty", map[string]string{},
			map[string]string{},
		),

		Entry("deprecated node module ready labels not found", map[string]string{"invalid": ""},
			map[string]string{},
		),

		Entry("deprecated node module ready labels found", map[string]string{fmt.Sprintf("kmm.node.kubernetes.io/%s.ready", nameFirst): ""},
			map[string]string{
				fmt.Sprintf("kmm.node.kubernetes.io/%s.ready", nameFirst): "",
			},
		),
	)
})

var _ = Describe("getSpecLabelsAndTheirConfigs", func() {

	var (
		nmc kmmv1beta1.NodeModulesConfig
		lph labelPreparationHelper
	)

	BeforeEach(func() {
		nmc = kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "nmcName"},
			Spec: kmmv1beta1.NodeModulesConfigSpec{
				Modules: []kmmv1beta1.NodeModuleSpec{
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Namespace: nsFirst,
							Name:      nameFirst,
						},
						Config: kmmv1beta1.ModuleConfig{ContainerImage: imageFirst},
					},
				},
			},
		}
		lph = newLabelPreparationHelper()
	})
	It("Should not have module not from nmc", func() {
		specLabels := lph.getSpecLabelsAndTheirConfigs(&nmc)
		Expect(specLabels).ToNot(HaveKey(types.NamespacedName{Namespace: nsSecond, Name: nameSecond}))
	})

	It("Should have module from nmc", func() {
		specLabels := lph.getSpecLabelsAndTheirConfigs(&nmc)
		Expect(specLabels).To(HaveKeyWithValue(types.NamespacedName{Namespace: nsFirst, Name: nameFirst}, kmmv1beta1.ModuleConfig{ContainerImage: imageFirst}))
	})
})

var _ = Describe("getStatusLabelsAndTheirConfigs", func() {

	var (
		nmc kmmv1beta1.NodeModulesConfig
		lph labelPreparationHelper
	)

	BeforeEach(func() {
		nmc = kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "nmcName"},
			Status: kmmv1beta1.NodeModulesConfigStatus{
				Modules: []kmmv1beta1.NodeModuleStatus{
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Namespace: nsFirst,
							Name:      nameFirst,
						},
						Config: kmmv1beta1.ModuleConfig{ContainerImage: imageFirst},
					},
				},
			},
		}
		lph = newLabelPreparationHelper()
	})
	It("Should not have module not from nmc", func() {
		statusLabels := lph.getStatusLabelsAndTheirConfigs(&nmc)
		Expect(statusLabels).To(HaveKeyWithValue(types.NamespacedName{Namespace: nsFirst, Name: nameFirst}, kmmv1beta1.ModuleConfig{ContainerImage: imageFirst}))
	})

	It("Should have module from nmc", func() {
		statusLabels := lph.getStatusLabelsAndTheirConfigs(&nmc)
		Expect(statusLabels).ToNot(HaveKey(types.NamespacedName{Namespace: nsSecond, Name: nameSecond}))
	})

})

var _ = Describe("removeOrphanedLabels", func() {

	var lph = labelPreparationHelperImpl{}

	DescribeTable("removeOrphanedLabels different scenarios", func(nodeModuleReadyLabels sets.Set[types.NamespacedName],
		specLabels map[types.NamespacedName]kmmv1beta1.ModuleConfig,
		statusLabels map[types.NamespacedName]kmmv1beta1.ModuleConfig,
		node v1.Node,
		expectedUnloaded []types.NamespacedName,
	) {
		unloaded := lph.removeOrphanedLabels(nodeModuleReadyLabels, specLabels, statusLabels)
		Expect(unloaded).To(Equal(expectedUnloaded))
	},
		Entry("Empty spec and status labels, should result of empty unloaded variable",
			sets.Set[types.NamespacedName]{},
			map[types.NamespacedName]kmmv1beta1.ModuleConfig{},
			map[types.NamespacedName]kmmv1beta1.ModuleConfig{},
			v1.Node{},
			[]types.NamespacedName{}),
		Entry("ModuleConfig obj exists in specLabels so it should not be in unloaded variable",
			sets.Set[types.NamespacedName]{
				{Namespace: nsFirst, Name: nameFirst}:   {},
				{Namespace: nsSecond, Name: nameSecond}: {},
			},
			map[types.NamespacedName]kmmv1beta1.ModuleConfig{{Namespace: nsFirst, Name: nameFirst}: {}},
			map[types.NamespacedName]kmmv1beta1.ModuleConfig{},
			v1.Node{},
			[]types.NamespacedName{{Namespace: nsSecond, Name: nameSecond}}),

		Entry("ModuleConfig obj exists in statusLabels so it should not be in unloaded variable",
			sets.Set[types.NamespacedName]{
				{Namespace: nsFirst, Name: nameFirst}:   {},
				{Namespace: nsSecond, Name: nameSecond}: {},
			},
			map[types.NamespacedName]kmmv1beta1.ModuleConfig{},
			map[types.NamespacedName]kmmv1beta1.ModuleConfig{{Namespace: nsFirst, Name: nameFirst}: {}},
			v1.Node{},
			[]types.NamespacedName{{Namespace: nsSecond, Name: nameSecond}}),

		Entry("Both ModuleConfig obj exist in specLabels or statusLabels so they should not be in unloaded variable",
			sets.Set[types.NamespacedName]{
				{Namespace: nsFirst, Name: nameFirst}:   {},
				{Namespace: nsSecond, Name: nameSecond}: {},
			},
			map[types.NamespacedName]kmmv1beta1.ModuleConfig{{Namespace: nsFirst, Name: nameFirst}: {}},
			map[types.NamespacedName]kmmv1beta1.ModuleConfig{{Namespace: nsSecond, Name: nameSecond}: {}},
			v1.Node{},
			[]types.NamespacedName{}))
})

var _ = Describe("addEqualLabels", func() {
	var lph = labelPreparationHelperImpl{}

	DescribeTable("addEqualLabels different scenarios", func(nodeModuleReadyLabels sets.Set[types.NamespacedName], specLabels map[types.NamespacedName]kmmv1beta1.ModuleConfig,
		statusLabels map[types.NamespacedName]kmmv1beta1.ModuleConfig,
		node v1.Node,
		expectedUnloaded []types.NamespacedName,
	) {
		loaded := lph.addEqualLabels(nodeModuleReadyLabels, specLabels, statusLabels)
		Expect(loaded).To(Equal(expectedUnloaded))
	},
		Entry("Empty spec and status labels, should result of empty loaded variable",
			sets.Set[types.NamespacedName]{},
			map[types.NamespacedName]kmmv1beta1.ModuleConfig{},
			map[types.NamespacedName]kmmv1beta1.ModuleConfig{},
			v1.Node{},
			[]types.NamespacedName{}),
		Entry("specConfig and statusConfig are equal and nsn is not in nodeModuleReadyLabels, so nsn should be returned",
			sets.Set[types.NamespacedName]{
				{Namespace: nsSecond, Name: nameSecond}: {},
			},
			map[types.NamespacedName]kmmv1beta1.ModuleConfig{{Namespace: nsFirst, Name: nameFirst}: {ContainerImage: imageFirst}},
			map[types.NamespacedName]kmmv1beta1.ModuleConfig{{Namespace: nsFirst, Name: nameFirst}: {ContainerImage: imageFirst}},
			v1.Node{},
			[]types.NamespacedName{{Namespace: nsFirst, Name: nameFirst}}),
		Entry("specConfig and statusConfig aren't equal so nsn shouldn't not be returned",
			sets.Set[types.NamespacedName]{
				{Namespace: nsSecond, Name: nameSecond}: {},
			},
			map[types.NamespacedName]kmmv1beta1.ModuleConfig{{Namespace: nsFirst, Name: nameFirst}: {ContainerImage: imageFirst}},
			map[types.NamespacedName]kmmv1beta1.ModuleConfig{{Namespace: nsFirst, Name: nameFirst}: {ContainerImage: imageSecond}},
			v1.Node{},
			[]types.NamespacedName{}),
		Entry("nsn is in nodeModuleReadyLabels so nsn should not be returned",
			sets.Set[types.NamespacedName]{
				{Namespace: nsFirst, Name: nameFirst}: {},
			},
			map[types.NamespacedName]kmmv1beta1.ModuleConfig{{Namespace: nsFirst, Name: nameFirst}: {ContainerImage: imageFirst}},
			map[types.NamespacedName]kmmv1beta1.ModuleConfig{{Namespace: nsFirst, Name: nameFirst}: {ContainerImage: imageFirst}},
			v1.Node{},
			[]types.NamespacedName{}))
})
