package worker

import (
	"context"

	"github.com/google/go-containerregistry/pkg/name"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ReadKubernetesSecrets", func() {
	It("should work as expected", func() {
		kc, err := ReadKubernetesSecrets(context.TODO(), "testdata/pull-secrets", GinkgoLogr)
		Expect(err).NotTo(HaveOccurred())

		By("dockercfg")

		dockerCfgTag, err := name.NewTag("dockercfg.registry/repo/image")
		Expect(err).NotTo(HaveOccurred())

		dockerCfgAuthenticator, err := kc.Resolve(dockerCfgTag)
		Expect(err).NotTo(HaveOccurred())

		dockerCfgAuthconfig, err := dockerCfgAuthenticator.Authorization()
		Expect(err).NotTo(HaveOccurred())

		Expect(dockerCfgAuthconfig.Username).To(Equal("username"))
		Expect(dockerCfgAuthconfig.Password).To(Equal("dockercfg"))

		By("dockerconfigjson")

		dockerConfigJsonTag, err := name.NewTag("dockerconfigjson.registry/repo/image")
		Expect(err).NotTo(HaveOccurred())

		dockerConfigJsonAuthenticator, err := kc.Resolve(dockerConfigJsonTag)
		Expect(err).NotTo(HaveOccurred())

		dockerConfigJsonAuthconfig, err := dockerConfigJsonAuthenticator.Authorization()
		Expect(err).NotTo(HaveOccurred())

		Expect(dockerConfigJsonAuthconfig.Username).To(Equal("username"))
		Expect(dockerConfigJsonAuthconfig.Password).To(Equal("dockerconfigjson"))

	})
})
