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

	ootov1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	"github.com/qbarrand/oot-operator/controllers/module"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ModuleReconciler reconciles a Module object
type ModuleReconciler struct {
	client.Client

	namespace string
	dc        DaemonSetCreator
	km        module.KernelMapper
	su        module.ConditionsUpdater
}

func NewModuleReconciler(
	client client.Client,
	namespace string,
	dg DaemonSetCreator,
	km module.KernelMapper,
	su module.ConditionsUpdater,
) *ModuleReconciler {
	return &ModuleReconciler{
		Client:    client,
		dc:        dg,
		km:        km,
		namespace: namespace,
		su:        su,
	}
}

//+kubebuilder:rbac:groups=ooto.sigs.k8s.io,resources=modules,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ooto.sigs.k8s.io,resources=modules/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ooto.sigs.k8s.io,resources=modules/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=create;get;list;patch;watch
//+kubebuilder:rbac:groups="core",resources=nodes,verbs=list;watch

// Reconcile lists all nodes and looks for kernels that match its mappings.
// For each mapping that matches at least one node in the cluster, it creates a DaemonSet running the container image
// on the nodes with a compatible kernel.
func (r *ModuleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	mod := ootov1beta1.Module{}

	if err := r.Client.Get(ctx, req.NamespacedName, &mod); err != nil {
		logger.Error(err, "Could not get module")
		return ctrl.Result{}, err
	}

	logger.V(1).Info("Listing nodes", "selector", mod.Spec.Selector)

	nodes := v1.NodeList{}

	opt := client.MatchingLabels(mod.Spec.Selector)

	if err := r.Client.List(ctx, &nodes, opt); err != nil {
		logger.Error(err, "Could not list nodes; retrying")
		return ctrl.Result{}, fmt.Errorf("could not list nodes: %v", err)
	}

	if len(nodes.Items) == 0 {
		logger.Info("No nodes matching the selector; skipping module")
		return ctrl.Result{}, nil
	}

	mappings := make(map[string]string)

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

		containerImage, err := r.km.FindImageForKernel(mod.Spec.KernelMappings, kernelVersion)
		if err != nil {
			nodeLogger.Info("no suitable container image found; skipping node")
			continue
		}

		nodeLogger.V(1).Info("Found a valid mapping", "image", containerImage)
		mappings[kernelVersion] = containerImage
	}

	dsByKernelVersion, err := r.dc.ModuleDaemonSetsByKernelVersion(ctx, mod)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("could get DaemonSets for module %s: %v", mod.Name, err)
	}

	//// TODO qbarrand: find a better place for this
	//if err := r.su.SetAsReady(ctx, &mod, "TODO", "TODO"); err != nil {
	//	return ctrl.Result{}, fmt.Errorf("could not set the initial conditions: %v", err)
	//}

	for kernelVersion, containerImage := range mappings {
		logger.WithValues("kernel version", kernelVersion, "image", containerImage)

		ds := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: r.namespace},
		}

		if existingDS := dsByKernelVersion[kernelVersion]; existingDS != nil {
			ds = existingDS
		} else {
			ds.GenerateName = mod.Name + "-"
		}

		var res controllerutil.OperationResult

		res, err = controllerutil.CreateOrPatch(ctx, r.Client, ds, func() error {
			return r.dc.SetAsDesired(ds, containerImage, mod, kernelVersion)
		})

		if err != nil {
			return ctrl.Result{}, fmt.Errorf("could not create or patch DaemonSet: %v", err)
		}

		logger.Info("Reconciled DaemonSet", "name", ds.Name, "result", res)
	}

	// TODO we need to garbage collect DaemonSets here.
	// If we removed a kernel mapping from the Module, the corresponding DaemonSet is currently not deleted.

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ModuleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ootov1beta1.Module{}).
		Owns(&appsv1.DaemonSet{}).
		WithEventFilter(SkipDeletions()).
		Complete(r)
}
