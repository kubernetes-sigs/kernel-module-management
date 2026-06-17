package controllers

import (
	"context"
	"errors"

	mock_client "github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("PodNodeLabelReconciler_Reconcile", func() {
	const (
		moduleName   = "module-name"
		nodeName     = "node-name"
		podName      = "pod-name"
		podNamespace = "pod-namespace"
	)

	var (
		kubeClient *mock_client.MockClient
		r          *PodNodeLabelReconciler
	)

	BeforeEach(func() {
		ctrl := gomock.NewController(GinkgoT())
		kubeClient = mock_client.NewMockClient(ctrl)
		r = NewPodNodeLabelReconciler(kubeClient)
	})

	ctx := context.Background()

	Context("device-plugin pods", func() {
		It("should return an error if the pod is not labeled", func() {
			_, err := r.Reconcile(ctx, &v1.Pod{})
			Expect(err).To(HaveOccurred())
		})

		It("should return an error if we failed to get the list of pods", func() {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{constants.ModuleNameLabel: moduleName},
					Name:   podName,
				},
				Spec: v1.PodSpec{NodeName: nodeName},
			}

			kubeClient.EXPECT().List(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("some error"))

			_, err := r.Reconcile(ctx, pod)
			Expect(err).To(HaveOccurred())
		})

		It("should unlabel the node when a Pod is not ready", func() {
			var (
				labelSelector = client.MatchingLabels{
					constants.ModuleNameLabel: moduleName,
					constants.DaemonSetRole:   constants.DevicePluginRoleLabelValue,
				}
				fieldSelector = client.MatchingFields{"spec.nodeName": nodeName}
			)

			gomock.InOrder(
				kubeClient.EXPECT().List(ctx, gomock.Any(), labelSelector, fieldSelector).Return(nil),
				kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Do(
					func(_ interface{}, _ interface{}, node *v1.Node, _ ...client.GetOption) {
						node.SetLabels(map[string]string{utils.GetDevicePluginNodeLabel(podNamespace, moduleName): ""})
					},
				),
				kubeClient.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Do(
					func(_ interface{}, node *v1.Node, p client.Patch, _ ...client.GetOption) {
						Expect(p.Type()).To(Equal(types.MergePatchType))
						Expect(p.Data(node)).To(Equal([]byte(`{"metadata":{"labels":null}}`)))
					},
				),
			)

			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels:    map[string]string{constants.ModuleNameLabel: moduleName},
					Name:      podName,
					Namespace: podNamespace,
				},
				Spec: v1.PodSpec{NodeName: nodeName},
			}

			_, err := r.Reconcile(ctx, pod)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should NOT unlabel the node when a Pod is not ready but there is a new running pod", func() {
			var (
				labelSelector = client.MatchingLabels{
					constants.ModuleNameLabel: moduleName,
					constants.DaemonSetRole:   constants.DevicePluginRoleLabelValue,
				}
				fieldSelector = client.MatchingFields{"spec.nodeName": nodeName}
			)

			kubeClient.EXPECT().List(ctx, gomock.Any(), labelSelector, fieldSelector).Do(
				func(_ interface{}, modulePodsList *v1.PodList, _ ...client.ListOption) {
					modulePodsList.Items = []v1.Pod{
						{
							Status: v1.PodStatus{
								Conditions: []v1.PodCondition{
									{
										Type:   v1.PodReady,
										Status: v1.ConditionTrue,
									},
								},
							},
						},
					}
				},
			)

			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{constants.ModuleNameLabel: moduleName},
					Name:   podName,
				},
				Spec: v1.PodSpec{NodeName: nodeName},
			}

			_, err := r.Reconcile(ctx, pod)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should NOT label or unlabel when the Pod has no .spec.nodeName", func() {
			now := metav1.Now()

			kubeClient.EXPECT().Patch(ctx, gomock.AssignableToTypeOf(&v1.Pod{}), gomock.Any()).Do(
				func(_ interface{}, pod *v1.Pod, p client.Patch, _ ...client.GetOption) {
					Expect(p.Type()).To(Equal(types.MergePatchType))
					Expect(p.Data(pod)).To(Equal([]byte(`{"metadata":{"finalizers":null}}`)))
				},
			)

			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &now,
					Finalizers:        []string{constants.NodeLabelerFinalizer},
					Labels:            map[string]string{constants.ModuleNameLabel: moduleName},
					Name:              podName,
				},
			}

			_, err := r.Reconcile(ctx, pod)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should label the node when a Pod is ready", func() {
			gomock.InOrder(
				kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(nil),
				kubeClient.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Do(
					func(_ interface{}, node *v1.Node, p client.Patch, _ ...client.GetOption) {
						Expect(p.Type()).To(Equal(types.MergePatchType))
						data, err := p.Data(node)
						Expect(err).NotTo(HaveOccurred())
						Expect(data).To(ContainSubstring("labels"))
						Expect(data).To(ContainSubstring(utils.GetDevicePluginNodeLabel(podNamespace, moduleName)))
					},
				),
			)

			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{constants.NodeLabelerFinalizer},
					Labels:     map[string]string{constants.ModuleNameLabel: moduleName},
					Name:       podName,
					Namespace:  podNamespace,
				},
				Spec: v1.PodSpec{NodeName: nodeName},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:   v1.PodReady,
							Status: v1.ConditionTrue,
						},
					},
				},
			}

			_, err := r.Reconcile(ctx, pod)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should remove the pod finalizer when the pod is being deleted", func() {
			now := metav1.Now()
			var (
				labelSelector = client.MatchingLabels{
					constants.ModuleNameLabel: moduleName,
					constants.DaemonSetRole:   constants.DevicePluginRoleLabelValue,
				}
				fieldSelector = client.MatchingFields{"spec.nodeName": nodeName}
			)

			patchRemoveFinalizerFunc := func(_ interface{}, pod *v1.Pod, p client.Patch, _ ...client.GetOption) {
				Expect(p.Type()).To(Equal(types.MergePatchType))
				Expect(p.Data(pod)).To(Equal([]byte(`{"metadata":{"finalizers":null}}`)))
			}

			gomock.InOrder(
				kubeClient.EXPECT().List(ctx, gomock.Any(), labelSelector, fieldSelector).Return(nil),
				kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(nil),
				kubeClient.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Return(nil),
				kubeClient.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Do(patchRemoveFinalizerFunc),
			)

			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &now,
					Finalizers:        []string{constants.NodeLabelerFinalizer},
					Labels:            map[string]string{constants.ModuleNameLabel: moduleName},
					Name:              podName,
				},
				Spec: v1.PodSpec{NodeName: nodeName},
			}

			_, err := r.Reconcile(ctx, pod)
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Context("DRA pods", func() {
		It("should return an error if the pod is not labeled", func() {
			_, err := r.Reconcile(ctx, &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						constants.DaemonSetRole: constants.DRARoleLabelValue,
					},
				},
			})
			Expect(err).To(HaveOccurred())
		})

		It("should return an error if we failed to get the list of DRA pods", func() {
			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						constants.ModuleNameLabel: moduleName,
						constants.DaemonSetRole:   constants.DRARoleLabelValue,
					},
					Name: podName,
				},
				Spec: v1.PodSpec{NodeName: nodeName},
			}

			kubeClient.EXPECT().List(ctx, gomock.Any(), gomock.Any(), gomock.Any()).Return(errors.New("some error"))

			_, err := r.Reconcile(ctx, pod)
			Expect(err).To(HaveOccurred())
		})

		It("should unlabel the node when a DRA Pod is not ready", func() {
			var (
				labelSelector = client.MatchingLabels{
					constants.ModuleNameLabel: moduleName,
					constants.DaemonSetRole:   constants.DRARoleLabelValue,
				}
				fieldSelector = client.MatchingFields{"spec.nodeName": nodeName}
			)

			gomock.InOrder(
				kubeClient.EXPECT().List(ctx, gomock.Any(), labelSelector, fieldSelector).Return(nil),
				kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Do(
					func(_ interface{}, _ interface{}, node *v1.Node, _ ...client.GetOption) {
						node.SetLabels(map[string]string{utils.GetDRANodeLabel(podNamespace, moduleName): ""})
					},
				),
				kubeClient.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Do(
					func(_ interface{}, node *v1.Node, p client.Patch, _ ...client.GetOption) {
						Expect(p.Type()).To(Equal(types.MergePatchType))
						Expect(p.Data(node)).To(Equal([]byte(`{"metadata":{"labels":null}}`)))
					},
				),
			)

			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						constants.ModuleNameLabel: moduleName,
						constants.DaemonSetRole:   constants.DRARoleLabelValue,
					},
					Name:      podName,
					Namespace: podNamespace,
				},
				Spec: v1.PodSpec{NodeName: nodeName},
			}

			_, err := r.Reconcile(ctx, pod)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should NOT unlabel the node when a DRA Pod is not ready but there is a new running DRA pod", func() {
			var (
				labelSelector = client.MatchingLabels{
					constants.ModuleNameLabel: moduleName,
					constants.DaemonSetRole:   constants.DRARoleLabelValue,
				}
				fieldSelector = client.MatchingFields{"spec.nodeName": nodeName}
			)

			kubeClient.EXPECT().List(ctx, gomock.Any(), labelSelector, fieldSelector).Do(
				func(_ interface{}, draPodsList *v1.PodList, _ ...client.ListOption) {
					draPodsList.Items = []v1.Pod{
						{
							Status: v1.PodStatus{
								Conditions: []v1.PodCondition{
									{
										Type:   v1.PodReady,
										Status: v1.ConditionTrue,
									},
								},
							},
						},
					}
				},
			)

			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						constants.ModuleNameLabel: moduleName,
						constants.DaemonSetRole:   constants.DRARoleLabelValue,
					},
					Name: podName,
				},
				Spec: v1.PodSpec{NodeName: nodeName},
			}

			_, err := r.Reconcile(ctx, pod)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should label the node with dra-ready when a DRA Pod is ready", func() {
			gomock.InOrder(
				kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(nil),
				kubeClient.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Do(
					func(_ interface{}, node *v1.Node, p client.Patch, _ ...client.GetOption) {
						Expect(p.Type()).To(Equal(types.MergePatchType))
						data, err := p.Data(node)
						Expect(err).NotTo(HaveOccurred())
						Expect(data).To(ContainSubstring("labels"))
						Expect(data).To(ContainSubstring(utils.GetDRANodeLabel(podNamespace, moduleName)))
					},
				),
			)

			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Finalizers: []string{constants.NodeLabelerFinalizer},
					Labels: map[string]string{
						constants.ModuleNameLabel: moduleName,
						constants.DaemonSetRole:   constants.DRARoleLabelValue,
					},
					Name:      podName,
					Namespace: podNamespace,
				},
				Spec: v1.PodSpec{NodeName: nodeName},
				Status: v1.PodStatus{
					Conditions: []v1.PodCondition{
						{
							Type:   v1.PodReady,
							Status: v1.ConditionTrue,
						},
					},
				},
			}

			_, err := r.Reconcile(ctx, pod)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should NOT label or unlabel when the DRA Pod has no .spec.nodeName", func() {
			now := metav1.Now()

			kubeClient.EXPECT().Patch(ctx, gomock.AssignableToTypeOf(&v1.Pod{}), gomock.Any()).Do(
				func(_ interface{}, pod *v1.Pod, p client.Patch, _ ...client.GetOption) {
					Expect(p.Type()).To(Equal(types.MergePatchType))
					Expect(p.Data(pod)).To(Equal([]byte(`{"metadata":{"finalizers":null}}`)))
				},
			)

			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &now,
					Finalizers:        []string{constants.NodeLabelerFinalizer},
					Labels: map[string]string{
						constants.ModuleNameLabel: moduleName,
						constants.DaemonSetRole:   constants.DRARoleLabelValue,
					},
					Name: podName,
				},
			}

			_, err := r.Reconcile(ctx, pod)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should remove the pod finalizer when the DRA pod is being deleted", func() {
			now := metav1.Now()
			var (
				labelSelector = client.MatchingLabels{
					constants.ModuleNameLabel: moduleName,
					constants.DaemonSetRole:   constants.DRARoleLabelValue,
				}
				fieldSelector = client.MatchingFields{"spec.nodeName": nodeName}
			)

			patchRemoveFinalizerFunc := func(_ interface{}, pod *v1.Pod, p client.Patch, _ ...client.GetOption) {
				Expect(p.Type()).To(Equal(types.MergePatchType))
				Expect(p.Data(pod)).To(Equal([]byte(`{"metadata":{"finalizers":null}}`)))
			}

			gomock.InOrder(
				kubeClient.EXPECT().List(ctx, gomock.Any(), labelSelector, fieldSelector).Return(nil),
				kubeClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(nil),
				kubeClient.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Return(nil),
				kubeClient.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Do(patchRemoveFinalizerFunc),
			)

			pod := &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					DeletionTimestamp: &now,
					Finalizers:        []string{constants.NodeLabelerFinalizer},
					Labels: map[string]string{
						constants.ModuleNameLabel: moduleName,
						constants.DaemonSetRole:   constants.DRARoleLabelValue,
					},
					Name: podName,
				},
				Spec: v1.PodSpec{NodeName: nodeName},
			}

			_, err := r.Reconcile(ctx, pod)
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
