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
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
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

func sameFiles(a, b string) (bool, error) {
	fiA, err := os.Stat(a)
	if err != nil {
		return false, fmt.Errorf("could not stat() the first file: %v", err)
	}

	fiB, err := os.Stat(b)
	if err != nil {
		return false, fmt.Errorf("could not stat() the second file: %v", err)
	}

	return os.SameFile(fiA, fiB), nil
}

var _ = Describe("imageMounter_MountImage", func() {
	var (
		expectedToken   *string
		remoteImageName string
		srcImg          v1.Image
		srcDigest       v1.Hash
		server          *httptest.Server
		serverURL       *url.URL
	)

	const imagePathAndTag = "/test/archive:tag"

	modConfig := &kmmv1beta1.ModuleConfig{
		InsecurePull: true,
	}

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
				expectedToken = ptr.To(
					base64.StdEncoding.EncodeToString([]byte(username + ":" + password)),
				)

				keyChain = &fakeKeyChainAndAuthenticator{token: *expectedToken}
			}

			rim := NewRemoteImageMounter(tmpDir, keyChain, GinkgoLogr)

			res, err := rim.MountImage(context.Background(), remoteImageName, modConfig)
			Expect(err).NotTo(HaveOccurred())

			imgRoot := filepath.Join(tmpDir, serverURL.Host, "test", "archive:tag", "fs")
			Expect(res).To(Equal(imgRoot))

			Expect(imgRoot).To(BeADirectory())
			Expect(filepath.Join(imgRoot, "subdir")).To(BeADirectory())
			Expect(filepath.Join(imgRoot, "subdir", "subsubdir")).To(BeADirectory())

			Expect(filepath.Join(imgRoot, "a")).To(BeARegularFile())
			Expect(filepath.Join(imgRoot, "subdir", "b")).To(BeARegularFile())
			Expect(filepath.Join(imgRoot, "subdir", "subsubdir", "c")).To(BeARegularFile())

			Expect(
				os.Readlink(filepath.Join(imgRoot, "lib-modules-symlink")),
			).To(
				Equal("/lib/modules"),
			)

			Expect(
				os.Readlink(filepath.Join(imgRoot, "symlink")),
			).To(
				Equal("a"),
			)

			Expect(
				sameFiles(filepath.Join(imgRoot, "link"), filepath.Join(imgRoot, "a")),
			).To(
				BeTrue(),
			)

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

		rim := NewRemoteImageMounter(tmpDir, authn.NewMultiKeychain(), GinkgoLogr)

		res, err := rim.MountImage(context.Background(), remoteImageName, modConfig)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(filepath.Join(dstDir, "fs")))
	})
})
