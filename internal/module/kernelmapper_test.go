package module

import (
	"fmt"
	"github.com/golang/mock/gomock"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
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
		ctrl *gomock.Controller
		kh   *MockkernelMapperHelperAPI
		km   *kernelMapper
		mod  kmmv1beta1.Module
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		kh = NewMockkernelMapperHelperAPI(ctrl)
		km = &kernelMapper{helper: kh}
		mod = kmmv1beta1.Module{}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("good flow", func() {
		mapping := kmmv1beta1.KernelMapping{}
		mld := api.ModuleLoaderData{KernelVersion: kernelVersion}
		kh.EXPECT().findKernelMapping(mod.Spec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mapping, nil)
		kh.EXPECT().prepareModuleLoaderData(&mapping, &mod, kernelVersion).Return(&mld, nil)
		kh.EXPECT().replaceTemplates(&mld).Return(nil)
		res, err := km.GetModuleLoaderDataForKernel(&mod, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(&mld))
	})

	It("failed to find kernel mapping", func() {
		kh.EXPECT().findKernelMapping(mod.Spec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(nil, fmt.Errorf("some error"))
		res, err := km.GetModuleLoaderDataForKernel(&mod, kernelVersion)
		Expect(err).To(HaveOccurred())
		Expect(res).To(BeNil())
	})

	It("failed to merge mapping data", func() {
		mapping := kmmv1beta1.KernelMapping{}
		kh.EXPECT().findKernelMapping(mod.Spec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mapping, nil)
		kh.EXPECT().prepareModuleLoaderData(&mapping, &mod, kernelVersion).Return(nil, fmt.Errorf("some error"))
		res, err := km.GetModuleLoaderDataForKernel(&mod, kernelVersion)
		Expect(err).To(HaveOccurred())
		Expect(res).To(BeNil())
	})

	It("failed to replace templates", func() {
		mapping := kmmv1beta1.KernelMapping{}
		mld := api.ModuleLoaderData{KernelVersion: kernelVersion}
		kh.EXPECT().findKernelMapping(mod.Spec.ModuleLoader.Container.KernelMappings, kernelVersion).Return(&mapping, nil)
		kh.EXPECT().prepareModuleLoaderData(&mapping, &mod, kernelVersion).Return(&mld, nil)
		kh.EXPECT().replaceTemplates(&mld).Return(fmt.Errorf("some error"))
		res, err := km.GetModuleLoaderDataForKernel(&mod, kernelVersion)
		Expect(err).To(HaveOccurred())
		Expect(res).To(BeNil())
	})
})

