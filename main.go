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

	"github.com/qbarrand/oot-operator/controllers/build"
	"github.com/qbarrand/oot-operator/controllers/build/job"
	"github.com/qbarrand/oot-operator/controllers/module"
	"k8s.io/apimachinery/pkg/util/sets"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/controllers"
	//+kubebuilder:scaffold:imports
)

const (
	NFDKernelLabelingMethod  = "nfd"
	OOTOKernelLabelingMethod = "ooto"

	KernelLabelingMethodEnvVar = "KERNEL_LABELING_METHOD"
)

var (
	scheme               = runtime.NewScheme()
	setupLog             = ctrl.Log.WithName("setup")
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

	opts := zap.Options{
		//Development: true,
	}

	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	commit, err := gitCommit()
	if err != nil {
		setupLog.Error(err, "Could not get the git commit; using <undefined>")
		commit = "<undefined>"
	}

	setupLog.Info("Creating manager", "git commit", commit)

	namespace := GetEnvWithDefault("OPERATOR_NAMESPACE", "default")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Namespace:              namespace,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "c5baf8af.sigs.k8s.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	client := mgr.GetClient()

	var (
		kernelLabel          string
		kernelLabelingMethod = GetEnvWithDefault(KernelLabelingMethodEnvVar, OOTOKernelLabelingMethod)
	)

	setupLog.V(1).Info("Determining kernel labeling method", KernelLabelingMethodEnvVar, kernelLabelingMethod)

	switch kernelLabelingMethod {
	case OOTOKernelLabelingMethod:
		kernelLabel = "oot.node.kubernetes.io/kernel-version.full"

		nkr := controllers.NewNodeKernelReconciler(client, kernelLabel)

		if err = nkr.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "NodeKernel")
			os.Exit(1)
		}
	case NFDKernelLabelingMethod:
		kernelLabel = "feature.node.kubernetes.io/kernel-version.full"
	default:
		setupLog.Error(
			fmt.Errorf("%q is not in %v", kernelLabelingMethod, validLabelingMethods.List()),
			"Invalid kernel labeling method",
		)

		os.Exit(1)
	}

	setupLog.V(1).Info("Using kernel label", "label", kernelLabel)

	bm := job.NewBuildManager(client, build.NewGetter(), job.NewMaker(namespace, scheme), namespace)
	dc := controllers.NewDaemonSetCreator(client, kernelLabel, namespace, scheme)
	km := module.NewKernelMapper()
	su := module.NewConditionsUpdater(client.Status())

	mc := controllers.NewModuleReconciler(client, namespace, bm, dc, km, su)

	if err = mc.SetupWithManager(mgr, kernelLabel); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "Module")
		os.Exit(1)
	}

	dsr := controllers.NewDaemonSetReconciler(client)

	if err = dsr.SetupWithManager(mgr, namespace); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "DaemonSet")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder

	if err = mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err = mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err = mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
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
