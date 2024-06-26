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

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Default", func() {

	var (
		md  *ModuleDefaulter
		ctx context.Context
	)

	BeforeEach(func() {

		md = NewModuleDefaulter(GinkgoLogr)
		ctx = context.TODO()
	})

	It("should fail if it got the wrong type", func() {

		err := md.Default(ctx, nil)
		Expect(err).To(HaveOccurred())
	})

	It("should default the container image tag to `latest`", func() {

		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						ContainerImage: "myregistry.com/org/image",
					},
				},
			},
		}

		err := md.Default(ctx, &mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(mod.Spec.ModuleLoader.Container.ContainerImage).To(Equal("myregistry.com/org/image:latest"))
	})

	It("should not default the container image tag to `latest` if a sha is speciied", func() {

		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						ContainerImage: "myregistry.com/org/image@99999",
					},
				},
			},
		}

		err := md.Default(ctx, &mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(mod.Spec.ModuleLoader.Container.ContainerImage).To(Equal("myregistry.com/org/image@99999"))
	})

	It("should not default the container image tag to `latest` if a tag is speciied", func() {

		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						ContainerImage: "myregistry.com/org/image:mytag",
					},
				},
			},
		}

		err := md.Default(ctx, &mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(mod.Spec.ModuleLoader.Container.ContainerImage).To(Equal("myregistry.com/org/image:mytag"))
	})

	It("should default the kernel-mappings image tags to `latest`", func() {

		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						KernelMappings: []kmmv1beta1.KernelMapping{
							{
								ContainerImage: "myregistry.com/org/image",
							},
						},
					},
				},
			},
		}

		err := md.Default(ctx, &mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(mod.Spec.ModuleLoader.Container.KernelMappings[0].ContainerImage).To(Equal("myregistry.com/org/image:latest"))
	})
})
