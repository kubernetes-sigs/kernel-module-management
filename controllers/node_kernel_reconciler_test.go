package controllers

import (
	"context"
	"errors"

	"github.com/golang/mock/gomock"
	clienttest "github.com/kubernetes-sigs/kernel-module-management/internal/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	runtimectrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("NodeKernelReconciler_Reconcile", func() {
	var (
		gCtrl *gomock.Controller
		clnt  *clienttest.MockClient
	)
	BeforeEach(func() {
		gCtrl = gomock.NewController(GinkgoT())
		clnt = clienttest.NewMockClient(gCtrl)
	})
	const (
		kernelVersion = "1.2.3"
		labelName     = "label-name"
		nodeName      = "node-name"
	)

	It("should return an error if the node cannot be found anymore", func() {
		ctx := context.Background()
		clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(errors.New("some error"))

		nkr := NewNodeKernelReconciler(clnt, labelName, nil)
		req := runtimectrl.Request{
			NamespacedName: types.NamespacedName{Name: nodeName},
		}

		_, err := nkr.Reconcile(ctx, req)
		Expect(err).To(HaveOccurred())
	})

	DescribeTable(
		"should set the label",
		func(kv, expected string, alreadyLabeled bool) {
			node := v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: nodeName},
				Status: v1.NodeStatus{
					NodeInfo: v1.NodeSystemInfo{KernelVersion: kv},
				},
			}

			if alreadyLabeled {
				node.SetLabels(map[string]string{labelName: "some-value"})
			}

			node.SetLabels(map[string]string{labelName: kernelVersion})
			nsn := types.NamespacedName{Name: nodeName}

			ctx := context.Background()
			gomock.InOrder(
				clnt.EXPECT().Get(ctx, nsn, &v1.Node{}).DoAndReturn(
					func(_ interface{}, _ interface{}, n *v1.Node, _ ...client.GetOption) error {
						n.ObjectMeta = node.ObjectMeta
						n.Status = node.Status
						return nil
					},
				),
				clnt.EXPECT().Patch(ctx, &node, gomock.Any()),
			)

			nkr := NewNodeKernelReconciler(clnt, labelName, nil)
			req := runtimectrl.Request{NamespacedName: nsn}

			res, err := nkr.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res.Requeue).To(BeFalse())
		},
		Entry(nil, kernelVersion, kernelVersion, false),
		Entry(nil, kernelVersion, kernelVersion, true),
		Entry(nil, kernelVersion+"+", kernelVersion, true),
	)
})
