package controllers

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/kubernetes-sigs/kernel-module-management/internal/node"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/budougumi0617/cmpmock"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	testclient "github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/config"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/nmc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	"github.com/kubernetes-sigs/kernel-module-management/internal/worker"
	"github.com/mitchellh/hashstructure/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/kubectl/pkg/cmd/util/podcmd"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	nmcName     = "nmc"
	nsFirst     = "example-ns-1"
	nsSecond    = "example-ns-2"
	nameFirst   = "example-name-1"
	nameSecond  = "example-name-2"
	imageFirst  = "example-image-1"
	imageSecond = "example-image-2"
)

var _ = Describe("NodeModulesConfigReconciler_Reconcile", func() {
	var (
		kubeClient *testclient.MockClient
		wh         *MocknmcReconcilerHelper

		r *NMCReconciler

		ctx    = context.TODO()
		nmcNsn = types.NamespacedName{Name: nmcName}
		req    = reconcile.Request{NamespacedName: nmcNsn}
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = testclient.NewMockClient(ctrl)
		wh = NewMocknmcReconcilerHelper(ctrl)
		r = &NMCReconciler{
			client: kubeClient,
			helper: wh,
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

		gomock.InOrder(
			kubeClient.
				EXPECT().
				Get(ctx, nmcNsn, &kmmv1beta1.NodeModulesConfig{}).
				Do(func(_ context.Context, _ types.NamespacedName, kubeNmc ctrlclient.Object, _ ...ctrlclient.Options) {
					*kubeNmc.(*kmmv1beta1.NodeModulesConfig) = *nmc
				}),
			wh.EXPECT().SyncStatus(ctx, nmc).Return(errors.New("random error")),
		)

		_, err := r.Reconcile(ctx, req)
		Expect(err).To(HaveOccurred())
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
			wh.EXPECT().SyncStatus(ctx, nmc),
			wh.EXPECT().ProcessModuleSpec(contextWithValueMatch, nmc, &spec0, &status0),
			wh.EXPECT().ProcessModuleSpec(contextWithValueMatch, nmc, &spec1, nil),
			wh.EXPECT().ProcessUnconfiguredModuleStatus(contextWithValueMatch, nmc, &status2),
			wh.EXPECT().GarbageCollectInUseLabels(ctx, nmc),
			kubeClient.EXPECT().Get(ctx, types.NamespacedName{Name: nmc.Name}, &node).Return(nil),
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
			wh.EXPECT().SyncStatus(ctx, nmc).Return(nil),
			wh.EXPECT().ProcessModuleSpec(contextWithValueMatch, nmc, &spec0, &status0).Return(fmt.Errorf(errorMeassge)),
			wh.EXPECT().ProcessUnconfiguredModuleStatus(contextWithValueMatch, nmc, &status2).Return(fmt.Errorf(errorMeassge)),
			wh.EXPECT().GarbageCollectInUseLabels(ctx, nmc).Return(fmt.Errorf(errorMeassge)),
			kubeClient.EXPECT().Get(ctx, types.NamespacedName{Name: nmc.Name}, &node).Return(nil),
			wh.EXPECT().UpdateNodeLabels(ctx, nmc, &node).Return(nil, nil, fmt.Errorf(errorMeassge)),
		)

		_, err = r.Reconcile(ctx, req)
		Expect(err).To(Equal(errors.Join(expectedErrors...)))
	})
})

