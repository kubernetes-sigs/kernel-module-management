package module

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gomock "github.com/golang/mock/gomock"
	v1 "k8s.io/api/core/v1"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
)

var _ = Describe("ImageExists", func() {
	const (
		imageName = "image-name"
		namespace = "test"
	)

	var (
		ctrl *gomock.Controller
		clnt *client.MockClient

		mockRegistry *registry.MockRegistry

		mld api.ModuleLoaderData
		ctx context.Context
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)

		mockRegistry = registry.NewMockRegistry(ctrl)

		mld = api.ModuleLoaderData{}
		ctx = context.Background()
	})

	It("should return true if the image exists", func() {
		gomock.InOrder(
			mockRegistry.EXPECT().ImageExists(ctx, imageName, gomock.Any(), nil).Return(true, nil),
		)

		exists, err := ImageExists(ctx, clnt, mockRegistry, &mld, namespace, imageName)

		Expect(err).ToNot(HaveOccurred())
		Expect(exists).To(BeTrue())
	})

	It("should return false if the image does not exist", func() {
		gomock.InOrder(
			mockRegistry.EXPECT().ImageExists(ctx, imageName, gomock.Any(), nil).Return(false, nil),
		)

		exists, err := ImageExists(ctx, clnt, mockRegistry, &mld, namespace, imageName)

		Expect(err).ToNot(HaveOccurred())
		Expect(exists).To(BeFalse())
	})

	It("should return an error if the registry call fails", func() {
		gomock.InOrder(
			mockRegistry.EXPECT().ImageExists(ctx, imageName, gomock.Any(), nil).Return(false, errors.New("some-error")),
		)

		exists, err := ImageExists(ctx, clnt, mockRegistry, &mld, namespace, imageName)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("some-error"))
		Expect(exists).To(BeFalse())
	})

	It("should use the ImageRepoSecret if one is specified", func() {
		mld.ImageRepoSecret = &v1.LocalObjectReference{
			Name: "secret",
		}

		gomock.InOrder(
			mockRegistry.EXPECT().ImageExists(ctx, imageName, gomock.Any(), gomock.Not(gomock.Nil())).Return(false, nil),
		)

		exists, err := ImageExists(ctx, clnt, mockRegistry, &mld, namespace, imageName)

		Expect(err).ToNot(HaveOccurred())
		Expect(exists).To(BeFalse())
	})
})
