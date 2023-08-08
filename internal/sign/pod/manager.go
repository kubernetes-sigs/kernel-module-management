package signpod

import (
	"context"
	"errors"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

type signPodManager struct {
	client    client.Client
	signer    Signer
	podHelper utils.PodHelper
	registry  registry.Registry
}

func NewSignPodManager(
	client client.Client,
	signer Signer,
	podHelper utils.PodHelper,
	registry registry.Registry) *signPodManager {
	return &signPodManager{
		client:    client,
		signer:    signer,
		podHelper: podHelper,
		registry:  registry,
	}
}

func (spm *signPodManager) GarbageCollect(ctx context.Context, modName, namespace string, owner metav1.Object) ([]string, error) {
	pods, err := spm.podHelper.GetModulePods(ctx, modName, namespace, utils.PodTypeSign, owner)
	if err != nil {
		return nil, fmt.Errorf("failed to get sign pods for module %s: %v", modName, err)
	}

	deleteNames := make([]string, 0, len(pods))
	for _, pod := range pods {
		if pod.Status.Phase == v1.PodSucceeded {
			err = spm.podHelper.DeletePod(ctx, &pod)
			if err != nil {
				return nil, fmt.Errorf("failed to delete build pod %s: %v", pod.Name, err)
			}
			deleteNames = append(deleteNames, pod.Name)
		}
	}
	return deleteNames, nil
}

func (spm *signPodManager) ShouldSync(ctx context.Context, mld *api.ModuleLoaderData) (bool, error) {

	// if there is no sign specified skip
	if !module.ShouldBeSigned(mld) {
		return false, nil
	}

	exists, err := module.ImageExists(ctx, spm.client, spm.registry, mld, mld.Namespace, mld.ContainerImage)
	if err != nil {
		return false, fmt.Errorf("failed to check existence of image %s: %w", mld.ContainerImage, err)
	}

	return !exists, nil
}

func (spm *signPodManager) Sync(
	ctx context.Context,
	mld *api.ModuleLoaderData,
	imageToSign string,
	pushImage bool,
	owner metav1.Object) (utils.Status, error) {

	logger := log.FromContext(ctx)

	logger.Info("Signing in-cluster")

	labels := spm.podHelper.PodLabels(mld.Name, mld.KernelVersion, "sign")

	podTemplate, err := spm.signer.MakePodTemplate(ctx, mld, labels, imageToSign, pushImage, owner)
	if err != nil {
		return "", fmt.Errorf("could not make Pod template: %v", err)
	}

	pod, err := spm.podHelper.GetModulePodByKernel(ctx, mld.Name, mld.Namespace, mld.KernelVersion, utils.PodTypeSign, owner)
	if err != nil {
		if !errors.Is(err, utils.ErrNoMatchingPod) {
			return "", fmt.Errorf("error getting the signing pod: %v", err)
		}

		logger.Info("Creating pod")
		err = spm.podHelper.CreatePod(ctx, podTemplate)
		if err != nil {
			return "", fmt.Errorf("could not create Signing Pod: %v", err)
		}

		return utils.StatusCreated, nil
	}
	// default, there are no errors, and there is a pod, check if it has changed
	changed, err := spm.podHelper.IsPodChanged(pod, podTemplate)
	if err != nil {
		return "", fmt.Errorf("could not determine if pod has changed: %v", err)
	}

	if changed {
		logger.Info("The module's sign spec has been changed, deleting the current pod so a new one can be created", "name", pod.Name)
		err = spm.podHelper.DeletePod(ctx, pod)
		if err != nil {
			logger.Info(utils.WarnString(fmt.Sprintf("failed to delete signing pod %s: %v", pod.Name, err)))
		}
		return utils.StatusInProgress, nil
	}

	logger.Info("Returning pod status", "name", pod.Name, "namespace", pod.Namespace)

	statusmsg, err := spm.podHelper.GetPodStatus(pod)
	if err != nil {
		return "", err
	}

	return statusmsg, nil
}
