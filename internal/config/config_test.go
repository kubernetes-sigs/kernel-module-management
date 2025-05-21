package config

import (
	"fmt"
	"github.com/go-logr/logr"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"time"
)

var _ = Describe("overrideConfigFromCM", func() {
	const configKey = "controller_config.yaml"
	var (
		ch configHelperAPI
	)
	BeforeEach(func() {
		ch = newConfigHelper()
	})

	It("should return error if ConfigMap is nil", func() {
		cfg := &Config{}
		err := ch.overrideConfigFromCM(nil, cfg)
		Expect(err).To(HaveOccurred())
	})

	It("should return error if config key is missing", func() {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test-ns",
				Name:      "test-cm",
			},
			Data: map[string]string{},
		}

		cfg := &Config{}
		err := ch.overrideConfigFromCM(cm, cfg)
		Expect(err).To(HaveOccurred())
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
		err := ch.overrideConfigFromCM(cm, cfg)
		Expect(err).To(HaveOccurred())
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
		err := ch.overrideConfigFromCM(cm, cfg)
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

var _ = Describe("GetConfig", func() {
	var (
		mch    *MockconfigHelperAPI
		ctrl   *gomock.Controller
		clnt   *client.MockClient
		ctx    context.Context
		logger logr.Logger
		cg     configGetter
		ch     configHelper
	)
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mch = NewMockconfigHelperAPI(ctrl)
		ctx = context.TODO()
		logger = log.FromContext(ctx)
		cg = configGetter{configHelper: mch, logger: logger}
		ch = configHelper{}
	})

	It("should return error if failed to get client", func() {
		gomock.InOrder(
			mch.EXPECT().newDefaultConfig(false).Return(&Config{}),
			mch.EXPECT().getClient().Return(nil, fmt.Errorf("some error")),
		)

		_, err := cg.GetConfig(ctx, "test-cm", "test-ns", false)
		Expect(err).To(HaveOccurred())
	})

	It("should return default config since ConfigMap not found", func() {
		expectedCfg := ch.newDefaultConfig(false)
		errNotFound := errors.NewNotFound(schema.GroupResource{
			Group:    "kmm",
			Resource: "configmaps",
		}, "my-configmap")
		gomock.InOrder(
			mch.EXPECT().newDefaultConfig(false).Return(expectedCfg),
			mch.EXPECT().getClient().Return(clnt, nil),
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(errNotFound),
		)
		cfg, err := cg.GetConfig(ctx, "test-cm", "test-ns", false)
		Expect(err).To(BeNil())
		Expect(cfg).To(Equal(expectedCfg))
	})
	It("should fail because of failture getting the ConfigMap", func() {
		expectedCfg := ch.newDefaultConfig(false)
		gomock.InOrder(
			mch.EXPECT().newDefaultConfig(false).Return(expectedCfg),
			mch.EXPECT().getClient().Return(clnt, nil),
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error")),
		)
		_, err := cg.GetConfig(ctx, "test-cm", "test-ns", false)
		Expect(err).To(HaveOccurred())
	})

	It("should fail to load config from ConfigMap", func() {
		gomock.InOrder(
			mch.EXPECT().newDefaultConfig(false).Return(&Config{}),
			mch.EXPECT().getClient().Return(clnt, nil),
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(nil),
			mch.EXPECT().overrideConfigFromCM(gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error")),
		)
		_, err := cg.GetConfig(ctx, "test-cm", "test-ns", false)
		Expect(err).To(HaveOccurred())
	})

	It("should load config from ConfigMap", func() {
		cm := &corev1.ConfigMap{}
		gomock.InOrder(
			mch.EXPECT().newDefaultConfig(false).Return(&Config{}),
			mch.EXPECT().getClient().Return(clnt, nil),
			clnt.EXPECT().Get(ctx, gomock.Any(), cm).Return(nil),
			mch.EXPECT().overrideConfigFromCM(gomock.Any(), gomock.Any()).Return(nil),
		)
		_, err := cg.GetConfig(ctx, "test-cm", "test-ns", false)
		Expect(err).To(BeNil())
	})
})

var _ = Describe("newDefaultConfig", func() {
	var (
		ch configHelperAPI
	)
	BeforeEach(func() {
		ch = newConfigHelper()
	})
	It("should return kmm-hub config", func() {
		data, err := os.ReadFile("testdata/hub-config.yaml")
		Expect(err).ToNot(HaveOccurred())
		var expectedCfg Config
		err = ch.decodeStrictYAMLIntoConfig(data, &expectedCfg)

		cfg := ch.newDefaultConfig(true)

		Expect(err).ToNot(HaveOccurred())
		Expect(cfg).To(Equal(&expectedCfg))
	})

	It("should return kmm config", func() {
		data, err := os.ReadFile("testdata/config.yaml")
		Expect(err).ToNot(HaveOccurred())
		var expectedCfg Config
		err = ch.decodeStrictYAMLIntoConfig(data, &expectedCfg)

		cfg := ch.newDefaultConfig(false)

		Expect(err).ToNot(HaveOccurred())
		Expect(cfg).To(Equal(&expectedCfg))
	})

	It("should return error on invalid field type", func() {
		yamlData := []byte(`
webhookPort: {"bad": "object"}
`)
		cfg := &Config{}
		err := ch.decodeStrictYAMLIntoConfig(yamlData, cfg)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("cannot unmarshal"))
	})
})

var _ = Describe("decodeStrictYAMLIntoConfig", func() {
	var (
		ch configHelperAPI
	)
	BeforeEach(func() {
		ch = newConfigHelper()
	})
	It("should decode valid YAML into config struct", func() {
		yamlData := []byte(`
healthProbeBindAddress: ":8082"
webhookPort: 8888
job:
 gcDelay: "45s"
`)
		cfg := &Config{}
		err := ch.decodeStrictYAMLIntoConfig(yamlData, cfg)
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
		err := ch.decodeStrictYAMLIntoConfig(yamlData, cfg)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("field someUnknownField not found"))
	})

	It("should return error on invalid field type", func() {
		yamlData := []byte(`
webhookPort: {"bad": "object"}
`)
		cfg := &Config{}
		err := ch.decodeStrictYAMLIntoConfig(yamlData, cfg)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("cannot unmarshal"))
	})
})

var _ = Describe("GetManagerOptionsFromConfig", func() {
	var (
		ctx    context.Context
		logger logr.Logger
		ch     ConfigGetter
	)
	BeforeEach(func() {
		ctx = context.TODO()
		logger = log.FromContext(ctx)
		ch = NewConfigGetter(logger)
	})
	DescribeTable(
		"should enable authn/authz if configured",
		func(enabled bool) {
			c := &Config{
				Metrics: Metrics{EnableAuthnAuthz: enabled},
			}
			mo := ch.GetManagerOptionsFromConfig(c, nil)
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
