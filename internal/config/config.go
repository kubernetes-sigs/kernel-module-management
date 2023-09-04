package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

type Worker struct {
	RunAsUser   *int64 `yaml:"runAsUser"`
	SELinuxType string `yaml:"seLinuxType"`
}

type LeaderElection struct {
	Enabled    bool   `yaml:"enabled"`
	ResourceID string `yaml:"resourceID"`
}

type Config struct {
	HealthProbeBindAddress string         `yaml:"healthProbeBindAddress"`
	MetricsBindAddress     string         `yaml:"metricsBindAddress"`
	LeaderElection         LeaderElection `yaml:"leaderElection"`
	WebhookPort            int            `yaml:"webhookPort"`
	Worker                 Worker         `yaml:"worker"`
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
	return &manager.Options{
		HealthProbeBindAddress: c.HealthProbeBindAddress,
		LeaderElection:         c.LeaderElection.Enabled,
		LeaderElectionID:       c.LeaderElection.ResourceID,
		MetricsBindAddress:     c.MetricsBindAddress,
		WebhookServer:          webhook.NewServer(webhook.Options{Port: c.WebhookPort}),
	}
}
