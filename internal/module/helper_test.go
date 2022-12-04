package module

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gomock "github.com/golang/mock/gomock"
	v1 "k8s.io/api/core/v1"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
)

var _ = Describe("TLSOptions", func() {
	It("should return the KernelMapping's TLSOptions if it's defined", func() {
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{},
				},
			},
		}
		km := kmmv1beta1.KernelMapping{
			RegistryTLS: &kmmv1beta1.TLSOptions{},
		}

		Expect(
			TLSOptions(mod.Spec, km),
		).To(
			Equal(km.RegistryTLS),
		)
	})

	It("should return the Module's TLSOptions if the KernelMapping's TLSOptions is not defined", func() {
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						RegistryTLS: kmmv1beta1.TLSOptions{},
					},
				},
			},
		}
		km := kmmv1beta1.KernelMapping{}

		Expect(
			*TLSOptions(mod.Spec, km),
		).To(
			Equal(mod.Spec.ModuleLoader.Container.RegistryTLS),
		)
	})
})

var _ = Describe("AppendToTag", func() {
	It("should append a tag to the image name", func() {
		name := "some-container-image-name"
		tag := "a-kmm-tag"

		Expect(
			AppendToTag(name, tag),
		).To(
			Equal(name + ":" + tag),
		)
	})

	It("should add a suffix to the already present tag", func() {
		name := "some-container-image-name:with-a-tag"
		tag := "a-kmm-tag-suffix"

		Expect(
			AppendToTag(name, tag),
		).To(
			Equal(name + "_" + tag),
		)
	})
})

var _ = Describe("IntermediateImageName", func() {
	It("should add the kmm_unsigned suffix to the target image name", func() {
		Expect(
			IntermediateImageName("module-name", "test-namespace", "some-image-name"),
		).To(
			Equal("some-image-name:test-namespace_module-name_kmm_unsigned"),
		)
	})
})

var _ = Describe("ShouldBeBuilt", func() {
	It("should return true if the Module's Build is defined", func() {
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						Build: &kmmv1beta1.Build{},
					},
				},
			},
		}
		km := kmmv1beta1.KernelMapping{}

		Expect(
			ShouldBeBuilt(mod.Spec, km),
		).To(
			BeTrue(),
		)
	})

	It("should return true if the KernelMapping's Build is defined", func() {
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{},
				},
			},
		}
		km := kmmv1beta1.KernelMapping{
			Build: &kmmv1beta1.Build{},
		}

		Expect(
			ShouldBeBuilt(mod.Spec, km),
		).To(
			BeTrue(),
		)
	})

	It("should return false if neither the Module's nor the KernelMapping's Build is defined", func() {
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{},
				},
			},
		}
		km := kmmv1beta1.KernelMapping{}

		Expect(
			ShouldBeBuilt(mod.Spec, km),
		).To(
			BeFalse(),
		)
	})
})

var _ = Describe("ShouldBeSigned", func() {
	It("should return true if the Module's Sign is defined", func() {
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						Sign: &kmmv1beta1.Sign{},
					},
				},
			},
		}
		km := kmmv1beta1.KernelMapping{}

		Expect(
			ShouldBeSigned(mod.Spec, km),
		).To(
			BeTrue(),
		)
	})

	It("should return true if the KernelMapping's Sign is defined", func() {
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{},
				},
			},
		}
		km := kmmv1beta1.KernelMapping{
			Sign: &kmmv1beta1.Sign{},
		}

		Expect(
			ShouldBeSigned(mod.Spec, km),
		).To(
			BeTrue(),
		)
	})

	It("should return false if neither the Module's nor the KernelMapping's Sign is defined", func() {
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{},
				},
			},
		}
		km := kmmv1beta1.KernelMapping{}

		Expect(
			ShouldBeSigned(mod.Spec, km),
		).To(
			BeFalse(),
		)
	})
})

var _ = Describe("ImageExists", func() {
	const (
		imageName = "image-name"
		namespace = "test"
	)

	var (
		ctrl *gomock.Controller
		clnt *client.MockClient

		mockRegistry *registry.MockRegistry

		mod kmmv1beta1.Module
		km  kmmv1beta1.KernelMapping
		ctx context.Context
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)

		mockRegistry = registry.NewMockRegistry(ctrl)

		mod = kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{},
				},
			},
		}

		km = kmmv1beta1.KernelMapping{}

		ctx = context.Background()
	})

	It("should return true if the image exists", func() {
		gomock.InOrder(
			mockRegistry.EXPECT().ImageExists(ctx, imageName, gomock.Any(), nil).Return(true, nil),
		)

		exists, err := ImageExists(ctx, clnt, mockRegistry, mod.Spec, namespace, km, imageName)

		Expect(err).ToNot(HaveOccurred())
		Expect(exists).To(BeTrue())
	})

	It("should return false if the image does not exist", func() {
		gomock.InOrder(
			mockRegistry.EXPECT().ImageExists(ctx, imageName, gomock.Any(), nil).Return(false, nil),
		)

		exists, err := ImageExists(ctx, clnt, mockRegistry, mod.Spec, namespace, km, imageName)

		Expect(err).ToNot(HaveOccurred())
		Expect(exists).To(BeFalse())
	})

	It("should return an error if the registry call fails", func() {
		gomock.InOrder(
			mockRegistry.EXPECT().ImageExists(ctx, imageName, gomock.Any(), nil).Return(false, errors.New("some-error")),
		)

		exists, err := ImageExists(ctx, clnt, mockRegistry, mod.Spec, namespace, km, imageName)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("some-error"))
		Expect(exists).To(BeFalse())
	})

	It("should use the ImageRepoSecret if one is specified", func() {
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ImageRepoSecret: &v1.LocalObjectReference{
					Name: "secret",
				},
			},
		}

		gomock.InOrder(
			mockRegistry.EXPECT().ImageExists(ctx, imageName, gomock.Any(), gomock.Not(gomock.Nil())).Return(false, nil),
		)

		exists, err := ImageExists(ctx, clnt, mockRegistry, mod.Spec, namespace, km, imageName)

		Expect(err).ToNot(HaveOccurred())
		Expect(exists).To(BeFalse())
	})
})
