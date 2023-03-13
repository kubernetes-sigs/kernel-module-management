package preflight

import (
	context "context"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	v1stream "github.com/google/go-containerregistry/pkg/v1/stream"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/imgbuild"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/statusupdater"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	moduleName     = "module name"
	containerImage = "container image"
	kernelVersion  = "kernel version"
)

var (
	mod *kmmv1beta1.Module
	pv  *kmmv1beta1.PreflightValidation
)

func TestPreflight(t *testing.T) {
	RegisterFailHandler(Fail)

	BeforeEach(func() {
		pv = &kmmv1beta1.PreflightValidation{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "preflight-name",
				Namespace: "preflight-namespace",
			},
			Spec: kmmv1beta1.PreflightValidationSpec{
				KernelVersion: kernelVersion,
			},
		}
		mod = &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name: moduleName,
			},
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						Modprobe: kmmv1beta1.ModprobeSpec{
							ModuleName: "simple-kmod",
							DirName:    "/opt",
						},
					},
				},
			},
		}
	})

	RunSpecs(t, "Preflight Suite")
}

var _ = Describe("preflight_PreflightUpgradeCheck", func() {
	const (
		buildExistsFlag   = true
		signExistsFlag    = true
		imageVerifiedFlag = true
		buildVerifiedFlag = true
		signVerifiedFlag  = true
	)
	var (
		ctrl              *gomock.Controller
		mockKernelAPI     *api.MockModuleLoaderDataFactory
		mockStatusUpdater *statusupdater.MockPreflightStatusUpdater
		preflightHelper   *MockpreflightHelperAPI
		p                 *preflight
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKernelAPI = api.NewMockModuleLoaderDataFactory(ctrl)
		mockStatusUpdater = statusupdater.NewMockPreflightStatusUpdater(ctrl)
		preflightHelper = NewMockpreflightHelperAPI(ctrl)
		p = &preflight{
			kernelAPI:     mockKernelAPI,
			helper:        preflightHelper,
			statusUpdater: mockStatusUpdater,
		}

	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("Failed to process mapping", func() {
		mod.Spec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{}
		mockKernelAPI.EXPECT().FromModule(mod, kernelVersion).Return(nil, fmt.Errorf("some error"))

		res, message := p.PreflightUpgradeCheck(context.Background(), pv, mod)

		Expect(res).To(BeFalse())
		Expect(message).To(Equal(fmt.Sprintf("failed to process kernel mapping in the module %s for kernel version %s", mod.Name, kernelVersion)))
	})

	DescribeTable("correct flow of the image/build/sign verification", func(buildExists, signExists, imageVerified, buildVerified, signVerified,
		returnedResult bool, returnedMessage string) {
		ctx := context.Background()
		mld := api.ModuleLoaderData{
			Name:           mod.Name,
			Namespace:      mod.Namespace,
			ContainerImage: containerImage,
			Owner:          mod,
			KernelVersion:  kernelVersion,
		}
		if buildExists {
			mld.Build = &kmmv1beta1.Build{}
		}
		if signExists {
			mld.Sign = &kmmv1beta1.Sign{}
		}

		mockKernelAPI.EXPECT().FromModule(mod, kernelVersion).Return(&mld, nil)
		mockStatusUpdater.EXPECT().PreflightSetVerificationStage(context.Background(), pv, mld.Name, kmmv1beta1.VerificationStageImage).Return(nil)
		preflightHelper.EXPECT().verifyImage(ctx, &mld).Return(imageVerified, "image message")
		if !imageVerified {
			if buildExists {
				mockStatusUpdater.EXPECT().PreflightSetVerificationStage(context.Background(), pv, mld.Name, kmmv1beta1.VerificationStageBuild).Return(nil)
				preflightHelper.EXPECT().verifyBuild(ctx, pv, &mld).Return(buildVerified, "build message")
			}
			if signExists {
				if buildVerified || !buildExists {
					mockStatusUpdater.EXPECT().PreflightSetVerificationStage(context.Background(), pv, mld.Name, kmmv1beta1.VerificationStageSign).Return(nil)
					preflightHelper.EXPECT().verifySign(ctx, pv, &mld).Return(signVerified, "sign message")
				}
			}
		}

		res, msg := p.PreflightUpgradeCheck(context.Background(), pv, mod)
		Expect(res).To(Equal(returnedResult))
		Expect(msg).To(Equal(returnedMessage))
	},
		Entry(
			"no build, no sign, image verified",
			!buildExistsFlag, signExistsFlag, imageVerifiedFlag, !buildVerifiedFlag, !signVerifiedFlag, true, "image message",
		),
		Entry(
			"no build, no sign, image not verified",
			!buildExistsFlag, !signExistsFlag, !imageVerifiedFlag, !buildVerifiedFlag, !signVerifiedFlag, false, "image message",
		),
		Entry(
			"build exists, no sign, image verified",
			buildExistsFlag, !signExistsFlag, imageVerifiedFlag, !buildVerifiedFlag, !signVerifiedFlag, true, "image message",
		),
		Entry(
			"build exists, no sign, image not verified, build verified",
			buildExistsFlag, !signExistsFlag, !imageVerifiedFlag, buildVerifiedFlag, !signVerifiedFlag, true, "build message",
		),
		Entry(
			"build exists, no sign, image not verified, build not verified",
			buildExistsFlag, !signExistsFlag, !imageVerifiedFlag, !buildVerifiedFlag, !signVerifiedFlag, false, "build message",
		),
		Entry(
			"build exists, sign exists , image verified",
			buildExistsFlag, signExistsFlag, imageVerifiedFlag, !buildVerifiedFlag, !signVerifiedFlag, true, "image message",
		),
		Entry(
			"build exists, sign exists , image not verified, build verified, sign not verified",
			buildExistsFlag, signExistsFlag, !imageVerifiedFlag, buildVerifiedFlag, !signVerifiedFlag, false, "sign message",
		),
		Entry(
			"build exists, sign exists , image not verified, build verified, sign verified",
			buildExistsFlag, signExistsFlag, !imageVerifiedFlag, buildVerifiedFlag, signVerifiedFlag, true, "sign message",
		),
		Entry(
			"build exists, sign exists , image not verified, build not verified",
			buildExistsFlag, signExistsFlag, !imageVerifiedFlag, !buildVerifiedFlag, signVerifiedFlag, false, "build message",
		),
		Entry(
			"build not exists, sign exists , image verified",
			!buildExistsFlag, signExistsFlag, imageVerifiedFlag, !buildVerifiedFlag, signVerifiedFlag, true, "image message",
		),
		Entry(
			"build not exists, sign exists , image not verified, sign not verified",
			!buildExistsFlag, signExistsFlag, !imageVerifiedFlag, !buildVerifiedFlag, !signVerifiedFlag, false, "sign message",
		),
		Entry(
			"build not exists, sign exists , image not verified, sign verified",
			!buildExistsFlag, signExistsFlag, !imageVerifiedFlag, !buildVerifiedFlag, signVerifiedFlag, true, "sign message",
		),
	)
})

var _ = Describe("preflightHelper_verifyImage", func() {
	var (
		ctrl            *gomock.Controller
		mockRegistryAPI *registry.MockRegistry
		clnt            *client.MockClient
		ph              *preflightHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockRegistryAPI = registry.NewMockRegistry(ctrl)
		ph = &preflightHelper{
			client:      clnt,
			registryAPI: mockRegistryAPI,
		}

	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("good flow", func() {
		mld := api.ModuleLoaderData{
			ContainerImage: containerImage,
			Modprobe:       mod.Spec.ModuleLoader.Container.Modprobe,
			KernelVersion:  kernelVersion,
		}
		digests := []string{"digest0", "digest1"}
		repoConfig := &registry.RepoPullConfig{}
		digestLayer := v1stream.Layer{}
		gomock.InOrder(
			mockRegistryAPI.EXPECT().GetLayersDigests(context.Background(), containerImage, gomock.Any(),
				gomock.Any()).Return(digests, repoConfig, nil),
			mockRegistryAPI.EXPECT().GetLayerByDigest(digests[1], repoConfig).Return(&digestLayer, nil),
			mockRegistryAPI.EXPECT().VerifyModuleExists(&digestLayer, "/opt", kernelVersion, "simple-kmod.ko").Return(true),
		)

		res, message := ph.verifyImage(context.Background(), &mld)

		Expect(res).To(BeTrue())
		Expect(message).To(Equal(fmt.Sprintf(VerificationStatusReasonVerified, "image accessible and verified")))
	})

	It("get layers digest failed", func() {
		mld := api.ModuleLoaderData{
			ContainerImage: containerImage,
			KernelVersion:  kernelVersion,
		}

		mockRegistryAPI.EXPECT().GetLayersDigests(context.Background(), containerImage, gomock.Any(),
			gomock.Any()).Return(nil, nil, fmt.Errorf("some error"))

		res, message := ph.verifyImage(context.Background(), &mld)

		Expect(res).To(BeFalse())
		Expect(message).To(Equal(fmt.Sprintf("image %s inaccessible or does not exists", containerImage)))
	})

	It("failed to get specific layer", func() {
		mld := api.ModuleLoaderData{
			ContainerImage: containerImage,
			KernelVersion:  kernelVersion,
		}
		digests := []string{"digest0", "digest1"}
		repoConfig := &registry.RepoPullConfig{}
		gomock.InOrder(
			mockRegistryAPI.EXPECT().GetLayersDigests(context.Background(), containerImage, gomock.Any(),
				gomock.Any()).Return(digests, repoConfig, nil),
			mockRegistryAPI.EXPECT().GetLayerByDigest(digests[1], repoConfig).Return(nil, fmt.Errorf("some error")),
		)

		res, message := ph.verifyImage(context.Background(), &mld)

		Expect(res).To(BeFalse())
		Expect(message).To(Equal(fmt.Sprintf("image %s, layer %s is inaccessible", containerImage, digests[1])))
	})

	It("kernel module not present in the correct path", func() {
		mld := api.ModuleLoaderData{
			ContainerImage: containerImage,
			Modprobe:       mod.Spec.ModuleLoader.Container.Modprobe,
			KernelVersion:  kernelVersion,
		}
		digests := []string{"digest0"}
		repoConfig := &registry.RepoPullConfig{}
		digestLayer := v1stream.Layer{}
		gomock.InOrder(
			mockRegistryAPI.EXPECT().GetLayersDigests(context.Background(), containerImage, gomock.Any(), gomock.Any()).Return(digests, repoConfig, nil),
			mockRegistryAPI.EXPECT().GetLayerByDigest(digests[0], repoConfig).Return(&digestLayer, nil),
			mockRegistryAPI.EXPECT().VerifyModuleExists(&digestLayer, "/opt", kernelVersion, "simple-kmod.ko").Return(false),
		)

		res, message := ph.verifyImage(context.Background(), &mld)

		Expect(res).To(BeFalse())
		Expect(message).To(Equal(fmt.Sprintf("image %s does not contain kernel module for kernel %s on any layer", containerImage, kernelVersion)))
	})

})

var _ = Describe("preflightHelper_verifyBuild", func() {
	var (
		ctrl         *gomock.Controller
		mockBuildAPI *imgbuild.MockJobManager
		clnt         *client.MockClient
		ph           *preflightHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockBuildAPI = imgbuild.NewMockJobManager(ctrl)
		ph = &preflightHelper{
			client:   clnt,
			buildAPI: mockBuildAPI,
		}

	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("sync failed", func() {
		mld := api.ModuleLoaderData{
			Name:           mod.Name,
			ContainerImage: containerImage,
			Build:          &kmmv1beta1.Build{},
			KernelVersion:  kernelVersion,
		}

		mockBuildAPI.EXPECT().Sync(context.Background(), &mld, pv.Spec.PushBuiltImage, pv).
			Return(imgbuild.Status(""), fmt.Errorf("some error"))

		res, msg := ph.verifyBuild(context.Background(), pv, &mld)
		Expect(res).To(BeFalse())
		Expect(msg).To(Equal(fmt.Sprintf("Failed to verify build for module %s, kernel version %s, error %s", mld.Name, kernelVersion, fmt.Errorf("some error"))))
	})

	It("sync completed", func() {
		mld := api.ModuleLoaderData{
			Name:           mod.Name,
			ContainerImage: containerImage,
			Build:          &kmmv1beta1.Build{},
			KernelVersion:  kernelVersion,
		}

		mockBuildAPI.EXPECT().Sync(context.Background(), &mld, pv.Spec.PushBuiltImage, pv).
			Return(imgbuild.StatusCompleted, nil)

		res, msg := ph.verifyBuild(context.Background(), pv, &mld)
		Expect(res).To(BeTrue())
		Expect(msg).To(Equal(fmt.Sprintf(VerificationStatusReasonVerified, "build compiles")))
	})

	It("sync not completed yet", func() {
		mld := api.ModuleLoaderData{
			Name:           mod.Name,
			ContainerImage: containerImage,
			Build:          &kmmv1beta1.Build{},
			KernelVersion:  kernelVersion,
		}

		mockBuildAPI.EXPECT().Sync(context.Background(), &mld, pv.Spec.PushBuiltImage, pv).
			Return(imgbuild.StatusInProgress, nil)

		res, msg := ph.verifyBuild(context.Background(), pv, &mld)
		Expect(res).To(BeFalse())
		Expect(msg).To(Equal("Waiting for build verification"))
	})
})

var _ = Describe("preflightHelper_verifySign", func() {
	var (
		ctrl        *gomock.Controller
		mockSignAPI *imgbuild.MockJobManager
		clnt        *client.MockClient
		ph          *preflightHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockSignAPI = imgbuild.NewMockJobManager(ctrl)
		ph = &preflightHelper{
			client:  clnt,
			signAPI: mockSignAPI,
		}

	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("sync failed", func() {
		mld := api.ModuleLoaderData{
			Name:           mod.Name,
			ContainerImage: containerImage,
			Sign:           &kmmv1beta1.Sign{},
			KernelVersion:  kernelVersion,
		}

		mockSignAPI.EXPECT().Sync(context.Background(), &mld, pv.Spec.PushBuiltImage, pv).
			Return(imgbuild.Status(""), fmt.Errorf("some error"))

		res, msg := ph.verifySign(context.Background(), pv, &mld)
		Expect(res).To(BeFalse())
		Expect(msg).To(Equal(fmt.Sprintf("Failed to verify signing for module %s, kernel version %s, error %s", mld.Name, kernelVersion, fmt.Errorf("some error"))))
	})

	It("sync completed", func() {
		mld := api.ModuleLoaderData{
			Name:           mod.Name,
			ContainerImage: containerImage,
			Sign:           &kmmv1beta1.Sign{},
			KernelVersion:  kernelVersion,
		}

		mockSignAPI.EXPECT().Sync(context.Background(), &mld, pv.Spec.PushBuiltImage, pv).
			Return(imgbuild.StatusCompleted, nil)

		res, msg := ph.verifySign(context.Background(), pv, &mld)
		Expect(res).To(BeTrue())
		Expect(msg).To(Equal(fmt.Sprintf(VerificationStatusReasonVerified, "sign completes")))
	})

	It("sync not completed yet", func() {
		mld := api.ModuleLoaderData{
			Name:           mod.Name,
			ContainerImage: containerImage,
			Sign:           &kmmv1beta1.Sign{},
			KernelVersion:  kernelVersion,
		}

		mockSignAPI.EXPECT().Sync(context.Background(), &mld, pv.Spec.PushBuiltImage, pv).
			Return(imgbuild.StatusInProgress, nil)

		res, msg := ph.verifySign(context.Background(), pv, &mld)
		Expect(res).To(BeFalse())
		Expect(msg).To(Equal("Waiting for sign verification"))
	})
})
