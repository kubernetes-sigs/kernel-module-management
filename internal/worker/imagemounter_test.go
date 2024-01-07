package worker

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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

var _ = Describe("ImageMounter_mountOCIImage", func() {

	It("good flow", func() {
		tmpDir := GinkgoT().TempDir()

		ociImage, err := crane.Append(empty.Image, "testdata/archive.tar")
		Expect(err).NotTo(HaveOccurred())

		oimh := newOCIImageMounterHelper(GinkgoLogr)
		err = oimh.mountOCIImage(ociImage, tmpDir)

		Expect(err).NotTo(HaveOccurred())
		Expect(filepath.Join(tmpDir, "subdir")).To(BeADirectory())
		Expect(filepath.Join(tmpDir, "subdir", "subsubdir")).To(BeADirectory())

		Expect(filepath.Join(tmpDir, "a")).To(BeARegularFile())
		Expect(filepath.Join(tmpDir, "subdir", "b")).To(BeARegularFile())
		Expect(filepath.Join(tmpDir, "subdir", "subsubdir", "c")).To(BeARegularFile())

		Expect(
			os.Readlink(filepath.Join(tmpDir, "lib-modules-symlink")),
		).To(
			Equal("/lib/modules"),
		)

		Expect(
			os.Readlink(filepath.Join(tmpDir, "symlink")),
		).To(
			Equal("a"),
		)

		Expect(
			sameFiles(filepath.Join(tmpDir, "link"), filepath.Join(tmpDir, "a")),
		).To(
			BeTrue(),
		)
	})
})
