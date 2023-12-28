package main

import (
	"fmt"

	"github.com/kubernetes-sigs/kernel-module-management/internal/worker"
	"github.com/spf13/cobra"
)

func kmodFuncPreRunE(cmd *cobra.Command, args []string) error {
	err := cmd.Parent().PersistentPreRunE(cmd.Parent(), args)
	if err != nil {
		return fmt.Errorf("failed to call root command pre-run: %v", err)
	}

	logger.Info("Reading pull secrets", "base dir", worker.PullSecretsDir)
	keyChain, err := worker.ReadKubernetesSecrets(cmd.Context(), worker.PullSecretsDir, logger)
	if err != nil {
		return fmt.Errorf("could not read pull secrets: %v", err)
	}

	ip := worker.NewRemoteImageMounter(worker.ImagesDir, keyChain, logger)
	mr := worker.NewModprobeRunner(logger)
	w = worker.NewWorker(ip, mr, logger)

	return nil
}

func kmodLoadFunc(cmd *cobra.Command, args []string) error {
	cfgPath := args[0]

	logger.V(1).Info("Reading config", "path", cfgPath)

	cfg, err := configHelper.ReadConfigFile(cfgPath)
	if err != nil {
		return fmt.Errorf("could not read config file %s: %v", cfgPath, err)
	}

	if flag := cmd.Flags().Lookup(worker.FlagFirmwareClassPath); flag.Changed {
		logger.V(1).Info(worker.FlagFirmwareClassPath + " set, setting firmware_class.path")

		if err := w.SetFirmwareClassPath(flag.Value.String()); err != nil {
			return fmt.Errorf("could not set the firmware_class.path parameter: %v", err)
		}
	}

	mountPathFlag := cmd.Flags().Lookup(worker.FlagFirmwareMountPath)

	return w.LoadKmod(cmd.Context(), cfg, mountPathFlag.Value.String())
}

func kmodUnloadFunc(cmd *cobra.Command, args []string) error {
	cfgPath := args[0]

	logger.V(1).Info("Reading config", "path", cfgPath)

	cfg, err := configHelper.ReadConfigFile(cfgPath)
	if err != nil {
		return fmt.Errorf("could not read config file %s: %v", cfgPath, err)
	}

	mountPathFlag := cmd.Flags().Lookup(worker.FlagFirmwareMountPath)

	return w.UnloadKmod(cmd.Context(), cfg, mountPathFlag.Value.String())
}
