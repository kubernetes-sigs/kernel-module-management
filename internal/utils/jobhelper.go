package utils

//go:generate mockgen -source=jobhelper.go -package=utils -destination=mock_jobhelper.go

import (
	"context"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Status string

const (
	jobHashAnnotation = "kmm.node.kubernetes.io/last-hash"

	StatusCompleted  = "completed"
	StatusCreated    = "created"
	StatusInProgress = "in progress"
	StatusFailed     = "Failed"
)

type Result struct {
	Requeue bool
	Status  Status
}

type JobHelper interface {
	IsJobChanged(existingJob *batchv1.Job, newJob *batchv1.Job) (bool, error)
	JobLabels(mod kmmv1beta1.Module, targetKernel string, jobType string) map[string]string
	GetJob(ctx context.Context, namespace string, jobType string, labels map[string]string) (*batchv1.Job, error)
	DeleteJob(ctx context.Context, job *batchv1.Job) error
	CreateJob(ctx context.Context, jobTemplate *batchv1.Job) error
	GetJobStatus(job *batchv1.Job) (Status, bool, error)
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
	if existingAnnotations[jobHashAnnotation] == newAnnotations[jobHashAnnotation] {
		return false, nil
	}
	return true, nil
}

func (jh *jobHelper) JobLabels(mod kmmv1beta1.Module, targetKernel string, jobType string) map[string]string {
	return map[string]string{
		constants.ModuleNameLabel:    mod.Name,
		constants.TargetKernelTarget: targetKernel,
		constants.JobType:            jobType,
	}
}

func (jh *jobHelper) GetJob(ctx context.Context, namespace string, jobType string, labels map[string]string) (*batchv1.Job, error) {
	jobList := batchv1.JobList{}

	opts := []client.ListOption{
		client.MatchingLabels(labels),
		client.InNamespace(namespace),
	}

	if err := jh.client.List(ctx, &jobList, opts...); err != nil {
		return nil, fmt.Errorf("could not list jobs: %v", err)
	}

	if n := len(jobList.Items); n == 0 {
		return nil, nil
	} else if n > 1 {
		return nil, fmt.Errorf("expected 0 or 1 %s job, got %d", jobType, n)
	}

	return &jobList.Items[0], nil
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

/* get the status of a job
** returns:
**	status - string representation of the status
**	inprogress - bool, is the job still in progress?
**	error - an error reporting failure state
 */
func (jh *jobHelper) GetJobStatus(job *batchv1.Job) (Status, bool, error) {
	switch {
	case job.Status.Succeeded == 1:
		return StatusCompleted, false, nil
	case job.Status.Active == 1:
		return StatusInProgress, true, nil
	case job.Status.Failed == 1:
		return StatusFailed, false, fmt.Errorf("job failed")
	default:
		return StatusFailed, false, fmt.Errorf("unknown status: %v", job.Status)
	}
}
