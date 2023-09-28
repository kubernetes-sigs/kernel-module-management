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
	"time"

	"github.com/kubernetes-sigs/kernel-module-management/internal/config"
	"github.com/kubernetes-sigs/kernel-module-management/internal/controllers"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build/pod"
	"github.com/kubernetes-sigs/kernel-module-management/internal/cache"
	"github.com/kubernetes-sigs/kernel-module-management/internal/cluster"
	"github.com/kubernetes-sigs/kernel-module-management/internal/cmd"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/controllers/hub"
	"github.com/kubernetes-sigs/kernel-module-management/internal/filter"
	"github.com/kubernetes-sigs/kernel-module-management/internal/manifestwork"
	"github.com/kubernetes-sigs/kernel-module-management/internal/metrics"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/nmc"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	signpod "github.com/kubernetes-sigs/kernel-module-management/internal/sign/pod"
	"github.com/kubernetes-sigs/kernel-module-management/internal/statusupdater"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	//+kubebuilder:scaffold:imports
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(v1beta1.AddToScheme(scheme))
	utilruntime.Must(clusterv1.Install(scheme))
	utilruntime.Must(workv1.Install(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	logger := klogr.New().WithName("kmm-hub")

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

	setupLogger.Info("Creating manager", "git commit", commit)

	cfg, err := config.ParseFile(configFile)
	if err != nil {
		cmd.FatalError(setupLogger, err, "could not parse the configuration file", "path", configFile)
	}

	options := cfg.ManagerOptions()
	options.Scheme = scheme

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), *options)
	if err != nil {
		cmd.FatalError(setupLogger, err, "unable to create manager")
	}

	client := mgr.GetClient()

	nmcHelper := nmc.NewHelper(client)
	filterAPI := filter.New(client, nmcHelper)

	metricsAPI := metrics.New()
	metricsAPI.Register()

	registryAPI := registry.NewRegistry()
	podHelperAPI := utils.NewPodHelper(client)
	buildHelper := build.NewHelper()

	buildAPI := pod.NewBuildManager(
		client,
		pod.NewMaker(client, buildHelper, podHelperAPI, scheme),
		podHelperAPI,
		registryAPI,
	)

	signAPI := signpod.NewSignPodManager(
		client,
		signpod.NewSigner(client, scheme, podHelperAPI),
		podHelperAPI,
		registryAPI,
	)

	kernelAPI := module.NewKernelMapper(buildHelper, sign.NewSignerHelper())

	ctrlLogger := setupLogger.WithValues("name", hub.ManagedClusterModuleReconcilerName)
	ctrlLogger.Info("Adding controller")

	operatorNamespace := cmd.GetEnvOrFatalError(constants.OperatorNamespaceEnvVar, setupLogger)

	cache := cache.New[string](10 * time.Minute)
	ctx := ctrl.SetupSignalHandler()
	cache.StartCollecting(ctx, 10*time.Minute)

	mcmr := hub.NewManagedClusterModuleReconciler(
		client,
		manifestwork.NewCreator(client, scheme, kernelAPI, registryAPI, cache, operatorNamespace),
		cluster.NewClusterAPI(client, kernelAPI, buildAPI, signAPI, operatorNamespace),
		statusupdater.NewManagedClusterModuleStatusUpdater(client),
		filterAPI,
	)

	if err = mcmr.SetupWithManager(mgr); err != nil {
		cmd.FatalError(ctrlLogger, err, "unable to create controller")
	}

	eventRecorder := mgr.GetEventRecorderFor("kmm-hub")

	if err = controllers.NewJobEventReconciler(client, eventRecorder).SetupWithManager(mgr); err != nil {
		cmd.FatalError(setupLogger, err, "unable to create controller", "name", controllers.JobEventReconcilerName)
	}

	if err = (&v1beta1.ManagedClusterModule{}).SetupWebhookWithManager(mgr); err != nil {
		cmd.FatalError(setupLogger, err, "unable to create webhook", "webhook", "ManagedClusterModule")
	}

	//+kubebuilder:scaffold:builder

	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		cmd.FatalError(setupLogger, err, "unable to set up health check")
	}
	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		cmd.FatalError(setupLogger, err, "unable to set up ready check")
	}

	setupLogger.Info("starting manager")
	if err = mgr.Start(ctx); err != nil {
		cmd.FatalError(setupLogger, err, "problem running manager")
	}

	cache.WaitForTermination()
}