var moduleConfig = kmmv1beta1.ModuleConfig{
	KernelVersion:         "kernel version",
	ContainerImage:        "container image",
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

var _ = Describe("nmcReconcilerHelperImpl_GarbageCollectInUseLabels", func() {
	var (
		ctx = context.TODO()

		client *testclient.MockClient
		pm     *MockpodManager
		wh     nmcReconcilerHelper
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		client = testclient.NewMockClient(ctrl)
		pm = NewMockpodManager(ctrl)
		wh = newNMCReconcilerHelper(client, pm, nil, nil)
	})

	It("should do nothing if no labels should be collected", func() {
		nmcObj := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
			Status: kmmv1beta1.NodeModulesConfigStatus{
				Modules: make([]kmmv1beta1.NodeModuleStatus, 0),
			},
		}

		gomock.InOrder(
			pm.EXPECT().ListWorkerPodsOnNode(ctx, nmcName),
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
			pm.EXPECT().ListWorkerPodsOnNode(ctx, nmcName).Return([]v1.Pod{pod}, nil),
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
		podName = workerPodName(nmcName, name)

		client *testclient.MockClient
		pm     *MockpodManager
		wh     nmcReconcilerHelper
		nm     node.Node
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		client = testclient.NewMockClient(ctrl)
		pm = NewMockpodManager(ctrl)
		nm = node.NewNode(client)
		wh = newNMCReconcilerHelper(client, pm, nil, nm)
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
			pm.EXPECT().GetWorkerPod(ctx, podName, namespace),
			pm.EXPECT().CreateLoaderPod(ctx, nmc, spec),
		)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, nil),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should create an unloader Pod if the spec is different from the status", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}

		spec := &kmmv1beta1.NodeModuleSpec{
			ModuleItem: kmmv1beta1.ModuleItem{
				Name:      name,
				Namespace: namespace,
			},
			Config: kmmv1beta1.ModuleConfig{ContainerImage: "old-container-image"},
		}

		status := &kmmv1beta1.NodeModuleStatus{
			ModuleItem: kmmv1beta1.ModuleItem{
				Name:      name,
				Namespace: namespace,
			},
			Config: kmmv1beta1.ModuleConfig{ContainerImage: "new-container-image"},
		}

		gomock.InOrder(
			pm.EXPECT().GetWorkerPod(ctx, podName, namespace),
			pm.EXPECT().CreateUnloaderPod(ctx, nmc, status),
		)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, status),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should return an error if we could not get the node", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}

		cfg := kmmv1beta1.ModuleConfig{ContainerImage: "some-image"}

		spec := &kmmv1beta1.NodeModuleSpec{
			ModuleItem: kmmv1beta1.ModuleItem{
				Name:      name,
				Namespace: namespace,
			},
			Config: cfg,
		}

		status := &kmmv1beta1.NodeModuleStatus{
			ModuleItem: kmmv1beta1.ModuleItem{
				Name:      name,
				Namespace: namespace,
			},
			Config: cfg,
		}

		gomock.InOrder(
			pm.EXPECT().GetWorkerPod(ctx, podName, namespace),
			client.
				EXPECT().
				Get(ctx, types.NamespacedName{Name: nmcName}, &v1.Node{}).
				Return(errors.New("random error")),
		)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, status),
		).To(
			HaveOccurred(),
		)
	})

	It("should return an error if the node has no ready condition", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}

		spec := &kmmv1beta1.NodeModuleSpec{
			ModuleItem: kmmv1beta1.ModuleItem{
				Name:      name,
				Namespace: namespace,
			},
			Config: moduleConfig,
		}

		status := &kmmv1beta1.NodeModuleStatus{
			ModuleItem: kmmv1beta1.ModuleItem{
				Name:      name,
				Namespace: namespace,
			},
			Config:             moduleConfig,
			LastTransitionTime: metav1.Now(),
		}

		gomock.InOrder(
			pm.EXPECT().GetWorkerPod(ctx, podName, namespace),
			client.EXPECT().Get(ctx, types.NamespacedName{Name: nmcName}, &v1.Node{}),
		)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, status),
		).To(
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

	now := metav1.Now()

	status := &kmmv1beta1.NodeModuleStatus{
		Config:             moduleConfig,
		LastTransitionTime: metav1.Time{Time: now.Add(-1 * time.Minute)},
		ModuleItem: kmmv1beta1.ModuleItem{
			Name:      name,
			Namespace: namespace,
		},
	}

	DescribeTable(
		"should create a loader Pod if status is older than the Ready condition",
		func(cs v1.ConditionStatus, shouldCreate bool) {

			getPod := pm.
				EXPECT().
				GetWorkerPod(ctx, podName, namespace)

			getNode := client.
				EXPECT().
				Get(ctx, types.NamespacedName{Name: nmcName}, &v1.Node{}).
				Do(func(_ context.Context, _ types.NamespacedName, node *v1.Node, _ ...ctrl.Options) {
					node.Status.Conditions = []v1.NodeCondition{
						{
							Type:               v1.NodeReady,
							Status:             cs,
							LastTransitionTime: now,
						},
					}
				}).
				After(getPod)

			if shouldCreate {
				pm.EXPECT().CreateLoaderPod(ctx, nmc, spec).After(getNode)
			}

			Expect(
				wh.ProcessModuleSpec(ctx, nmc, spec, status),
			).NotTo(
				HaveOccurred(),
			)
		},
		Entry(nil, v1.ConditionFalse, false),
		Entry(nil, v1.ConditionTrue, true),
	)

	It("should do nothing if the pod is not loading a kmod", func() {
		pm.
			EXPECT().
			GetWorkerPod(ctx, podName, namespace).
			Return(&v1.Pod{}, nil)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, status),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should do nothing if the worker container has not restarted", func() {
		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{actionLabelKey: WorkerActionLoad},
			},
		}

		pm.
			EXPECT().
			GetWorkerPod(ctx, podName, namespace).
			Return(&pod, nil)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, status),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should return an error if there was an error making the Pod template", func() {
		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{actionLabelKey: WorkerActionLoad},
			},
			Status: v1.PodStatus{
				ContainerStatuses: []v1.ContainerStatus{
					{
						Name:         workerContainerName,
						RestartCount: 1,
					},
				},
			},
		}

		gomock.InOrder(
			pm.
				EXPECT().
				GetWorkerPod(ctx, podName, namespace).
				Return(&pod, nil),
			pm.
				EXPECT().
				LoaderPodTemplate(ctx, nmc, spec).
				Return(nil, errors.New("random error")),
		)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, status),
		).To(
			HaveOccurred(),
		)
	})

	It("should delete the existing pod if its hash annotation is outdated", func() {
		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Labels:      map[string]string{actionLabelKey: WorkerActionLoad},
				Annotations: map[string]string{hashAnnotationKey: "123"},
			},
			Status: v1.PodStatus{
				ContainerStatuses: []v1.ContainerStatus{
					{
						Name:         workerContainerName,
						RestartCount: 1,
					},
				},
			},
		}

		podTemplate := pod.DeepCopy()
		podTemplate.Annotations[hashAnnotationKey] = "456"

		gomock.InOrder(
			pm.
				EXPECT().
				GetWorkerPod(ctx, podName, namespace).
				Return(&pod, nil),
			pm.
				EXPECT().
				LoaderPodTemplate(ctx, nmc, spec).
				Return(podTemplate, nil),
			pm.
				EXPECT().
				DeletePod(ctx, &pod),
		)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, status),
		).NotTo(
			HaveOccurred(),
		)
	})
})

