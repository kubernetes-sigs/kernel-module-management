package module

import (
	"github.com/golang/mock/gomock"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
)

var _ = Describe("FindMappingForKernel", func() {
	const (
		kernelVersion = "1.2.3"
		selectedImage = "image1"
	)

	var (
		ctrl        *gomock.Controller
		buildHelper *build.MockHelper
		signHelper  *sign.MockHelper
		km          KernelMapper
		modSpec     kmmv1beta1.ModuleSpec
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		buildHelper = build.NewMockHelper(ctrl)
		signHelper = sign.NewMockHelper(ctrl)
		km = NewKernelMapper(buildHelper, signHelper)
		modSpec = kmmv1beta1.ModuleSpec{
			ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
				Container: kmmv1beta1.ModuleLoaderContainerSpec{},
			},
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("one literal mapping, no module sign or build", func() {
		mapping := kmmv1beta1.KernelMapping{
			ContainerImage: selectedImage,
			Literal:        "1.2.3",
		}
		expectedMapping := kmmv1beta1.KernelMapping{
			ContainerImage: selectedImage,
			Literal:        "1.2.3",
			RegistryTLS:    &kmmv1beta1.TLSOptions{},
		}
		modSpec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{mapping}

		m, err := km.FindMappingForKernel(&modSpec, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(m).To(Equal(&expectedMapping))
	})

	It("one regexp mapping, no module sign or build", func() {
		mapping := kmmv1beta1.KernelMapping{
			ContainerImage: selectedImage,
			Regexp:         `1\..*`,
		}
		expectedMapping := kmmv1beta1.KernelMapping{
			ContainerImage: selectedImage,
			Regexp:         `1\..*`,
			RegistryTLS:    &kmmv1beta1.TLSOptions{},
		}
		modSpec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{mapping}

		m, err := km.FindMappingForKernel(&modSpec, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(m).To(Equal(&expectedMapping))
	})

	It("should return an error if a regex is invalid", func() {
		mapping := kmmv1beta1.KernelMapping{
			ContainerImage: selectedImage,
			Regexp:         "invalid)",
		}
		modSpec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{mapping}

		_, err := km.FindMappingForKernel(&modSpec, kernelVersion)
		Expect(err).To(HaveOccurred())
	})

	It("should return an error if no mapping work", func() {
		mappings := []kmmv1beta1.KernelMapping{
			{
				ContainerImage: selectedImage,
				Regexp:         `1.2.2`,
			},
			{
				ContainerImage: selectedImage,
				Regexp:         `0\..*`,
			},
		}
		modSpec.ModuleLoader.Container.KernelMappings = mappings

		_, err := km.FindMappingForKernel(&modSpec, kernelVersion)
		Expect(err).To(MatchError("failed to find mapping for kernel 1.2.3: no suitable mapping found"))
	})

	It("one literal, unify module and mapping build", func() {
		mapping := kmmv1beta1.KernelMapping{
			ContainerImage: selectedImage,
			Literal:        "1.2.3",
		}
		modSpec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{mapping}
		modSpec.ModuleLoader.Container.Build = &kmmv1beta1.Build{
			DockerfileConfigMap: &v1.LocalObjectReference{
				Name: "some name",
			},
		}
		expectedMapping := kmmv1beta1.KernelMapping{
			ContainerImage: selectedImage,
			Literal:        "1.2.3",
			RegistryTLS:    &kmmv1beta1.TLSOptions{},
			Build: &kmmv1beta1.Build{
				DockerfileConfigMap: &v1.LocalObjectReference{
					Name: "some name",
				},
			},
		}
		buildHelper.EXPECT().GetRelevantBuild(modSpec.ModuleLoader.Container.Build, mapping.Build).Return(modSpec.ModuleLoader.Container.Build)

		m, err := km.FindMappingForKernel(&modSpec, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(m).To(Equal(&expectedMapping))
	})

	It("one literal, unify module and mapping sign", func() {
		mapping := kmmv1beta1.KernelMapping{
			ContainerImage: selectedImage,
			Literal:        "1.2.3",
		}
		modSpec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{mapping}
		modSpec.ModuleLoader.Container.Sign = &kmmv1beta1.Sign{
			UnsignedImage: "some image",
		}
		expectedMapping := kmmv1beta1.KernelMapping{
			ContainerImage: selectedImage,
			Literal:        "1.2.3",
			RegistryTLS:    &kmmv1beta1.TLSOptions{},
			Sign: &kmmv1beta1.Sign{
				UnsignedImage: "some image",
			},
		}
		signHelper.EXPECT().GetRelevantSign(modSpec.ModuleLoader.Container.Sign, mapping.Sign, kernelVersion).Return(modSpec.ModuleLoader.Container.Sign, nil)

		m, err := km.FindMappingForKernel(&modSpec, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(m).To(Equal(&expectedMapping))
	})
})

var _ = Describe("PrepareKernelMapping", func() {
	osConfig := NodeOSConfig{
		KernelFullVersion:  "kernelFullVersion",
		KernelVersionMMP:   "kernelMMP",
		KernelVersionMajor: "kernelMajor",
		KernelVersionMinor: "kernelMinor",
		KernelVersionPatch: "kernelPatch",
	}
	km := NewKernelMapper(nil, nil)

	It("error input", func() {
		mapping := kmmv1beta1.KernelMapping{
			ContainerImage: "some image:${KERNEL_XYZ",
			Literal:        "some literal",
			Regexp:         "regexp",
		}
		_, err := km.PrepareKernelMapping(&mapping, &osConfig)
		Expect(err).To(HaveOccurred())
	})

	It("should only substitute the ContainerImage field", func() {
		const (
			literal = "some literal:${KERNEL_XYZ"
			regexp  = "some regexp:${KERNEL_XYZ"
		)

		mapping := kmmv1beta1.KernelMapping{
			ContainerImage: "some image:${KERNEL_XYZ}",
			Literal:        literal,
			Regexp:         regexp,
			Build: &kmmv1beta1.Build{
				BuildArgs: []kmmv1beta1.BuildArg{
					{Name: "name1", Value: "value1"},
					{Name: "kernel version", Value: "${KERNEL_FULL_VERSION}"},
				},
				DockerfileConfigMap: &v1.LocalObjectReference{},
			},
		}
		expectMapping := kmmv1beta1.KernelMapping{
			ContainerImage: "some image:kernelMMP",
			Literal:        literal,
			Regexp:         regexp,
			Build: &kmmv1beta1.Build{
				BuildArgs: []kmmv1beta1.BuildArg{
					{Name: "name1", Value: "value1"},
					{Name: "kernel version", Value: "${KERNEL_FULL_VERSION}"},
				},
				DockerfileConfigMap: &v1.LocalObjectReference{},
			},
		}

		res, err := km.PrepareKernelMapping(&mapping, &osConfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(*res).To(Equal(expectMapping))
	})
})

var _ = Describe("GetNodeOSConfig", func() {
	km := NewKernelMapper(nil, nil)

	It("parsing the node data", func() {
		node := v1.Node{
			Status: v1.NodeStatus{
				NodeInfo: v1.NodeSystemInfo{
					KernelVersion: "4.18.0-305.45.1.el8_4.x86_64",
					OSImage:       "Red Hat Enterprise Linux CoreOS 410.84.202205191234-0 (Ootpa)",
				},
			},
		}

		expectedOSConfig := NodeOSConfig{
			KernelFullVersion:  "4.18.0-305.45.1.el8_4.x86_64",
			KernelVersionMMP:   "4.18.0",
			KernelVersionMajor: "4",
			KernelVersionMinor: "18",
			KernelVersionPatch: "0",
		}

		res := km.GetNodeOSConfig(&node)
		Expect(*res).To(Equal(expectedOSConfig))
	})
})

var _ = Describe("GetNodeOSConfigFromKernelVersion", func() {
	km := NewKernelMapper(nil, nil)

	It("parsing the kernel version", func() {
		kernelVersion := "4.18.0-305.45.1.el8_4.x86_64"

		expectedOSConfig := NodeOSConfig{
			KernelFullVersion:  "4.18.0-305.45.1.el8_4.x86_64",
			KernelVersionMMP:   "4.18.0",
			KernelVersionMajor: "4",
			KernelVersionMinor: "18",
			KernelVersionPatch: "0",
		}

		res := km.GetNodeOSConfigFromKernelVersion(kernelVersion)
		Expect(*res).To(Equal(expectedOSConfig))
	})
})
