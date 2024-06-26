package hub

import (
	"context"

	kmmhub "github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	kmm "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Default", func() {

	var (
		ctx  context.Context
		mcmd *ManagedClusterModuleDefaulter
	)

	BeforeEach(func() {

		mcmd = NewManagedClusterModuleDefaulter(GinkgoLogr)
	})

	It("should fail if it got the wrong type", func() {

		err := mcmd.Default(ctx, nil)
		Expect(err).To(HaveOccurred())
	})

	It("should default the container image tag to `latest`", func() {

		mcm := kmmhub.ManagedClusterModule{
			Spec: kmmhub.ManagedClusterModuleSpec{
				ModuleSpec: kmm.ModuleSpec{
					ModuleLoader: kmm.ModuleLoaderSpec{
						Container: kmm.ModuleLoaderContainerSpec{
							ContainerImage: "myregistry.com/org/image",
						},
					},
				},
			},
		}

		err := mcmd.Default(ctx, &mcm)
		Expect(err).NotTo(HaveOccurred())
		Expect(mcm.Spec.ModuleSpec.ModuleLoader.Container.ContainerImage).To(Equal("myregistry.com/org/image:latest"))
	})

	It("should not default the container image tag to `latest` if a sha is speciied", func() {

		mcm := kmmhub.ManagedClusterModule{
			Spec: kmmhub.ManagedClusterModuleSpec{
				ModuleSpec: kmm.ModuleSpec{
					ModuleLoader: kmm.ModuleLoaderSpec{
						Container: kmm.ModuleLoaderContainerSpec{
							ContainerImage: "myregistry.com/org/image@99999",
						},
					},
				},
			},
		}

		err := mcmd.Default(ctx, &mcm)
		Expect(err).NotTo(HaveOccurred())
		Expect(mcm.Spec.ModuleSpec.ModuleLoader.Container.ContainerImage).To(Equal("myregistry.com/org/image@99999"))
	})

	It("should not default the container image tag to `latest` if a tag is speciied", func() {

		mcm := kmmhub.ManagedClusterModule{
			Spec: kmmhub.ManagedClusterModuleSpec{
				ModuleSpec: kmm.ModuleSpec{
					ModuleLoader: kmm.ModuleLoaderSpec{
						Container: kmm.ModuleLoaderContainerSpec{
							ContainerImage: "myregistry.com/org/image:mytag",
						},
					},
				},
			},
		}

		err := mcmd.Default(ctx, &mcm)
		Expect(err).NotTo(HaveOccurred())
		Expect(mcm.Spec.ModuleSpec.ModuleLoader.Container.ContainerImage).To(Equal("myregistry.com/org/image:mytag"))
	})

	It("should default the kernel-mappings image tags to `latest`", func() {

		mcm := kmmhub.ManagedClusterModule{
			Spec: kmmhub.ManagedClusterModuleSpec{
				ModuleSpec: kmm.ModuleSpec{
					ModuleLoader: kmm.ModuleLoaderSpec{
						Container: kmm.ModuleLoaderContainerSpec{
							KernelMappings: []kmm.KernelMapping{
								{
									ContainerImage: "myregistry.com/org/image",
								},
							},
						},
					},
				},
			},
		}

		err := mcmd.Default(ctx, &mcm)
		Expect(err).NotTo(HaveOccurred())
		Expect(mcm.Spec.ModuleSpec.ModuleLoader.Container.KernelMappings[0].ContainerImage).To(Equal("myregistry.com/org/image:latest"))
	})
})
