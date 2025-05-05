package filter

import (
	"context"
	"time"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	hubv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	mockClient "github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/nmc"
)

var (
	mockCtrl  *gomock.Controller
	clnt      *mockClient.MockClient
	nmcHelper *nmc.MockHelper
	f         *Filter
)

var _ = Describe("HasLabel", func() {
	const label = "test-label"

	dsWithEmptyLabel := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{label: ""},
		},
	}

	dsWithLabel := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{label: "some-module"},
		},
	}

	DescribeTable("Should return the expected value",
		func(obj client.Object, expected bool) {
			Expect(
				HasLabel(label).Delete(event.DeleteEvent{Object: obj}),
			).To(
				Equal(expected),
			)
		},
		Entry("label not set", &appsv1.DaemonSet{}, false),
		Entry("label set to empty value", dsWithEmptyLabel, false),
		Entry("label set to a concrete value", dsWithLabel, true),
	)
})

var _ = Describe("skipDeletions", func() {
	It("should return false for delete events", func() {
		Expect(
			skipDeletions.Delete(event.DeleteEvent{}),
		).To(
			BeFalse(),
		)
	})
})

var _ = Describe("skipCreations", func() {
	It("should return false for create events", func() {
		Expect(
			skipCreations.Create(event.CreateEvent{}),
		).To(
			BeFalse(),
		)
	})
})

var _ = Describe("nodeBecomesSchedulable", func() {
	DescribeTable("should work as expected", func(oldNodeSchedulable, newNodeSchedulable, expectedRes bool) {
		oldNode := v1.Node{}
		newNode := v1.Node{}
		if !oldNodeSchedulable {
			oldNode.Spec.Taints = []v1.Taint{
				v1.Taint{
					Effect: v1.TaintEffectNoSchedule,
				},
			}
		}
		if !newNodeSchedulable {
			newNode.Spec.Taints = []v1.Taint{
				v1.Taint{
					Effect: v1.TaintEffectNoSchedule,
				},
			}
		}

		res := nodeBecomesSchedulable.Update(event.UpdateEvent{ObjectOld: &oldNode, ObjectNew: &newNode})
		Expect(res).To(Equal(expectedRes))

	},
		Entry("old schedulable, new schedulable", true, true, false),
		Entry("old schedulable, new non-schedulable", true, false, false),
		Entry("old non-schedulable, new non-schedulable", false, false, false),
		Entry("old non-schedulable, new schedulable", false, true, true),
	)
})

