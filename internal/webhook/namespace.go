package webhook

import (
	"context"
	"errors"

	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/meta"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type NamespaceValidator struct{}

func (nd *NamespaceValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	// controller-runtime will set the path to `validate-<group>-<version>-<resource> so we
	// need to make sure it is set correctly in the +kubebuilder annotation below.
	return ctrl.NewWebhookManagedBy(mgr).
		For(&v1.Namespace{}).
		WithValidator(nd).
		Complete()
}

//+kubebuilder:webhook:path=/validate--v1-namespace,mutating=false,failurePolicy=fail,sideEffects=None,groups="",resources=namespaces,verbs=delete,versions=v1,name=namespace-deletion.kmm.sigs.k8s.io,admissionReviewVersions=v1

func (nd *NamespaceValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return nil, NotImplemented
}

func (nd *NamespaceValidator) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	return nil, NotImplemented
}

func (nd *NamespaceValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	// We could just always return some error, as if the webhook was called, the namespace should indeed have the label.
	// Just make another check here to be super safe, and because it's cheap enough.
	if meta.HasLabel(obj.(*v1.Namespace), constants.NamespaceLabelKey) {
		return nil, errors.New("namespace contains one or more Module resources; delete those before deleting the namespace")
	}

	return nil, nil
}
