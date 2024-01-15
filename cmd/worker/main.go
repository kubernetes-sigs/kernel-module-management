package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/go-logr/logr"
	kmmcmd "github.com/kubernetes-sigs/kernel-module-management/internal/cmd"
	"github.com/kubernetes-sigs/kernel-module-management/internal/worker"
	"github.com/spf13/cobra"
	"k8s.io/klog/v2/textlogger"
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
	PersistentPreRunE: rootFuncPreRunE,
}

var kmodCmd = &cobra.Command{
	Use:   "kmod",
	Short: "Manage kernel modules",
}

var kmodLoadCmd = &cobra.Command{
	Use:   "load",
	Short: "Load a kernel module",
	Args:  cobra.ExactArgs(1),
	RunE:  kmodLoadFunc,
}

var kmodUnloadCmd = &cobra.Command{
	Use:   "unload",
	Short: "Unload a kernel module",
	Args:  cobra.ExactArgs(1),
	RunE:  kmodUnloadFunc,
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM)
	defer cancel()

	rootCmd.AddCommand(kmodCmd)

	kmodCmd.AddCommand(kmodLoadCmd, kmodUnloadCmd)

	setCommandsFlags()

	configureLogging()

	if err := rootCmd.ExecuteContext(ctx); err != nil {
		kmmcmd.FatalError(logger, err, "Fatal error")
		os.Exit(1)
	}
}

func configureLogging() {
	klogFlagSet := flag.NewFlagSet("klog", flag.ContinueOnError)
	logConfig := textlogger.NewConfig()
	logConfig.AddFlags(klogFlagSet)
	logger = textlogger.NewLogger(logConfig).WithName("kmm-worker")
	rootCmd.PersistentFlags().AddGoFlagSet(klogFlagSet)
}
