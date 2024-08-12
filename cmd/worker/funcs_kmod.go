package main

import (
	"fmt"

	kmmcmd "github.com/kubernetes-sigs/kernel-module-management/internal/cmd"
	"github.com/kubernetes-sigs/kernel-module-management/internal/worker"
	"github.com/spf13/cobra"
)

func rootFuncPreRunE(cmd *cobra.Command, args []string) error {
	commit, err := kmmcmd.GitCommit()
	if err != nil {
		logger.Error(err, "Could not get the git commit; using <undefined>")
		commit = "<undefined>"
	}
	logger.Info("Starting worker", "version", Version, "git commit", commit)

	im, err := getImageMounter(cmd)
	if err != nil {
		return fmt.Errorf("failed to get appropriate ImageMounter: %v", err)
	}
	mr := worker.NewModprobeRunner(logger)
	w = worker.NewWorker(im, mr, logger)

	return nil
}

func kmodLoadFunc(cmd *cobra.Command, args []string) error {
	cfgPath := args[0]

	logger.Info("Reading config", "path", cfgPath)

	cfg, err := configHelper.ReadConfigFile(cfgPath)
	if err != nil {
		return fmt.Errorf("could not read config file %s: %v", cfgPath, err)
	}

	mountPathFlag := cmd.Flags().Lookup(worker.FlagFirmwarePath)
	if mountPathFlag.Changed {
		logger.V(1).Info(worker.FlagFirmwarePath + " set, setting firmware_class.path")

		if err := w.SetFirmwareClassPath(mountPathFlag.Value.String()); err != nil {
			return fmt.Errorf("could not set the firmware_class.path parameter: %v", err)
		}
	}

	return w.LoadKmod(cmd.Context(), cfg, mountPathFlag.Value.String())
}

func kmodUnloadFunc(cmd *cobra.Command, args []string) error {
	cfgPath := args[0]

	logger.Info("Reading config", "path", cfgPath)

	cfg, err := configHelper.ReadConfigFile(cfgPath)
	if err != nil {
		return fmt.Errorf("could not read config file %s: %v", cfgPath, err)
	}

	return w.UnloadKmod(cmd.Context(), cfg, cmd.Flags().Lookup(worker.FlagFirmwarePath).Value.String())
}

func setCommandsFlags() {
	kmodLoadCmd.Flags().String(
		worker.FlagFirmwarePath,
		"",
		"if set, this value will be written to "+worker.FirmwareClassPathLocation+" and it is also the value that firmware host path is mounted to")

	kmodUnloadCmd.Flags().String(
		worker.FlagFirmwarePath,
		"",
		"if set, this the value that firmware host path is mounted to")
}

func getImageMounter(cmd *cobra.Command) (worker.ImageMounter, error) {
	logger.Info("Reading pull secrets", "base dir", worker.PullSecretsDir)
	keyChain, err := worker.ReadKubernetesSecrets(cmd.Context(), worker.PullSecretsDir, logger)
	if err != nil {
		return nil, fmt.Errorf("could not read pull secrets: %v", err)
	}
	return worker.NewRemoteImageMounter(worker.ImagesDir, keyChain, logger), nil
}
