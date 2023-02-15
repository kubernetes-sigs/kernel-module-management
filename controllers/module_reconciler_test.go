package controllers

import (
	"context"

	"github.com/golang/mock/gomock"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/daemonset"
	"github.com/kubernetes-sigs/kernel-module-management/internal/metrics"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/statusupdater"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	namespace = "namespace"
)

var _ = Describe("ModuleReconciler_Reconcile", func() {
	var (
		ctrl        *gomock.Controller
		clnt        *client.MockClient
		mockBM      *build.MockManager
		mockSM      *sign.MockSignManager
		mockDC      *daemonset.MockDaemonSetCreator
		mockKM      *module.MockKernelMapper
		mockMetrics *metrics.MockMetrics
		mockSU      *statusupdater.MockModuleStatusUpdater
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockBM = build.NewMockManager(ctrl)
		mockSM = sign.NewMockSignManager(ctrl)
		mockDC = daemonset.NewMockDaemonSetCreator(ctrl)
		mockKM = module.NewMockKernelMapper(ctrl)
		mockMetrics = metrics.NewMockMetrics(ctrl)
		mockSU = statusupdater.NewMockModuleStatusUpdater(ctrl)
	})

	const moduleName = "test-module"

	nsn := types.NamespacedName{
		Name:      moduleName,
		Namespace: namespace,
	}

	req := reconcile.Request{NamespacedName: nsn}

	ctx := context.Background()

	It("should do nothing if the Module is not available anymore", func() {
		clnt.
			EXPECT().
			Get(ctx, nsn, &kmmv1beta1.Module{}).
			Return(
				apierrors.NewNotFound(schema.GroupResource{}, moduleName),
			)

		mr := NewModuleReconciler(clnt, mockBM, mockSM, mockDC, mockKM, mockMetrics, nil, mockSU, namespace)
		Expect(
			mr.Reconcile(ctx, req),
		).To(
			Equal(reconcile.Result{}),
		)
	})

	It("should add the module loader and device plugin ServiceAccounts if they are not set", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ModuleSpec{
				Selector: map[string]string{"key": "value"},
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					ServiceAccountName: "",
				},
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{
					ServiceAccountName: "",
				},
			},
		}
		ds := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName + "-device-plugin",
				Namespace: namespace,
			},
		}

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, req.NamespacedName, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, m *kmmv1beta1.Module, _ ...ctrlclient.GetOption) error {
					m.ObjectMeta = mod.ObjectMeta
					m.Spec = mod.Spec
					return nil
				},
			),
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.Module{mod}
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
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(apierrors.NewNotFound(schema.GroupResource{}, "whatever")),
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(apierrors.NewNotFound(schema.GroupResource{}, "whatever")),
			mockDC.EXPECT().SetDevicePluginAsDesired(context.Background(), &ds, gomock.AssignableToTypeOf(&mod)),
			clnt.EXPECT().Create(ctx, gomock.Any()).Return(nil),
			mockMetrics.EXPECT().SetCompletedStage(moduleName, namespace, "", metrics.DevicePluginStage, false),
		)

		mr := NewModuleReconciler(clnt, mockBM, mockSM, mockDC, mockKM, mockMetrics, nil, mockSU, namespace)

		dsByKernelVersion := make(map[string]*appsv1.DaemonSet)

		gomock.InOrder(
			mockDC.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, moduleName, namespace).Return(dsByKernelVersion, nil),
			mockDC.EXPECT().GarbageCollect(ctx, dsByKernelVersion, sets.New[string]()),
			mockBM.EXPECT().GarbageCollect(ctx, mod.Name, mod.Namespace, &mod),
			mockSM.EXPECT().GarbageCollect(ctx, mod.Name, mod.Namespace, &mod),
			mockSU.EXPECT().ModuleUpdateStatus(ctx, &mod, []v1.Node{}, []v1.Node{}, dsByKernelVersion).Return(nil),
		)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should do nothing when no nodes match the selector", func() {
		const serviceAccountName = "module-loader-service-account"

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ModuleSpec{
				Selector: map[string]string{"key": "value"},
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					ServiceAccountName: serviceAccountName,
				},
			},
		}

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, req.NamespacedName, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, m *kmmv1beta1.Module, _ ...ctrlclient.GetOption) error {
					m.ObjectMeta = mod.ObjectMeta
					m.Spec = mod.Spec
					return nil
				},
			),
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.Module{mod}
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

		mr := NewModuleReconciler(clnt, mockBM, mockSM, mockDC, mockKM, mockMetrics, nil, mockSU, namespace)

		dsByKernelVersion := make(map[string]*appsv1.DaemonSet)

		gomock.InOrder(
			mockDC.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, moduleName, namespace).Return(dsByKernelVersion, nil),
			mockDC.EXPECT().GarbageCollect(ctx, dsByKernelVersion, sets.New[string]()),
			mockBM.EXPECT().GarbageCollect(ctx, mod.Name, mod.Namespace, &mod),
			mockSM.EXPECT().GarbageCollect(ctx, mod.Name, mod.Namespace, &mod),
			mockSU.EXPECT().ModuleUpdateStatus(ctx, &mod, []v1.Node{}, []v1.Node{}, dsByKernelVersion).Return(nil),
		)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should remove obsolete DaemonSets when no nodes match the selector", func() {
		const (
			kernelVersion      = "1.2.3"
			serviceAccountName = "module-loader-service-account"
		)

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ModuleSpec{
				Selector: map[string]string{"key": "value"},
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					ServiceAccountName: serviceAccountName,
				},
			},
		}

		ds := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "some-daemonset",
				Namespace: namespace,
			},
		}

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, req.NamespacedName, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, m *kmmv1beta1.Module, _ ...ctrlclient.GetOption) error {
					m.ObjectMeta = mod.ObjectMeta
					m.Spec = mod.Spec
					return nil
				},
			),
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.Module{mod}
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

		mr := NewModuleReconciler(clnt, mockBM, mockSM, mockDC, mockKM, mockMetrics, nil, mockSU, namespace)

		dsByKernelVersion := map[string]*appsv1.DaemonSet{kernelVersion: &ds}

		gomock.InOrder(
			mockDC.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, moduleName, namespace).Return(dsByKernelVersion, nil),
			mockDC.EXPECT().GarbageCollect(ctx, dsByKernelVersion, sets.New[string]()),
			mockBM.EXPECT().GarbageCollect(ctx, mod.Name, mod.Namespace, &mod),
			mockSM.EXPECT().GarbageCollect(ctx, mod.Name, mod.Namespace, &mod),
			mockSU.EXPECT().ModuleUpdateStatus(ctx, &mod, []v1.Node{}, []v1.Node{}, dsByKernelVersion).Return(nil),
		)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should create a DaemonSet when a node matches the selector", func() {
		const (
			imageName          = "test-image"
			kernelVersion      = "1.2.3"
			serviceAccountName = "module-loader-service-account"
		)

		mappings := []kmmv1beta1.KernelMapping{
			{
				ContainerImage: imageName,
				Literal:        kernelVersion,
			},
		}

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					ServiceAccountName: serviceAccountName,
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						KernelMappings: mappings,
					},
				},
				Selector: map[string]string{"key": "value"},
			},
		}

		returnedMld := api.ModuleLoaderData{
			ContainerImage:     imageName,
			Name:               mod.Name,
			Namespace:          mod.Namespace,
			ServiceAccountName: serviceAccountName,
			Selector:           mod.Spec.Selector,
			KernelVersion:      kernelVersion,
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

		dsByKernelVersion := make(map[string]*appsv1.DaemonSet)

		mr := NewModuleReconciler(clnt, mockBM, mockSM, mockDC, mockKM, mockMetrics, nil, mockSU, namespace)

		ds := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: moduleName + "-",
				Namespace:    namespace,
			},
		}

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, req.NamespacedName, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, m *kmmv1beta1.Module, _ ...ctrlclient.GetOption) error {
					m.ObjectMeta = mod.ObjectMeta
					m.Spec = mod.Spec
					return nil
				},
			),
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
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
			mockKM.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&returnedMld, nil),
			mockDC.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, moduleName, namespace).Return(dsByKernelVersion, nil),
			mockBM.EXPECT().ShouldSync(gomock.Any(), &returnedMld).Return(true, nil),
			mockBM.EXPECT().Sync(gomock.Any(), &returnedMld, true, returnedMld.Owner).Return(utils.Status(utils.StatusCompleted), nil),
			mockMetrics.EXPECT().SetCompletedStage(moduleName, namespace, kernelVersion, metrics.BuildStage, true),
			mockSM.EXPECT().ShouldSync(gomock.Any(), &returnedMld).Return(true, nil),
			mockSM.EXPECT().Sync(gomock.Any(), &returnedMld, "", true, returnedMld.Owner).Return(utils.Status(utils.StatusCompleted), nil),
			mockMetrics.EXPECT().SetCompletedStage(moduleName, namespace, kernelVersion, metrics.SignStage, true),
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(apierrors.NewNotFound(schema.GroupResource{}, "whatever")),
			mockDC.EXPECT().SetDriverContainerAsDesired(context.Background(), &ds, gomock.AssignableToTypeOf(&returnedMld)),
			clnt.EXPECT().Create(ctx, gomock.Any()).Return(nil),
			mockMetrics.EXPECT().SetCompletedStage(moduleName, namespace, kernelVersion, metrics.ModuleLoaderStage, false),
			mockDC.EXPECT().GarbageCollect(ctx, dsByKernelVersion, sets.New[string](kernelVersion)),
			mockBM.EXPECT().GarbageCollect(ctx, mod.Name, mod.Namespace, &mod),
			mockSM.EXPECT().GarbageCollect(ctx, mod.Name, mod.Namespace, &mod),
			mockSU.EXPECT().ModuleUpdateStatus(ctx, &mod, nodeList.Items, nodeList.Items, dsByKernelVersion).Return(nil),
		)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should patch the DaemonSet when it already exists", func() {
		const (
			imageName          = "test-image"
			kernelVersion      = "1.2.3"
			serviceAccountName = "module-loader-service-account"
		)

		mappings := []kmmv1beta1.KernelMapping{
			{
				ContainerImage: imageName,
				Literal:        kernelVersion,
			},
		}

		nodeLabels := map[string]string{"key": "value"}

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					ServiceAccountName: serviceAccountName,
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						KernelMappings: mappings,
					},
				},
				Selector: nodeLabels,
			},
		}

		returnedMld := api.ModuleLoaderData{
			ContainerImage:     imageName,
			Name:               mod.Name,
			Namespace:          mod.Namespace,
			ServiceAccountName: serviceAccountName,
			Selector:           mod.Spec.Selector,
			KernelVersion:      kernelVersion,
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

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, req.NamespacedName, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, m *kmmv1beta1.Module, _ ...ctrlclient.GetOption) error {
					m.ObjectMeta = mod.ObjectMeta
					m.Spec = mod.Spec
					return nil
				},
			),
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
					list.Items = []kmmv1beta1.Module{mod}
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

		mr := NewModuleReconciler(clnt, mockBM, mockSM, mockDC, mockKM, mockMetrics, nil, mockSU, namespace)

		dsByKernelVersion := map[string]*appsv1.DaemonSet{kernelVersion: &ds}

		gomock.InOrder(
			mockKM.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&returnedMld, nil),
			mockDC.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, moduleName, namespace).Return(dsByKernelVersion, nil),
			mockBM.EXPECT().ShouldSync(gomock.Any(), &returnedMld).Return(true, nil),
			mockBM.EXPECT().Sync(gomock.Any(), &returnedMld, true, returnedMld.Owner).Return(utils.Status(utils.StatusCompleted), nil),
			mockMetrics.EXPECT().SetCompletedStage(moduleName, namespace, kernelVersion, metrics.BuildStage, true),
			mockSM.EXPECT().ShouldSync(gomock.Any(), &returnedMld).Return(true, nil),
			mockSM.EXPECT().Sync(gomock.Any(), &returnedMld, "", true, returnedMld.Owner).Return(utils.Status(utils.StatusCompleted), nil),
			mockMetrics.EXPECT().SetCompletedStage(moduleName, namespace, kernelVersion, metrics.SignStage, true),
			mockDC.EXPECT().SetDriverContainerAsDesired(context.Background(), &ds, gomock.AssignableToTypeOf(&returnedMld)).Do(
				func(ctx context.Context, d *appsv1.DaemonSet, _ *api.ModuleLoaderData) {
					d.SetLabels(map[string]string{"test": "test"})
				}),
			mockDC.EXPECT().GarbageCollect(ctx, dsByKernelVersion, sets.New[string](kernelVersion)),
			mockBM.EXPECT().GarbageCollect(ctx, mod.Name, mod.Namespace, &mod),
			mockSM.EXPECT().GarbageCollect(ctx, mod.Name, mod.Namespace, &mod),
			mockSU.EXPECT().ModuleUpdateStatus(ctx, &mod, nodeList.Items, nodeList.Items, dsByKernelVersion).Return(nil),
		)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should create a Device plugin if defined in the module", func() {
		const (
			imageName     = "test-image"
			kernelVersion = "1.2.3"

			moduleLoaderServiceAccountName = "module-loader-service-account"
			devicePluginServiceAccountName = "device-plugin-service-account"
		)

		mappings := []kmmv1beta1.KernelMapping{
			{
				ContainerImage: imageName,
				Literal:        kernelVersion,
			},
		}

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ModuleSpec{
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{
					ServiceAccountName: devicePluginServiceAccountName,
				},
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					ServiceAccountName: moduleLoaderServiceAccountName,
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						KernelMappings: mappings,
					},
				},
				Selector: map[string]string{"key": "value"},
			},
		}

		mr := NewModuleReconciler(clnt, mockBM, mockSM, mockDC, mockKM, mockMetrics, nil, mockSU, namespace)

		ds := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName + "-device-plugin",
				Namespace: namespace,
			},
		}

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, req.NamespacedName, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, m *kmmv1beta1.Module, _ ...ctrlclient.GetOption) error {
					m.ObjectMeta = mod.ObjectMeta
					m.Spec = mod.Spec
					return nil
				},
			),
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
					return nil
				},
			),
			mockMetrics.EXPECT().SetExistingKMMOModules(0),
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *v1.NodeList, _ ...interface{}) error {
					list.Items = []v1.Node{}
					return nil
				},
			),
			mockDC.EXPECT().ModuleDaemonSetsByKernelVersion(ctx, moduleName, namespace).Return(nil, nil),
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(apierrors.NewNotFound(schema.GroupResource{}, "whatever")),
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(apierrors.NewNotFound(schema.GroupResource{}, "whatever")),
			mockDC.EXPECT().SetDevicePluginAsDesired(context.Background(), &ds, gomock.AssignableToTypeOf(&mod)),
			clnt.EXPECT().Create(ctx, gomock.Any()).Return(nil),
			mockMetrics.EXPECT().SetCompletedStage(moduleName, namespace, "", metrics.DevicePluginStage, false),
			mockDC.EXPECT().GarbageCollect(ctx, nil, sets.New[string]()),
			mockBM.EXPECT().GarbageCollect(ctx, mod.Name, mod.Namespace, &mod),
			mockSM.EXPECT().GarbageCollect(ctx, mod.Name, mod.Namespace, &mod),
			mockSU.EXPECT().ModuleUpdateStatus(ctx, &mod, []v1.Node{}, []v1.Node{}, nil).Return(nil),
		)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})
})