var _ = Describe("filterRelevantNodeUpdates", func() {

	It("should return false if there's no relevant change", func() {
		oldNode := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"node-role.kubernetes.io/worker": "true"},
			},
			Spec: v1.NodeSpec{
				Unschedulable: false,
			},
			Status: v1.NodeStatus{
				NodeInfo: v1.NodeSystemInfo{
					KernelVersion: "v1",
				},
				Conditions: []v1.NodeCondition{
					{
						Type:   v1.NodeReady,
						Status: v1.ConditionTrue,
					},
				},
			},
		}

		// New node with just a heartbeat difference (which is ignored now)
		newNode := oldNode.DeepCopy()
		newNode.Status.Conditions[0].LastHeartbeatTime = metav1.NewTime(time.Now())

		res := filterRelevantNodeUpdates().Update(event.UpdateEvent{ObjectOld: &oldNode, ObjectNew: newNode})
		Expect(res).To(BeFalse())
	})

	It("should return true if spec changed", func() {
		oldNode := v1.Node{}
		newNode := v1.Node{
			Spec: v1.NodeSpec{
				Unschedulable: true,
			},
		}

		res := filterRelevantNodeUpdates().Update(event.UpdateEvent{ObjectOld: &oldNode, ObjectNew: &newNode})
		Expect(res).To(BeTrue())
	})

	It("should return true if labels changed", func() {
		oldNode := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"a": "1"},
			},
		}
		newNode := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"a": "2"},
			},
		}

		res := filterRelevantNodeUpdates().Update(event.UpdateEvent{ObjectOld: &oldNode, ObjectNew: &newNode})
		Expect(res).To(BeTrue())
	})

	It("should return true if NodeInfo changed", func() {
		oldNode := v1.Node{
			Status: v1.NodeStatus{
				NodeInfo: v1.NodeSystemInfo{KernelVersion: "1"},
			},
		}
		newNode := v1.Node{
			Status: v1.NodeStatus{
				NodeInfo: v1.NodeSystemInfo{KernelVersion: "2"},
			},
		}

		res := filterRelevantNodeUpdates().Update(event.UpdateEvent{ObjectOld: &oldNode, ObjectNew: &newNode})
		Expect(res).To(BeTrue())
	})

	It("should return true if Ready condition transitioned from NotReady to Ready", func() {
		oldNode := v1.Node{
			Status: v1.NodeStatus{
				Conditions: []v1.NodeCondition{
					{
						Type:   v1.NodeReady,
						Status: v1.ConditionFalse,
					},
				},
			},
		}
		newNode := v1.Node{
			Status: v1.NodeStatus{
				Conditions: []v1.NodeCondition{
					{
						Type:   v1.NodeReady,
						Status: v1.ConditionTrue,
					},
				},
			},
		}

		res := filterRelevantNodeUpdates().Update(event.UpdateEvent{ObjectOld: &oldNode, ObjectNew: &newNode})
		Expect(res).To(BeTrue())
	})

	It("should return false if Ready condition stays True", func() {
		oldNode := v1.Node{
			Status: v1.NodeStatus{
				Conditions: []v1.NodeCondition{
					{
						Type:   v1.NodeReady,
						Status: v1.ConditionTrue,
					},
				},
			},
		}
		newNode := v1.Node{
			Status: v1.NodeStatus{
				Conditions: []v1.NodeCondition{
					{
						Type:               v1.NodeReady,
						Status:             v1.ConditionTrue,
						LastHeartbeatTime:  metav1.NewTime(time.Now()),
						LastTransitionTime: metav1.NewTime(time.Now()),
					},
				},
			},
		}

		res := filterRelevantNodeUpdates().Update(event.UpdateEvent{ObjectOld: &oldNode, ObjectNew: &newNode})
		Expect(res).To(BeFalse())
	})
})

var _ = Describe("modulePodSuccess", func() {
	DescribeTable("should work as expected", func(podControlledByModule, podSucceeded, expectedRes bool) {
		newPod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "somePod", Namespace: "moduleNamespace"},
		}
		if podSucceeded {
			newPod.Status.Phase = v1.PodSucceeded
		}

		res := modulePodSuccess.Update(event.UpdateEvent{ObjectNew: &newPod})
		Expect(res).To(Equal(expectedRes))

	},
		Entry("pod succeeded", true, true, true),
		Entry("pod not succeeded", true, false, false),
	)
})

var _ = Describe("kmmClusterClaimChanged", func() {
	updateFunc := kmmClusterClaimChanged.Update

	managedCluster1 := clusterv1.ManagedCluster{
		Status: clusterv1.ManagedClusterStatus{
			ClusterClaims: []clusterv1.ManagedClusterClaim{
				{
					Name:  constants.KernelVersionsClusterClaimName,
					Value: "a-kernel-version",
				},
			},
		},
	}
	managedCluster2 := clusterv1.ManagedCluster{
		Status: clusterv1.ManagedClusterStatus{
			ClusterClaims: []clusterv1.ManagedClusterClaim{
				{
					Name:  constants.KernelVersionsClusterClaimName,
					Value: "another-kernel-version",
				},
			},
		},
	}

	DescribeTable(
		"should work as expected",
		func(updateEvent event.UpdateEvent, expectedResult bool) {
			Expect(
				updateFunc(updateEvent),
			).To(
				Equal(expectedResult),
			)
		},
		Entry(nil, event.UpdateEvent{ObjectOld: &v1.Pod{}, ObjectNew: &clusterv1.ManagedCluster{}}, false),
		Entry(nil, event.UpdateEvent{ObjectOld: &clusterv1.ManagedCluster{}, ObjectNew: &v1.Pod{}}, false),
		Entry(nil, event.UpdateEvent{ObjectOld: &managedCluster1, ObjectNew: &clusterv1.ManagedCluster{}}, false),
		Entry(nil, event.UpdateEvent{ObjectOld: &managedCluster1, ObjectNew: &managedCluster1}, false),
		Entry(nil, event.UpdateEvent{ObjectOld: &managedCluster1, ObjectNew: &managedCluster2}, true),
	)
})

