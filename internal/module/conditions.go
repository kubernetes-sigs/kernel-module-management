package module

import (
	"context"

	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	errored     = "Errored"
	progressing = "Progressing"
	ready       = "Ready"
)

//go:generate mockgen -source=conditions.go -package=module -destination=mock_conditions.go

type ConditionsUpdater interface {
	SetAsReady(ctx context.Context, mod *ootov1alpha1.Module, reason, message string) error
	SetAsProgressing(ctx context.Context, mod *ootov1alpha1.Module, reason, message string) error
	SetAsErrored(ctx context.Context, mod *ootov1alpha1.Module, reason, message string) error
}

type conditionsUpdater struct {
	sw client.StatusWriter
}

func NewConditionsUpdater(sw client.StatusWriter) ConditionsUpdater {
	return &conditionsUpdater{sw: sw}
}

// SetAsProgressing sets the Module's Progressing condition as true, Ready and Errored conditions to false, and updates the status in the API.
func (cu *conditionsUpdater) SetAsProgressing(ctx context.Context, mod *ootov1alpha1.Module, reason, message string) error {
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: progressing, Status: metav1.ConditionTrue, Reason: reason, Message: message})
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: ready, Status: metav1.ConditionFalse, Reason: progressing})
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: errored, Status: metav1.ConditionFalse, Reason: progressing})

	return cu.sw.Update(ctx, mod)
}

// SetAsReady changes SpecialResource's Ready condition as true and changes Progressing and Errored conditions to false, and updates the status in the API.
func (cu *conditionsUpdater) SetAsReady(ctx context.Context, mod *ootov1alpha1.Module, reason, message string) error {
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: ready, Status: metav1.ConditionTrue, Reason: reason, Message: message})
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: progressing, Status: metav1.ConditionFalse, Reason: ready})
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: errored, Status: metav1.ConditionFalse, Reason: ready})

	return cu.sw.Update(ctx, mod)
}

// SetAsErrored changes SpecialResource's Errored condition as true and changes Ready and Progressing conditions to false, and updates the status in the API.
func (cu *conditionsUpdater) SetAsErrored(ctx context.Context, mod *ootov1alpha1.Module, reason, message string) error {
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: errored, Status: metav1.ConditionTrue, Reason: reason, Message: message})
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: ready, Status: metav1.ConditionFalse, Reason: errored})
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: progressing, Status: metav1.ConditionFalse, Reason: errored})

	return cu.sw.Update(ctx, mod)
}