var _ = Describe("ModuleReconciler_handleBuild", func() {
	var (
		ctrl        *gomock.Controller
		clnt        *client.MockClient
		mockBM      *build.MockManager
		mockSM      *sign.MockSignManager
		mockDC      *daemonset.MockDaemonSetCreator
		mockKM      *module.MockKernelMapper
		mockMetrics *metrics.MockMetrics
		mockSU      *statusupdater.MockModuleStatusUpdater
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockBM = build.NewMockManager(ctrl)
		mockSM = sign.NewMockSignManager(ctrl)
		mockDC = daemonset.NewMockDaemonSetCreator(ctrl)
		mockKM = module.NewMockKernelMapper(ctrl)
		mockMetrics = metrics.NewMockMetrics(ctrl)
		mockSU = statusupdater.NewMockModuleStatusUpdater(ctrl)
	})

	const (
		moduleName    = "test-module"
		kernelVersion = "1.2.3"
		imageName     = "test-image"
	)

	It("should do nothing when build is skipped", func() {
		mld := &api.ModuleLoaderData{KernelVersion: kernelVersion}

		gomock.InOrder(
			mockBM.EXPECT().ShouldSync(gomock.Any(), mld).Return(false, nil),
		)

		mr := NewModuleReconciler(clnt, mockBM, mockSM, mockDC, mockKM, mockMetrics, nil, mockSU, namespace)

		completed, err := mr.handleBuild(context.Background(), mld)
		Expect(err).NotTo(HaveOccurred())
		Expect(completed).To(BeTrue())
	})

	It("should record that a job was created when the build sync returns StatusCreated", func() {
		mld := api.ModuleLoaderData{
			Name:           moduleName,
			Namespace:      namespace,
			ContainerImage: imageName,
			Build:          &kmmv1beta1.Build{},
			KernelVersion:  kernelVersion,
		}

		gomock.InOrder(
			mockBM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
			mockBM.EXPECT().Sync(gomock.Any(), &mld, true, mld.Owner).Return(utils.Status(utils.StatusCreated), nil),
			mockMetrics.EXPECT().SetCompletedStage(mld.Name, mld.Namespace, kernelVersion, metrics.BuildStage, false),
		)

		mr := NewModuleReconciler(clnt, mockBM, mockSM, mockDC, mockKM, mockMetrics, nil, mockSU, namespace)
		completed, err := mr.handleBuild(context.Background(), &mld)
		Expect(err).NotTo(HaveOccurred())
		Expect(completed).To(BeFalse())
	})

	It("should record that a job was completed, when the build sync returns StatusCompleted", func() {
		mld := &api.ModuleLoaderData{
			Name:           moduleName,
			Namespace:      namespace,
			ContainerImage: imageName,
			Build:          &kmmv1beta1.Build{},
			Owner:          &kmmv1beta1.Module{},
			KernelVersion:  kernelVersion,
		}
		gomock.InOrder(
			mockBM.EXPECT().ShouldSync(gomock.Any(), mld).Return(true, nil),
			mockBM.EXPECT().Sync(gomock.Any(), mld, true, mld.Owner).Return(utils.Status(utils.StatusCompleted), nil),
			mockMetrics.EXPECT().SetCompletedStage(mld.Name, mld.Namespace, kernelVersion, metrics.BuildStage, true),
		)

		mr := NewModuleReconciler(clnt, mockBM, mockSM, mockDC, mockKM, mockMetrics, nil, mockSU, namespace)
		completed, err := mr.handleBuild(context.Background(), mld)
		Expect(err).NotTo(HaveOccurred())
		Expect(completed).To(BeTrue())
	})
})

