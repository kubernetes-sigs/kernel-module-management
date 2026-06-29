/*
Copyright 2022.

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
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/node"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	DRAReconcilerName = "DRAReconciler"

	kubeletPluginsVolumeName         = "kubelet-plugins"
	kubeletPluginsPath               = "/var/lib/kubelet/plugins/"
	kubeletPluginsRegistryVolumeName = "kubelet-plugins-registry"
	kubeletPluginsRegistryPath       = "/var/lib/kubelet/plugins_registry/"
	cdiVolumeName                    = "cdi"
	cdiPath                          = "/var/run/cdi"

	draHealthcheckPort = 51515
)

type DRAReconciler struct {
	client         client.Client
	reconHelperAPI draReconcilerHelperAPI
}

func NewDRAReconciler(
	client client.Client,
	nodeAPI node.Node,
	scheme *runtime.Scheme,
) *DRAReconciler {
	reconHelperAPI := newDRAReconcilerHelper(client, nodeAPI, scheme)
	return &DRAReconciler{
		client:         client,
		reconHelperAPI: reconHelperAPI,
	}
}

func (r *DRAReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kmmv1beta1.Module{}).
		Owns(&appsv1.DaemonSet{}).
		Watches(
			&resourcev1.DeviceClass{},
			handler.EnqueueRequestsFromMapFunc(filter.DeviceClassToModuleReconcileRequest),
			builder.WithPredicates(filter.HasLabel(constants.ModuleNameLabel)),
		).
		Named(DRAReconcilerName).
		Complete(
			reconcile.AsReconciler[*kmmv1beta1.Module](r.client, r),
		)
}

func (r *DRAReconciler) Reconcile(ctx context.Context, mod *kmmv1beta1.Module) (ctrl.Result, error) {
	res := ctrl.Result{}

	logger := log.FromContext(ctx)

	existingDRADS, err := r.reconHelperAPI.getModuleDRADaemonSets(ctx, mod.Name, mod.Namespace)
	if err != nil {
		return res, fmt.Errorf("could not get DRA DaemonSets for module %s, namespace %s: %v", mod.Name, mod.Namespace, err)
	}

	existingDCs, err := r.reconHelperAPI.getModuleDeviceClasses(ctx, mod.Name, mod.Namespace)
	if err != nil {
		return res, fmt.Errorf("could not get DeviceClasses for module %s, namespace %s: %v", mod.Name, mod.Namespace, err)
	}

	if mod.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, r.reconHelperAPI.deleteDRAResources(ctx, mod.Name, mod.Namespace)
	}

	if mod.Spec.DRA == nil {
		if err = r.reconHelperAPI.deleteDRAResources(ctx, mod.Name, mod.Namespace); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, r.reconHelperAPI.clearDRAStatus(ctx, mod)
	}

	err = r.reconHelperAPI.handleDRA(ctx, mod, existingDRADS)
	if err != nil {
		return res, fmt.Errorf("could not handle DRA: %v", err)
	}

	err = r.reconHelperAPI.garbageCollectDRADaemonSets(ctx, mod, existingDRADS)
	if err != nil {
		return res, fmt.Errorf("failed to run DRA garbage collection: %v", err)
	}

	err = r.reconHelperAPI.handleDeviceClasses(ctx, mod, existingDCs)
	if err != nil {
		return res, fmt.Errorf("could not handle DeviceClasses: %v", err)
	}

	err = r.reconHelperAPI.moduleUpdateDRAStatus(ctx, mod, existingDRADS)
	if err != nil {
		return res, fmt.Errorf("failed to update DRA status of the module: %v", err)
	}

	logger.Info("DRA reconcile loop finished successfully")

	return res, nil
}

//go:generate mockgen -source=dra_reconciler.go -package=controllers -destination=mock_dra_reconciler.go draReconcilerHelperAPI,draDaemonSetCreator

type draReconcilerHelperAPI interface {
	getModuleDRADaemonSets(ctx context.Context, name, namespace string) ([]appsv1.DaemonSet, error)
	handleDRA(ctx context.Context, mod *kmmv1beta1.Module, existingDRADS []appsv1.DaemonSet) error
	garbageCollectDRADaemonSets(ctx context.Context, mod *kmmv1beta1.Module, existingDS []appsv1.DaemonSet) error
	deleteDRAResources(ctx context.Context, moduleName, moduleNamespace string) error
	moduleUpdateDRAStatus(ctx context.Context, mod *kmmv1beta1.Module, existingDRADS []appsv1.DaemonSet) error
	clearDRAStatus(ctx context.Context, mod *kmmv1beta1.Module) error
	getModuleDeviceClasses(ctx context.Context, name, namespace string) ([]resourcev1.DeviceClass, error)
	handleDeviceClasses(ctx context.Context, mod *kmmv1beta1.Module, existingDCs []resourcev1.DeviceClass) error
}

type draReconcilerHelper struct {
	client          client.Client
	daemonSetHelper draDaemonSetCreator
	nodeAPI         node.Node
}

func newDRAReconcilerHelper(client client.Client,
	nodeAPI node.Node,
	scheme *runtime.Scheme,
) draReconcilerHelperAPI {
	daemonSetHelper := newDRADaemonSetCreator(scheme)
	return &draReconcilerHelper{
		client:          client,
		daemonSetHelper: daemonSetHelper,
		nodeAPI:         nodeAPI,
	}
}

func (drh *draReconcilerHelper) getModuleDRADaemonSets(ctx context.Context, name, namespace string) ([]appsv1.DaemonSet, error) {
	dsList := appsv1.DaemonSetList{}
	opts := []client.ListOption{
		client.MatchingLabels(map[string]string{
			constants.ModuleNameLabel: name,
			constants.DaemonSetRole:   constants.DRARoleLabelValue,
		}),
		client.InNamespace(namespace),
	}
	if err := drh.client.List(ctx, &dsList, opts...); err != nil {
		return nil, fmt.Errorf("could not list DaemonSets: %v", err)
	}

	return dsList.Items, nil
}

func (drh *draReconcilerHelper) handleDRA(ctx context.Context, mod *kmmv1beta1.Module, existingDRADS []appsv1.DaemonSet) error {
	if mod.Spec.DRA == nil {
		return nil
	}

	logger := log.FromContext(ctx)

	ds, version := getExistingDRADSFromVersion(existingDRADS, mod.Namespace, mod.Name, mod.Spec.ModuleLoader)
	if ds == nil {
		logger.Info("creating new DRA DaemonSet", "version", version)
		ds = &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: mod.Namespace, GenerateName: mod.Name + "-dra-"},
		}
	}

	opRes, err := controllerutil.CreateOrPatch(ctx, drh.client, ds, func() error {
		return drh.daemonSetHelper.setDRAAsDesired(ctx, ds, mod)
	})

	if err == nil {
		logger.Info("Reconciled DRA", "name", ds.Name, "result", opRes)
	}

	return err
}

func (drh *draReconcilerHelper) garbageCollectDRADaemonSets(ctx context.Context, mod *kmmv1beta1.Module, existingDS []appsv1.DaemonSet) error {
	if mod.Spec.ModuleLoader == nil {
		return nil
	}

	logger := log.FromContext(ctx)
	deleted := make([]string, 0)
	for _, ds := range existingDS {
		if isOlderVersionUnusedDRADaemonSet(&ds, mod.Namespace, mod.Spec.ModuleLoader.Container.Version) {
			deleted = append(deleted, ds.Name)
			if err := drh.client.Delete(ctx, &ds); err != nil {
				return fmt.Errorf("could not delete DRA DaemonSet %s: %v", ds.Name, err)
			}
		}
	}

	logger.Info("garbage-collected DRA DaemonSets", "names", deleted)
	return nil
}

func getExistingDRADSFromVersion(existingDS []appsv1.DaemonSet,
	moduleNamespace string,
	moduleName string,
	moduleLoader *kmmv1beta1.ModuleLoaderSpec) (*appsv1.DaemonSet, string) {
	version := ""
	if moduleLoader != nil {
		version = moduleLoader.Container.Version
	}

	versionLabel := utils.GetSchedulePluginVersionLabelName(moduleNamespace, moduleName)
	for _, ds := range existingDS {
		dsModuleVersion := ds.GetLabels()[versionLabel]
		if dsModuleVersion == version {
			return &ds, version
		}
	}
	return nil, version
}

func isOlderVersionUnusedDRADaemonSet(ds *appsv1.DaemonSet, moduleNamespace, moduleVersion string) bool {
	moduleName := ds.Labels[constants.ModuleNameLabel]
	versionLabel := utils.GetSchedulePluginVersionLabelName(moduleNamespace, moduleName)
	return ds.Labels[versionLabel] != moduleVersion && ds.Status.DesiredNumberScheduled == 0
}

// deleteDRAResources deletes all DRA-owned DaemonSets and DeviceClasses using label-based bulk deletion.
func (drh *draReconcilerHelper) deleteDRAResources(ctx context.Context, moduleName, moduleNamespace string) error {
	var errs []error

	dsDeleteOpts := []client.DeleteAllOfOption{
		client.MatchingLabels{
			constants.ModuleNameLabel: moduleName,
			constants.DaemonSetRole:   constants.DRARoleLabelValue,
		},
		client.InNamespace(moduleNamespace),
	}
	if err := drh.client.DeleteAllOf(ctx, &appsv1.DaemonSet{}, dsDeleteOpts...); err != nil {
		errs = append(errs, fmt.Errorf("failed to delete DRA DaemonSets for module %s/%s: %v", moduleNamespace, moduleName, err))
	}

	dcDeleteOpts := []client.DeleteAllOfOption{
		client.MatchingLabels{
			constants.ModuleNameLabel:      moduleName,
			constants.ModuleNamespaceLabel: moduleNamespace,
		},
	}
	if err := drh.client.DeleteAllOf(ctx, &resourcev1.DeviceClass{}, dcDeleteOpts...); err != nil {
		errs = append(errs, fmt.Errorf("failed to delete DeviceClasses for module %s/%s: %v", moduleNamespace, moduleName, err))
	}

	return errors.Join(errs...)
}

func (drh *draReconcilerHelper) moduleUpdateDRAStatus(ctx context.Context,
	mod *kmmv1beta1.Module,
	existingDRADS []appsv1.DaemonSet) error {

	if mod.Spec.DRA == nil {
		return nil
	}

	numTargetedNodes, err := drh.nodeAPI.GetNumTargetedNodes(ctx, mod.Spec.Selector, mod.Spec.Tolerations)
	if err != nil {
		return fmt.Errorf("failed to determine the number of nodes targeted by Module %s/%s selector: %v", mod.Namespace, mod.Name, err)
	}

	numAvailable := int32(0)
	for _, ds := range existingDRADS {
		numAvailable += ds.Status.NumberAvailable
	}

	unmodifiedMod := mod.DeepCopy()

	mod.Status.DRA.NodesMatchingSelectorNumber = int32(numTargetedNodes)
	mod.Status.DRA.DesiredNumber = int32(numTargetedNodes)
	mod.Status.DRA.AvailableNumber = numAvailable

	return drh.client.Status().Patch(ctx, mod, client.MergeFrom(unmodifiedMod))
}

func (drh *draReconcilerHelper) clearDRAStatus(ctx context.Context, mod *kmmv1beta1.Module) error {
	emptyStatus := kmmv1beta1.DaemonSetStatus{}
	if mod.Status.DRA == emptyStatus {
		return nil
	}

	unmodifiedMod := mod.DeepCopy()

	mod.Status.DRA = kmmv1beta1.DaemonSetStatus{}

	return drh.client.Status().Patch(ctx, mod, client.MergeFrom(unmodifiedMod))
}

func (drh *draReconcilerHelper) getModuleDeviceClasses(ctx context.Context, name, namespace string) ([]resourcev1.DeviceClass, error) {
	dcList := resourcev1.DeviceClassList{}
	opts := []client.ListOption{
		client.MatchingLabels(map[string]string{
			constants.ModuleNameLabel:      name,
			constants.ModuleNamespaceLabel: namespace,
		}),
	}
	if err := drh.client.List(ctx, &dcList, opts...); err != nil {
		return nil, fmt.Errorf("could not list DeviceClasses: %v", err)
	}

	return dcList.Items, nil
}

// handleDeviceClasses reconciles cluster-scoped DeviceClass resources to match the desired state
// declared in mod.Spec.DRA.DeviceClasses. It performs declarative convergence:
//   - DeviceClasses present in the spec but missing from the cluster are created.
//   - DeviceClasses present in both are patched to reflect the current spec (drift correction).
//   - DeviceClasses present in the cluster but absent from the spec are deleted (stale cleanup).
func (drh *draReconcilerHelper) handleDeviceClasses(ctx context.Context, mod *kmmv1beta1.Module, existingDCs []resourcev1.DeviceClass) error {
	if mod.Spec.DRA == nil {
		return nil
	}

	logger := log.FromContext(ctx)

	existingByName := make(map[string]resourcev1.DeviceClass, len(existingDCs))
	for _, dc := range existingDCs {
		existingByName[dc.Name] = dc
	}

	var errs []error

	// Create missing or patch existing DeviceClasses to match the desired spec.
	for _, desired := range mod.Spec.DRA.DeviceClasses {
		dc := &resourcev1.DeviceClass{
			ObjectMeta: metav1.ObjectMeta{Name: desired.Name},
		}
		opRes, err := controllerutil.CreateOrPatch(ctx, drh.client, dc, func() error {
			if dc.Labels == nil {
				dc.Labels = make(map[string]string)
			}
			dc.Labels[constants.ModuleNameLabel] = mod.Name
			dc.Labels[constants.ModuleNamespaceLabel] = mod.Namespace
			dc.Spec.Selectors = desired.Selectors
			dc.Spec.Config = desired.Config
			return nil
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to create or patch DeviceClass %s: %v", desired.Name, err))
		} else {
			logger.Info("Reconciled DeviceClass", "name", desired.Name, "result", opRes)
		}
		delete(existingByName, desired.Name)
	}

	// Delete DeviceClasses that exist in the cluster but are no longer in the desired spec.
	for name, dc := range existingByName {
		if deleteErr := drh.client.Delete(ctx, &dc); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
			errs = append(errs, fmt.Errorf("failed to delete DeviceClass %s: %v", name, deleteErr))
		} else {
			logger.Info("Deleted extra DeviceClass", "name", name)
		}
	}

	return errors.Join(errs...)
}

type draDaemonSetCreator interface {
	setDRAAsDesired(ctx context.Context, ds *appsv1.DaemonSet, mod *kmmv1beta1.Module) error
}

type draDaemonSetCreatorImpl struct {
	scheme *runtime.Scheme
}

func newDRADaemonSetCreator(scheme *runtime.Scheme) draDaemonSetCreator {
	return &draDaemonSetCreatorImpl{
		scheme: scheme,
	}
}

func (dsci *draDaemonSetCreatorImpl) setDRAAsDesired(
	ctx context.Context,
	ds *appsv1.DaemonSet,
	mod *kmmv1beta1.Module,
) error {
	if ds == nil {
		return errors.New("ds cannot be nil")
	}

	if mod.Spec.DRA == nil {
		return errors.New("DRA spec in module should not be nil")
	}

	hostPathDirOrCreate := v1.HostPathDirectoryOrCreate
	hostPathDir := v1.HostPathDirectory

	pluginsVolume := v1.Volume{
		Name: kubeletPluginsVolumeName,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: kubeletPluginsPath,
				Type: &hostPathDirOrCreate,
			},
		},
	}

	registryVolume := v1.Volume{
		Name: kubeletPluginsRegistryVolumeName,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: kubeletPluginsRegistryPath,
				Type: &hostPathDir,
			},
		},
	}

	cdiVolume := v1.Volume{
		Name: cdiVolumeName,
		VolumeSource: v1.VolumeSource{
			HostPath: &v1.HostPathVolumeSource{
				Path: cdiPath,
				Type: &hostPathDirOrCreate,
			},
		},
	}

	containerVolumeMounts := []v1.VolumeMount{
		{Name: kubeletPluginsVolumeName, MountPath: kubeletPluginsPath},
		{Name: kubeletPluginsRegistryVolumeName, MountPath: kubeletPluginsRegistryPath},
		{Name: cdiVolumeName, MountPath: cdiPath},
	}

	presetEnv := []v1.EnvVar{
		{
			Name: "NODE_NAME",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{FieldPath: "spec.nodeName"},
			},
		},
		{
			Name: "POD_UID",
			ValueFrom: &v1.EnvVarSource{
				FieldRef: &v1.ObjectFieldSelector{FieldPath: "metadata.uid"},
			},
		},
		{
			Name:  "CDI_ROOT",
			Value: cdiPath,
		},
		{
			Name:  "KUBELET_REGISTRAR_DIRECTORY_PATH",
			Value: kubeletPluginsRegistryPath,
		},
		{
			Name:  "KUBELET_PLUGINS_DIRECTORY_PATH",
			Value: kubeletPluginsPath,
		},
		{
			Name:  "HEALTHCHECK_PORT",
			Value: fmt.Sprintf("%d", draHealthcheckPort),
		},
	}

	draLivenessProbe := &v1.Probe{
		ProbeHandler: v1.ProbeHandler{
			GRPC: &v1.GRPCAction{
				Port:    draHealthcheckPort,
				Service: ptr.To("liveness"),
			},
		},
		InitialDelaySeconds: 30,
		PeriodSeconds:       10,
		TimeoutSeconds:      5,
		FailureThreshold:    3,
	}

	standardLabels := map[string]string{
		constants.ModuleNameLabel: mod.Name,
		constants.DaemonSetRole:   constants.DRARoleLabelValue,
	}

	nodeSelector := map[string]string{
		utils.GetKernelModuleReadyNodeLabel(mod.Namespace, mod.Name): "",
	}

	if mod.Spec.ModuleLoader != nil && mod.Spec.ModuleLoader.Container.Version != "" {
		versionLabel := utils.GetSchedulePluginVersionLabelName(mod.Namespace, mod.Name)
		standardLabels[versionLabel] = mod.Spec.ModuleLoader.Container.Version
		nodeSelector[versionLabel] = mod.Spec.ModuleLoader.Container.Version
	}

	ds.SetLabels(
		overrideLabels(ds.GetLabels(), standardLabels),
	)

	ds.Spec = appsv1.DaemonSetSpec{
		Selector: &metav1.LabelSelector{MatchLabels: standardLabels},
		Template: v1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels:     standardLabels,
				Finalizers: []string{constants.NodeLabelerFinalizer},
			},
			Spec: v1.PodSpec{
				InitContainers:               generatePodContainerSpec(mod.Spec.DRA.InitContainer, "dra-init", nil, nil, nil),
				Containers:                   generatePodContainerSpec(&mod.Spec.DRA.Container, "dra", containerVolumeMounts, presetEnv, draLivenessProbe),
				PriorityClassName:            "system-node-critical",
				HostNetwork:                  true,
				ImagePullSecrets:             getPodPullSecrets(mod.Spec.ImageRepoSecret),
				NodeSelector:                 nodeSelector,
				ServiceAccountName:           mod.Spec.DRA.ServiceAccountName,
				Volumes:                      append([]v1.Volume{pluginsVolume, registryVolume, cdiVolume}, mod.Spec.DRA.Volumes...),
				Tolerations:                  mod.Spec.Tolerations,
				AutomountServiceAccountToken: mod.Spec.DRA.AutomountServiceAccountToken,
			},
		},
	}

	return controllerutil.SetControllerReference(mod, ds, dsci.scheme)
}
