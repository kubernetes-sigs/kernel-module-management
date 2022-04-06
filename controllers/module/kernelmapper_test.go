package module

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
)

var _ = Describe("KernelMapper", func() {
	km := NewKernelMapper()

	Describe("FindMappingForKernel", func() {
		const (
			kernelVersion = "1.2.3"
			selectedImage = "image1"
		)

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
})
