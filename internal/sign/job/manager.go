package signjob

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

type signJobManager struct {
	client    client.Client
	signer    Signer
	jobHelper utils.JobHelper
	registry  registry.Registry
}

func NewSignJobManager(
	client client.Client,
	signer Signer,
	jobHelper utils.JobHelper,
	registry registry.Registry) *signJobManager {
	return &signJobManager{
		client:    client,
		signer:    signer,
		jobHelper: jobHelper,
		registry:  registry,
	}
}

func (jbm *signJobManager) GarbageCollect(ctx context.Context, modName, namespace string, owner metav1.Object) ([]string, error) {
	jobs, err := jbm.jobHelper.GetModuleJobs(ctx, modName, namespace, utils.JobTypeSign, owner)
	if err != nil {
		return nil, fmt.Errorf("failed to get sign jobs for module %s: %v", modName, err)
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

func (jbm *signJobManager) ShouldSync(ctx context.Context, mld *api.ModuleLoaderData) (bool, error) {

	// if there is no sign specified skip
	if !module.ShouldBeSigned(mld) {
		return false, nil
	}

	exists, err := module.ImageExists(ctx, jbm.client, jbm.registry, mld, mld.Namespace, mld.ContainerImage)
	if err != nil {
		return false, fmt.Errorf("failed to check existence of image %s: %w", mld.ContainerImage, err)
	}

	return !exists, nil
}

func (jbm *signJobManager) Sync(
	ctx context.Context,
	mld *api.ModuleLoaderData,
	imageToSign string,
	pushImage bool,
	owner metav1.Object) (utils.Status, error) {

	logger := log.FromContext(ctx)

	logger.Info("Signing in-cluster")

	labels := jbm.jobHelper.JobLabels(mld.Name, mld.KernelVersion, "sign")

	jobTemplate, err := jbm.signer.MakeJobTemplate(ctx, mld, labels, imageToSign, pushImage, owner)
	if err != nil {
		return "", fmt.Errorf("could not make Job template: %v", err)
	}

	job, err := jbm.jobHelper.GetModuleJobByKernel(ctx, mld.Name, mld.Namespace, mld.KernelVersion, utils.JobTypeSign, owner)
	if err != nil {
		if !errors.Is(err, utils.ErrNoMatchingJob) {
			return "", fmt.Errorf("error getting the signing job: %v", err)
		}

		logger.Info("Creating job")
		err = jbm.jobHelper.CreateJob(ctx, jobTemplate)
		if err != nil {
			return "", fmt.Errorf("could not create Signing Job: %v", err)
		}

		return utils.StatusCreated, nil
	}
	// default, there are no errors, and there is a job, check if it has changed
	changed, err := jbm.jobHelper.IsJobChanged(job, jobTemplate)
	if err != nil {
		return "", fmt.Errorf("could not determine if job has changed: %v", err)
	}

	if changed {
		logger.Info("The module's sign spec has been changed, deleting the current job so a new one can be created", "name", job.Name)
		err = jbm.jobHelper.DeleteJob(ctx, job)
		if err != nil {
			logger.Info(utils.WarnString(fmt.Sprintf("failed to delete signing job %s: %v", job.Name, err)))
		}
		return utils.StatusInProgress, nil
	}

	logger.Info("Returning job status", "name", job.Name, "namespace", job.Namespace)

	statusmsg, err := jbm.jobHelper.GetJobStatus(job)
	if err != nil {
		return "", err
	}

	return statusmsg, nil
}
