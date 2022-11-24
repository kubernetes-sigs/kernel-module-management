package job

import (
	"context"
	"errors"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type jobManager struct {
	client    client.Client
	maker     Maker
	helper    build.Helper
	jobHelper utils.JobHelper
}

func NewBuildManager(client client.Client, maker Maker, helper build.Helper, jobHelper utils.JobHelper) *jobManager {
	return &jobManager{
		client:    client,
		maker:     maker,
		helper:    helper,
		jobHelper: jobHelper,
	}
}

func (jbm *jobManager) GarbageCollect(ctx context.Context, mod kmmv1beta1.Module) ([]string, error) {
	jobs, err := jbm.jobHelper.GetModuleJobs(ctx, mod, utils.JobTypeBuild)
	if err != nil {
		return nil, fmt.Errorf("failed to get build jobs for module %s: %v", mod.Name, err)
	}

	deleteNames := make([]string, 0, len(jobs))
	for _, job := range jobs {
		if job.Status.Succeeded == 1 {
			err = jbm.jobHelper.DeleteJob(ctx, &job)
			if err != nil {
				return nil, fmt.Errorf("failed to delete build job %s: %v", job.Name, err)
			}
			deleteNames = append(deleteNames, job.Name)
		}
	}
	return deleteNames, nil
}

func (jbm *jobManager) Sync(ctx context.Context, mod kmmv1beta1.Module, m kmmv1beta1.KernelMapping, targetKernel string, targetImage string, pushImage bool) (build.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Building in-cluster")

	buildConfig := jbm.helper.GetRelevantBuild(mod, m)

	jobTemplate, err := jbm.maker.MakeJobTemplate(ctx, mod, buildConfig, targetKernel, targetImage, pushImage, m.RegistryTLS)
	if err != nil {
		return build.Result{}, fmt.Errorf("could not make Job template: %v", err)
	}

	job, err := jbm.jobHelper.GetModuleJobByKernel(ctx, mod, targetKernel, utils.JobTypeBuild)
	if err != nil {
		if !errors.Is(err, utils.ErrNoMatchingJob) {
			return build.Result{}, fmt.Errorf("error getting the build: %v", err)
		}

		logger.Info("Creating job")
		err = jbm.jobHelper.CreateJob(ctx, jobTemplate)
		if err != nil {
			return build.Result{}, fmt.Errorf("could not create Job: %v", err)
		}

		return build.Result{Status: build.StatusCreated, Requeue: true}, nil
	}

	changed, err := jbm.jobHelper.IsJobChanged(job, jobTemplate)
	if err != nil {
		return build.Result{}, fmt.Errorf("could not determine if job has changed: %v", err)
	}

	if changed {
		logger.Info("The module's build spec has been changed, deleting the current job so a new one can be created", "name", job.Name)
		err = jbm.jobHelper.DeleteJob(ctx, job)
		if err != nil {
			logger.Info(utils.WarnString(fmt.Sprintf("failed to delete build job %s: %v", job.Name, err)))
		}
		return build.Result{Status: build.StatusInProgress, Requeue: true}, nil
	}

	logger.Info("Returning job status", "name", job.Name, "namespace", job.Namespace)

	switch {
	case job.Status.Succeeded == 1:
		return build.Result{Status: build.StatusCompleted}, nil
	case job.Status.Active == 1:
		return build.Result{Status: build.StatusInProgress, Requeue: true}, nil
	case job.Status.Failed == 1:
		return build.Result{}, fmt.Errorf("job failed: %v", err)
	default:
		return build.Result{}, fmt.Errorf("unknown status: %v", job.Status)
	}
}
