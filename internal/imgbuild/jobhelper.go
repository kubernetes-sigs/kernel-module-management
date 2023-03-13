package imgbuild

import (
	"context"
	"errors"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
)

const JobHashAnnotation = "kmm.node.kubernetes.io/last-hash"

var ErrNoMatchingJob = errors.New("no matching job")

//go:generate mockgen -source=jobhelper.go -package=imgbuild -destination=mock_jobhelper.go

type JobHelper interface {
	IsJobChanged(existingJob *batchv1.Job, newJob *batchv1.Job) (bool, error)
	JobLabels(modName string, targetKernel string, jobType JobType) map[string]string
	GetModuleJobByKernel(ctx context.Context, modName, namespace, targetKernel string, jobType JobType, owner metav1.Object) (*batchv1.Job, error)
	GetModuleJobs(ctx context.Context, modName, namespace string, jobType JobType, owner metav1.Object) ([]batchv1.Job, error)
	DeleteJob(ctx context.Context, job *batchv1.Job) error
	CreateJob(ctx context.Context, jobTemplate *batchv1.Job) error
	GetJobStatus(job *batchv1.Job) (Status, error)
}

type jobHelper struct {
	client client.Client
}

func NewJobHelper(client client.Client) JobHelper {
	return &jobHelper{
		client: client,
	}
}

func (jh *jobHelper) IsJobChanged(existingJob *batchv1.Job, newJob *batchv1.Job) (bool, error) {
	existingAnnotations := existingJob.GetAnnotations()
	newAnnotations := newJob.GetAnnotations()

	if existingAnnotations == nil {
		return false, fmt.Errorf("annotations are not present in the existing job %s", existingJob.Name)
	}

	return existingAnnotations[JobHashAnnotation] != newAnnotations[JobHashAnnotation], nil
}

func (jh *jobHelper) JobLabels(modName string, targetKernel string, jobType JobType) map[string]string {
	return moduleKernelLabels(modName, targetKernel, jobType)
}

func (jh *jobHelper) GetModuleJobByKernel(ctx context.Context, modName, namespace, targetKernel string, jobType JobType, owner metav1.Object) (*batchv1.Job, error) {
	matchLabels := moduleKernelLabels(modName, targetKernel, jobType)
	jobs, err := jh.getJobs(ctx, namespace, matchLabels)
	if err != nil {
		return nil, fmt.Errorf("failed to get module %s, jobs by kernel %s: %v", modName, targetKernel, err)
	}

	moduleOwnedJobs := filterJobsByOwner(jobs, owner)
	numFoundJobs := len(moduleOwnedJobs)
	if numFoundJobs == 0 {
		return nil, ErrNoMatchingJob
	} else if numFoundJobs > 1 {
		return nil, fmt.Errorf("expected 0 or 1 %s job, got %d", jobType, numFoundJobs)
	}

	return &moduleOwnedJobs[0], nil
}

func (jh *jobHelper) GetModuleJobs(ctx context.Context, modName, namespace string, jobType JobType, owner metav1.Object) ([]batchv1.Job, error) {
	matchLabels := moduleLabels(modName, jobType)
	jobs, err := jh.getJobs(ctx, namespace, matchLabels)
	if err != nil {
		return nil, fmt.Errorf("failed to get jobs for module %s, namespace %s: %v", modName, namespace, err)
	}
	moduleOwnedJobs := filterJobsByOwner(jobs, owner)
	return moduleOwnedJobs, nil
}

func (jh *jobHelper) DeleteJob(ctx context.Context, job *batchv1.Job) error {
	opts := []client.DeleteOption{
		client.PropagationPolicy(metav1.DeletePropagationBackground),
	}
	err := jh.client.Delete(ctx, job, opts...)
	if err != nil {
		return err
	}
	return nil
}

func (jh *jobHelper) CreateJob(ctx context.Context, jobTemplate *batchv1.Job) error {
	err := jh.client.Create(ctx, jobTemplate)
	if err != nil {
		return err
	}
	return nil
}

// GetJobStatus returns the status of a Job, whether the latter is in progress or not and
// whether there was an error or not
func (jh *jobHelper) GetJobStatus(job *batchv1.Job) (Status, error) {
	switch {
	case job.Status.Succeeded == 1:
		return StatusCompleted, nil
	case job.Status.Active == 1:
		return StatusInProgress, nil
	case job.Status.Failed == 1:
		return StatusFailed, nil
	default:
		return "", fmt.Errorf("unknown status: %v", job.Status)
	}
}

func (jh *jobHelper) getJobs(ctx context.Context, namespace string, labels map[string]string) ([]batchv1.Job, error) {
	jobList := batchv1.JobList{}
	opts := []client.ListOption{
		client.MatchingLabels(labels),
		client.InNamespace(namespace),
	}
	if err := jh.client.List(ctx, &jobList, opts...); err != nil {
		return nil, fmt.Errorf("could not list jobs: %v", err)
	}

	return jobList.Items, nil
}

func moduleKernelLabels(moduleName, targetKernel string, jobType JobType) map[string]string {
	labels := moduleLabels(moduleName, jobType)
	labels[constants.TargetKernelTarget] = targetKernel
	return labels
}

func moduleLabels(moduleName string, jobType JobType) map[string]string {
	return map[string]string{
		constants.ModuleNameLabel: moduleName,
		constants.JobType:         string(jobType),
	}
}

func filterJobsByOwner(jobs []batchv1.Job, owner metav1.Object) []batchv1.Job {
	ownedJobs := make([]batchv1.Job, 0, len(jobs))

	for _, job := range jobs {
		if metav1.IsControlledBy(&job, owner) {
			ownedJobs = append(ownedJobs, job)
		}
	}

	return ownedJobs
}
