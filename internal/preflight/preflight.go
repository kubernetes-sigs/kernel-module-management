package preflight

import (
	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta2"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const VerificationStatusReasonVerified = "Verification successful (%s), this Module will not be verified again in this Preflight CR"

//go:generate mockgen -source=preflight.go -package=preflight -destination=mock_preflight.go

type PreflightAPI interface {
	SetModuleStatus(pv *v1beta2.PreflightValidation, namespace, name, status, reason string)
	GetModuleStatus(pv *v1beta2.PreflightValidation, namespace, name string) string
	AllModulesVerified(pv *v1beta2.PreflightValidation) bool
}

func NewPreflightAPI() PreflightAPI {
	return &preflight{}
}

type preflight struct {
}

func (p *preflight) SetModuleStatus(pv *v1beta2.PreflightValidation, namespace, name, status, reason string) {
	stage := v1beta2.VerificationStageImage
	if status == v1beta2.VerificationSuccess || status == v1beta2.VerificationFailure {
		stage = v1beta2.VerificationStageDone
	}
	newStatus := v1beta2.PreflightValidationModuleStatus{
		Name:      name,
		Namespace: namespace,
		CRBaseStatus: v1beta2.CRBaseStatus{
			VerificationStatus: status,
			StatusReason:       reason,
			VerificationStage:  stage,
			LastTransitionTime: metav1.Now(),
		},
	}
	for i, moduleStatus := range pv.Status.Modules {
		if moduleStatus.Name == name && moduleStatus.Namespace == namespace {
			pv.Status.Modules[i] = newStatus
			return
		}
	}

	pv.Status.Modules = append(pv.Status.Modules, newStatus)
}

func (p *preflight) GetModuleStatus(pv *v1beta2.PreflightValidation, namespace, name string) string {
	for _, moduleStatus := range pv.Status.Modules {
		if moduleStatus.Name == name && moduleStatus.Namespace == namespace {
			return moduleStatus.VerificationStatus
		}
	}
	return ""
}

func (p *preflight) AllModulesVerified(pv *v1beta2.PreflightValidation) bool {
	for _, moduleStatus := range pv.Status.Modules {
		if !(moduleStatus.VerificationStatus == v1beta2.VerificationSuccess || moduleStatus.VerificationStatus == v1beta2.VerificationFailure) {
			return false
		}
	}
	return true
}
