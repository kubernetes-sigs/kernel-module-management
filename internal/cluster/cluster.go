package cluster

import (
	"context"
	"errors"
	"fmt"
	"strings"

	hubv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

//go:generate mockgen -source=cluster.go -package=cluster -destination=mock_cluster.go

type ClusterAPI interface {
	RequestedManagedClusterModule(ctx context.Context, namespacedName types.NamespacedName) (*hubv1beta1.ManagedClusterModule, error)
	SelectedManagedClusters(ctx context.Context, mcm *hubv1beta1.ManagedClusterModule) (*clusterv1.ManagedClusterList, error)
	BuildAndSign(ctx context.Context, mcm hubv1beta1.ManagedClusterModule, cluster clusterv1.ManagedCluster) (bool, error)
	GarbageCollectBuilds(ctx context.Context, mcm hubv1beta1.ManagedClusterModule) ([]string, error)
}

type clusterAPI struct {
	client    client.Client
	kernelAPI module.KernelMapper
	buildAPI  build.Manager
	signAPI   sign.SignManager
	namespace string
}

func NewClusterAPI(
	client client.Client,
	kernelAPI module.KernelMapper,
	buildAPI build.Manager,
	signAPI sign.SignManager,
	defaultJobNamespace string) ClusterAPI {
	return &clusterAPI{
		client:    client,
		kernelAPI: kernelAPI,
		buildAPI:  buildAPI,
		signAPI:   signAPI,
		namespace: defaultJobNamespace,
	}
}

func (c *clusterAPI) RequestedManagedClusterModule(
	ctx context.Context,
	namespacedName types.NamespacedName) (*hubv1beta1.ManagedClusterModule, error) {

	mcm := hubv1beta1.ManagedClusterModule{}
	if err := c.client.Get(ctx, namespacedName, &mcm); err != nil {
		return nil, fmt.Errorf("failed to get the ManagedClusterModule %s: %w", namespacedName, err)
	}
	return &mcm, nil
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

func (c *clusterAPI) BuildAndSign(
	ctx context.Context,
	mcm hubv1beta1.ManagedClusterModule,
	cluster clusterv1.ManagedCluster) (bool, error) {

	modSpec := mcm.Spec.ModuleSpec
	mappings, err := c.kernelMappingsByKernelVersion(ctx, &modSpec, cluster)
	if err != nil {
		return false, err
	}

	// if no mappings were found, return not completed
	if len(mappings) == 0 {
		return false, nil
	}

	mod := kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcm.Name,
			Namespace: c.namespace,
		},
		Spec: modSpec,
	}

	logger := log.FromContext(ctx)

	completedSuccessfully := true
	for kernelVersion, m := range mappings {
		buildCompleted, err := c.build(ctx, mod, &mcm, m, kernelVersion)
		if err != nil {
			return false, err
		}

		kernelVersionLogger := logger.WithValues(
			"kernel version", kernelVersion,
		)

		if !buildCompleted {
			kernelVersionLogger.Info("Build for mapping has not completed yet, skipping Sign")
			completedSuccessfully = false
			continue
		}

		signCompleted, err := c.sign(ctx, mod, &mcm, m, kernelVersion)
		if err != nil {
			return false, err
		}

		if !signCompleted {
			kernelVersionLogger.Info("Sign for mapping has not completed yet")
			completedSuccessfully = false
			continue
		}
	}

	return completedSuccessfully, nil
}

func (c *clusterAPI) GarbageCollectBuilds(ctx context.Context, mcm hubv1beta1.ManagedClusterModule) ([]string, error) {
	return c.buildAPI.GarbageCollect(ctx, mcm.Name, c.namespace, &mcm)
}

