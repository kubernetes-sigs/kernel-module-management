package controllers

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/budougumi0617/cmpmock"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	testclient "github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const nmcName = "nmc"

var _ = Describe("NodeModulesConfigReconciler_Reconcile", func() {
	var (
		kubeClient *testclient.MockClient
		wh         *MockworkerHelper

		r *NMCReconciler

		ctx    = context.TODO()
		nmcNsn = types.NamespacedName{Name: nmcName}
		req    = reconcile.Request{NamespacedName: nmcNsn}
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = testclient.NewMockClient(ctrl)
		wh = NewMockworkerHelper(ctrl)
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
			wh.EXPECT().RemoveOrphanFinalizers(ctx, nmcName),
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
		spec0 := kmmv1beta1.NodeModuleSpec{
			Namespace: namespace,
			Name:      mod0Name,
		}

		spec1 := kmmv1beta1.NodeModuleSpec{
			Namespace: namespace,
			Name:      mod1Name,
		}

		status0 := kmmv1beta1.NodeModuleStatus{
			Namespace: namespace,
			Name:      mod0Name,
		}

		status2 := kmmv1beta1.NodeModuleStatus{
			Namespace: namespace,
			Name:      mod2Name,
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
			wh.EXPECT().ProcessOrphanModuleStatus(contextWithValueMatch, nmc, &status2),
		)

		Expect(
			r.Reconcile(ctx, req),
		).To(
			BeZero(),
		)
	})
})

var moduleConfig = kmmv1beta1.ModuleConfig{
	KernelVersion:        "kernel version",
	ContainerImage:       "container image",
	InsecurePull:         true,
	InTreeModuleToRemove: "intree",
	Modprobe: kmmv1beta1.ModprobeSpec{
		ModuleName:          "test",
		Parameters:          []string{"a", "b"},
		DirName:             "/dir",
		Args:                nil,
		RawArgs:             nil,
		FirmwarePath:        "/firmware-path",
		ModulesLoadingOrder: []string{"a", "b", "c"},
	},
}

