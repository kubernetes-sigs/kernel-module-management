package signjob

import (
	"context"
	"errors"

	"github.com/golang/mock/gomock"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("JobManager", func() {
	Describe("Sync", func() {

		var (
			ctrl      *gomock.Controller
			maker     *MockSigner
			helper    *sign.MockHelper
			jobhelper *utils.MockJobHelper
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
			helper = sign.NewMockHelper(ctrl)
			jobhelper = utils.NewMockJobHelper(ctrl)
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
					helper.EXPECT().GetRelevantSign(mod, km).Return(km.Sign),

					jobhelper.EXPECT().JobLabels(mod, kernelVersion, "sign").Return(labels),
					maker.EXPECT().MakeJobTemplate(mod, km.Sign, kernelVersion, previousImageName, km.ContainerImage, labels, true).Return(&j, nil),
					jobhelper.EXPECT().GetModuleJobByKernel(ctx, mod, kernelVersion, utils.JobTypeSign).Return(&newJob, nil),
					jobhelper.EXPECT().IsJobChanged(&j, &newJob).Return(false, nil),
					jobhelper.EXPECT().GetJobStatus(&newJob).Return(r.Status, r.Requeue, joberr),
				)

				mgr := NewSignJobManager(maker, helper, jobhelper)

				res, err := mgr.Sync(ctx, mod, km, kernelVersion, previousImageName, imageName, true)

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
				helper.EXPECT().GetRelevantSign(mod, km).Return(km.Sign),
				jobhelper.EXPECT().JobLabels(mod, kernelVersion, "sign").Return(labels),
				maker.EXPECT().MakeJobTemplate(mod, km.Sign, kernelVersion, previousImageName, km.ContainerImage, labels, true).Return(nil, errors.New("random error")),
			)

			mgr := NewSignJobManager(maker, helper, jobhelper)

			Expect(
				mgr.Sync(ctx, mod, km, kernelVersion, previousImageName, imageName, true),
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
				helper.EXPECT().GetRelevantSign(mod, km).Return(km.Sign),
				jobhelper.EXPECT().JobLabels(mod, kernelVersion, "sign").Return(labels),
				maker.EXPECT().MakeJobTemplate(mod, km.Sign, kernelVersion, previousImageName, km.ContainerImage, labels, true).Return(&j, nil),
				jobhelper.EXPECT().GetModuleJobByKernel(ctx, mod, kernelVersion, utils.JobTypeSign).Return(nil, errors.New("random error")),
			)

			mgr := NewSignJobManager(maker, helper, jobhelper)

			Expect(
				mgr.Sync(ctx, mod, km, kernelVersion, previousImageName, imageName, true),
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
				helper.EXPECT().GetRelevantSign(mod, km).Return(km.Sign),
				jobhelper.EXPECT().JobLabels(mod, kernelVersion, "sign").Return(labels),
				maker.EXPECT().MakeJobTemplate(mod, km.Sign, kernelVersion, previousImageName, km.ContainerImage, labels, true).Return(&j, nil),
				jobhelper.EXPECT().GetModuleJobByKernel(ctx, mod, kernelVersion, utils.JobTypeSign).Return(nil, utils.ErrNoMatchingJob),
				jobhelper.EXPECT().CreateJob(ctx, &j).Return(errors.New("unable to create job")),
			)

			mgr := NewSignJobManager(maker, helper, jobhelper)

			Expect(
				mgr.Sync(ctx, mod, km, kernelVersion, previousImageName, imageName, true),
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
				helper.EXPECT().GetRelevantSign(mod, km).Return(km.Sign),
				jobhelper.EXPECT().JobLabels(mod, kernelVersion, "sign").Return(labels),
				maker.EXPECT().MakeJobTemplate(mod, km.Sign, kernelVersion, previousImageName, km.ContainerImage, labels, true).Return(&j, nil),
				jobhelper.EXPECT().GetModuleJobByKernel(ctx, mod, kernelVersion, utils.JobTypeSign).Return(nil, utils.ErrNoMatchingJob),
				jobhelper.EXPECT().CreateJob(ctx, &j).Return(nil),
			)

			mgr := NewSignJobManager(maker, helper, jobhelper)

			Expect(
				mgr.Sync(ctx, mod, km, kernelVersion, previousImageName, imageName, true),
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
				helper.EXPECT().GetRelevantSign(mod, km).Return(km.Sign),
				jobhelper.EXPECT().JobLabels(mod, kernelVersion, "sign").Return(labels),
				maker.EXPECT().MakeJobTemplate(mod, km.Sign, kernelVersion, previousImageName, km.ContainerImage, labels, true).Return(&newJob, nil),
				jobhelper.EXPECT().GetModuleJobByKernel(ctx, mod, kernelVersion, utils.JobTypeSign).Return(&newJob, nil),
				jobhelper.EXPECT().IsJobChanged(&newJob, &newJob).Return(true, nil),
				jobhelper.EXPECT().DeleteJob(ctx, &newJob).Return(nil),
			)

			mgr := NewSignJobManager(maker, helper, jobhelper)

			Expect(
				mgr.Sync(ctx, mod, km, kernelVersion, previousImageName, imageName, true),
			).To(
				Equal(utils.Result{Requeue: true, Status: utils.StatusInProgress}),
			)
		})
	})
})