var _ = Describe("ListModulesForNMC", func() {
	const (
		namespace0 = "namespace0"
		name0      = "name0"
		namespace1 = "namespace1"
		name1      = "name1"
		namespace2 = "namespace2"
		name2      = "name2"
	)

	It("should work as expected", func() {
		nmcObj := &kmmv1beta1.NodeModulesConfig{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"abc": "def",
					nmc.ModuleConfiguredLabel(namespace0, name0): "",
					nmc.ModuleInUseLabel(namespace0, name0):      "",
					nmc.ModuleConfiguredLabel(namespace1, name1): "",
					nmc.ModuleInUseLabel(namespace2, name2):      "",
				},
			},
		}

		Expect(
			ListModulesForNMC(context.TODO(), nmcObj),
		).To(
			ConsistOf(
				reconcile.Request{NamespacedName: types.NamespacedName{Namespace: namespace0, Name: name0}},
				reconcile.Request{NamespacedName: types.NamespacedName{Namespace: namespace1, Name: name1}},
				reconcile.Request{NamespacedName: types.NamespacedName{Namespace: namespace2, Name: name2}},
			),
		)
	})
})

var _ = Describe("ModuleReconcilerNodePredicate", func() {
	const kernelLabel = "kernel-label"
	var p predicate.Predicate

	BeforeEach(func() {
		f = New(nil, nil)
		p = f.ModuleReconcilerNodePredicate(kernelLabel)
	})

	It("should return true for creations", func() {
		ev := event.CreateEvent{
			Object: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{kernelLabel: "1.2.3"},
				},
			},
		}

		Expect(
			p.Create(ev),
		).To(
			BeTrue(),
		)
	})

	It("should return true for label updates", func() {
		ev := event.UpdateEvent{
			ObjectOld: &v1.Node{},
			ObjectNew: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{kernelLabel: "1.2.3"},
				},
			},
		}

		Expect(
			p.Update(ev),
		).To(
			BeTrue(),
		)
	})
	It("should return false for label updates without the expected label", func() {
		ev := event.UpdateEvent{
			ObjectOld: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"a": "b"},
				},
			},
			ObjectNew: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"c": "d"},
				},
			},
		}

		Expect(
			p.Update(ev),
		).To(
			BeFalse(),
		)
	})

	It("should return false for deletions", func() {
		ev := event.DeleteEvent{
			Object: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{kernelLabel: "1.2.3"},
				},
			},
		}

		Expect(
			p.Delete(ev),
		).To(
			BeFalse(),
		)
	})
})

var _ = Describe("NodeUpdateKernelChangedPredicate", func() {
	updateFunc := NodeUpdateKernelChangedPredicate().Update

	node1 := v1.Node{
		Status: v1.NodeStatus{
			NodeInfo: v1.NodeSystemInfo{KernelVersion: "v1"},
		},
	}

	node2 := v1.Node{
		Status: v1.NodeStatus{
			NodeInfo: v1.NodeSystemInfo{KernelVersion: "v2"},
		},
	}

	DescribeTable(
		"should work as expected",
		func(updateEvent event.UpdateEvent, expectedResult bool) {
			Expect(
				updateFunc(updateEvent),
			).To(
				Equal(expectedResult),
			)
		},
		Entry(nil, event.UpdateEvent{ObjectOld: &v1.Pod{}, ObjectNew: &v1.Node{}}, false),
		Entry(nil, event.UpdateEvent{ObjectOld: &v1.Node{}, ObjectNew: &v1.Pod{}}, false),
		Entry(nil, event.UpdateEvent{ObjectOld: &v1.Node{}, ObjectNew: &v1.Pod{}}, false),
		Entry(nil, event.UpdateEvent{ObjectOld: &node1, ObjectNew: &node1}, false),
		Entry(nil, event.UpdateEvent{ObjectOld: &node1, ObjectNew: &node2}, true),
	)
})

