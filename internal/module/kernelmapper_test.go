package module

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	v1 "k8s.io/api/core/v1"
)

var _ = Describe("FindMappingForKernel", func() {
	const (
		kernelVersion = "1.2.3"
		selectedImage = "image1"
	)

	km := NewKernelMapper()

	It("should work with one literal mapping", func() {
		mapping := ootov1alpha1.KernelMapping{
			ContainerImage: selectedImage,
			Literal:        "1.2.3",
		}

		m, err := km.FindMappingForKernel([]ootov1alpha1.KernelMapping{mapping}, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(m).To(Equal(&mapping))
	})

	It("should work with one regexp mapping", func() {
		mapping := ootov1alpha1.KernelMapping{
			ContainerImage: selectedImage,
			Regexp:         `1\..*`,
		}

		m, err := km.FindMappingForKernel([]ootov1alpha1.KernelMapping{mapping}, kernelVersion)
		Expect(err).NotTo(HaveOccurred())
		Expect(m).To(Equal(&mapping))
	})

	It("should return an error if a regex is invalid", func() {
		mapping := ootov1alpha1.KernelMapping{
			ContainerImage: selectedImage,
			Regexp:         "invalid)",
		}

		_, err := km.FindMappingForKernel([]ootov1alpha1.KernelMapping{mapping}, kernelVersion)
		Expect(err).To(HaveOccurred())
	})

	It("should return an error if no mapping work", func() {
		mappings := []ootov1alpha1.KernelMapping{
			{
				ContainerImage: selectedImage,
				Regexp:         `1.2.2`,
			},
			{
				ContainerImage: selectedImage,
				Regexp:         `0\..*`,
			},
		}

		_, err := km.FindMappingForKernel(mappings, kernelVersion)
		Expect(err).To(MatchError("no suitable mapping found"))
	})
})

var _ = Describe("PrepareKernelMapping", func() {
	km := NewKernelMapper()
	osConfig := NodeOSConfig{
		kernelFullVersion:  "kernelFullVersion",
		kernelVersionMMP:   "kernelMMP",
		kernelVersionMajor: "kernelMajor",
		kernelVersionMinor: "kernelMinor",
		kernelVersionPatch: "kernelPatch",
		kernelRHELVersion:  "kernelVersion",
		kernelRHELRelease:  "kernelRelease",
		kernelRHELArch:     "kernelArch",
	}

	It("error input", func() {
		mapping := ootov1alpha1.KernelMapping{
			ContainerImage: "some image:${KERNEL_XYZ",
			Literal:        "some literal",
			Regexp:         "regexp",
		}
		_, err := km.PrepareKernelMapping(&mapping, &osConfig)
		Expect(err).To(HaveOccurred())
	})

	It("should only substitute the ContainerImage field", func() {
		const (
			dockerfile = "RUN echo $MYVAR"
			literal    = "some literal:${KERNEL_XYZ"
			regexp     = "some regexp:${KERNEL_XYZ"
		)

		mapping := ootov1alpha1.KernelMapping{
			ContainerImage: "some image:${KERNEL_XYZ}",
			Literal:        literal,
			Regexp:         regexp,
			Build: &ootov1alpha1.Build{
				BuildArgs: []ootov1alpha1.BuildArg{
					{Name: "name1", Value: "value1"},
					{Name: "kernel version", Value: "${KERNEL_FULL_VERSION}"},
				},
				Dockerfile: dockerfile,
			},
		}
		expectMapping := ootov1alpha1.KernelMapping{
			ContainerImage: "some image:kernelMMP",
			Literal:        literal,
			Regexp:         regexp,
			Build: &ootov1alpha1.Build{
				BuildArgs: []ootov1alpha1.BuildArg{
					{Name: "name1", Value: "value1"},
					{Name: "kernel version", Value: "${KERNEL_FULL_VERSION}"},
				},
				Dockerfile: dockerfile,
			},
		}

		res, err := km.PrepareKernelMapping(&mapping, &osConfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(*res).To(Equal(expectMapping))
	})
})

var _ = Describe("GetNodeOSConfig", func() {
	km := NewKernelMapper()

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
			kernelFullVersion:  "4.18.0-305.45.1.el8_4.x86_64",
			kernelVersionMMP:   "4.18.0",
			kernelVersionMajor: "4",
			kernelVersionMinor: "18",
			kernelVersionPatch: "0",
			kernelRHELVersion:  "el8_4",
			kernelRHELRelease:  "305.45.1.el8_4",
			kernelRHELArch:     "x86_64",
		}

		res := km.GetNodeOSConfig(&node)
		Expect(*res).To(Equal(expectedOSConfig))
	})
})
