package main

import (
	"fmt"

	"github.com/kubernetes-sigs/kernel-module-management/internal/worker"
	"github.com/spf13/cobra"
)

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
