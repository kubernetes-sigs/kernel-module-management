package cluster

import (
	"context"
	"errors"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	hubv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

var _ = Describe("ClusterAPI", func() {
	var (
		ctrl   *gomock.Controller
		clnt   *client.MockClient
		mockKM *module.MockKernelMapper
		mockBM *build.MockManager
		mockSM *sign.MockSignManager
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockKM = module.NewMockKernelMapper(ctrl)
		mockBM = build.NewMockManager(ctrl)
		mockSM = sign.NewMockSignManager(ctrl)
	})

	const (
		mcmName = "test-module"

		namespace = "namespace"
	)

	var _ = Describe("RequestedManagedClusterModule", func() {
		It("should return the requested ManagedClusterModule", func() {
			mcm := &hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcmName,
					Namespace: namespace,
				},
				Spec: hubv1beta1.ManagedClusterModuleSpec{
					ModuleSpec: kmmv1beta1.ModuleSpec{},
					Selector:   map[string]string{"key": "value"},
				},
			}

			nsn := types.NamespacedName{
				Name:      mcmName,
				Namespace: namespace,
			}

			ctx := context.Background()

			gomock.InOrder(
				clnt.EXPECT().Get(ctx, nsn, gomock.Any()).DoAndReturn(
					func(_ interface{}, _ interface{}, m *hubv1beta1.ManagedClusterModule, _ ...ctrlclient.GetOption) error {
						m.ObjectMeta = mcm.ObjectMeta
						m.Spec = mcm.Spec
						return nil
					},
				),
			)

			c := NewClusterAPI(clnt, mockKM, nil, nil, "")

			res, err := c.RequestedManagedClusterModule(ctx, nsn)

			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal(mcm))
		})

		It("should return the respective error when the client Get request fails", func() {
			nsn := types.NamespacedName{
				Name:      mcmName,
				Namespace: namespace,
			}

			ctx := context.Background()

			gomock.InOrder(
				clnt.EXPECT().Get(ctx, nsn, gomock.Any()).Return(errors.New("generic-error")),
			)

			c := NewClusterAPI(clnt, mockKM, nil, nil, "")

			res, err := c.RequestedManagedClusterModule(ctx, nsn)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("generic-error"))
			Expect(res).To(BeNil())
		})
	})

	var _ = Describe("SelectedManagedClusters", func() {
		It("should return the ManagedClusters matching the ManagedClusterModule selector", func() {
			mcm := &hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcmName,
					Namespace: namespace,
				},
				Spec: hubv1beta1.ManagedClusterModuleSpec{
					ModuleSpec: kmmv1beta1.ModuleSpec{},
					Selector:   map[string]string{"key": "value"},
				},
			}

			clusterList := clusterv1.ManagedClusterList{
				Items: []clusterv1.ManagedCluster{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "default",
							Labels: map[string]string{"key": "value"},
						},
					},
				},
			}

			ctx := context.Background()

			gomock.InOrder(
				clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ interface{}, list *clusterv1.ManagedClusterList, _ ...interface{}) error {
						list.Items = clusterList.Items
						return nil
					},
				),
			)

			c := NewClusterAPI(clnt, mockKM, nil, nil, "")

			res, err := c.SelectedManagedClusters(ctx, mcm)

			Expect(err).ToNot(HaveOccurred())
			Expect(res).To(Equal(&clusterList))
		})

		It("should return the respective error when the client List request fails", func() {
			ctx := context.Background()

			gomock.InOrder(
				clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).Return(errors.New("generic-error")),
			)

			c := NewClusterAPI(clnt, mockKM, nil, nil, "")

			res, err := c.SelectedManagedClusters(ctx, &hubv1beta1.ManagedClusterModule{})

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("generic-error"))
			Expect(res.Items).To(BeEmpty())
		})
	})

	var _ = Describe("BuildAndSign", func() {
		const (
			imageName = "test-image"

			kernelVersion = "1.2.3"
		)

		It("should do nothing when no kernel mappings are found", func() {
			osConfig := module.NodeOSConfig{}

			mcm := &hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcmName,
					Namespace: namespace,
				},
				Spec: hubv1beta1.ManagedClusterModuleSpec{
					ModuleSpec: kmmv1beta1.ModuleSpec{},
					Selector:   map[string]string{"key": "value"},
				},
			}

			clusterList := clusterv1.ManagedClusterList{
				Items: []clusterv1.ManagedCluster{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "default",
							Labels: map[string]string{"key": "value"},
						},
						Status: clusterv1.ManagedClusterStatus{
							ClusterClaims: []clusterv1.ManagedClusterClaim{
								{
									Name:  clusterClaimName,
									Value: kernelVersion,
								},
							},
						},
					},
				},
			}

			ctx := context.Background()

			gomock.InOrder(
				mockKM.EXPECT().GetNodeOSConfigFromKernelVersion(kernelVersion).Return(&osConfig),
				mockKM.EXPECT().FindMappingForKernel(mcm.Spec.ModuleSpec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(nil, errors.New("generic-error")),
			)

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, "")

			requeue, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(requeue).To(BeFalse())
		})

		It("should return an error when ClusterClaims are not found or empty", func() {
			mappings := []kmmv1beta1.KernelMapping{
				{
					ContainerImage: imageName,
					Literal:        kernelVersion,
				},
			}

			mcm := &hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcmName,
					Namespace: namespace,
				},
				Spec: hubv1beta1.ManagedClusterModuleSpec{
					ModuleSpec: kmmv1beta1.ModuleSpec{
						ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
							Container: kmmv1beta1.ModuleLoaderContainerSpec{
								KernelMappings: mappings,
							},
						},
					},
					Selector: map[string]string{"key": "value"},
				},
			}

			clusterList := clusterv1.ManagedClusterList{
				Items: []clusterv1.ManagedCluster{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "default",
							Labels: map[string]string{"key": "value"},
						},
					},
				},
			}

			ctx := context.Background()

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, "")

			requeue, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).To(HaveOccurred())
			Expect(requeue).To(BeFalse())
		})

		It("should do nothing when Build and Sign are not needed", func() {
			osConfig := module.NodeOSConfig{}

			mappings := []kmmv1beta1.KernelMapping{
				{
					ContainerImage: imageName,
					Literal:        kernelVersion,
				},
			}

			mcm := &hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{
					Name: mcmName,
				},
				Spec: hubv1beta1.ManagedClusterModuleSpec{
					JobNamespace: "test",
					ModuleSpec: kmmv1beta1.ModuleSpec{
						ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
							Container: kmmv1beta1.ModuleLoaderContainerSpec{
								KernelMappings: mappings,
							},
						},
					},
					Selector: map[string]string{"key": "value"},
				},
			}

			clusterList := clusterv1.ManagedClusterList{
				Items: []clusterv1.ManagedCluster{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "default",
							Labels: map[string]string{"key": "value"},
						},
						Status: clusterv1.ManagedClusterStatus{
							ClusterClaims: []clusterv1.ManagedClusterClaim{
								{
									Name:  clusterClaimName,
									Value: kernelVersion,
								},
							},
						},
					},
				},
			}

			mod := kmmv1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcm.Name,
					Namespace: mcm.Spec.JobNamespace,
				},
				Spec: mcm.Spec.ModuleSpec,
			}

			ctx := context.Background()

			gomock.InOrder(
				mockKM.EXPECT().GetNodeOSConfigFromKernelVersion(kernelVersion).Return(&osConfig),
				mockKM.EXPECT().FindMappingForKernel(mcm.Spec.ModuleSpec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mappings[0], nil),
				mockKM.EXPECT().PrepareKernelMapping(&mappings[0], &osConfig).Return(&mappings[0], nil),
				mockBM.EXPECT().ShouldSync(gomock.Any(), mod, mappings[0]).Return(false, nil),
				mockSM.EXPECT().ShouldSync(gomock.Any(), mod, mappings[0]).Return(false, nil),
			)

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, "")

			requeue, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(requeue).To(BeFalse())
		})

		It("should run build sync if needed", func() {
			osConfig := module.NodeOSConfig{}

			mappings := []kmmv1beta1.KernelMapping{
				{
					ContainerImage: imageName,
					Literal:        kernelVersion,
				},
			}

			mcm := &hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{
					Name: mcmName,
				},
				Spec: hubv1beta1.ManagedClusterModuleSpec{
					JobNamespace: "test",
					ModuleSpec: kmmv1beta1.ModuleSpec{
						ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
							Container: kmmv1beta1.ModuleLoaderContainerSpec{
								KernelMappings: mappings,
							},
						},
					},
					Selector: map[string]string{"key": "value"},
				},
			}

			clusterList := clusterv1.ManagedClusterList{
				Items: []clusterv1.ManagedCluster{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "default",
							Labels: map[string]string{"key": "value"},
						},
						Status: clusterv1.ManagedClusterStatus{
							ClusterClaims: []clusterv1.ManagedClusterClaim{
								{
									Name:  clusterClaimName,
									Value: kernelVersion,
								},
							},
						},
					},
				},
			}

			mod := kmmv1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcm.Name,
					Namespace: mcm.Spec.JobNamespace,
				},
				Spec: mcm.Spec.ModuleSpec,
			}

			ctx := context.Background()

			gomock.InOrder(
				mockKM.EXPECT().GetNodeOSConfigFromKernelVersion(kernelVersion).Return(&osConfig),
				mockKM.EXPECT().FindMappingForKernel(mcm.Spec.ModuleSpec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mappings[0], nil),
				mockKM.EXPECT().PrepareKernelMapping(&mappings[0], &osConfig).Return(&mappings[0], nil),
				mockBM.EXPECT().ShouldSync(gomock.Any(), mod, mappings[0]).Return(true, nil),
				mockBM.EXPECT().Sync(gomock.Any(), mod, mappings[0], kernelVersion, true, mcm),
				mockSM.EXPECT().ShouldSync(gomock.Any(), mod, mappings[0]).Return(false, nil),
			)

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, "")

			requeue, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(requeue).To(BeFalse())
		})

		It("should return an error when build sync errors", func() {
			osConfig := module.NodeOSConfig{}

			mappings := []kmmv1beta1.KernelMapping{
				{
					ContainerImage: imageName,
					Literal:        kernelVersion,
				},
			}

			mcm := &hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{
					Name: mcmName,
				},
				Spec: hubv1beta1.ManagedClusterModuleSpec{
					JobNamespace: "test",
					ModuleSpec: kmmv1beta1.ModuleSpec{
						ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
							Container: kmmv1beta1.ModuleLoaderContainerSpec{
								KernelMappings: mappings,
							},
						},
					},
					Selector: map[string]string{"key": "value"},
				},
			}

			clusterList := clusterv1.ManagedClusterList{
				Items: []clusterv1.ManagedCluster{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "default",
							Labels: map[string]string{"key": "value"},
						},
						Status: clusterv1.ManagedClusterStatus{
							ClusterClaims: []clusterv1.ManagedClusterClaim{
								{
									Name:  clusterClaimName,
									Value: kernelVersion,
								},
							},
						},
					},
				},
			}

			mod := kmmv1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcm.Name,
					Namespace: mcm.Spec.JobNamespace,
				},
				Spec: mcm.Spec.ModuleSpec,
			}

			ctx := context.Background()

			gomock.InOrder(
				mockKM.EXPECT().GetNodeOSConfigFromKernelVersion(kernelVersion).Return(&osConfig),
				mockKM.EXPECT().FindMappingForKernel(mcm.Spec.ModuleSpec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mappings[0], nil),
				mockKM.EXPECT().PrepareKernelMapping(&mappings[0], &osConfig).Return(&mappings[0], nil),
				mockBM.EXPECT().ShouldSync(gomock.Any(), mod, mappings[0]).Return(true, nil),
				mockBM.EXPECT().Sync(gomock.Any(), mod, mappings[0], kernelVersion, true, mcm).Return(build.Result{}, errors.New("test-error")),
			)

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, "")

			requeue, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).To(HaveOccurred())
			Expect(requeue).To(BeFalse())
		})

		It("should run sign sync if needed", func() {
			osConfig := module.NodeOSConfig{}

			mappings := []kmmv1beta1.KernelMapping{
				{
					ContainerImage: imageName,
					Literal:        kernelVersion,
				},
			}

			mcm := &hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{
					Name: mcmName,
				},
				Spec: hubv1beta1.ManagedClusterModuleSpec{
					JobNamespace: "test",
					ModuleSpec: kmmv1beta1.ModuleSpec{
						ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
							Container: kmmv1beta1.ModuleLoaderContainerSpec{
								KernelMappings: mappings,
							},
						},
					},
					Selector: map[string]string{"key": "value"},
				},
			}

			clusterList := clusterv1.ManagedClusterList{
				Items: []clusterv1.ManagedCluster{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "default",
							Labels: map[string]string{"key": "value"},
						},
						Status: clusterv1.ManagedClusterStatus{
							ClusterClaims: []clusterv1.ManagedClusterClaim{
								{
									Name:  clusterClaimName,
									Value: kernelVersion,
								},
							},
						},
					},
				},
			}

			mod := kmmv1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcm.Name,
					Namespace: mcm.Spec.JobNamespace,
				},
				Spec: mcm.Spec.ModuleSpec,
			}

			ctx := context.Background()

			gomock.InOrder(
				mockKM.EXPECT().GetNodeOSConfigFromKernelVersion(kernelVersion).Return(&osConfig),
				mockKM.EXPECT().FindMappingForKernel(mcm.Spec.ModuleSpec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mappings[0], nil),
				mockKM.EXPECT().PrepareKernelMapping(&mappings[0], &osConfig).Return(&mappings[0], nil),
				mockBM.EXPECT().ShouldSync(gomock.Any(), mod, mappings[0]).Return(false, nil),
				mockSM.EXPECT().ShouldSync(gomock.Any(), mod, mappings[0]).Return(true, nil),
				mockSM.EXPECT().Sync(gomock.Any(), mod, mappings[0], kernelVersion, "", true, mcm),
			)

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, "")

			requeue, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(requeue).To(BeFalse())
		})

		It("should return an error when sign sync errors", func() {
			osConfig := module.NodeOSConfig{}

			mappings := []kmmv1beta1.KernelMapping{
				{
					ContainerImage: imageName,
					Literal:        kernelVersion,
				},
			}

			mcm := &hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{
					Name: mcmName,
				},
				Spec: hubv1beta1.ManagedClusterModuleSpec{
					JobNamespace: "test",
					ModuleSpec: kmmv1beta1.ModuleSpec{
						ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
							Container: kmmv1beta1.ModuleLoaderContainerSpec{
								KernelMappings: mappings,
							},
						},
					},
					Selector: map[string]string{"key": "value"},
				},
			}

			clusterList := clusterv1.ManagedClusterList{
				Items: []clusterv1.ManagedCluster{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "default",
							Labels: map[string]string{"key": "value"},
						},
						Status: clusterv1.ManagedClusterStatus{
							ClusterClaims: []clusterv1.ManagedClusterClaim{
								{
									Name:  clusterClaimName,
									Value: kernelVersion,
								},
							},
						},
					},
				},
			}

			mod := kmmv1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcm.Name,
					Namespace: mcm.Spec.JobNamespace,
				},
				Spec: mcm.Spec.ModuleSpec,
			}

			ctx := context.Background()

			gomock.InOrder(
				mockKM.EXPECT().GetNodeOSConfigFromKernelVersion(kernelVersion).Return(&osConfig),
				mockKM.EXPECT().FindMappingForKernel(mcm.Spec.ModuleSpec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mappings[0], nil),
				mockKM.EXPECT().PrepareKernelMapping(&mappings[0], &osConfig).Return(&mappings[0], nil),
				mockBM.EXPECT().ShouldSync(gomock.Any(), mod, mappings[0]).Return(false, nil),
				mockSM.EXPECT().ShouldSync(gomock.Any(), mod, mappings[0]).Return(true, nil),
				mockSM.EXPECT().Sync(gomock.Any(), mod, mappings[0], kernelVersion, "", true, mcm).Return(utils.Result{}, errors.New("test-error")),
			)

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, "")

			requeue, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).To(HaveOccurred())
			Expect(requeue).To(BeFalse())
		})

		It("should run both build sync and sign sync if needed", func() {
			osConfig := module.NodeOSConfig{}

			mappings := []kmmv1beta1.KernelMapping{
				{
					ContainerImage: imageName,
					Literal:        kernelVersion,
				},
			}

			mcm := &hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{
					Name: mcmName,
				},
				Spec: hubv1beta1.ManagedClusterModuleSpec{
					JobNamespace: "test",
					ModuleSpec: kmmv1beta1.ModuleSpec{
						ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
							Container: kmmv1beta1.ModuleLoaderContainerSpec{
								KernelMappings: mappings,
							},
						},
					},
					Selector: map[string]string{"key": "value"},
				},
			}

			clusterList := clusterv1.ManagedClusterList{
				Items: []clusterv1.ManagedCluster{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "default",
							Labels: map[string]string{"key": "value"},
						},
						Status: clusterv1.ManagedClusterStatus{
							ClusterClaims: []clusterv1.ManagedClusterClaim{
								{
									Name:  clusterClaimName,
									Value: kernelVersion,
								},
							},
						},
					},
				},
			}

			mod := kmmv1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcm.Name,
					Namespace: mcm.Spec.JobNamespace,
				},
				Spec: mcm.Spec.ModuleSpec,
			}

			ctx := context.Background()

			gomock.InOrder(
				mockKM.EXPECT().GetNodeOSConfigFromKernelVersion(kernelVersion).Return(&osConfig),
				mockKM.EXPECT().FindMappingForKernel(mcm.Spec.ModuleSpec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mappings[0], nil),
				mockKM.EXPECT().PrepareKernelMapping(&mappings[0], &osConfig).Return(&mappings[0], nil),
				mockBM.EXPECT().ShouldSync(gomock.Any(), mod, mappings[0]).Return(true, nil),
				mockBM.EXPECT().Sync(gomock.Any(), mod, mappings[0], kernelVersion, true, mcm),
				mockSM.EXPECT().ShouldSync(gomock.Any(), mod, mappings[0]).Return(true, nil),
				mockSM.EXPECT().Sync(gomock.Any(), mod, mappings[0], kernelVersion, "", true, mcm),
			)

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, "")

			requeue, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(requeue).To(BeFalse())
		})

		It("should run both build sync and sign sync if needed to the defaultJobNamespace", func() {
			osConfig := module.NodeOSConfig{}

			mappings := []kmmv1beta1.KernelMapping{
				{
					ContainerImage: imageName,
					Literal:        kernelVersion,
				},
			}

			mcm := &hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{
					Name: mcmName,
				},
				Spec: hubv1beta1.ManagedClusterModuleSpec{
					ModuleSpec: kmmv1beta1.ModuleSpec{
						ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
							Container: kmmv1beta1.ModuleLoaderContainerSpec{
								KernelMappings: mappings,
							},
						},
					},
					Selector: map[string]string{"key": "value"},
				},
			}

			clusterList := clusterv1.ManagedClusterList{
				Items: []clusterv1.ManagedCluster{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "default",
							Labels: map[string]string{"key": "value"},
						},
						Status: clusterv1.ManagedClusterStatus{
							ClusterClaims: []clusterv1.ManagedClusterClaim{
								{
									Name:  clusterClaimName,
									Value: kernelVersion,
								},
							},
						},
					},
				},
			}

			defaultJobNamespace := "test-namespace"

			mod := kmmv1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcm.Name,
					Namespace: defaultJobNamespace,
				},
				Spec: mcm.Spec.ModuleSpec,
			}

			ctx := context.Background()

			gomock.InOrder(
				mockKM.EXPECT().GetNodeOSConfigFromKernelVersion(kernelVersion).Return(&osConfig),
				mockKM.EXPECT().FindMappingForKernel(mcm.Spec.ModuleSpec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mappings[0], nil),
				mockKM.EXPECT().PrepareKernelMapping(&mappings[0], &osConfig).Return(&mappings[0], nil),
				mockBM.EXPECT().ShouldSync(gomock.Any(), mod, mappings[0]).Return(true, nil),
				mockBM.EXPECT().Sync(gomock.Any(), mod, mappings[0], kernelVersion, true, mcm),
				mockSM.EXPECT().ShouldSync(gomock.Any(), mod, mappings[0]).Return(true, nil),
				mockSM.EXPECT().Sync(gomock.Any(), mod, mappings[0], kernelVersion, "", true, mcm),
			)

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, defaultJobNamespace)

			requeue, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(requeue).To(BeFalse())
		})
	})

	var _ = Describe("GarbageCollectBuilds", func() {
		It("should return the deleted build names when garbage collection succeeds", func() {
			mcm := hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{
					Name: mcmName,
				},
				Spec: hubv1beta1.ManagedClusterModuleSpec{
					JobNamespace: "test",
				},
			}

			collectedBuilds := []string{"test-build"}

			ctx := context.Background()

			gomock.InOrder(
				mockBM.EXPECT().GarbageCollect(ctx, mcm.Name, mcm.Spec.JobNamespace, &mcm).Return(collectedBuilds, nil),
			)

			c := NewClusterAPI(clnt, nil, mockBM, nil, "")

			collected, err := c.GarbageCollectBuilds(ctx, mcm)

			Expect(err).ToNot(HaveOccurred())
			Expect(collected).To(Equal(collectedBuilds))
		})

		It("should return an error when garbage collection fails", func() {
			mcm := hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{
					Name: mcmName,
				},
				Spec: hubv1beta1.ManagedClusterModuleSpec{
					JobNamespace: "test",
				},
			}
			ctx := context.Background()

			gomock.InOrder(
				mockBM.EXPECT().GarbageCollect(ctx, mcm.Name, mcm.Spec.JobNamespace, &mcm).Return(nil, errors.New("test")),
			)

			c := NewClusterAPI(clnt, nil, mockBM, nil, "")

			_, err := c.GarbageCollectBuilds(ctx, mcm)

			Expect(err).To(HaveOccurred())
		})

		It("should use the defaultJobNamespace if the ManagedClusterModule.Spec.JobNamespace is not specified", func() {
			mcm := hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{
					Name: mcmName,
				},
			}

			collectedBuilds := []string{"test-build"}

			defaultJobNamespace := "test-namspace"

			ctx := context.Background()

			gomock.InOrder(
				mockBM.EXPECT().GarbageCollect(ctx, mcm.Name, defaultJobNamespace, &mcm).Return(collectedBuilds, nil),
			)

			c := NewClusterAPI(clnt, nil, mockBM, nil, defaultJobNamespace)

			collected, err := c.GarbageCollectBuilds(ctx, mcm)

			Expect(err).ToNot(HaveOccurred())
			Expect(collected).To(Equal(collectedBuilds))
		})

	})
})