var _ = Describe("workerHelper_ProcessModuleSpec", func() {
	const (
		name      = "name"
		namespace = "namespace"
	)

	var (
		ctx = context.TODO()

		client *testclient.MockClient
		pm     *MockpodManager
		wh     workerHelper
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		client = testclient.NewMockClient(ctrl)
		pm = NewMockpodManager(ctrl)
		wh = NewWorkerHelper(client, pm)
	})

	It("should create a loader Pod if the corresponding status is missing", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{}
		spec := &kmmv1beta1.NodeModuleSpec{Name: name, Namespace: namespace}

		pm.EXPECT().CreateLoaderPod(ctx, nmc, spec)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, nil),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should create a loader Pod if inStatus is false, but the Config is not define (nil)", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{}
		spec := &kmmv1beta1.NodeModuleSpec{
			Name:      name,
			Namespace: namespace,
			Config:    kmmv1beta1.ModuleConfig{ContainerImage: "old-container-image"},
		}

		status := &kmmv1beta1.NodeModuleStatus{
			Name:      name,
			Namespace: namespace,
		}
		pm.EXPECT().CreateLoaderPod(ctx, nmc, spec)

		Expect(
			wh.ProcessModuleSpec(ctx, &kmmv1beta1.NodeModulesConfig{}, spec, status),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should create an unloader Pod if the spec is different from the status", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{}

		spec := &kmmv1beta1.NodeModuleSpec{
			Name:      name,
			Namespace: namespace,
			Config:    kmmv1beta1.ModuleConfig{ContainerImage: "old-container-image"},
		}

		status := &kmmv1beta1.NodeModuleStatus{
			Name:      name,
			Namespace: namespace,
			Config:    &kmmv1beta1.ModuleConfig{ContainerImage: "new-container-image"},
		}

		pm.EXPECT().CreateUnloaderPod(ctx, nmc, status)

		Expect(
			wh.ProcessModuleSpec(ctx, nmc, spec, status),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should do nothing if InProgress is true, even though the config is different", func() {
		spec := &kmmv1beta1.NodeModuleSpec{
			Name:      name,
			Namespace: namespace,
			Config:    kmmv1beta1.ModuleConfig{ContainerImage: "old-container-image"},
		}

		status := &kmmv1beta1.NodeModuleStatus{
			Name:       name,
			Namespace:  namespace,
			Config:     &kmmv1beta1.ModuleConfig{ContainerImage: "new-container-image"},
			InProgress: true,
		}

		Expect(
			wh.ProcessModuleSpec(ctx, &kmmv1beta1.NodeModulesConfig{}, spec, status),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should return an error if the node has no ready condition", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}

		spec := &kmmv1beta1.NodeModuleSpec{
			Config:    moduleConfig,
			Name:      name,
			Namespace: namespace,
		}

		now := metav1.Now()

		status := &kmmv1beta1.NodeModuleStatus{
			Config:             &moduleConfig,
			Name:               name,
			Namespace:          namespace,
			LastTransitionTime: &now,
		}

		client.EXPECT().Get(ctx, types.NamespacedName{Name: nmcName}, &v1.Node{})

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
		Config:    moduleConfig,
		Name:      name,
		Namespace: namespace,
	}

	now := metav1.Now()

	status := &kmmv1beta1.NodeModuleStatus{
		Config:             &moduleConfig,
		Name:               name,
		Namespace:          namespace,
		LastTransitionTime: &metav1.Time{Time: now.Add(-1 * time.Minute)},
	}

	DescribeTable(
		"should create a loader Pod if status is older than the Ready condition",
		func(cs v1.ConditionStatus, shouldCreate bool) {
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
				})

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
})

var _ = Describe("workerHelper_ProcessOrphanModuleStatus", func() {
	ctx := context.TODO()

	It("should do nothing if the status sync is in progress", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{}
		status := &kmmv1beta1.NodeModuleStatus{InProgress: true}

		Expect(
			NewWorkerHelper(nil, nil).ProcessOrphanModuleStatus(ctx, nmc, status),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should remove status in case Config is nil", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{}
		status := &kmmv1beta1.NodeModuleStatus{}

		ctrl := gomock.NewController(GinkgoT())
		client := testclient.NewMockClient(ctrl)
		sw := testclient.NewMockStatusWriter(ctrl)
		client.EXPECT().Status().Return(sw)
		sw.EXPECT().Patch(ctx, nmc, gomock.Any())
		Expect(
			NewWorkerHelper(client, nil).ProcessOrphanModuleStatus(ctx, nmc, status),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should create an unloader Pod", func() {
		ctrl := gomock.NewController(GinkgoT())
		client := testclient.NewMockClient(ctrl)

		pm := NewMockpodManager(ctrl)
		wh := NewWorkerHelper(client, pm)

		nmc := &kmmv1beta1.NodeModulesConfig{}
		status := &kmmv1beta1.NodeModuleStatus{
			Config: &kmmv1beta1.ModuleConfig{},
		}

		pm.EXPECT().CreateUnloaderPod(ctx, nmc, status)

		Expect(
			wh.ProcessOrphanModuleStatus(ctx, nmc, status),
		).NotTo(
			HaveOccurred(),
		)
	})
})

var _ = Describe("workerHelper_SyncStatus", func() {
	var (
		ctx = context.TODO()

		ctrl       *gomock.Controller
		kubeClient *testclient.MockClient
		pm         *MockpodManager
		wh         workerHelper
		sw         *testclient.MockStatusWriter
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		kubeClient = testclient.NewMockClient(ctrl)
		pm = NewMockpodManager(ctrl)
		wh = NewWorkerHelper(kubeClient, pm)
		sw = testclient.NewMockStatusWriter(ctrl)
	})

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

	It("should delete the pod if it is failed", func() {
		const (
			podName      = "pod-name"
			podNamespace = "pod-namespace"
		)

		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: podNamespace,
				Name:      podName,
			},
			Status: v1.PodStatus{Phase: v1.PodFailed},
		}

		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}

		gomock.InOrder(
			pm.EXPECT().ListWorkerPodsOnNode(ctx, nmcName).Return([]v1.Pod{pod}, nil),
			pm.EXPECT().DeletePod(ctx, &pod),
			kubeClient.EXPECT().Status().Return(sw),
			sw.EXPECT().Patch(ctx, nmc, gomock.Any()),
		)

		Expect(
			wh.SyncStatus(ctx, nmc),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should set the in progress status if pods are pending or running", func() {
		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}

		pods := []v1.Pod{
			{
				Status: v1.PodStatus{Phase: v1.PodRunning},
			},
			{
				Status: v1.PodStatus{Phase: v1.PodPending},
			},
		}

		gomock.InOrder(
			pm.EXPECT().ListWorkerPodsOnNode(ctx, nmcName).Return(pods, nil),
			kubeClient.EXPECT().Status().Return(sw),
			sw.EXPECT().Patch(ctx, nmc, gomock.Any()),
		)

		Expect(
			wh.SyncStatus(ctx, nmc),
		).NotTo(
			HaveOccurred(),
		)

		Expect(nmc.Status.Modules).To(HaveLen(1))
		Expect(nmc.Status.Modules[0].InProgress).To(BeTrue())
	})

	It("should remove the status if an unloader pod was successful", func() {
		const (
			modName      = "module"
			modNamespace = "namespace"
		)

		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
			Status: kmmv1beta1.NodeModulesConfigStatus{
				Modules: []kmmv1beta1.NodeModuleStatus{
					{
						Name:      modName,
						Namespace: modNamespace,
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
			pm.EXPECT().DeletePod(ctx, &pod),
			kubeClient.EXPECT().Status().Return(sw),
			sw.EXPECT().Patch(ctx, nmc, gomock.Any()),
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
			modName      = "module"
			modNamespace = "namespace"
		)

		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}

		now := metav1.Now()

		cfg := kmmv1beta1.ModuleConfig{
			KernelVersion:        "some-kernel-version",
			ContainerImage:       "some-container-image",
			InsecurePull:         true,
			InTreeModuleToRemove: "intree",
			Modprobe:             kmmv1beta1.ModprobeSpec{ModuleName: "test"},
		}

		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: modNamespace,
				Labels: map[string]string{
					actionLabelKey:            WorkerActionLoad,
					constants.ModuleNameLabel: modName,
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
			pm.EXPECT().DeletePod(ctx, &pod),
			kubeClient.EXPECT().Status().Return(sw),
			sw.EXPECT().Patch(ctx, nmc, gomock.Any()),
		)

		Expect(
			wh.SyncStatus(ctx, nmc),
		).NotTo(
			HaveOccurred(),
		)

		Expect(nmc.Status.Modules).To(HaveLen(1))

		expectedStatus := kmmv1beta1.NodeModuleStatus{
			Config:             &cfg,
			LastTransitionTime: &now,
			Name:               modName,
			Namespace:          modNamespace,
		}

		Expect(nmc.Status.Modules[0]).To(BeComparableTo(expectedStatus))
	})
})