var _ = Describe("nmcReconcilerHelperImpl_ProcessUnconfiguredModuleStatus", func() {
	const name = "name"

	var (
		ctx     = context.TODO()
		podName = workerPodName(nmcName, name)

		client *testclient.MockClient
		pm     *MockpodManager
		helper nmcReconcilerHelper
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		client = testclient.NewMockClient(ctrl)
		pm = NewMockpodManager(ctrl)
		helper = newNMCReconcilerHelper(client, pm, nil, nil)
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

	It("should create an unloader Pod if no worker Pod exists", func() {
		gomock.InOrder(
			pm.EXPECT().GetWorkerPod(ctx, podName, namespace),
			pm.EXPECT().CreateUnloaderPod(ctx, nmc, status),
		)

		Expect(
			helper.ProcessUnconfiguredModuleStatus(ctx, nmc, status),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should delete the current worker if it is loading a module", func() {
		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
				Labels:    map[string]string{actionLabelKey: WorkerActionLoad},
			},
		}

		gomock.InOrder(
			pm.EXPECT().GetWorkerPod(ctx, podName, namespace).Return(&pod, nil),
			pm.EXPECT().DeletePod(ctx, &pod),
		)

		Expect(
			helper.ProcessUnconfiguredModuleStatus(ctx, nmc, status),
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

		pm.EXPECT().GetWorkerPod(ctx, podName, namespace).Return(&pod, nil)

		Expect(
			helper.ProcessUnconfiguredModuleStatus(ctx, nmc, status),
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
						Name:         workerContainerName,
						RestartCount: 1,
					},
				},
			},
		}

		gomock.InOrder(
			pm.EXPECT().GetWorkerPod(ctx, podName, namespace).Return(&pod, nil),
			pm.EXPECT().UnloaderPodTemplate(ctx, nmc, status).Return(nil, errors.New("random error")),
		)

		Expect(
			helper.ProcessUnconfiguredModuleStatus(ctx, nmc, status),
		).To(
			HaveOccurred(),
		)
	})

	It("should delete the existing pod if its hash annotation is outdated", func() {
		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:        podName,
				Namespace:   namespace,
				Annotations: map[string]string{hashAnnotationKey: "123"},
			},
			Status: v1.PodStatus{
				ContainerStatuses: []v1.ContainerStatus{
					{
						Name:         workerContainerName,
						RestartCount: 1,
					},
				},
			},
		}

		podTemplate := pod.DeepCopy()
		podTemplate.Annotations[hashAnnotationKey] = "456"

		gomock.InOrder(
			pm.EXPECT().GetWorkerPod(ctx, podName, namespace).Return(&pod, nil),
			pm.EXPECT().UnloaderPodTemplate(ctx, nmc, status).Return(podTemplate, nil),
			pm.EXPECT().DeletePod(ctx, &pod),
		)

		Expect(
			helper.ProcessUnconfiguredModuleStatus(ctx, nmc, status),
		).NotTo(
			HaveOccurred(),
		)
	})

})

