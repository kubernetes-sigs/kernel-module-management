package worker

import (
	"context"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("imagePuller_PullAndExtract", func() {
	var (
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

		server = httptest.NewServer(registry.New())

		GinkgoWriter.Print("Listening on " + server.URL)

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
	})

	It("should work as expected", func() {
		tmpDir := GinkgoT().TempDir()

		ip := NewImagePuller(tmpDir, GinkgoLogr)

		res, err := ip.PullAndExtract(context.Background(), remoteImageName, true)
		Expect(err).NotTo(HaveOccurred())

		imgRoot := filepath.Join(tmpDir, serverURL.Host, "test", "archive:tag", "fs")
		Expect(res.fsDir).To(Equal(imgRoot))
		Expect(res.pulled).To(BeTrue())

		Expect(imgRoot).To(ExistAsFile(true))
		Expect(filepath.Join(imgRoot, "subdir")).To(ExistAsFile(true))
		Expect(filepath.Join(imgRoot, "subdir", "subsubdir")).To(ExistAsFile(true))

		Expect(filepath.Join(imgRoot, "a")).To(ExistAsFile(false))
		Expect(filepath.Join(imgRoot, "subdir", "b")).To(ExistAsFile(false))
		Expect(filepath.Join(imgRoot, "subdir", "subsubdir", "c")).To(ExistAsFile(false))

		digestFilePath := filepath.Join(tmpDir, serverURL.Host, "test", "archive:tag", "digest")

		Expect(os.ReadFile(digestFilePath)).To(Equal([]byte(srcDigest.String())))
	})

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

		ip := NewImagePuller(tmpDir, GinkgoLogr)

		res, err := ip.PullAndExtract(context.Background(), remoteImageName, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(res.fsDir).To(Equal(filepath.Join(dstDir, "fs")))
		Expect(res.pulled).To(BeFalse())
	})
})
