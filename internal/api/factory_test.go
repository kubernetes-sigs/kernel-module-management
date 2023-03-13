package api

import (
	"strings"

	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
)

var _ = Describe("moduleLoaderDataFactoryHelper_findKernelMapping", func() {
	const kernelVersion = "1.2.3"

	var (
		ctrl  *gomock.Controller
		mldfh moduleLoaderDataFactoryHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mldfh = &moduleLoaderDataFactoryHelperImpl{
			sectionGetter: NewMocksectionGetter(ctrl),
		}
	})

	It("one literal mapping", func() {
		mapping := kmmv1beta1.KernelMapping{
			Literal: "1.2.3",
		}

		m, err := mldfh.findKernelMapping([]kmmv1beta1.KernelMapping{mapping}, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(m).To(Equal(&mapping))
	})

	It("one regexp mapping", func() {
		mapping := kmmv1beta1.KernelMapping{
			Regexp: `1\..*`,
		}

		m, err := mldfh.findKernelMapping([]kmmv1beta1.KernelMapping{mapping}, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(m).To(Equal(&mapping))
	})

	It("should return an error if a regex is invalid", func() {
		mapping := kmmv1beta1.KernelMapping{
			Regexp: "invalid)",
		}

		m, err := mldfh.findKernelMapping([]kmmv1beta1.KernelMapping{mapping}, kernelVersion)
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

		m, err := mldfh.findKernelMapping(mappings, kernelVersion)
		Expect(err).To(MatchError("no suitable mapping found"))
		Expect(m).To(BeNil())
	})
})

var _ = Describe("moduleLoaderDataFactoryHelper_prepareModuleLoaderData", func() {
	const kernelVersion = "1.2.3"

	var (
		ctrl    *gomock.Controller
		mod     kmmv1beta1.Module
		mapping kmmv1beta1.KernelMapping
		mldfh   *moduleLoaderDataFactoryHelperImpl
		sg      *MocksectionGetter
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		sg = NewMocksectionGetter(ctrl)
		mldfh = &moduleLoaderDataFactoryHelperImpl{sectionGetter: sg}
		mod = kmmv1beta1.Module{}
		mod.Spec.ModuleLoader.Container.ContainerImage = "spec container image"
		mapping = kmmv1beta1.KernelMapping{}
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

		mld := ModuleLoaderData{
			Name:               mod.Name,
			Namespace:          mod.Namespace,
			ImageRepoSecret:    mod.Spec.ImageRepoSecret,
			Owner:              &mod,
			Selector:           mod.Spec.Selector,
			ServiceAccountName: mod.Spec.ModuleLoader.ServiceAccountName,
			Modprobe:           mod.Spec.ModuleLoader.Container.Modprobe,
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
			sg.EXPECT().GetRelevantBuild(mod.Spec.ModuleLoader.Container.Build, mapping.Build).Return(build)
		}
		if signExistsInMapping || SignExistsInModuleSpec {
			mld.Sign = sign
			sg.EXPECT().GetRelevantSign(mod.Spec.ModuleLoader.Container.Sign, mapping.Sign, kernelVersion).Return(sign, nil)
		}

		res, err := mldfh.prepareModuleLoaderData(&mapping, &mod, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(*res).To(Equal(mld))
	},
		Entry("build in mapping only", true, false, false, false, false, false),
		Entry("build in spec only", false, true, false, false, false, false),
		Entry("sign in mapping only", false, false, true, false, false, false),
		Entry("sign in spec only", false, false, false, true, false, false),
		Entry("registryTLS in mapping", false, false, false, false, true, false),
		Entry("containerImage in mapping", false, false, false, false, false, true),
	)
})

var _ = Describe("moduleLoaderDataFactoryHelper_replaceTemplates", func() {
	const kernelVersion = "5.8.18-100.fc31.x86_64"

	mdlfh := moduleLoaderDataFactoryHelperImpl{}

	It("error input", func() {
		mld := ModuleLoaderData{
			ContainerImage: "some image:${KERNEL_XYZ",
			KernelVersion:  kernelVersion,
		}
		err := mdlfh.replaceTemplates(&mld)
		Expect(err).To(HaveOccurred())
	})

	It("should only substitute the ContainerImage field", func() {
		mld := ModuleLoaderData{
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
		expectMld := ModuleLoaderData{
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

		err := mdlfh.replaceTemplates(&mld)
		Expect(err).NotTo(HaveOccurred())
		Expect(mld).To(Equal(expectMld))
	})

})

var _ = Describe("sectionGetterImpl_GetRelevantBuild", func() {
	sg := sectionGetterImpl{}

	It("kernel mapping build present, module loader build absent", func() {
		mappingBuild := &kmmv1beta1.Build{
			DockerfileConfigMap: &v1.LocalObjectReference{Name: "some kernel mapping build name"},
		}

		res := sg.GetRelevantBuild(nil, mappingBuild)
		Expect(res).To(Equal(mappingBuild))
	})

	It("kernel mapping build absent, module loader build present", func() {
		moduleBuild := &kmmv1beta1.Build{
			DockerfileConfigMap: &v1.LocalObjectReference{Name: "some load module build name"},
		}

		res := sg.GetRelevantBuild(moduleBuild, nil)

		Expect(res).To(Equal(moduleBuild))
	})

	It("kernel mapping and module loader builds are present, overrides", func() {
		moduleBuild := &kmmv1beta1.Build{
			DockerfileConfigMap: &v1.LocalObjectReference{Name: "some load module build name"},
			BaseImageRegistryTLS: kmmv1beta1.TLSOptions{
				Insecure:              true,
				InsecureSkipTLSVerify: true,
			},
		}
		mappingBuild := &kmmv1beta1.Build{
			DockerfileConfigMap: &v1.LocalObjectReference{Name: "some kernel mapping build name"},
		}

		res := sg.GetRelevantBuild(moduleBuild, mappingBuild)
		Expect(res.DockerfileConfigMap).To(Equal(mappingBuild.DockerfileConfigMap))
		Expect(res.BaseImageRegistryTLS).To(Equal(moduleBuild.BaseImageRegistryTLS))
	})
})

var _ = Describe("sectionGetterImpl_GetRelevantSign", func() {

	const (
		unsignedImage = "my.registry/my/image"
		keySecret     = "securebootkey"
		certSecret    = "securebootcert"
		filesToSign   = "/modules/simple-kmod.ko:/modules/simple-procfs-kmod.ko"
		kernelVersion = "1.2.3"
	)

	sg := sectionGetterImpl{}

	expected := &kmmv1beta1.Sign{
		UnsignedImage: unsignedImage,
		KeySecret:     &v1.LocalObjectReference{Name: keySecret},
		CertSecret:    &v1.LocalObjectReference{Name: certSecret},
		FilesToSign:   strings.Split(filesToSign, ":"),
	}

	DescribeTable("should set fields correctly", func(moduleSign *kmmv1beta1.Sign, mappingSign *kmmv1beta1.Sign) {
		actual, err := sg.GetRelevantSign(moduleSign, mappingSign, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(
			cmp.Diff(expected, actual),
		).To(
			BeEmpty(),
		)
	},
		Entry(
			"no km.Sign",
			&kmmv1beta1.Sign{
				UnsignedImage: unsignedImage,
				KeySecret:     &v1.LocalObjectReference{Name: keySecret},
				CertSecret:    &v1.LocalObjectReference{Name: certSecret},
				FilesToSign:   strings.Split(filesToSign, ":"),
			},
			nil,
		),
		Entry(
			"no container.Sign",
			nil,
			&kmmv1beta1.Sign{
				UnsignedImage: unsignedImage,
				KeySecret:     &v1.LocalObjectReference{Name: keySecret},
				CertSecret:    &v1.LocalObjectReference{Name: certSecret},
				FilesToSign:   strings.Split(filesToSign, ":"),
			},
		),
		Entry(
			"default UnsignedImage",
			&kmmv1beta1.Sign{
				UnsignedImage: unsignedImage,
			},
			&kmmv1beta1.Sign{
				KeySecret:   &v1.LocalObjectReference{Name: keySecret},
				CertSecret:  &v1.LocalObjectReference{Name: certSecret},
				FilesToSign: strings.Split(filesToSign, ":"),
			},
		),
		Entry(
			"default UnsignedImage and KeySecret",
			&kmmv1beta1.Sign{
				UnsignedImage: unsignedImage,
				KeySecret:     &v1.LocalObjectReference{Name: keySecret},
			},
			&kmmv1beta1.Sign{
				CertSecret:  &v1.LocalObjectReference{Name: certSecret},
				FilesToSign: strings.Split(filesToSign, ":"),
			},
		),
		Entry(
			"default UnsignedImage, KeySecret, and CertSecret",
			&kmmv1beta1.Sign{
				UnsignedImage: unsignedImage,
				KeySecret:     &v1.LocalObjectReference{Name: keySecret},
				CertSecret:    &v1.LocalObjectReference{Name: certSecret},
			},
			&kmmv1beta1.Sign{
				FilesToSign: strings.Split(filesToSign, ":"),
			},
		),
		Entry(
			"default FilesToSign only",
			&kmmv1beta1.Sign{
				FilesToSign: strings.Split(filesToSign, ":"),
			},
			&kmmv1beta1.Sign{
				UnsignedImage: unsignedImage,
				KeySecret:     &v1.LocalObjectReference{Name: keySecret},
				CertSecret:    &v1.LocalObjectReference{Name: certSecret},
			},
		),
	)

})