var _ = Describe("nmcReconcilerHelperImpl_SyncStatus", func() {
	var (
		ctx = context.TODO()

		ctrl       *gomock.Controller
		kubeClient *testclient.MockClient
		pm         *MockpodManager
		sw         *testclient.MockStatusWriter
		wh         nmcReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		kubeClient = testclient.NewMockClient(ctrl)
		pm = NewMockpodManager(ctrl)
		wh = newNMCReconcilerHelper(kubeClient, pm, nil, nil)
		sw = testclient.NewMockStatusWriter(ctrl)
	})

	const (
		podName      = "pod-name"
		podNamespace = "pod-namespace"
	)

	It("should do nothing if there are no running pods for this NMC", func() {
		pm.EXPECT().ListWorkerPodsOnNode(ctx, nmcName)

		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}

		Expect(
			wh.SyncStatus(ctx, nmc),
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
			pm.EXPECT().ListWorkerPodsOnNode(ctx, nmcName).Return(pods, nil),
			kubeClient.EXPECT().Status().Return(sw),
			sw.EXPECT().Patch(ctx, nmc, gomock.Any()),
			pm.EXPECT().DeletePod(ctx, &podWithStatus),
			pm.EXPECT().DeletePod(ctx, &podWithoutStatus),
		)

		Expect(
			wh.SyncStatus(ctx, nmc),
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
					actionLabelKey:            WorkerActionUnload,
					constants.ModuleNameLabel: modName,
				},
			},
			Status: v1.PodStatus{Phase: v1.PodSucceeded},
		}

		gomock.InOrder(
			pm.EXPECT().ListWorkerPodsOnNode(ctx, nmcName).Return([]v1.Pod{pod}, nil),
			kubeClient.EXPECT().Status().Return(sw),
			sw.EXPECT().Patch(ctx, nmc, gomock.Any()),
			pm.EXPECT().DeletePod(ctx, &pod),
		)

		Expect(
			wh.SyncStatus(ctx, nmc),
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

		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: modNamespace,
				Labels: map[string]string{
					actionLabelKey:            WorkerActionLoad,
					constants.ModuleNameLabel: modName,
				},
			},
			Spec: v1.PodSpec{
				ServiceAccountName: serviceAccountName,
				Volumes: []v1.Volume{
					{
						Name: volNameImageRepoSecret,
						VolumeSource: v1.VolumeSource{
							Secret: &v1.SecretVolumeSource{SecretName: irsName},
						},
					},
				},
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

		Expect(
			setWorkerConfigAnnotation(&pod, cfg),
		).NotTo(
			HaveOccurred(),
		)

		gomock.InOrder(
			pm.EXPECT().ListWorkerPodsOnNode(ctx, nmcName).Return([]v1.Pod{pod}, nil),
			kubeClient.EXPECT().Status().Return(sw),
			sw.EXPECT().Patch(ctx, nmc, gomock.Any()),
			pm.EXPECT().DeletePod(ctx, &pod),
		)

		Expect(
			wh.SyncStatus(ctx, nmc),
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
			},
			Config:             cfg,
			LastTransitionTime: now,
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
					actionLabelKey:            WorkerActionUnload,
					constants.ModuleNameLabel: modName,
				},
			},
			Status: v1.PodStatus{Phase: v1.PodSucceeded},
		}

		gomock.InOrder(
			pm.EXPECT().ListWorkerPodsOnNode(ctx, nmcName).Return([]v1.Pod{pod}, nil),
			kubeClient.EXPECT().Status().Return(sw),
			sw.EXPECT().Patch(ctx, nmc, gomock.Any()).Return(errors.New("some error")),
		)

		Expect(
			wh.SyncStatus(ctx, nmc),
		).To(
			HaveOccurred(),
		)
	})
})

