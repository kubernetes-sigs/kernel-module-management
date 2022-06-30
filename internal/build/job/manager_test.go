package job

import (
	"context"
	"errors"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/internal/build"
	"github.com/qbarrand/oot-operator/internal/client"
	"github.com/qbarrand/oot-operator/internal/constants"
	"github.com/qbarrand/oot-operator/internal/registry"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Labels", func() {
	It("should work as expected", func() {
		const (
			moduleName   = "module-name"
			targetKernel = "1.2.3"
		)

		mod := ootov1alpha1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: moduleName},
		}

		labels := labels(mod, targetKernel)

		Expect(labels).To(HaveKeyWithValue(constants.ModuleNameLabel, moduleName))
		Expect(labels).To(HaveKeyWithValue(constants.TargetKernelTarget, targetKernel))
	})
})

var _ = Describe("JobManager", func() {
	Describe("Sync", func() {
		var (
			ctrl   *gomock.Controller
			clnt   *client.MockClient
			getter *registry.MockGetter
			maker  *MockMaker
			helper *build.MockHelper
		)

		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
			clnt = client.NewMockClient(ctrl)
			getter = registry.NewMockGetter(ctrl)
			maker = NewMockMaker(ctrl)
			helper = build.NewMockHelper(ctrl)
		})

		const (
			imageName = "image-name"
			namespace = "some-namespace"
		)

		po := ootov1alpha1.PullOptions{}

		km := ootov1alpha1.KernelMapping{
			Build:          &ootov1alpha1.Build{Pull: po},
			ContainerImage: imageName,
		}

		It("should return an error if there was an error getting the image", func() {
			ctx := context.TODO()
			gomock.InOrder(
				helper.EXPECT().GetRelevantBuild(gomock.Any(), km).Return(km.Build),
				getter.EXPECT().ImageExists(ctx, imageName, po).Return(false, errors.New("random error")),
			)
			mgr := NewBuildManager(nil, getter, maker, helper)

			_, err := mgr.Sync(ctx, ootov1alpha1.Module{}, km, "")
			Expect(err).To(HaveOccurred())
		})

		It("should return StatusCompleted if the image already exists", func() {
			ctx := context.TODO()

			gomock.InOrder(
				helper.EXPECT().GetRelevantBuild(gomock.Any(), km).Return(km.Build),
				getter.EXPECT().ImageExists(ctx, imageName, po).Return(true, nil),
			)

			mgr := NewBuildManager(nil, getter, maker, helper)

			Expect(
				mgr.Sync(ctx, ootov1alpha1.Module{}, km, ""),
			).To(
				Equal(build.Result{Status: build.StatusCompleted}),
			)
		})

		const (
			moduleName    = "module-name"
			kernelVersion = "1.2.3"
		)

		mod := ootov1alpha1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: moduleName},
		}

		DescribeTable("should return the correct status depending on the job status",
			func(s batchv1.JobStatus, r build.Result, expectsErr bool) {
				j := batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Labels:    labels(mod, kernelVersion),
						Namespace: namespace,
					},
					Status: s,
				}
				ctx := context.TODO()
				clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ interface{}, list *batchv1.JobList, _ ...interface{}) error {
						list.Items = []batchv1.Job{j}
						return nil
					},
				)

				gomock.InOrder(
					helper.EXPECT().GetRelevantBuild(mod, km).Return(km.Build),
					getter.EXPECT().ImageExists(ctx, imageName, po).Return(false, nil),
				)

				mgr := NewBuildManager(clnt, getter, maker, helper)

				res, err := mgr.Sync(ctx, mod, km, kernelVersion)

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
			ctx := context.TODO()

			gomock.InOrder(
				helper.EXPECT().GetRelevantBuild(mod, km).Return(km.Build),
				getter.EXPECT().ImageExists(ctx, imageName, po),
				maker.EXPECT().MakeJob(mod, km.Build, kernelVersion, km.ContainerImage).Return(nil, errors.New("random error")),
			)
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any(), gomock.Any())

			mgr := NewBuildManager(clnt, getter, maker, helper)

			Expect(
				mgr.Sync(ctx, mod, km, kernelVersion),
			).Error().To(
				HaveOccurred(),
			)
		})

		It("should create the job if there was no error making it", func() {
			ctx := context.TODO()

			const jobName = "some-job"

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
				getter.EXPECT().ImageExists(ctx, imageName, po),
				maker.EXPECT().MakeJob(mod, km.Build, kernelVersion, km.ContainerImage).Return(&j, nil),
			)

			gomock.InOrder(
				clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any(), gomock.Any()),
				clnt.EXPECT().Create(ctx, &j),
			)

			mgr := NewBuildManager(clnt, getter, maker, helper)

			Expect(
				mgr.Sync(ctx, mod, km, kernelVersion),
			).To(
				Equal(build.Result{Requeue: true, Status: build.StatusCreated}),
			)
		})
	})
})
