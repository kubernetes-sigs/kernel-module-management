package job

import (
	"context"
	"errors"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	batchv1 "k8s.io/api/batch/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

const jobHashAnnotation = "kmm.node.kubernetes.io/last-hash"

var errNoMatchingBuild = errors.New("no matching build")

type jobManager struct {
	client client.Client
	maker  Maker
	helper build.Helper
}

func NewBuildManager(client client.Client, maker Maker, helper build.Helper) *jobManager {
	return &jobManager{
		client: client,
		maker:  maker,
		helper: helper,
	}
}

func labels(mod kmmv1beta1.Module, targetKernel string) map[string]string {
	return map[string]string{
		constants.ModuleNameLabel:    mod.Name,
		constants.TargetKernelTarget: targetKernel,
	}
}

func (jbm *jobManager) getJob(ctx context.Context, mod kmmv1beta1.Module, targetKernel string) (*batchv1.Job, error) {
	jobList := batchv1.JobList{}

	opts := []client.ListOption{
		client.MatchingLabels(labels(mod, targetKernel)),
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

	jobTemplate, err := jbm.maker.MakeJobTemplate(mod, buildConfig, targetKernel, m.ContainerImage, pushImage)
	if err != nil {
		return build.Result{}, fmt.Errorf("could not make Job template: %v", err)
	}

	job, err := jbm.getJob(ctx, mod, targetKernel)
	if err != nil {
		if !errors.Is(err, errNoMatchingBuild) {
			return build.Result{}, fmt.Errorf("error getting the build: %v", err)
		}

		logger.Info("Creating job")

		if err = jbm.client.Create(ctx, jobTemplate); err != nil {
			return build.Result{}, fmt.Errorf("could not create Job: %v", err)
		}

		return build.Result{Status: build.StatusCreated, Requeue: true}, nil
	}

	changed, err := jbm.isJobChanged(job, jobTemplate)
	if err != nil {
		return build.Result{}, fmt.Errorf("could not determine if job has changed: %v", err)
	}

	if changed {
		logger.Info("The module's build spec has been changed, deleting the current job so a new one can be created", "name", job.Name)
		err = jbm.client.Delete(ctx, job)
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

func (jbm *jobManager) isJobChanged(existingJob *batchv1.Job, newJob *batchv1.Job) (bool, error) {
	existingAnnotations := existingJob.GetAnnotations()
	newAnnotations := newJob.GetAnnotations()
	if existingAnnotations == nil {
		return false, fmt.Errorf("annotations are not present in the existing job %s", existingJob.Name)
	}
	if existingAnnotations[jobHashAnnotation] == newAnnotations[jobHashAnnotation] {
		return false, nil
	}
	return true, nil
}
