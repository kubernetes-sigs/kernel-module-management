package controllers_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/qbarrand/oot-operator/controllers"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var _ = Describe("DaemonSetReconciler", func() {
	Describe("Reconcile", func() {
		const (
			dsName      = "some-ds"
			dsNamespace = "some-namespace"
		)

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{Name: dsName, Namespace: dsNamespace},
		}

		It("should return an error if the DaemonSet cannot be found", func() {
			dsr := controllers.NewDaemonSetReconciler(
				fake.NewClientBuilder().Build(),
			)

			_, err := dsr.Reconcile(context.TODO(), req)
			Expect(err).To(HaveOccurred())
		})

		DescribeTable("should delete the DaemonSet or not",
			func(creationTime metav1.Time, desired int32, listLength int) {
				ds := appsv1.DaemonSet{
					ObjectMeta: metav1.ObjectMeta{
						Name:              dsName,
						Namespace:         dsNamespace,
						CreationTimestamp: creationTime,
					},
					Status: appsv1.DaemonSetStatus{
						DesiredNumberScheduled: desired,
					},
				}

				client := fake.NewClientBuilder().WithObjects(&ds).Build()

				dsr := controllers.NewDaemonSetReconciler(client)

				ctx := context.TODO()

				res, err := dsr.Reconcile(ctx, req)
				Expect(err).NotTo(HaveOccurred())
				Expect(res).To(Equal(ctrl.Result{}))

				dsList := appsv1.DaemonSetList{}

				err = client.List(ctx, &dsList)
				Expect(err).NotTo(HaveOccurred())
				Expect(dsList.Items).To(HaveLen(listLength))
			},
			Entry("Young DS, 0 desired", metav1.Now(), int32(0), 1),
			Entry("Young DS, 1 desired", metav1.Now(), int32(1), 1),
			Entry("Old DS, 1 desired", metav1.NewTime(metav1.Now().Add(-1*time.Hour)), int32(1), 1),
			Entry("Old DS, 0 desired", metav1.NewTime(metav1.Now().Add(-1*time.Hour)), int32(0), 0),
		)
	})
})

var _ = Describe("ModuleLabelNotEmptyFilter", func() {
	dsWithEmptyLabel := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{controllers.ModuleNameLabel: ""},
		},
	}

	dsWithLabel := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{controllers.ModuleNameLabel: "some-module"},
		},
	}

	DescribeTable("Should return the expected value",
		func(obj client.Object, expected bool) {
			Expect(
				controllers.ModuleLabelNotEmptyFilter(obj),
			).To(
				Equal(expected),
			)
		},
		Entry("label not set", &appsv1.DaemonSet{}, false),
		Entry("label set to empty value", dsWithEmptyLabel, false),
		Entry("label set to a concrete value", dsWithLabel, true),
	)
})
