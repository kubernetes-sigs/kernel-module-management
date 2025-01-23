package mbsc

import (
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
)

var _ = Describe("SetModuleImageSpec", func() {
	var (
		mbscHelper Helper
	)

	BeforeEach(func() {
		mbscHelper = NewHelper()
	})

	It("MBSC does not have any images in spec", func() {
		mbscObj := kmmv1beta1.ModuleBuildSignConfig{
			Spec: kmmv1beta1.ModuleImagesConfigSpec{},
		}
		imageSpec := kmmv1beta1.ModuleImageSpec{Image: "some image"}

		mbscHelper.SetModuleImageSpec(&mbscObj, &imageSpec, &v1.LocalObjectReference{})

		Expect(len(mbscObj.Spec.Images)).To(Equal(1))
		Expect(mbscObj.Spec.ImageRepoSecret).ToNot(BeNil())
	})

	It("MBSC has different images in spec", func() {
		mbscObj := kmmv1beta1.ModuleBuildSignConfig{
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				Images: []kmmv1beta1.ModuleImageSpec{
					kmmv1beta1.ModuleImageSpec{Image: "image 1"},
					kmmv1beta1.ModuleImageSpec{Image: "image 2"},
				},
			},
		}
		imageSpec := kmmv1beta1.ModuleImageSpec{Image: "some image"}

		mbscHelper.SetModuleImageSpec(&mbscObj, &imageSpec, &v1.LocalObjectReference{})

		Expect(len(mbscObj.Spec.Images)).To(Equal(3))
		Expect(mbscObj.Spec.ImageRepoSecret).ToNot(BeNil())
	})

	It("MBSC has the image already in spec", func() {
		mbscObj := kmmv1beta1.ModuleBuildSignConfig{
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				Images: []kmmv1beta1.ModuleImageSpec{
					kmmv1beta1.ModuleImageSpec{Image: "image 1"},
					kmmv1beta1.ModuleImageSpec{Image: "image 2", Generation: "generation 2"},
				},
			},
		}
		imageSpec := kmmv1beta1.ModuleImageSpec{Image: "image 2", Generation: "some generation"}

		mbscHelper.SetModuleImageSpec(&mbscObj, &imageSpec, &v1.LocalObjectReference{})

		Expect(len(mbscObj.Spec.Images)).To(Equal(2))
		Expect(mbscObj.Spec.Images[1].Generation).To(Equal("some generation"))
		Expect(mbscObj.Spec.ImageRepoSecret).ToNot(BeNil())
	})
})
