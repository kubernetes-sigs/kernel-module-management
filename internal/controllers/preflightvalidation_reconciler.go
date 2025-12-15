/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"errors"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta2"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/metrics"
	"github.com/kubernetes-sigs/kernel-module-management/internal/mic"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/preflight"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	PreflightValidationReconcilerName = "PreflightValidation"
)

// PreflightReconciler reconciles a PreflightValidation object
type preflightValidationReconciler struct {
	client       client.Client
	filterAPI    *filter.Filter
	metricsAPI   metrics.Metrics
	helper       preflightReconcilerHelper
	preflightAPI preflight.PreflightAPI
}

func NewPreflightValidationReconciler(
	client client.Client,
	filterAPI *filter.Filter,
	metricsAPI metrics.Metrics,
	micAPI mic.MIC,
	kernelAPI module.KernelMapper,
	preflightAPI preflight.PreflightAPI,
) *preflightValidationReconciler {
	helper := newPreflightReconcilerHelper(client, micAPI, metricsAPI, kernelAPI, preflightAPI)
	return &preflightValidationReconciler{
		client:       client,
		filterAPI:    filterAPI,
		metricsAPI:   metricsAPI,
		helper:       helper,
		preflightAPI: preflightAPI,
	}
}

func (r *preflightValidationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(PreflightValidationReconcilerName).
		For(&v1beta2.PreflightValidation{}, builder.WithPredicates(filter.PreflightReconcilerUpdatePredicate())).
		Owns(&kmmv1beta1.ModuleImagesConfig{}).
		Watches(
			&kmmv1beta1.Module{},
			handler.EnqueueRequestsFromMapFunc(r.filterAPI.EnqueueAllPreflightValidations),
			builder.WithPredicates(filter.PreflightReconcilerUpdatePredicate()),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
		}).
		Complete(
			reconcile.AsReconciler[*v1beta2.PreflightValidation](r.client, r),
		)
}

