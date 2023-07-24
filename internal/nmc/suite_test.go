package nmc

import (
	"testing"

	"github.com/kubernetes-sigs/kernel-module-management/internal/test"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"
)

var scheme *runtime.Scheme

func TestSuite(t *testing.T) {
	RegisterFailHandler(Fail)

	var err error

	scheme, err = test.TestScheme()
	Expect(err).NotTo(HaveOccurred())

	RunSpecs(t, "NMC Suite")
}
