package config

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ParseFile", func() {
	It("should return an error if the file does not exist", func() {
		_, err := ParseFile("/non/existent/path")
		Expect(err).To(HaveOccurred())
	})

	It("should parse the file correctly", func() {
		expected := &Config{
			HealthProbeBindAddress: ":8081",
			MetricsBindAddress:     "127.0.0.1:8080",
			LeaderElection: LeaderElection{
				Enabled:    true,
				ResourceID: "some-resource-id",
			},
			WebhookPort: 9443,
		}

		Expect(
			ParseFile("testdata/config.yaml"),
		).To(
			Equal(expected),
		)
	})
})