// Reconcile Reconiliation entry point
func (r *preflightValidationReconciler) Reconcile(ctx context.Context, pv *v1beta2.PreflightValidation) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx).WithValues("preflight name", pv.Name)
	log.Info("Start PreflightValidation Reconciliation")

	// we want the metric just to signal that this customer uses preflight
	r.metricsAPI.SetKMMPreflightsNum(1)

	modsWithMapping, modsWithoutMapping, err := r.helper.getModulesData(ctx, pv)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get modules' data: %v", err)
	}

	err = r.helper.updateStatus(ctx, modsWithMapping, modsWithoutMapping, pv)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update PreflightValidation's status: %v", err)
	}

	err = r.helper.processPreflightValidation(ctx, modsWithMapping, pv)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to process the preflight validation: %v", err)
	}

	if r.preflightAPI.AllModulesVerified(pv) {
		log.Info("PreflightValidation reconciliation success")
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

//go:generate mockgen -source=preflightvalidation_reconciler.go -package=controllers -destination=mock_preflightvalidation_reconciler.go preflightReconcilerHelper
type preflightReconcilerHelper interface {
	updateStatus(ctx context.Context, modsWithMapping []*api.ModuleLoaderData, modsWithoutMapping []types.NamespacedName, pv *v1beta2.PreflightValidation) error
	getModulesData(ctx context.Context, pv *v1beta2.PreflightValidation) ([]*api.ModuleLoaderData, []types.NamespacedName, error)
	processPreflightValidation(ctx context.Context, modsWithMapping []*api.ModuleLoaderData, pv *v1beta2.PreflightValidation) error
}

type preflightReconcilerHelperImpl struct {
	client       client.Client
	micAPI       mic.MIC
	metricsAPI   metrics.Metrics
	kernelAPI    module.KernelMapper
	preflightAPI preflight.PreflightAPI
}

func newPreflightReconcilerHelper(client client.Client,
	micAPI mic.MIC,
	metricsAPI metrics.Metrics,
	kernelAPI module.KernelMapper,
	preflightAPI preflight.PreflightAPI) preflightReconcilerHelper {

	return &preflightReconcilerHelperImpl{
		client:       client,
		micAPI:       micAPI,
		metricsAPI:   metricsAPI,
		kernelAPI:    kernelAPI,
		preflightAPI: preflightAPI,
	}
}

func (p *preflightReconcilerHelperImpl) updateStatus(
	ctx context.Context,
	modsWithMapping []*api.ModuleLoaderData,
	modsWithoutMapping []types.NamespacedName,
	pv *v1beta2.PreflightValidation) error {

	unmodifiedPV := pv.DeepCopy()

	// setting the status for modules without mapping
	for _, mod := range modsWithoutMapping {
		p.preflightAPI.SetModuleStatus(pv, mod.Namespace, mod.Name, v1beta2.VerificationFailure, "mapping not found")
	}

	// setting status for modules with mapping
	for _, mod := range modsWithMapping {
		modStatus := v1beta2.VerificationInProgress
		modReason := "verification is not finished yet"
		foundMIC, err := p.micAPI.Get(ctx, mod.Name+"-preflight", mod.Namespace)
		if err == nil {
			imageStatus := p.micAPI.GetImageState(foundMIC, mod.ContainerImage)
			switch imageStatus {
			case kmmv1beta1.ImageExists:
				modStatus = v1beta2.VerificationSuccess
				modReason = "verified image exists"
			case kmmv1beta1.ImageDoesNotExist:
				modStatus = v1beta2.VerificationFailure
				modReason = "verified image does not exist"
				if mod.Build != nil || mod.Sign != nil {
					modReason += " and build/sign failed"
				}
			}
		}
		p.preflightAPI.SetModuleStatus(pv, mod.Namespace, mod.Name, modStatus, modReason)
	}

	return p.client.Status().Patch(ctx, pv, client.MergeFrom(unmodifiedPV))
}

func (p *preflightReconcilerHelperImpl) getModulesData(ctx context.Context, pv *v1beta2.PreflightValidation) ([]*api.ModuleLoaderData, []types.NamespacedName, error) {
	modulesList := kmmv1beta1.ModuleList{}
	err := p.client.List(ctx, &modulesList)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get list of all Modules: %v", err)
	}

	mldsWithoutMapping := make([]types.NamespacedName, 0, len(modulesList.Items))
	mldsWithMapping := make([]*api.ModuleLoaderData, 0, len(modulesList.Items))
	for _, mod := range modulesList.Items {
		// ignore modules being deleted
		if mod.GetDeletionTimestamp() != nil {
			continue
		}
		mld, err := p.kernelAPI.GetModuleLoaderDataForKernel(&mod, pv.Spec.KernelVersion)
		if err != nil {
			if !errors.Is(err, module.ErrNoMatchingKernelMapping) {
				return nil, nil, fmt.Errorf("failed to get MLD for module %s/%s: %v", mod.Namespace, mod.Name, err)
			}
			mldsWithoutMapping = append(mldsWithoutMapping, types.NamespacedName{Name: mod.Name, Namespace: mod.Namespace})
		} else {
			mldsWithMapping = append(mldsWithMapping, mld)
		}
	}

	return mldsWithMapping, mldsWithoutMapping, nil
}

func (p *preflightReconcilerHelperImpl) processPreflightValidation(ctx context.Context, modsWithMapping []*api.ModuleLoaderData, pv *v1beta2.PreflightValidation) error {
	errs := []error{}
	for _, mod := range modsWithMapping {
		status := p.preflightAPI.GetModuleStatus(pv, mod.Namespace, mod.Name)
		if status == v1beta2.VerificationSuccess || status == v1beta2.VerificationFailure {
			continue
		}

		micObjSpec := kmmv1beta1.ModuleImageSpec{
			Image:         mod.ContainerImage,
			KernelVersion: mod.KernelVersion,
			Build:         mod.Build,
			Sign:          mod.Sign,
			RegistryTLS:   mod.RegistryTLS,
		}
		micName := mod.Name + "-preflight"
		err := p.micAPI.CreateOrPatch(ctx, micName, mod.Namespace, []kmmv1beta1.ModuleImageSpec{micObjSpec},
			mod.ImageRepoSecret, mod.ImagePullPolicy, pv.Spec.PushBuiltImage, nil, pv)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to apply %s/%s MIC: %v", mod.Namespace, mod.Name, err))
		}
	}
	return errors.Join(errs...)
}
