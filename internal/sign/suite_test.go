package sign

import (
	"testing"

	"github.com/kubernetes-sigs/kernel-module-management/internal/test"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSuite(t *testing.T) {
	RegisterFailHandler(Fail)

	var err error

	_, err = test.TestScheme()
	Expect(err).NotTo(HaveOccurred())

	RunSpecs(t, "Build Suite")
}
