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
	"errors"
	"flag"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/go-logr/logr"
	"github.com/kubernetes-sigs/kernel-module-management/controllers"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build/job"
	"github.com/kubernetes-sigs/kernel-module-management/internal/cluster"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/manifestwork"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"

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

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	//+kubebuilder:scaffold:imports
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(kmmv1beta1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func fatal(l logr.Logger, err error, msg string, fields ...interface{}) {
	l.Error(err, msg, fields...)
	os.Exit(1)
}

func main() {
	logger := klogr.New().WithName("kmm")

	ctrl.SetLogger(logger)

	setupLogger := logger.WithName("setup")

	var (
		configFile           string
		metricsAddr          string
		enableLeaderElection bool
		probeAddr            string
	)

	m, err := defaultMode()
	if err != nil {
		fatal(logger, err, "could not get the default mode")
	}

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Var(m, "mode", "The manager's mode: kmm, hub or spoke (default \"kmm\").")

	flag.StringVar(&configFile, "config", "", "The path to the configuration file.")

	klog.InitFlags(flag.CommandLine)

	flag.Parse()

	setupLogger = setupLogger.WithValues("mode", m.String())

	commit, err := gitCommit()
	if err != nil {
		setupLogger.Error(err, "Could not get the git commit; using <undefined>")
		commit = "<undefined>"
	}

	setupLogger.Info("Creating manager", "git commit", commit)

	options := ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "c5baf8af.sigs.k8s.io",
	}
	if configFile != "" {
		options, err = options.AndFrom(ctrl.ConfigFile().AtPath(configFile))
		if err != nil {
			fatal(setupLogger, err, "unable to load the config file")
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		fatal(setupLogger, err, "unable to start manager")
	}

	client := mgr.GetClient()

	filterAPI := filter.New(client, mgr.GetLogger())

	metricsAPI := metrics.New()
	metricsAPI.Register()

	registryAPI := registry.NewRegistry()
	jobHelperAPI := utils.NewJobHelper(client)

	var (
		buildAPI build.Manager
		signAPI  sign.SignManager
	)

	if m.builds {
		makerAPI := job.NewMaker(
			client,
			build.NewHelper(),
			jobHelperAPI,
			scheme,
		)

		buildAPI = job.NewBuildManager(client, makerAPI, jobHelperAPI, registryAPI)
	}

	if m.signs {
		signAPI = signjob.NewSignJobManager(
			client,
			signjob.NewSigner(client, scheme, sign.NewSignerHelper(), jobHelperAPI),
			utils.NewJobHelper(client),
			registryAPI,
		)
	}

	daemonAPI := daemonset.NewCreator(client, constants.KernelLabel, scheme)
	kernelAPI := module.NewKernelMapper()

	if m.startsManagedClusterModuleReconciler {
		setupLogger := setupLogger.WithValues("name", controllers.ManagedClusterModuleReconcilerName)
		setupLogger.Info("Adding controller")

		if err = workv1.Install(scheme); err != nil {
			fatal(setupLogger, err, "could not add the Work API to the scheme")
		}

		const operatorNamespaceEnvVar = "OPERATOR_NAMESPACE"

		operatorNamespace := os.Getenv(operatorNamespaceEnvVar)
		if operatorNamespace == "" {
			fatal(setupLogger, errors.New("empty value"), "Could not determine the current namespace", "name", operatorNamespace)
		}

		mcmr := controllers.NewManagedClusterModuleReconciler(
			client,
			manifestwork.NewCreator(client, scheme),
			cluster.NewClusterAPI(client, kernelAPI, buildAPI, signAPI, operatorNamespace),
			filterAPI,
		)

		if err = mcmr.SetupWithManager(mgr); err != nil {
			fatal(setupLogger, err, "unable to create controller")
		}
	}

	if m.startsModuleReconciler {
		setupLogger := setupLogger.WithValues("name", controllers.ModuleReconcilerName)
		setupLogger.Info("Adding controller")

		mc := controllers.NewModuleReconciler(
			client,
			buildAPI,
			signAPI,
			rbac.NewCreator(client, scheme),
			daemonAPI,
			kernelAPI,
			metricsAPI,
			filterAPI,
			statusupdater.NewModuleStatusUpdater(client, daemonAPI, metricsAPI),
		)

		if err = mc.SetupWithManager(mgr, constants.KernelLabel); err != nil {
			fatal(setupLogger, err, "unable to create controller")
		}
	}

	if m.startsNodeKernelClusterClaimReconciler {
		setupLogger := setupLogger.WithValues("name", controllers.NodeKernelClusterClaimReconcilerName)
		setupLogger.Info("Adding controller")

		if err = clusterv1.Install(scheme); err != nil {
			fatal(setupLogger, err, "could not add the Cluster API to the scheme")
		}

		if err = controllers.NewNodeKernelClusterClaimReconciler(client).SetupWithManager(mgr); err != nil {
			fatal(setupLogger, err, "unable to create controller")
		}
	}

	if m.startsNodeKernelReconciler {
		setupLogger := setupLogger.WithValues("name", controllers.NodeKernelReconcilerName)
		setupLogger.Info("Adding controller")

		nodeKernelReconciler := controllers.NewNodeKernelReconciler(client, constants.KernelLabel, filterAPI)

		if err = nodeKernelReconciler.SetupWithManager(mgr); err != nil {
			fatal(setupLogger, err, "unable to create controller")
		}
	}

	if m.startsPodNodeModuleReconciler {
		setupLogger := setupLogger.WithValues("name", controllers.PodNodeModuleReconcilerName)
		setupLogger.Info("Adding controller")

		if err = controllers.NewPodNodeModuleReconciler(client, daemonAPI).SetupWithManager(mgr); err != nil {
			fatal(setupLogger, err, "unable to create controller")
		}
	}

	if m.startsPreflightValidationReconciler {
		setupLogger := setupLogger.WithValues("name", controllers.PreflightValidationReconcilerName)
		setupLogger.Info("Adding controller")

		preflightStatusUpdaterAPI := statusupdater.NewPreflightStatusUpdater(client)
		preflightAPI := preflight.NewPreflightAPI(client, buildAPI, signAPI, registryAPI, preflightStatusUpdaterAPI, kernelAPI)

		if err = controllers.NewPreflightValidationReconciler(client, filterAPI, preflightStatusUpdaterAPI, preflightAPI).SetupWithManager(mgr); err != nil {
			fatal(setupLogger, err, "unable to create controller")
		}
	}

	//+kubebuilder:scaffold:builder

	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		fatal(setupLogger, err, "unable to set up health check")
	}
	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		fatal(setupLogger, err, "unable to set up ready check")
	}

	setupLogger.Info("starting manager")
	if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		fatal(setupLogger, err, "problem running manager")
	}
}

func gitCommit() (string, error) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", errors.New("build info is not available")
	}

	const vcsRevisionKey = "vcs.revision"

	for _, s := range bi.Settings {
		if s.Key == vcsRevisionKey {
			return s.Value, nil
		}
	}

	return "", fmt.Errorf("%s not found in build info settings", vcsRevisionKey)
}
