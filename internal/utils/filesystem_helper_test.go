package utils

import (
	"os"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RemoveSrcFilesFromDst", func() {

	It("test removal", func() {
		// source
		err := os.MkdirAll("./srcDir/level1", 0750)
		Expect(err).NotTo(HaveOccurred())
		err = os.MkdirAll("./srcDir/level2", 0750)
		Expect(err).NotTo(HaveOccurred())
		err = os.MkdirAll("./srcDir/level4", 0750)
		Expect(err).NotTo(HaveOccurred())
		createEmptyFile("./srcDir/level1/testfile1")
		createEmptyFile("./srcDir/level2/testfile2")
		createEmptyFile("./srcDir/level4/testfile4")

		// destination
		err = os.MkdirAll("./dstDir/level1", 0750)
		Expect(err).NotTo(HaveOccurred())
		err = os.MkdirAll("./dstDir/level2", 0750)
		Expect(err).NotTo(HaveOccurred())
		err = os.MkdirAll("./dstDir/level3", 0750)
		Expect(err).NotTo(HaveOccurred())
		createEmptyFile("./dstDir/level1/testfile1")
		createEmptyFile("./dstDir/level2/testfile2")
		createEmptyFile("./dstDir/level3/testfile3")

		helper := NewFSHelper(logr.Discard())

		err = helper.RemoveSrcFilesFromDst("./srcDir", "./dstDir")
		Expect(err).NotTo(HaveOccurred())

		verifyFileNotExists("./dstDir/level1/testfile1")
		verifyFileNotExists("./dstDir/level2/testfile2")

		verifyFileExists("./dstDir/level3/testfile3")

		defer os.RemoveAll("./dstDir")
		defer os.RemoveAll("./srcDir")
	})
})

func createEmptyFile(filePath string) {
	file, err := os.Create(filePath)
	Expect(err).NotTo(HaveOccurred())
	defer file.Close()
}

func verifyFileExists(filepath string) {
	_, err := os.Stat(filepath)
	Expect(err).NotTo(HaveOccurred())
}

func verifyFileNotExists(filepath string) {
	_, err := os.Stat(filepath)
	Expect(err).To(HaveOccurred())
}
