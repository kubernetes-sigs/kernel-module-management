package module

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("AppendToTag", func() {
	It("should append a tag to the image name", func() {
		name := "some-container-image-name"
		tag := "a-kmm-tag"

		Expect(
			AppendToTag(name, tag),
		).To(
			Equal(name + ":" + tag),
		)
	})

	It("should add a suffix to the already present tag", func() {
		name := "some-container-image-name:with-a-tag"
		tag := "a-kmm-tag-suffix"

		Expect(
			AppendToTag(name, tag),
		).To(
			Equal(name + "_" + tag),
		)
	})
})

var _ = Describe("IntermediateImageName", func() {
	It("should add the kmm_unsigned suffix to the target image name", func() {
		Expect(
			IntermediateImageName("module-name", "test-namespace", "some-image-name"),
		).To(
			Equal("some-image-name:test-namespace_module-name_kmm_unsigned"),
		)
	})
})
