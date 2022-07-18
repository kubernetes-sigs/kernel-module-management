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

	"github.com/qbarrand/oot-operator/internal/auth"
	"github.com/qbarrand/oot-operator/internal/build"
	"github.com/qbarrand/oot-operator/internal/build/job"
	"github.com/qbarrand/oot-operator/internal/daemonset"
	"github.com/qbarrand/oot-operator/internal/filter"
	"github.com/qbarrand/oot-operator/internal/metrics"
	"github.com/qbarrand/oot-operator/internal/module"
	"github.com/qbarrand/oot-operator/internal/registry"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2/klogr"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/controllers"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	//+kubebuilder:scaffold:imports
)

const (
	NFDKernelLabelingMethod  = "nfd"
	OOTOKernelLabelingMethod = "ooto"

	KernelLabelingMethodEnvVar = "KERNEL_LABELING_METHOD"
)

var (
	scheme               = runtime.NewScheme()
	validLabelingMethods = sets.NewString(OOTOKernelLabelingMethod, NFDKernelLabelingMethod)
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(ootov1alpha1.AddToScheme(scheme))
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

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "c5baf8af.sigs.k8s.io",
	})
	if err != nil {
		setupLogger.Error(err, "unable to start manager")
		os.Exit(1)
	}

	client := mgr.GetClient()

	var (
		kernelLabel          string
		kernelLabelingMethod = GetEnvWithDefault(KernelLabelingMethodEnvVar, OOTOKernelLabelingMethod)
	)

	setupLogger.V(1).Info("Determining kernel labeling method", KernelLabelingMethodEnvVar, kernelLabelingMethod)

	filter := filter.New(client, mgr.GetLogger())

	switch kernelLabelingMethod {
	case OOTOKernelLabelingMethod:
		kernelLabel = "oot.node.kubernetes.io/kernel-version.full"

		nodeKernelReconciler := controllers.NewNodeKernelReconciler(client, kernelLabel, filter)

		if err = nodeKernelReconciler.SetupWithManager(mgr); err != nil {
			setupLogger.Error(err, "unable to create controller", "controller", "NodeKernel")
			os.Exit(1)
		}
	case NFDKernelLabelingMethod:
		kernelLabel = "feature.node.kubernetes.io/kernel-version.full"
	default:
		setupLogger.Error(
			fmt.Errorf("%q is not in %v", kernelLabelingMethod, validLabelingMethods.List()),
			"Invalid kernel labeling method",
		)

		os.Exit(1)
	}

	setupLogger.V(1).Info("Using kernel label", "label", kernelLabel)

	metricsAPI := metrics.New()
	metricsAPI.Register()
	registryAuthAPI := auth.NewRegistryAuth(client)
	registryAPI := registry.NewRegistry(registryAuthAPI)
	helperAPI := build.NewHelper()
	makerAPI := job.NewMaker(helperAPI, scheme)
	buildAPI := job.NewBuildManager(client, registryAPI, makerAPI, helperAPI)
	daemonAPI := daemonset.NewCreator(client, kernelLabel, scheme)
	kernelAPI := module.NewKernelMapper()
	statusUpdaterAPI := module.NewStatusUpdater(client, daemonAPI, metricsAPI)

	mc := controllers.NewModuleReconciler(client, buildAPI, daemonAPI, kernelAPI, metricsAPI, filter, statusUpdaterAPI)

	if err = mc.SetupWithManager(mgr, kernelLabel); err != nil {
		setupLogger.Error(err, "unable to create controller", "controller", "Module")
		os.Exit(1)
	}

	if err = controllers.NewPodNodeModuleReconciler(client).SetupWithManager(mgr); err != nil {
		setupLogger.Error(err, "unable to create controller", "controller", "PodNodeModule")
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

func GetEnvWithDefault(key, def string) string {
	v, ok := os.LookupEnv(key)
	if !ok {
		v = def
	}

	return v
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
