package api

import (
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ModuleLoaderData_BuildConfigured", func() {
	DescribeTable("works as expected",
		func(mld *ModuleLoaderData, expected bool) {
			Expect(mld.BuildConfigured()).To(Equal(expected))
		},
		Entry(nil, &ModuleLoaderData{}, false),
		Entry(nil, &ModuleLoaderData{Build: &kmmv1beta1.Build{}}, true),
	)
})

var _ = Describe("ModuleLoaderData_BuildDestinationImage", func() {
	DescribeTable("works as expected",
		func(mld *ModuleLoaderData, expected string, returnsError bool) {
			expect := Expect(mld.BuildDestinationImage())

			if returnsError {
				expect.Error().To(HaveOccurred())
				return
			}

			expect.To(Equal(expected))
		},
		Entry(
			"no build configured",
			&ModuleLoaderData{},
			"",
			true,
		),
		Entry(
			"build configured, sign not configured",
			&ModuleLoaderData{Build: &kmmv1beta1.Build{}, ContainerImage: "test"},
			"test",
			false,
		),
		Entry(
			"build and sign configured",
			&ModuleLoaderData{
				Build:          &kmmv1beta1.Build{},
				Sign:           &kmmv1beta1.Sign{},
				ContainerImage: "test",
				Namespace:      "ns",
				Name:           "name",
			},
			"test:ns_name_kmm_unsigned",
			false,
		),
	)
})

var _ = Describe("ModuleLoaderData_SignConfigured", func() {
	DescribeTable("works as expected",
		func(mld *ModuleLoaderData, expected bool) {
			Expect(mld.SignConfigured()).To(Equal(expected))
		},
		Entry(nil, &ModuleLoaderData{}, false),
		Entry(nil, &ModuleLoaderData{Sign: &kmmv1beta1.Sign{}}, true),
	)
})

var _ = Describe("ModuleLoaderData_UnsignedImage", func() {
	When("an unsigned image is specified", func() {
		const unsignedImage = "other-repo/other-image:other-tag"

		mld := ModuleLoaderData{
			Name:           "test-module",
			Namespace:      "test-namespace",
			ContainerImage: "registry.com/repo/image:tag",
			Sign:           &kmmv1beta1.Sign{UnsignedImage: unsignedImage},
		}

		It("should return it when no build section is present", func() {
			Expect(
				mld.UnsignedImage(),
			).To(
				Equal(unsignedImage),
			)
		})

		It("should ignore it if a build section is present", func() {
			mld.Build = &kmmv1beta1.Build{}

			Expect(
				mld.UnsignedImage(),
			).To(
				Equal("registry.com/repo/image:tag_test-namespace_test-module_kmm_unsigned"),
			)
		})
	})
})

var _ = Describe("ModuleLoaderData_intermediateImageName", func() {
	DescribeTable("works as expected",
		func(mld *ModuleLoaderData, expected string, returnsError bool) {
			expect := Expect(mld.intermediateImageName())

			if returnsError {
				expect.Error().To(HaveOccurred())
				return
			}

			expect.To(Equal(expected))
		},
		Entry(
			"empty containerImage",
			&ModuleLoaderData{},
			"",
			true,
		),
		Entry(
			"bad containerImage",
			&ModuleLoaderData{ContainerImage: "bad@name"},
			"",
			true,
		),
		Entry(
			"containerImage without host and label",
			&ModuleLoaderData{ContainerImage: "name", Namespace: "ns", Name: "name"},
			"name:ns_name_kmm_unsigned",
			false,
		),
		Entry(
			"containerImage with a host but no label",
			&ModuleLoaderData{ContainerImage: "example.org/name", Namespace: "ns", Name: "name"},
			"example.org/name:ns_name_kmm_unsigned",
			false,
		),
		Entry(
			"containerImage with a host and a label",
			&ModuleLoaderData{ContainerImage: "example.org/name:test", Namespace: "ns", Name: "name"},
			"example.org/name:test_ns_name_kmm_unsigned",
			false,
		),
	)
})
