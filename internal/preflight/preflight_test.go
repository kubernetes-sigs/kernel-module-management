package preflight

import (
	context "context"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	v1stream "github.com/google/go-containerregistry/pkg/v1/stream"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
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
	var (
		ctrl              *gomock.Controller
		mockKernelAPI     *module.MockKernelMapper
		mockStatusUpdater *statusupdater.MockPreflightStatusUpdater
		preflightHelper   *MockpreflightHelperAPI
		p                 *preflight
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockKernelAPI = module.NewMockKernelMapper(ctrl)
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

	It("Failed to find mapping", func() {
		mod.Spec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{}
		mockKernelAPI.EXPECT().FindMappingForKernel(mod.Spec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(nil, fmt.Errorf("some error"))

		res, message := p.PreflightUpgradeCheck(context.Background(), pv, mod)

		Expect(res).To(BeFalse())
		Expect(message).To(Equal(fmt.Sprintf("Failed to find kernel mapping in the module %s for kernel version %s", mod.Name, kernelVersion)))
	})

	It("failed to prepare kernel mapping", func() {
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage}
		mod.Spec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{}

		gomock.InOrder(
			mockKernelAPI.EXPECT().FindMappingForKernel(mod.Spec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mapping, nil),
			mockKernelAPI.EXPECT().PrepareKernelMapping(&mapping, gomock.Any()).Return(nil, fmt.Errorf("some error")),
		)

		res, message := p.PreflightUpgradeCheck(context.Background(), pv, mod)

		Expect(res).To(BeFalse())
		Expect(message).To(Equal(fmt.Sprintf("Failed to substitute template in kernel mapping in the module %s for kernel version %s", mod.Name, kernelVersion)))
	})

	It("verify image success, no build", func() {
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage}
		mod.Spec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{mapping}

		gomock.InOrder(
			mockKernelAPI.EXPECT().FindMappingForKernel(mod.Spec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mapping, nil),
			mockKernelAPI.EXPECT().PrepareKernelMapping(&mapping, gomock.Any()).Return(&mapping, nil),
			mockStatusUpdater.EXPECT().PreflightSetVerificationStage(context.Background(), pv, mod.Name, kmmv1beta1.VerificationStageImage),
			preflightHelper.EXPECT().verifyImage(context.Background(), &mapping, mod, kernelVersion).Return(true, "some message"),
		)

		res, message := p.PreflightUpgradeCheck(context.Background(), pv, mod)

		Expect(res).To(BeTrue())
		Expect(message).To(Equal("some message"))
	})

	It("verify image failed, no build", func() {
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage}
		mod.Spec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{mapping}

		gomock.InOrder(
			mockKernelAPI.EXPECT().FindMappingForKernel(mod.Spec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mapping, nil),
			mockKernelAPI.EXPECT().PrepareKernelMapping(&mapping, gomock.Any()).Return(&mapping, nil),
			mockStatusUpdater.EXPECT().PreflightSetVerificationStage(context.Background(), pv, mod.Name, kmmv1beta1.VerificationStageImage),
			preflightHelper.EXPECT().verifyImage(context.Background(), &mapping, mod, kernelVersion).Return(false, "some error message"),
		)

		res, message := p.PreflightUpgradeCheck(context.Background(), pv, mod)

		Expect(res).To(BeFalse())
		Expect(message).To(Equal("some error message"))
	})

	It("verify image failed, build exists, successful", func() {
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage, Build: &kmmv1beta1.Build{}}
		mod.Spec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{mapping}

		gomock.InOrder(
			mockKernelAPI.EXPECT().FindMappingForKernel(mod.Spec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mapping, nil),
			mockKernelAPI.EXPECT().PrepareKernelMapping(&mapping, gomock.Any()).Return(&mapping, nil),
			mockStatusUpdater.EXPECT().PreflightSetVerificationStage(context.Background(), pv, mod.Name, kmmv1beta1.VerificationStageImage),
			preflightHelper.EXPECT().verifyImage(context.Background(), &mapping, mod, kernelVersion).Return(false, "some error message"),
			mockStatusUpdater.EXPECT().PreflightSetVerificationStage(context.Background(), pv, mod.Name, kmmv1beta1.VerificationStageBuild),
			preflightHelper.EXPECT().verifyBuild(context.Background(), pv, &mapping, mod).Return(true, "some message"),
		)

		res, message := p.PreflightUpgradeCheck(context.Background(), pv, mod)

		Expect(res).To(BeTrue())
		Expect(message).To(Equal("some message"))
	})

	It("verify image failed, build exists, failed", func() {
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage, Build: &kmmv1beta1.Build{}}
		mod.Spec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{mapping}

		gomock.InOrder(
			mockKernelAPI.EXPECT().FindMappingForKernel(mod.Spec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mapping, nil),
			mockKernelAPI.EXPECT().PrepareKernelMapping(&mapping, gomock.Any()).Return(&mapping, nil),
			mockStatusUpdater.EXPECT().PreflightSetVerificationStage(context.Background(), pv, mod.Name, kmmv1beta1.VerificationStageImage),
			preflightHelper.EXPECT().verifyImage(context.Background(), &mapping, mod, kernelVersion).Return(false, "some error message"),
			mockStatusUpdater.EXPECT().PreflightSetVerificationStage(context.Background(), pv, mod.Name, kmmv1beta1.VerificationStageBuild),
			preflightHelper.EXPECT().verifyBuild(context.Background(), pv, &mapping, mod).Return(false, "some error message"),
		)

		res, message := p.PreflightUpgradeCheck(context.Background(), pv, mod)

		Expect(res).To(BeFalse())
		Expect(message).To(Equal("some error message"))
	})
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
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage}
		digests := []string{"digest0", "digest1"}
		repoConfig := &registry.RepoPullConfig{}
		digestLayer := v1stream.Layer{}
		gomock.InOrder(
			mockRegistryAPI.EXPECT().GetLayersDigests(context.Background(), containerImage, nil, gomock.Any()).Return(digests, repoConfig, nil),
			mockRegistryAPI.EXPECT().GetLayerByDigest(digests[1], repoConfig).Return(&digestLayer, nil),
			mockRegistryAPI.EXPECT().VerifyModuleExists(&digestLayer, "/opt", kernelVersion, "simple-kmod.ko").Return(true),
		)

		res, message := ph.verifyImage(context.Background(), &mapping, mod, kernelVersion)

		Expect(res).To(BeTrue())
		Expect(message).To(Equal(fmt.Sprintf(VerificationStatusReasonVerified, "image accessible and verified")))
	})

	It("get layers digest failed", func() {
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage}
		mockRegistryAPI.EXPECT().GetLayersDigests(context.Background(), containerImage, nil, gomock.Any()).Return(nil, nil, fmt.Errorf("some error"))

		res, message := ph.verifyImage(context.Background(), &mapping, mod, kernelVersion)

		Expect(res).To(BeFalse())
		Expect(message).To(Equal(fmt.Sprintf("image %s inaccessible or does not exists", containerImage)))
	})

	It("failed to get specific layer", func() {
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage}
		digests := []string{"digest0", "digest1"}
		repoConfig := &registry.RepoPullConfig{}
		gomock.InOrder(
			mockRegistryAPI.EXPECT().GetLayersDigests(context.Background(), containerImage, nil, gomock.Any()).Return(digests, repoConfig, nil),
			mockRegistryAPI.EXPECT().GetLayerByDigest(digests[1], repoConfig).Return(nil, fmt.Errorf("some error")),
		)

		res, message := ph.verifyImage(context.Background(), &mapping, mod, kernelVersion)

		Expect(res).To(BeFalse())
		Expect(message).To(Equal(fmt.Sprintf("image %s, layer %s is inaccessible", containerImage, digests[1])))
	})

	It("kernel module not present in the correct path", func() {
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage}
		digests := []string{"digest0"}
		repoConfig := &registry.RepoPullConfig{}
		digestLayer := v1stream.Layer{}
		gomock.InOrder(
			mockRegistryAPI.EXPECT().GetLayersDigests(context.Background(), containerImage, nil, gomock.Any()).Return(digests, repoConfig, nil),
			mockRegistryAPI.EXPECT().GetLayerByDigest(digests[0], repoConfig).Return(&digestLayer, nil),
			mockRegistryAPI.EXPECT().VerifyModuleExists(&digestLayer, "/opt", kernelVersion, "simple-kmod.ko").Return(false),
		)

		res, message := ph.verifyImage(context.Background(), &mapping, mod, kernelVersion)

		Expect(res).To(BeFalse())
		Expect(message).To(Equal(fmt.Sprintf("image %s does not contain kernel module for kernel %s on any layer", containerImage, kernelVersion)))
	})

})

