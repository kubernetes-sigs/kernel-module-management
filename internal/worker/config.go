package worker

import (
	"fmt"
	"os"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"k8s.io/apimachinery/pkg/util/yaml"
)

type ConfigHelper interface {
	ReadConfigFile(path string) (*kmmv1beta1.ModuleConfig, error)
}

type configHelper struct{}

func NewConfigHelper() ConfigHelper {
	return &configHelper{}
}

func (c *configHelper) ReadConfigFile(path string) (*kmmv1beta1.ModuleConfig, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read the configuration file %s: %v", path, err)
	}

	cfg := kmmv1beta1.ModuleConfig{}

	if err = yaml.UnmarshalStrict(b, &cfg); err != nil {
		return nil, fmt.Errorf("could not decode the configuration from %s: %v", path, err)
	}

	return &cfg, nil
}