var _ = Describe("workerHelper_RemoveOrphanFinalizers", func() {
	const nodeName = "node-name"

	var (
		ctx = context.TODO()

		kubeClient *testclient.MockClient
		pm         *MockpodManager
		wh         workerHelper
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = testclient.NewMockClient(ctrl)
		pm = NewMockpodManager(ctrl)
		wh = NewWorkerHelper(kubeClient, pm)
	})

	It("should do nothing if no pods are present", func() {
		pm.EXPECT().ListWorkerPodsOnNode(ctx, nodeName)

		Expect(
			wh.RemoveOrphanFinalizers(ctx, nodeName),
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
			wh.RemoveOrphanFinalizers(ctx, nodeName),
		).NotTo(
			HaveOccurred(),
		)
	})
})

const (
	moduleName         = "my-module"
	serviceAccountName = "some-sa"
	workerImage        = "worker-image"
)

var _ = Describe("podManager_CreateLoaderPod", func() {
	It("should work as expected", func() {
		ctrl := gomock.NewController(GinkgoT())
		client := testclient.NewMockClient(ctrl)

		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}

		nms := &kmmv1beta1.NodeModuleSpec{
			Name:               moduleName,
			Namespace:          namespace,
			Config:             moduleConfig,
			ServiceAccountName: serviceAccountName,
		}

		expected := getBaseWorkerPod("load", WorkerActionLoad, nmc)

		Expect(
			controllerutil.SetControllerReference(nmc, expected, scheme),
		).NotTo(
			HaveOccurred(),
		)

		controllerutil.AddFinalizer(expected, nodeModulesConfigFinalizer)

		ctx := context.TODO()

		client.EXPECT().Create(ctx, cmpmock.DiffEq(expected))

		Expect(
			NewPodManager(client, workerImage, scheme).CreateLoaderPod(ctx, nmc, nms),
		).NotTo(
			HaveOccurred(),
		)
	})
})