var _ = Describe("nmcReconcilerHelperImpl_RemovePodFinalizers", func() {
	const nodeName = "node-name"

	var (
		ctx = context.TODO()

		kubeClient *testclient.MockClient
		pm         *MockpodManager
		wh         nmcReconcilerHelper
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = testclient.NewMockClient(ctrl)
		pm = NewMockpodManager(ctrl)
		wh = newNMCReconcilerHelper(kubeClient, pm, nil, nil)
	})

	It("should do nothing if no pods are present", func() {
		pm.EXPECT().ListWorkerPodsOnNode(ctx, nodeName)

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
				Finalizers: []string{nodeModulesConfigFinalizer},
			},
		}

		podWithoutFinalizer := podWithFinalizer
		podWithoutFinalizer.Finalizers = []string{}

		gomock.InOrder(
			pm.EXPECT().ListWorkerPodsOnNode(ctx, nodeName).Return([]v1.Pod{podWithFinalizer, {}}, nil),
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
		ctx             context.Context
		client          *testclient.MockClient
		fakeRecorder    *record.FakeRecorder
		n               *node.MockNode
		nmc             kmmv1beta1.NodeModulesConfig
		wh              nmcReconcilerHelper
		mlph            *MocklabelPreparationHelper
		firstLabelName  string
		secondLabelName string
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
			client:   client,
			pm:       nil,
			recorder: fakeRecorder,
			nodeAPI:  n,
			lph:      mlph,
		}
		firstLabelName = fmt.Sprintf("kmm.node.kubernetes.io/%s.%s.ready", nsFirst, nameFirst)
		secondLabelName = fmt.Sprintf("kmm.node.kubernetes.io/%s.%s.ready", nsSecond, nameSecond)
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
			mlph.EXPECT().getDeprecatedKernelModuleReadyLabels(node).Return(sets.Set[string]{}),
			mlph.EXPECT().getSpecLabelsAndTheirConfigs(&nmc).Return(emptyMap),
			mlph.EXPECT().getStatusLabelsAndTheirConfigs(&nmc).Return(emptyMap),
			mlph.EXPECT().removeOrphanedLabels(emptySet, emptyMap, emptyMap).Return([]types.NamespacedName{{Name: nameSecond, Namespace: nsSecond}}),
			mlph.EXPECT().addEqualLabels(emptySet, emptyMap, emptyMap).Return([]types.NamespacedName{{Name: nameFirst, Namespace: nsFirst}}),
			n.EXPECT().
				UpdateLabels(
					ctx,
					&node,
					[]string{firstLabelName},
					[]string{secondLabelName},
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

		gomock.InOrder(
			mlph.EXPECT().getNodeKernelModuleReadyLabels(node).Return(emptySet),
			mlph.EXPECT().getDeprecatedKernelModuleReadyLabels(node).Return(sets.Set[string]{}),
			mlph.EXPECT().getSpecLabelsAndTheirConfigs(&nmc).Return(emptyMap),
			mlph.EXPECT().getStatusLabelsAndTheirConfigs(&nmc).Return(emptyMap),
			mlph.EXPECT().removeOrphanedLabels(emptySet, emptyMap, emptyMap).Return([]types.NamespacedName{{Name: nameSecond, Namespace: nsSecond}}),
			mlph.EXPECT().addEqualLabels(emptySet, emptyMap, emptyMap).Return([]types.NamespacedName{{Name: nameFirst, Namespace: nsFirst}}),
			n.EXPECT().
				UpdateLabels(
					ctx,
					&node,
					[]string{firstLabelName},
					[]string{secondLabelName},
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
		n            node.Node
		wh           nmcReconcilerHelper
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		client = testclient.NewMockClient(ctrl)
		n = node.NewNode(client)
		fakeRecorder = record.NewFakeRecorder(10)
		wh = newNMCReconcilerHelper(client, nil, fakeRecorder, n)
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

const (
	serviceAccountName = "some-sa"
	workerImage        = "worker-image"
)

var workerCfg = &config.Worker{
	RunAsUser:   ptr.To[int64](1234),
	SELinuxType: "someType",
}

var _ = Describe("podManagerImpl_CreateLoaderPod", func() {

	const irsName = "some-secret"

	var (
		ctrl   *gomock.Controller
		client *testclient.MockClient
		psh    *MockpullSecretHelper

		nmc *kmmv1beta1.NodeModulesConfig
		mi  kmmv1beta1.ModuleItem

		ctx               context.Context
		moduleConfigToUse kmmv1beta1.ModuleConfig
	)

	BeforeEach(func() {

		ctrl = gomock.NewController(GinkgoT())
		client = testclient.NewMockClient(ctrl)
		psh = NewMockpullSecretHelper(ctrl)

		nmc = &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}

		mi = kmmv1beta1.ModuleItem{
			ImageRepoSecret:    &v1.LocalObjectReference{Name: irsName},
			Name:               moduleName,
			Namespace:          namespace,
			ServiceAccountName: serviceAccountName,
		}

		moduleConfigToUse = moduleConfig
		ctx = context.TODO()
	})

	It("it should fail if firmwareClassPath was not set but firmware loading was", func() {

		moduleConfigToUse.Modprobe.FirmwarePath = "/firmware-path"

		nms := &kmmv1beta1.NodeModuleSpec{
			ModuleItem: mi,
			Config:     moduleConfigToUse,
		}

		psh.EXPECT().VolumesAndVolumeMounts(ctx, &mi)

		pm := &podManagerImpl{
			client:      client,
			psh:         psh,
			scheme:      scheme,
			workerImage: workerImage,
			workerCfg:   workerCfg,
		}

		Expect(
			pm.CreateLoaderPod(ctx, nmc, nms),
		).To(
			HaveOccurred(),
		)
	})

	DescribeTable(
		"should work as expected",
		func(firmwareClassPath *string, withFirmwareLoading bool) {

			if withFirmwareLoading {
				moduleConfigToUse.Modprobe.FirmwarePath = "/firmware-path"
			}

			nms := &kmmv1beta1.NodeModuleSpec{
				ModuleItem: mi,
				Config:     moduleConfigToUse,
			}

			expected := getBaseWorkerPod("load", WorkerActionLoad, nmc, firmwareClassPath, withFirmwareLoading, true)

			Expect(
				controllerutil.SetControllerReference(nmc, expected, scheme),
			).NotTo(
				HaveOccurred(),
			)

			controllerutil.AddFinalizer(expected, nodeModulesConfigFinalizer)

			container, _ := podcmd.FindContainerByName(expected, "worker")
			Expect(container).NotTo(BeNil())

			if withFirmwareLoading && firmwareClassPath != nil {
				container.SecurityContext = &v1.SecurityContext{
					Privileged: ptr.To(true),
				}
			} else {
				container.SecurityContext = &v1.SecurityContext{
					Capabilities: &v1.Capabilities{
						Add: []v1.Capability{"SYS_MODULE"},
					},
					RunAsUser:      workerCfg.RunAsUser,
					SELinuxOptions: &v1.SELinuxOptions{Type: workerCfg.SELinuxType},
				}
			}

			hash, err := hashstructure.Hash(expected, hashstructure.FormatV2, nil)
			Expect(err).NotTo(HaveOccurred())

			expected.Annotations[hashAnnotationKey] = fmt.Sprintf("%d", hash)

			gomock.InOrder(
				psh.EXPECT().VolumesAndVolumeMounts(ctx, &mi),
				client.EXPECT().Create(ctx, cmpmock.DiffEq(expected)),
			)

			workerCfg := *workerCfg
			workerCfg.SetFirmwareClassPath = firmwareClassPath

			pm := &podManagerImpl{
				client:      client,
				psh:         psh,
				scheme:      scheme,
				workerImage: workerImage,
				workerCfg:   &workerCfg,
			}

			Expect(
				pm.CreateLoaderPod(ctx, nmc, nms),
			).NotTo(
				HaveOccurred(),
			)
		},
		Entry("pod without firmwareClassPath, without firmware loading", nil, false),
		Entry("pod with empty firmwareClassPath, without firmware loading", ptr.To(""), false),
		Entry("pod with firmwareClassPath, without firmware loading", ptr.To("some-path"), false),
		Entry("pod with firmwareClassPath, with firmware loading", ptr.To("some-path"), true),
	)
})

var _ = Describe("podManagerImpl_CreateUnloaderPod", func() {

	const irsName = "some-secret"

	var (
		ctrl   *gomock.Controller
		client *testclient.MockClient
		psh    *MockpullSecretHelper

		nmc *kmmv1beta1.NodeModulesConfig
		mi  kmmv1beta1.ModuleItem

		ctx               context.Context
		moduleConfigToUse kmmv1beta1.ModuleConfig
		status            *kmmv1beta1.NodeModuleStatus
	)

	BeforeEach(func() {

		ctrl = gomock.NewController(GinkgoT())
		client = testclient.NewMockClient(ctrl)
		psh = NewMockpullSecretHelper(ctrl)

		nmc = &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}

		mi = kmmv1beta1.ModuleItem{
			ImageRepoSecret:    &v1.LocalObjectReference{Name: irsName},
			Name:               moduleName,
			Namespace:          namespace,
			ServiceAccountName: serviceAccountName,
		}

		moduleConfigToUse = moduleConfig
		moduleConfigToUse.Modprobe.FirmwarePath = "/firmware-path"
		status = &kmmv1beta1.NodeModuleStatus{
			ModuleItem: mi,
			Config:     moduleConfigToUse,
		}
		ctx = context.TODO()
	})

	It("it should fail if firmwareClassPath was not set but firmware loading was", func() {

		psh.EXPECT().VolumesAndVolumeMounts(ctx, &mi)

		pm := newPodManager(client, workerImage, scheme, workerCfg)
		pm.(*podManagerImpl).psh = psh

		Expect(
			pm.CreateUnloaderPod(ctx, nmc, status),
		).To(
			HaveOccurred(),
		)
	})

	It("should work as expected", func() {

		expected := getBaseWorkerPod("unload", WorkerActionUnload, nmc, ptr.To("some-path"), true, false)

		container, _ := podcmd.FindContainerByName(expected, "worker")
		Expect(container).NotTo(BeNil())

		container.SecurityContext = &v1.SecurityContext{
			Capabilities: &v1.Capabilities{
				Add: []v1.Capability{"SYS_MODULE"},
			},
			RunAsUser:      workerCfg.RunAsUser,
			SELinuxOptions: &v1.SELinuxOptions{Type: workerCfg.SELinuxType},
		}

		hash, err := hashstructure.Hash(expected, hashstructure.FormatV2, nil)
		Expect(err).NotTo(HaveOccurred())

		expected.Annotations[hashAnnotationKey] = fmt.Sprintf("%d", hash)

		gomock.InOrder(
			psh.EXPECT().VolumesAndVolumeMounts(ctx, &mi),
			client.EXPECT().Create(ctx, cmpmock.DiffEq(expected)),
		)

		workerCfg := *workerCfg
		workerCfg.SetFirmwareClassPath = ptr.To("some-path")

		pm := newPodManager(client, workerImage, scheme, &workerCfg)
		pm.(*podManagerImpl).psh = psh

		Expect(
			pm.CreateUnloaderPod(ctx, nmc, status),
		).NotTo(
			HaveOccurred(),
		)
	})
})

var _ = Describe("podManagerImpl_DeletePod", func() {
	ctx := context.TODO()
	now := metav1.Now()

	DescribeTable(
		"should work as expected",
		func(deletionTimestamp *metav1.Time) {
			ctrl := gomock.NewController(GinkgoT())
			kubeclient := testclient.NewMockClient(ctrl)

			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: deletionTimestamp,
					Finalizers:        []string{nodeModulesConfigFinalizer},
				},
			}

			patchedPod := pod
			patchedPod.Finalizers = nil

			patch := kubeclient.EXPECT().Patch(ctx, patchedPod, gomock.Any())

			if deletionTimestamp == nil {
				kubeclient.EXPECT().Delete(ctx, patchedPod).After(patch)
			}

			Expect(
				newPodManager(kubeclient, workerImage, scheme, workerCfg).DeletePod(ctx, patchedPod),
			).NotTo(
				HaveOccurred(),
			)
		},
		Entry("deletionTimestamp not set", nil),
		Entry("deletionTimestamp set", &now),
	)
})