var _ = Describe("preflightHelper_verifyBuild", func() {
	var (
		ctrl         *gomock.Controller
		mockBuildAPI *build.MockManager
		clnt         *client.MockClient
		ph           *preflightHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockBuildAPI = build.NewMockManager(ctrl)
		ph = &preflightHelper{
			client:   clnt,
			buildAPI: mockBuildAPI,
		}

	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("sync failed", func() {
		mod.Spec.ModuleLoader.Container.Build = &kmmv1beta1.Build{}
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage}
		mockBuildAPI.EXPECT().Sync(context.Background(), *mod, mapping, kernelVersion, mapping.ContainerImage, false).Return(build.Result{}, fmt.Errorf("some error"))

		res, msg := ph.verifyBuild(context.Background(), pv, &mapping, mod)
		Expect(res).To(BeFalse())
		Expect(msg).To(Equal(fmt.Sprintf("Failed to verify build for module %s, kernel version %s, error %s", mod.Name, kernelVersion, fmt.Errorf("some error"))))

	})

	It("sync completed", func() {
		mod.Spec.ModuleLoader.Container.Build = &kmmv1beta1.Build{}
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage}
		mockBuildAPI.EXPECT().Sync(context.Background(), *mod, mapping, kernelVersion, mapping.ContainerImage, false).Return(build.Result{Status: build.StatusCompleted}, nil)

		res, msg := ph.verifyBuild(context.Background(), pv, &mapping, mod)
		Expect(res).To(BeTrue())
		Expect(msg).To(Equal(fmt.Sprintf(VerificationStatusReasonVerified, "build compiles")))

	})

	It("sync not completed yet", func() {
		mod.Spec.ModuleLoader.Container.Build = &kmmv1beta1.Build{}
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage}
		mockBuildAPI.EXPECT().Sync(context.Background(), *mod, mapping, kernelVersion, mapping.ContainerImage, false).Return(build.Result{Status: build.StatusInProgress}, nil)

		res, msg := ph.verifyBuild(context.Background(), pv, &mapping, mod)
		Expect(res).To(BeFalse())
		Expect(msg).To(Equal("Waiting for build verification"))

	})
})
