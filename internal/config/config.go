package config

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"os"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"time"

	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	"sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
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

type UserConfig struct {
	SELinuxType      *string        `yaml:"seLinuxType,omitempty"`
	FirmwareHostPath *string        `yaml:"firmwareHostPath,omitempty"`
	GCDelay          *time.Duration `yaml:"gcDelay,omitempty"`
}

func LoadUserConfigFromCM(ctx context.Context, reader ctrlclient.Reader, nsName types.NamespacedName) (*UserConfig, error) {
	cm := &corev1.ConfigMap{}
	if err := reader.Get(ctx, nsName, cm); err != nil {
		return nil, fmt.Errorf("failed to get ConfigMap %s/%s: %v", nsName.Namespace, nsName.Name, err)
	}
	uc := &UserConfig{}

	if val, ok := cm.Data["seLinuxType"]; ok {
		uc.SELinuxType = &val
	}
	if val, ok := cm.Data["firmwareHostPath"]; ok {
		uc.FirmwareHostPath = &val
	}
	if val, ok := cm.Data["gcDelay"]; ok {
		duration, err := time.ParseDuration(val)
		if err != nil {
			return nil, fmt.Errorf("invalid duration format for gcDelay: %v", err)
		}
		uc.GCDelay = &duration
	}

	return uc, nil
}

func MergeUserConfigInto(uc *UserConfig, cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("cfg must not be nil")
	}
	if uc == nil {
		return nil
	}
	if uc.SELinuxType != nil {
		cfg.Worker.SELinuxType = *uc.SELinuxType
	}
	if uc.FirmwareHostPath != nil {
		cfg.Worker.FirmwareHostPath = uc.FirmwareHostPath
	}
	if uc.GCDelay != nil {
		cfg.Job.GCDelay = *uc.GCDelay
	}
	return nil
}

func ParseFile(path string) (*Config, error) {
	fd, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("could not open the configuration file: %v", err)
	}
	defer fd.Close()

	cfg := Config{}

	if err = yaml.NewDecoder(fd).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("could not decode configuration file: %v", err)
	}

	return &cfg, nil
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
