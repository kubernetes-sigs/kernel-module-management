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
})
