package cluster

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	hubv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

const (
	mcmName = "test-module"

	namespace = "namespace"
)

var _ = Describe("RequestedManagedClusterModule", func() {
	var c ClusterAPI

	BeforeEach(func() {
		c = NewClusterAPI(clnt, mockKM, nil, nil, "")
	})

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

		res, err := c.RequestedManagedClusterModule(ctx, nsn)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("generic-error"))
		Expect(res).To(BeNil())
	})
})

var _ = Describe("SelectedManagedClusters", func() {
	var c ClusterAPI

	BeforeEach(func() {
		c = NewClusterAPI(clnt, mockKM, nil, nil, "")
	})

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

		res, err := c.SelectedManagedClusters(ctx, mcm)

		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(Equal(&clusterList))
	})

	It("should return the respective error when the client List request fails", func() {
		ctx := context.Background()

		gomock.InOrder(
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).Return(errors.New("generic-error")),
		)

		res, err := c.SelectedManagedClusters(ctx, &hubv1beta1.ManagedClusterModule{})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("generic-error"))
		Expect(res.Items).To(BeEmpty())
	})
})

var _ = Describe("KernelVersions", func() {
	var c ClusterAPI

	BeforeEach(func() {
		c = NewClusterAPI(clnt, mockKM, nil, nil, "")
	})

	It("should return an error when no cluster claims are found", func() {
		cluster := clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "default",
				Labels: map[string]string{"key": "value"},
			},
		}

		versions, err := c.KernelVersions(cluster)

		Expect(err).To(HaveOccurred())
		Expect(versions).To(BeNil())
	})

	It("should return the sorted kernel versions found in the KMM cluster claim", func() {
		kernelVersions := []string{"2.0.0", "1.0.0"}

		cluster := clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:   "default",
				Labels: map[string]string{"key": "value"},
			},
			Status: clusterv1.ManagedClusterStatus{
				ClusterClaims: []clusterv1.ManagedClusterClaim{
					{
						Name:  constants.KernelVersionsClusterClaimName,
						Value: strings.Join(kernelVersions, "\n"),
					},
				},
			},
		}

		versions, err := c.KernelVersions(cluster)

		Expect(err).ToNot(HaveOccurred())
		Expect(versions).To(HaveLen(2))
		Expect(versions).To(ContainElements(kernelVersions[0], kernelVersions[1]))
		Expect(sort.StringsAreSorted(versions)).To(BeTrue())
	})
})

var _ = Describe("BuildAndSign", func() {
	const (
		imageName = "test-image"

		kernelVersion = "1.2.3"
	)

	var c ClusterAPI

	BeforeEach(func() {
		c = NewClusterAPI(clnt, mockKM, mockBM, mockSM, namespace)
	})

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
			mockKM.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(nil, errors.New("generic-error")),
		)

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

		completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])

		Expect(err).To(HaveOccurred())
		Expect(completed).To(BeFalse())
	})

	It("should do nothing when Build and Sign are not needed", func() {
		gomock.InOrder(
			mockKM.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&mld, nil),
			mockBM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(false, nil),
			mockSM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(false, nil),
		)

		completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])

		Expect(err).ToNot(HaveOccurred())
		Expect(completed).To(BeTrue())
	})

	It("should run build sync if needed", func() {
		gomock.InOrder(
			mockKM.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&mld, nil),
			mockBM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
			mockBM.EXPECT().Sync(gomock.Any(), &mld, true, mcm).Return(utils.Status(utils.StatusCompleted), nil),
			mockSM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(false, nil),
		)

		completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])

		Expect(err).ToNot(HaveOccurred())
		Expect(completed).To(BeTrue())
	})

	It("should return an error when build sync errors", func() {
		gomock.InOrder(
			mockKM.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&mld, nil),
			mockBM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
			mockBM.EXPECT().Sync(gomock.Any(), &mld, true, mcm).Return(utils.Status(""), errors.New("test-error")),
		)

		completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])

		Expect(err).To(HaveOccurred())
		Expect(completed).To(BeFalse())
	})

	It("should run sign sync if needed", func() {
		gomock.InOrder(
			mockKM.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&mld, nil),
			mockBM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(false, nil),
			mockSM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
			mockSM.EXPECT().Sync(gomock.Any(), &mld, "", true, mcm).Return(utils.Status(utils.StatusInProgress), nil),
		)

		completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])

		Expect(err).ToNot(HaveOccurred())
		Expect(completed).To(BeFalse())
	})

	It("should return an error when sign sync errors", func() {
		gomock.InOrder(
			mockKM.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&mld, nil),
			mockBM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(false, nil),
			mockSM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
			mockSM.EXPECT().Sync(gomock.Any(), &mld, "", true, mcm).Return(utils.Status(""), errors.New("test-error")),
		)

		completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])

		Expect(err).To(HaveOccurred())
		Expect(completed).To(BeFalse())
	})

	It("should not run sign sync when build sync does not complete", func() {
		gomock.InOrder(
			mockKM.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&mld, nil),
			mockBM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
			mockBM.EXPECT().Sync(gomock.Any(), &mld, true, mcm).Return(utils.Status(utils.StatusInProgress), nil),
		)

		completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])

		Expect(err).ToNot(HaveOccurred())
		Expect(completed).To(BeFalse())
	})

	It("should run both build sync and sign sync when build is completed", func() {
		gomock.InOrder(
			mockKM.EXPECT().GetModuleLoaderDataForKernel(&mod, kernelVersion).Return(&mld, nil),
			mockBM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
			mockBM.EXPECT().Sync(gomock.Any(), &mld, true, mcm).Return(utils.Status(utils.StatusCompleted), nil),
			mockSM.EXPECT().ShouldSync(gomock.Any(), &mld).Return(true, nil),
			mockSM.EXPECT().Sync(gomock.Any(), &mld, "", true, mcm).Return(utils.Status(utils.StatusCompleted), nil),
		)

		completed, err := c.BuildAndSign(ctx, *mcm, clusterList.Items[0])

		Expect(err).ToNot(HaveOccurred())
		Expect(completed).To(BeTrue())
	})
})

