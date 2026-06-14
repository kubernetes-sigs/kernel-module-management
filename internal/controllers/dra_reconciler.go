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
	"github.com/kubernetes-sigs/kernel-module-management/internal/node"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	DRAReconcilerName = "DRAReconciler"

	kubeletPluginsVolumeName         = "kubelet-plugins"
	kubeletPluginsPath               = "/var/lib/kubelet/plugins/"
	kubeletPluginsRegistryVolumeName = "kubelet-plugins-registry"
	kubeletPluginsRegistryPath       = "/var/lib/kubelet/plugins_registry/"
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
		return ctrl.Result{}, r.reconHelperAPI.deleteDRAResources(ctx, existingDRADS, existingDCs)
	}

	if mod.Spec.DRA == nil {
		if len(existingDRADS) > 0 || len(existingDCs) > 0 {
			if err = r.reconHelperAPI.deleteDRAResources(ctx, existingDRADS, existingDCs); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, r.reconHelperAPI.clearDRAStatus(ctx, mod)
	}

	err = r.reconHelperAPI.handleDRA(ctx, mod, existingDRADS)
	if err != nil {
		return res, fmt.Errorf("could not handle DRA: %v", err)
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
	deleteDRAResources(ctx context.Context, daemonSets []appsv1.DaemonSet, deviceClasses []resourcev1.DeviceClass) error
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

	var ds *appsv1.DaemonSet
	if len(existingDRADS) > 0 {
		ds = &existingDRADS[0]
	} else {
		logger.Info("creating new DRA DaemonSet")
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

// deleteDRAResources deletes all DRA-owned DaemonSets and DeviceClasses.
// It attempts every deletion and returns a joined error so the controller requeues on partial failure.
func (drh *draReconcilerHelper) deleteDRAResources(ctx context.Context, daemonSets []appsv1.DaemonSet, deviceClasses []resourcev1.DeviceClass) error {
	var errs []error
	for _, ds := range daemonSets {
		if err := drh.client.Delete(ctx, &ds); err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("failed to delete DRA DaemonSet %s/%s: %v", ds.Namespace, ds.Name, err))
		}
	}
	for _, dc := range deviceClasses {
		if err := drh.client.Delete(ctx, &dc); err != nil && !apierrors.IsNotFound(err) {
			errs = append(errs, fmt.Errorf("failed to delete DeviceClass %s: %v", dc.Name, err))
		}
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

	desiredByName := make(map[string]kmmv1beta1.DeviceClassSpec, len(mod.Spec.DRA.DeviceClasses))
	for _, dc := range mod.Spec.DRA.DeviceClasses {
		desiredByName[dc.Name] = dc
	}

	var errs []error

	// Create missing or patch existing DeviceClasses to match the desired spec.
	for _, desired := range mod.Spec.DRA.DeviceClasses {
		existingDC, exists := existingByName[desired.Name]
		if err := drh.createOrPatchDeviceClass(ctx, mod, desired, existingDC, exists); err != nil {
			errs = append(errs, err)
		}
	}

	// Delete DeviceClasses that exist in the cluster but are no longer in the desired spec.
	for name, dc := range existingByName {
		if _, desired := desiredByName[name]; !desired {
			if deleteErr := drh.client.Delete(ctx, &dc); deleteErr != nil && !apierrors.IsNotFound(deleteErr) {
				errs = append(errs, fmt.Errorf("failed to delete DeviceClass %s: %v", name, deleteErr))
			} else {
				logger.Info("Deleted extra DeviceClass", "name", name)
			}
		}
	}

	return errors.Join(errs...)
}

// createOrPatchDeviceClass ensures a single DeviceClass matches the desired spec.
// If the DeviceClass does not exist, it creates one with ownership labels.
// If it already exists, it patches selectors and config to correct any drift.
func (drh *draReconcilerHelper) createOrPatchDeviceClass(
	ctx context.Context,
	mod *kmmv1beta1.Module,
	desired kmmv1beta1.DeviceClassSpec,
	existingDC resourcev1.DeviceClass,
	exists bool,
) error {
	logger := log.FromContext(ctx)

	if !exists {
		dc := resourcev1.DeviceClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: desired.Name,
				Labels: map[string]string{
					constants.ModuleNameLabel:      mod.Name,
					constants.ModuleNamespaceLabel: mod.Namespace,
				},
			},
			Spec: resourcev1.DeviceClassSpec{
				Selectors: desired.Selectors,
				Config:    desired.Config,
			},
		}
		if err := drh.client.Create(ctx, &dc); err != nil {
			return fmt.Errorf("failed to create DeviceClass %s: %v", desired.Name, err)
		}
		logger.Info("Created DeviceClass", "name", desired.Name)
		return nil
	}

	patch := client.MergeFrom(existingDC.DeepCopy())
	existingDC.Spec.Selectors = desired.Selectors
	existingDC.Spec.Config = desired.Config
	if err := drh.client.Patch(ctx, &existingDC, patch); err != nil {
		return fmt.Errorf("failed to patch DeviceClass %s: %v", desired.Name, err)
	}
	logger.Info("Patched DeviceClass", "name", desired.Name)
	return nil
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

	containerVolumeMounts := []v1.VolumeMount{
		{Name: kubeletPluginsVolumeName, MountPath: kubeletPluginsPath},
		{Name: kubeletPluginsRegistryVolumeName, MountPath: kubeletPluginsRegistryPath},
	}

	standardLabels := map[string]string{
		constants.ModuleNameLabel: mod.Name,
		constants.DaemonSetRole:   constants.DRARoleLabelValue,
	}

	nodeSelector := map[string]string{
		utils.GetKernelModuleReadyNodeLabel(mod.Namespace, mod.Name): "",
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
				InitContainers:               generatePodContainerSpec(mod.Spec.DRA.InitContainer, "dra-init", nil),
				Containers:                   generatePodContainerSpec(&mod.Spec.DRA.Container, "dra", containerVolumeMounts),
				PriorityClassName:            "system-node-critical",
				ImagePullSecrets:             getPodPullSecrets(mod.Spec.ImageRepoSecret),
				NodeSelector:                 nodeSelector,
				ServiceAccountName:           mod.Spec.DRA.ServiceAccountName,
				Volumes:                      append([]v1.Volume{pluginsVolume, registryVolume}, mod.Spec.DRA.Volumes...),
				Tolerations:                  mod.Spec.Tolerations,
				AutomountServiceAccountToken: mod.Spec.DRA.AutomountServiceAccountToken,
			},
		},
	}

	return controllerutil.SetControllerReference(mod, ds, dsci.scheme)
}
