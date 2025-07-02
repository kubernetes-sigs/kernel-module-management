package config

import (
	"bytes"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/net/context"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

var ErrCannotUseCustomConfig = errors.New("cannot use custom config on top of the default config; using default configs")

//go:generate mockgen -source=config.go -package=config -destination=mock_config.go ConfigGetter,configHelperAPI
type ConfigGetter interface {
	GetConfig(ctx context.Context, userConfigMapName, userConfigMapNamespace string, isHubConfig bool) (*Config, error)
	GetManagerOptionsFromConfig(conf *Config, scheme *runtime.Scheme) manager.Options
}

func NewConfigGetter(logger logr.Logger) ConfigGetter {
	return &configGetter{
		configHelper: newConfigHelper(),
		logger:       logger,
	}
}

func (cg *configGetter) GetConfig(ctx context.Context,
	userConfigMapName, userConfigMapNamespace string,
	isHubConfig bool) (*Config, error) {

	defaultCfg := cg.configHelper.newDefaultConfig(isHubConfig)
	clnt, err := cg.configHelper.getClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get client %v", err)
	}

	managerConfig := &corev1.ConfigMap{}
	namespacedName := types.NamespacedName{
		Namespace: userConfigMapNamespace,
		Name:      userConfigMapName,
	}
	if err := clnt.Get(ctx, namespacedName, managerConfig); err != nil {
		if k8serrors.IsNotFound(err) {
			cg.logger.Info("No ConfigMap configuring the manager was found in namespace, using default configuration",
				"namespace", userConfigMapNamespace, "name", userConfigMapName)
			return defaultCfg, nil
		}
		return nil, fmt.Errorf("failed to get ConfigMap %s/%s: %v", userConfigMapName, userConfigMapNamespace, err)
	}

	cfg := cg.configHelper.newDefaultConfig(isHubConfig)
	err = cg.configHelper.overrideConfigFromCM(managerConfig, cfg)
	if err != nil {
		return defaultCfg, fmt.Errorf("%w; unable to load KMM config from ConfigMap: %v", ErrCannotUseCustomConfig, err)
	}
	return cfg, nil
}

func (cg *configGetter) GetManagerOptionsFromConfig(conf *Config, scheme *runtime.Scheme) manager.Options {

	metrics := server.Options{
		BindAddress:   conf.Metrics.BindAddress,
		SecureServing: conf.Metrics.SecureServing,
	}

	if conf.Metrics.EnableAuthnAuthz {
		metrics.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	return manager.Options{
		HealthProbeBindAddress: conf.HealthProbeBindAddress,
		LeaderElection:         conf.LeaderElection.Enabled,
		LeaderElectionID:       conf.LeaderElection.ResourceID,
		Metrics:                metrics,
		WebhookServer:          webhook.NewServer(webhook.Options{Port: conf.WebhookPort}),
		Scheme:                 scheme,
	}
}

type configHelperAPI interface {
	getClient() (client.Client, error)
	overrideConfigFromCM(cm *corev1.ConfigMap, cfg *Config) error
	decodeStrictYAMLIntoConfig(yamlData []byte, config *Config) error
	newDefaultConfig(isHubConfig bool) *Config
}
type configGetter struct {
	configHelper configHelperAPI
	logger       logr.Logger
}

type configHelper struct{}

func newConfigHelper() configHelperAPI {
	return &configHelper{}
}

func (ch *configHelper) getClient() (client.Client, error) {
	clnt, err := client.New(ctrl.GetConfigOrDie(), client.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to create new client %v", err)
	}
	return clnt, nil
}

func (ch *configHelper) overrideConfigFromCM(cm *corev1.ConfigMap, cfg *Config) error {
	const configKey = "controller_config.yaml"
	if cm == nil {
		return fmt.Errorf("cm can not be nil")
	}
	rawConfig, ok := cm.Data[configKey]
	if !ok {
		return fmt.Errorf("key %q not found in ConfigMap %s/%s", configKey, cm.Namespace, cm.Name)
	}
	return ch.decodeStrictYAMLIntoConfig([]byte(rawConfig), cfg)
}

func (ch *configHelper) decodeStrictYAMLIntoConfig(yamlData []byte, config *Config) error {
	decoder := yaml.NewDecoder(bytes.NewReader(yamlData))
	decoder.KnownFields(true)

	if err := decoder.Decode(config); err != nil {
		return fmt.Errorf("error unmarshaling YAML: %v", err)
	}

	return nil
}

func (ch *configHelper) newDefaultConfig(isHubConfig bool) *Config {
	leaderElectionResourceID := "kmm.sigs.x-k8s.io"
	gcDelay, _ := time.ParseDuration("0s")
	if isHubConfig {
		leaderElectionResourceID = "kmm-hub.sigs.x-k8s.io"
	}
	return &Config{
		HealthProbeBindAddress: ":8081",
		WebhookPort:            9443,
		LeaderElection: LeaderElection{
			Enabled:    true,
			ResourceID: leaderElectionResourceID,
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
		Job: Job{
			GCDelay: gcDelay,
		},
	}
}
