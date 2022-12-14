package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func Test(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Main Suite")
}

var _ = Describe("mode_String", func() {
	It("should return the internal mode field", func() {
		const value = "random-value"

		Expect(
			(&mode{value: value}).String(),
		).To(
			Equal(value),
		)
	})
})

var _ = Describe("mode_Set", func() {
	kmm := &mode{
		value: "kmm",

		builds:                              true,
		signs:                               true,
		startsModuleReconciler:              true,
		startsNodeKernelReconciler:          true,
		startsPodNodeModuleReconciler:       true,
		startsPreflightValidationReconciler: true,
	}

	hub := &mode{
		value: "hub",

		builds:                               true,
		signs:                                true,
		startsManagedClusterModuleReconciler: true,
	}

	spoke := &mode{
		value: "spoke",

		builds:                                 false,
		signs:                                  false,
		startsModuleReconciler:                 true,
		startsNodeKernelClusterClaimReconciler: true,
		startsNodeKernelReconciler:             true,
		startsPodNodeModuleReconciler:          true,
	}

	DescribeTable(
		"should have the expected values",
		func(m *mode, returnsError bool) {
			populatedMode := &mode{}

			err := populatedMode.Set(m.value)

			if returnsError {
				Expect(err).To(HaveOccurred())
				return
			}

			Expect(err).NotTo(HaveOccurred())
			Expect(populatedMode).To(Equal(m))
		},
		Entry(nil, kmm, false),
		Entry(nil, hub, false),
		Entry(nil, spoke, false),
		Entry(nil, &mode{value: "test"}, true),
	)
})
