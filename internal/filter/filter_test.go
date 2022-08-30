package filter

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kmmv1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	mockClient "github.com/qbarrand/oot-operator/internal/client"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	ctrl *gomock.Controller
	clnt *mockClient.MockClient
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

var _ = Describe("ModuleReconcilerNodePredicate", func() {
	const kernelLabel = "kernel-label"
	var p predicate.Predicate

	BeforeEach(func() {
		p = New(nil, logr.Discard()).ModuleReconcilerNodePredicate(kernelLabel)
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
		ctrl = gomock.NewController(GinkgoT())
		p = New(nil, logr.Discard()).NodeKernelReconcilerPredicate(labelName)
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

var _ = Describe("FindModulesForNode", func() {
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = mockClient.NewMockClient(ctrl)
	})

	It("should return nothing if there are no modules", func() {
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any())

		p := New(clnt, logr.Discard())
		Expect(
			p.FindModulesForNode(&v1.Node{}),
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

		p := New(clnt, logr.Discard())

		Expect(
			p.FindModulesForNode(&v1.Node{}),
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

		p := New(clnt, logr.Discard())

		expectedReq := reconcile.Request{
			NamespacedName: types.NamespacedName{Name: mod1Name},
		}

		reqs := p.FindModulesForNode(&node)
		Expect(reqs).To(Equal([]reconcile.Request{expectedReq}))
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

var _ = Describe("PodHasSpecNodeName", func() {
	p := PodHasSpecNodeName()

	DescribeTable(
		"should return the expected value",
		func(o client.Object, expected bool) {
			Expect(
				p.Create(event.CreateEvent{Object: o}),
			).To(
				Equal(expected),
			)
		},
		Entry("ConfigMap: false", &v1.ConfigMap{}, false),
		Entry("Pod with no nodeName: false", &v1.Pod{}, false),
		Entry("Pod with a nodeName: true", &v1.Pod{Spec: v1.PodSpec{NodeName: "test"}}, true),
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
		ctrl = gomock.NewController(GinkgoT())
		clnt = mockClient.NewMockClient(ctrl)
	})

	It("no preflight exists", func() {
		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any())

		p := New(clnt, logr.Discard())

		res := p.EnqueueAllPreflightValidations(&kmmv1beta1.Module{})
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

		p := New(clnt, logr.Discard())
		res := p.EnqueueAllPreflightValidations(&kmmv1beta1.Module{})
		Expect(res).To(Equal(expectedRes))
	})

})
