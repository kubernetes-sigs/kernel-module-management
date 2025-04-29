package cluster

import (
	"context"
	"errors"
	"sort"
	"strings"

	hubv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
)

//go:generate mockgen -source=cluster.go -package=cluster -destination=mock_cluster.go

type ClusterAPI interface {
	SelectedManagedClusters(ctx context.Context, mcm *hubv1beta1.ManagedClusterModule) (*clusterv1.ManagedClusterList, error)
	KernelVersions(cluster clusterv1.ManagedCluster) ([]string, error)
	GetModuleLoaderDataForKernel(mcm *hubv1beta1.ManagedClusterModule, kernelVersion string) (*api.ModuleLoaderData, error)
	GetDefaultArtifactsNamespace() string
}

type clusterAPI struct {
	client    client.Client
	kernelAPI module.KernelMapper
	namespace string
}

func NewClusterAPI(
	client client.Client,
	kernelAPI module.KernelMapper,
	defaultPodNamespace string) ClusterAPI {
	return &clusterAPI{
		client:    client,
		kernelAPI: kernelAPI,
		namespace: defaultPodNamespace,
	}
}

func (c *clusterAPI) SelectedManagedClusters(
	ctx context.Context,
	mcm *hubv1beta1.ManagedClusterModule) (*clusterv1.ManagedClusterList, error) {

	clusterList := &clusterv1.ManagedClusterList{}

	opts := []client.ListOption{
		client.MatchingLabelsSelector{
			Selector: labels.Set(mcm.Spec.Selector).AsSelector(),
		},
	}

	err := c.client.List(ctx, clusterList, opts...)

	return clusterList, err
}

func (c *clusterAPI) KernelVersions(cluster clusterv1.ManagedCluster) ([]string, error) {
	for _, clusterClaim := range cluster.Status.ClusterClaims {
		if clusterClaim.Name != constants.KernelVersionsClusterClaimName {
			continue
		}

		kernelVersions := strings.Split(clusterClaim.Value, "\n")
		sort.Strings(kernelVersions)

		return kernelVersions, nil
	}
	return nil, errors.New("KMM kernel version ClusterClaim not found")
}

func (c *clusterAPI) GetModuleLoaderDataForKernel(mcm *hubv1beta1.ManagedClusterModule,
	kernelVersion string) (*api.ModuleLoaderData, error) {

	mod := &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcm.Name,
			Namespace: c.namespace,
		},
		Spec: mcm.Spec.ModuleSpec,
	}
	return c.kernelAPI.GetModuleLoaderDataForKernel(mod, kernelVersion)
}

func (c *clusterAPI) GetDefaultArtifactsNamespace() string {
	return c.namespace
}
