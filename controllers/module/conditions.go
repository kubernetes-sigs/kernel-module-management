package module

import (
	"context"

	ootov1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	Errored     = "Errored"
	Progressing = "Progressing"
	Ready       = "Ready"
)

//go:generate mockgen -source=conditions.go -package=module -destination=mock_conditions.go

type ConditionsUpdater interface {
	SetAsReady(ctx context.Context, mod *ootov1beta1.Module, reason, message string) error
	SetAsProgressing(ctx context.Context, mod *ootov1beta1.Module, reason, message string) error
	SetAsErrored(ctx context.Context, mod *ootov1beta1.Module, reason, message string) error
}

type conditionsUpdater struct {
	sw client.StatusWriter
}

func NewConditionsUpdater(sw client.StatusWriter) ConditionsUpdater {
	return &conditionsUpdater{sw: sw}
}

// SetAsProgressing sets the Module's Progressing condition as true, Ready and Errored conditions to false, and updates the status in the API.
func (cu *conditionsUpdater) SetAsProgressing(ctx context.Context, mod *ootov1beta1.Module, reason, message string) error {
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: Progressing, Status: metav1.ConditionTrue, Reason: reason, Message: message})
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: Ready, Status: metav1.ConditionFalse, Reason: Progressing})
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: Errored, Status: metav1.ConditionFalse, Reason: Progressing})

	return cu.sw.Update(ctx, mod)
}

// SetAsReady changes SpecialResource's Ready condition as true and changes Progressing and Errored conditions to false, and updates the status in the API.
func (cu *conditionsUpdater) SetAsReady(ctx context.Context, mod *ootov1beta1.Module, reason, message string) error {
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: Ready, Status: metav1.ConditionTrue, Reason: reason, Message: message})
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: Progressing, Status: metav1.ConditionFalse, Reason: Ready})
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: Errored, Status: metav1.ConditionFalse, Reason: Ready})

	return cu.sw.Update(ctx, mod)
}

// SetAsErrored changes SpecialResource's Errored condition as true and changes Ready and Progressing conditions to false, and updates the status in the API.
func (cu *conditionsUpdater) SetAsErrored(ctx context.Context, mod *ootov1beta1.Module, reason, message string) error {
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: Errored, Status: metav1.ConditionTrue, Reason: reason, Message: message})
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: Ready, Status: metav1.ConditionFalse, Reason: Errored})
	meta.SetStatusCondition(&mod.Status.Conditions, metav1.Condition{Type: Progressing, Status: metav1.ConditionFalse, Reason: Errored})

	return cu.sw.Update(ctx, mod)
}