var _ = Describe("FindModulesForNode", func() {
	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		clnt = mockClient.NewMockClient(mockCtrl)
		f = New(clnt, nil)
	})

	ctx := context.Background()

	It("should return nothing if there are no modules", func() {
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any())

		Expect(
			f.FindModulesForNode(ctx, &v1.Node{}),
		).To(
			BeEmpty(),
		)
	})

	It("should return nothing if the node labels match no module", func() {
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				Selector: map[string]string{"key": "value"},
			},
		}

		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
				list.Items = []kmmv1beta1.Module{mod}
				return nil
			},
		)

		Expect(
			f.FindModulesForNode(ctx, &v1.Node{}),
		).To(
			BeEmpty(),
		)
	})

	It("should return only modules matching the node", func() {
		nodeLabels := map[string]string{"key": "value"}

		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{Labels: nodeLabels},
		}

		const mod1Name = "mod1"

		mod1 := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: mod1Name},
			Spec:       kmmv1beta1.ModuleSpec{Selector: nodeLabels},
		}

		mod2 := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "mod2"},
			Spec: kmmv1beta1.ModuleSpec{
				Selector: map[string]string{"other-key": "other-value"},
			},
		}
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
				list.Items = []kmmv1beta1.Module{mod1, mod2}
				return nil
			},
		)

		expectedReq := reconcile.Request{
			NamespacedName: types.NamespacedName{Name: mod1Name},
		}

		reqs := f.FindModulesForNode(ctx, &node)
		Expect(reqs).To(Equal([]reconcile.Request{expectedReq}))
	})
})

var _ = Describe("FindModulesForNMCNodeChange", func() {
	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		clnt = mockClient.NewMockClient(mockCtrl)
		nmcHelper = nmc.NewMockHelper(mockCtrl)
		f = New(clnt, nmcHelper)
	})

	ctx := context.Background()

	It("should return nothing if there are no modules and no NMC", func() {
		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "some node"},
		}
		gomock.InOrder(
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).Return(nil),
			nmcHelper.EXPECT().Get(ctx, node.Name).Return(nil, apierrors.NewNotFound(schema.GroupResource{}, node.Name)),
		)

		res := f.FindModulesForNMCNodeChange(ctx, node)
		Expect(res).To(BeEmpty())
	})

	It("should return nothing if the node labels match no module and NMC spec is empty", func() {
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				Selector: map[string]string{"key": "value"},
			},
		}
		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "some node"},
		}

		nmc := kmmv1beta1.NodeModulesConfig{}

		gomock.InOrder(
			clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.Module{mod}
					return nil
				},
			),
			nmcHelper.EXPECT().Get(ctx, node.Name).Return(&nmc, nil),
		)
		res := f.FindModulesForNMCNodeChange(ctx, node)
		Expect(res).To(BeEmpty())
	})

	It("should return modules matching node's label and modules in the NMC spec", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "module name", Namespace: "module namespace"},
			Spec: kmmv1beta1.ModuleSpec{
				Selector: map[string]string{"key": "value"},
			},
		}
		nodeLabels := map[string]string{"key": "value"}
		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: "some node", Labels: nodeLabels},
		}

		nmc := kmmv1beta1.NodeModulesConfig{
			Spec: kmmv1beta1.NodeModulesConfigSpec{
				Modules: []kmmv1beta1.NodeModuleSpec{
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Name:      "new module name",
							Namespace: "new module namespace",
						},
					},
					{
						ModuleItem: kmmv1beta1.ModuleItem{
							Name:      "module name",
							Namespace: "module namespace",
						},
					},
				},
			},
		}

		gomock.InOrder(
			clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.Module{mod}
					return nil
				},
			),
			nmcHelper.EXPECT().Get(ctx, node.Name).Return(&nmc, nil),
		)
		res := f.FindModulesForNMCNodeChange(ctx, node)

		expectedReq := []reconcile.Request{
			{
				NamespacedName: types.NamespacedName{Name: mod.Name, Namespace: mod.Namespace},
			},
			{
				NamespacedName: types.NamespacedName{Name: "new module name", Namespace: "new module namespace"},
			},
		}
		Expect(res).Should(ConsistOf(expectedReq))
	})
})

