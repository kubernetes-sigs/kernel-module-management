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

package v1beta1

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestV1beta1(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "V1beta1 Suite")
}

var _ = Describe("validateKernelMapping", func() {
	It("should pass when there are not kernel mappings", func() {
		e := (&Module{}).validateKernelMapping()
		Expect(e).ToNot(HaveOccurred())
	})

	It("should pass when regexp,literal and containerImage are valid", func() {
		mod1 := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						KernelMappings: []KernelMapping{
							{Regexp: "valid-regexp", ContainerImage: "image-url"},
							{Regexp: "^.*$", ContainerImage: "image-url"},
						},
					},
				},
			},
		}
		Expect(mod1.validateKernelMapping()).ToNot(HaveOccurred())

		mod2 := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						ContainerImage: "image-url",
						KernelMappings: []KernelMapping{
							{Regexp: "valid-regexp"},
						},
					},
				},
			},
		}
		Expect(mod2.validateKernelMapping()).ToNot(HaveOccurred())

		mod3 := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						ContainerImage: "image-url",
						KernelMappings: []KernelMapping{
							{Literal: "any-value"},
						},
					},
				},
			},
		}
		Expect(mod3.validateKernelMapping()).ToNot(HaveOccurred())
	})

	It("should fail when an invalid regex is found", func() {
		mod := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						KernelMappings: []KernelMapping{
							{Regexp: "*-invalid-regexp"},
						},
					},
				},
			},
		}

		e := mod.validateKernelMapping()
		Expect(e).To(HaveOccurred())
		Expect(e.Error()).To(ContainSubstring("invalid regexp"))
	})

	It("should fail when literal and regex are set", func() {
		mod := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						KernelMappings: []KernelMapping{
							{Regexp: "^valid-regexp$", Literal: "any-value"},
						},
					},
				},
			},
		}

		e := mod.validateKernelMapping()
		Expect(e).To(HaveOccurred())
		Expect(e.Error()).To(ContainSubstring("regexp and literal are mutually exclusive properties at kernelMappings"))
	})

	It("should fail when neither literal and regex are set", func() {
		mod := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						KernelMappings: []KernelMapping{
							{ContainerImage: "image-url"},
						},
					},
				},
			},
		}

		e := mod.validateKernelMapping()
		Expect(e).To(HaveOccurred())
		Expect(e.Error()).To(ContainSubstring("regexp or literal must be set at kernelMappings"))
	})

	It("should fail when a kernel-mapping has invalid containerName", func() {
		mod := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						KernelMappings: []KernelMapping{
							{Regexp: "valid-regexp"},
						},
					},
				},
			},
		}

		e := mod.validateKernelMapping()
		Expect(e).To(HaveOccurred())
		Expect(e.Error()).To(
			ContainSubstring(
				"missing spec.moduleLoader.container.kernelMappings",
			),
		)
	})
})

