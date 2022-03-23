package module

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
)

var _ = Describe("KernelMapper", func() {
	km := NewKernelMapper()

	Describe("FindImageForKernel", func() {
		const (
			kernelVersion = "1.2.3"
			selectedImage = "image1"
		)

		It("should work with one literal mapping", func() {
			mappings := []ootov1beta1.KernelMapping{
				{
					ContainerImage: selectedImage,
					Literal:        "1.2.3",
				},
			}

			m, err := km.FindImageForKernel(mappings, kernelVersion)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(Equal(selectedImage))
		})

		It("should work with one regexp mapping", func() {
			mappings := []ootov1beta1.KernelMapping{
				{
					ContainerImage: selectedImage,
					Regexp:         `1\..*`,
				},
			}

			m, err := km.FindImageForKernel(mappings, kernelVersion)
			Expect(err).NotTo(HaveOccurred())
			Expect(m).To(Equal(selectedImage))
		})

		It("should return an error if a regex is invalid", func() {
			mappings := []ootov1beta1.KernelMapping{
				{
					ContainerImage: selectedImage,
					Regexp:         "invalid)",
				},
			}

			_, err := km.FindImageForKernel(mappings, kernelVersion)
			Expect(err).To(HaveOccurred())
		})

		It("should return an error if no mapping work", func() {
			mappings := []ootov1beta1.KernelMapping{
				{
					ContainerImage: selectedImage,
					Regexp:         `1.2.2`,
				},
				{
					ContainerImage: selectedImage,
					Regexp:         `0\..*`,
				},
			}

			_, err := km.FindImageForKernel(mappings, kernelVersion)
			Expect(err).To(MatchError("no suitable mapping found"))
		})
	})
})
