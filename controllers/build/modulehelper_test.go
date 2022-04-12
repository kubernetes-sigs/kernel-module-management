package build_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/controllers/build"
)

var _ = Describe("ModuleHelper", func() {
	Describe("ApplyBuildArgOverrides", func() {
		It("should do nothing if there is nothing to override", func() {
			args := make([]ootov1alpha1.BuildArg, 2)

			Expect(
				build.NewModuleHelper().ApplyBuildArgOverrides(args),
			).To(
				Equal(args),
			)
		})

		It("should override fields that need to be overridden", func() {
			ba1 := ootov1alpha1.BuildArg{Name: "not-overridden", Value: "value1"}
			ba2 := ootov1alpha1.BuildArg{Name: "overridden", Value: "new-value"}

			args := []ootov1alpha1.BuildArg{
				ba1,
				{Name: "overridden", Value: "old-value"},
			}

			Expect(
				build.NewModuleHelper().ApplyBuildArgOverrides(args, ba2),
			).To(
				Equal([]ootov1alpha1.BuildArg{ba1, ba2}),
			)
		})

		It("should add args that are not overrides at the end", func() {
			ba1 := ootov1alpha1.BuildArg{Name: "name1", Value: "value1"}
			ba2 := ootov1alpha1.BuildArg{Name: "name2", Value: "value2"}
			ba3 := ootov1alpha1.BuildArg{Name: "name3", Value: "value3"}

			Expect(
				build.NewModuleHelper().ApplyBuildArgOverrides([]ootov1alpha1.BuildArg{ba1, ba2}, ba3),
			).To(
				Equal([]ootov1alpha1.BuildArg{ba1, ba2, ba3}),
			)
		})
	})
})
