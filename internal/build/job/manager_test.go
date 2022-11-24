package job

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang/mock/gomock"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Sync", func() {

	var (
		ctrl      *gomock.Controller
		clnt      *client.MockClient
		maker     *MockMaker
		helper    *build.MockHelper
		jobhelper *utils.MockJobHelper
	)

	const (
		imageName = "image-name"
		namespace = "some-namespace"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		maker = NewMockMaker(ctrl)
		helper = build.NewMockHelper(ctrl)
		jobhelper = utils.NewMockJobHelper(ctrl)
	})

	km := kmmv1beta1.KernelMapping{
		Build:          &kmmv1beta1.Build{},
		ContainerImage: imageName,
	}

	const (
		moduleName    = "module-name"
		kernelVersion = "1.2.3"
		jobName       = "some-job"
	)

	mod := kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: moduleName},
	}

	DescribeTable("should return the correct status depending on the job status",
		func(s batchv1.JobStatus, r build.Result, expectsErr bool) {
			j := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"label key": "some label"},
					Namespace:   namespace,
					Annotations: map[string]string{constants.JobHashAnnotation: "some hash"},
				},
				Status: s,
			}
			ctx := context.Background()

			gomock.InOrder(
				helper.EXPECT().GetRelevantBuild(mod, km).Return(km.Build),
				maker.EXPECT().MakeJobTemplate(ctx, mod, km.Build, kernelVersion, km.ContainerImage, true, nil).Return(&j, nil),
				jobhelper.EXPECT().GetModuleJobByKernel(ctx, mod, kernelVersion, utils.JobTypeBuild).Return(&j, nil),
				jobhelper.EXPECT().IsJobChanged(&j, &j).Return(false, nil),
			)

			mgr := NewBuildManager(clnt, maker, helper, jobhelper)

			res, err := mgr.Sync(ctx, mod, km, kernelVersion, km.ContainerImage, true)

			if expectsErr {
				Expect(err).To(HaveOccurred())
				return
			}

			Expect(res).To(Equal(r))
		},
		Entry("active", batchv1.JobStatus{Active: 1}, build.Result{Requeue: true, Status: build.StatusInProgress}, false),
		Entry("succeeded", batchv1.JobStatus{Succeeded: 1}, build.Result{Status: build.StatusCompleted}, false),
		Entry("failed", batchv1.JobStatus{Failed: 1}, build.Result{}, true),
	)

	It("should return an error if there was an error creating the job template", func() {
		ctx := context.Background()

		gomock.InOrder(
			helper.EXPECT().GetRelevantBuild(mod, km).Return(km.Build),
			maker.EXPECT().MakeJobTemplate(ctx, mod, km.Build, kernelVersion, km.ContainerImage, true, nil).Return(nil, errors.New("random error")),
		)

		mgr := NewBuildManager(clnt, maker, helper, jobhelper)

		Expect(
			mgr.Sync(ctx, mod, km, kernelVersion, km.ContainerImage, true),
		).Error().To(
			HaveOccurred(),
		)
	})

	It("should return an error if there was an error creating the job", func() {
		ctx := context.Background()
		j := batchv1.Job{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "batch/v1",
				Kind:       "Job",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: namespace,
			},
		}

		gomock.InOrder(
			helper.EXPECT().GetRelevantBuild(mod, km).Return(km.Build),
			maker.EXPECT().MakeJobTemplate(ctx, mod, km.Build, kernelVersion, km.ContainerImage, true, nil).Return(&j, nil),
			jobhelper.EXPECT().GetModuleJobByKernel(ctx, mod, kernelVersion, utils.JobTypeBuild).Return(nil, utils.ErrNoMatchingJob),
			jobhelper.EXPECT().CreateJob(ctx, &j).Return(errors.New("some error")),
		)

		mgr := NewBuildManager(clnt, maker, helper, jobhelper)

		Expect(
			mgr.Sync(ctx, mod, km, kernelVersion, km.ContainerImage, true),
		).Error().To(
			HaveOccurred(),
		)
	})

	It("should create the job if there was no error making it", func() {
		ctx := context.Background()

		j := batchv1.Job{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "batch/v1",
				Kind:       "Job",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      jobName,
				Namespace: namespace,
			},
		}

		gomock.InOrder(
			helper.EXPECT().GetRelevantBuild(mod, km).Return(km.Build),
			maker.EXPECT().MakeJobTemplate(ctx, mod, km.Build, kernelVersion, km.ContainerImage, true, nil).Return(&j, nil),
			jobhelper.EXPECT().GetModuleJobByKernel(ctx, mod, kernelVersion, utils.JobTypeBuild).Return(nil, utils.ErrNoMatchingJob),
			jobhelper.EXPECT().CreateJob(ctx, &j).Return(nil),
		)

		mgr := NewBuildManager(clnt, maker, helper, jobhelper)

		Expect(
			mgr.Sync(ctx, mod, km, kernelVersion, km.ContainerImage, true),
		).To(
			Equal(build.Result{Requeue: true, Status: build.StatusCreated}),
		)
	})

	It("should delete the job if it was edited", func() {
		ctx := context.Background()

		j := batchv1.Job{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "batch/v1",
				Kind:       "Job",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        jobName,
				Namespace:   namespace,
				Annotations: map[string]string{constants.JobHashAnnotation: "some hash"},
			},
		}

		newJob := batchv1.Job{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "batch/v1",
				Kind:       "Job",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        jobName,
				Namespace:   namespace,
				Annotations: map[string]string{constants.JobHashAnnotation: "new hash"},
			},
		}

		gomock.InOrder(
			helper.EXPECT().GetRelevantBuild(mod, km).Return(km.Build),
			maker.EXPECT().MakeJobTemplate(ctx, mod, km.Build, kernelVersion, km.ContainerImage, true, nil).Return(&newJob, nil),
			jobhelper.EXPECT().GetModuleJobByKernel(ctx, mod, kernelVersion, utils.JobTypeBuild).Return(&j, nil),
			jobhelper.EXPECT().IsJobChanged(&j, &newJob).Return(true, nil),
			jobhelper.EXPECT().DeleteJob(ctx, &j).Return(nil),
		)

		mgr := NewBuildManager(clnt, maker, helper, jobhelper)

		Expect(
			mgr.Sync(ctx, mod, km, kernelVersion, km.ContainerImage, true),
		).To(
			Equal(build.Result{Requeue: true, Status: build.StatusInProgress}),
		)
	})
})

