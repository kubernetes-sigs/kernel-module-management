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

var _ = Describe("JobLabels", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
	})

	It("get job labels", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName"},
		}
		mgr := NewJobHelper(clnt)
		labels := mgr.JobLabels(mod, "targetKernel", "jobType")

		Expect(labels).To(HaveKeyWithValue(constants.ModuleNameLabel, "moduleName"))
		Expect(labels).To(HaveKeyWithValue(constants.TargetKernelTarget, "targetKernel"))
		Expect(labels).To(HaveKeyWithValue(constants.JobType, "jobType"))
	})
})

var _ = Describe("GetModuleJobByKernel", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		jh   JobHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		jh = NewJobHelper(clnt)
	})

	It("should return only one job", func() {
		ctx := context.Background()

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName", Namespace: "moduleNamespace"},
		}
		j := batchv1.Job{}

		labels := map[string]string{
			constants.ModuleNameLabel:    "moduleName",
			constants.TargetKernelTarget: "targetKernel",
			constants.JobType:            "jobType",
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace("moduleNamespace"),
		}

		clnt.EXPECT().List(ctx, gomock.Any(), opts).DoAndReturn(
			func(_ interface{}, list *batchv1.JobList, _ ...interface{}) error {
				list.Items = []batchv1.Job{j}
				return nil
			},
		)

		job, err := jh.GetModuleJobByKernel(ctx, mod, "targetKernel", "jobType")

		Expect(job).To(Equal(&j))
		Expect(err).NotTo(HaveOccurred())
	})

	It("failure to fetch jobs", func() {
		ctx := context.Background()
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName", Namespace: "moduleNamespace"},
		}

		labels := map[string]string{
			constants.ModuleNameLabel:    "moduleName",
			constants.TargetKernelTarget: "targetKernel",
			constants.JobType:            "jobType",
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace("moduleNamespace"),
		}
		jobList := batchv1.JobList{}

		clnt.EXPECT().List(ctx, &jobList, opts).Return(errors.New("random error"))

		_, err := jh.GetModuleJobByKernel(ctx, mod, "targetKernel", "jobType")

		Expect(err).To(HaveOccurred())
	})

	It("should fails if more then 1 job exists", func() {
		ctx := context.Background()

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName", Namespace: "moduleNamespace"},
		}

		labels := map[string]string{
			constants.ModuleNameLabel:    "moduleName",
			constants.TargetKernelTarget: "targetKernel",
			constants.JobType:            "jobType",
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace("moduleNamespace"),
		}

		clnt.EXPECT().List(ctx, gomock.Any(), opts).DoAndReturn(
			func(_ interface{}, list *batchv1.JobList, _ ...interface{}) error {
				list.Items = []batchv1.Job{batchv1.Job{}, batchv1.Job{}}
				return nil
			},
		)

		_, err := jh.GetModuleJobByKernel(ctx, mod, "targetKernel", "jobType")

		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("GetModuleJobs", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		jh   JobHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		jh = NewJobHelper(clnt)
	})

	It("return all found jobs", func() {
		ctx := context.Background()

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName", Namespace: "moduleNamespace"},
		}

		labels := map[string]string{
			constants.ModuleNameLabel: "moduleName",
			constants.JobType:         "jobType",
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace("moduleNamespace"),
		}

		clnt.EXPECT().List(ctx, gomock.Any(), opts).DoAndReturn(
			func(_ interface{}, list *batchv1.JobList, _ ...interface{}) error {
				list.Items = []batchv1.Job{batchv1.Job{}, batchv1.Job{}}
				return nil
			},
		)

		jobs, err := jh.GetModuleJobs(ctx, mod, "jobType")

		Expect(err).NotTo(HaveOccurred())
		Expect(len(jobs)).To(Equal(2))
	})

	It("error flow", func() {
		ctx := context.Background()

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName", Namespace: "moduleNamespace"},
		}

		labels := map[string]string{
			constants.ModuleNameLabel: "moduleName",
			constants.JobType:         "jobType",
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace("moduleNamespace"),
		}

		clnt.EXPECT().List(ctx, gomock.Any(), opts).Return(fmt.Errorf("some error"))

		_, err := jh.GetModuleJobs(ctx, mod, "jobType")

		Expect(err).To(HaveOccurred())
	})

	It("zero jobs found", func() {
		ctx := context.Background()

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName", Namespace: "moduleNamespace"},
		}

		labels := map[string]string{
			constants.ModuleNameLabel: "moduleName",
			constants.JobType:         "jobType",
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace("moduleNamespace"),
		}

		clnt.EXPECT().List(ctx, gomock.Any(), opts).DoAndReturn(
			func(_ interface{}, list *batchv1.JobList, _ ...interface{}) error {
				list.Items = []batchv1.Job{}
				return nil
			},
		)

		jobs, err := jh.GetModuleJobs(ctx, mod, "jobType")

		Expect(err).NotTo(HaveOccurred())
		Expect(len(jobs)).To(Equal(0))
	})
})

