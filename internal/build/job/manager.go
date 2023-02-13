package job

import (
	"context"
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

type jobManager struct {
	client    client.Client
	maker     Maker
	jobHelper utils.JobHelper
	registry  registry.Registry
}

func NewBuildManager(
	client client.Client,
	maker Maker,
	jobHelper utils.JobHelper,
	registry registry.Registry) *jobManager {
	return &jobManager{
		client:    client,
		maker:     maker,
		jobHelper: jobHelper,
		registry:  registry,
	}
}

func (jbm *jobManager) GarbageCollect(ctx context.Context, modName, namespace string, owner metav1.Object) ([]string, error) {
	jobs, err := jbm.jobHelper.GetModuleJobs(ctx, modName, namespace, utils.JobTypeBuild, owner)
	if err != nil {
		return nil, fmt.Errorf("failed to get build jobs for module %s: %v", modName, err)
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

func (jbm *jobManager) ShouldSync(
	ctx context.Context,
	mod kmmv1beta1.Module,
	m kmmv1beta1.KernelMapping) (bool, error) {

	// if there is no build specified skip
	if !module.ShouldBeBuilt(m) {
		return false, nil
	}

	targetImage := m.ContainerImage

	// if build AND sign are specified, then we will build an intermediate image
	// and let sign produce the one specified in targetImage
	if module.ShouldBeSigned(m) {
		targetImage = module.IntermediateImageName(mod.Name, mod.Namespace, targetImage)
	}

	// build is specified and targetImage is either the final image or the intermediate image
	// tag, depending on whether sign is specified or not. Either way, if targetImage exists
	// we can skip building it
	exists, err := module.ImageExists(ctx, jbm.client, jbm.registry, mod.Spec, mod.Namespace, m, targetImage)
	if err != nil {
		return false, fmt.Errorf("failed to check existence of image %s: %w", targetImage, err)
	}

	return !exists, nil
}

func (jbm *jobManager) Sync(
	ctx context.Context,
	mod kmmv1beta1.Module,
	m kmmv1beta1.KernelMapping,
	targetKernel string,
	pushImage bool,
	owner metav1.Object) (utils.Status, error) {

	logger := log.FromContext(ctx)

	logger.Info("Building in-cluster")

	jobTemplate, err := jbm.maker.MakeJobTemplate(ctx, mod, m, targetKernel, owner, pushImage)
	if err != nil {
		return "", fmt.Errorf("could not make Job template: %v", err)
	}

	job, err := jbm.jobHelper.GetModuleJobByKernel(ctx, mod.Name, mod.Namespace, targetKernel, utils.JobTypeBuild, owner)
	if err != nil {
		if !errors.Is(err, utils.ErrNoMatchingJob) {
			return "", fmt.Errorf("error getting the build: %v", err)
		}

		logger.Info("Creating job")
		err = jbm.jobHelper.CreateJob(ctx, jobTemplate)
		if err != nil {
			return "", fmt.Errorf("could not create Job: %v", err)
		}

		return utils.StatusCreated, nil
	}

	changed, err := jbm.jobHelper.IsJobChanged(job, jobTemplate)
	if err != nil {
		return "", fmt.Errorf("could not determine if job has changed: %v", err)
	}

	if changed {
		logger.Info("The module's build spec has been changed, deleting the current job so a new one can be created", "name", job.Name)
		err = jbm.jobHelper.DeleteJob(ctx, job)
		if err != nil {
			logger.Info(utils.WarnString(fmt.Sprintf("failed to delete build job %s: %v", job.Name, err)))
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