var _ = Describe("FindManagedClusterModulesForCluster", func() {
	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		clnt = mockClient.NewMockClient(mockCtrl)
		f = New(clnt, nil)
	})

	ctx := context.Background()

	It("should return nothing if there are no ManagedClusterModules", func() {
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any())

		Expect(
			f.FindManagedClusterModulesForCluster(ctx, &clusterv1.ManagedCluster{}),
		).To(
			BeEmpty(),
		)
	})

	It("should return nothing if the cluster labels match no ManagedClusterModule", func() {
		mod := hubv1beta1.ManagedClusterModule{
			Spec: hubv1beta1.ManagedClusterModuleSpec{
				Selector: map[string]string{"key": "value"},
			},
		}

		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *hubv1beta1.ManagedClusterModuleList, _ ...interface{}) error {
				list.Items = []hubv1beta1.ManagedClusterModule{mod}
				return nil
			},
		)

		Expect(
			f.FindManagedClusterModulesForCluster(ctx, &clusterv1.ManagedCluster{}),
		).To(
			BeEmpty(),
		)
	})

	It("should return only ManagedClusterModules matching the cluster", func() {
		clusterLabels := map[string]string{"key": "value"}

		cluster := clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Labels: clusterLabels,
			},
		}

		matchingMod := hubv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{Name: "matching-mod"},
			Spec: hubv1beta1.ManagedClusterModuleSpec{
				Selector: clusterLabels,
			},
		}

		mod := hubv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{Name: "mod"},
			Spec: hubv1beta1.ManagedClusterModuleSpec{
				Selector: map[string]string{"other-key": "other-value"},
			},
		}

		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *hubv1beta1.ManagedClusterModuleList, _ ...interface{}) error {
				list.Items = []hubv1beta1.ManagedClusterModule{matchingMod, mod}
				return nil
			},
		)

		expectedReq := reconcile.Request{
			NamespacedName: types.NamespacedName{Name: matchingMod.Name},
		}

		reqs := f.FindManagedClusterModulesForCluster(ctx, &cluster)
		Expect(reqs).To(Equal([]reconcile.Request{expectedReq}))
	})
})

var _ = Describe("ManagedClusterModuleReconcilerManagedClusterPredicate", func() {
	var p predicate.Predicate

	BeforeEach(func() {
		f = New(nil, nil)
		p = f.ManagedClusterModuleReconcilerManagedClusterPredicate()
	})

	It("should return true for creations", func() {
		ev := event.CreateEvent{
			Object: &clusterv1.ManagedCluster{},
		}

		Expect(
			p.Create(ev),
		).To(
			BeTrue(),
		)
	})

	It("should return true for deletions", func() {
		ev := event.DeleteEvent{
			Object: &clusterv1.ManagedCluster{},
		}

		Expect(
			p.Delete(ev),
		).To(
			BeTrue(),
		)
	})

	It("should return true for label updates", func() {
		ev := event.UpdateEvent{
			ObjectOld: &clusterv1.ManagedCluster{},
			ObjectNew: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"key": "value"},
				},
			},
		}

		Expect(
			p.Update(ev),
		).To(
			BeTrue(),
		)
	})

	It("should return true for KMM ClusterClaim updates", func() {
		ev := event.UpdateEvent{
			ObjectOld: &clusterv1.ManagedCluster{},
			ObjectNew: &clusterv1.ManagedCluster{
				Status: clusterv1.ManagedClusterStatus{
					ClusterClaims: []clusterv1.ManagedClusterClaim{
						{
							Name:  constants.KernelVersionsClusterClaimName,
							Value: "a-kernel-version",
						},
					},
				},
			},
		}

		Expect(
			p.Update(ev),
		).To(
			BeTrue(),
		)
	})
})

