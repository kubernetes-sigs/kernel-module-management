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

package hub

import (
	"context"
	"errors"
	"fmt"
	"strings"

	hubv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/cluster"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/manifestwork"
	"github.com/kubernetes-sigs/kernel-module-management/internal/mic"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/statusupdater"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const ManagedClusterModuleReconcilerName = "ManagedClusterModule"

// ManagedClusterModuleReconciler reconciles a ManagedClusterModule object
type ManagedClusterModuleReconciler struct {
	client client.Client

	manifestAPI      manifestwork.ManifestWorkCreator
	clusterAPI       cluster.ClusterAPI
	statusupdaterAPI statusupdater.ManagedClusterModuleStatusUpdater

	filter      *filter.Filter
	micAPI      mic.MIC
	reconHelper managedClusterModuleReconcilerHelperAPI
}

//+kubebuilder:rbac:groups=hub.kmm.sigs.x-k8s.io,resources=managedclustermodules,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=hub.kmm.sigs.x-k8s.io,resources=managedclustermodules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=hub.kmm.sigs.x-k8s.io,resources=managedclustermodules/finalizers,verbs=update
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=moduleimagesconfigs,verbs=get;list;watch;patch;create;delete
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=moduleimagesconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modulebuildsignconfigs,verbs=get;list;watch;update;patch;create
//+kubebuilder:rbac:groups=kmm.sigs.x-k8s.io,resources=modulebuildsignconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=pods,verbs=create;delete;list;patch;watch
//+kubebuilder:rbac:groups="core",resources=secrets,verbs=get;list;watch
//+kubebuilder:rbac:groups="core",resources=configmaps,verbs=get;list;watch

func NewManagedClusterModuleReconciler(
	client client.Client,
	manifestAPI manifestwork.ManifestWorkCreator,
	clusterAPI cluster.ClusterAPI,
	statusupdaterAPI statusupdater.ManagedClusterModuleStatusUpdater,
	filter *filter.Filter,
	micAPI mic.MIC) *ManagedClusterModuleReconciler {

	reconHelper := newManagedClusterModuleReconcilerHelper(clusterAPI, micAPI)
	return &ManagedClusterModuleReconciler{
		client:           client,
		manifestAPI:      manifestAPI,
		clusterAPI:       clusterAPI,
		statusupdaterAPI: statusupdaterAPI,
		filter:           filter,
		micAPI:           micAPI,
		reconHelper:      reconHelper,
	}
}

func (r *ManagedClusterModuleReconciler) Reconcile(ctx context.Context, mcm *hubv1beta1.ManagedClusterModule) (ctrl.Result, error) {

	logger := log.FromContext(ctx)

	logger.Info("Starting ManagedClusterModule reconciliation")

	clusters, err := r.clusterAPI.SelectedManagedClusters(ctx, mcm)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get selected clusters: %v", err)
	}

	for _, cluster := range clusters.Items {

		logger := log.FromContext(ctx).WithValues("cluster", cluster.Name)
		clusterCtx := log.IntoContext(ctx, logger)

		kernelVersions, err := r.clusterAPI.KernelVersions(cluster)
		if err != nil {
			logger.Info(utils.WarnString(
				fmt.Sprintf("No kernel versions found for managed cluster; skipping MIC patch: %v", err),
			))
			continue
		}

		err = r.reconHelper.setMicAsDesired(ctx, mcm, cluster.Name, kernelVersions)
		if err != nil {
			logger.Info(utils.WarnString(fmt.Sprintf("Failed to set MIC as desired: %v", err)))
			continue
		}

		allImagesReady, err := r.reconHelper.areImagesReady(ctx, mcm.Name, cluster.Name)
		if err != nil {
			logger.Info(utils.WarnString(fmt.Sprintf("Failed to check if MIC is ready: %v", err)))
			continue
		}
		if !allImagesReady {
			logger.Info("not all images exist yet for the cluster; skipping ManifestWork reconciliation")
			continue
		}

		mw := &workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mcm.Name,
				Namespace: cluster.Name,
			},
		}

		opRes, err := controllerutil.CreateOrPatch(clusterCtx, r.client, mw, func() error {
			return r.manifestAPI.SetManifestWorkAsDesired(ctx, mw, *mcm, kernelVersions)
		})
		if err != nil {
			logger.Info(utils.WarnString(fmt.Sprintf("failed to create/patch ManifestWork for managed cluster: %v", err)))
			continue
		}

		logger.Info("Reconciled ManifestWork", "name", mw.Name, "namespace", mw.Namespace, "result", opRes)
	}

	if err := r.manifestAPI.GarbageCollect(ctx, *clusters, *mcm); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to garbage collect ManifestWorks with no matching cluster selector: %v", err)
	}

	ownedManifestWorkList, err := r.manifestAPI.GetOwnedManifestWorks(ctx, *mcm)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to fetch owned ManifestWorks of the ManagedClusterModule: %v", err)
	}

	if err := r.statusupdaterAPI.ManagedClusterModuleUpdateStatus(ctx, mcm, ownedManifestWorkList.Items); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update status of the ManagedClusterModule: %v", err)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedClusterModuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&hubv1beta1.ManagedClusterModule{}).
		Owns(&workv1.ManifestWork{}).
		Owns(&kmmv1beta1.ModuleImagesConfig{}, builder.WithPredicates(filter.ModuleReconcileMICPredicate())).
		Watches(
			&clusterv1.ManagedCluster{},
			handler.EnqueueRequestsFromMapFunc(r.filter.FindManagedClusterModulesForCluster),
			builder.WithPredicates(
				r.filter.ManagedClusterModuleReconcilerManagedClusterPredicate(),
			),
		).
		Named(ManagedClusterModuleReconcilerName).
		Complete(
			reconcile.AsReconciler[*hubv1beta1.ManagedClusterModule](mgr.GetClient(), r),
		)
}

