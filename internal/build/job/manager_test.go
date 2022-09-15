package job

import (
	"context"
	"errors"

	"github.com/golang/mock/gomock"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Labels", func() {
	It("should work as expected", func() {
		const (
			moduleName   = "module-name"
			targetKernel = "1.2.3"
			buildStage   = "test"
		)

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: moduleName},
		}

		labels := labels(mod, targetKernel, buildStage)

		Expect(labels).To(HaveKeyWithValue(constants.ModuleNameLabel, moduleName))
		Expect(labels).To(HaveKeyWithValue(constants.TargetKernelTarget, targetKernel))
		Expect(labels).To(HaveKeyWithValue(constants.BuildStage, buildStage))
	})
})

var _ = Describe("addTag", func() {
	It("should work as expected", func() {
		const (
			shortimagename = "mycontainer"
			longimagename  = "mycontainer:tagged"
			tag            = "tag"
		)

		fullname := addTag(shortimagename, tag)
		Expect(fullname).To(Equal(shortimagename + ":" + tag))

		fullname = addTag(longimagename, tag)
		Expect(fullname).To(Equal(longimagename + tag))
	})
})

var _ = Describe("JobManager", func() {
	Describe("Sync", func() {

		var (
			ctrl   *gomock.Controller
			clnt   *client.MockClient
			maker  *MockMaker
			helper *build.MockHelper
		)

		const (
			imageName  = "image-name"
			namespace  = "some-namespace"
			buildStage = "test"
		)

		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
			clnt = client.NewMockClient(ctrl)
			maker = NewMockMaker(ctrl)
			helper = build.NewMockHelper(ctrl)
		})

		po := &kmmv1beta1.PullOptions{}

		km := kmmv1beta1.KernelMapping{
			Build:          &kmmv1beta1.Build{Pull: *po},
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
						Labels:    labels(mod, kernelVersion, buildStage),
						Namespace: namespace,
					},
					Status: s,
				}
				ctx := context.Background()

				gomock.InOrder(
					helper.EXPECT().GetRelevantBuild(mod, km).Return(km.Build),
					maker.EXPECT().ShouldRun(&mod, &km).Return(true),
					maker.EXPECT().GetName().Return("build"),
					clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
						func(_ interface{}, list *batchv1.JobList, _ ...interface{}) error {
							list.Items = []batchv1.Job{j}
							return nil
						},
					),
				)

				mgr := NewBuildManager(clnt, helper, maker)

				res, err := mgr.Sync(ctx, mod, km, kernelVersion, true)

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

		It("should return an error if there was an error creating the job", func() {
			ctx := context.Background()

			gomock.InOrder(
				helper.EXPECT().GetRelevantBuild(mod, km).Return(km.Build),
				maker.EXPECT().ShouldRun(&mod, &km).Return(true),
				maker.EXPECT().GetName().Return("build"),
				maker.EXPECT().MakeJob(mod, km.Build, kernelVersion, "", km.ContainerImage, true).Return(nil, errors.New("random error")),
			)
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any(), gomock.Any())

			mgr := NewBuildManager(clnt, helper, maker)

			Expect(
				mgr.Sync(ctx, mod, km, kernelVersion, true),
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
				maker.EXPECT().ShouldRun(&mod, &km).Return(true),
				maker.EXPECT().GetName().Return("build"),
				maker.EXPECT().MakeJob(mod, km.Build, kernelVersion, "", km.ContainerImage, true).Return(&j, nil),
			)

			gomock.InOrder(
				clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any(), gomock.Any()),
				clnt.EXPECT().Create(ctx, &j),
			)

			mgr := NewBuildManager(clnt, helper, maker)

			Expect(
				mgr.Sync(ctx, mod, km, kernelVersion, true),
			).To(
				Equal(build.Result{Requeue: true, Status: build.StatusCreated}),
			)
		})
	})
})
