package controllers_test

import (
	"context"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	"github.com/qbarrand/oot-operator/controllers"
	"github.com/qbarrand/oot-operator/controllers/module"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("ModuleReconciler", func() {
	Describe("Reconcile", func() {
		var (
			ctrl *gomock.Controller
			dc   *controllers.MockDaemonSetCreator
			km   *module.MockKernelMapper
		)

		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
			dc = controllers.NewMockDaemonSetCreator(ctrl)
			km = module.NewMockKernelMapper(ctrl)
		})

		const (
			kernelVersion = "1.2.3"
			nodeName      = "test-node"
			namespace     = "test-namespace"
		)

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{Name: nodeName},
		}

		It("should return an error if the node cannot be found", func() {
			nr := controllers.NewNodeReconciler(fake.NewClientBuilder().Build(), namespace, dc, km)

			ctx := context.TODO()

			_, err := nr.Reconcile(ctx, req)
			Expect(err).To(HaveOccurred())
		})

		It("should do nothing when the node does require any module", func() {
			node := v1.Node{
				ObjectMeta: metav1.ObjectMeta{Name: nodeName},
			}

			modules := ootov1beta1.ModuleList{
				Items: []ootov1beta1.Module{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "mod1"},
						Spec: ootov1beta1.ModuleSpec{
							Selector: map[string]string{"key1": "value1"},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{Name: "mod2"},
						Spec: ootov1beta1.ModuleSpec{
							Selector: map[string]string{"key2": "value2"},
						},
					},
				},
			}

			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&node).WithLists(&modules).Build()

			nr := controllers.NewNodeReconciler(client, namespace, dc, km)

			res, err := nr.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(reconcile.Result{}))
		})

		It("should do nothing when a DaemonSet already exist for a module/kernel pair", func() {
			const kernelVersion = "1.2.3"

			nodeLabels := map[string]string{"key": "value"}

			node := v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   nodeName,
					Labels: nodeLabels,
				},
				Status: v1.NodeStatus{
					NodeInfo: v1.NodeSystemInfo{KernelVersion: kernelVersion},
				},
			}

			mod := ootov1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{Name: "mod1"},
				Spec:       ootov1beta1.ModuleSpec{Selector: nodeLabels},
			}

			client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&mod, &node).Build()

			nr := controllers.NewNodeReconciler(client, namespace, dc, km)

			ctx := context.TODO()

			dsByKernelVersion := map[string]*appsv1.DaemonSet{
				kernelVersion: {},
			}

			dc.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, mod).Return(dsByKernelVersion, nil)

			res, err := nr.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(reconcile.Result{}))
		})

		It("should create one DaemonSet nothing two modules exist and one DaemonSet already exists", func() {
			nodeLabels := map[string]string{"key": "value"}

			node := v1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   nodeName,
					Labels: nodeLabels,
				},
				Status: v1.NodeStatus{
					NodeInfo: v1.NodeSystemInfo{KernelVersion: kernelVersion},
				},
			}

			mod1 := ootov1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{Name: "mod1"},
				Spec:       ootov1beta1.ModuleSpec{Selector: nodeLabels},
			}

			mod2Mappings := make([]ootov1beta1.KernelMapping, 0)

			mod2 := ootov1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{Name: "mod2"},
				Spec: ootov1beta1.ModuleSpec{
					KernelMappings: mod2Mappings,
					Selector:       nodeLabels,
				},
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(&node, &mod1, &mod2).
				Build()

			nr := controllers.NewNodeReconciler(client, namespace, dc, km)

			ctx := context.TODO()

			dsByKernelVersion := map[string]*appsv1.DaemonSet{
				kernelVersion: {},
			}

			const containerImage = "some-container-image"

			ds := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: mod2.Name + "-",
					Namespace:    namespace,
				},
			}

			gomock.InOrder(
				dc.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, mod1).Return(dsByKernelVersion, nil),
				dc.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, mod2).Return(map[string]*appsv1.DaemonSet{}, nil),
				km.EXPECT().FindImageForKernel(mod2Mappings, kernelVersion).Return(containerImage, nil),
				dc.EXPECT().SetAsDesired(&ds, containerImage, mod2, kernelVersion),
			)

			res, err := nr.Reconcile(ctx, req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(reconcile.Result{}))

			dsList := appsv1.DaemonSetList{}

			Expect(
				client.List(context.TODO(), &dsList),
			).To(Succeed())

			Expect(dsList.Items).To(HaveLen(1))
			Expect(dsList.Items[0].Name).To(MatchRegexp("^mod2-[a-z1-9]+$"))
		})
	})
})
