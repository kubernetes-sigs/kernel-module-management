package labels

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("GetModuleReadyAndDevicePluginReadyLabels", func() {
	It("module ready label", func() {
		res := GetKernelModuleReadyNodeLabel("some-namespace", "some-module")
		Expect(res).To(Equal("kmm.node.kubernetes.io/some-namespace.some-module.ready"))
	})

	It("device-plugin ready label", func() {
		res := GetDevicePluginNodeLabel("some-namespace", "some-module")
		Expect(res).To(Equal("kmm.node.kubernetes.io/some-namespace.some-module.device-plugin-ready"))
	})

	It("module version label", func() {
		res := GetModuleVersionLabelName("some-namespace", "some-module")
		Expect(res).To(Equal("kmm.node.kubernetes.io/version-module.some-namespace.some-module"))
	})

	It("module version ready label", func() {
		res := GetKernelModuleVersionReadyNodeLabel("some-namespace", "some-module")
		Expect(res).To(Equal("kmm.node.kubernetes.io/some-namespace.some-module.version.ready"))
	})

})
