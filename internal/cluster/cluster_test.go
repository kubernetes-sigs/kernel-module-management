package cluster

import (
	"context"
	"errors"

	"github.com/golang/mock/gomock"
	"github.com/kubernetes-sigs/kernel-module-management/internal/imgbuild"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	hubv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
)

var _ = Describe("ClusterAPI", func() {
	var (
		ctrl   *gomock.Controller
		clnt   *client.MockClient
		mockKM *api.MockModuleLoaderDataFactory
		mockBM *imgbuild.MockJobManager
		mockSM *imgbuild.MockJobManager
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockKM = api.NewMockModuleLoaderDataFactory(ctrl)
		mockBM = imgbuild.NewMockJobManager(ctrl)
		mockSM = imgbuild.NewMockJobManager(ctrl)
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

		var (
			mld = api.ModuleLoaderData{
				ContainerImage: imageName,
				KernelVersion:  kernelVersion,
			}

			mcm = &hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcmName,
					Namespace: namespace,
				},
				Spec: hubv1beta1.ManagedClusterModuleSpec{
					ModuleSpec: kmmv1beta1.ModuleSpec{},
					Selector:   map[string]string{"key": "value"},
				},
			}

			clusterList = clusterv1.ManagedClusterList{
				Items: []clusterv1.ManagedCluster{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "default",
							Labels: map[string]string{"key": "value"},
						},
						Status: clusterv1.ManagedClusterStatus{
							ClusterClaims: []clusterv1.ManagedClusterClaim{
								{
									Name:  constants.KernelVersionsClusterClaimName,
									Value: kernelVersion,
								},
							},
						},
					},
				},
			}

			mod = kmmv1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      mcm.Name,
					Namespace: namespace,
				},
				Spec: mcm.Spec.ModuleSpec,
			}

			ctx = context.Background()
		)

		It("should do nothing when no kernel mappings are found", func() {
			gomock.InOrder(
				mockKM.EXPECT().FromModule(&mod, kernelVersion).Return(nil, errors.New("generic-error")),
			)

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, namespace)

			completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(completed).To(BeFalse())
		})

		It("should return an error when ClusterClaims are not found or empty", func() {
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

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, namespace)

			completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).To(HaveOccurred())
			Expect(completed).To(BeFalse())
		})

		It("should do nothing when Build and Sign are not needed", func() {
			gomock.InOrder(
				mockKM.EXPECT().FromModule(&mod, kernelVersion).Return(&mld, nil),
				mockBM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(false, nil),
				mockSM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(false, nil),
			)

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, namespace)

			completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(completed).To(BeTrue())
		})

		It("should run build sync if needed", func() {
			gomock.InOrder(
				mockKM.EXPECT().FromModule(&mod, kernelVersion).Return(&mld, nil),
				mockBM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
				mockBM.EXPECT().Sync(gomock.Any(), &mld, true, mcm).Return(imgbuild.StatusCompleted, nil),
				mockSM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(false, nil),
			)

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, namespace)

			completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(completed).To(BeTrue())
		})

		It("should return an error when build sync errors", func() {
			gomock.InOrder(
				mockKM.EXPECT().FromModule(&mod, kernelVersion).Return(&mld, nil),
				mockBM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
				mockBM.EXPECT().Sync(gomock.Any(), &mld, true, mcm).Return(imgbuild.Status(""), errors.New("test-error")),
			)

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, namespace)

			completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).To(HaveOccurred())
			Expect(completed).To(BeFalse())
		})

		It("should run sign sync if needed", func() {
			gomock.InOrder(
				mockKM.EXPECT().FromModule(&mod, kernelVersion).Return(&mld, nil),
				mockBM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(false, nil),
				mockSM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
				mockSM.EXPECT().Sync(gomock.Any(), &mld, true, mcm).Return(imgbuild.StatusInProgress, nil),
			)

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, namespace)

			completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(completed).To(BeFalse())
		})

		It("should return an error when sign sync errors", func() {
			gomock.InOrder(
				mockKM.EXPECT().FromModule(&mod, kernelVersion).Return(&mld, nil),
				mockBM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(false, nil),
				mockSM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
				mockSM.EXPECT().Sync(gomock.Any(), &mld, true, mcm).Return(imgbuild.Status(""), errors.New("test-error")),
			)

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, namespace)

			completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).To(HaveOccurred())
			Expect(completed).To(BeFalse())
		})

		It("should not run sign sync when build sync does not complete", func() {
			gomock.InOrder(
				mockKM.EXPECT().FromModule(&mod, kernelVersion).Return(&mld, nil),
				mockBM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
				mockBM.EXPECT().Sync(gomock.Any(), &mld, true, mcm).Return(imgbuild.StatusInProgress, nil),
			)

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, namespace)

			completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(completed).To(BeFalse())
		})

		It("should run both build sync and sign sync when build is completed", func() {
			gomock.InOrder(
				mockKM.EXPECT().FromModule(&mod, kernelVersion).Return(&mld, nil),
				mockBM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
				mockBM.EXPECT().Sync(gomock.Any(), &mld, true, mcm).Return(imgbuild.StatusCompleted, nil),
				mockSM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
				mockSM.EXPECT().Sync(gomock.Any(), &mld, true, mcm).Return(imgbuild.StatusCompleted, nil),
			)

			c := NewClusterAPI(clnt, mockKM, mockBM, mockSM, namespace)

			completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])
			Expect(err).ToNot(HaveOccurred())
			Expect(completed).To(BeTrue())
		})
	})

	var _ = Describe("GarbageCollectBuilds", func() {
		It("should return the deleted build names when garbage collection succeeds", func() {
			mcm := hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{Name: mcmName},
			}

			collectedBuilds := []string{"test-build"}

			ctx := context.Background()

			gomock.InOrder(
				mockBM.EXPECT().GarbageCollect(ctx, mcm.Name, namespace, &mcm).Return(collectedBuilds, nil),
			)

			c := NewClusterAPI(clnt, nil, mockBM, nil, namespace)

			collected, err := c.GarbageCollectBuilds(ctx, mcm)

			Expect(err).ToNot(HaveOccurred())
			Expect(collected).To(Equal(collectedBuilds))
		})

		It("should return an error when garbage collection fails", func() {
			mcm := hubv1beta1.ManagedClusterModule{
				ObjectMeta: metav1.ObjectMeta{Name: mcmName},
			}
			ctx := context.Background()

			gomock.InOrder(
				mockBM.EXPECT().GarbageCollect(ctx, mcm.Name, namespace, &mcm).Return(nil, errors.New("test")),
			)

			c := NewClusterAPI(clnt, nil, mockBM, nil, namespace)

			_, err := c.GarbageCollectBuilds(ctx, mcm)

			Expect(err).To(HaveOccurred())
		})
	})
})
