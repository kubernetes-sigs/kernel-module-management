package signjob

import (
	"context"
	"errors"
	"fmt"

	"github.com/golang/mock/gomock"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("ShouldSync", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		reg  *registry.MockRegistry
		mgr  *signJobManager
	)

	const (
		moduleName    = "module-name"
		imageName     = "image-name"
		namespace     = "some-namespace"
		kernelVersion = "1.2.3"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		reg = registry.NewMockRegistry(ctrl)
		mgr = NewSignJobManager(clnt, nil, nil, reg)
	})

	It("should return false if there was not sign section", func() {
		ctx := context.Background()

		mod := kmmv1beta1.Module{}
		km := kmmv1beta1.KernelMapping{}

		shouldSync, err := mgr.ShouldSync(ctx, mod, km)

		Expect(err).ToNot(HaveOccurred())
		Expect(shouldSync).To(BeFalse())
	})

	It("should return false if image already exists", func() {
		ctx := context.Background()

		km := kmmv1beta1.KernelMapping{
			Sign:           &kmmv1beta1.Sign{},
			ContainerImage: imageName,
		}

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ModuleSpec{
				ImageRepoSecret: &v1.LocalObjectReference{Name: "pull-push-secret"},
			},
		}

		gomock.InOrder(
			reg.EXPECT().ImageExists(ctx, imageName, gomock.Any(), gomock.Any()).Return(true, nil),
		)

		shouldSync, err := mgr.ShouldSync(ctx, mod, km)

		Expect(err).ToNot(HaveOccurred())
		Expect(shouldSync).To(BeFalse())
	})

	It("should return false and an error if image check fails", func() {
		ctx := context.Background()

		km := kmmv1beta1.KernelMapping{
			Sign:           &kmmv1beta1.Sign{},
			ContainerImage: imageName,
		}

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ModuleSpec{
				ImageRepoSecret: &v1.LocalObjectReference{Name: "pull-push-secret"},
			},
		}

		gomock.InOrder(
			reg.EXPECT().ImageExists(ctx, imageName, gomock.Any(), gomock.Any()).Return(false, errors.New("generic-registry-error")),
		)

		shouldSync, err := mgr.ShouldSync(ctx, mod, km)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("generic-registry-error"))
		Expect(shouldSync).To(BeFalse())
	})

	It("should return true if image does not exist", func() {
		ctx := context.Background()

		km := kmmv1beta1.KernelMapping{
			Sign:           &kmmv1beta1.Sign{},
			ContainerImage: imageName,
		}

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ModuleSpec{
				ImageRepoSecret: &v1.LocalObjectReference{Name: "pull-push-secret"},
			},
		}

		gomock.InOrder(
			reg.EXPECT().ImageExists(ctx, imageName, gomock.Any(), gomock.Any()).Return(false, nil),
		)

		shouldSync, err := mgr.ShouldSync(ctx, mod, km)

		Expect(err).ToNot(HaveOccurred())
		Expect(shouldSync).To(BeTrue())
	})
})

