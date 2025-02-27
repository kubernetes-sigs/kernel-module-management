package pod

import (
	"context"
	"errors"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/buildsign/pod"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

type podManager struct {
	client              client.Client
	maker               Maker
	buildSignPodManager pod.BuildSignPodManager
	registry            registry.Registry
}

func NewBuildManager(
	client client.Client,
	maker Maker,
	buildSignPodManager pod.BuildSignPodManager,
	registry registry.Registry) *podManager {
	return &podManager{
		client:              client,
		maker:               maker,
		buildSignPodManager: buildSignPodManager,
		registry:            registry,
	}
}

func (pm *podManager) GarbageCollect(ctx context.Context, modName, namespace string, owner metav1.Object) ([]string, error) {
	pods, err := pm.buildSignPodManager.GetModulePods(ctx, modName, namespace, pod.PodTypeBuild, owner)
	if err != nil {
		return nil, fmt.Errorf("failed to get build pods for module %s: %v", modName, err)
	}

	deleteNames := make([]string, 0, len(pods))
	for _, pod := range pods {
		if pod.Status.Phase == v1.PodSucceeded {
			err = pm.buildSignPodManager.DeletePod(ctx, &pod)
			if err != nil {
				return nil, fmt.Errorf("failed to delete build pod %s: %v", pod.Name, err)
			}
			deleteNames = append(deleteNames, pod.Name)
		}
	}
	return deleteNames, nil
}

func (pm *podManager) ShouldSync(ctx context.Context, mld *api.ModuleLoaderData) (bool, error) {

	// if there is no build specified skip
	if !module.ShouldBeBuilt(mld) {
		return false, nil
	}

	targetImage := mld.ContainerImage

	// if build AND sign are specified, then we will build an intermediate image
	// and let sign produce the one specified in targetImage
	if module.ShouldBeSigned(mld) {
		targetImage = module.IntermediateImageName(mld.Name, mld.Namespace, targetImage)
	}

	// build is specified and targetImage is either the final image or the intermediate image
	// tag, depending on whether sign is specified or not. Either way, if targetImage exists
	// we can skip building it
	exists, err := module.ImageExists(ctx, pm.client, pm.registry, mld, mld.Namespace, targetImage)
	if err != nil {
		return false, fmt.Errorf("failed to check existence of image %s: %w", targetImage, err)
	}

	return !exists, nil
}

func (pm *podManager) Sync(
	ctx context.Context,
	mld *api.ModuleLoaderData,
	pushImage bool,
	owner metav1.Object) (pod.Status, error) {

	logger := log.FromContext(ctx)

	logger.Info("Building in-cluster")

	podTemplate, err := pm.maker.MakePodTemplate(ctx, mld, owner, pushImage)
	if err != nil {
		return "", fmt.Errorf("could not make Pod template: %v", err)
	}

	p, err := pm.buildSignPodManager.GetModulePodByKernel(ctx, mld.Name, mld.Namespace,
		mld.KernelNormalizedVersion, pod.PodTypeBuild, owner)

	if err != nil {
		if !errors.Is(err, pod.ErrNoMatchingPod) {
			return "", fmt.Errorf("error getting the build: %v", err)
		}

		logger.Info("Creating pod")
		err = pm.buildSignPodManager.CreatePod(ctx, podTemplate)
		if err != nil {
			return "", fmt.Errorf("could not create Pod: %v", err)
		}

		return pod.StatusCreated, nil
	}

	changed, err := pm.buildSignPodManager.IsPodChanged(p, podTemplate)
	if err != nil {
		return "", fmt.Errorf("could not determine if pod has changed: %v", err)
	}

	if changed {
		logger.Info("The module's build spec has been changed, deleting the current pod so a new one can be created", "name", p.Name)
		err = pm.buildSignPodManager.DeletePod(ctx, p)
		if err != nil {
			logger.Info(utils.WarnString(fmt.Sprintf("failed to delete build pod %s: %v", p.Name, err)))
		}
		return pod.StatusInProgress, nil
	}

	logger.Info("Returning pod status", "name", p.Name, "namespace", p.Namespace)

	statusmsg, err := pm.buildSignPodManager.GetPodStatus(p)
	if err != nil {
		return "", err
	}

	return statusmsg, nil
}
