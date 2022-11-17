package signjob

import (
	"context"
	"errors"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type signJobManager struct {
	signer    Signer
	helper    sign.Helper
	jobHelper utils.JobHelper
}

func NewSignJobManager(signer Signer, helper sign.Helper, jobHelper utils.JobHelper) *signJobManager {
	return &signJobManager{
		signer:    signer,
		helper:    helper,
		jobHelper: jobHelper,
	}
}

func (jbm *signJobManager) Sync(ctx context.Context, mod kmmv1beta1.Module, m kmmv1beta1.KernelMapping, targetKernel string, imageToSign string, targetImage string, pushImage bool) (utils.Result, error) {
	logger := log.FromContext(ctx)

	logger.Info("Signing in-cluster")

	signConfig := jbm.helper.GetRelevantSign(mod, m)
	jobLabels := jbm.jobHelper.JobLabels(mod, targetKernel, "sign")

	jobTemplate, err := jbm.signer.MakeJobTemplate(mod, signConfig, targetKernel, imageToSign, targetImage, jobLabels, pushImage)
	if err != nil {
		return utils.Result{}, fmt.Errorf("could not make Job template: %v", err)
	}

	job, err := jbm.jobHelper.GetModuleJobByKernel(ctx, mod, targetKernel, utils.JobTypeSign)
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
