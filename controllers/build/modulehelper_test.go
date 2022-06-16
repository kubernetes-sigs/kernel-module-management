package build_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/controllers/build"
	v1 "k8s.io/api/core/v1"
)

var _ = Describe("ApplyBuildArgOverrides", func() {
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

var _ = Describe("GetReleventBuild", func() {
	specBuild := ootov1alpha1.Build{
		BuildArgs: []ootov1alpha1.BuildArg{
			ootov1alpha1.BuildArg{Name: "specBuildArg1", Value: "specBuildArg1Value"},
			ootov1alpha1.BuildArg{Name: "specBuildArg2", Value: "specBuildArg2Value"},
		},
		Dockerfile: "specBuildDockerfile",
		Pull:       ootov1alpha1.PullOptions{Insecure: false},
		Push:       ootov1alpha1.PushOptions{Insecure: true},
		Secrets: []v1.LocalObjectReference{
			v1.LocalObjectReference{Name: "specBuildSecret1"},
			v1.LocalObjectReference{Name: "specBuildSecret2"},
		},
	}

	mappingBuild := ootov1alpha1.Build{
		BuildArgs: []ootov1alpha1.BuildArg{
			ootov1alpha1.BuildArg{Name: "mappingBuildArg1", Value: "mappingBuildArg1Value"},
			ootov1alpha1.BuildArg{Name: "mappingBuildArg2", Value: "mappingBuildArg2Value"},
		},
		Dockerfile: "mappingBuildDockerfile",
		Pull:       ootov1alpha1.PullOptions{Insecure: true},
		Push:       ootov1alpha1.PushOptions{Insecure: false},
		Secrets: []v1.LocalObjectReference{
			v1.LocalObjectReference{Name: "mappingBuildSecret1"},
			v1.LocalObjectReference{Name: "mappingBuildSecret2"},
		},
	}

	It("only spec build is present", func() {
		mod := ootov1alpha1.Module{Spec: ootov1alpha1.ModuleSpec{Build: &specBuild}}
		km := ootov1alpha1.KernelMapping{Build: nil}
		res := build.NewModuleHelper().GetReleventBuild(mod, km)
		Expect(res).To(Equal(&specBuild))
	})

	It("only mapping build is present", func() {
		mod := ootov1alpha1.Module{Spec: ootov1alpha1.ModuleSpec{Build: nil}}
		km := ootov1alpha1.KernelMapping{Build: &mappingBuild}
		res := build.NewModuleHelper().GetReleventBuild(mod, km)
		Expect(res).To(Equal(&mappingBuild))
	})

	It("mapping and spec build present, override and merge", func() {
		expectedBuild := ootov1alpha1.Build{
			BuildArgs: []ootov1alpha1.BuildArg{
				ootov1alpha1.BuildArg{Name: "specBuildArg1", Value: "specBuildArg1Value"},
				ootov1alpha1.BuildArg{Name: "specBuildArg2", Value: "specBuildArg2Value"},
				ootov1alpha1.BuildArg{Name: "mappingBuildArg1", Value: "mappingBuildArg1Value"},
				ootov1alpha1.BuildArg{Name: "mappingBuildArg2", Value: "mappingBuildArg2Value"},
			},
			Dockerfile: "mappingBuildDockerfile",
			Pull:       ootov1alpha1.PullOptions{Insecure: false},
			Push:       ootov1alpha1.PushOptions{Insecure: true},
			Secrets: []v1.LocalObjectReference{
				v1.LocalObjectReference{Name: "specBuildSecret1"},
				v1.LocalObjectReference{Name: "specBuildSecret2"},
				v1.LocalObjectReference{Name: "mappingBuildSecret1"},
				v1.LocalObjectReference{Name: "mappingBuildSecret2"},
			},
		}
		mod := ootov1alpha1.Module{Spec: ootov1alpha1.ModuleSpec{Build: &specBuild}}
		km := ootov1alpha1.KernelMapping{Build: &mappingBuild}
		res := build.NewModuleHelper().GetReleventBuild(mod, km)
		Expect(res).To(Equal(&expectedBuild))
	})
})
