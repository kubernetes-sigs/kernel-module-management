package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
)

//go:generate mockgen -source=worker.go -package=worker -destination=mock_worker.go

type Worker interface {
	LoadKmod(ctx context.Context, cfg *kmmv1beta1.ModuleConfig) error
	SetFirmwareClassPath(value string) error
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

var firmwareClassPathLocation = FirmwareClassPathLocation

func (w *worker) SetFirmwareClassPath(value string) error {
	orig, err := os.ReadFile(firmwareClassPathLocation)
	if err != nil {
		return fmt.Errorf("could not read %s: %v", firmwareClassPathLocation, err)
	}

	origStr := string(orig)

	w.logger.V(1).Info("Read current firmware_class.path", "value", origStr)

	if string(orig) != value {
		w.logger.V(1).Info("Writing new firmware_class.path", "value", value)

		// 0666 set by os.Create; reuse that
		if err = os.WriteFile(firmwareClassPathLocation, []byte(value), 0666); err != nil {
			return fmt.Errorf("could not write %q into %s: %v", value, firmwareClassPathLocation, err)
		}
	}

	return nil
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
