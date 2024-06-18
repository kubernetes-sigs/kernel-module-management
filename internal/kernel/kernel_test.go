package kernel

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NormalizeVersion", func() {
	DescribeTable(
		"should work as expected",
		func(version, expected string) {
			Expect(
				NormalizeVersion(version),
			).To(
				Equal(expected),
			)
		},
		Entry(nil, "1.2.3-4", "1.2.3-4"),
		Entry(nil, "1.2.3+4", "1.2.3_4"),
		Entry(nil, "1.2.3_4", "1.2.3_4"),
		Entry(nil, "1.2.3&4", "1.2.3_4"),
		Entry(nil, "1.2.3%4", "1.2.3_4"),
		Entry(nil, "1.2.3?4", "1.2.3_4"),
		Entry(nil, "1.2.3 4", "1.2.3_4"),
	)
})
