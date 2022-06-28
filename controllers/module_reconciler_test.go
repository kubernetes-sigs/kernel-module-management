package controllers

import (
	"context"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/internal/build"
	"github.com/qbarrand/oot-operator/internal/daemonset"
	"github.com/qbarrand/oot-operator/internal/metrics"
	"github.com/qbarrand/oot-operator/internal/module"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	namespace = "namespace"
)

var _ = Describe("ModuleReconciler", func() {
	Describe("Reconcile", func() {
		var (
			ctrl        *gomock.Controller
			mockBM      *build.MockManager
			mockCU      *module.MockConditionsUpdater
			mockDC      *daemonset.MockDaemonSetCreator
			mockKM      *module.MockKernelMapper
			mockMetrics *metrics.MockMetrics
		)

		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
			mockBM = build.NewMockManager(ctrl)
			mockCU = module.NewMockConditionsUpdater(ctrl)
			mockDC = daemonset.NewMockDaemonSetCreator(ctrl)
			mockKM = module.NewMockKernelMapper(ctrl)
			mockMetrics = metrics.NewMockMetrics(ctrl)
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

			nodeList := v1.NodeList{
				Items: []v1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{Name: "node1"},
					},
				},
			}

			client := fake.
				NewClientBuilder().
				WithScheme(scheme).
				WithObjects(&mod).
				WithLists(&nodeList).
				Build()

			mr := NewModuleReconciler(client, mockBM, mockDC, mockKM, mockCU, mockMetrics, nil)

			ctx := context.TODO()

			dsByKernelVersion := make(map[string]*appsv1.DaemonSet)

			gomock.InOrder(
				mockMetrics.EXPECT().SetExistingKMMOModules(1),
				mockDC.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, moduleName, namespace).Return(dsByKernelVersion, nil),
				mockDC.EXPECT().GarbageCollect(ctx, dsByKernelVersion, sets.NewString()),
			)

			res, err := mr.Reconcile(context.TODO(), req)
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

			c := fake.
				NewClientBuilder().
				WithScheme(scheme).
				WithObjects(&mod, &ds).
				Build()

			mr := NewModuleReconciler(c, mockBM, mockDC, mockKM, mockCU, mockMetrics, nil)

			ctx := context.TODO()

			dsByKernelVersion := map[string]*appsv1.DaemonSet{kernelVersion: &ds}

			gomock.InOrder(
				mockMetrics.EXPECT().SetExistingKMMOModules(1),
				mockDC.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, moduleName, namespace).Return(dsByKernelVersion, nil),
				mockDC.EXPECT().GarbageCollect(ctx, dsByKernelVersion, sets.NewString()),
			)

			res, err := mr.Reconcile(context.TODO(), req)
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

			c := fake.
				NewClientBuilder().
				WithScheme(scheme).
				WithObjects(&mod).
				WithLists(&nodeList).
				Build()

			mr := NewModuleReconciler(c, mockBM, mockDC, mockKM, mockCU, mockMetrics, nil)

			ctx := context.TODO()

			ds := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: moduleName + "-",
					Namespace:    namespace,
				},
			}

			gomock.InOrder(
				mockMetrics.EXPECT().SetExistingKMMOModules(1),
				mockKM.EXPECT().GetNodeOSConfig(&nodeList.Items[0]).Return(&osConfig),
				mockKM.EXPECT().FindMappingForKernel(mappings, kernelVersion).Return(&mappings[0], nil),
				mockKM.EXPECT().PrepareKernelMapping(&mappings[0], &osConfig).Return(&mappings[0], nil),
				mockDC.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, moduleName, namespace),
				mockDC.EXPECT().SetDriverContainerAsDesired(context.TODO(), &ds, imageName, gomock.AssignableToTypeOf(mod), kernelVersion),
				mockDC.EXPECT().GarbageCollect(ctx, nil, sets.NewString(kernelVersion)),
			)

			res, err := mr.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(reconcile.Result{}))

			dsList := appsv1.DaemonSetList{}

			err = c.List(ctx, &dsList)
			Expect(err).NotTo(HaveOccurred())
			Expect(dsList.Items).To(HaveLen(1))
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

			c := fake.
				NewClientBuilder().
				WithScheme(scheme).
				WithObjects(&mod, &ds).
				WithLists(&nodeList).
				Build()

			mr := NewModuleReconciler(c, mockBM, mockDC, mockKM, mockCU, mockMetrics, nil)

			ctx := context.TODO()

			dsByKernelVersion := map[string]*appsv1.DaemonSet{kernelVersion: &ds}

			gomock.InOrder(
				mockMetrics.EXPECT().SetExistingKMMOModules(1),
				mockKM.EXPECT().GetNodeOSConfig(&nodeList.Items[0]).Return(&osConfig),
				mockKM.EXPECT().FindMappingForKernel(mappings, kernelVersion).Return(&mappings[0], nil),
				mockKM.EXPECT().PrepareKernelMapping(&mappings[0], &osConfig).Return(&mappings[0], nil),
				mockDC.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, moduleName, namespace).Return(dsByKernelVersion, nil),
				mockDC.EXPECT().SetDriverContainerAsDesired(context.TODO(), &ds, imageName, gomock.AssignableToTypeOf(mod), kernelVersion).Do(
					func(ctx context.Context, d *appsv1.DaemonSet, _ string, _ ootov1alpha1.Module, _ string) {
						d.SetLabels(map[string]string{"test": "test"})
					}),
				mockDC.EXPECT().GarbageCollect(ctx, dsByKernelVersion, sets.NewString(kernelVersion)),
			)

			res, err := mr.Reconcile(context.TODO(), req)
			Expect(err).NotTo(HaveOccurred())
			Expect(res).To(Equal(reconcile.Result{}))

			dsList := appsv1.DaemonSetList{}

			err = c.List(ctx, &dsList)
			Expect(err).NotTo(HaveOccurred())
			Expect(dsList.Items).To(HaveLen(1))
			Expect(dsList.Items[0].Name).To(Equal(dsName))
			Expect(dsList.Items[0].Labels).To(HaveKeyWithValue("test", "test"))
		})
	})
})
