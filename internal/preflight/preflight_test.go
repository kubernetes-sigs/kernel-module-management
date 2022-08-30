package preflight

import (
	context "context"
	"fmt"
	"testing"

	"github.com/golang/mock/gomock"
	v1stream "github.com/google/go-containerregistry/pkg/v1/stream"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kmmv1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	"github.com/qbarrand/oot-operator/internal/client"
	"github.com/qbarrand/oot-operator/internal/module"
	"github.com/qbarrand/oot-operator/internal/registry"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	moduleName     = "module name"
	containerImage = "container image"
	kernelVersion  = "kernel version"
)

var (
	ctrl            *gomock.Controller
	mockRegistryAPI *registry.MockRegistry
	mockKernelAPI   *module.MockKernelMapper
	clnt            *client.MockClient
	p               *preflight
	mod             *kmmv1beta1.Module
)

func TestPreflight(t *testing.T) {
	RegisterFailHandler(Fail)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockRegistryAPI = registry.NewMockRegistry(ctrl)
		mockKernelAPI = module.NewMockKernelMapper(ctrl)
		mod = &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name: moduleName,
			},
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						Modprobe: kmmv1beta1.ModprobeSpec{
							ModuleName: "simple-kmod.ko",
							DirName:    "/opt",
						},
					},
				},
			},
		}
		p = NewPreflightAPI(clnt,
			mockRegistryAPI,
			mockKernelAPI).(*preflight)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	RunSpecs(t, "Preflight Suite")
}

var _ = Describe("preflight upgrade", func() {

	It("Failed to find mapping", func() {
		mod.Spec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{}
		mockKernelAPI.EXPECT().FindMappingForKernel(mod.Spec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(nil, fmt.Errorf("some error"))

		res, message := p.PreflightUpgradeCheck(context.Background(), mod, kernelVersion)

		Expect(res).To(BeFalse())
		Expect(message).To(Equal(fmt.Sprintf("Failed to find kernel mapping in the module %s for kernel version %s", mod.Name, kernelVersion)))
	})

	It("failed to prepare kernel mapping", func() {
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage}
		mod.Spec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{}

		mockKernelAPI.EXPECT().FindMappingForKernel(mod.Spec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mapping, nil)
		mockKernelAPI.EXPECT().PrepareKernelMapping(&mapping, gomock.Any()).Return(nil, fmt.Errorf("some error"))

		res, message := p.PreflightUpgradeCheck(context.Background(), mod, kernelVersion)

		Expect(res).To(BeFalse())
		Expect(message).To(Equal(fmt.Sprintf("Failed to substitute template in kernel mapping in the module %s for kernel version %s", mod.Name, kernelVersion)))
	})
})

var _ = Describe("verifyImage", func() {

	It("good flow", func() {
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage}
		digests := []string{"digest0", "digest1"}
		repoConfig := &registry.RepoPullConfig{}
		digestLayer := v1stream.Layer{}
		mockRegistryAPI.EXPECT().GetLayersDigests(context.Background(), containerImage, gomock.Any()).Return(digests, repoConfig, nil)
		mockRegistryAPI.EXPECT().GetLayerByDigest(digests[1], repoConfig).Return(&digestLayer, nil)
		mockRegistryAPI.EXPECT().VerifyModuleExists(&digestLayer, "/opt", kernelVersion, "simple-kmod.ko").Return(true)

		res, message := p.verifyImage(context.Background(), &mapping, mod, kernelVersion)

		Expect(res).To(BeTrue())
		Expect(message).To(Equal(VerificationStatusReasonVerified))
	})

	It("get layers digest failed", func() {
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage}
		mockRegistryAPI.EXPECT().GetLayersDigests(context.Background(), containerImage, gomock.Any()).Return(nil, nil, fmt.Errorf("some error"))

		res, message := p.verifyImage(context.Background(), &mapping, mod, kernelVersion)

		Expect(res).To(BeFalse())
		Expect(message).To(Equal(fmt.Sprintf("image %s inaccessible or does not exists", containerImage)))
	})

	It("failed to get specific layer", func() {
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage}
		digests := []string{"digest0", "digest1"}
		repoConfig := &registry.RepoPullConfig{}
		mockRegistryAPI.EXPECT().GetLayersDigests(context.Background(), containerImage, gomock.Any()).Return(digests, repoConfig, nil)
		mockRegistryAPI.EXPECT().GetLayerByDigest(digests[1], repoConfig).Return(nil, fmt.Errorf("some error"))

		res, message := p.verifyImage(context.Background(), &mapping, mod, kernelVersion)

		Expect(res).To(BeFalse())
		Expect(message).To(Equal(fmt.Sprintf("image %s, layer %s is inaccessible", containerImage, digests[1])))
	})

	It("kernel module not present in the correct path", func() {
		mapping := kmmv1beta1.KernelMapping{ContainerImage: containerImage}
		digests := []string{"digest0"}
		repoConfig := &registry.RepoPullConfig{}
		digestLayer := v1stream.Layer{}
		mockRegistryAPI.EXPECT().GetLayersDigests(context.Background(), containerImage, gomock.Any()).Return(digests, repoConfig, nil)
		mockRegistryAPI.EXPECT().GetLayerByDigest(digests[0], repoConfig).Return(&digestLayer, nil)
		mockRegistryAPI.EXPECT().VerifyModuleExists(&digestLayer, "/opt", kernelVersion, "simple-kmod.ko").Return(false)

		res, message := p.verifyImage(context.Background(), &mapping, mod, kernelVersion)

		Expect(res).To(BeFalse())
		Expect(message).To(Equal(fmt.Sprintf("image %s does not contain kernel module for kernel %s on any layer", containerImage, kernelVersion)))
	})

})