var _ = Describe("podManager_CreateUnloaderPod", func() {
	It("should work as expected", func() {
		ctrl := gomock.NewController(GinkgoT())
		client := testclient.NewMockClient(ctrl)

		nmc := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{Name: nmcName},
		}

		status := &kmmv1beta1.NodeModuleStatus{
			Name:               moduleName,
			Namespace:          namespace,
			Config:             &moduleConfig,
			ServiceAccountName: serviceAccountName,
		}

		expected := getBaseWorkerPod("unload", WorkerActionUnload, nmc)

		ctx := context.TODO()

		client.EXPECT().Create(ctx, cmpmock.DiffEq(expected))

		Expect(
			NewPodManager(client, workerImage, scheme).CreateUnloaderPod(ctx, nmc, status),
		).NotTo(
			HaveOccurred(),
		)
	})
})

var _ = Describe("podManager_DeletePod", func() {
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
				NewPodManager(kubeclient, workerImage, scheme).DeletePod(ctx, patchedPod),
			).NotTo(
				HaveOccurred(),
			)
		},
		Entry("deletionTimestamp not set", nil),
		Entry("deletionTimestamp set", &now),
	)
})

var _ = Describe("podManager_ListWorkerPodsOnNode", func() {
	const nodeName = "some-node"

	var (
		ctx = context.TODO()

		kubeClient *testclient.MockClient
		pm         podManager
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = testclient.NewMockClient(ctrl)
		pm = NewPodManager(kubeClient, workerImage, scheme)
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

func getBaseWorkerPod(subcommand string, action WorkerAction, owner ctrlclient.Object) *v1.Pod {
	GinkgoHelper()

	const (
		volNameLibModules     = "lib-modules"
		volNameUsrLibModules  = "usr-lib-modules"
		volNameVarLibFirmware = "var-lib-firmware"
	)

	hostPathDirectory := v1.HostPathDirectory
	hostPathDirectoryOrCreate := v1.HostPathDirectoryOrCreate

	pod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      workerPodName(nmcName, moduleName),
			Namespace: namespace,
			Labels: map[string]string{
				actionLabelKey:            string(action),
				constants.ModuleNameLabel: moduleName,
			},
			Annotations: map[string]string{configAnnotationKey: `containerImage: container image
inTreeModuleToRemove: intree
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
`,
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "worker",
					Image: workerImage,
					Args:  []string{"kmod", subcommand, "/etc/kmm-worker/config.yaml"},
					SecurityContext: &v1.SecurityContext{
						Capabilities: &v1.Capabilities{
							Add: []v1.Capability{"SYS_MODULE"},
						},
					},
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
							Name:      volNameVarLibFirmware,
							MountPath: "/var/lib/firmware",
						},
					},
				},
			},
			NodeName:           nmcName,
			RestartPolicy:      v1.RestartPolicyNever,
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
					Name: volNameVarLibFirmware,
					VolumeSource: v1.VolumeSource{
						HostPath: &v1.HostPathVolumeSource{
							Path: "/var/lib/firmware",
							Type: &hostPathDirectoryOrCreate,
						},
					},
				},
			},
		},
	}

	Expect(
		controllerutil.SetControllerReference(owner, &pod, scheme),
	).NotTo(
		HaveOccurred(),
	)

	controllerutil.AddFinalizer(&pod, nodeModulesConfigFinalizer)

	return &pod
}