var _ = Describe("findKernelMapping", func() {
	const (
		kernelVersion = "1.2.3"
	)

	var (
		ctrl *gomock.Controller
		kh   kernelMapperHelperAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		kh = newKernelMapperHelper(nil, nil)
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

var _ = Describe("prepareModuleLoaderData", func() {
	const (
		kernelVersion = "1.2.3"
		selectedImage = "image1"
	)

	var (
		ctrl        *gomock.Controller
		buildHelper *build.MockHelper
		signHelper  *sign.MockHelper
		kh          kernelMapperHelperAPI
		mod         kmmv1beta1.Module
		mapping     kmmv1beta1.KernelMapping
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		buildHelper = build.NewMockHelper(ctrl)
		signHelper = sign.NewMockHelper(ctrl)
		kh = newKernelMapperHelper(buildHelper, signHelper)
		mod = kmmv1beta1.Module{}
		mod.Spec.ModuleLoader.Container.ContainerImage = "spec container image"
		mod.Spec.ModuleLoader.Container.ImagePullPolicy = "Always"
		mapping = kmmv1beta1.KernelMapping{}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	DescribeTable("prepare mapping", func(buildExistsInMapping, buildExistsInModuleSpec, signExistsInMapping, SignExistsInModuleSpec,
		registryTLSExistsInMapping, containerImageExistsInMapping, inTreeModuleToRemoveExistsInMapping bool) {
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

		mld := api.ModuleLoaderData{
			Name:               mod.Name,
			Namespace:          mod.Namespace,
			ImageRepoSecret:    mod.Spec.ImageRepoSecret,
			Owner:              &mod,
			Selector:           mod.Spec.Selector,
			ServiceAccountName: mod.Spec.ModuleLoader.ServiceAccountName,
			Modprobe:           mod.Spec.ModuleLoader.Container.Modprobe,
			ImagePullPolicy:    mod.Spec.ModuleLoader.Container.ImagePullPolicy,
			KernelVersion:      kernelVersion,
		}

		if buildExistsInMapping {
			mapping.Build = build
		}
		if buildExistsInModuleSpec {
			mod.Spec.ModuleLoader.Container.Build = build
		}
		if signExistsInMapping {
			mapping.Sign = sign
		}
		if SignExistsInModuleSpec {
			mod.Spec.ModuleLoader.Container.Sign = sign
		}
		mld.RegistryTLS = &mod.Spec.ModuleLoader.Container.RegistryTLS
		if registryTLSExistsInMapping {
			mapping.RegistryTLS = registryTSL
			mld.RegistryTLS = registryTSL
		}
		mld.ContainerImage = mod.Spec.ModuleLoader.Container.ContainerImage
		if containerImageExistsInMapping {
			mapping.ContainerImage = "mapping container image"
			mld.ContainerImage = mapping.ContainerImage
		}

		if buildExistsInMapping || buildExistsInModuleSpec {
			mld.Build = build
			buildHelper.EXPECT().GetRelevantBuild(mod.Spec.ModuleLoader.Container.Build, mapping.Build).Return(build)
		}
		if signExistsInMapping || SignExistsInModuleSpec {
			mld.Sign = sign
			signHelper.EXPECT().GetRelevantSign(mod.Spec.ModuleLoader.Container.Sign, mapping.Sign, kernelVersion).Return(sign, nil)
		}
		if inTreeModuleToRemoveExistsInMapping {
			mld.InTreeModuleToRemove = "some module"
			mapping.InTreeModuleToRemove = "some module"
		}

		res, err := kh.prepareModuleLoaderData(&mapping, &mod, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(*res).To(Equal(mld))
	},
		Entry("build in mapping only", true, false, false, false, false, false, false),
		Entry("build in spec only", false, true, false, false, false, false, false),
		Entry("sign in mapping only", false, false, true, false, false, false, false),
		Entry("sign in spec only", false, false, false, true, false, false, false),
		Entry("registryTLS in mapping", false, false, false, false, true, false, false),
		Entry("containerImage in mapping", false, false, false, false, false, true, false),
		Entry("inTreeModuleToRemove in mapping", false, false, false, false, false, false, true),
	)
})

var _ = Describe("replaceTemplates", func() {
	const kernelVersion = "5.8.18-100.fc31.x86_64"

	kh := newKernelMapperHelper(nil, nil)

	It("error input", func() {
		mld := api.ModuleLoaderData{
			ContainerImage: "some image:${KERNEL_XYZ",
			KernelVersion:  kernelVersion,
		}
		err := kh.replaceTemplates(&mld)
		Expect(err).To(HaveOccurred())
	})

	It("should only substitute the ContainerImage field", func() {
		mld := api.ModuleLoaderData{
			ContainerImage: "some image:${KERNEL_XYZ}",
			Build: &kmmv1beta1.Build{
				BuildArgs: []kmmv1beta1.BuildArg{
					{Name: "name1", Value: "value1"},
					{Name: "kernel version", Value: "${KERNEL_FULL_VERSION}"},
				},
				DockerfileConfigMap: &v1.LocalObjectReference{},
			},
			KernelVersion: kernelVersion,
		}
		expectMld := api.ModuleLoaderData{
			ContainerImage: "some image:5.8.18",
			Build: &kmmv1beta1.Build{
				BuildArgs: []kmmv1beta1.BuildArg{
					{Name: "name1", Value: "value1"},
					{Name: "kernel version", Value: "${KERNEL_FULL_VERSION}"},
				},
				DockerfileConfigMap: &v1.LocalObjectReference{},
			},
			KernelVersion: kernelVersion,
		}

		err := kh.replaceTemplates(&mld)
		Expect(err).NotTo(HaveOccurred())
		Expect(mld).To(Equal(expectMld))
	})

})
