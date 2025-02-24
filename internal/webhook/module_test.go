/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package webhook

import (
	"context"
	v1 "k8s.io/api/core/v1"
	"strings"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func getLengthAfterSlash(s string) int {
	before, after, found := strings.Cut(s, "/")

	if found {
		return len(after)
	}

	return len(before)
}

var (
	validModule = kmmv1beta1.Module{
		Spec: kmmv1beta1.ModuleSpec{
			ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
				Container: kmmv1beta1.ModuleLoaderContainerSpec{
					Modprobe: kmmv1beta1.ModprobeSpec{
						ModuleName: "mod-name",
					},
					KernelMappings: []kmmv1beta1.KernelMapping{
						{Regexp: "valid-regexp", ContainerImage: "image-url:mytag"},
					},
				},
			},
		},
	}

	moduleWebhook = NewModuleValidator(GinkgoLogr)
)

var _ = Describe("maxCombinedLength", func() {
	It("should be the accurate maximum length", func() {
		const maxLabelLength = 63

		baseLength := getLengthAfterSlash(
			utils.GetModuleVersionLabelName("", ""),
		)

		if l := getLengthAfterSlash(utils.GetWorkerPodVersionLabelName("", "")); l > baseLength {
			baseLength = l
		}

		if l := getLengthAfterSlash(utils.GetDevicePluginVersionLabelName("", "")); l > baseLength {
			baseLength = l
		}

		Expect(maxCombinedLength).To(Equal(maxLabelLength - baseLength))
	})
})

var _ = Describe("validateImageFormat", func() {

	It("should fail if no tag or digest were specified", func() {

		Expect(
			validateImageFormat("image-ur"),
		).To(HaveOccurred())
	})

	It("should work if a tag or digest were specified", func() {

		Expect(
			validateImageFormat("image-url:tag"),
		).NotTo(HaveOccurred())

		Expect(
			validateImageFormat("image-url@sha256:xxxxxxxxxxxx"),
		).NotTo(HaveOccurred())
	})
})

