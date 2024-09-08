package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-logr/logr"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	cp "github.com/otiai10/copy"
)

//go:generate mockgen -source=worker.go -package=worker -destination=mock_worker.go

type Worker interface {
	LoadKmod(ctx context.Context, cfg *kmmv1beta1.ModuleConfig, firmwareMountPath string) error
	SetFirmwareClassPath(value string) error
	UnloadKmod(ctx context.Context, cfg *kmmv1beta1.ModuleConfig, firmwareMountPath string) error
}

type worker struct {
	logger logr.Logger
	mr     ModprobeRunner
	fh     utils.FSHelper
}

func NewWorker(mr ModprobeRunner, fh utils.FSHelper, logger logr.Logger) Worker {
	return &worker{
		logger: logger,
		mr:     mr,
		fh:     fh,
	}
}

const sharedFilesDir = "/tmp"

func (w *worker) LoadKmod(ctx context.Context, cfg *kmmv1beta1.ModuleConfig, firmwareMountPath string) error {

	inTreeModulesToRemove := cfg.InTreeModulesToRemove
	// [TODO] - remove handling cfg.InTreeModuleToRemove once we cease to support it
	if inTreeModulesToRemove == nil && cfg.InTreeModuleToRemove != "" {
		inTreeModulesToRemove = []string{cfg.InTreeModuleToRemove}
	}

	if inTreeModulesToRemove != nil {
		w.logger.Info("Unloading in-tree modules", "names", inTreeModulesToRemove)

		runArgs := append([]string{"-rv"}, inTreeModulesToRemove...)
		if err := w.mr.Run(ctx, runArgs...); err != nil {
			return fmt.Errorf("could not remove in-tree modules %s: %v", strings.Join(inTreeModulesToRemove, ""), err)
		}
	}

	// prepare firmware
	if cfg.Modprobe.FirmwarePath != "" {
		imageFirmwarePath := filepath.Join(sharedFilesDir, cfg.Modprobe.FirmwarePath)
		w.logger.Info("preparing firmware for loading", "image directory", imageFirmwarePath, "host mount directory", firmwareMountPath)
		options := cp.Options{
			OnError: func(src, dest string, err error) error {
				if err != nil {
					return fmt.Errorf("internal copy error: failed to copy from %s to %s: %v", src, dest, err)
				}
				return nil
			},
		}
		if err := cp.Copy(imageFirmwarePath, firmwareMountPath, options); err != nil {
			return fmt.Errorf("failed to copy firmware from path %s to path %s: %v", imageFirmwarePath, firmwareMountPath, err)
		}
	}

	moduleName := cfg.Modprobe.ModuleName

	var args []string

	if cfg.Modprobe.RawArgs != nil {
		args = cfg.Modprobe.RawArgs.Load
	} else {
		args = []string{"-vd", filepath.Join(sharedFilesDir, cfg.Modprobe.DirName)}

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

func (w *worker) UnloadKmod(ctx context.Context, cfg *kmmv1beta1.ModuleConfig, firmwareMountPath string) error {

	moduleName := cfg.Modprobe.ModuleName

	var args []string

	if cfg.Modprobe.RawArgs != nil {
		args = cfg.Modprobe.RawArgs.Unload
	} else {
		args = []string{"-rvd", filepath.Join(sharedFilesDir, cfg.Modprobe.DirName)}

		if cfg.Modprobe.Args != nil {
			args = append(args, cfg.Modprobe.Args.Unload...)
		}

		args = append(args, moduleName)
	}

	w.logger.Info("Unloading module", "name", moduleName)

	if err := w.mr.Run(ctx, args...); err != nil {
		return fmt.Errorf("could not unload module %s: %v", moduleName, err)
	}

	//remove firmware files only (no directories)
	if cfg.Modprobe.FirmwarePath != "" {
		imageFirmwarePath := filepath.Join(sharedFilesDir, cfg.Modprobe.FirmwarePath)
		err := w.fh.RemoveSrcFilesFromDst(imageFirmwarePath, firmwareMountPath)
		if err != nil {
			w.logger.Info(utils.WarnString("failed to remove all firmware blobs"), "error", err)
		}
	}

	return nil
}
