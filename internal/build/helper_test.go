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
		mappingBuild := &kmmv1beta1.Build{
			DockerfileConfigMap: &v1.LocalObjectReference{Name: "some kernel mapping build name"},
		}

		res := nh.GetRelevantBuild(nil, mappingBuild)
		Expect(res).To(Equal(mappingBuild))
	})

	It("kernel mapping build absent, module loader build present", func() {
		moduleBuild := &kmmv1beta1.Build{
			DockerfileConfigMap: &v1.LocalObjectReference{Name: "some load module build name"},
		}

		res := nh.GetRelevantBuild(moduleBuild, nil)

		Expect(res).To(Equal(moduleBuild))
	})

	It("kernel mapping and module loader builds are present, overrides", func() {
		moduleBuild := &kmmv1beta1.Build{
			DockerfileConfigMap: &v1.LocalObjectReference{Name: "some load module build name"},
			BaseImageRegistryTLS: kmmv1beta1.TLSOptions{
				Insecure:              true,
				InsecureSkipTLSVerify: true,
			},
		}
		mappingBuild := &kmmv1beta1.Build{
			DockerfileConfigMap: &v1.LocalObjectReference{Name: "some kernel mapping build name"},
		}

		res := nh.GetRelevantBuild(moduleBuild, mappingBuild)
		Expect(res.DockerfileConfigMap).To(Equal(mappingBuild.DockerfileConfigMap))
		Expect(res.BaseImageRegistryTLS).To(Equal(moduleBuild.BaseImageRegistryTLS))
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
