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

	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build/job"

	"github.com/kubernetes-sigs/kernel-module-management/internal/daemonset"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/metrics"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/preflight"
	"github.com/kubernetes-sigs/kernel-module-management/internal/rbac"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign/job"
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
	"github.com/kubernetes-sigs/kernel-module-management/controllers"
	//+kubebuilder:scaffold:imports
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(kmmv1beta1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var (
		configFile           string
		metricsAddr          string
		enableLeaderElection bool
		probeAddr            string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	flag.StringVar(&configFile, "config", "", "The path to the configuration file.")

	klog.InitFlags(flag.CommandLine)

	flag.Parse()

	logger := klogr.New()

	ctrl.SetLogger(logger)

	setupLogger := logger.WithName("setup")

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
			setupLogger.Error(err, "unable to load the config file")
			os.Exit(1)
		}
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), options)
	if err != nil {
		setupLogger.Error(err, "unable to start manager")
		os.Exit(1)
	}

	client := mgr.GetClient()

	filter := filter.New(client, mgr.GetLogger())

	const kernelLabel = "kmm.node.kubernetes.io/kernel-version.full"

	nodeKernelReconciler := controllers.NewNodeKernelReconciler(client, kernelLabel, filter)

	if err = nodeKernelReconciler.SetupWithManager(mgr); err != nil {
		setupLogger.Error(err, "unable to create controller", "controller", "NodeKernel")
		os.Exit(1)
	}

	metricsAPI := metrics.New()
	metricsAPI.Register()
	registryAPI := registry.NewRegistry()
	helperAPI := build.NewHelper()
	jobHelperAPI := utils.NewJobHelper(client)
	makerAPI := job.NewMaker(client, helperAPI, jobHelperAPI, scheme)
	buildAPI := job.NewBuildManager(client, makerAPI, helperAPI, jobHelperAPI)

	signAPI := signjob.NewSignJobManager(
		signjob.NewSigner(scheme),
		sign.NewSignerHelper(),
		utils.NewJobHelper(client),
	)

	rbacAPI := rbac.NewCreator(client, scheme)
	daemonAPI := daemonset.NewCreator(client, kernelLabel, scheme)
	kernelAPI := module.NewKernelMapper()
	moduleStatusUpdaterAPI := statusupdater.NewModuleStatusUpdater(client, daemonAPI, metricsAPI)
	preflightStatusUpdaterAPI := statusupdater.NewPreflightStatusUpdater(client)
	preflightAPI := preflight.NewPreflightAPI(client, buildAPI, registryAPI, preflightStatusUpdaterAPI, kernelAPI)

	mc := controllers.NewModuleReconciler(client, buildAPI, signAPI, rbacAPI, daemonAPI, kernelAPI, metricsAPI, filter, registryAPI, moduleStatusUpdaterAPI)

	if err = mc.SetupWithManager(mgr, kernelLabel); err != nil {
		setupLogger.Error(err, "unable to create controller", "controller", "Module")
		os.Exit(1)
	}

	if err = controllers.NewPodNodeModuleReconciler(client, daemonAPI).SetupWithManager(mgr); err != nil {
		setupLogger.Error(err, "unable to create controller", "controller", "PodNodeModule")
		os.Exit(1)
	}

	if err = controllers.NewPreflightValidationReconciler(client, filter, preflightStatusUpdaterAPI, preflightAPI).SetupWithManager(mgr); err != nil {
		setupLogger.Error(err, "unable to create controller", "controller", "Preflight")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLogger.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLogger.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLogger.Info("starting manager")
	if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLogger.Error(err, "problem running manager")
		os.Exit(1)
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
