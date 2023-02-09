package module

import (
	"fmt"
	"github.com/golang/mock/gomock"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
)

var _ = Describe("GetMergedMappingForKernel", func() {
	const (
		kernelVersion = "1.2.3"
		selectedImage = "image1"
	)

	var (
		ctrl    *gomock.Controller
		kh      *MockkernelMapperHelperAPI
		km      *kernelMapper
		modSpec kmmv1beta1.ModuleSpec
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		kh = NewMockkernelMapperHelperAPI(ctrl)
		km = &kernelMapper{helper: kh}
		modSpec = kmmv1beta1.ModuleSpec{}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("good flow", func() {
		mapping := kmmv1beta1.KernelMapping{}
		kh.EXPECT().findKernelMapping(modSpec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mapping, nil)
		kh.EXPECT().mergeMappingData(&mapping, &modSpec, kernelVersion).Return(nil)
		kh.EXPECT().replaceTemplates(&mapping, kernelVersion).Return(nil)
		res, err := km.GetMergedMappingForKernel(&modSpec, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(&mapping))
	})

	It("failed to find kernel mapping", func() {
		kh.EXPECT().findKernelMapping(modSpec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(nil, fmt.Errorf("some error"))
		res, err := km.GetMergedMappingForKernel(&modSpec, kernelVersion)
		Expect(err).To(HaveOccurred())
		Expect(res).To(BeNil())
	})

	It("failed to merge mapping data", func() {
		mapping := kmmv1beta1.KernelMapping{}
		kh.EXPECT().findKernelMapping(modSpec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mapping, nil)
		kh.EXPECT().mergeMappingData(&mapping, &modSpec, kernelVersion).Return(fmt.Errorf("some error"))
		res, err := km.GetMergedMappingForKernel(&modSpec, kernelVersion)
		Expect(err).To(HaveOccurred())
		Expect(res).To(BeNil())
	})

	It("failed to replace templates", func() {
		mapping := kmmv1beta1.KernelMapping{}
		kh.EXPECT().findKernelMapping(modSpec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mapping, nil)
		kh.EXPECT().mergeMappingData(&mapping, &modSpec, kernelVersion).Return(nil)
		kh.EXPECT().replaceTemplates(&mapping, kernelVersion).Return(fmt.Errorf("some error"))
		res, err := km.GetMergedMappingForKernel(&modSpec, kernelVersion)
		Expect(err).To(HaveOccurred())
		Expect(res).To(BeNil())
	})
})

var _ = Describe("findKernelMapping", func() {
	const (
		kernelVersion = "1.2.3"
	)

	var (
		ctrl    *gomock.Controller
		kh      kernelMapperHelperAPI
		modSpec kmmv1beta1.ModuleSpec
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		kh = newKernelMapperHelper(nil, nil)
		modSpec = kmmv1beta1.ModuleSpec{}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("one literal mapping", func() {
		mapping := kmmv1beta1.KernelMapping{
			Literal: "1.2.3",
		}

		m, err := kh.findKernelMapping([]kmmv1beta1.KernelMapping{mapping}, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(m).To(Equal(&mapping))
	})

	It("one regexp mapping", func() {
		mapping := kmmv1beta1.KernelMapping{
			Regexp: `1\..*`,
		}

		m, err := kh.findKernelMapping([]kmmv1beta1.KernelMapping{mapping}, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(m).To(Equal(&mapping))
	})

	It("should return an error if a regex is invalid", func() {
		mapping := kmmv1beta1.KernelMapping{
			Regexp: "invalid)",
		}
		modSpec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{mapping}

		m, err := kh.findKernelMapping([]kmmv1beta1.KernelMapping{mapping}, kernelVersion)
		Expect(err).To(HaveOccurred())
		Expect(m).To(BeNil())
	})

	It("should return an error if no mapping work", func() {
		mappings := []kmmv1beta1.KernelMapping{
			{
				Regexp: `1.2.2`,
			},
			{
				Regexp: `0\..*`,
			},
		}

		m, err := kh.findKernelMapping(mappings, kernelVersion)
		Expect(err).To(MatchError("no suitable mapping found"))
		Expect(m).To(BeNil())
	})
})

var _ = Describe("mergeMappingData", func() {
	const (
		kernelVersion = "1.2.3"
		selectedImage = "image1"
	)

	var (
		ctrl            *gomock.Controller
		buildHelper     *build.MockHelper
		signHelper      *sign.MockHelper
		kh              kernelMapperHelperAPI
		modSpec         kmmv1beta1.ModuleSpec
		mapping         kmmv1beta1.KernelMapping
		expectedMapping kmmv1beta1.KernelMapping
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		buildHelper = build.NewMockHelper(ctrl)
		signHelper = sign.NewMockHelper(ctrl)
		kh = newKernelMapperHelper(buildHelper, signHelper)
		modSpec = kmmv1beta1.ModuleSpec{}
		modSpec.ModuleLoader.Container.ContainerImage = "spec container image"
		mapping = kmmv1beta1.KernelMapping{}
		expectedMapping = kmmv1beta1.KernelMapping{}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	DescribeTable("prepare mapping", func(buildExistsInMapping, buildExistsInModuleSpec, signExistsInMapping, SignExistsInModuleSpec,
		registryTLSExistsInMapping, containerImageExistsInMapping bool) {
		build := &kmmv1beta1.Build{
			DockerfileConfigMap: &v1.LocalObjectReference{
				Name: "some name",
			},
		}
		sign := &kmmv1beta1.Sign{
			UnsignedImage: "some unsigned image",
		}
		registryTSL := &kmmv1beta1.TLSOptions{
			Insecure: true,
		}
		if buildExistsInMapping {
			mapping.Build = build
		}
		if buildExistsInModuleSpec {
			modSpec.ModuleLoader.Container.Build = build
		}
		if signExistsInMapping {
			mapping.Sign = sign
		}
		if SignExistsInModuleSpec {
			modSpec.ModuleLoader.Container.Sign = sign
		}
		expectedMapping.RegistryTLS = &modSpec.ModuleLoader.Container.RegistryTLS
		if registryTLSExistsInMapping {
			mapping.RegistryTLS = registryTSL
			expectedMapping.RegistryTLS = registryTSL
		}
		expectedMapping.ContainerImage = modSpec.ModuleLoader.Container.ContainerImage
		if containerImageExistsInMapping {
			mapping.ContainerImage = "mapping container image"
			expectedMapping.ContainerImage = mapping.ContainerImage
		}

		if buildExistsInMapping || buildExistsInModuleSpec {
			expectedMapping.Build = build
			buildHelper.EXPECT().GetRelevantBuild(modSpec.ModuleLoader.Container.Build, mapping.Build).Return(build)
		}
		if signExistsInMapping || SignExistsInModuleSpec {
			expectedMapping.Sign = sign
			signHelper.EXPECT().GetRelevantSign(modSpec.ModuleLoader.Container.Sign, mapping.Sign, kernelVersion).Return(sign, nil)
		}

		err := kh.mergeMappingData(&mapping, &modSpec, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(mapping).To(Equal(expectedMapping))
	},
		Entry("build in mapping only", true, false, false, false, false, false),
		Entry("build in spec only", false, true, false, false, false, false),
		Entry("sign in mapping only", false, false, true, false, false, false),
		Entry("sign in spec only", false, false, false, true, false, false),
		Entry("registryTLS in mapping", false, false, false, false, true, false),
		Entry("containerImage in mapping", false, false, false, false, false, true),
	)
})

var _ = Describe("replaceTemplates", func() {
	const kernelVersion = "5.8.18-100.fc31.x86_64"

	kh := newKernelMapperHelper(nil, nil)

	It("error input", func() {
		mapping := kmmv1beta1.KernelMapping{
			ContainerImage: "some image:${KERNEL_XYZ",
			Literal:        "some literal",
			Regexp:         "regexp",
		}
		err := kh.replaceTemplates(&mapping, kernelVersion)
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
			ContainerImage: "some image:5.8.18",
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

		err := kh.replaceTemplates(&mapping, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(mapping).To(Equal(expectMapping))
	})

})
