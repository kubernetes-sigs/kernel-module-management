package controllers_test

import (
	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	"github.com/qbarrand/oot-operator/controllers"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("FindModulesForNode", func() {
	It("should return nothing if there are no modules", func() {
		nmm := controllers.NewNodeModuleMapper(
			fake.NewClientBuilder().WithScheme(scheme).Build(),
			logr.Discard(),
		)

		Expect(
			nmm.FindModulesForNode(&v1.Node{}),
		).To(
			BeEmpty(),
		)
	})

	It("should return nothing if the node labels match no module", func() {
		mod := ootov1beta1.Module{
			Spec: ootov1beta1.ModuleSpec{
				Selector: map[string]string{"key": "value"},
			},
		}

		nmm := controllers.NewNodeModuleMapper(
			fake.NewClientBuilder().WithScheme(scheme).WithObjects(&mod).Build(),
			logr.Discard(),
		)

		Expect(
			nmm.FindModulesForNode(&v1.Node{}),
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

		mod1 := ootov1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: mod1Name},
			Spec:       ootov1beta1.ModuleSpec{Selector: nodeLabels},
		}

		mod2 := ootov1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "mod2"},
			Spec: ootov1beta1.ModuleSpec{
				Selector: map[string]string{"other-key": "other-value"},
			},
		}

		nmm := controllers.NewNodeModuleMapper(
			fake.NewClientBuilder().WithScheme(scheme).WithObjects(&mod1, &mod2, &node).Build(),
			logr.Discard(),
		)

		expectedReq := reconcile.Request{
			NamespacedName: types.NamespacedName{Name: mod1Name},
		}

		reqs := nmm.FindModulesForNode(&node)
		Expect(reqs).To(Equal([]reconcile.Request{expectedReq}))
	})
})
