package pod

import (
	"context"
	"errors"
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/buildsign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/kernel"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

type podManager struct {
	client              client.Client
	maker               Maker
	signer              Signer
	buildSignPodManager BuildSignPodManager
}

func NewManager(client client.Client, helper buildsign.Helper, scheme *runtime.Scheme) buildsign.Manager {
	buildSignPodManager := NewBuildSignPodManager(client)
	maker := NewMaker(client, helper, buildSignPodManager, scheme)
	signer := NewSigner(client, scheme, buildSignPodManager)
	return &podManager{
		client:              client,
		maker:               maker,
		signer:              signer,
		buildSignPodManager: buildSignPodManager,
	}
}

func (pm *podManager) GetStatus(ctx context.Context, name, namespace, kernelVersion string,
	action kmmv1beta1.BuildOrSignAction, owner metav1.Object) (kmmv1beta1.BuildOrSignStatus, error) {
	podType := PodTypeBuild
	if action == kmmv1beta1.SignImage {
		podType = PodTypeSign
	}
	normalizedKernel := kernel.NormalizeVersion(kernelVersion)
	foundPod, err := pm.buildSignPodManager.GetModulePodByKernel(ctx, name, namespace, normalizedKernel, podType, owner)
	if err != nil {
		if !errors.Is(err, ErrNoMatchingPod) {
			return kmmv1beta1.BuildOrSignStatus(""), fmt.Errorf("failed to get pod %s/%s, action %s: %v", namespace, name, action, err)
		}
		return kmmv1beta1.BuildOrSignStatus(""), nil
	}
	status, err := pm.buildSignPodManager.GetPodStatus(foundPod)
	if err != nil {
		return kmmv1beta1.BuildOrSignStatus(""), fmt.Errorf("failed to get status from the pod %s/%s, action %s: %v",
			foundPod.Namespace, foundPod.Name, action, err)
	}
	switch status {
	case StatusCompleted:
		return kmmv1beta1.ActionSuccess, nil
	case StatusFailed:
		return kmmv1beta1.ActionFailure, nil
	}

	// any other status means the pod is still not finished, returning empty status
	return kmmv1beta1.BuildOrSignStatus(""), nil
}

func (pm *podManager) Sync(ctx context.Context, mld *api.ModuleLoaderData, pushImage bool, action kmmv1beta1.BuildOrSignAction, owner metav1.Object) error {
	logger := log.FromContext(ctx)
	var (
		podType     string
		podTemplate *v1.Pod
		err         error
	)
	switch action {
	case kmmv1beta1.BuildImage:
		logger.Info("Building in-cluster")
		podType = PodTypeBuild
		podTemplate, err = pm.maker.MakePodTemplate(ctx, mld, owner, pushImage)
	case kmmv1beta1.SignImage:
		logger.Info("Signing in-cluster")
		podType = PodTypeSign
		podTemplate, err = pm.signer.MakePodTemplate(ctx, mld, owner, pushImage)
	default:
		return fmt.Errorf("invalid action %s", action)
	}

	if err != nil {
		return fmt.Errorf("could not make Pod template: %v", err)
	}

	p, err := pm.buildSignPodManager.GetModulePodByKernel(ctx, mld.Name, mld.Namespace,
		mld.KernelNormalizedVersion, podType, owner)

	if err != nil {
		if !errors.Is(err, ErrNoMatchingPod) {
			return fmt.Errorf("error getting the %s pod: %v", podType, err)
		}

		logger.Info("Creating pod")
		err = pm.buildSignPodManager.CreatePod(ctx, podTemplate)
		if err != nil {
			return fmt.Errorf("could not create Pod: %v", err)
		}

		return nil
	}

	changed, err := pm.buildSignPodManager.IsPodChanged(p, podTemplate)
	if err != nil {
		return fmt.Errorf("could not determine if pod has changed: %v", err)
	}

	if changed {
		logger.Info("The module's spec has been changed, deleting the current pod so a new one can be created", "name", p.Name, "action", action)
		err = pm.buildSignPodManager.DeletePod(ctx, p)
		if err != nil {
			logger.Info(utils.WarnString(fmt.Sprintf("failed to delete %s pod %s: %v", podType, p.Name, err)))
		}
	}

	return nil
}

func (pm *podManager) GarbageCollect(ctx context.Context, name, namespace string, action kmmv1beta1.BuildOrSignAction, owner metav1.Object) ([]string, error) {
	podType := PodTypeBuild
	if action == kmmv1beta1.SignImage {
		podType = PodTypeSign
	}
	pods, err := pm.buildSignPodManager.GetModulePods(ctx, name, namespace, podType, owner)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s pods for mbsc %s/%s: %v", podType, namespace, name, err)
	}

	logger := log.FromContext(ctx)
	errs := make([]error, 0, len(pods))
	deletePodsNames := make([]string, 0, len(pods))
	for _, pod := range pods {
		if pod.Status.Phase == v1.PodSucceeded {
			err = pm.buildSignPodManager.DeletePod(ctx, &pod)
			errs = append(errs, err)
			if err != nil {
				logger.Info(utils.WarnString("failed to delete %s pod %s in garbage collection: %v"), podType, pod.Name, err)
				continue
			}
			deletePodsNames = append(deletePodsNames, pod.Name)
		}
	}
	return deletePodsNames, errors.Join(errs...)
}