var _ = Describe("podManagerImpl_ListWorkerPodsOnNode", func() {
	const nodeName = "some-node"

	var (
		ctx = context.TODO()

		kubeClient *testclient.MockClient
		pm         podManager
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = testclient.NewMockClient(ctrl)
		pm = newPodManager(kubeClient, workerImage, scheme, nil)
	})

	opts := []interface{}{
		ctrlclient.HasLabels{actionLabelKey},
		ctrlclient.MatchingFields{".spec.nodeName": nodeName},
	}

	It("should return an error if the kube client encountered one", func() {
		kubeClient.EXPECT().List(ctx, &v1.PodList{}, opts...).Return(errors.New("random error"))

		_, err := pm.ListWorkerPodsOnNode(ctx, nodeName)
		Expect(err).To(HaveOccurred())
	})

	It("should the list of pods", func() {
		pods := []v1.Pod{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-0"},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "pod-1"},
			},
		}

		kubeClient.
			EXPECT().
			List(ctx, &v1.PodList{}, opts...).
			Do(func(_ context.Context, pl *v1.PodList, _ ...ctrlclient.ListOption) {
				pl.Items = pods
			})

		Expect(
			pm.ListWorkerPodsOnNode(ctx, nodeName),
		).To(
			Equal(pods),
		)
	})
})

