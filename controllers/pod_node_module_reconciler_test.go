package controllers

import (
	"context"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	mock_client "github.com/qbarrand/oot-operator/internal/client"
	"github.com/qbarrand/oot-operator/internal/constants"
	"github.com/qbarrand/oot-operator/internal/daemonset"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("PodNodeModuleReconciler", func() {
	Describe("Reconcile", func() {
		const (
			moduleName   = "module-name"
			nodeName     = "node-name"
			podName      = "pod-name"
			podNamespace = "pod-namespace"
			nodeLabel    = "some node label"
		)

		var (
			kubeClient *mock_client.MockClient
			r          *PodNodeModuleReconciler
			mockDC     *daemonset.MockDaemonSetCreator
		)

		BeforeEach(func() {
			ctrl := gomock.NewController(GinkgoT())
			kubeClient = mock_client.NewMockClient(ctrl)
			mockDC = daemonset.NewMockDaemonSetCreator(ctrl)
			r = NewPodNodeModuleReconciler(kubeClient, mockDC)
		})

		ctx := context.Background()
		nn := types.NamespacedName{
			Namespace: podNamespace,
			Name:      podName,
		}
		req := ctrl.Request{NamespacedName: nn}

		It("should return an error if the pod is not labeled", func() {
			gomock.InOrder(
				kubeClient.EXPECT().Get(ctx, nn, gomock.AssignableToTypeOf(&v1.Pod{})),
			)

			_, err := r.Reconcile(ctx, req)
			Expect(err).To(HaveOccurred())
		})

		It("should unlabel the node when a Pod is not ready", func() {
			pod := v1.Pod{}
			podWithModuleName := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{constants.ModuleNameLabel: moduleName}},
				Spec:       v1.PodSpec{NodeName: nodeName},
			}
			node := v1.Node{}
			nodeWithEmptyLabels := v1.Node{
				ObjectMeta: metav1.ObjectMeta{Labels: make(map[string]string)},
			}

			gomock.InOrder(
				kubeClient.
					EXPECT().
					Get(ctx, nn, &pod).
					Do(func(_ context.Context, _ types.NamespacedName, o client.Object) {
						o.SetLabels(map[string]string{constants.ModuleNameLabel: moduleName})
						o.(*v1.Pod).Spec.NodeName = nodeName
					}),
				mockDC.EXPECT().GetNodeLabelFromPod(&podWithModuleName, moduleName).Return(nodeLabel),
				kubeClient.
					EXPECT().
					Get(ctx, types.NamespacedName{Name: nodeName}, &node).
					Do(func(_ context.Context, _ types.NamespacedName, o client.Object) {
						o.SetLabels(map[string]string{nodeLabel: ""})
					}),
				kubeClient.
					EXPECT().
					Patch(ctx, &nodeWithEmptyLabels, gomock.Any()).
					Do(func(_ context.Context, n client.Object, p client.Patch, _ ...client.PatchOption) {
						Expect(p.Type()).To(Equal(types.MergePatchType))
						Expect(p.Data(n)).To(
							Equal(
								[]byte(`{"metadata":{"labels":null}}`),
							),
						)
					}),
			)

			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should label the node when a Pod is ready", func() {
			pod := v1.Pod{}
			readyPod := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{constants.ModuleNameLabel: moduleName}},
				Spec:       v1.PodSpec{NodeName: nodeName},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:   v1.PodReady,
							Status: v1.ConditionTrue,
						},
					},
				},
			}
			node := v1.Node{}
			nodeWithLabel := node
			nodeWithLabel.SetLabels(map[string]string{nodeLabel: ""})
			patch := client.MergeFrom(&node)

			gomock.InOrder(
				kubeClient.
					EXPECT().
					Get(ctx, nn, &pod).
					Do(func(_ context.Context, _ types.NamespacedName, o client.Object) {
						o.SetLabels(map[string]string{constants.ModuleNameLabel: moduleName})
						o.(*v1.Pod).Spec.NodeName = nodeName
						o.(*v1.Pod).Status.Conditions = []v1.PodCondition{
							{
								Type:   v1.PodReady,
								Status: v1.ConditionTrue,
							},
						}
					}),
				mockDC.EXPECT().GetNodeLabelFromPod(&readyPod, moduleName).Return(nodeLabel),
				kubeClient.EXPECT().Get(ctx, types.NamespacedName{Name: nodeName}, &node),
				kubeClient.
					EXPECT().
					Patch(ctx, &nodeWithLabel, gomock.AssignableToTypeOf(patch)).
					Do(func(_ context.Context, n client.Object, p client.Patch, _ ...client.PatchOption) {
						Expect(p.Type()).To(Equal(types.MergePatchType))
						Expect(p.Data(n)).To(
							Equal(
								[]byte(`{"metadata":{"labels":{"some node label":""}}}`),
							),
						)
					}),
			)

			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should unlabel the node and remove the pod finalizer when the pod is being deleted", func() {
			now := metav1.Now()

			pod := v1.Pod{}
			deletedPod := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &now,
					Finalizers:        []string{constants.NodeLabelerFinalizer},
					Labels:            map[string]string{constants.ModuleNameLabel: moduleName},
				},
				Spec: v1.PodSpec{NodeName: nodeName},
			}
			podWithoutFinalizer := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &now,
					Finalizers:        make([]string, 0),
					Labels:            map[string]string{constants.ModuleNameLabel: moduleName},
				},
				Spec: v1.PodSpec{NodeName: nodeName},
			}

			node := v1.Node{}
			nodeWithEmptyLabels := v1.Node{
				ObjectMeta: metav1.ObjectMeta{Labels: make(map[string]string)},
			}

			gomock.InOrder(
				kubeClient.
					EXPECT().
					Get(ctx, nn, &pod).
					Do(func(_ context.Context, _ types.NamespacedName, o client.Object) {
						o.SetLabels(map[string]string{constants.ModuleNameLabel: moduleName})
						o.(*v1.Pod).Spec.NodeName = nodeName
						o.SetDeletionTimestamp(&now)
						o.SetFinalizers([]string{constants.NodeLabelerFinalizer})
					}),
				mockDC.EXPECT().GetNodeLabelFromPod(&deletedPod, moduleName).Return(nodeLabel),
				kubeClient.
					EXPECT().
					Get(ctx, types.NamespacedName{Name: nodeName}, &node).
					Do(func(_ context.Context, _ types.NamespacedName, o client.Object) {
						o.SetLabels(map[string]string{nodeLabel: ""})
					}),
				kubeClient.
					EXPECT().
					Patch(ctx, &nodeWithEmptyLabels, gomock.Any()).
					Do(func(_ context.Context, n client.Object, p client.Patch, _ ...client.PatchOption) {
						Expect(p.Type()).To(Equal(types.MergePatchType))
						Expect(p.Data(n)).To(
							Equal(
								[]byte(`{"metadata":{"labels":null}}`),
							),
						)
					}),
				kubeClient.
					EXPECT().
					Patch(ctx, &podWithoutFinalizer, gomock.Any()).
					Do(func(_ context.Context, po client.Object, p client.Patch, _ ...client.PatchOption) {
						Expect(p.Type()).To(Equal(types.MergePatchType))
						Expect(p.Data(po)).To(
							Equal(
								[]byte(`{"metadata":{"finalizers":null}}`),
							),
						)
					}),
			)

			_, err := r.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
