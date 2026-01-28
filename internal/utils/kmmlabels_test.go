package utils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetModuleVersionLabelName", func() {
	It("should work as expected", func() {
		res := GetModuleVersionLabelName("some-namespace", "some-name")
		Expect(res).To(Equal("kmm.node.kubernetes.io/version-module.some-namespace.some-name"))
	})
})

var _ = Describe("GetWorkerPodVersionLabelName", func() {
	It("should work as expected", func() {
		res := GetWorkerPodVersionLabelName("some-namespace", "some-name")
		Expect(res).To(Equal("beta.kmm.node.kubernetes.io/version-worker-pod.some-namespace.some-name"))
	})
})

var _ = Describe("GetDevicePluginVersionLabelName", func() {
	It("should work as expected", func() {
		res := GetDevicePluginVersionLabelName("some-namespace", "some-name")
		Expect(res).To(Equal("beta.kmm.node.kubernetes.io/version-device-plugin.some-namespace.some-name"))
	})
})

var _ = Describe("GetNamespaceNameFromVersionLabel", func() {
	DescribeTable("should return correct name and namespace",
		func(versionLabel, expectedNamespace, expectedName string, expectsErr bool) {
			namespace, name, err := GetNamespaceNameFromVersionLabel(versionLabel)

			if expectsErr {
				Expect(err).To(HaveOccurred())
				return
			}

			Expect(namespace).To(Equal(expectedNamespace))
			Expect(name).To(Equal(expectedName))
		},
		Entry("workerPod label", "beta.kmm.node.kubernetes.io/version-worker-pod.some-namespace.some-name", "some-namespace", "some-name", false),
		Entry("devicePlugin label", "beta.kmm.node.kubernetes.io/version-device-plugin.some-namespace.some-name", "some-namespace", "some-name", false),
		Entry("module label", "kmm.node.kubernetes.io/version-module.some-namespace.some-name", "some-namespace", "some-name", false),
		Entry("with error", "version-module-some-namespace-some-name", "some-namespace", "some-name", true),
	)
})

var _ = Describe("IsDeprecatedKernelModuleReadyNodeLabel", func() {
	DescribeTable(
		"should work as expected",
		func(input string, expected bool) {
			Expect(
				IsDeprecatedKernelModuleReadyNodeLabel(input),
			).To(
				Equal(expected),
			)
		},
		Entry(nil, "kmm.node.kubernetes.io/a.ready", true),
		Entry(nil, "kmm.node.kubernetes.io/1.ready", true),
		Entry(nil, "kmm.node.kubernetes.io/with-hyphen.ready", true),
		Entry(nil, "test.ready", false),
		Entry(nil, "kmm.node.kubernetes.io/ns.name.ready", false),
		Entry(nil, "kmm.node.kubernetes.io/..ready", false),
	)
})

var _ = Describe("IsKernelModuleVersionReadyNodeLabel", func() {
	DescribeTable(
		"should work as expected",
		func(input string, expected bool) {
			Expect(
				IsKernelModuleVersionReadyNodeLabel(input),
			).To(
				Equal(expected),
			)
		},
		Entry(nil, "kmm.node.kubernetes.io/ns.name.version.ready", true),
		Entry(nil, "kmm.node.kubernetes.io/ns.dot.in.name.version.ready", true),
		Entry(nil, "kmm.node.kubernetes.io/ns.name.ready", false),
		Entry(nil, "kmm.node.kubernetes.io/ns.dot.in.name.ready", false),
	)
})

var _ = Describe("IsKernelModuleReadyNodeLabel", func() {
	DescribeTable(
		"should work as expected",
		func(input string, expectedOK bool, expectedNamespace, expectedName string) {
			ok, namespace, name := IsKernelModuleReadyNodeLabel(input)

			if !expectedOK {
				Expect(ok).To(BeFalse())
				return
			}

			Expect(ok).To(BeTrue())
			Expect(namespace).To(Equal(expectedNamespace))
			Expect(name).To(Equal(expectedName))
		},
		Entry(nil, "kmm.node.kubernetes.io/..ready", false, "", ""),
		Entry(nil, "kmm.node.kubernetes.io/a..ready", false, "", ""),
		Entry(nil, "kmm.node.kubernetes.io/a..ready", false, "", ""),
		Entry(nil, "kmm.node.kubernetes.io/.b.ready", false, "", ""),
		Entry(nil, "kmm.node.kubernetes.io/a.ready", false, "", ""),
		Entry(nil, "kmm.node.kubernetes.io/a.b", false, "", ""),
		Entry(nil, "kmm.node.kubernetes.io/a.b.read", false, "", ""),
		Entry(nil, "a.b.read", false, "", ""),
		Entry(nil, "kmm.node.kubernetes.io/a.b.ready", true, "a", "b"),
		Entry(nil, "kmm.node.kubernetes.io/a1-2b.c3-4d.ready", true, "a1-2b", "c3-4d"),
		Entry(nil, "kmm.node.kubernetes.io/ns.dot.in.name.ready", true, "ns", "dot.in.name"),
		Entry(nil, "kmm.node.kubernetes.io/ns.name.version.ready", false, "", ""),
		Entry(nil, "kmm.node.kubernetes.io/ns.dot.in.name.version.ready", false, "", ""),
	)
})