func getBaseWorkerPod(subcommand string, action WorkerAction, owner ctrlclient.Object, firmwareClassPath *string,
	withFirmware, isLoaderPod bool) *v1.Pod {
	GinkgoHelper()

	const (
		volNameLibModules     = "lib-modules"
		volNameUsrLibModules  = "usr-lib-modules"
		volNameVarLibFirmware = "var-lib-firmware"
	)

	hostPathDirectory := v1.HostPathDirectory
	hostPathDirectoryOrCreate := v1.HostPathDirectoryOrCreate

	configAnnotationValue := `containerImage: container image
inTreeModulesToRemove:
- intree1
- intree2
insecurePull: true
kernelVersion: kernel version
modprobe:
  dirName: /dir
  firmwarePath: /firmware-path
  moduleName: test
  modulesLoadingOrder:
  - a
  - b
  - c
  parameters:
  - a
  - b
`
	modulesOrderValue := `softdep a pre: b
softdep b pre: c
`

	args := []string{"kmod", subcommand, "/etc/kmm-worker/config.yaml"}
	if withFirmware {
		args = append(args, "--set-firmware-mount-path", *firmwareClassPath)
		if isLoaderPod && firmwareClassPath != nil {
			args = append(args, "--set-firmware-class-path", *firmwareClassPath)
		}
	} else {
		configAnnotationValue = strings.ReplaceAll(configAnnotationValue, "firmwarePath: /firmware-path\n  ", "")
	}
	pod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workerPodName(nmcName, moduleName),
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/component": "worker",
				"app.kubernetes.io/name":      "kmm",
				"app.kubernetes.io/part-of":   "kmm",
				actionLabelKey:                string(action),
				constants.ModuleNameLabel:     moduleName,
			},
			Annotations: map[string]string{
				configAnnotationKey: configAnnotationValue,
				modulesOrderKey:     modulesOrderValue,
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "worker",
					Image: workerImage,
					Args:  args,
					Resources: v1.ResourceRequirements{
						Limits:   limits,
						Requests: requests,
					},
					SecurityContext: &v1.SecurityContext{},
					VolumeMounts: []v1.VolumeMount{
						{
							Name:      volNameConfig,
							MountPath: "/etc/kmm-worker",
							ReadOnly:  true,
						},
						{
							Name:      volNameLibModules,
							MountPath: "/lib/modules",
							ReadOnly:  true,
						},
						{
							Name:      volNameUsrLibModules,
							MountPath: "/usr/lib/modules",
							ReadOnly:  true,
						},
						{
							Name:      "modules-order",
							ReadOnly:  true,
							MountPath: "/etc/modprobe.d",
						},
					},
				},
			},
			NodeName:           nmcName,
			RestartPolicy:      v1.RestartPolicyOnFailure,
			ServiceAccountName: serviceAccountName,
			Volumes: []v1.Volume{
				{
					Name: volumeNameConfig,
					VolumeSource: v1.VolumeSource{
						DownwardAPI: &v1.DownwardAPIVolumeSource{
							Items: []v1.DownwardAPIVolumeFile{
								{
									Path: "config.yaml",
									FieldRef: &v1.ObjectFieldSelector{
										FieldPath: fmt.Sprintf("metadata.annotations['%s']", configAnnotationKey),
									},
								},
							},
						},
					},
				},
				{
					Name: volNameLibModules,
					VolumeSource: v1.VolumeSource{
						HostPath: &v1.HostPathVolumeSource{
							Path: "/lib/modules",
							Type: &hostPathDirectory,
						},
					},
				},
				{
					Name: volNameUsrLibModules,
					VolumeSource: v1.VolumeSource{
						HostPath: &v1.HostPathVolumeSource{
							Path: "/usr/lib/modules",
							Type: &hostPathDirectory,
						},
					},
				},
				{
					Name: "modules-order",
					VolumeSource: v1.VolumeSource{
						DownwardAPI: &v1.DownwardAPIVolumeSource{
							Items: []v1.DownwardAPIVolumeFile{
								{
									Path:     "softdep.conf",
									FieldRef: &v1.ObjectFieldSelector{FieldPath: fmt.Sprintf("metadata.annotations['%s']", modulesOrderKey)},
								},
							},
						},
					},
				},
			},
		},
	}

	if withFirmware {
		fwVolMount := v1.VolumeMount{
			Name:      volNameVarLibFirmware,
			MountPath: *firmwareClassPath,
		}
		pod.Spec.Containers[0].VolumeMounts = append(pod.Spec.Containers[0].VolumeMounts, fwVolMount)
		fwVol := v1.Volume{
			Name: volNameVarLibFirmware,
			VolumeSource: v1.VolumeSource{
				HostPath: &v1.HostPathVolumeSource{
					Path: *firmwareClassPath,
					Type: &hostPathDirectoryOrCreate,
				},
			},
		}
		pod.Spec.Volumes = append(pod.Spec.Volumes, fwVol)

	}

	Expect(
		controllerutil.SetControllerReference(owner, &pod, scheme),
	).NotTo(
		HaveOccurred(),
	)

	controllerutil.AddFinalizer(&pod, nodeModulesConfigFinalizer)

	return &pod
}

