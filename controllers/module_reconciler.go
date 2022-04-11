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

	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/controllers/build"
	"github.com/qbarrand/oot-operator/controllers/module"
	"github.com/qbarrand/oot-operator/controllers/predicates"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ModuleReconciler reconciles a Module object
type ModuleReconciler struct {
	client.Client

	namespace string
	bm        build.Manager
	dc        DaemonSetCreator
	km        module.KernelMapper
	su        module.ConditionsUpdater
}

func NewModuleReconciler(
	client client.Client,
	namespace string,
	bm build.Manager,
	dg DaemonSetCreator,
	km module.KernelMapper,
	su module.ConditionsUpdater,
) *ModuleReconciler {
	return &ModuleReconciler{
		Client:    client,
		bm:        bm,
		dc:        dg,
		km:        km,
		namespace: namespace,
		su:        su,
	}
}

//+kubebuilder:rbac:groups=ooto.sigs.k8s.io,resources=modules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ooto.sigs.k8s.io,resources=modules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ooto.sigs.k8s.io,resources=modules/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=create;delete;get;list;patch;watch
//+kubebuilder:rbac:groups="core",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups="batch",resources=jobs,verbs=create;list;watch

// Reconcile lists all nodes and looks for kernels that match its mappings.
// For each mapping that matches at least one node in the cluster, it creates a DaemonSet running the container image
// on the nodes with a compatible kernel.
func (r *ModuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	res := ctrl.Result{}

	logger := log.FromContext(ctx)

	mod := ootov1alpha1.Module{}

	if err := r.Client.Get(ctx, req.NamespacedName, &mod); err != nil {
		logger.Error(err, "Could not get module")
		return res, err
	}

	logger.V(1).Info("Listing nodes", "selector", mod.Spec.Selector)

	nodes := v1.NodeList{}

	opt := client.MatchingLabels(mod.Spec.Selector)

	if err := r.Client.List(ctx, &nodes, opt); err != nil {
		logger.Error(err, "Could not list nodes; retrying")
		return res, fmt.Errorf("could not list nodes: %v", err)
	}

	mappings := make(map[string]*ootov1alpha1.KernelMapping)

	for _, node := range nodes.Items {
		kernelVersion := node.Status.NodeInfo.KernelVersion

		nodeLogger := logger.WithValues(
			"node", node.Name,
			"kernel version", kernelVersion,
		)

		if image, ok := mappings[kernelVersion]; ok {
			nodeLogger.V(1).Info("Using cached image", "image", image)
			continue
		}

		m, err := r.km.FindMappingForKernel(mod.Spec.KernelMappings, kernelVersion)
		if err != nil {
			nodeLogger.Info("no suitable container image found; skipping node")
			continue
		}

		nodeLogger.V(1).Info("Found a valid mapping",
			"image", m.ContainerImage,
			"build", m.Build != nil,
		)

		mappings[kernelVersion] = m
	}

	dsByKernelVersion, err := r.dc.ModuleDaemonSetsByKernelVersion(ctx, mod)
	if err != nil {
		return res, fmt.Errorf("could get DaemonSets for module %s: %v", mod.Name, err)
	}

	//// TODO qbarrand: find a better place for this
	//if err := r.su.SetAsReady(ctx, &mod, "TODO", "TODO"); err != nil {
	//	return res, fmt.Errorf("could not set the initial conditions: %v", err)
	//}

	for kernelVersion, m := range mappings {
		logger.WithValues("kernel version", kernelVersion, "image", m)

		if m.Build != nil {
			buildCtx := log.IntoContext(ctx, logger)

			buildRes, err := r.bm.Sync(buildCtx, mod, *m, kernelVersion)
			if err != nil {
				return res, fmt.Errorf("could not synchronize the build: %v", err)
			}

			if buildRes.Requeue {
				logger.Info("Build requires a requeue; skipping this mapping for now", "status", buildRes.Status)
				res.Requeue = true
				continue
			}
		}

		ds := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: r.namespace},
		}

		if existingDS := dsByKernelVersion[kernelVersion]; existingDS != nil {
			ds = existingDS
		} else {
			ds.GenerateName = mod.Name + "-"
		}

		var opRes controllerutil.OperationResult

		opRes, err = controllerutil.CreateOrPatch(ctx, r.Client, ds, func() error {
			return r.dc.SetAsDesired(ds, m.ContainerImage, mod, kernelVersion)
		})

		if err != nil {
			return res, fmt.Errorf("could not create or patch DaemonSet: %v", err)
		}

		logger.Info("Reconciled DaemonSet", "name", ds.Name, "result", opRes)
	}

	logger.Info("Garbage-collecting DaemonSets")

	// Garbage collect old DaemonSets for which there are no nodes.
	validKernels := sets.StringKeySet(mappings)

	deleted, err := r.dc.GarbageCollect(ctx, dsByKernelVersion, validKernels)
	if err != nil {
		return res, fmt.Errorf("could not garbage collect DaemonSets: %v", err)
	}

	logger.Info("Garbage-collected DaemonSets", "names", deleted)

	return res, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModuleReconciler) SetupWithManager(mgr ctrl.Manager, kernelLabel string) error {
	nmm := NewNodeModuleMapper(
		r.Client,
		mgr.GetLogger().WithName("NodeModuleMapper"),
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&ootov1alpha1.Module{}).
		Owns(&appsv1.DaemonSet{}).
		Owns(&batchv1.Job{}).
		Watches(
			&source.Kind{Type: &v1.Node{}},
			handler.EnqueueRequestsFromMapFunc(nmm.FindModulesForNode),
			builder.WithPredicates(
				ModuleReconcilerNodePredicate(kernelLabel),
			),
		).
		Complete(r)
}

func ModuleReconcilerNodePredicate(kernelLabel string) predicate.Predicate {
	return predicate.And(
		predicates.SkipDeletions,
		predicates.HasLabel(kernelLabel),
		predicate.LabelChangedPredicate{},
	)
}
