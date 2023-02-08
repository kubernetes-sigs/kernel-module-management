package utils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("KernelComponentsAsEnvVars", func() {
	It("should work as expected", func() {
		const kernelVersion = "6.0.15-300.fc37.x86_64"

		expected := []string{
			"KERNEL_FULL_VERSION=" + kernelVersion,
			"KERNEL_VERSION=" + kernelVersion,
			"KERNEL_XYZ=6.0.15",
			"KERNEL_X=6",
			"KERNEL_Y=0",
			"KERNEL_Z=15",
		}

		Expect(KernelComponentsAsEnvVars(kernelVersion)).To(Equal(expected))
	})
})

var _ = Describe("ReplaceInTemplates", func() {
	It("should work as expected", func() {
		vars := []string{"A=AAA", "B=BBB", "C=CCC"}

		templates := []string{
			"string with template $A",
			"string with template ${B}",
			"string without template",
		}

		expected := []string{
			"string with template AAA",
			"string with template BBB",
			"string without template",
		}

		Expect(ReplaceInTemplates(vars, templates...)).To(Equal(expected))
	})
})
