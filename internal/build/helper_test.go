package build

import (
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
)

var _ = Describe("GetRelevantBuild", func() {

	var nh Helper

	BeforeEach(func() {
		nh = NewHelper()
	})

	It("kernel mapping build present, module loader build absent", func() {
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						Build: nil,
					},
				},
			},
		}
		km := kmmv1beta1.KernelMapping{
			Build: &kmmv1beta1.Build{
				DockerfileConfigMap: &v1.LocalObjectReference{Name: "some kernel mapping build name"},
			},
		}

		res := nh.GetRelevantBuild(mod, km)
		Expect(res).To(Equal(km.Build))
	})

	It("kernel mapping build absent, module loader build present", func() {
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						Build: &kmmv1beta1.Build{
							DockerfileConfigMap: &v1.LocalObjectReference{Name: "some load module build name"},
						},
					},
				},
			},
		}
		km := kmmv1beta1.KernelMapping{
			Build: nil,
		}

		res := nh.GetRelevantBuild(mod, km)
		Expect(res).To(Equal(mod.Spec.ModuleLoader.Container.Build))
	})

	It("kernel mapping and module loader builds are present, overrides", func() {
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						Build: &kmmv1beta1.Build{
							DockerfileConfigMap: &v1.LocalObjectReference{Name: "some load module build name"},
							BaseImageRegistryTLS: kmmv1beta1.TLSOptions{
								Insecure:              true,
								InsecureSkipTLSVerify: true,
							},
						},
					},
				},
			},
		}
		km := kmmv1beta1.KernelMapping{
			Build: &kmmv1beta1.Build{
				DockerfileConfigMap: &v1.LocalObjectReference{Name: "some kernel mapping build name"},
			},
		}

		res := nh.GetRelevantBuild(mod, km)
		Expect(res.DockerfileConfigMap).To(Equal(km.Build.DockerfileConfigMap))
		Expect(res.BaseImageRegistryTLS).To(Equal(mod.Spec.ModuleLoader.Container.Build.BaseImageRegistryTLS))
	})
})

var _ = Describe("ApplyBuildArgOverrides", func() {

	var nh Helper

	BeforeEach(func() {
		nh = NewHelper()
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

		res := nh.ApplyBuildArgOverrides(args, overrides...)
		Expect(res).To(Equal(expected))
	})
})
