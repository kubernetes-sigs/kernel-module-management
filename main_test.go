package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	//+kubebuilder:scaffold:imports
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Main Suite")
}

var _ = Describe("GetEnvWithDefault", func() {
	const envVar = "_OOTO_TEST_PRIVATE_VAR"

	It("should return the default value if the variable is undefined", func() {
		const def = "some-default"

		res := GetEnvWithDefault(envVar, def)

		Expect(res).To(Equal(def))
	})

	It("should return the environment variable if it is defined", func() {
		const value = "some-value"

		GinkgoT().Setenv(envVar, value)

		res := GetEnvWithDefault(envVar, "some-default")

		Expect(res).To(Equal(value))
	})
})
