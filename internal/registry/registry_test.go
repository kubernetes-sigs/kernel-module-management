package registry

import (
	context "context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"

	"github.com/golang/mock/gomock"
	"github.com/google/go-containerregistry/pkg/authn"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kmmv1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	"github.com/qbarrand/oot-operator/internal/auth"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var _ = Describe("ImageExists", func() {

	const (
		validImageHost = "gcr.io"
		validImageOrg  = "org"
		validImageName = "image-name"
		validImageTag  = "some-tag"
		invalidImage   = "non-valid-image-name"
	)

	var (
		ctrl                   *gomock.Controller
		ctx                    context.Context
		mockRegistryAuthGetter *auth.MockRegistryAuthGetter
		reg                    Registry
		validImage             = fmt.Sprintf("%s/%s/%s:%s", validImageHost, validImageOrg, validImageName, validImageTag)
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

			_, err = reg.ImageExists(ctx, invalidImage, kmmv1beta1.PullOptions{}, nil)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not contain hash or tag"))
		})

		It("should fail if it cannot get key chain from secret", func() {

			mockRegistryAuthGetter.EXPECT().GetKeyChain(ctx).Return(nil, errors.New("some error"))

			_, err = reg.ImageExists(ctx, validImage, kmmv1beta1.PullOptions{}, mockRegistryAuthGetter)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot get keychain from the registry auth getter"))
		})
	})

	Context("Cannot get image manifest ", func() {

		var err error

		AfterEach(func() {
			Expect(err.Error()).To(ContainSubstring("could not get image"))
			Expect(err.Error()).To(ContainSubstring("failed to get manifest stream from image"))
		})

		It("should fail if it cannot get crane manifest", func() {

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
			}))
			defer server.Close()
			u := mustParseURL(server.URL)

			image := fmt.Sprintf("%s/%s/%s:%s", u.Host, validImageOrg, validImageName, validImageTag)
			_, err = reg.ImageExists(ctx, image, kmmv1beta1.PullOptions{}, nil)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get crane manifest from image"))
		})

		It("should fail if it cannot unmarshal crane manifest", func() {

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// return no error but with an empty manifest (which is not a valid manifest file)
			}))
			defer server.Close()
			u := mustParseURL(server.URL)

			image := fmt.Sprintf("%s/%s/%s:%s", u.Host, validImageOrg, validImageName, validImageTag)
			_, err = reg.ImageExists(ctx, image, kmmv1beta1.PullOptions{}, nil)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to unmarshal crane manifest"))
		})

		It("should fail if the manifest doesn't contain a 'mediaType' field", func() {

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				manifest, err := os.ReadFile("testdata/image_manifest.json")
				Expect(err).NotTo(HaveOccurred())

				release := unstructured.Unstructured{}
				err = json.Unmarshal(manifest, &release.Object)
				Expect(err).NotTo(HaveOccurred())
				unstructured.RemoveNestedField(release.Object, "mediaType")
				manifest, err = json.Marshal(release.Object)
				Expect(err).NotTo(HaveOccurred())

				_, err = w.Write(manifest)
				Expect(err).NotTo(HaveOccurred())
			}))
			defer server.Close()
			u := mustParseURL(server.URL)

			image := fmt.Sprintf("%s/%s/%s:%s", u.Host, validImageOrg, validImageName, validImageTag)
			_, err = reg.ImageExists(ctx, image, kmmv1beta1.PullOptions{}, nil)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("mediaType is missing from the image"))
		})
	})

	It("should not fail if the image doesn't exist", func() {

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer server.Close()
		u := mustParseURL(server.URL)

		image := fmt.Sprintf("%s/%s/%s:%s", u.Host, validImageOrg, validImageName, validImageTag)
		_, err := reg.ImageExists(ctx, image, kmmv1beta1.PullOptions{}, nil)

		Expect(err).ToNot(HaveOccurred())
	})

	DescribeTable("should work as expected", func(withRegistryAuthGetter bool) {

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			manifest, err := os.ReadFile("testdata/image_manifest.json")
			Expect(err).NotTo(HaveOccurred())
			_, err = w.Write(manifest)
			Expect(err).NotTo(HaveOccurred())
		}))
		defer server.Close()
		u := mustParseURL(server.URL)

		if withRegistryAuthGetter {
			mockRegistryAuthGetter.EXPECT().GetKeyChain(ctx).Return(authn.DefaultKeychain, nil)
		}

		var err error
		image := fmt.Sprintf("%s/%s/%s:%s", u.Host, validImageOrg, validImageName, validImageTag)
		if withRegistryAuthGetter {
			_, err = reg.ImageExists(ctx, image, kmmv1beta1.PullOptions{}, mockRegistryAuthGetter)
		} else {
			_, err = reg.ImageExists(ctx, image, kmmv1beta1.PullOptions{}, nil)
		}
		Expect(err).ToNot(HaveOccurred())
	},
		Entry("with public registry", false),
		Entry("with private registry", true),
	)
})