//go:generate mockgen -source=managedclustermodule_reconciler.go -package=hub -destination=mock_managedclustermodule_reconciler.go managedClusterModuleReconcilerHelperAPI

type managedClusterModuleReconcilerHelperAPI interface {
	setMicAsDesired(ctx context.Context, mcm *hubv1beta1.ManagedClusterModule, clusterName string, kernelVersions []string) error
	areImagesReady(ctx context.Context, mcmName, clusterName string) (bool, error)
}

type managedClusterModuleReconcilerHelper struct {
	clusterAPI cluster.ClusterAPI
	micAPI     mic.MIC
}

func newManagedClusterModuleReconcilerHelper(clusterAPI cluster.ClusterAPI, micAPI mic.MIC) managedClusterModuleReconcilerHelperAPI {
	return &managedClusterModuleReconcilerHelper{
		clusterAPI: clusterAPI,
		micAPI:     micAPI,
	}
}

func (rh *managedClusterModuleReconcilerHelper) setMicAsDesired(ctx context.Context, mcm *hubv1beta1.ManagedClusterModule,
	clusterName string, kernelVersions []string) error {

	var images []kmmv1beta1.ModuleImageSpec
	for _, kver := range kernelVersions {

		kver = strings.TrimSuffix(kver, "+")
		mld, err := rh.clusterAPI.GetModuleLoaderDataForKernel(mcm, kver)
		if err != nil {
			if !errors.Is(err, module.ErrNoMatchingKernelMapping) {
				return fmt.Errorf("failed to get MLD for kernel %s: %v", kver, err)
			}
			// error getting kernelVersion or kernelVersion is not targeted by the managedClusterModule
			continue
		}
		mis := kmmv1beta1.ModuleImageSpec{
			Image:         mld.ContainerImage,
			KernelVersion: mld.KernelVersion,
			Build:         mld.Build,
			Sign:          mld.Sign,
			RegistryTLS:   mld.RegistryTLS,
		}
		images = append(images, mis)
	}

	micName := mcm.Name + "-" + clusterName
	micNamespace := rh.clusterAPI.GetDefaultArtifactsNamespace()
	if err := rh.micAPI.CreateOrPatch(ctx, micName, micNamespace, images, mcm.Spec.ModuleSpec.ImageRepoSecret,
		mcm.Spec.ModuleSpec.ModuleLoader.Container.ImagePullPolicy, true, mcm.Spec.ModuleSpec.ImageRebuildTriggerGeneration, mcm.Spec.ModuleSpec.Tolerations, mcm); err != nil {
		return fmt.Errorf("failed to createOrPatch MIC %s: %v", micName, err)
	}

	return nil
}

func (rh *managedClusterModuleReconcilerHelper) areImagesReady(ctx context.Context, mcmName, clusterName string) (bool, error) {

	micName := mcmName + "-" + clusterName
	micNamespace := rh.clusterAPI.GetDefaultArtifactsNamespace()

	micObj, err := rh.micAPI.Get(ctx, micName, micNamespace)
	if err != nil {
		return false, fmt.Errorf("failed to get MIC %s: %v", micName, err)
	}

	return rh.micAPI.DoAllImagesExist(micObj), nil
}
