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
		Entry(nil, "kmm.node.kubernetes.io/.b.ready", false, "", ""),
		Entry(nil, "kmm.node.kubernetes.io/a.b", false, "", ""),
		Entry(nil, "kmm.node.kubernetes.io/a.b.read", false, "", ""),
		Entry(nil, "a.b.read", false, "", ""),
		Entry(nil, "kmm.node.kubernetes.io/a.b.ready", true, "a", "b"),
		Entry(nil, "kmm.node.kubernetes.io/a1-2b.c3-4d.ready", true, "a1-2b", "c3-4d"),
	)
})
