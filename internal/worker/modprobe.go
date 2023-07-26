package worker

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/go-logr/logr"
)

//go:generate mockgen -source=modprobe.go -package=worker -destination=mock_modprobe.go

type ModprobeRunner interface {
	Run(ctx context.Context, args ...string) error
}

type modprobeRunnerImpl struct {
	logger logr.Logger
}

func NewModprobeRunner(logger logr.Logger) ModprobeRunner {
	return &modprobeRunnerImpl{logger: logger}
}

func (mr *modprobeRunnerImpl) Run(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "modprobe", args...)

	cl, err := NewCommandLogger(cmd, mr.logger.WithName("modprobe"))
	if err != nil {
		return fmt.Errorf("could not create a command logger: %v", err)
	}

	mr.logger.Info("Running modprobe", "command", cmd.String())

	if err = cmd.Start(); err != nil {
		return fmt.Errorf("could not start modprobe: %v", err)
	}

	if err = cl.Wait(); err != nil {
		return fmt.Errorf("error while waiting on the command logger: %v", err)
	}

	if err = cmd.Wait(); err != nil {
		return fmt.Errorf("error while waiting on the command: %v", err)
	}

	return nil
}
