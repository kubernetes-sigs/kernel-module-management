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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	kmmv1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	"github.com/qbarrand/oot-operator/internal/filter"
	"github.com/qbarrand/oot-operator/internal/preflight"
	"github.com/qbarrand/oot-operator/internal/statusupdater"
	"github.com/qbarrand/oot-operator/internal/utils"
)

const reconcileRequeueInSeconds = 60

// ClusterPreflightReconciler reconciles a PreflightValidation object
type PreflightValidationReconciler struct {
	client        client.Client
	filter        *filter.Filter
	statusUpdater statusupdater.PreflightStatusUpdater
	preflight     preflight.PreflightAPI
}

func NewPreflightValidationReconciler(
	client client.Client,
	filter *filter.Filter,
	statusUpdater statusupdater.PreflightStatusUpdater,
	preflight preflight.PreflightAPI) *PreflightValidationReconciler {
	return &PreflightValidationReconciler{
		client:        client,
		filter:        filter,
		statusUpdater: statusUpdater,
		preflight:     preflight}
}

func (r *PreflightValidationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("preflightvalidation").
		For(&kmmv1beta1.PreflightValidation{}).
		Watches(
			&source.Kind{Type: &kmmv1beta1.Module{}},
			handler.EnqueueRequestsFromMapFunc(r.filter.EnqueueAllPreflightValidations),
			builder.WithPredicates(filter.PreflightReconcilerModulePredicate()),
		).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: 1,
		}).
		Complete(r)
}

//+kubebuilder:rbac:groups=kmm.sigs.k8s.io,resources=modules,verbs=get;list;watch
//+kubebuilder:rbac:groups=kmm.sigs.k8s.io,resources=preflightvalidations,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kmm.sigs.k8s.io,resources=preflightvalidations/status,verbs=get;update;patch

// Reconcile Reconiliation entry point
func (r *PreflightValidationReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrl.LoggerFrom(ctx)
	log.Info("Start PreflightValidation Reconciliation")

	pv := kmmv1beta1.PreflightValidation{}
	err := r.client.Get(ctx, req.NamespacedName, &pv)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Reconciliation object not found; not reconciling")
			return ctrl.Result{}, nil
		}
		log.Error(err, "preflight validation reconcile failed to find object")
		return ctrl.Result{}, err
	}

	reconCompleted, err := r.runPreflightValidation(ctx, &pv)
	if err != nil {
		log.Error(err, "runPreflightValidation failed")
		return ctrl.Result{}, err
	}

	if reconCompleted {
		log.Info("PreflightValidation reconciliation success")
		return ctrl.Result{}, nil
	}
	log.Info("PreflightValidation reconciliation requeue")
	return ctrl.Result{RequeueAfter: time.Second * reconcileRequeueInSeconds}, nil
}

func (r *PreflightValidationReconciler) runPreflightValidation(ctx context.Context, pv *kmmv1beta1.PreflightValidation) (bool, error) {
	log := ctrl.LoggerFrom(ctx)

	modulesToCheck, err := r.getModulesToCheck(ctx, pv)
	if err != nil {
		return false, fmt.Errorf("failed to get list of modules to check for preflight: %w", err)
	}

	for _, module := range modulesToCheck {
		log.Info("start module preflight validation", "name", module.Name)

		verified, message := r.preflight.PreflightUpgradeCheck(ctx, &module, pv.Spec.KernelVersion)

		log.Info("module preflight validation result", "name", module.Name, "verified", verified)

		r.updatePreflightStatus(ctx, pv, module.Name, message, verified)
	}

	return r.checkPreflightCompletion(ctx, pv.Name, pv.Namespace)
}

func (r *PreflightValidationReconciler) getModulesToCheck(ctx context.Context, pv *kmmv1beta1.PreflightValidation) ([]kmmv1beta1.Module, error) {
	log := ctrl.LoggerFrom(ctx)

	modulesList := kmmv1beta1.ModuleList{}
	err := r.client.List(ctx, &modulesList)
	if err != nil {
		return nil, fmt.Errorf("failed to get list of all Modules: %w", err)
	}

	err = r.presetModulesStatuses(ctx, pv, modulesList.Items)
	if err != nil {
		return nil, fmt.Errorf("failed to preset new modules' statuses: %w", err)
	}

	modulesToCheck := make([]kmmv1beta1.Module, 0, len(modulesList.Items))
	for _, module := range modulesList.Items {
		if module.GetDeletionTimestamp() != nil {
			log.Info("Module is marked for deletion, skipping preflight validation", "name", module.Name)
			continue
		}
		if pv.Status.CRStatuses[module.Name].VerificationStatus != kmmv1beta1.VerificationTrue {
			modulesToCheck = append(modulesToCheck, module)
		}
	}
	return modulesToCheck, nil
}

func (r *PreflightValidationReconciler) updatePreflightStatus(ctx context.Context, pv *kmmv1beta1.PreflightValidation, moduleName, message string, verified bool) {
	log := ctrl.LoggerFrom(ctx)
	verificationStatus := kmmv1beta1.VerificationFalse
	if verified {
		verificationStatus = kmmv1beta1.VerificationTrue
	}
	err := r.statusUpdater.PreflightSetVerificationStatus(ctx, pv, moduleName, verificationStatus, message)
	if err != nil {
		log.Info(utils.WarnString("failed to update the status of Module CR in preflight"), "module", moduleName, "error", err)
	}
}

func (r *PreflightValidationReconciler) presetModulesStatuses(ctx context.Context, pv *kmmv1beta1.PreflightValidation, modules []kmmv1beta1.Module) error {
	if pv.Status.CRStatuses == nil {
		pv.Status.CRStatuses = make(map[string]*kmmv1beta1.CRStatus, len(modules))
	}
	existingModulesName := sets.NewString()
	newModulesNames := make([]string, 0, len(modules))
	for _, module := range modules {
		if module.GetDeletionTimestamp() != nil {
			continue
		}
		existingModulesName.Insert(module.Name)
		if _, ok := pv.Status.CRStatuses[module.Name]; ok {
			continue
		}
		newModulesNames = append(newModulesNames, module.Name)
	}
	return r.statusUpdater.PreflightPresetStatuses(ctx, pv, existingModulesName, newModulesNames)
}

func (r *PreflightValidationReconciler) checkPreflightCompletion(ctx context.Context, name, namespace string) (bool, error) {
	pv := kmmv1beta1.PreflightValidation{}
	err := r.client.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, &pv)
	if err != nil {
		return false, fmt.Errorf("failed to get preflight validation object in checkPreflightCompletion: %w", err)
	}

	for modName, crStatus := range pv.Status.CRStatuses {
		if crStatus.VerificationStatus != kmmv1beta1.VerificationTrue {
			ctrl.LoggerFrom(ctx).Info("at least one Module is not verified yet", "module", modName, "status", crStatus.VerificationStatus)
			return false, nil
		}
	}

	return true, nil
}
