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
	"fmt"
	"time"

	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta2"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/metrics"
	"github.com/kubernetes-sigs/kernel-module-management/internal/preflight"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	reconcileRequeueInSeconds = 180

	PreflightValidationReconcilerName = "PreflightValidation"
)

// ClusterPreflightReconciler reconciles a PreflightValidation object
type PreflightValidationReconciler struct {
	client        client.Client
	filter        *filter.Filter
	metricsAPI    metrics.Metrics
	statusUpdater preflight.StatusUpdater
	preflight     preflight.PreflightAPI
}

func NewPreflightValidationReconciler(
	client client.Client,
	filter *filter.Filter,
	metricsAPI metrics.Metrics,
	statusUpdater preflight.StatusUpdater,
	preflight preflight.PreflightAPI,
) *PreflightValidationReconciler {
	return &PreflightValidationReconciler{
		client:        client,
		filter:        filter,
		metricsAPI:    metricsAPI,
		statusUpdater: statusUpdater,
		preflight:     preflight}
}

func (r *PreflightValidationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(PreflightValidationReconcilerName).
		For(&v1beta2.PreflightValidation{}, builder.WithPredicates(filter.PreflightReconcilerUpdatePredicate())).
		Owns(&v1.Pod{}).
		Watches(
			&v1beta1.Module{},
			handler.EnqueueRequestsFromMapFunc(r.filter.EnqueueAllPreflightValidations),
			builder.WithPredicates(filter.PreflightReconcilerUpdatePredicate()),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
		}).
		Complete(
			reconcile.AsReconciler[*v1beta2.PreflightValidation](r.client, r),
		)
}

//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modules,verbs=get;list;watch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=preflightvalidations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=preflightvalidations/status,verbs=get;update;patch

// Reconcile Reconiliation entry point
func (r *PreflightValidationReconciler) Reconcile(ctx context.Context, pv *v1beta2.PreflightValidation) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Start PreflightValidation Reconciliation")

	// we want the metric just to signal that this customer uses preflight
	r.metricsAPI.SetKMMPreflightsNum(1)

	reconCompleted, err := r.runPreflightValidation(ctx, pv)
	if err != nil {
		log.Error(err, "runPreflightValidation failed")
		return ctrl.Result{}, err
	}

	if reconCompleted {
		log.Info("PreflightValidation reconciliation success")
		return ctrl.Result{}, nil
	}
	// if not all the modules has been reconciled, then maybe there is a problem with build
	// configuration. Since build configuration is stored in the ConfigMap, and we cannot watch
	// ConfigMap (since we don't know which ones to watch), then we need a requeue in order to run
	// reconciliation again
	log.Info("PreflightValidation reconciliation requeue")
	return ctrl.Result{RequeueAfter: time.Second * reconcileRequeueInSeconds}, nil
}

func (r *PreflightValidationReconciler) runPreflightValidation(ctx context.Context, pv *v1beta2.PreflightValidation) (bool, error) {
	log := ctrl.LoggerFrom(ctx)

	modulesToCheck, err := r.getModulesToCheck(ctx, pv)
	if err != nil {
		return false, fmt.Errorf("failed to get list of modules to check for preflight: %w", err)
	}

	for _, module := range modulesToCheck {
		log.Info("start module preflight validation", "name", module.Name)

		verified, message := r.preflight.PreflightUpgradeCheck(ctx, pv, &module)

		log.Info("module preflight validation result", "name", module.Name, "verified", verified)

		r.updatePreflightStatus(ctx, pv, types.NamespacedName{Name: module.Name, Namespace: module.Namespace}, message, verified)
	}

	return r.checkPreflightCompletion(ctx, pv.Name)
}

func (r *PreflightValidationReconciler) getModulesToCheck(ctx context.Context, pv *v1beta2.PreflightValidation) ([]v1beta1.Module, error) {
	log := ctrl.LoggerFrom(ctx)

	modulesList := v1beta1.ModuleList{}
	err := r.client.List(ctx, &modulesList)
	if err != nil {
		return nil, fmt.Errorf("failed to get list of all Modules: %w", err)
	}

	err = r.presetModulesStatuses(ctx, pv, modulesList.Items)
	if err != nil {
		return nil, fmt.Errorf("failed to preset new modules' statuses: %w", err)
	}

	modulesToCheck := make([]v1beta1.Module, 0, len(modulesList.Items))
	for _, module := range modulesList.Items {
		if module.GetDeletionTimestamp() != nil {
			log.Info("Module is marked for deletion, skipping preflight validation", "name", module.Name)
			continue
		}

		status, ok := preflight.FindModuleStatus(pv.Status.Modules, types.NamespacedName{Name: module.Name, Namespace: module.Namespace})
		if !ok || status.VerificationStatus != v1beta2.VerificationTrue {
			modulesToCheck = append(modulesToCheck, module)
		}
	}

	return modulesToCheck, nil
}

func (r *PreflightValidationReconciler) updatePreflightStatus(ctx context.Context, pv *v1beta2.PreflightValidation, moduleName types.NamespacedName, message string, verified bool) {
	log := ctrl.LoggerFrom(ctx)
	verificationStatus := v1beta2.VerificationFalse
	verificationStage := v1beta2.VerificationStageRequeued
	if verified {
		verificationStatus = v1beta2.VerificationTrue
		verificationStage = v1beta2.VerificationStageDone
	}
	err := r.statusUpdater.SetVerificationStatus(ctx, pv, moduleName, verificationStatus, message)
	if err != nil {
		log.Info(utils.WarnString("failed to update the status of Module CR in preflight"), "module", moduleName, "error", err)
	}

	err = r.statusUpdater.SetVerificationStage(ctx, pv, moduleName, verificationStage)
	if err != nil {
		log.Info(utils.WarnString("failed to update the stage of Module CR in preflight"), "module", moduleName, "error", err)
	}
}

func (r *PreflightValidationReconciler) presetModulesStatuses(ctx context.Context, pv *v1beta2.PreflightValidation, modules []v1beta1.Module) error {
	if pv.Status.Modules == nil {
		pv.Status.Modules = make([]v1beta2.PreflightValidationModuleStatus, 0, len(modules))
	}

	existingModulesName := sets.New[types.NamespacedName]()
	newModulesNames := make([]types.NamespacedName, 0, len(modules))

	for _, module := range modules {
		if module.GetDeletionTimestamp() != nil {
			continue
		}

		nsn := types.NamespacedName{
			Name:      module.Name,
			Namespace: module.Namespace,
		}

		existingModulesName.Insert(nsn)

		if _, ok := preflight.FindModuleStatus(pv.Status.Modules, nsn); !ok {
			newModulesNames = append(newModulesNames, nsn)
		}
	}
	return r.statusUpdater.PresetStatuses(ctx, pv, existingModulesName, newModulesNames)
}

func (r *PreflightValidationReconciler) checkPreflightCompletion(ctx context.Context, name string) (bool, error) {
	pv := v1beta2.PreflightValidation{}
	err := r.client.Get(ctx, types.NamespacedName{Name: name}, &pv)
	if err != nil {
		return false, fmt.Errorf("failed to get preflight validation object in checkPreflightCompletion: %w", err)
	}

	for _, crStatus := range pv.Status.Modules {
		if crStatus.VerificationStatus != v1beta2.VerificationTrue {
			ctrl.LoggerFrom(ctx).Info("at least one Module is not verified yet", "module name", crStatus.Name, "module namespace", crStatus.Namespace, "status", crStatus.VerificationStatus)
			return false, nil
		}
	}

	return true, nil
}