var _ = Describe("validateModuleLoaderContainerSpec", func() {
	It("should pass when there are not kernel mappings", func() {
		Expect(
			validateModuleLoaderContainerSpec(kmmv1beta1.ModuleLoaderContainerSpec{}),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should fail if the containerImage doesn't contain a sha or tag", func() {

		containerSpec := kmmv1beta1.ModuleLoaderContainerSpec{
			ContainerImage: "image-url",
			KernelMappings: []kmmv1beta1.KernelMapping{
				{Regexp: "valid-regexp"},
			},
		}

		Expect(
			validateModuleLoaderContainerSpec(containerSpec),
		).To(
			MatchError(
				ContainSubstring("failed to validate image format"),
			),
		)
	})

	It("should pass when regexp,literal and containerImage are valid", func() {
		containerSpec1 := kmmv1beta1.ModuleLoaderContainerSpec{
			KernelMappings: []kmmv1beta1.KernelMapping{
				{Regexp: "valid-regexp", ContainerImage: "image-url:mytag"},
				{Regexp: "^.*$", ContainerImage: "image-url:mytag"},
			},
		}

		Expect(validateModuleLoaderContainerSpec(containerSpec1)).ToNot(HaveOccurred())

		containerSpec2 := kmmv1beta1.ModuleLoaderContainerSpec{
			ContainerImage: "image-url:mytag",
			KernelMappings: []kmmv1beta1.KernelMapping{
				{Regexp: "valid-regexp"},
			},
		}

		Expect(validateModuleLoaderContainerSpec(containerSpec2)).ToNot(HaveOccurred())

		containerSpec3 := kmmv1beta1.ModuleLoaderContainerSpec{
			ContainerImage: "image-url:mytag",
			KernelMappings: []kmmv1beta1.KernelMapping{
				{Literal: "any-value"},
			},
		}
		Expect(validateModuleLoaderContainerSpec(containerSpec3)).ToNot(HaveOccurred())
	})

	It("should fail when an invalid regex is found", func() {
		containerSpec := kmmv1beta1.ModuleLoaderContainerSpec{
			KernelMappings: []kmmv1beta1.KernelMapping{
				{Regexp: "*-invalid-regexp"},
			},
		}

		Expect(
			validateModuleLoaderContainerSpec(containerSpec),
		).To(
			MatchError(
				ContainSubstring("invalid regexp"),
			),
		)
	})

	It("should fail when literal and regex are set", func() {
		containerSpec := kmmv1beta1.ModuleLoaderContainerSpec{
			KernelMappings: []kmmv1beta1.KernelMapping{
				{Regexp: "^valid-regexp$", Literal: "any-value"},
			},
		}

		Expect(
			validateModuleLoaderContainerSpec(containerSpec),
		).To(
			MatchError(
				ContainSubstring("regexp and literal are mutually exclusive properties at kernelMappings"),
			),
		)
	})

	It("should fail when neither literal and regex are set", func() {
		containerSpec := kmmv1beta1.ModuleLoaderContainerSpec{
			KernelMappings: []kmmv1beta1.KernelMapping{
				{ContainerImage: "image-url"},
			},
		}

		Expect(
			validateModuleLoaderContainerSpec(containerSpec),
		).To(
			MatchError(
				ContainSubstring("regexp or literal must be set at kernelMappings"),
			),
		)
	})

	It("should fail when a kernel-mapping has invalid containerName", func() {
		containerSpec := kmmv1beta1.ModuleLoaderContainerSpec{
			KernelMappings: []kmmv1beta1.KernelMapping{
				{Regexp: "valid-regexp"},
			},
		}

		Expect(
			validateModuleLoaderContainerSpec(containerSpec),
		).To(
			MatchError(
				ContainSubstring("missing spec.moduleLoader.container.kernelMappings"),
			),
		)
	})

	It("should fail when a kernel-mapping has invalid containerName", func() {
		containerSpec := kmmv1beta1.ModuleLoaderContainerSpec{
			KernelMappings: []kmmv1beta1.KernelMapping{
				{Regexp: "valid-regexp", ContainerImage: "image-url"},
			},
		}

		Expect(
			validateModuleLoaderContainerSpec(containerSpec),
		).To(
			MatchError(
				ContainSubstring("failed to validate image format"),
			),
		)
	})

	DescribeTable("should fail when InTreeModulesToRemove and InTreeModuleToRemove both defined",
		func(inTreeModulesInContainer, inTreeModuleInContainer, inTreeModulesInKM, inTreeModuleInKM bool) {
			containerSpec := kmmv1beta1.ModuleLoaderContainerSpec{}
			km := kmmv1beta1.KernelMapping{ContainerImage: "image-url"}

			if inTreeModulesInContainer {
				containerSpec.InTreeModulesToRemove = []string{"some value"}
			}
			if inTreeModuleInContainer {
				containerSpec.InTreeModuleToRemove = "some value" //nolint:staticcheck
			}
			if inTreeModulesInKM {
				km.InTreeModulesToRemove = []string{"some value"}
			}
			if inTreeModuleInKM {
				km.InTreeModuleToRemove = "some value" //nolint:staticcheck
			}
			containerSpec.KernelMappings = []kmmv1beta1.KernelMapping{km}

			err := validateModuleLoaderContainerSpec(containerSpec)
			Expect(err).ToNot(BeNil())
		},
		Entry("InTreeModulesToRemove and InTreeModuleToRemove set in containerspec", true, true, false, false),
		Entry("InTreeModulesToRemove and InTreeModuleToRemove set in kernel mapping", false, false, true, true),
		Entry("InTreeModulesToRemove set in container spec, InTreeModuleToRemove set in kernel mapping", true, false, false, true),
		Entry("InTreeModuleToRemove set in container spec, InTreeModulesToRemove set in kernel mapping", true, false, true, false),
	)

})

var _ = Describe("validateModprobe", func() {
	It("should fail when moduleName and rawArgs are missing", func() {
		Expect(
			validateModprobe(kmmv1beta1.ModprobeSpec{}),
		).To(
			MatchError(
				ContainSubstring("load and unload rawArgs must be set when moduleName is unset"),
			),
		)
	})

	It("should fail when rawArgs.load is empty and moduleName is not set", func() {
		modprobe := kmmv1beta1.ModprobeSpec{
			RawArgs: &kmmv1beta1.ModprobeArgs{
				Load:   []string{},
				Unload: []string{"arg"},
			},
		}

		Expect(
			validateModprobe(modprobe),
		).To(
			MatchError(
				ContainSubstring("load and unload rawArgs must be set when moduleName is unset"),
			),
		)
	})

	It("should fail when rawArgs and moduleName are set", func() {
		modprobe := kmmv1beta1.ModprobeSpec{
			ModuleName: "mod-name",
			RawArgs: &kmmv1beta1.ModprobeArgs{
				Load:   []string{"arg"},
				Unload: []string{"arg"},
			},
		}

		Expect(
			validateModprobe(modprobe),
		).To(
			MatchError(
				ContainSubstring("rawArgs cannot be set when moduleName is set"),
			),
		)
	})

	It("should fail when rawArgs.unload is empty and moduleName is not set", func() {
		modprobe := kmmv1beta1.ModprobeSpec{
			RawArgs: &kmmv1beta1.ModprobeArgs{
				Load:   []string{"arg"},
				Unload: []string{},
			},
		}

		Expect(
			validateModprobe(modprobe),
		).To(
			MatchError(
				ContainSubstring("load and unload rawArgs must be set when moduleName is unset"),
			),
		)
	})

	It("should pass when rawArgs has load and unload values and moduleName is not set", func() {
		modprobe := kmmv1beta1.ModprobeSpec{
			RawArgs: &kmmv1beta1.ModprobeArgs{
				Load:   []string{"arg"},
				Unload: []string{"arg"},
			},
		}

		Expect(
			validateModprobe(modprobe),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should pass when modprobe has moduleName and rawArgs are not set", func() {
		modprobe := kmmv1beta1.ModprobeSpec{ModuleName: "module-name"}

		Expect(
			validateModprobe(modprobe),
		).NotTo(
			HaveOccurred(),
		)
	})

	It("should fail when ModulesLoadingOrder is defined but is length is < 2", func() {
		modprobe := kmmv1beta1.ModprobeSpec{
			ModuleName:          "module-name",
			ModulesLoadingOrder: []string{"test"},
		}

		Expect(
			validateModprobe(modprobe),
		).To(
			HaveOccurred(),
		)
	})

	It("should fail when ModulesLoadingOrder is defined but moduleName is empty", func() {
		modprobe := kmmv1beta1.ModprobeSpec{ModulesLoadingOrder: make([]string, 0)}

		Expect(
			validateModprobe(modprobe),
		).To(
			HaveOccurred(),
		)
	})

	It("should fail when ModulesLoadingOrder is defined but its first element is not moduleName", func() {
		modprobe := kmmv1beta1.ModprobeSpec{
			ModulesLoadingOrder: []string{"test1", "test2"},
		}

		Expect(
			validateModprobe(modprobe),
		).To(
			HaveOccurred(),
		)
	})

	It("should fail when ModulesLoadingOrder contains duplicates", func() {
		const moduleName = "module-name"

		modprobe := kmmv1beta1.ModprobeSpec{
			ModuleName:          moduleName,
			ModulesLoadingOrder: []string{moduleName, "test", "test"},
		}

		Expect(
			validateModprobe(modprobe),
		).To(
			HaveOccurred(),
		)
	})
})

var _ = Describe("validateModule", func() {
	chars21 := strings.Repeat("a", 21)

	DescribeTable(
		"should work as expected",
		func(name, ns string, errExpected bool) {
			mod := validModule
			mod.Name = name
			mod.Namespace = ns

			_, err := validateModule(&mod)
			exp := Expect(err)

			if errExpected {
				exp.To(HaveOccurred())
			} else {
				exp.NotTo(HaveOccurred())
			}
		},
		Entry("not too long", "name", "ns", false),
		Entry("too long", chars21, chars21, true),
	)
})

var _ = Describe("ValidateCreate", func() {
	ctx := context.TODO()

	It("should pass when all conditions are met", func() {
		mod := validModule

		_, err := moduleWebhook.ValidateCreate(ctx, &mod)
		Expect(err).ToNot(HaveOccurred())
	})

	It("should fail when validating kernel mappings regexps", func() {
		mod := &kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						KernelMappings: []kmmv1beta1.KernelMapping{
							{Regexp: "*-invalid-regexp"},
						},
					},
				},
			},
		}

		_, err := moduleWebhook.ValidateCreate(ctx, mod)
		Expect(err).To(
			MatchError(
				ContainSubstring("failed to validate kernel mappings"),
			),
		)
	})
})

