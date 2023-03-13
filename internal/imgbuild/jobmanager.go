package imgbuild

import (
	"context"
	"errors"
	"fmt"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type JobType string

const (
	JobTypeBuild JobType = "build"
	JobTypeSign  JobType = "sign"
)

type BaseJobManager struct {
	client    client.Client
	jobHelper JobHelper
	jobType   JobType
	maker     JobMaker
	registry  registry.Registry
}

func (bjm *BaseJobManager) GarbageCollect(ctx context.Context, modName, namespace string, owner metav1.Object) ([]string, error) {
	jobs, err := bjm.jobHelper.GetModuleJobs(ctx, modName, namespace, bjm.jobType, owner)
	if err != nil {
		return nil, fmt.Errorf("failed to get build jobs for module %s: %v", modName, err)
	}

	deleteNames := make([]string, 0, len(jobs))
	for _, job := range jobs {
		if job.Status.Succeeded == 1 {
			err = bjm.jobHelper.DeleteJob(ctx, &job)
			if err != nil {
				return nil, fmt.Errorf("failed to delete build job %s: %v", job.Name, err)
			}
			deleteNames = append(deleteNames, job.Name)
		}
	}
	return deleteNames, nil
}

func (bjm *BaseJobManager) Sync(ctx context.Context, mld *api.ModuleLoaderData, pushImage bool, owner metav1.Object) (Status, error) {
	logger := log.FromContext(ctx).WithValues("type", bjm.jobType)

	logger.Info("Syncing job")

	jobTemplate, err := bjm.maker.MakeJob(ctx, mld, owner, pushImage)
	if err != nil {
		return "", fmt.Errorf("could not make Job template: %v", err)
	}

	job, err := bjm.jobHelper.GetModuleJobByKernel(ctx, mld.Name, mld.Namespace, mld.KernelVersion, bjm.jobType, owner)
	if err != nil {
		if !errors.Is(err, ErrNoMatchingJob) {
			return "", fmt.Errorf("error getting the build: %v", err)
		}

		logger.Info("Creating job")
		err = bjm.jobHelper.CreateJob(ctx, jobTemplate)
		if err != nil {
			return "", fmt.Errorf("could not create Job: %v", err)
		}

		return StatusCreated, nil
	}

	changed, err := bjm.jobHelper.IsJobChanged(job, jobTemplate)
	if err != nil {
		return "", fmt.Errorf("could not determine if job has changed: %v", err)
	}

	if changed {
		logger.Info("The Job spec has changed, deleting the current job so a new one can be created", "name", job.Name)
		err = bjm.jobHelper.DeleteJob(ctx, job)
		if err != nil {
			logger.Info(utils.WarnString(fmt.Sprintf("failed to delete build job %s: %v", job.Name, err)))
		}
		return StatusInProgress, nil
	}

	logger.Info("Returning job status", "name", job.Name, "namespace", job.Namespace)

	statusmsg, err := bjm.jobHelper.GetJobStatus(job)
	if err != nil {
		return "", err
	}

	return statusmsg, nil
}

type buildJobManager struct {
	*BaseJobManager
}

func NewBuildManager(client client.Client, helper JobHelper, maker JobMaker, registry registry.Registry) JobManager {
	return &buildJobManager{
		BaseJobManager: &BaseJobManager{
			client:    client,
			jobHelper: helper,
			jobType:   JobTypeBuild,
			maker:     maker,
			registry:  registry,
		},
	}
}

func (bjm *buildJobManager) ShouldSync(ctx context.Context, mld *api.ModuleLoaderData) (bool, error) {
	// if there is no build specified skip
	if !mld.BuildConfigured() {
		return false, nil
	}

	destinationImage, err := mld.BuildDestinationImage()
	if err != nil {
		return false, fmt.Errorf("could not determine the build destination image: %v", err)
	}

	// build is specified and targetImage is either the final image or the intermediate image
	// tag, depending on whether sign is specified or not. Either way, if targetImage exists
	// we can skip building it
	exists, err := module.ImageExists(ctx, bjm.client, bjm.registry, mld, mld.Namespace, destinationImage)
	if err != nil {
		return false, fmt.Errorf("failed to check existence of image %s: %w", destinationImage, err)
	}

	return !exists, nil
}

type signJobManager struct {
	*BaseJobManager
}

func NewSignManager(client client.Client, helper JobHelper, maker JobMaker, registry registry.Registry) JobManager {
	return &signJobManager{
		BaseJobManager: &BaseJobManager{
			client:    client,
			jobHelper: helper,
			jobType:   JobTypeSign,
			maker:     maker,
			registry:  registry,
		},
	}
}

func (sjm *signJobManager) ShouldSync(ctx context.Context, mld *api.ModuleLoaderData) (bool, error) {
	if !mld.SignConfigured() {
		return false, nil
	}

	exists, err := module.ImageExists(ctx, sjm.client, sjm.registry, mld, mld.Namespace, mld.ContainerImage)
	if err != nil {
		return false, fmt.Errorf("failed to check existence of image %s: %w", mld.ContainerImage, err)
	}

	return !exists, nil
}
