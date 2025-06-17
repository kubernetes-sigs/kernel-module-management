package webhook

import (
	"context"
	"fmt"
	"github.com/go-logr/logr"
	kmmv1beta2 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta2"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// PreflightValidationValidator validates PreflightValidation resources.
type PreflightValidationValidator struct {
	logger logr.Logger
}

func NewPreflightValidationValidator(logger logr.Logger) *PreflightValidationValidator {
	return &PreflightValidationValidator{logger: logger}
}

func (v *PreflightValidationValidator) SetupWebhookWithManager(mgr ctrl.Manager, pf *kmmv1beta2.PreflightValidation) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(pf).
		WithValidator(v).
		Complete()
}

//+kubebuilder:webhook:path=/validate-kmm-sigs-x-k8s-io-v1beta2-preflightvalidation,mutating=false,failurePolicy=fail,sideEffects=None,groups=kmm.sigs.x-k8s.io,resources=preflightvalidations,verbs=create;update,versions=v1beta2,name=vpreflightvalidation.kb.io,admissionReviewVersions=v1

func (v *PreflightValidationValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	pv, ok := obj.(*kmmv1beta2.PreflightValidation)
	if !ok {
		return nil, fmt.Errorf("bad type for the object; expected %v, got %v", pv, obj)
	}

	v.logger.Info("Validating PreflightValidation creation", "name", pv.Name)
	return validatePreflight(pv)
}

func (v *PreflightValidationValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	oldPV, ok := oldObj.(*kmmv1beta2.PreflightValidation)
	if !ok {
		return nil, fmt.Errorf("bad type for the old object; expected %v, got %v", oldPV, oldObj)
	}

	newPV, ok := newObj.(*kmmv1beta2.PreflightValidation)
	if !ok {
		return nil, fmt.Errorf("bad type for the new object; expected %v, got %v", newPV, newObj)
	}

	v.logger.Info("Validating PreflightValidation update", "name", oldPV.Name)
	return validatePreflight(newPV)
}

func (v *PreflightValidationValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, NotImplemented
}

func validatePreflight(pv *kmmv1beta2.PreflightValidation) (admission.Warnings, error) {
	if pv.Spec.KernelVersion == "" {
		return nil, fmt.Errorf("kernelVersion cannot be empty")
	}

	fields := utils.KernelRegexp.Split(pv.Spec.KernelVersion, -1)
	if len(fields) < 3 {
		return nil, fmt.Errorf("invalid kernelVersion %s", pv.Spec.KernelVersion)
	}

	return nil, nil
}