var _ = Describe("pullSecretHelperImpl_VolumesAndVolumeMounts", func() {
	It("should work as expected", func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient := testclient.NewMockClient(ctrl)
		psh := pullSecretHelperImpl{client: kubeClient}

		const (
			irs           = "pull-secret-0"
			saPullSecret1 = "pull-secret-1"
			saPullSecret2 = "pull-secret-2"
		)

		saPullSecret1Hash, err := hashstructure.Hash(saPullSecret1, hashstructure.FormatV2, nil)
		Expect(err).To(BeNil())
		saPullSecret2Hash, err := hashstructure.Hash(saPullSecret2, hashstructure.FormatV2, nil)
		Expect(err).To(BeNil())

		item := kmmv1beta1.ModuleItem{
			ImageRepoSecret:    &v1.LocalObjectReference{Name: irs},
			Namespace:          namespace,
			ServiceAccountName: serviceAccountName,
		}

		ctx := context.TODO()
		nsn := types.NamespacedName{Namespace: namespace, Name: serviceAccountName}

		kubeClient.
			EXPECT().
			Get(ctx, nsn, &v1.ServiceAccount{}).
			Do(func(_ context.Context, _ types.NamespacedName, sa *v1.ServiceAccount, _ ...ctrlclient.GetOption) {
				sa.ImagePullSecrets = []v1.LocalObjectReference{
					{Name: saPullSecret1},
					{Name: saPullSecret1}, // intentional duplicate, should not be in the volume list
					{Name: saPullSecret2},
				}
			})

		vols := []v1.Volume{
			{
				Name: volNameImageRepoSecret,
				VolumeSource: v1.VolumeSource{
					Secret: &v1.SecretVolumeSource{
						SecretName: irs,
						Optional:   ptr.To(false),
					},
				},
			},
			{
				Name: fmt.Sprintf("pull-secret-%d", saPullSecret1Hash),
				VolumeSource: v1.VolumeSource{
					Secret: &v1.SecretVolumeSource{
						SecretName: saPullSecret1,
						Optional:   ptr.To(true),
					},
				},
			},
			{
				Name: fmt.Sprintf("pull-secret-%d", saPullSecret2Hash),
				VolumeSource: v1.VolumeSource{
					Secret: &v1.SecretVolumeSource{
						SecretName: saPullSecret2,
						Optional:   ptr.To(true),
					},
				},
			},
		}

		volMounts := []v1.VolumeMount{
			{
				Name:      volNameImageRepoSecret,
				ReadOnly:  true,
				MountPath: filepath.Join(worker.PullSecretsDir, irs),
			},
			{
				Name:      fmt.Sprintf("pull-secret-%d", saPullSecret1Hash),
				ReadOnly:  true,
				MountPath: filepath.Join(worker.PullSecretsDir, saPullSecret1),
			},
			{
				Name:      fmt.Sprintf("pull-secret-%d", saPullSecret2Hash),
				ReadOnly:  true,
				MountPath: filepath.Join(worker.PullSecretsDir, saPullSecret2),
			},
		}

		resVols, resVolMounts, err := psh.VolumesAndVolumeMounts(ctx, &item)
		Expect(err).NotTo(HaveOccurred())
		Expect(resVols).To(BeComparableTo(vols))
		Expect(resVolMounts).To(BeComparableTo(volMounts))
	})
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
		deprecatedNodeModuleReadyLabelsEqual sets.Set[string]) {
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: labels,
			},
		}

		deprecatedNodeModuleReadyLabels := lph.getDeprecatedKernelModuleReadyLabels(node)

		Expect(deprecatedNodeModuleReadyLabels).To(Equal(deprecatedNodeModuleReadyLabelsEqual))
	},
		Entry("Should be empty", map[string]string{},
			sets.Set[string]{},
		),

		Entry("deprecated node module ready labels not found", map[string]string{"invalid": ""},
			sets.Set[string]{},
		),

		Entry("deprecated node module ready labels found", map[string]string{fmt.Sprintf("kmm.node.kubernetes.io/%s.ready", nameFirst): ""},
			sets.Set[string]{
				fmt.Sprintf("kmm.node.kubernetes.io/%s.ready", nameFirst): {},
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
