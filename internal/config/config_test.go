package config

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"time"
)

var _ = Describe("LoadConfigFromCM", func() {
	const configKey = "controller_config.yaml"

	It("should return error if config key is missing", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-ns",
				Name:      "test-cm",
			},
			Data: map[string]string{},
		}

		cfg := &Config{}
		err := LoadConfigFromCM(cm, cfg)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring(configKey))
	})

	It("should return error if config YAML is invalid", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-ns",
				Name:      "test-cm",
			},
			Data: map[string]string{
				configKey: "invalid_yaml: :",
			},
		}

		cfg := &Config{}
		err := LoadConfigFromCM(cm, cfg)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("error unmarshaling YAML"))
	})

	It("should populate config fields if configMap is valid", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-ns",
				Name:      "test-cm",
			},
			Data: map[string]string{
				configKey: `
healthProbeBindAddress: ":9090"
webhookPort: 1234
leaderElection:
  enabled: true
  resourceID: "some-id"
metrics:
  bindAddress: "0.0.0.0:9091"
  enableAuthnAuthz: true
  secureServing: false
job:
  gcDelay: "2m"
worker:
  runAsUser: 1000
  seLinuxType: "custom_t"
  firmwareHostPath: "/firmware"
`,
			},
		}

		cfg := &Config{}
		err := LoadConfigFromCM(cm, cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.HealthProbeBindAddress).To(Equal(":9090"))
		Expect(cfg.WebhookPort).To(Equal(1234))
		Expect(cfg.LeaderElection.ResourceID).To(Equal("some-id"))
		Expect(cfg.Worker.SELinuxType).To(Equal("custom_t"))
		Expect(*cfg.Worker.FirmwareHostPath).To(Equal("/firmware"))
		Expect(cfg.Job.GCDelay).To(Equal(2 * time.Minute))
		Expect(*cfg.Worker.RunAsUser).To(Equal(int64(1000)))
	})
})

var _ = Describe("decodeStrictYAMLIntoConfig", func() {
	It("should decode valid YAML into config struct", func() {
		yamlData := []byte(`
healthProbeBindAddress: ":8082"
webhookPort: 8888
job:
  gcDelay: "45s"
`)
		cfg := &Config{}
		err := decodeStrictYAMLIntoConfig(yamlData, cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.HealthProbeBindAddress).To(Equal(":8082"))
		Expect(cfg.WebhookPort).To(Equal(8888))
		Expect(cfg.Job.GCDelay).To(Equal(45 * time.Second))
	})

	It("should return error on unknown field", func() {
		yamlData := []byte(`
someUnknownField: true
`)
		cfg := &Config{}
		err := decodeStrictYAMLIntoConfig(yamlData, cfg)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("field someUnknownField not found"))
	})

	It("should return error on invalid field type", func() {
		yamlData := []byte(`
webhookPort: {"bad": "object"}
`)
		cfg := &Config{}
		err := decodeStrictYAMLIntoConfig(yamlData, cfg)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("cannot unmarshal"))
	})
})

var _ = Describe("ManagerOptions", func() {
	DescribeTable(
		"should enable authn/authz if configured",
		func(enabled bool) {
			c := &Config{
				Metrics: Metrics{EnableAuthnAuthz: enabled},
			}

			mo := c.ManagerOptions()

			if enabled {
				Expect(mo.Metrics.FilterProvider).NotTo(BeNil())
			} else {
				Expect(mo.Metrics.FilterProvider).To(BeNil())
			}
		},
		Entry(nil, false),
		Entry(nil, true),
	)
})
