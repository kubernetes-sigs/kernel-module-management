package sign

import (
	"github.com/google/go-cmp/cmp"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	"strings"
)

var _ = Describe("GetRelevantSign", func() {

	const (
		unsignedImage = "my.registry/my/image"
		keySecret     = "securebootkey"
		certSecret    = "securebootcert"
		filesToSign   = "/modules/simple-kmod.ko:/modules/simple-procfs-kmod.ko"
	)

	var (
		h Helper
	)

	BeforeEach(func() {
		h = NewSignerHelper()
	})

	expected := &kmmv1beta1.Sign{
		UnsignedImage: unsignedImage,
		KeySecret:     &v1.LocalObjectReference{Name: keySecret},
		CertSecret:    &v1.LocalObjectReference{Name: certSecret},
		FilesToSign:   strings.Split(filesToSign, ":"),
	}

	DescribeTable("should set fields correctly", func(mod kmmv1beta1.Module, km kmmv1beta1.KernelMapping) {

		actual := h.GetRelevantSign(mod, km)
		Expect(
			cmp.Diff(expected, actual),
		).To(
			BeEmpty(),
		)
	},
		Entry(
			"no km.Sign",
			kmmv1beta1.Module{
				Spec: kmmv1beta1.ModuleSpec{
					ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
						Container: kmmv1beta1.ModuleLoaderContainerSpec{
							Sign: &kmmv1beta1.Sign{
								UnsignedImage: unsignedImage,
								KeySecret:     &v1.LocalObjectReference{Name: keySecret},
								CertSecret:    &v1.LocalObjectReference{Name: certSecret},
								FilesToSign:   strings.Split(filesToSign, ":"),
							},
						},
					},
				},
			},
			kmmv1beta1.KernelMapping{},
		),
		Entry(
			"no container.Sign",
			kmmv1beta1.Module{
				Spec: kmmv1beta1.ModuleSpec{
					ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
						Container: kmmv1beta1.ModuleLoaderContainerSpec{},
					},
				},
			},
			kmmv1beta1.KernelMapping{
				Sign: &kmmv1beta1.Sign{
					UnsignedImage: unsignedImage,
					KeySecret:     &v1.LocalObjectReference{Name: keySecret},
					CertSecret:    &v1.LocalObjectReference{Name: certSecret},
					FilesToSign:   strings.Split(filesToSign, ":"),
				},
			},
		),
		Entry(
			"default UnsignedImage",
			kmmv1beta1.Module{
				Spec: kmmv1beta1.ModuleSpec{
					ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
						Container: kmmv1beta1.ModuleLoaderContainerSpec{
							Sign: &kmmv1beta1.Sign{
								UnsignedImage: unsignedImage,
							},
						},
					},
				},
			},
			kmmv1beta1.KernelMapping{
				Sign: &kmmv1beta1.Sign{
					KeySecret:   &v1.LocalObjectReference{Name: keySecret},
					CertSecret:  &v1.LocalObjectReference{Name: certSecret},
					FilesToSign: strings.Split(filesToSign, ":"),
				},
			},
		),
		Entry(
			"default UnsignedImage and KeySecret",
			kmmv1beta1.Module{
				Spec: kmmv1beta1.ModuleSpec{
					ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
						Container: kmmv1beta1.ModuleLoaderContainerSpec{
							Sign: &kmmv1beta1.Sign{
								UnsignedImage: unsignedImage,
								KeySecret:     &v1.LocalObjectReference{Name: keySecret},
							},
						},
					},
				},
			},
			kmmv1beta1.KernelMapping{
				Sign: &kmmv1beta1.Sign{
					CertSecret:  &v1.LocalObjectReference{Name: certSecret},
					FilesToSign: strings.Split(filesToSign, ":"),
				},
			},
		),
		Entry(
			"default UnsignedImage, KeySecret, and CertSecret",
			kmmv1beta1.Module{
				Spec: kmmv1beta1.ModuleSpec{
					ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
						Container: kmmv1beta1.ModuleLoaderContainerSpec{
							Sign: &kmmv1beta1.Sign{
								UnsignedImage: unsignedImage,
								KeySecret:     &v1.LocalObjectReference{Name: keySecret},
								CertSecret:    &v1.LocalObjectReference{Name: certSecret},
							},
						},
					},
				},
			},
			kmmv1beta1.KernelMapping{
				Sign: &kmmv1beta1.Sign{
					FilesToSign: strings.Split(filesToSign, ":"),
				},
			},
		),
		Entry(
			"default FilesToSign only",
			kmmv1beta1.Module{
				Spec: kmmv1beta1.ModuleSpec{
					ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
						Container: kmmv1beta1.ModuleLoaderContainerSpec{
							Sign: &kmmv1beta1.Sign{
								FilesToSign: strings.Split(filesToSign, ":"),
							},
						},
					},
				},
			},
			kmmv1beta1.KernelMapping{
				Sign: &kmmv1beta1.Sign{
					UnsignedImage: unsignedImage,
					KeySecret:     &v1.LocalObjectReference{Name: keySecret},
					CertSecret:    &v1.LocalObjectReference{Name: certSecret},
				},
			},
		),
	)

})