var _ = Describe("DeletingPredicate", func() {
	now := metav1.Now()

	DescribeTable("should return the expected value",
		func(o client.Object, expected bool) {
			Expect(
				DeletingPredicate().Generic(event.GenericEvent{Object: o}),
			).To(
				Equal(expected),
			)
		},
		Entry(nil, &v1.Pod{ObjectMeta: metav1.ObjectMeta{DeletionTimestamp: &now}}, true),
		Entry(nil, &v1.Pod{}, false),
	)
})

var _ = Describe("PodReadinessChangedPredicate", func() {
	p := PodReadinessChangedPredicate(logr.Discard())

	DescribeTable(
		"should return the expected value",
		func(e event.UpdateEvent, expected bool) {
			Expect(p.Update(e)).To(Equal(expected))
		},
		Entry("objects are nil", event.UpdateEvent{}, true),
		Entry("old object is not a Pod", event.UpdateEvent{ObjectOld: &v1.Node{}}, true),
		Entry(
			"new object is not a Pod",
			event.UpdateEvent{
				ObjectOld: &v1.Pod{},
				ObjectNew: &v1.Node{},
			},
			true,
		),
		Entry(
			"both objects are pods with the same conditions",
			event.UpdateEvent{
				ObjectOld: &v1.Pod{},
				ObjectNew: &v1.Pod{},
			},
			false,
		),
		Entry(
			"both objects are pods with different conditions",
			event.UpdateEvent{
				ObjectOld: &v1.Pod{},
				ObjectNew: &v1.Pod{
					Status: v1.PodStatus{
						Conditions: []v1.PodCondition{
							{
								Type:   v1.PodReady,
								Status: v1.ConditionTrue,
							},
						},
					},
				},
			},
			true,
		),
	)
})

var _ = Describe("FindPreflightsForModule", func() {

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		clnt = mockClient.NewMockClient(mockCtrl)
		f = New(clnt, nil)
	})

	ctx := context.Background()

	It("no preflight exists", func() {
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any())

		res := f.EnqueueAllPreflightValidations(ctx, &kmmv1beta1.Module{})
		Expect(res).To(BeEmpty())
	})

	It("preflight exists", func() {
		preflight := kmmv1beta1.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{Name: "preflight"},
		}

		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *kmmv1beta1.PreflightValidationList, _ ...interface{}) error {
				list.Items = []kmmv1beta1.PreflightValidation{preflight}
				return nil
			},
		)

		expectedRes := []reconcile.Request{
			reconcile.Request{
				NamespacedName: types.NamespacedName{Name: preflight.Name},
			},
		}

		res := f.EnqueueAllPreflightValidations(ctx, &kmmv1beta1.Module{})
		Expect(res).To(Equal(expectedRes))
	})

})

var _ = Describe("nodeBecomesSchedulable", func() {
	var (
		oldNode v1.Node
		newNode *v1.Node
	)

	BeforeEach(func() {
		oldNode = v1.Node{
			Status: v1.NodeStatus{
				Conditions: []v1.NodeCondition{
					{
						Type:   v1.NodeMemoryPressure,
						Status: v1.ConditionFalse,
					},
				},
			},
		}
		newNode = oldNode.DeepCopy()
	})

	nonSchedulableTaint := v1.Taint{
		Effect: v1.TaintEffectNoSchedule,
	}

	DescribeTable("Should return the expected value", func(oldTaint *v1.Taint, newTaint *v1.Taint, expected bool) {
		if newTaint != nil {
			newNode.Spec.Taints = append(newNode.Spec.Taints, *newTaint)
		}
		if oldTaint != nil {
			oldNode.Spec.Taints = append(oldNode.Spec.Taints, *oldTaint)
		}

		res := nodeBecomesSchedulable.Update(event.UpdateEvent{ObjectOld: &oldNode, ObjectNew: newNode})
		if expected {
			Expect(res).To(BeTrue())
		} else {
			Expect(res).To(BeFalse())
		}
	},
		Entry("old Schedulable, new Schedulable", nil, nil, false),
		Entry("old NonSchedulable, new NonSchedulable", &nonSchedulableTaint, &nonSchedulableTaint, false),
		Entry("old Schedulable, new NonSchedulable", nil, &nonSchedulableTaint, false),
		Entry("old NonSchedulable, new Schedulable", &nonSchedulableTaint, nil, true),
	)
})
