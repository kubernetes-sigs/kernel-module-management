package mbsc

import (
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("SetModuleImageSpec", func() {
	var (
		mbsc MBSC
	)

	BeforeEach(func() {
		mbsc = NewMBSC()
	})

	It("MBSC does not have any images in spec", func() {
		mbscObj := kmmv1beta1.ModuleBuildSignConfig{
			Spec: kmmv1beta1.ModuleImagesConfigSpec{},
		}
		imageSpec := kmmv1beta1.ModuleImageSpec{Image: "some image"}

		mbsc.SetModuleImageSpec(&mbscObj, &imageSpec)

		Expect(len(mbscObj.Spec.Images)).To(Equal(1))
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

		mbsc.SetModuleImageSpec(&mbscObj, &imageSpec)

		Expect(len(mbscObj.Spec.Images)).To(Equal(3))
	})

	It("MBSC has the image already in spec", func() {
		mbscObj := kmmv1beta1.ModuleBuildSignConfig{
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				Images: []kmmv1beta1.ModuleImageSpec{
					kmmv1beta1.ModuleImageSpec{Image: "image 1"},
					kmmv1beta1.ModuleImageSpec{Image: "image 2"},
				},
			},
		}
		imageSpec := kmmv1beta1.ModuleImageSpec{Image: "image 2"}

		mbsc.SetModuleImageSpec(&mbscObj, &imageSpec)

		Expect(len(mbscObj.Spec.Images)).To(Equal(2))
	})
})
