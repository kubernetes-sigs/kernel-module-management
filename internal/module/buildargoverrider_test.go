package module

import (
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ApplyBuildArgOverrides", func() {

	var bao BuildArgOverrider

	BeforeEach(func() {
		bao = NewBuildArgOverrider()
	})

	It("apply overrides", func() {
		args := []kmmv1beta1.BuildArg{
			{
				Name:  "name1",
				Value: "value1",
			},
			{
				Name:  "name2",
				Value: "value2",
			},
		}
		overrides := []kmmv1beta1.BuildArg{
			{
				Name:  "name1",
				Value: "valueOverride1",
			},
			{
				Name:  "overrideName2",
				Value: "overrideValue2",
			},
		}

		expected := []kmmv1beta1.BuildArg{
			{
				Name:  "name1",
				Value: "valueOverride1",
			},
			{
				Name:  "name2",
				Value: "value2",
			},
			{
				Name:  "overrideName2",
				Value: "overrideValue2",
			},
		}

		res := bao.ApplyBuildArgOverrides(args, overrides...)
		Expect(res).To(Equal(expected))
	})
})