var _ = Describe("GarbageCollectBuildsAndSigns", func() {
	var c ClusterAPI

	BeforeEach(func() {
		c = NewClusterAPI(clnt, nil, mockBM, mockSM, namespace)
	})

	DescribeTable("return deleted builds and signs if no failure", func(buildsToDeleteFound, signsToDeleteFound bool) {
		deletedBuilds := []string{}
		deletedSigns := []string{}
		if buildsToDeleteFound {
			deletedBuilds = append(deletedBuilds, "test-build")
		}
		if signsToDeleteFound {
			deletedSigns = append(deletedSigns, "test-sign")
		}
		expectedCollected := append(deletedBuilds, deletedSigns...)
		mcm := hubv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{Name: mcmName},
		}
		ctx := context.Background()

		gomock.InOrder(
			mockBM.EXPECT().GarbageCollect(ctx, mcm.Name, namespace, &mcm).Return(deletedBuilds, nil),
			mockSM.EXPECT().GarbageCollect(ctx, mcm.Name, namespace, &mcm).Return(deletedSigns, nil),
		)

		collected, err := c.GarbageCollectBuildsAndSigns(ctx, mcm)

		Expect(err).ToNot(HaveOccurred())
		Expect(collected).To(Equal(expectedCollected))
	},
		Entry("builds to delete found, signs to delete not found", true, false),
		Entry("builds to delete not found, signs to delete found", false, true),
		Entry("builds to delete found, signs to delete found", true, true),
		Entry("builds to delete not found, signs to delete not found", true, true),
	)

	It("should return an error when builds garbage collection fails", func() {
		mcm := hubv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{Name: mcmName},
		}
		ctx := context.Background()
		collectedBuilds := []string{"test-build"}

		gomock.InOrder(
			mockBM.EXPECT().GarbageCollect(ctx, mcm.Name, namespace, &mcm).Return(collectedBuilds, nil),
			mockSM.EXPECT().GarbageCollect(ctx, mcm.Name, namespace, &mcm).Return(nil, errors.New("test")),
		)

		collected, err := c.GarbageCollectBuildsAndSigns(ctx, mcm)

		Expect(err).To(HaveOccurred())
		Expect(collected).To(BeNil())
	})

	It("should return an error when signs garbage collection fails", func() {
		mcm := hubv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{Name: mcmName},
		}
		ctx := context.Background()

		gomock.InOrder(
			mockBM.EXPECT().GarbageCollect(ctx, mcm.Name, namespace, &mcm).Return(nil, errors.New("test")),
		)

		collected, err := c.GarbageCollectBuildsAndSigns(ctx, mcm)

		Expect(err).To(HaveOccurred())
		Expect(collected).To(BeNil())
	})
})
