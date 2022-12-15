package main

import (
	"fmt"
	"os"

	"github.com/kubernetes-sigs/kernel-module-management/controllers"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build/job"
	"github.com/kubernetes-sigs/kernel-module-management/internal/cluster"
	"github.com/kubernetes-sigs/kernel-module-management/internal/daemonset"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/manifestwork"
	"github.com/kubernetes-sigs/kernel-module-management/internal/metrics"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/preflight"
	"github.com/kubernetes-sigs/kernel-module-management/internal/rbac"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	signjob "github.com/kubernetes-sigs/kernel-module-management/internal/sign/job"
	"github.com/kubernetes-sigs/kernel-module-management/internal/statusupdater"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

func setupError(reconcilerName string, err error) error {
	return fmt.Errorf("could not add the %s reconciler: %v", reconcilerName, err)
}

func addStandardReconcilers(mgr ctrl.Manager) error {
	client := mgr.GetClient()

	filterAPI := filter.New(client, mgr.GetLogger())

	metricsAPI := metrics.New()
	metricsAPI.Register()

	registryAPI := registry.NewRegistry()
	jobHelperAPI := utils.NewJobHelper(client)

	buildAPI := job.NewBuildManager(
		client,
		job.NewMaker(client, build.NewHelper(), jobHelperAPI, scheme),
		jobHelperAPI,
		registryAPI,
	)

	signAPI := signjob.NewSignJobManager(
		client,
		signjob.NewSigner(client, scheme, sign.NewSignerHelper(), jobHelperAPI),
		utils.NewJobHelper(client),
		registryAPI,
	)

	daemonAPI := daemonset.NewCreator(client, kernelLabel, scheme)
	kernelAPI := module.NewKernelMapper()
	moduleStatusUpdaterAPI := statusupdater.NewModuleStatusUpdater(client, daemonAPI, metricsAPI)
	preflightStatusUpdaterAPI := statusupdater.NewPreflightStatusUpdater(client)
	preflightAPI := preflight.NewPreflightAPI(client, buildAPI, signAPI, registryAPI, preflightStatusUpdaterAPI, kernelAPI)

	mc := controllers.NewModuleReconciler(
		client,
		buildAPI,
		signAPI,
		rbac.NewCreator(client, scheme),
		daemonAPI,
		kernelAPI,
		metricsAPI,
		filterAPI,
		moduleStatusUpdaterAPI,
	)

	if err := mc.SetupWithManager(mgr, kernelLabel); err != nil {
		return setupError(controllers.ModuleReconcilerName, err)
	}

	if err := controllers.NewNodeKernelReconciler(client, kernelLabel, filterAPI).SetupWithManager(mgr); err != nil {
		return setupError(controllers.NodeKernelReconcilerName, err)
	}

	if err := controllers.NewPodNodeModuleReconciler(client, daemonAPI).SetupWithManager(mgr); err != nil {
		return setupError(controllers.PodNodeModuleReconcilerName, err)
	}

	if err := controllers.NewPreflightValidationReconciler(client, filterAPI, preflightStatusUpdaterAPI, preflightAPI).SetupWithManager(mgr); err != nil {
		return setupError(controllers.PreflightValidationReconcilerName, err)
	}

	return nil
}

func addHubReconcilers(mgr ctrl.Manager) error {
	if err := workv1.Install(scheme); err != nil {
		return fmt.Errorf("could not add the Work API to the scheme: %v", err)
	}

	const operatorNamespaceEnvVar = "OPERATOR_NAMESPACE"

	operatorNamespace := os.Getenv(operatorNamespaceEnvVar)
	if operatorNamespace == "" {
		return fmt.Errorf("could not determine the current namespace: %q is not defined or empty", operatorNamespaceEnvVar)
	}

	client := mgr.GetClient()

	jobHelperAPI := utils.NewJobHelper(client)
	registryAPI := registry.NewRegistry()

	buildAPI := job.NewBuildManager(
		client,
		job.NewMaker(
			client,
			build.NewHelper(),
			jobHelperAPI,
			scheme,
		),
		jobHelperAPI,
		registryAPI)

	signAPI := signjob.NewSignJobManager(
		client,
		signjob.NewSigner(client, scheme, sign.NewSignerHelper(), jobHelperAPI),
		utils.NewJobHelper(client),
		registryAPI,
	)

	mcmr := controllers.NewManagedClusterModuleReconciler(
		client,
		manifestwork.NewCreator(client, scheme),
		cluster.NewClusterAPI(
			client,
			module.NewKernelMapper(),
			buildAPI,
			signAPI,
			operatorNamespace,
		),
		filter.New(client, mgr.GetLogger()),
	)

	if err := mcmr.SetupWithManager(mgr); err != nil {
		return setupError(controllers.ManagedClusterModuleReconcilerName, err)
	}

	return nil
}

func addSpokeReconcilers(mgr ctrl.Manager) error {
	if err := clusterv1.Install(scheme); err != nil {
		return fmt.Errorf("could not add the Cluster API to the scheme: %v", err)
	}

	client := mgr.GetClient()

	if err := controllers.NewNodeKernelClusterClaimReconciler(client).SetupWithManager(mgr); err != nil {
		return setupError(controllers.NodeKernelClusterClaimReconcilerName, err)
	}

	metricsAPI := metrics.New()
	metricsAPI.Register()

	daemonAPI := daemonset.NewCreator(client, kernelLabel, scheme)

	mc := controllers.NewModuleReconciler(
		client,
		nil,
		nil,
		rbac.NewCreator(client, scheme),
		daemonAPI,
		module.NewKernelMapper(),
		metricsAPI,
		filter.New(client, mgr.GetLogger()),
		statusupdater.NewModuleStatusUpdater(client, daemonAPI, metricsAPI),
	)

	if err := mc.SetupWithManager(mgr, kernelLabel); err != nil {
		return setupError(controllers.ModuleReconcilerName, err)
	}

	return nil
}
