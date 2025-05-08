package config

import (
	"fmt"
	mockclient "github.com/kubernetes-sigs/kernel-module-management/internal/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

var _ = Describe("LoadUserConfigFromCM", func() {
	var (
		ctx    context.Context
		client *mockclient.MockClient
		nm     types.NamespacedName
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl := gomock.NewController(GinkgoT())
		client = mockclient.NewMockClient(ctrl)
		nm = types.NamespacedName{Namespace: "test-ns", Name: "test-name"}
	})

	It("should return error if ConfigMap does not exist", func() {
		client.EXPECT().Get(ctx, nm, gomock.Any()).Return(fmt.Errorf("some error"))
		_, err := LoadUserConfigFromCM(ctx, client, nm)
		Expect(err).To(HaveOccurred())
	})

	It("should load existing ConfigMap with relevant keys", func() {
		ExpectedCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "kmm",
				Name:      "user-config",
			},
			Data: map[string]string{
				"seLinuxType":      "spc_t",
				"firmwareHostPath": "/lib/firmware",
			},
		}
		client.EXPECT().
			Get(ctx, nm, gomock.AssignableToTypeOf(&corev1.ConfigMap{})).
			DoAndReturn(func(_ context.Context, _ types.NamespacedName, obj ctrlclient.Object, _ ...interface{}) error {
				cm := obj.(*corev1.ConfigMap)
				cm.ObjectMeta = ExpectedCM.ObjectMeta
				cm.Data = ExpectedCM.Data
				return nil
			})

		uc, err := LoadUserConfigFromCM(ctx, client, nm)
		Expect(err).NotTo(HaveOccurred())
		Expect(uc).NotTo(BeNil())
		Expect(*uc.SELinuxType).To(Equal("spc_t"))
		Expect(*uc.FirmwareHostPath).To(Equal("/lib/firmware"))
	})
})

var _ = Describe("MergeUserConfigInto", func() {
	It("should return error if cfg is nil", func() {
		err := MergeUserConfigInto(&UserConfig{}, nil)
		Expect(err).To(HaveOccurred())
	})

	It("should return nil if uc is nil", func() {
		firmwarePath := ptr.To("/bar")
		config := &Config{
			Worker: Worker{
				FirmwareHostPath: firmwarePath,
				SELinuxType:      "foo_t",
			},
		}
		err := MergeUserConfigInto(nil, config)
		Expect(err).NotTo(HaveOccurred())
		Expect(config.Worker.SELinuxType).To(Equal("foo_t"))
		Expect(config.Worker.FirmwareHostPath).To(Equal(firmwarePath))
	})

	It("should apply user config to cfg.Worker", func() {
		uc := &UserConfig{
			SELinuxType:      ptr.To("foo_t"),
			FirmwareHostPath: ptr.To("/bar"),
		}
		cfg := &Config{
			Worker: Worker{},
		}
		err := MergeUserConfigInto(uc, cfg)
		Expect(err).NotTo(HaveOccurred())
		Expect(cfg.Worker.SELinuxType).To(Equal("foo_t"))
		Expect(*cfg.Worker.FirmwareHostPath).To(Equal("/bar"))
	})
})

var _ = Describe("ParseFile", func() {
	It("should return an error if the file does not exist", func() {
		_, err := ParseFile("/non/existent/path")
		Expect(err).To(HaveOccurred())
	})

	It("should parse the file correctly", func() {
		expected := &Config{
			HealthProbeBindAddress: ":8081",
			Job: Job{
				GCDelay: time.Hour,
			},
			LeaderElection: LeaderElection{
				Enabled:    true,
				ResourceID: "some-resource-id",
			},
			Metrics: Metrics{
				BindAddress:      "0.0.0.0:8443",
				EnableAuthnAuthz: true,
				SecureServing:    true,
			},
			WebhookPort: 9443,
			Worker: Worker{
				RunAsUser:        ptr.To[int64](1234),
				SELinuxType:      "mySELinuxType",
				FirmwareHostPath: ptr.To("/some/path"),
			},
		}

		Expect(
			ParseFile("testdata/config.yaml"),
		).To(
			Equal(expected),
		)
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
