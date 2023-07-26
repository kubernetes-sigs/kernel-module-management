package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-logr/logr"
	kmmcmd "github.com/kubernetes-sigs/kernel-module-management/internal/cmd"
	"github.com/kubernetes-sigs/kernel-module-management/internal/worker"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
)

var (
	Version = "undefined"

	configHelper = worker.NewConfigHelper()
	logger       logr.Logger
	w            worker.Worker
)

var rootCmd = &cobra.Command{
	CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	SilenceUsage:      true,
	SilenceErrors:     true,
	Use:               "worker",
	Version:           Version,
}

var kmodCmd = &cobra.Command{
	Use:   "kmod",
	Short: "Manage kernel modules",
}

var kmodLoadCmd = &cobra.Command{
	Use:   "load",
	Short: "Load a kernel module",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := args[0]

		logger.V(1).Info("Reading config", "path", cfgPath)

		cfg, err := configHelper.ReadConfigFile(cfgPath)
		if err != nil {
			return fmt.Errorf("could not read config file %s: %v", cfgPath, err)
		}

		return w.LoadKmod(cmd.Context(), cfg)
	},
}

var kmodUnloadCmd = &cobra.Command{
	Use:   "unload",
	Short: "Unload a kernel module",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfgPath := args[0]

		logger.V(1).Info("Reading config", "path", cfgPath)

		cfg, err := configHelper.ReadConfigFile(cfgPath)
		if err != nil {
			return fmt.Errorf("could not read config file %s: %v", cfgPath, err)
		}

		return w.UnloadKmod(cmd.Context(), cfg)
	},
}

func main() {
	var imgBaseDir string

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer cancel()

	rootCmd.AddCommand(kmodCmd)

	kmodCmd.AddCommand(kmodLoadCmd, kmodUnloadCmd)

	klogFlagSet := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(klogFlagSet)

	rootCmd.PersistentFlags().AddGoFlagSet(klogFlagSet)
	rootCmd.PersistentFlags().StringVar(&imgBaseDir, "img-base-dir", "/mnt/img", "path to the base directory for extracted images")

	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		logger = klogr.New().WithName("kmm-worker")

		commit, err := kmmcmd.GitCommit()
		if err != nil {
			logger.Error(err, "Could not get the git commit; using <undefined>")
			commit = "<undefined>"
		}

		logger.Info("Starting worker", "version", rootCmd.Version, "git commit", commit)

		ip := worker.NewImagePuller(imgBaseDir, logger)
		mr := worker.NewModprobeRunner(logger)
		w = worker.NewWorker(ip, mr, logger)
	}

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		kmmcmd.FatalError(logger, err, "Fatal error")
		os.Exit(1)
	}
}