func (c *clusterAPI) kernelMappingsByKernelVersion(
	ctx context.Context,
	modSpec *kmmv1beta1.ModuleSpec,
	cluster clusterv1.ManagedCluster) (map[string]*kmmv1beta1.KernelMapping, error) {

	kernelVersions, err := c.kernelVersions(cluster)
	if err != nil {
		return nil, err
	}

	mappings := make(map[string]*kmmv1beta1.KernelMapping)
	logger := log.FromContext(ctx)

	for _, kernelVersion := range kernelVersions {
		kernelVersion := strings.TrimSuffix(kernelVersion, "+")

		kernelVersionLogger := logger.WithValues(
			"kernel version", kernelVersion,
		)

		if image, ok := mappings[kernelVersion]; ok {
			kernelVersionLogger.V(1).Info("Using cached image", "image", image)
			continue
		}

		m, err := c.kernelAPI.GetMergedMappingForKernel(modSpec, kernelVersion)
		if err != nil {
			kernelVersionLogger.Info("no suitable container image found; skipping kernel version")
			continue
		}

		kernelVersionLogger.V(1).Info("Found a valid mapping",
			"image", m.ContainerImage,
			"build", m.Build != nil,
		)

		mappings[kernelVersion] = m
	}

	return mappings, nil
}

func (c *clusterAPI) kernelVersions(cluster clusterv1.ManagedCluster) ([]string, error) {
	for _, clusterClaim := range cluster.Status.ClusterClaims {
		if clusterClaim.Name != constants.KernelVersionsClusterClaimName {
			continue
		}
		return strings.Split(clusterClaim.Value, "\n"), nil
	}
	return nil, errors.New("KMM ClusterClaim not found")
}

func (c *clusterAPI) build(
	ctx context.Context,
	mod kmmv1beta1.Module,
	mcm *hubv1beta1.ManagedClusterModule,
	kernelMapping *kmmv1beta1.KernelMapping,
	kernelVersion string) (bool, error) {

	shouldSync, err := c.buildAPI.ShouldSync(ctx, mod, *kernelMapping)
	if err != nil {
		return false, fmt.Errorf("could not check if build synchronization is needed: %v", err)
	}
	if !shouldSync {
		return true, nil
	}

	logger := log.FromContext(ctx).WithValues(
		"kernel version", kernelVersion,
		"image", kernelMapping.ContainerImage)
	buildCtx := log.IntoContext(ctx, logger)

	buildStatus, err := c.buildAPI.Sync(buildCtx, mod, *kernelMapping, kernelVersion, true, mcm)
	if err != nil {
		return false, fmt.Errorf("could not synchronize the build: %w", err)
	}

	if buildStatus == utils.StatusCompleted {
		return true, nil
	}
	return false, nil
}

func (c *clusterAPI) sign(
	ctx context.Context,
	mod kmmv1beta1.Module,
	mcm *hubv1beta1.ManagedClusterModule,
	kernelMapping *kmmv1beta1.KernelMapping,
	kernelVersion string) (bool, error) {

	shouldSync, err := c.signAPI.ShouldSync(ctx, mod, *kernelMapping)
	if err != nil {
		return false, fmt.Errorf("could not check if signing synchronization is needed: %v", err)
	}
	if !shouldSync {
		return true, nil
	}

	// if we need to sign AND we've built, then we must have built
	// the intermediate image so must figure out its name
	previousImage := ""
	if module.ShouldBeBuilt(*kernelMapping) {
		previousImage = module.IntermediateImageName(mod.Name, mod.Namespace, kernelMapping.ContainerImage)
	}

	logger := log.FromContext(ctx).WithValues(
		"kernel version", kernelVersion,
		"image", kernelMapping.ContainerImage)
	signCtx := log.IntoContext(ctx, logger)

	signStatus, err := c.signAPI.Sync(signCtx, mod, *kernelMapping, kernelVersion, previousImage, true, mcm)
	if err != nil {
		return false, fmt.Errorf("could not synchronize the signing: %w", err)
	}

	if signStatus == utils.StatusCompleted {
		return true, nil
	}
	return false, nil
}