var _ = Describe("ModuleReconciler_handleSigning", func() {
	var (
		ctrl        *gomock.Controller
		clnt        *client.MockClient
		mockBM      *build.MockManager
		mockSM      *sign.MockSignManager
		mockDC      *daemonset.MockDaemonSetCreator
		mockKM      *module.MockKernelMapper
		mockMetrics *metrics.MockMetrics
		mockSU      *statusupdater.MockModuleStatusUpdater
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockBM = build.NewMockManager(ctrl)
		mockSM = sign.NewMockSignManager(ctrl)
		mockDC = daemonset.NewMockDaemonSetCreator(ctrl)
		mockKM = module.NewMockKernelMapper(ctrl)
		mockMetrics = metrics.NewMockMetrics(ctrl)
		mockSU = statusupdater.NewMockModuleStatusUpdater(ctrl)
	})

	const (
		moduleName    = "test-module"
		kernelVersion = "1.2.3"
		imageName     = "test-image"
	)

	It("should do nothing when build is skipped", func() {
		mld := &api.ModuleLoaderData{
			ContainerImage: imageName,
			KernelVersion:  kernelVersion,
		}

		gomock.InOrder(
			mockSM.EXPECT().ShouldSync(gomock.Any(), mld).Return(false, nil),
		)

		mr := NewModuleReconciler(clnt, mockBM, mockSM, mockDC, mockKM, mockMetrics, nil, mockSU, namespace)

		completed, err := mr.handleSigning(context.Background(), mld)
		Expect(err).NotTo(HaveOccurred())
		Expect(completed).To(BeTrue())
	})

	It("should record that a job was created when the sign sync returns StatusCreated", func() {
		mld := api.ModuleLoaderData{
			Name:           moduleName,
			Namespace:      namespace,
			ContainerImage: imageName,
			Sign:           &kmmv1beta1.Sign{},
			KernelVersion:  kernelVersion,
		}

		gomock.InOrder(
			mockSM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
			mockSM.EXPECT().Sync(gomock.Any(), &mld, "", true, mld.Owner).Return(utils.Status(utils.StatusCreated), nil),
			mockMetrics.EXPECT().SetCompletedStage(mld.Name, mld.Namespace, kernelVersion, metrics.SignStage, false),
		)

		mr := NewModuleReconciler(clnt, mockBM, mockSM, mockDC, mockKM, mockMetrics, nil, mockSU, namespace)

		completed, err := mr.handleSigning(context.Background(), &mld)

		Expect(err).NotTo(HaveOccurred())
		Expect(completed).To(BeFalse())
	})

	It("should record that a job was completed when the sign sync returns StatusCompleted", func() {
		mld := api.ModuleLoaderData{
			Name:           moduleName,
			Namespace:      namespace,
			ContainerImage: imageName,
			Sign:           &kmmv1beta1.Sign{},
			KernelVersion:  kernelVersion,
		}

		gomock.InOrder(
			mockSM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
			mockSM.EXPECT().Sync(gomock.Any(), &mld, "", true, mld.Owner).Return(utils.Status(utils.StatusCompleted), nil),
			mockMetrics.EXPECT().SetCompletedStage(mld.Name, mld.Namespace, kernelVersion, metrics.SignStage, true),
		)

		mr := NewModuleReconciler(clnt, mockBM, mockSM, mockDC, mockKM, mockMetrics, nil, mockSU, namespace)

		completed, err := mr.handleSigning(context.Background(), &mld)

		Expect(err).NotTo(HaveOccurred())
		Expect(completed).To(BeTrue())
	})

	It("should run sign sync with the previous image as well when module build and sign are specified", func() {
		mld := &api.ModuleLoaderData{
			Name:           moduleName,
			Namespace:      namespace,
			ContainerImage: imageName,
			Sign:           &kmmv1beta1.Sign{},
			Build:          &kmmv1beta1.Build{},
			Owner:          &kmmv1beta1.Module{},
			KernelVersion:  kernelVersion,
		}

		gomock.InOrder(
			mockSM.EXPECT().ShouldSync(gomock.Any(), mld).Return(true, nil),
			mockSM.EXPECT().Sync(gomock.Any(), mld, imageName+":"+namespace+"_"+moduleName+"_kmm_unsigned", true, mld.Owner).
				Return(utils.Status(utils.StatusCompleted), nil),
			mockMetrics.EXPECT().SetCompletedStage(mld.Name, mld.Namespace, kernelVersion, metrics.SignStage, true),
		)

		mr := NewModuleReconciler(clnt, mockBM, mockSM, mockDC, mockKM, mockMetrics, nil, mockSU, namespace)

		completed, err := mr.handleSigning(context.Background(), mld)

		Expect(err).NotTo(HaveOccurred())
		Expect(completed).To(BeTrue())
	})
})

