package nmc

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ModuleConfiguredLabel", func() {
	It("should work as expected", func() {

		Expect(
			ModuleConfiguredLabel("a", "b"),
		).To(
			Equal("beta.kmm.node.kubernetes.io/a.b.module-configured"),
		)
	})

	It("should work as expected with a dot in the name", func() {

		Expect(
			ModuleConfiguredLabel("a", "b.1"),
		).To(
			Equal("beta.kmm.node.kubernetes.io/a.b.1.module-configured"),
		)
	})
})

var _ = Describe("ModuleInUseLabel", func() {
	It("should work as expected", func() {

		Expect(
			ModuleInUseLabel("a", "b"),
		).To(
			Equal("beta.kmm.node.kubernetes.io/a.b.module-in-use"),
		)
	})

	It("should work as expected with a dot in the name", func() {

		Expect(
			ModuleInUseLabel("a", "b.1"),
		).To(
			Equal("beta.kmm.node.kubernetes.io/a.b.1.module-in-use"),
		)
	})
})

var _ = Describe("IsModuleConfiguredLabel", func() {
	DescribeTable(
		"should work as expected",
		func(input string, expectedOK bool, expectedNS, expectedName string) {
			ok, ns, name := IsModuleConfiguredLabel(input)

			if !expectedOK {
				Expect(ok).To(BeFalse())
				return
			}

			Expect(ok).To(BeTrue())
			Expect(ns).To(Equal(expectedNS))
			Expect(name).To(Equal(expectedName))
		},
		Entry(nil, "a.b.module-in-use", false, "", ""),
		Entry(nil, "beta.kmm.node.kubernetes.io/a.b.module-configured", true, "a", "b"),
		Entry(nil, "beta.kmm.node.kubernetes.io/..module-configured", false, "", ""),
		Entry(nil, "beta.kmm.node.kubernetes.io/a123.b456.module-configured", true, "a123", "b456"),
		Entry(nil, "beta.kmm.node.kubernetes.io/with-hypen.withouthypen.module-configured", true, "with-hypen", "withouthypen"),
		Entry(nil, "beta.kmm.node.kubernetes.io/my-namespace.my-name.stil-my-name.module-configured", true, "my-namespace", "my-name.stil-my-name"),
	)
})

var _ = Describe("IsModuleInUseLabel", func() {
	DescribeTable(
		"should work as expected",
		func(input string, expectedOK bool, expectedNS, expectedName string) {
			ok, ns, name := IsModuleInUseLabel(input)

			if !expectedOK {
				Expect(ok).To(BeFalse())
				return
			}

			Expect(ok).To(BeTrue())
			Expect(ns).To(Equal(expectedNS))
			Expect(name).To(Equal(expectedName))
		},
		Entry(nil, "a.b.module-in-use", false, "", ""),
		Entry(nil, "beta.kmm.node.kubernetes.io/a.b.module-in-use", true, "a", "b"),
		Entry(nil, "beta.kmm.node.kubernetes.io/..module-in-use", false, "", ""),
		Entry(nil, "beta.kmm.node.kubernetes.io/a123.b456.module-in-use", true, "a123", "b456"),
		Entry(nil, "beta.kmm.node.kubernetes.io/with-hypen.withouthypen.module-in-use", true, "with-hypen", "withouthypen"),
		Entry(nil, "beta.kmm.node.kubernetes.io/my-namespace.my-name.stil-my-name.module-in-use", true, "my-namespace", "my-name.stil-my-name"),
	)
})
