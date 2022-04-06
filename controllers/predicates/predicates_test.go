package predicates_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/qbarrand/oot-operator/controllers/predicates"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
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
				predicates.HasLabel(label).Delete(event.DeleteEvent{Object: obj}),
			).To(
				Equal(expected),
			)
		},
		Entry("label not set", &appsv1.DaemonSet{}, false),
		Entry("label set to empty value", dsWithEmptyLabel, false),
		Entry("label set to a concrete value", dsWithLabel, true),
	)
})

var _ = Describe("Namespace", func() {
	DescribeTable("should return the expected value",
		func(targetNamespace, resourceNamespace string, expected bool) {
			ev := event.DeleteEvent{
				Object: &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{Namespace: resourceNamespace},
				},
			}

			Expect(
				predicates.Namespace(targetNamespace).Delete(ev),
			).To(
				Equal(expected),
			)
		},
		Entry("matching namespace", "ns", "ns", true),
		Entry("different namespaces", "ns", "other-ns", false),
	)
})

var _ = Describe("SkipDeletions", func() {
	It("should return false for delete events", func() {
		Expect(
			predicates.SkipDeletions.Delete(event.DeleteEvent{}),
		).To(
			BeFalse(),
		)
	})
})
