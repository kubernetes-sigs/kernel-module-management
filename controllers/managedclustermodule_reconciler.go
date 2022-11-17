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
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/manifestwork"
)

// ManagedClusterModuleReconciler reconciles a ManagedClusterModule object
type ManagedClusterModuleReconciler struct {
	client client.Client

	manifestAPI manifestwork.ManifestWorkCreator
	filter      *filter.Filter
}

//+kubebuilder:rbac:groups=kmm.sigs.k8s.io,resources=managedclustermodules,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=kmm.sigs.k8s.io,resources=managedclustermodules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kmm.sigs.k8s.io,resources=managedclustermodules/finalizers,verbs=update
//+kubebuilder:rbac:groups=work.open-cluster-management.io,resources=manifestworks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cluster.open-cluster-management.io,resources=managedclusters,verbs=get;list;watch

func NewManagedClusterModuleReconciler(
	client client.Client,
	manifestAPI manifestwork.ManifestWorkCreator,
	filter *filter.Filter) *ManagedClusterModuleReconciler {
	return &ManagedClusterModuleReconciler{
		client:      client,
		manifestAPI: manifestAPI,
		filter:      filter,
	}
}

func (r *ManagedClusterModuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	res := ctrl.Result{}

	logger := log.FromContext(ctx)

	mcm, err := r.getRequestedManagedClusterModule(ctx, req.NamespacedName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.Info("ManagedClusterModule deleted")
			return res, nil
		}

		return res, fmt.Errorf("failed to get the requested CR: %v", err)
	}

	logger.Info("Requested KMMO ManagedClusterModule")

	clusters, err := r.getSelectedManagedClusters(ctx, mcm)
	if err != nil {
		return res, fmt.Errorf("failed to get selected clusters: %v", err)
	}

	for _, cluster := range clusters.Items {
		mw := &workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mcm.Name,
				Namespace: cluster.Name,
			},
		}

		opRes, err := controllerutil.CreateOrPatch(ctx, r.client, mw, func() error {
			return r.manifestAPI.SetManifestWorkAsDesired(ctx, mw, *mcm)
		})

		if err != nil {
			return res, fmt.Errorf("failed to create/patch ManifestWork for managed cluster %s: %v", cluster.Name, err)
		}

		logger.Info("Reconciled ManifestWork", "name", mw.Name, "namespace", mw.Namespace, "result", opRes)
	}

	if err := r.manifestAPI.GarbageCollect(ctx, *clusters, *mcm); err != nil {
		return res, fmt.Errorf("failed to garbage collect ManifestWorks with no matching cluster selector: %v", err)
	}

	return res, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedClusterModuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kmmv1beta1.ManagedClusterModule{}).
		Owns(&workv1.ManifestWork{}).
		Watches(
			&source.Kind{Type: &clusterv1.ManagedCluster{}},
			handler.EnqueueRequestsFromMapFunc(r.filter.FindManagedClusterModulesForCluster),
			builder.WithPredicates(predicate.LabelChangedPredicate{})).
		Named("managedclustermodule").
		Complete(r)
}

func (r *ManagedClusterModuleReconciler) getRequestedManagedClusterModule(
	ctx context.Context,
	namespacedName types.NamespacedName) (*kmmv1beta1.ManagedClusterModule, error) {

	mcm := kmmv1beta1.ManagedClusterModule{}
	if err := r.client.Get(ctx, namespacedName, &mcm); err != nil {
		return nil, fmt.Errorf("failed to get the ManagedClusterModule %s: %w", namespacedName, err)
	}
	return &mcm, nil
}

func (r *ManagedClusterModuleReconciler) getSelectedManagedClusters(
	ctx context.Context,
	mcm *kmmv1beta1.ManagedClusterModule) (*clusterv1.ManagedClusterList, error) {

	clusterList := &clusterv1.ManagedClusterList{}

	opts := []client.ListOption{
		client.MatchingLabelsSelector{
			Selector: labels.Set(mcm.Spec.Selector).AsSelector(),
		},
	}
	err := r.client.List(ctx, clusterList, opts...)

	return clusterList, err
}
