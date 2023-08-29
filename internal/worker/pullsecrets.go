package worker

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/authn/kubernetes"
	v1 "k8s.io/api/core/v1"
)

func ReadKubernetesSecrets(ctx context.Context, rootDir string, logger logr.Logger) (authn.Keychain, error) {
	var secrets []v1.Secret

	err := filepath.WalkDir(rootDir, func(path string, de fs.DirEntry, err error) error {
		if err != nil || de.IsDir() {
			return err
		}

		var (
			sKey  string
			sType v1.SecretType
		)

		switch sKey = filepath.Base(path); sKey {
		case v1.DockerConfigKey:
			sType = v1.SecretTypeDockercfg
		case v1.DockerConfigJsonKey:
			sType = v1.SecretTypeDockerConfigJson
		default:
			logger.Info("Unhandled file name; ignoring", "path", path)
			return nil
		}

		logger.Info("Reading file", "path", path, "type", sType)

		b, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("could not read %s: %v", path, err)
		}

		s := v1.Secret{
			Type: sType,
			Data: map[string][]byte{sKey: b},
		}

		secrets = append(secrets, s)

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error while walking %q: %v", rootDir, err)
	}

	return kubernetes.NewFromPullSecrets(ctx, secrets)
}