var _ = Describe("ValidateUpdate", func() {
	ctx := context.TODO()

	It("should pass when all conditions are met", func() {
		mod1 := &kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						Modprobe: kmmv1beta1.ModprobeSpec{
							ModuleName: "module-name",
						},
						ContainerImage: "image-url:mytag",
						KernelMappings: []kmmv1beta1.KernelMapping{
							{Regexp: "valid-regexp"},
						},
					},
				},
			},
		}

		mod2 := &kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						Modprobe: kmmv1beta1.ModprobeSpec{
							RawArgs: &kmmv1beta1.ModprobeArgs{
								Load: []string{"arg-1"}, Unload: []string{"arg-1"},
							},
						},
						ContainerImage: "image-url:mytag",
						KernelMappings: []kmmv1beta1.KernelMapping{
							{Regexp: "valid-regexp"},
						},
					},
				},
			},
		}

		_, err1 := moduleWebhook.ValidateUpdate(ctx, mod1, mod2)
		Expect(err1).ToNot(HaveOccurred())
	})

	It("should fail when validating kernel mappings regexps", func() {
		mod := &kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						KernelMappings: []kmmv1beta1.KernelMapping{
							{Regexp: "*-invalid-regexp"},
						},
					},
				},
			},
		}

		_, err := moduleWebhook.ValidateUpdate(ctx, &kmmv1beta1.Module{}, mod)
		Expect(err).
			To(
				MatchError(
					ContainSubstring("failed to validate kernel mappings"),
				),
			)
	})

	DescribeTable(
		"version updates",
		func(oldVersion, newVersion string, errorExpected bool) {
			old := validModule
			old.Spec.ModuleLoader.Container.Version = oldVersion

			new := validModule
			new.Spec.ModuleLoader.Container.Version = newVersion

			_, err := moduleWebhook.ValidateUpdate(ctx, &old, &new)
			exp := Expect(err)

			if errorExpected {
				exp.To(HaveOccurred())
			} else {
				exp.NotTo(HaveOccurred())
			}
		},
		Entry(nil, "v1", "", true),
		Entry(nil, "", "v2", true),
		Entry(nil, "", "", false),
		Entry(nil, "v1", "v2", false),
	)
})

