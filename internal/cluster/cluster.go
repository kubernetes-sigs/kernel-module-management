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
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
)

const (
	clusterClaimName = "kernel-versions.kmm.node.kubernetes.io"
)

//go:generate mockgen -source=cluster.go -package=cluster -destination=mock_cluster.go

type ClusterAPI interface {
	RequestedManagedClusterModule(ctx context.Context, namespacedName types.NamespacedName) (*hubv1beta1.ManagedClusterModule, error)
	SelectedManagedClusters(ctx context.Context, mcm *hubv1beta1.ManagedClusterModule) (*clusterv1.ManagedClusterList, error)
	BuildAndSign(ctx context.Context, mcm hubv1beta1.ManagedClusterModule, cluster clusterv1.ManagedCluster) (bool, error)
	GarbageCollectBuilds(ctx context.Context, mcm hubv1beta1.ManagedClusterModule) ([]string, error)
}

type clusterAPI struct {
	client              client.Client
	kernelAPI           module.KernelMapper
	buildAPI            build.Manager
	signAPI             sign.SignManager
	defaultJobNamespace string
}

func NewClusterAPI(
	client client.Client,
	kernelAPI module.KernelMapper,
	buildAPI build.Manager,
	signAPI sign.SignManager,
	defaultJobNamespace string) ClusterAPI {
	return &clusterAPI{
		client:              client,
		kernelAPI:           kernelAPI,
		buildAPI:            buildAPI,
		signAPI:             signAPI,
		defaultJobNamespace: defaultJobNamespace,
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

	requeue := false

	modKernelMappings := mcm.Spec.ModuleSpec.ModuleLoader.Container.KernelMappings
	mappings, err := c.kernelMappingsByKernelVersion(ctx, modKernelMappings, cluster)
	if err != nil {
		return false, err
	}

	namespace := c.defaultJobNamespace
	if mcm.Spec.JobNamespace != "" {
		namespace = mcm.Spec.JobNamespace
	}

	mod := kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{
			Name:      mcm.Name,
			Namespace: namespace,
		},
		Spec: mcm.Spec.ModuleSpec,
	}

	for kernelVersion, m := range mappings {
		buildRequeue, err := c.build(ctx, mod, &mcm, m, kernelVersion)
		if err != nil {
			return false, err
		}

		signRequeue, err := c.sign(ctx, mod, &mcm, m, kernelVersion)
		if err != nil {
			return false, err
		}

		if buildRequeue || signRequeue {
			requeue = true
		}
	}

	return requeue, nil
}

func (c *clusterAPI) GarbageCollectBuilds(ctx context.Context, mcm hubv1beta1.ManagedClusterModule) ([]string, error) {
	namespace := c.defaultJobNamespace
	if mcm.Spec.JobNamespace != "" {
		namespace = mcm.Spec.JobNamespace
	}
	return c.buildAPI.GarbageCollect(ctx, mcm.Name, namespace, &mcm)
}

func (c *clusterAPI) kernelMappingsByKernelVersion(
	ctx context.Context,
	modKernelMappings []kmmv1beta1.KernelMapping,
	cluster clusterv1.ManagedCluster) (map[string]*kmmv1beta1.KernelMapping, error) {

	kernelVersions, err := c.kernelVersions(cluster)
	if err != nil {
		return nil, err
	}

	mappings := make(map[string]*kmmv1beta1.KernelMapping)
	logger := log.FromContext(ctx)

	for _, kernelVersion := range kernelVersions {
		osConfig := c.kernelAPI.GetNodeOSConfigFromKernelVersion(kernelVersion)
		kernelVersion := strings.TrimSuffix(kernelVersion, "+")

		kernelVersionLogger := logger.WithValues(
			"kernel version", kernelVersion,
		)

		if image, ok := mappings[kernelVersion]; ok {
			kernelVersionLogger.V(1).Info("Using cached image", "image", image)
			continue
		}

		m, err := c.kernelAPI.FindMappingForKernel(modKernelMappings, kernelVersion)
		if err != nil {
			kernelVersionLogger.Info("no suitable container image found; skipping kernel version")
			continue
		}

		m, err = c.kernelAPI.PrepareKernelMapping(m, osConfig)
		if err != nil {
			kernelVersionLogger.Info("failed to substitute the template variables in the mapping", "error", err)
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
		if clusterClaim.Name != clusterClaimName {
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
		return false, nil
	}

	logger := log.FromContext(ctx).WithValues(
		"kernel version", kernelVersion,
		"image", kernelMapping.ContainerImage)
	buildCtx := log.IntoContext(ctx, logger)

	buildRes, err := c.buildAPI.Sync(buildCtx, mod, *kernelMapping, kernelVersion, true, mcm)
	if err != nil {
		return false, fmt.Errorf("could not synchronize the build: %w", err)
	}

	return buildRes.Requeue, nil
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
		return false, nil
	}

	// if we need to sign AND we've built, then we must have built
	// the intermediate image so must figure out its name
	previousImage := ""
	if module.ShouldBeBuilt(mcm.Spec.ModuleSpec, *kernelMapping) {
		previousImage = module.IntermediateImageName(mod.Name, mod.Namespace, kernelMapping.ContainerImage)
	}

	logger := log.FromContext(ctx).WithValues(
		"kernel version", kernelVersion,
		"image", kernelMapping.ContainerImage)
	signCtx := log.IntoContext(ctx, logger)

	signRes, err := c.signAPI.Sync(signCtx, mod, *kernelMapping, kernelVersion, previousImage, true, mcm)
	if err != nil {
		return false, fmt.Errorf("could not synchronize the signing: %w", err)
	}

	return signRes.Requeue, nil
}
