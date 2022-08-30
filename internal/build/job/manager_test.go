package job

import (
	"context"
	"errors"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kmmv1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	"github.com/qbarrand/oot-operator/internal/auth"
	"github.com/qbarrand/oot-operator/internal/build"
	"github.com/qbarrand/oot-operator/internal/client"
	"github.com/qbarrand/oot-operator/internal/constants"
	registrypkg "github.com/qbarrand/oot-operator/internal/registry"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Labels", func() {
	It("should work as expected", func() {
		const (
			moduleName   = "module-name"
			targetKernel = "1.2.3"
		)

		mod := kmmv1beta1.Module{
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
			ctrl     *gomock.Controller
			clnt     *client.MockClient
			registry *registrypkg.MockRegistry
			maker    *MockMaker
			helper   *build.MockHelper
		)

		const (
			imageName = "image-name"
			namespace = "some-namespace"
		)

		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
			clnt = client.NewMockClient(ctrl)
			registry = registrypkg.NewMockRegistry(ctrl)
			maker = NewMockMaker(ctrl)
			helper = build.NewMockHelper(ctrl)
		})

		po := kmmv1beta1.PullOptions{}

		km := kmmv1beta1.KernelMapping{
			Build:          &kmmv1beta1.Build{Pull: po},
			ContainerImage: imageName,
		}

		It("should return an error if there was an error getting the image", func() {
			ctx := context.Background()
			gomock.InOrder(
				helper.EXPECT().GetRelevantBuild(gomock.Any(), km).Return(km.Build),
				registry.EXPECT().ImageExists(ctx, imageName, po, gomock.Any()).Return(false, errors.New("random error")),
			)
			mgr := NewBuildManager(nil, registry, maker, helper)

			_, err := mgr.Sync(ctx, kmmv1beta1.Module{}, km, "")
			Expect(err).To(HaveOccurred())
		})

		It("should return StatusCompleted if the image already exists", func() {
			ctx := context.Background()

			gomock.InOrder(
				helper.EXPECT().GetRelevantBuild(gomock.Any(), km).Return(km.Build),
				registry.EXPECT().ImageExists(ctx, imageName, po, gomock.Any()).Return(true, nil),
			)

			mgr := NewBuildManager(nil, registry, maker, helper)

			Expect(
				mgr.Sync(ctx, kmmv1beta1.Module{}, km, ""),
			).To(
				Equal(build.Result{Status: build.StatusCompleted}),
			)
		})

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
						Labels:    labels(mod, kernelVersion),
						Namespace: namespace,
					},
					Status: s,
				}
				ctx := context.Background()
				clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
					func(_ interface{}, list *batchv1.JobList, _ ...interface{}) error {
						list.Items = []batchv1.Job{j}
						return nil
					},
				)

				gomock.InOrder(
					helper.EXPECT().GetRelevantBuild(mod, km).Return(km.Build),
					registry.EXPECT().ImageExists(ctx, imageName, po, gomock.Any()).Return(false, nil),
				)

				mgr := NewBuildManager(clnt, registry, maker, helper)

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
			ctx := context.Background()

			gomock.InOrder(
				helper.EXPECT().GetRelevantBuild(mod, km).Return(km.Build),
				registry.EXPECT().ImageExists(ctx, imageName, po, gomock.Any()),
				maker.EXPECT().MakeJob(mod, km.Build, kernelVersion, km.ContainerImage).Return(nil, errors.New("random error")),
			)
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any(), gomock.Any())

			mgr := NewBuildManager(clnt, registry, maker, helper)

			Expect(
				mgr.Sync(ctx, mod, km, kernelVersion),
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
				registry.EXPECT().ImageExists(ctx, imageName, po, gomock.Any()),
				maker.EXPECT().MakeJob(mod, km.Build, kernelVersion, km.ContainerImage).Return(&j, nil),
			)

			gomock.InOrder(
				clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any(), gomock.Any()),
				clnt.EXPECT().Create(ctx, &j),
			)

			mgr := NewBuildManager(clnt, registry, maker, helper)

			Expect(
				mgr.Sync(ctx, mod, km, kernelVersion),
			).To(
				Equal(build.Result{Requeue: true, Status: build.StatusCreated}),
			)
		})

		It("should use a non-nil RegistryAuthGetter if the imagePullSecret is set in the module", func() {

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

			mod.Spec.ImageRepoSecret = &v1.LocalObjectReference{Name: "pull-push-secret"}

			gomock.InOrder(
				helper.EXPECT().GetRelevantBuild(mod, km).Return(km.Build),
				registry.EXPECT().ImageExists(ctx, imageName, po, gomock.Any()).DoAndReturn(
					func(_ interface{}, _ interface{}, _ interface{}, registryAuthGetter auth.RegistryAuthGetter) (bool, error) {
						Expect(registryAuthGetter).ToNot(BeNil())
						return false, nil
					},
				),
				clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any(), gomock.Any()),
				maker.EXPECT().MakeJob(mod, km.Build, kernelVersion, km.ContainerImage).Return(&j, nil),
				clnt.EXPECT().Create(ctx, &j),
			)

			mgr := NewBuildManager(clnt, registry, maker, helper)

			Expect(
				mgr.Sync(ctx, mod, km, kernelVersion),
			).To(
				Equal(build.Result{Requeue: true, Status: build.StatusCreated}),
			)
		})
	})
})
