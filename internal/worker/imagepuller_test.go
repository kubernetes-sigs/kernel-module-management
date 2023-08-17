package worker

import (
	"context"
	"encoding/base64"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/pointer"
)

const (
	username = "username"
	password = "password"
)

type fakeKeyChainAndAuthenticator struct {
	token string
}

func (f *fakeKeyChainAndAuthenticator) Resolve(_ authn.Resource) (authn.Authenticator, error) {
	return f, nil
}

func (f *fakeKeyChainAndAuthenticator) Authorization() (*authn.AuthConfig, error) {
	return &authn.AuthConfig{Auth: f.token}, nil
}

var _ = Describe("imagePuller_PullAndExtract", func() {
	var (
		expectedToken   *string
		remoteImageName string
		srcImg          v1.Image
		srcDigest       v1.Hash
		server          *httptest.Server
		serverURL       *url.URL
	)

	const imagePathAndTag = "/test/archive:tag"

	BeforeEach(func() {
		var err error

		srcImg, err = crane.Append(empty.Image, "testdata/archive.tar")
		Expect(err).NotTo(HaveOccurred())

		srcDigest, err = srcImg.Digest()
		Expect(err).NotTo(HaveOccurred())

		ginkgoLogger := log.New(GinkgoWriter, "registry | ", log.LstdFlags)

		mw := func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if expectedToken != nil {
					user, pass, ok := r.BasicAuth()
					if !ok {
						w.WriteHeader(http.StatusUnauthorized)
						return
					}

					if user != username || pass != password {
						http.Error(w, fmt.Sprintf("Unexpected credentials: %s %s", user, pass), http.StatusForbidden)
					}
				}

				next.ServeHTTP(w, r)
			})
		}

		handler := mw(
			registry.New(registry.Logger(ginkgoLogger)),
		)

		server = httptest.NewServer(handler)

		GinkgoWriter.Println("Listening on " + server.URL)

		serverURL, err = url.Parse(server.URL)
		Expect(err).NotTo(HaveOccurred())

		remoteImageName = serverURL.Host + imagePathAndTag

		Expect(
			crane.Push(srcImg, remoteImageName, crane.Insecure),
		).NotTo(
			HaveOccurred(),
		)
	})

	AfterEach(func() {
		server.Close()
		expectedToken = nil
	})

	DescribeTable(
		"should work as expected",
		func(token string) {
			tmpDir := GinkgoT().TempDir()

			keyChain := authn.NewMultiKeychain()

			if token != "" {
				expectedToken = pointer.String(
					base64.StdEncoding.EncodeToString([]byte(username + ":" + password)),
				)

				keyChain = &fakeKeyChainAndAuthenticator{token: *expectedToken}
			}

			ip := NewImagePuller(tmpDir, keyChain, GinkgoLogr)

			res, err := ip.PullAndExtract(context.Background(), remoteImageName, true)
			Expect(err).NotTo(HaveOccurred())

			imgRoot := filepath.Join(tmpDir, serverURL.Host, "test", "archive:tag", "fs")
			Expect(res.fsDir).To(Equal(imgRoot))
			Expect(res.pulled).To(BeTrue())

			Expect(imgRoot).To(BeADirectory())
			Expect(filepath.Join(imgRoot, "subdir")).To(BeADirectory())
			Expect(filepath.Join(imgRoot, "subdir", "subsubdir")).To(BeADirectory())

			Expect(filepath.Join(imgRoot, "a")).To(BeARegularFile())
			Expect(filepath.Join(imgRoot, "subdir", "b")).To(BeARegularFile())
			Expect(filepath.Join(imgRoot, "subdir", "subsubdir", "c")).To(BeARegularFile())

			digestFilePath := filepath.Join(tmpDir, serverURL.Host, "test", "archive:tag", "digest")

			Expect(os.ReadFile(digestFilePath)).To(Equal([]byte(srcDigest.String())))
		},
		Entry("without authentication", ""),
		Entry("with authentication", ""),
	)

	It("should not pull if the digest file exist an has the expected value", func() {
		tmpDir := GinkgoT().TempDir()

		dstDir := filepath.Join(tmpDir, serverURL.Host, "test", "archive:tag")

		Expect(
			os.MkdirAll(dstDir, os.ModeDir|0755),
		).NotTo(
			HaveOccurred(),
		)

		Expect(
			os.WriteFile(filepath.Join(dstDir, "digest"), []byte(srcDigest.String()), 0700),
		).NotTo(
			HaveOccurred(),
		)

		ip := NewImagePuller(tmpDir, authn.NewMultiKeychain(), GinkgoLogr)

		res, err := ip.PullAndExtract(context.Background(), remoteImageName, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.fsDir).To(Equal(filepath.Join(dstDir, "fs")))
		Expect(res.pulled).To(BeFalse())
	})
})
