package preflight

import (
	"context"
	"fmt"
	"slices"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen -source=statusupdater.go -package=preflight -destination=mock_statusupdater.go

type StatusUpdater interface {
	PresetStatuses(
		ctx context.Context,
		pv *v1beta2.PreflightValidation,
		existingModules sets.Set[types.NamespacedName],
		newModules []types.NamespacedName,
	) error
	SetVerificationStatus(
		ctx context.Context,
		preflight *v1beta2.PreflightValidation,
		moduleName types.NamespacedName,
		verificationStatus string,
		message string,
	) error
	SetVerificationStage(
		ctx context.Context,
		preflight *v1beta2.PreflightValidation,
		moduleName types.NamespacedName,
		stage string,
	) error
}

type statusUpdater struct {
	client client.Client
}

func NewStatusUpdater(client client.Client) StatusUpdater {
	return &statusUpdater{
		client: client,
	}
}

func (p *statusUpdater) PresetStatuses(
	ctx context.Context,
	pv *v1beta2.PreflightValidation,
	existingModules sets.Set[types.NamespacedName],
	newModules []types.NamespacedName,
) error {
	pv.Status.Modules = slices.DeleteFunc(pv.Status.Modules, func(status v1beta2.PreflightValidationModuleStatus) bool {
		nsn := types.NamespacedName{
			Namespace: status.Namespace,
			Name:      status.Name,
		}

		return !existingModules.Has(nsn)
	})

	for _, nsn := range newModules {
		status := v1beta2.PreflightValidationModuleStatus{
			Namespace: nsn.Namespace,
			Name:      nsn.Name,
			CRBaseStatus: v1beta2.CRBaseStatus{
				VerificationStatus: kmmv1beta1.VerificationFalse,
				VerificationStage:  kmmv1beta1.VerificationStageImage,
				LastTransitionTime: metav1.Now(),
			},
		}

		pv.Status.Modules = append(pv.Status.Modules, status)
	}

	return p.client.Status().Update(ctx, pv)
}

func (p *statusUpdater) SetVerificationStatus(
	ctx context.Context,
	pv *v1beta2.PreflightValidation,
	moduleName types.NamespacedName,
	verificationStatus string,
	message string,
) error {
	status, ok := FindModuleStatus(pv.Status.Modules, moduleName)
	if !ok {
		return fmt.Errorf("failed to find module status %s in preflight %s", moduleName, pv.Name)
	}

	status.VerificationStatus = verificationStatus
	status.StatusReason = message
	status.LastTransitionTime = metav1.Now()

	return p.client.Status().Update(ctx, pv)
}

func (p *statusUpdater) SetVerificationStage(
	ctx context.Context,
	pv *v1beta2.PreflightValidation,
	moduleName types.NamespacedName,
	stage string,
) error {
	status, ok := FindModuleStatus(pv.Status.Modules, moduleName)
	if !ok {
		return fmt.Errorf("failed to find module status %s in preflight %s", moduleName, pv.Name)
	}

	status.VerificationStage = stage
	status.LastTransitionTime = metav1.Now()

	return p.client.Status().Update(ctx, pv)
}
