package auth

import (
	"context"
	"errors"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/qbarrand/oot-operator/internal/client"
	v1 "k8s.io/api/core/v1"
)

var _ = Describe("GetKeyChainFromSecret", func() {

	const (
		secretName      = "pull-push-secret"
		secretNamespace = "default"
	)

	var (
		ctrl       *gomock.Controller
		ctx        context.Context
		mockClient *client.MockClient
		a          RegistryAuth
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		ctx = context.TODO()
		mockClient = client.NewMockClient(ctrl)
		a = NewRegistryAuth(mockClient)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	It("should fail if it cannot get the secret", func() {

		mockClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(errors.New("some error"))

		_, err := a.GetKeyChainFromSecret(ctx, secretName, secretNamespace)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("cannot find secret"))
	})

	It("should fail if the secret doesn't contains auth data", func() {

		mockClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, _ interface{}, s *v1.Secret) error {
				s.Type = v1.SecretTypeDockerConfigJson
				s.Data = map[string][]byte{
					v1.DockerConfigJsonKey: []byte("some data"),
				}
				return nil
			},
		)

		_, err := a.GetKeyChainFromSecret(ctx, secretName, secretNamespace)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("could not create a keycahin from secret"))
	})

	It("should work as expected", func() {

		mockClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(nil)

		_, err := a.GetKeyChainFromSecret(ctx, secretName, secretNamespace)
		Expect(err).NotTo(HaveOccurred())
	})
})