var _ = Describe("validateModprobe", func() {
	It("should fail when moduleName and rawArgs are missing", func() {
		mod := &Module{}

		e := mod.validateModprobe()
		Expect(e).To(HaveOccurred())
		Expect(e.Error()).To(ContainSubstring("load and unload rawArgs must be set when moduleName is unset"))
	})

	It("should fail when rawArgs.load is empty and moduleName is not set", func() {
		mod := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						Modprobe: ModprobeSpec{
							RawArgs: &ModprobeArgs{
								Load:   []string{},
								Unload: []string{"arg"},
							},
						},
					},
				},
			},
		}

		e := mod.validateModprobe()
		Expect(e).To(HaveOccurred())
		Expect(e.Error()).To(
			ContainSubstring("load and unload rawArgs must be set when moduleName is unset"),
		)
	})

	It("should fail when rawArgs and moduleName are set", func() {
		mod := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						Modprobe: ModprobeSpec{
							ModuleName: "mod-name",
							RawArgs: &ModprobeArgs{
								Load:   []string{"arg"},
								Unload: []string{"arg"},
							},
						},
					},
				},
			},
		}

		e := mod.validateModprobe()
		Expect(e).To(HaveOccurred())
		Expect(e.Error()).To(
			ContainSubstring("rawArgs cannot be set when moduleName is set"),
		)
	})

	It("should fail when rawArgs.unload is empty and moduleName is not set", func() {
		mod := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						Modprobe: ModprobeSpec{
							RawArgs: &ModprobeArgs{
								Load:   []string{"arg"},
								Unload: []string{},
							},
						},
					},
				},
			},
		}

		e := mod.validateModprobe()
		Expect(e).To(HaveOccurred())
		Expect(e.Error()).To(
			ContainSubstring(
				"load and unload rawArgs must be set when moduleName is unset",
			),
		)
	})

	It("should pass when rawArgs has load and unload values and moduleName is not set", func() {
		mod := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						Modprobe: ModprobeSpec{
							RawArgs: &ModprobeArgs{
								Load:   []string{"arg"},
								Unload: []string{"arg"},
							},
						},
					},
				},
			},
		}

		e := mod.validateModprobe()
		Expect(e).ToNot(HaveOccurred())
	})

	It("should pass when modprobe has moduleName and rawArgs are not set", func() {
		mod := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						Modprobe: ModprobeSpec{
							ModuleName: "module-name",
						},
					},
				},
			},
		}

		e := mod.validateModprobe()
		Expect(e).ToNot(HaveOccurred())
	})

	It("should fail when ModulesLoadingOrder is defined but is length is < 2", func() {
		mod := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						Modprobe: ModprobeSpec{
							ModuleName:          "module-name",
							ModulesLoadingOrder: []string{"test"},
						},
					},
				},
			},
		}

		Expect(
			mod.validateModprobe(),
		).To(
			HaveOccurred(),
		)
	})

	It("should fail when ModulesLoadingOrder is defined but moduleName is empty", func() {
		mod := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						Modprobe: ModprobeSpec{ModulesLoadingOrder: make([]string, 0)},
					},
				},
			},
		}

		Expect(
			mod.validateModprobe(),
		).To(
			HaveOccurred(),
		)
	})

	It("should fail when ModulesLoadingOrder is defined but its first element is not moduleName", func() {
		mod := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						Modprobe: ModprobeSpec{
							ModulesLoadingOrder: []string{"test1", "test2"},
						},
					},
				},
			},
		}

		Expect(
			mod.validateModprobe(),
		).To(
			HaveOccurred(),
		)
	})

	It("should fail when ModulesLoadingOrder contains duplicates", func() {
		const moduleName = "module-name"

		mod := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						Modprobe: ModprobeSpec{
							ModuleName:          moduleName,
							ModulesLoadingOrder: []string{moduleName, "test", "test"},
						},
					},
				},
			},
		}

		Expect(
			mod.validateModprobe(),
		).To(
			HaveOccurred(),
		)
	})
})

var _ = Describe("ValidateCreate", func() {
	It("should pass when all conditions are met", func() {
		mod := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						Modprobe: ModprobeSpec{
							ModuleName: "mod-name",
						},
						KernelMappings: []KernelMapping{
							{Regexp: "valid-regexp", ContainerImage: "image-url"},
						},
					},
				},
			},
		}

		e := mod.ValidateCreate()
		Expect(e).ToNot(HaveOccurred())
	})

	It("should fail when validating kernel mappings regexps", func() {
		mod := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						KernelMappings: []KernelMapping{
							{Regexp: "*-invalid-regexp"},
						},
					},
				},
			},
		}

		e := mod.ValidateCreate()
		Expect(e).To(HaveOccurred())
		Expect(e.Error()).To(ContainSubstring("failed to validate kernel mappings"))
	})
})

var _ = Describe("ValidateUpdate", func() {
	It("should pass when all conditions are met", func() {
		mod1 := &Module{

			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						Modprobe: ModprobeSpec{
							ModuleName: "module-name",
						},
						ContainerImage: "image-url",
						KernelMappings: []KernelMapping{
							{Regexp: "valid-regexp"},
						},
					},
				},
			},
		}

		modprobeRawArgs := ModprobeArgs{
			Load: []string{"arg-1"}, Unload: []string{"arg-1"},
		}
		mod2 := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						Modprobe: ModprobeSpec{
							RawArgs: &modprobeRawArgs,
						},
						ContainerImage: "image-url",
						KernelMappings: []KernelMapping{
							{Regexp: "valid-regexp"},
						},
					},
				},
			},
		}

		Expect(mod1.ValidateUpdate(nil)).ToNot(HaveOccurred())
		Expect(mod2.ValidateUpdate(nil)).ToNot(HaveOccurred())
	})

	It("should fail when validating kernel mappings regexps", func() {
		mod := &Module{
			Spec: ModuleSpec{
				ModuleLoader: ModuleLoaderSpec{
					Container: ModuleLoaderContainerSpec{
						KernelMappings: []KernelMapping{
							{Regexp: "*-invalid-regexp"},
						},
					},
				},
			},
		}

		e := mod.ValidateUpdate(nil)
		Expect(e).To(HaveOccurred())
		Expect(e.Error()).To(ContainSubstring("failed to validate kernel mappings"))
	})
})

var _ = Describe("ValidateDelete", func() {
	It("should do nothing and return always nil", func() {
		module := &Module{}
		Expect(module.ValidateDelete()).ToNot(HaveOccurred())
	})
})