var _ = Describe("GetLayersDigests", func() {

	const (
		validImageHost = "gcr.io"
		validImageOrg  = "org"
		validImageName = "image-name"
		validImageTag  = "some-tag"
		invalidImage   = "non-valid-image-name"
	)

	var (
		ctrl                   *gomock.Controller
		ctx                    context.Context
		mockRegistryAuthGetter *auth.MockRegistryAuthGetter
		reg                    Registry
		validImage             = fmt.Sprintf("%s/%s/%s:%s", validImageHost, validImageOrg, validImageName, validImageTag)
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

			_, err = reg.ImageExists(ctx, invalidImage, kmmv1beta1.PullOptions{}, nil)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("does not contain hash or tag"))
		})

		It("should fail if it cannot get key chain from secret", func() {

			mockRegistryAuthGetter.EXPECT().GetKeyChain(ctx).Return(nil, errors.New("some error"))

			_, err = reg.ImageExists(ctx, validImage, kmmv1beta1.PullOptions{}, mockRegistryAuthGetter)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("cannot get keychain from the registry auth getter"))
		})
	})

	Context("Cannot get image manifest ", func() {

		var err error

		AfterEach(func() {
			Expect(err.Error()).To(ContainSubstring("failed to get manifest from image"))
			Expect(err.Error()).To(ContainSubstring("failed to get manifest stream from image"))
		})

		It("should fail if it cannot get crane manifest", func() {

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
			}))
			defer server.Close()
			u := mustParseURL(server.URL)

			image := fmt.Sprintf("%s/%s/%s:%s", u.Host, validImageOrg, validImageName, validImageTag)
			_, _, err = reg.GetLayersDigests(ctx, image, nil)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get crane manifest from image"))
		})

		It("should fail if it cannot unmarshal crane manifest", func() {

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				// return no error but with an empty manifest (which is not a valid manifest file)
			}))
			defer server.Close()
			u := mustParseURL(server.URL)

			image := fmt.Sprintf("%s/%s/%s:%s", u.Host, validImageOrg, validImageName, validImageTag)
			_, _, err = reg.GetLayersDigests(ctx, image, nil)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to unmarshal crane manifest"))
		})

		It("should fail if the manifest doesn't contain a 'mediaType' field", func() {

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				manifest, err := os.ReadFile("testdata/image_manifest.json")
				Expect(err).NotTo(HaveOccurred())

				release := unstructured.Unstructured{}
				err = json.Unmarshal(manifest, &release.Object)
				Expect(err).NotTo(HaveOccurred())
				unstructured.RemoveNestedField(release.Object, "mediaType")
				manifest, err = json.Marshal(release.Object)
				Expect(err).NotTo(HaveOccurred())

				_, err = w.Write(manifest)
				Expect(err).NotTo(HaveOccurred())
			}))
			defer server.Close()
			u := mustParseURL(server.URL)

			image := fmt.Sprintf("%s/%s/%s:%s", u.Host, validImageOrg, validImageName, validImageTag)
			_, _, err = reg.GetLayersDigests(ctx, image, nil)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("mediaType is missing from the image"))
		})
	})

	DescribeTable("should work as expected", func(withRegistryAuthGetter bool) {

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			manifest, err := os.ReadFile("testdata/image_manifest.json")
			Expect(err).NotTo(HaveOccurred())
			_, err = w.Write(manifest)
			Expect(err).NotTo(HaveOccurred())
		}))
		defer server.Close()
		u := mustParseURL(server.URL)

		if withRegistryAuthGetter {
			mockRegistryAuthGetter.EXPECT().GetKeyChain(ctx).Return(authn.DefaultKeychain, nil)
		}

		var err error
		image := fmt.Sprintf("%s/%s/%s:%s", u.Host, validImageOrg, validImageName, validImageTag)
		if withRegistryAuthGetter {
			_, _, err = reg.GetLayersDigests(ctx, image, mockRegistryAuthGetter)
		} else {
			_, _, err = reg.GetLayersDigests(ctx, image, nil)
		}
		Expect(err).ToNot(HaveOccurred())
	},
		Entry("with public registry", false),
		Entry("with private registry", true),
	)
})

var _ = Describe("VerifyModuleExists", func() {
	reg := NewRegistry()

	It("file is not present", func() {
		const fileName = "/etc/fileName"
		layer, err := prepareLayer(fileName, []byte("some data"))
		Expect(err).ToNot(HaveOccurred())

		res := reg.VerifyModuleExists(layer, "", "somekernel", "module_name.ko")
		Expect(res).To(BeFalse())
	})

	It("file is present", func() {
		const fileName = "/opt/lib/modules/somekernel/module_name.ko"
		layer, err := prepareLayer(fileName, []byte("some data"))
		Expect(err).ToNot(HaveOccurred())

		res := reg.VerifyModuleExists(layer, "/opt", "somekernel", "module_name.ko")
		Expect(res).To(BeTrue())
	})
})

func mustParseURL(rawURL string) *url.URL {
	u, err := url.Parse(rawURL)
	Expect(err).ToNot(HaveOccurred())
	return u
}
