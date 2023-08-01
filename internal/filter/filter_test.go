package filter

import (
	"context"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
)

var (
	mockCtrl *gomock.Controller
	clnt     *mockClient.MockClient
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

var _ = Describe("ModuleReconcilerNodePredicate", func() {
	const kernelLabel = "kernel-label"
	var p predicate.Predicate

	BeforeEach(func() {
		p = New(nil).ModuleReconcilerNodePredicate(kernelLabel)
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

var _ = Describe("NodeKernelReconcilerPredicate", func() {
	const (
		kernelVersion = "1.2.3"
		labelName     = "test-label"
	)

	var p predicate.Predicate

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		p = New(nil).NodeKernelReconcilerPredicate(labelName)
	})

	It("should return true if the node has no labels", func() {
		ev := event.CreateEvent{
			Object: &v1.Node{
				Status: v1.NodeStatus{
					NodeInfo: v1.NodeSystemInfo{KernelVersion: kernelVersion},
				},
			},
		}

		Expect(
			p.Create(ev),
		).To(
			BeTrue(),
		)
	})

	It("should return true if the node has the wrong label value", func() {
		ev := event.CreateEvent{
			Object: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{labelName: "some-other-value"},
				},
				Status: v1.NodeStatus{
					NodeInfo: v1.NodeSystemInfo{KernelVersion: kernelVersion},
				},
			},
		}

		Expect(
			p.Create(ev),
		).To(
			BeTrue(),
		)
	})

	It("should return false if the node has the wrong label value but event is deletion", func() {
		ev := event.DeleteEvent{
			Object: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{labelName: labelName},
				},
				Status: v1.NodeStatus{
					NodeInfo: v1.NodeSystemInfo{KernelVersion: kernelVersion},
				},
			},
		}

		Expect(
			p.Delete(ev),
		).To(
			BeFalse(),
		)
	})

	It("should return false if the label is correctly set", func() {
		ev := event.CreateEvent{
			Object: &v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{labelName: kernelVersion},
				},
				Status: v1.NodeStatus{
					NodeInfo: v1.NodeSystemInfo{KernelVersion: kernelVersion},
				},
			},
		}

		Expect(
			p.Create(ev),
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
	})

	ctx := context.Background()

	It("should return nothing if there are no modules", func() {
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any())

		p := New(clnt)
		Expect(
			p.FindModulesForNode(ctx, &v1.Node{}),
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

		p := New(clnt)

		Expect(
			p.FindModulesForNode(ctx, &v1.Node{}),
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

		p := New(clnt)

		expectedReq := reconcile.Request{
			NamespacedName: types.NamespacedName{Name: mod1Name},
		}

		reqs := p.FindModulesForNode(ctx, &node)
		Expect(reqs).To(Equal([]reconcile.Request{expectedReq}))
	})
})

var _ = Describe("FindManagedClusterModulesForCluster", func() {
	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		clnt = mockClient.NewMockClient(mockCtrl)
	})

	ctx := context.Background()

	It("should return nothing if there are no ManagedClusterModules", func() {
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any())

		p := New(clnt)
		Expect(
			p.FindManagedClusterModulesForCluster(ctx, &clusterv1.ManagedCluster{}),
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

		p := New(clnt)

		Expect(
			p.FindManagedClusterModulesForCluster(ctx, &clusterv1.ManagedCluster{}),
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

		p := New(clnt)

		expectedReq := reconcile.Request{
			NamespacedName: types.NamespacedName{Name: matchingMod.Name},
		}

		reqs := p.FindManagedClusterModulesForCluster(ctx, &cluster)
		Expect(reqs).To(Equal([]reconcile.Request{expectedReq}))
	})
})

var _ = Describe("ManagedClusterModuleReconcilerManagedClusterPredicate", func() {
	var p predicate.Predicate

	BeforeEach(func() {
		p = New(nil).ManagedClusterModuleReconcilerManagedClusterPredicate()
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
	})

	ctx := context.Background()

	It("no preflight exists", func() {
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any())

		p := New(clnt)

		res := p.EnqueueAllPreflightValidations(ctx, &kmmv1beta1.Module{})
		Expect(res).To(BeEmpty())
	})

	It("preflight exists", func() {
		preflight := kmmv1beta1.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "preflight",
				Namespace: "preflightNamespace",
			},
		}

		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *kmmv1beta1.PreflightValidationList, _ ...interface{}) error {
				list.Items = []kmmv1beta1.PreflightValidation{preflight}
				return nil
			},
		)

		expectedRes := []reconcile.Request{
			reconcile.Request{
				NamespacedName: types.NamespacedName{Name: preflight.Name, Namespace: preflight.Namespace},
			},
		}

		p := New(clnt)
		res := p.EnqueueAllPreflightValidations(ctx, &kmmv1beta1.Module{})
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
