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

package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"

	v1beta12 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/cmd"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/kubernetes-sigs/kernel-module-management/controllers"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build/job"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/daemonset"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/metrics"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/preflight"
	"github.com/kubernetes-sigs/kernel-module-management/internal/rbac"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	signjob "github.com/kubernetes-sigs/kernel-module-management/internal/sign/job"
	"github.com/kubernetes-sigs/kernel-module-management/internal/statusupdater"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	//+kubebuilder:scaffold:imports
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1beta12.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	logger := klogr.New().WithName("kmm")

	ctrl.SetLogger(logger)

	setupLogger := logger.WithName("setup")

	var configFile string

	flag.StringVar(&configFile, "config", "", "The path to the configuration file.")

	klog.InitFlags(flag.CommandLine)

	flag.Parse()

	commit, err := cmd.GitCommit()
	if err != nil {
		setupLogger.Error(err, "Could not get the git commit; using <undefined>")
		commit = "<undefined>"
	}

	managed, err := GetBoolEnv("KMM_MANAGED")
	if err != nil {
		setupLogger.Error(err, "could not determine if we are running as managed; disabling")
		managed = false
	}

	setupLogger.Info("Creating manager", "git commit", commit)

	options := ctrl.Options{Scheme: scheme}

	options, err = options.AndFrom(ctrl.ConfigFile().AtPath(configFile))
	if err != nil {
		cmd.FatalError(setupLogger, err, "unable to load the config file")
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		cmd.FatalError(setupLogger, err, "unable to create manager")
	}

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
		jobHelperAPI,
		registryAPI,
	)

	daemonAPI := daemonset.NewCreator(client, constants.KernelLabel, scheme)
	kernelAPI := module.NewKernelMapper()

	mc := controllers.NewModuleReconciler(
		client,
		buildAPI,
		signAPI,
		rbac.NewCreator(client, scheme),
		daemonAPI,
		kernelAPI,
		metricsAPI,
		filterAPI,
		statusupdater.NewModuleStatusUpdater(client, metricsAPI),
	)

	if err = mc.SetupWithManager(mgr, constants.KernelLabel); err != nil {
		cmd.FatalError(setupLogger, err, "unable to create controller", "name", controllers.ModuleReconcilerName)
	}

	nodeKernelReconciler := controllers.NewNodeKernelReconciler(client, constants.KernelLabel, filterAPI)

	if err = nodeKernelReconciler.SetupWithManager(mgr); err != nil {
		cmd.FatalError(setupLogger, err, "unable to create controller", "name", controllers.NodeKernelReconcilerName)
	}

	if err = controllers.NewPodNodeModuleReconciler(client, daemonAPI).SetupWithManager(mgr); err != nil {
		cmd.FatalError(setupLogger, err, "unable to create controller", "name", controllers.PodNodeModuleReconcilerName)
	}

	preflightStatusUpdaterAPI := statusupdater.NewPreflightStatusUpdater(client)
	preflightAPI := preflight.NewPreflightAPI(client, buildAPI, signAPI, registryAPI, preflightStatusUpdaterAPI, kernelAPI)

	if err = controllers.NewPreflightValidationReconciler(client, filterAPI, preflightStatusUpdaterAPI, preflightAPI).SetupWithManager(mgr); err != nil {
		cmd.FatalError(setupLogger, err, "unable to create controller", "name", controllers.PreflightValidationReconcilerName)
	}

	if managed {
		setupLogger.Info("Starting as managed")

		if err = clusterv1.Install(scheme); err != nil {
			cmd.FatalError(setupLogger, err, "could not add the Cluster API to the scheme")
		}

		if err = controllers.NewNodeKernelClusterClaimReconciler(client).SetupWithManager(mgr); err != nil {
			cmd.FatalError(setupLogger, err, "unable to create controller", "name", controllers.NodeKernelClusterClaimReconcilerName)
		}
	}

	//+kubebuilder:scaffold:builder

	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		cmd.FatalError(setupLogger, err, "unable to set up health check")
	}
	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		cmd.FatalError(setupLogger, err, "unable to set up ready check")
	}

	setupLogger.Info("starting manager")
	if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		cmd.FatalError(setupLogger, err, "problem running manager")
	}
}

func GetBoolEnv(s string) (bool, error) {
	envValue := os.Getenv(s)

	if envValue == "" {
		return false, nil
	}

	managed, err := strconv.ParseBool(envValue)
	if err != nil {
		return false, fmt.Errorf("%q: invalid value for %s", envValue, s)
	}

	return managed, nil
}