var _ = Describe("DeleteJob", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		jh   JobHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		jh = NewJobHelper(clnt)
	})

	It("good flow", func() {
		ctx := context.Background()

		j := batchv1.Job{}
		opts := []sigclient.DeleteOption{
			sigclient.PropagationPolicy(metav1.DeletePropagationBackground),
		}
		clnt.EXPECT().Delete(ctx, &j, opts).Return(nil)

		err := jh.DeleteJob(ctx, &j)

		Expect(err).NotTo(HaveOccurred())

	})

	It("error flow", func() {
		ctx := context.Background()

		j := batchv1.Job{}
		opts := []sigclient.DeleteOption{
			sigclient.PropagationPolicy(metav1.DeletePropagationBackground),
		}
		clnt.EXPECT().Delete(ctx, &j, opts).Return(errors.New("random error"))

		err := jh.DeleteJob(ctx, &j)

		Expect(err).To(HaveOccurred())

	})
})

var _ = Describe("CreateJob", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		jh   JobHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		jh = NewJobHelper(clnt)
	})

	It("good flow", func() {
		ctx := context.Background()

		j := batchv1.Job{}
		clnt.EXPECT().Create(ctx, &j).Return(nil)

		err := jh.CreateJob(ctx, &j)

		Expect(err).NotTo(HaveOccurred())

	})

	It("error flow", func() {
		ctx := context.Background()

		j := batchv1.Job{}
		clnt.EXPECT().Create(ctx, &j).Return(errors.New("random error"))

		err := jh.CreateJob(ctx, &j)

		Expect(err).To(HaveOccurred())

	})
})

var _ = Describe("JobStatus", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		jh   JobHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		jh = NewJobHelper(clnt)
	})

	DescribeTable("should return the correct status depending on the job status",
		func(s *batchv1.Job, r string, expectinprogress bool, expectsErr bool) {

			res, inprogress, err := jh.GetJobStatus(s)
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
})

var _ = Describe("IsJobChnaged", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		jh   JobHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		jh = NewJobHelper(clnt)
	})

	DescribeTable("should detect if a job has changed",
		func(annotation map[string]string, expectchanged bool, expectsErr bool) {
			existingJob := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					//Namespace:   namespace,
					Annotations: annotation,
				},
			}
			newJob := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					//Namespace:   namespace,
					Annotations: map[string]string{constants.JobHashAnnotation: "some hash"},
				},
			}
			fmt.Println(existingJob.GetAnnotations())

			changed, err := jh.IsJobChanged(&existingJob, &newJob)

			if expectsErr {
				Expect(err).To(HaveOccurred())
				return
			}
			Expect(expectchanged).To(Equal(changed))
		},

		Entry("should error if job has no annotations", nil, false, true),
		Entry("should return true if job has changed", map[string]string{constants.JobHashAnnotation: "some other hash"}, true, false),
		Entry("should return false is job has not changed ", map[string]string{constants.JobHashAnnotation: "some hash"}, false, false),
	)
})