var _ = Describe("Sync", func() {
	var (
		ctrl      *gomock.Controller
		maker     *MockSigner
		jobhelper *utils.MockJobHelper
		mgr       *signJobManager
	)

	const (
		imageName         = "image-name"
		previousImageName = "previous-image"
		namespace         = "some-namespace"

		moduleName    = "module-name"
		kernelVersion = "1.2.3"
		jobName       = "some-job"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		maker = NewMockSigner(ctrl)
		jobhelper = utils.NewMockJobHelper(ctrl)
		mgr = NewSignJobManager(nil, maker, jobhelper, nil)
	})

	km := kmmv1beta1.KernelMapping{
		Build:          &kmmv1beta1.Build{},
		ContainerImage: imageName,
	}
	labels := map[string]string{"kmm.node.kubernetes.io/job-type": "sign",
		"kmm.node.kubernetes.io/module.name":   moduleName,
		"kmm.node.kubernetes.io/target-kernel": kernelVersion,
	}

	mod := kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: moduleName},
	}

	DescribeTable("should return the correct status depending on the job status",
		func(s batchv1.JobStatus, r utils.Result, expectsErr bool) {
			j := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"kmm.node.kubernetes.io/job-type": "sign",
						"kmm.node.kubernetes.io/module.name":   moduleName,
						"kmm.node.kubernetes.io/target-kernel": kernelVersion,
					},
					Namespace:   namespace,
					Annotations: map[string]string{constants.JobHashAnnotation: "some hash"},
				},
				Status: s,
			}
			newJob := batchv1.Job{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"kmm.node.kubernetes.io/job-type": "sign",
						"kmm.node.kubernetes.io/module.name":   moduleName,
						"kmm.node.kubernetes.io/target-kernel": kernelVersion,
					},
					Namespace:   namespace,
					Annotations: map[string]string{constants.JobHashAnnotation: "some hash"},
				},
				Status: s,
			}
			ctx := context.Background()

			var joberr error
			if expectsErr {
				joberr = errors.New("random error")
			}

			gomock.InOrder(
				jobhelper.EXPECT().JobLabels(mod.Name, kernelVersion, "sign").Return(labels),
				maker.EXPECT().MakeJobTemplate(ctx, mod, km, kernelVersion, labels, previousImageName, true, &mod).Return(&j, nil),
				jobhelper.EXPECT().GetModuleJobByKernel(ctx, mod.Name, mod.Namespace, kernelVersion, utils.JobTypeSign, &mod).Return(&newJob, nil),
				jobhelper.EXPECT().IsJobChanged(&j, &newJob).Return(false, nil),
				jobhelper.EXPECT().GetJobStatus(&newJob).Return(r.Status, r.Requeue, joberr),
			)

			res, err := mgr.Sync(ctx, mod, km, kernelVersion, previousImageName, true, &mod)

			if expectsErr {
				Expect(err).To(HaveOccurred())
				return
			}

			Expect(res).To(Equal(r))
		},
		Entry("active", batchv1.JobStatus{Active: 1}, utils.Result{Requeue: true, Status: utils.StatusInProgress}, false),
		Entry("active", batchv1.JobStatus{Active: 1}, utils.Result{Requeue: true, Status: utils.StatusInProgress}, false),
		Entry("succeeded", batchv1.JobStatus{Succeeded: 1}, utils.Result{Status: utils.StatusCompleted}, false),
		Entry("failed", batchv1.JobStatus{Failed: 1}, utils.Result{}, true),
	)

	It("should return an error if there was an error creating the job template", func() {
		ctx := context.Background()

		gomock.InOrder(
			jobhelper.EXPECT().JobLabels(mod.Name, kernelVersion, "sign").Return(labels),
			maker.EXPECT().MakeJobTemplate(ctx, mod, km, kernelVersion, labels, previousImageName, true, &mod).
				Return(nil, errors.New("random error")),
		)

		Expect(
			mgr.Sync(ctx, mod, km, kernelVersion, previousImageName, true, &mod),
		).Error().To(
			HaveOccurred(),
		)
	})

	It("should return an error if there was an error getting running jobs", func() {
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
			jobhelper.EXPECT().JobLabels(mod.Name, kernelVersion, "sign").Return(labels),
			maker.EXPECT().MakeJobTemplate(ctx, mod, km, kernelVersion, labels, previousImageName, true, &mod).Return(&j, nil),
			jobhelper.EXPECT().GetModuleJobByKernel(ctx, mod.Name, mod.Namespace, kernelVersion, utils.JobTypeSign, &mod).Return(nil, errors.New("random error")),
		)

		Expect(
			mgr.Sync(ctx, mod, km, kernelVersion, previousImageName, true, &mod),
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
			jobhelper.EXPECT().JobLabels(mod.Name, kernelVersion, "sign").Return(labels),
			maker.EXPECT().MakeJobTemplate(ctx, mod, km, kernelVersion, labels, previousImageName, true, &mod).Return(&j, nil),
			jobhelper.EXPECT().GetModuleJobByKernel(ctx, mod.Name, mod.Namespace, kernelVersion, utils.JobTypeSign, &mod).Return(nil, utils.ErrNoMatchingJob),
			jobhelper.EXPECT().CreateJob(ctx, &j).Return(errors.New("unable to create job")),
		)

		Expect(
			mgr.Sync(ctx, mod, km, kernelVersion, previousImageName, true, &mod),
		).Error().To(
			HaveOccurred(),
		)
	})

	It("should create the job and return without error if it doesn't exist", func() {
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
			jobhelper.EXPECT().JobLabels(mod.Name, kernelVersion, "sign").Return(labels),
			maker.EXPECT().MakeJobTemplate(ctx, mod, km, kernelVersion, labels, previousImageName, true, &mod).Return(&j, nil),
			jobhelper.EXPECT().GetModuleJobByKernel(ctx, mod.Name, mod.Namespace, kernelVersion, utils.JobTypeSign, &mod).Return(nil, utils.ErrNoMatchingJob),
			jobhelper.EXPECT().CreateJob(ctx, &j).Return(nil),
		)

		Expect(
			mgr.Sync(ctx, mod, km, kernelVersion, previousImageName, true, &mod),
		).To(
			Equal(utils.Result{Requeue: true, Status: utils.StatusCreated}),
		)
	})

	It("should delete the job if it was edited", func() {
		ctx := context.Background()

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
			jobhelper.EXPECT().JobLabels(mod.Name, kernelVersion, "sign").Return(labels),
			maker.EXPECT().MakeJobTemplate(ctx, mod, km, kernelVersion, labels, previousImageName, true, &mod).Return(&newJob, nil),
			jobhelper.EXPECT().GetModuleJobByKernel(ctx, mod.Name, mod.Namespace, kernelVersion, utils.JobTypeSign, &mod).Return(&newJob, nil),
			jobhelper.EXPECT().IsJobChanged(&newJob, &newJob).Return(true, nil),
			jobhelper.EXPECT().DeleteJob(ctx, &newJob).Return(nil),
		)

		Expect(
			mgr.Sync(ctx, mod, km, kernelVersion, previousImageName, true, &mod),
		).To(
			Equal(utils.Result{Requeue: true, Status: utils.StatusInProgress}),
		)
	})
})

var _ = Describe("GarbageCollect", func() {
	var (
		ctrl      *gomock.Controller
		clnt      *client.MockClient
		jobhelper *utils.MockJobHelper
		mgr       *signJobManager
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		jobhelper = utils.NewMockJobHelper(ctrl)
		mgr = NewSignJobManager(clnt, nil, jobhelper, nil)
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

			jobhelper.EXPECT().GetModuleJobs(context.Background(), mod.Name, mod.Namespace, utils.JobTypeSign, &mod).Return([]batchv1.Job{job1, job2}, returnedError)
			if !expectsErr {
				if job1.Status.Succeeded == 1 {
					jobhelper.EXPECT().DeleteJob(context.Background(), &job1).Return(nil)
				}
				if job2.Status.Succeeded == 1 {
					jobhelper.EXPECT().DeleteJob(context.Background(), &job2).Return(nil)
				}
			}

			names, err := mgr.GarbageCollect(context.Background(), mod.Name, mod.Namespace, &mod)

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
