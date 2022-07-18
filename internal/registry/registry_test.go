package registry

import (
	context "context"
	"errors"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/internal/auth"
	v1 "k8s.io/api/core/v1"
)

var _ = Describe("ImageExists", func() {

	const (
		psName       = "pull-push-secrert"
		psNamespace  = "default"
		validImage   = "gcr.io/org/image-name:some-tag"
		invalidImage = "non-valid-image-name"
	)

	var (
		ctrl             *gomock.Controller
		ctx              context.Context
		mockRegistryAuth *auth.MockRegistryAuth
		reg              Registry
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		ctx = context.TODO()
		mockRegistryAuth = auth.NewMockRegistryAuth(ctrl)
		reg = NewRegistry(mockRegistryAuth)
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

			_, err = reg.ImageExists(ctx, invalidImage, ootov1alpha1.PullOptions{}, nil, "")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not contain hash or tag"))
		})

		It("should fail if it cannot get key chain from secret", func() {

			mockRegistryAuth.EXPECT().GetKeyChainFromSecret(ctx, psName, psNamespace).Return(nil, errors.New("some error"))

			_, err = reg.ImageExists(ctx, validImage, ootov1alpha1.PullOptions{}, &v1.LocalObjectReference{Name: psName}, psNamespace)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot get keychain for secret"))
		})
	})
})

var _ = Describe("GetLayersDigests", func() {

	const (
		psName       = "pull-push-secrert"
		psNamespace  = "default"
		validImage   = "gcr.io/org/image-name:some-tag"
		invalidImage = "non-valid-image-name"
	)

	var (
		ctrl             *gomock.Controller
		ctx              context.Context
		mockRegistryAuth *auth.MockRegistryAuth
		reg              Registry
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		ctx = context.TODO()
		mockRegistryAuth = auth.NewMockRegistryAuth(ctrl)
		reg = NewRegistry(mockRegistryAuth)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("should fail if the image name isn't valid", func() {

		_, _, err := reg.GetLayersDigests(ctx, invalidImage)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get pull options for image"))
		Expect(err.Error()).To(ContainSubstring("does not contain hash or tag"))
	})
})