var _ = Describe("GarbageCollect", func() {

	var (
		ctrl      *gomock.Controller
		clnt      *client.MockClient
		maker     *MockMaker
		helper    *build.MockHelper
		jobhelper *utils.MockJobHelper
		mgr       *jobManager
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		maker = NewMockMaker(ctrl)
		helper = build.NewMockHelper(ctrl)
		jobhelper = utils.NewMockJobHelper(ctrl)
		mgr = NewBuildManager(clnt, maker, helper, jobhelper)
	})

	mod := kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: "moduleName"},
	}

	DescribeTable("should return the correct error and names of the collected jobs",
		func(jobStatus1 batchv1.JobStatus, jobStatus2 batchv1.JobStatus, expectsErr bool) {
			job1 := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "jobName1",
				},
				Status: jobStatus1,
			}
			job2 := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Name: "jobName2",
				},
				Status: jobStatus2,
			}
			expectedNames := []string{}
			if !expectsErr {
				if job1.Status.Succeeded == 1 {
					expectedNames = append(expectedNames, "jobName1")
				}
				if job2.Status.Succeeded == 1 {
					expectedNames = append(expectedNames, "jobName2")
				}
			}
			returnedError := fmt.Errorf("some error")
			if !expectsErr {
				returnedError = nil
			}

			jobhelper.EXPECT().GetModuleJobs(context.Background(), mod, utils.JobTypeBuild).Return([]batchv1.Job{job1, job2}, returnedError)
			if !expectsErr {
				if job1.Status.Succeeded == 1 {
					jobhelper.EXPECT().DeleteJob(context.Background(), &job1).Return(nil)
				}
				if job2.Status.Succeeded == 1 {
					jobhelper.EXPECT().DeleteJob(context.Background(), &job2).Return(nil)
				}
			}

			names, err := mgr.GarbageCollect(context.Background(), mod)

			if expectsErr {
				Expect(err).To(HaveOccurred())
				Expect(names).To(BeNil())
			} else {
				Expect(err).NotTo(HaveOccurred())
				Expect(expectedNames).To(Equal(names))
			}
		},
		Entry("all jobs succeeded", batchv1.JobStatus{Succeeded: 1}, batchv1.JobStatus{Succeeded: 1}, false),
		Entry("1 job succeeded", batchv1.JobStatus{Succeeded: 1}, batchv1.JobStatus{Succeeded: 0}, false),
		Entry("0 job succeeded", batchv1.JobStatus{Succeeded: 0}, batchv1.JobStatus{Succeeded: 0}, false),
		Entry("error occured", batchv1.JobStatus{Succeeded: 0}, batchv1.JobStatus{Succeeded: 0}, true),
	)
})
