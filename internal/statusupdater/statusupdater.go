package statusupdater

import (
	"context"
	"time"

	kmmv1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	"github.com/qbarrand/oot-operator/internal/daemonset"
	"github.com/qbarrand/oot-operator/internal/metrics"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen -source=statusupdater.go -package=statusupdater -destination=mock_statusupdater.go

type ModuleStatusUpdater interface {
	ModuleUpdateStatus(ctx context.Context, mod *kmmv1beta1.Module, kernelMappingNodes []v1.Node,
		targetedNodes []v1.Node, dsByKernelVersion map[string]*appsv1.DaemonSet) error
}

//go:generate mockgen -source=statusupdater.go -package=statusupdater -destination=mock_statusupdater.go

type PreflightStatusUpdater interface {
	PreflightPresetVerificationStatus(ctx context.Context, pv *kmmv1beta1.PreflightValidation,
		status *kmmv1beta1.CRStatus) error
	PreflightSetVerificationStatus(ctx context.Context, preflight *kmmv1beta1.PreflightValidation, status *kmmv1beta1.CRStatus,
		verificationStatus string, message string) error
	PreflightSetVerificationStage(ctx context.Context, preflight *kmmv1beta1.PreflightValidation,
		status *kmmv1beta1.CRStatus, stage string) error
}

type moduleStatusUpdater struct {
	client     client.Client
	daemonAPI  daemonset.DaemonSetCreator
	metricsAPI metrics.Metrics
}

type preflightStatusUpdater struct {
	client client.Client
}

func NewModuleStatusUpdater(client client.Client, daemonAPI daemonset.DaemonSetCreator, metricsAPI metrics.Metrics) ModuleStatusUpdater {
	return &moduleStatusUpdater{
		client:     client,
		daemonAPI:  daemonAPI,
		metricsAPI: metricsAPI,
	}
}

func NewPreflightStatusUpdater(client client.Client) PreflightStatusUpdater {
	return &preflightStatusUpdater{
		client: client,
	}
}

func (m *moduleStatusUpdater) ModuleUpdateStatus(ctx context.Context,
	mod *kmmv1beta1.Module,
	kernelMappingNodes []v1.Node,
	targetedNodes []v1.Node,
	dsByKernelVersion map[string]*appsv1.DaemonSet) error {

	nodesMatchingSelectorNumber := int32(len(targetedNodes))
	numDesired := int32(len(kernelMappingNodes))
	var numAvailableDevicePlugin int32
	var numAvailableKernelModule int32
	for kernelVersion, ds := range dsByKernelVersion {
		if daemonset.IsDevicePluginKernelVersion(kernelVersion) {
			numAvailableDevicePlugin += ds.Status.NumberAvailable
		} else {
			numAvailableKernelModule += ds.Status.NumberAvailable
		}
	}
	mod.Status.DriverContainer.NodesMatchingSelectorNumber = nodesMatchingSelectorNumber
	mod.Status.DriverContainer.DesiredNumber = numDesired
	mod.Status.DriverContainer.AvailableNumber = numAvailableKernelModule
	if mod.Spec.DevicePlugin != nil {
		mod.Status.DevicePlugin.NodesMatchingSelectorNumber = nodesMatchingSelectorNumber
		mod.Status.DevicePlugin.DesiredNumber = numDesired
		mod.Status.DevicePlugin.AvailableNumber = numAvailableDevicePlugin
	}
	m.updateMetrics(ctx, mod, dsByKernelVersion)
	return m.client.Status().Update(ctx, mod)
}

func (p *preflightStatusUpdater) PreflightPresetVerificationStatus(ctx context.Context,
	pv *kmmv1beta1.PreflightValidation,
	status *kmmv1beta1.CRStatus) error {
	status.VerificationStatus = kmmv1beta1.VerificationFalse
	status.VerificationStage = kmmv1beta1.VerificationStageImage
	status.LastTransitionTime = metav1.NewTime(time.Now())
	return p.client.Status().Update(ctx, pv)
}

func (p *preflightStatusUpdater) PreflightSetVerificationStatus(ctx context.Context, pv *kmmv1beta1.PreflightValidation, status *kmmv1beta1.CRStatus,
	verificationStatus string, message string) error {
	original := pv.DeepCopy()
	status.VerificationStatus = verificationStatus
	status.StatusReason = message
	status.LastTransitionTime = metav1.NewTime(time.Now())
	patch := client.MergeFrom(original)
	return p.client.Status().Patch(ctx, pv, patch)
}

func (p *preflightStatusUpdater) PreflightSetVerificationStage(ctx context.Context, pv *kmmv1beta1.PreflightValidation,
	status *kmmv1beta1.CRStatus, stage string) error {
	original := pv.DeepCopy()
	status.VerificationStage = stage
	status.LastTransitionTime = metav1.NewTime(time.Now())
	patch := client.MergeFrom(original)
	return p.client.Status().Patch(ctx, pv, patch)
}

func (m *moduleStatusUpdater) updateMetrics(ctx context.Context, mod *kmmv1beta1.Module, dsByKernelVersion map[string]*appsv1.DaemonSet) {
	for kernelVersion, ds := range dsByKernelVersion {
		stage := metrics.DriverContainerStage
		if daemonset.IsDevicePluginKernelVersion(kernelVersion) {
			stage = metrics.DevicePluginStage
		}
		m.metricsAPI.SetCompletedStage(mod.Name,
			mod.Namespace,
			kernelVersion,
			stage,
			ds.Status.DesiredNumberScheduled == ds.Status.NumberAvailable)
	}
}
