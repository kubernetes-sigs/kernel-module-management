package config

import (
	"bytes"
	"fmt"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"time"
)

type Job struct {
	GCDelay time.Duration `yaml:"gcDelay,omitempty"`
}

type Worker struct {
	RunAsUser        *int64  `yaml:"runAsUser"`
	SELinuxType      string  `yaml:"seLinuxType"`
	FirmwareHostPath *string `yaml:"firmwareHostPath,omitempty"`
}

type LeaderElection struct {
	Enabled    bool   `yaml:"enabled"`
	ResourceID string `yaml:"resourceID"`
}

type Metrics struct {
	BindAddress      string `yaml:"bindAddress"`
	EnableAuthnAuthz bool   `yaml:"enableAuthnAuthz"`
	SecureServing    bool   `yaml:"secureServing"`
}

type Config struct {
	HealthProbeBindAddress string         `yaml:"healthProbeBindAddress"`
	Job                    Job            `yaml:"job"`
	LeaderElection         LeaderElection `yaml:"leaderElection"`
	Metrics                Metrics        `yaml:"metrics"`
	WebhookPort            int            `yaml:"webhookPort"`
	Worker                 Worker         `yaml:"worker"`
}

func LoadConfigFromCM(cm *corev1.ConfigMap, cfg *Config) error {
	const configKey = "controller_config.yaml"
	rawConfig, ok := cm.Data[configKey]
	if !ok {
		return fmt.Errorf("key %q not found in ConfigMap %s/%s", configKey, cm.Namespace, cm.Name)
	}
	return decodeStrictYAMLIntoConfig([]byte(rawConfig), cfg)
}

func decodeStrictYAMLIntoConfig(yamlData []byte, config *Config) error {
	decoder := yaml.NewDecoder(bytes.NewReader(yamlData))
	decoder.KnownFields(true)

	if err := decoder.Decode(config); err != nil {
		return fmt.Errorf("error unmarshaling YAML: %v", err)
	}

	return nil
}

func NewDefaultConfig() *Config {
	return &Config{
		HealthProbeBindAddress: ":8081",
		WebhookPort:            9443,
		LeaderElection: LeaderElection{
			Enabled:    true,
			ResourceID: "kmm.sigs.x-k8s.io",
		},
		Metrics: Metrics{
			EnableAuthnAuthz: true,
			BindAddress:      "0.0.0.0:8443",
			SecureServing:    true,
		},
		Worker: Worker{
			RunAsUser:        ptr.To[int64](0),
			SELinuxType:      "spc_t",
			FirmwareHostPath: ptr.To("/lib/firmware"),
		},
	}
}

func (c *Config) ManagerOptions() *manager.Options {
	metrics := server.Options{
		BindAddress:   c.Metrics.BindAddress,
		SecureServing: c.Metrics.SecureServing,
	}

	if c.Metrics.EnableAuthnAuthz {
		metrics.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	return &manager.Options{
		HealthProbeBindAddress: c.HealthProbeBindAddress,
		LeaderElection:         c.LeaderElection.Enabled,
		LeaderElectionID:       c.LeaderElection.ResourceID,
		Metrics:                metrics,
		WebhookServer:          webhook.NewServer(webhook.Options{Port: c.WebhookPort}),
	}
}
