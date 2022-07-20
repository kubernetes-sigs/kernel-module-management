package registry

import (
	context "context"
	"errors"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/internal/auth"
)

var _ = Describe("ImageExists", func() {

	const (
		validImage   = "gcr.io/org/image-name:some-tag"
		invalidImage = "non-valid-image-name"
	)

	var (
		ctrl                   *gomock.Controller
		ctx                    context.Context
		mockRegistryAuthGetter *auth.MockRegistryAuthGetter
		reg                    Registry
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		ctx = context.TODO()
		mockRegistryAuthGetter = auth.NewMockRegistryAuthGetter(ctrl)
		reg = NewRegistry()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("Cannot get pull options", func() {

		var err error

		AfterEach(func() {
			Expect(err.Error()).To(ContainSubstring("failed to get pull options for image"))
		})

		It("should fail if the image name isn't valid", func() {

			_, err = reg.ImageExists(ctx, invalidImage, ootov1alpha1.PullOptions{}, nil)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not contain hash or tag"))
		})

		It("should fail if it cannot get key chain from secret", func() {

			mockRegistryAuthGetter.EXPECT().GetKeyChain(ctx).Return(nil, errors.New("some error"))

			_, err = reg.ImageExists(ctx, validImage, ootov1alpha1.PullOptions{}, mockRegistryAuthGetter)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot get keychain from the registry auth getter"))
		})
	})
})

var _ = Describe("GetLayersDigests", func() {

	const (
		validImage   = "gcr.io/org/image-name:some-tag"
		invalidImage = "non-valid-image-name"
	)

	var (
		ctrl                   *gomock.Controller
		ctx                    context.Context
		mockRegistryAuthGetter *auth.MockRegistryAuthGetter
		reg                    Registry
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		ctx = context.TODO()
		mockRegistryAuthGetter = auth.NewMockRegistryAuthGetter(ctrl)
		reg = NewRegistry()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("Cannot get pull options", func() {

		var err error

		AfterEach(func() {
			Expect(err.Error()).To(ContainSubstring("failed to get pull options for image"))
		})

		It("should fail if the image name isn't valid", func() {

			_, err = reg.ImageExists(ctx, invalidImage, ootov1alpha1.PullOptions{}, nil)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not contain hash or tag"))
		})

		It("should fail if it cannot get key chain from secret", func() {

			mockRegistryAuthGetter.EXPECT().GetKeyChain(ctx).Return(nil, errors.New("some error"))

			_, err = reg.ImageExists(ctx, validImage, ootov1alpha1.PullOptions{}, mockRegistryAuthGetter)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot get keychain from the registry auth getter"))
		})
	})
})
