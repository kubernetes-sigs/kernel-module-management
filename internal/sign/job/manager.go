package signjob

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

func (jbm *signJobManager) ShouldSync(
	ctx context.Context,
	mod kmmv1beta1.Module,
	m kmmv1beta1.KernelMapping) (bool, error) {

	// if there is no sign specified skip
	if !module.ShouldBeSigned(mod.Spec, m) {
		return false, nil
	}

	exists, err := module.ImageExists(ctx, jbm.client, jbm.registry, mod.Spec, mod.Namespace, m, m.ContainerImage)
	if err != nil {
		return false, fmt.Errorf("failed to check existence of image %s: %w", m.ContainerImage, err)
	}

	return !exists, nil
}

func (jbm *signJobManager) Sync(
	ctx context.Context,
	mod kmmv1beta1.Module,
	m kmmv1beta1.KernelMapping,
	targetKernel string,
	imageToSign string,
	pushImage bool,
	owner metav1.Object) (utils.Result, error) {

	logger := log.FromContext(ctx)

	logger.Info("Signing in-cluster")

	labels := jbm.jobHelper.JobLabels(mod.Name, targetKernel, "sign")

	jobTemplate, err := jbm.signer.MakeJobTemplate(ctx, mod, m, targetKernel, labels, imageToSign, pushImage, owner)
	if err != nil {
		return utils.Result{}, fmt.Errorf("could not make Job template: %v", err)
	}

	job, err := jbm.jobHelper.GetModuleJobByKernel(ctx, mod.Name, mod.Namespace, targetKernel, utils.JobTypeSign, owner)
	if err != nil {
		if !errors.Is(err, utils.ErrNoMatchingJob) {
			return utils.Result{}, fmt.Errorf("error getting the signing job: %v", err)
		}

		logger.Info("Creating job")
		err = jbm.jobHelper.CreateJob(ctx, jobTemplate)
		if err != nil {
			return utils.Result{}, fmt.Errorf("could not create Signing Job: %v", err)
		}

		return utils.Result{Status: utils.StatusCreated, Requeue: true}, nil
	}
	// default, there are no errors, and there is a job, check if it has changed
	changed, err := jbm.jobHelper.IsJobChanged(job, jobTemplate)
	if err != nil {
		return utils.Result{}, fmt.Errorf("could not determine if job has changed: %v", err)
	}

	if changed {
		logger.Info("The module's sign spec has been changed, deleting the current job so a new one can be created", "name", job.Name)
		err = jbm.jobHelper.DeleteJob(ctx, job)
		if err != nil {
			logger.Info(utils.WarnString(fmt.Sprintf("failed to delete signing job %s: %v", job.Name, err)))
		}
		return utils.Result{Status: utils.StatusInProgress, Requeue: true}, nil
	}

	logger.Info("Returning job status", "name", job.Name, "namespace", job.Namespace)

	statusmsg, inprogress, err := jbm.jobHelper.GetJobStatus(job)
	if err != nil {
		return utils.Result{}, err
	}

	return utils.Result{Status: statusmsg, Requeue: inprogress}, nil
}
