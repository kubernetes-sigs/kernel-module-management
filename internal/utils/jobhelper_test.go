package utils

import (
	"context"
	"errors"
	"fmt"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/golang/mock/gomock"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	sigclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("Labels", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
	)

	const (
		moduleName   = "module-name"
		targetKernel = "1.2.3"
		jobType      = "sign"
		namespace    = "mynamespace"
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
	})

	It("should set labels correctly", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: moduleName},
		}

		mgr := NewJobHelper(clnt)
		labels := mgr.JobLabels(mod, targetKernel, jobType)

		Expect(labels).To(HaveKeyWithValue(constants.ModuleNameLabel, moduleName))
		Expect(labels).To(HaveKeyWithValue(constants.TargetKernelTarget, targetKernel))
		Expect(labels).To(HaveKeyWithValue(constants.JobType, jobType))
	})

	It("GetJob should return something if it finds a job", func() {
		ctx := context.Background()

		mgr := NewJobHelper(clnt)

		labels := map[string]string{
			constants.ModuleNameLabel:    moduleName,
			constants.TargetKernelTarget: targetKernel,
			constants.JobType:            jobType,
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace(namespace),
		}
		jobList := batchv1.JobList{}

		gomock.InOrder(
			clnt.EXPECT().List(ctx, &jobList, opts).Return(nil),
		)

		job, err := mgr.GetJob(ctx, namespace, jobType, labels)

		Expect(job).NotTo(Equal(nil))
		Expect(err).NotTo(HaveOccurred())
	})

	It("GetJob return an error if list does", func() {
		ctx := context.Background()

		mgr := NewJobHelper(clnt)

		labels := map[string]string{
			constants.ModuleNameLabel:    moduleName,
			constants.TargetKernelTarget: targetKernel,
			constants.JobType:            jobType,
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace(namespace),
		}
		jobList := batchv1.JobList{}

		gomock.InOrder(
			clnt.EXPECT().List(ctx, &jobList, opts).Return(errors.New("random error")),
		)

		_, err := mgr.GetJob(ctx, namespace, jobType, labels)

		Expect(err).To(HaveOccurred())
	})

	It("should delete jobs", func() {
		ctx := context.Background()

		j := batchv1.Job{}
		opts := []sigclient.DeleteOption{
			sigclient.PropagationPolicy(metav1.DeletePropagationBackground),
		}
		gomock.InOrder(
			clnt.EXPECT().Delete(ctx, &j, opts).Return(nil),
		)

		mgr := NewJobHelper(clnt)
		err := mgr.DeleteJob(ctx, &j)

		Expect(err).NotTo(HaveOccurred())

	})

	It("delete should pass errors through", func() {
		ctx := context.Background()

		j := batchv1.Job{}
		opts := []sigclient.DeleteOption{
			sigclient.PropagationPolicy(metav1.DeletePropagationBackground),
		}
		gomock.InOrder(
			clnt.EXPECT().Delete(ctx, &j, opts).Return(errors.New("random error")),
		)

		mgr := NewJobHelper(clnt)
		err := mgr.DeleteJob(ctx, &j)

		Expect(err).To(HaveOccurred())

	})

	It("should create jobs", func() {
		ctx := context.Background()

		j := batchv1.Job{}
		gomock.InOrder(
			clnt.EXPECT().Create(ctx, &j).Return(nil),
		)

		mgr := NewJobHelper(clnt)
		err := mgr.CreateJob(ctx, &j)

		Expect(err).NotTo(HaveOccurred())

	})

	It("create should pass errors though", func() {
		ctx := context.Background()

		j := batchv1.Job{}
		gomock.InOrder(
			clnt.EXPECT().Create(ctx, &j).Return(errors.New("random error")),
		)
		mgr := NewJobHelper(clnt)
		err := mgr.CreateJob(ctx, &j)

		Expect(err).To(HaveOccurred())

	})

	DescribeTable("should return the correct status depending on the job status",
		func(s *batchv1.Job, r string, expectinprogress bool, expectsErr bool) {

			mgr := NewJobHelper(clnt)
			res, inprogress, err := mgr.GetJobStatus(s)
			if expectsErr {
				Expect(err).To(HaveOccurred())
				return
			}

			Expect(string(res)).To(Equal(r))
			Expect(inprogress).To(Equal(expectinprogress))
		},
		Entry("succeeded", &batchv1.Job{Status: batchv1.JobStatus{Succeeded: 1}}, StatusCompleted, false, false),
		Entry("in progress", &batchv1.Job{Status: batchv1.JobStatus{Active: 1}}, StatusInProgress, true, false),
		Entry("Failed", &batchv1.Job{Status: batchv1.JobStatus{Failed: 1}}, StatusFailed, false, true),
		Entry("unknown", &batchv1.Job{Status: batchv1.JobStatus{Failed: 2}}, StatusFailed, false, true),
	)

	DescribeTable("should detect if a job has changed",
		func(annotation map[string]string, expectchanged bool, expectsErr bool) {
			existingJob := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   namespace,
					Annotations: annotation,
				},
			}
			newJob := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   namespace,
					Annotations: map[string]string{jobHashAnnotation: "some hash"},
				},
			}
			fmt.Println(existingJob.GetAnnotations())

			mgr := NewJobHelper(clnt)
			changed, err := mgr.IsJobChanged(&existingJob, &newJob)

			if expectsErr {
				Expect(err).To(HaveOccurred())
				return
			}
			Expect(expectchanged).To(Equal(changed))
		},

		Entry("should error if job has no annotations", nil, false, true),
		Entry("should return true if job has changed", map[string]string{jobHashAnnotation: "some other hash"}, true, false),
		Entry("should return false is job has not changed ", map[string]string{jobHashAnnotation: "some hash"}, false, false),
	)
})