var _ = Describe("ModuleReconciler_getNodesListBySelector", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
	})

	ctx := context.Background()
	mod := kmmv1beta1.Module{
		Spec: kmmv1beta1.ModuleSpec{
			Selector: map[string]string{"key": "value"},
		},
	}

	It("no nodes with matching labels", func() {
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *v1.NodeList, _ ...interface{}) error {
				list.Items = []v1.Node{}
				return nil
			},
		)
		mr := NewModuleReconciler(clnt, nil, nil, nil, nil, nil, nil, nil, namespace)
		nodeList, err := mr.getNodesListBySelector(context.Background(), &mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodeList)).To(Equal(0))
	})

	It("2 nodes with matching labels, all schedulable", func() {
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *v1.NodeList, _ ...interface{}) error {
				list.Items = []v1.Node{v1.Node{}, v1.Node{}}
				return nil
			},
		)
		mr := NewModuleReconciler(clnt, nil, nil, nil, nil, nil, nil, nil, namespace)
		nodeList, err := mr.getNodesListBySelector(context.Background(), &mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodeList)).To(Equal(2))
	})

	It("2 nodes with matching labels, 1 not schedulable", func() {
		notSchedulableNode := v1.Node{
			Spec: v1.NodeSpec{
				Taints: []v1.Taint{
					{
						Effect: v1.TaintEffectNoSchedule,
					},
				},
			},
		}
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *v1.NodeList, _ ...interface{}) error {
				list.Items = []v1.Node{notSchedulableNode, v1.Node{}}
				return nil
			},
		)
		mr := NewModuleReconciler(clnt, nil, nil, nil, nil, nil, nil, nil, namespace)
		nodeList, err := mr.getNodesListBySelector(context.Background(), &mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(nodeList)).To(Equal(1))
	})
})
