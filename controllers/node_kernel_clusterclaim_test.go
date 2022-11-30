package controllers

import (
	"context"

	"github.com/golang/mock/gomock"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"open-cluster-management.io/api/cluster/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("NodeKernelClusterClaimReconciler_Reconcile", func() {
	var (
		gCtrl      *gomock.Controller
		kubeClient *client.MockClient
	)

	BeforeEach(func() {
		gCtrl = gomock.NewController(GinkgoT())
		kubeClient = client.NewMockClient(gCtrl)
	})

	It("should work as expected", func() {
		const ccName = "kernel-versions.kmm.node.kubernetes.io"

		ctx := context.Background()

		cc := v1alpha1.ClusterClaim{
			ObjectMeta: metav1.ObjectMeta{Name: ccName},
		}

		initialCC := cc.DeepCopy()

		cc.Spec.Value = "a1\na2\nb"

		gomock.InOrder(
			kubeClient.
				EXPECT().
				List(ctx, &v1.NodeList{}).
				Do(func(_ context.Context, nl *v1.NodeList, _ ...metav1.ListOptions) {
					nl.Items = []v1.Node{
						{
							Status: v1.NodeStatus{
								NodeInfo: v1.NodeSystemInfo{KernelVersion: "b"},
							},
						},
						{
							Status: v1.NodeStatus{
								NodeInfo: v1.NodeSystemInfo{KernelVersion: "a1"},
							},
						},
						{
							Status: v1.NodeStatus{
								NodeInfo: v1.NodeSystemInfo{KernelVersion: "a2"},
							},
						},
						{
							Status: v1.NodeStatus{
								NodeInfo: v1.NodeSystemInfo{KernelVersion: "b"},
							},
						},
						{
							Status: v1.NodeStatus{
								NodeInfo: v1.NodeSystemInfo{KernelVersion: "a1"},
							},
						},
					}
				}),
			kubeClient.EXPECT().Get(ctx, types.NamespacedName{Name: ccName}, initialCC),
			kubeClient.
				EXPECT().
				Patch(ctx, &cc, gomock.AssignableToTypeOf(ctrlclient.MergeFrom(initialCC))).
				Do(func(_ context.Context, obj *v1alpha1.ClusterClaim, p ctrlclient.Patch, _ ...ctrlclient.PatchOption) {
					Expect(
						p.Data(&cc),
					).To(
						Equal([]byte(`{"spec":{"value":"a1\na2\nb"}}`)),
					)
				}),
		)

		_, err := NewNodeKernelClusterClaimReconciler(kubeClient).Reconcile(ctx, ctrl.Request{})
		Expect(err).NotTo(HaveOccurred())
	})
})
