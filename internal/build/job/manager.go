package job

import (
	"context"
	"errors"
	"fmt"
	"strings"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var errNoMatchingBuild = errors.New("no matching build")

type jobManager struct {
	client client.Client
	helper build.Helper
	tasks  []Maker
}

func NewBuildManager(client client.Client, helper build.Helper, maker ...Maker) *jobManager {
	return &jobManager{
		client: client,
		helper: helper,
		tasks:  maker,
	}
}

func labels(mod kmmv1beta1.Module, targetKernel string, stage string) map[string]string {
	return map[string]string{
		constants.ModuleNameLabel:    mod.Name,
		constants.TargetKernelTarget: targetKernel,
		constants.BuildStage:         stage,
	}
}

func addTag(imagename string, tag string) string {
	if strings.Contains(imagename, ":") {
		return imagename + tag
	} else {
		return imagename + ":" + tag
	}
}

func (jbm *jobManager) getJob(ctx context.Context, mod kmmv1beta1.Module, targetKernel string, jobname string) (*batchv1.Job, error) {
	jobList := batchv1.JobList{}

	opts := []client.ListOption{
		client.MatchingLabels(labels(mod, targetKernel, jobname)),
		client.InNamespace(mod.Namespace),
	}

	if err := jbm.client.List(ctx, &jobList, opts...); err != nil {
		return nil, fmt.Errorf("could not list jobs: %v", err)
	}

	if n := len(jobList.Items); n == 0 {
		return nil, errNoMatchingBuild
	} else if n > 1 {
		return nil, fmt.Errorf("expected 0 or 1 job, got %d", n)
	}

	return &jobList.Items[0], nil
}

func (jbm *jobManager) Sync(ctx context.Context, mod kmmv1beta1.Module, m kmmv1beta1.KernelMapping, targetKernel string, pushImage bool) (build.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Building in-cluster")

	buildConfig := jbm.helper.GetRelevantBuild(mod, m)

	var tasksToRun []Maker

	for _, task := range jbm.tasks {
		if task.ShouldRun(&mod, &m) {
			tasksToRun = append(tasksToRun, task)
		}
	}
	taskCount := len(tasksToRun) - 1
	previousImage := ""

	for i, task := range tasksToRun {
		taskname := task.GetName()
		job, err := jbm.getJob(ctx, mod, targetKernel, taskname)
		if err != nil {
			if !errors.Is(err, errNoMatchingBuild) {
				return build.Result{}, fmt.Errorf("error getting the %s job: %v", taskname, err)
			}

			logger.Info("Creating " + taskname + " job")

			outputImage := m.ContainerImage
			if taskCount > i {
				outputImage = addTag(outputImage, taskname)
			}

			job, err = task.MakeJob(mod, buildConfig, targetKernel, previousImage, outputImage, pushImage)
			if err != nil {
				return build.Result{}, fmt.Errorf("could not make %s Job: %v", taskname, err)
			}

			if err = jbm.client.Create(ctx, job); err != nil {
				return build.Result{}, fmt.Errorf("could not create %s Job: %v", taskname, err)
			}

			return build.Result{Status: build.StatusCreated, Requeue: true}, nil
		}
		logger.Info("Returning job status", "name", job.Name, "namespace", job.Namespace)

		switch {
		case job.Status.Succeeded == 1:
			// this job is done, move on the next one if we need to
			previousImage = addTag(m.ContainerImage, taskname)
			continue
		case job.Status.Active == 1:
			return build.Result{Status: build.StatusInProgress, Requeue: true}, nil
		case job.Status.Failed == 1:
			return build.Result{}, fmt.Errorf("job failed: %v", err)
		default:
			return build.Result{}, fmt.Errorf("unknown status: %v", job.Status)
		}
	}
	return build.Result{Status: build.StatusCompleted}, nil
}
