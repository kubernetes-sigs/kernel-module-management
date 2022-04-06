package controllers_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/qbarrand/oot-operator/controllers"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

var _ = Describe("NodeKernelReconciler", func() {
	Describe("Reconcile", func() {
		const (
			kernelVersion = "1.2.3"
			labelName     = "label-name"
			nodeName      = "node-name"
		)

		It("should return an error if the node cannot be found anymore", func() {
			nkr := controllers.NewNodeKernelReconciler(fake.NewClientBuilder().Build(), labelName)
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{Name: nodeName},
			}

			_, err := nkr.Reconcile(context.TODO(), req)
			Expect(err).To(HaveOccurred())
		})

		It("should set the label if it does not exist", func() {
			node := v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: nodeName},
				Status: v1.NodeStatus{
					NodeInfo: v1.NodeSystemInfo{KernelVersion: kernelVersion},
				},
			}

			client := fake.NewClientBuilder().WithObjects(&node).Build()

			nkr := controllers.NewNodeKernelReconciler(client, labelName)
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{Name: nodeName},
			}

			ctx := context.TODO()

			res, err := nkr.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(res))

			updatedNode := v1.Node{}

			err = client.Get(ctx, types.NamespacedName{Name: nodeName}, &updatedNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedNode.Labels).To(HaveKeyWithValue(labelName, kernelVersion))
		})

		It("should set the label if it already exists", func() {
			node := v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   nodeName,
					Labels: map[string]string{kernelVersion: "4.5.6"},
				},
				Status: v1.NodeStatus{
					NodeInfo: v1.NodeSystemInfo{KernelVersion: kernelVersion},
				},
			}

			client := fake.NewClientBuilder().WithObjects(&node).Build()

			nkr := controllers.NewNodeKernelReconciler(client, labelName)
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{Name: nodeName},
			}

			ctx := context.TODO()

			res, err := nkr.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(res))

			updatedNode := v1.Node{}

			err = client.Get(ctx, types.NamespacedName{Name: nodeName}, &updatedNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(updatedNode.Labels).To(HaveKeyWithValue(labelName, kernelVersion))
		})
	})
})

var _ = Describe("NodeKernelReconcilerPredicate", func() {
	const (
		kernelVersion = "1.2.3"
		labelName     = "test-label"
	)

	p := controllers.NodeKernelReconcilerPredicate(labelName)

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
