package cluster

import (
	"context"
	"errors"
	"sort"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	hubv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
)

const (
	mcmName = "test-module"

	namespace = "namespace"
)

var _ = Describe("SelectedManagedClusters", func() {
	var c ClusterAPI

	BeforeEach(func() {
		c = NewClusterAPI(clnt, mockKM, "")
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
		c = NewClusterAPI(clnt, mockKM, "")
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

var _ = Describe("GetModuleLoaderDataForKernel", func() {

	It("should work as expected", func() {

		const kernelVersion = "v1.2.3"

		mockKernelAPI := module.NewMockKernelMapper(ctrl)
		clusterAPI := NewClusterAPI(nil, mockKernelAPI, namespace)

		mcm := &hubv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mcmName,
				Namespace: namespace,
			},
			Spec: hubv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{},
			},
		}

		expectedMod := &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mcm.Name,
				Namespace: mcm.Namespace,
			},
			Spec: mcm.Spec.ModuleSpec,
		}

		mockKernelAPI.EXPECT().GetModuleLoaderDataForKernel(expectedMod, kernelVersion)

		_, _ = clusterAPI.GetModuleLoaderDataForKernel(mcm, kernelVersion)
	})
})
