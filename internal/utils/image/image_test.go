package image_test

import (
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils/image"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SetOrAppendTag", func() {
	DescribeTable(
		"should work as expected",
		func(imageName, tag, sep, outputName string, expectsError bool) {
			expect := Expect(
				image.SetOrAppendTag(imageName, tag, sep),
			)

			if expectsError {
				expect.Error().To(HaveOccurred())
				return
			}

			expect.To(Equal(outputName))
		},
		Entry(nil, "example.org/repo/image:my-tag", "extra", "_", "example.org/repo/image:my-tag_extra", false),
		Entry(nil, "example.org/repo/image", "extra", "_", "example.org/repo/image:extra", false),
		Entry(nil, "", "", "", "", true),
		Entry(nil, "example.org/repo/image@sha256:62fa0f2b9e7cabfb58d8ef34eaacd22e17e0d342c15a8e061bf814780aa3427a", "extra", "_", "example.org/repo/image:extra", false),
	)
})