var _ = Describe("ValidateDelete", func() {
	It("should do nothing and return always nil", func() {
		_, err := moduleWebhook.ValidateDelete(context.TODO(), &kmmv1beta1.Module{})
		Expect(err).To(MatchError(NotImplemented))
	})
})

var _ = Describe("validateTolerations", func() {
	It("should fail when Module has an invalid toleration effect", func() {
		tolerations := []v1.Toleration{
			{
				Key: "Test-Key1", Operator: v1.TolerationOpEqual, Value: "Test-Value1", Effect: v1.TaintEffectPreferNoSchedule,
			},
			{
				Key: "Test-Key2", Operator: v1.TolerationOpExists, Effect: "Invalid-Effect",
			},
		}

		err := validateTolerations(tolerations)
		Expect(err).To(HaveOccurred())
	})

	It("should fail when Module has an invalid toleration operator", func() {
		tolerations := []v1.Toleration{
			{
				Key: "Test-Key1", Operator: v1.TolerationOpEqual, Value: "Test-Value1", Effect: v1.TaintEffectPreferNoSchedule,
			},
			{
				Key: "Test-Key2", Operator: "Invalid-operator-value", Value: "Test-Value2", Effect: v1.TaintEffectNoExecute,
			},
		}

		err := validateTolerations(tolerations)
		Expect(err).To(HaveOccurred())
	})

	It("should fail when toleration has non empty value and Exists operator", func() {
		tolerations := []v1.Toleration{
			{
				Key: "Test-Key1", Operator: v1.TolerationOpEqual, Value: "Test-Value1", Effect: v1.TaintEffectPreferNoSchedule,
			},
			{
				Key: "Test-Key2", Operator: v1.TolerationOpExists, Value: "Test-Value2", Effect: v1.TaintEffectNoExecute,
			},
		}

		err := validateTolerations(tolerations)
		Expect(err).To(HaveOccurred())
	})

	It("should work when all tolerations have valid effects", func() {
		tolerations := []v1.Toleration{
			{
				Key: "Test-Key1", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectPreferNoSchedule,
			},
			{
				Key: "Test-Key2", Operator: v1.TolerationOpEqual, Value: "Test-Value", Effect: v1.TaintEffectNoSchedule,
			},
			{
				Key: "Test-Key3", Operator: v1.TolerationOpEqual, Value: "Test-Value", Effect: v1.TaintEffectNoExecute,
			},
		}

		err := validateTolerations(tolerations)
		Expect(err).To(Not(HaveOccurred()))
	})

	It("should fail when toleration has an invalid key format", func() {
		tolerations := []v1.Toleration{
			{
				Key: "Invalid Key!", Operator: v1.TolerationOpEqual, Value: "Test-Value",
			},
		}

		err := validateTolerations(tolerations)
		Expect(err).To(HaveOccurred())
	})

	It("should fail when toleration has empty key with an invalid operator", func() {
		tolerations := []v1.Toleration{
			{
				Key: "", Operator: v1.TolerationOpEqual, Value: "Test-Value",
			},
		}

		err := validateTolerations(tolerations)
		Expect(err).To(HaveOccurred())
	})

	It("should fail when TolerationSeconds is set but Effect is not NoExecute", func() {
		tolSecs := int64(30)
		tolerations := []v1.Toleration{
			{
				Key: "Test-Key", Operator: v1.TolerationOpEqual, Value: "Test-Value", Effect: v1.TaintEffectNoSchedule, TolerationSeconds: &tolSecs,
			},
		}

		err := validateTolerations(tolerations)
		Expect(err).To(HaveOccurred())
	})

	It("should pass when TolerationSeconds is set with NoExecute effect", func() {
		tolSecs := int64(60)
		tolerations := []v1.Toleration{
			{
				Key: "Test-Key", Operator: v1.TolerationOpEqual, Value: "Test-Value", Effect: v1.TaintEffectNoExecute, TolerationSeconds: &tolSecs,
			},
		}

		err := validateTolerations(tolerations)
		Expect(err).To(Not(HaveOccurred()))
	})

	It("should fail when an invalid effect is provided", func() {
		tolerations := []v1.Toleration{
			{
				Key: "Test-Key", Operator: v1.TolerationOpExists, Effect: "Invalid-Effect",
			},
		}

		err := validateTolerations(tolerations)
		Expect(err).To(HaveOccurred())
	})

	It("should fail when operator is not Equal or Exists", func() {
		tolerations := []v1.Toleration{
			{
				Key: "Test-Key", Operator: "InvalidOperator", Value: "Test-Value",
			},
		}

		err := validateTolerations(tolerations)
		Expect(err).To(HaveOccurred())
	})

	It("should pass with an empty toleration list", func() {
		var tolerations []v1.Toleration

		err := validateTolerations(tolerations)
		Expect(err).To(Not(HaveOccurred()))
	})
})
