package worker

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/go-logr/logr"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
)

type Worker interface {
	LoadKmod(ctx context.Context, cfg *kmmv1beta1.ModuleConfig) error
	UnloadKmod(ctx context.Context, cfg *kmmv1beta1.ModuleConfig) error
}

type worker struct {
	ip     ImagePuller
	logger logr.Logger
	mr     ModprobeRunner
}

func NewWorker(ip ImagePuller, mr ModprobeRunner, logger logr.Logger) Worker {
	return &worker{
		ip:     ip,
		logger: logger,
		mr:     mr,
	}
}

func (w *worker) LoadKmod(ctx context.Context, cfg *kmmv1beta1.ModuleConfig) error {
	imageName := cfg.ContainerImage

	w.logger.Info("Pulling image", "name", imageName)

	pr, err := w.ip.PullAndExtract(ctx, imageName, cfg.InsecurePull)
	if err != nil {
		return fmt.Errorf("could not pull %q: %v", imageName, err)
	}

	if inTree := cfg.InTreeModuleToRemove; inTree != "" {
		w.logger.Info("Unloading in-tree module", "name", inTree)

		if err = w.mr.Run(ctx, "-rv", inTree); err != nil {
			return fmt.Errorf("could not remove in-tree module %s: %v", inTree, err)
		}
	}

	// TODO copy firmware
	// TODO handle ModulesLoadingOrder

	moduleName := cfg.Modprobe.ModuleName

	var args []string

	if cfg.Modprobe.RawArgs != nil {
		args = cfg.Modprobe.RawArgs.Load
	} else {
		args = []string{"-vd", filepath.Join(pr.fsDir, cfg.Modprobe.DirName)}

		if cfg.Modprobe.Args != nil {
			args = append(args, cfg.Modprobe.Args.Load...)
		}

		args = append(args, moduleName)
		args = append(args, cfg.Modprobe.Parameters...)
	}

	return w.mr.Run(ctx, args...)
}

func (w *worker) UnloadKmod(ctx context.Context, cfg *kmmv1beta1.ModuleConfig) error {
	imageName := cfg.ContainerImage

	w.logger.Info("Pulling image", "name", imageName)

	pr, err := w.ip.PullAndExtract(ctx, imageName, cfg.InsecurePull)
	if err != nil {
		return fmt.Errorf("could not pull %q: %v", imageName, err)
	}

	moduleName := cfg.Modprobe.ModuleName

	var args []string

	if cfg.Modprobe.RawArgs != nil {
		args = cfg.Modprobe.RawArgs.Unload
	} else {
		args = []string{"-rvd", filepath.Join(pr.fsDir, cfg.Modprobe.DirName)}

		if cfg.Modprobe.Args != nil {
			args = append(args, cfg.Modprobe.Args.Unload...)
		}

		args = append(args, moduleName)
	}

	w.logger.Info("Unloading module", "name", moduleName)

	if err = w.mr.Run(ctx, args...); err != nil {
		return fmt.Errorf("could not unload module %s: %v", moduleName, err)
	}

	// TODO remove firmware

	return nil
}
