package module

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("SetAs{Ready,Progressing,Errored}", func() {
	const (
		name      = "sr-name"
		namespace = "sr-namespace"
	)

	var (
		c   ctrlclient.StatusWriter
		mod *ootov1alpha1.Module
	)

	BeforeEach(func() {
		mod = &ootov1alpha1.Module{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace}}

		c = fake.
			NewClientBuilder().
			WithScheme(scheme).
			WithObjects(mod).
			Build()
	})

	DescribeTable("Setting one condition to true, should set others to false",
		func(expectedType string, call func(cu ConditionsUpdater) error) {
			cu := NewConditionsUpdater(c)

			Expect(
				call(cu),
			).
				To(
					Succeed(),
				)

			for _, cond := range mod.Status.Conditions {
				if cond.Type == expectedType {
					Expect(cond.Status).To(Equal(metav1.ConditionTrue))
				} else {
					Expect(cond.Status).NotTo(Equal(metav1.ConditionTrue))
				}
			}

			// Make sure Conditions are set for object that was passed in and visible outside
			Expect(mod.Status.Conditions).To(HaveLen(3))
		},
		Entry("Ready",
			ready,
			func(su ConditionsUpdater) error { return su.SetAsReady(context.Background(), mod, "x", "x") },
		),
		Entry("Errored",
			errored,
			func(su ConditionsUpdater) error { return su.SetAsErrored(context.Background(), mod, "x", "x") },
		),
		Entry("Progressing",
			progressing,
			func(cu ConditionsUpdater) error {
				return cu.SetAsProgressing(context.Background(), mod, "x", "x")
			},
		),
	)
})
