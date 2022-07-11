package controllers

import (
	"context"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/internal/build"
	"github.com/qbarrand/oot-operator/internal/client"
	"github.com/qbarrand/oot-operator/internal/daemonset"
	"github.com/qbarrand/oot-operator/internal/metrics"
	"github.com/qbarrand/oot-operator/internal/module"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	namespace = "namespace"
)

var _ = Describe("ModuleReconciler", func() {
	Describe("Reconcile", func() {
		var (
			ctrl        *gomock.Controller
			clnt        *client.MockClient
			mockBM      *build.MockManager
			mockDC      *daemonset.MockDaemonSetCreator
			mockKM      *module.MockKernelMapper
			mockMetrics *metrics.MockMetrics
			mockSU      *module.MockStatusUpdater
		)

		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
			clnt = client.NewMockClient(ctrl)
			mockBM = build.NewMockManager(ctrl)
			mockDC = daemonset.NewMockDaemonSetCreator(ctrl)
			mockKM = module.NewMockKernelMapper(ctrl)
			mockMetrics = metrics.NewMockMetrics(ctrl)
			mockSU = module.NewMockStatusUpdater(ctrl)
		})

		const moduleName = "test-module"

		req := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      moduleName,
				Namespace: namespace,
			},
		}

		It("should do nothing when no nodes match the selector", func() {
			mod := ootov1alpha1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: namespace,
				},
				Spec: ootov1alpha1.ModuleSpec{
					Selector: map[string]string{"key": "value"},
				},
			}

			ctx := context.Background()

			gomock.InOrder(
				clnt.EXPECT().Get(ctx, req.NamespacedName, gomock.Any()).DoAndReturn(
					func(_ interface{}, _ interface{}, m *ootov1alpha1.Module) error {
						m.ObjectMeta = mod.ObjectMeta
						m.Spec = mod.Spec
						return nil
					},
				),
				clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ interface{}, list *ootov1alpha1.ModuleList, _ ...interface{}) error {
						list.Items = []ootov1alpha1.Module{mod}
						return nil
					},
				),
				mockMetrics.EXPECT().SetExistingKMMOModules(1),
				clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ interface{}, list *v1.NodeList, _ ...interface{}) error {
						list.Items = []v1.Node{}
						return nil
					},
				),
			)

			mr := NewModuleReconciler(clnt, mockBM, mockDC, mockKM, mockMetrics, nil, mockSU)

			dsByKernelVersion := make(map[string]*appsv1.DaemonSet)

			gomock.InOrder(
				mockDC.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, moduleName, namespace).Return(dsByKernelVersion, nil),
				mockDC.EXPECT().GarbageCollect(ctx, dsByKernelVersion, sets.NewString()),
				mockSU.EXPECT().UpdateModuleStatus(ctx, &mod, []v1.Node{}, []v1.Node{}).Return(nil),
			)

			res, err := mr.Reconcile(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(reconcile.Result{}))
		})

		It("should remove obsolete DaemonSets when no nodes match the selector", func() {
			const kernelVersion = "1.2.3"

			mod := ootov1alpha1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: namespace,
				},
				Spec: ootov1alpha1.ModuleSpec{
					Selector: map[string]string{"key": "value"},
				},
			}

			ds := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "some-daemonset",
					Namespace: namespace,
				},
			}

			ctx := context.Background()
			gomock.InOrder(
				clnt.EXPECT().Get(ctx, req.NamespacedName, gomock.Any()).DoAndReturn(
					func(_ interface{}, _ interface{}, m *ootov1alpha1.Module) error {
						m.ObjectMeta = mod.ObjectMeta
						m.Spec = mod.Spec
						return nil
					},
				),
				clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ interface{}, list *ootov1alpha1.ModuleList, _ ...interface{}) error {
						list.Items = []ootov1alpha1.Module{mod}
						return nil
					},
				),
				mockMetrics.EXPECT().SetExistingKMMOModules(1),
				clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ interface{}, list *v1.NodeList, _ ...interface{}) error {
						list.Items = []v1.Node{}
						return nil
					},
				),
			)

			mr := NewModuleReconciler(clnt, mockBM, mockDC, mockKM, mockMetrics, nil, mockSU)

			dsByKernelVersion := map[string]*appsv1.DaemonSet{kernelVersion: &ds}

			gomock.InOrder(
				mockDC.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, moduleName, namespace).Return(dsByKernelVersion, nil),
				mockDC.EXPECT().GarbageCollect(ctx, dsByKernelVersion, sets.NewString()),
				mockSU.EXPECT().UpdateModuleStatus(ctx, &mod, []v1.Node{}, []v1.Node{}).Return(nil),
			)

			res, err := mr.Reconcile(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(reconcile.Result{}))
		})

		It("should create a DaemonSet when a node matches the selector", func() {
			const (
				imageName     = "test-image"
				kernelVersion = "1.2.3"
			)

			mappings := []ootov1alpha1.KernelMapping{
				{
					ContainerImage: imageName,
					Literal:        kernelVersion,
				},
			}

			osConfig := module.NodeOSConfig{}

			mod := ootov1alpha1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: namespace,
				},
				Spec: ootov1alpha1.ModuleSpec{
					KernelMappings: mappings,
					Selector:       map[string]string{"key": "value"},
				},
			}

			nodeList := v1.NodeList{
				Items: []v1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "node1",
							Labels: map[string]string{"key": "value"},
						},
						Status: v1.NodeStatus{
							NodeInfo: v1.NodeSystemInfo{KernelVersion: kernelVersion},
						},
					},
				},
			}

			ctx := context.Background()

			gomock.InOrder(
				clnt.EXPECT().Get(ctx, req.NamespacedName, gomock.Any()).DoAndReturn(
					func(_ interface{}, _ interface{}, m *ootov1alpha1.Module) error {
						m.ObjectMeta = mod.ObjectMeta
						m.Spec = mod.Spec
						return nil
					},
				),
				clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ interface{}, list *ootov1alpha1.ModuleList, _ ...interface{}) error {
						return nil
					},
				),
				mockMetrics.EXPECT().SetExistingKMMOModules(0),
				clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ interface{}, list *v1.NodeList, _ ...interface{}) error {
						list.Items = nodeList.Items
						return nil
					},
				),
				clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()),
			)

			mr := NewModuleReconciler(clnt, mockBM, mockDC, mockKM, mockMetrics, nil, mockSU)

			ds := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: moduleName + "-",
					Namespace:    namespace,
				},
			}

			gomock.InOrder(
				mockKM.EXPECT().GetNodeOSConfig(&nodeList.Items[0]).Return(&osConfig),
				mockKM.EXPECT().FindMappingForKernel(mappings, kernelVersion).Return(&mappings[0], nil),
				mockKM.EXPECT().PrepareKernelMapping(&mappings[0], &osConfig).Return(&mappings[0], nil),
				mockDC.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, moduleName, namespace),
				mockDC.EXPECT().SetDriverContainerAsDesired(context.Background(), &ds, imageName, gomock.AssignableToTypeOf(mod), kernelVersion),
				mockDC.EXPECT().GarbageCollect(ctx, nil, sets.NewString(kernelVersion)),
				mockSU.EXPECT().UpdateModuleStatus(ctx, &mod, nodeList.Items, nodeList.Items).Return(nil),
			)

			res, err := mr.Reconcile(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(reconcile.Result{}))
		})

		It("should patch the DaemonSet when it already exists", func() {
			const (
				imageName     = "test-image"
				kernelVersion = "1.2.3"
			)

			osConfig := module.NodeOSConfig{}

			mappings := []ootov1alpha1.KernelMapping{
				{
					ContainerImage: imageName,
					Literal:        kernelVersion,
				},
			}

			nodeLabels := map[string]string{"key": "value"}

			mod := ootov1alpha1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: namespace,
				},
				Spec: ootov1alpha1.ModuleSpec{
					KernelMappings: mappings,
					Selector:       nodeLabels,
				},
			}

			nodeList := v1.NodeList{
				Items: []v1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "node1",
							Labels: nodeLabels,
						},
						Status: v1.NodeStatus{
							NodeInfo: v1.NodeSystemInfo{KernelVersion: kernelVersion},
						},
					},
				},
			}

			const (
				dsName      = "some-daemonset"
				dsNamespace = "test-namespace"
			)

			ds := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dsName,
					Namespace: dsNamespace,
				},
			}

			ctx := context.Background()

			gomock.InOrder(
				clnt.EXPECT().Get(ctx, req.NamespacedName, gomock.Any()).DoAndReturn(
					func(_ interface{}, _ interface{}, m *ootov1alpha1.Module) error {
						m.ObjectMeta = mod.ObjectMeta
						m.Spec = mod.Spec
						return nil
					},
				),
				clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ interface{}, list *ootov1alpha1.ModuleList, _ ...interface{}) error {
						list.Items = []ootov1alpha1.Module{mod}
						return nil
					},
				),
				mockMetrics.EXPECT().SetExistingKMMOModules(1),
				clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ interface{}, list *v1.NodeList, _ ...interface{}) error {
						list.Items = nodeList.Items
						return nil
					},
				),
				clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()),
				clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()),
			)

			mr := NewModuleReconciler(clnt, mockBM, mockDC, mockKM, mockMetrics, nil, mockSU)

			dsByKernelVersion := map[string]*appsv1.DaemonSet{kernelVersion: &ds}

			gomock.InOrder(
				mockKM.EXPECT().GetNodeOSConfig(&nodeList.Items[0]).Return(&osConfig),
				mockKM.EXPECT().FindMappingForKernel(mappings, kernelVersion).Return(&mappings[0], nil),
				mockKM.EXPECT().PrepareKernelMapping(&mappings[0], &osConfig).Return(&mappings[0], nil),
				mockDC.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, moduleName, namespace).Return(dsByKernelVersion, nil),
				mockDC.EXPECT().SetDriverContainerAsDesired(context.Background(), &ds, imageName, gomock.AssignableToTypeOf(mod), kernelVersion).Do(
					func(ctx context.Context, d *appsv1.DaemonSet, _ string, _ ootov1alpha1.Module, _ string) {
						d.SetLabels(map[string]string{"test": "test"})
					}),
				mockDC.EXPECT().GarbageCollect(ctx, dsByKernelVersion, sets.NewString(kernelVersion)),
				mockSU.EXPECT().UpdateModuleStatus(ctx, &mod, nodeList.Items, nodeList.Items).Return(nil),
			)

			res, err := mr.Reconcile(context.Background(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(reconcile.Result{}))
		})
	})
})
