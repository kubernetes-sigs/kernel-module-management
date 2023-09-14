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
