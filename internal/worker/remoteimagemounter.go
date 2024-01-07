package worker

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

type remoteImageMounter struct {
	ociImageHelper ociImageMounterHelperAPI
	baseDir        string
	keyChain       authn.Keychain
	logger         logr.Logger
}

func NewRemoteImageMounter(baseDir string, keyChain authn.Keychain, logger logr.Logger) ImageMounter {
	ociImageHelper := newOCIImageMounterHelper(logger)
	return &remoteImageMounter{
		ociImageHelper: ociImageHelper,
		baseDir:        baseDir,
		keyChain:       keyChain,
		logger:         logger,
	}
}

func (rim *remoteImageMounter) MountImage(ctx context.Context, imageName string, cfg *kmmv1beta1.ModuleConfig) (string, error) {
	logger := rim.logger.V(1).WithValues("image name", imageName)

	opts := []crane.Option{
		crane.WithContext(ctx),
		crane.WithAuthFromKeychain(rim.keyChain),
	}

	if cfg.InsecurePull {
		logger.Info(utils.WarnString("Pulling without TLS"))
		opts = append(opts, crane.Insecure)
	}

	logger.V(1).Info("Getting digest")

	remoteDigest, err := crane.Digest(imageName, opts...)
	if err != nil {
		return "", fmt.Errorf("could not get the digest for %s: %v", imageName, err)
	}

	dstDir := filepath.Join(rim.baseDir, imageName)
	digestPath := filepath.Join(dstDir, "digest")

	dstDirFS := filepath.Join(dstDir, "fs")
	cleanup := false

	logger.Info("Reading digest file", "path", digestPath)

	b, err := os.ReadFile(digestPath)
	if err != nil {
		if os.IsNotExist(err) {
			cleanup = true
		} else {
			return "", fmt.Errorf("could not open the digest file %s: %v", digestPath, err)
		}
	} else {
		logger.V(1).Info(
			"Comparing digests",
			"local file",
			string(b),
			"remote image",
			remoteDigest,
		)

		if string(b) == remoteDigest {
			logger.Info("Local file and remote digest are identical; skipping pull")
			return dstDirFS, nil
		} else {
			logger.Info("Local file and remote digest differ; pulling image")
			cleanup = true
		}
	}

	if cleanup {
		logger.Info("Cleaning up image directory", "path", dstDir)

		if err = os.RemoveAll(dstDir); err != nil {
			return "", fmt.Errorf("could not cleanup %s: %v", dstDir, err)
		}
	}

	if err = os.MkdirAll(dstDirFS, os.ModeDir|0755); err != nil {
		return "", fmt.Errorf("could not create the filesystem directory %s: %v", dstDirFS, err)
	}

	logger.V(1).Info("Pulling image")

	img, err := crane.Pull(imageName, opts...)
	if err != nil {
		return "", fmt.Errorf("could not pull %s: %v", imageName, err)
	}

	err = rim.ociImageHelper.mountOCIImage(img, dstDirFS)
	if err != nil {
		return "", fmt.Errorf("failed mounting oci image: %v", err)
	}

	if err = ctx.Err(); err != nil {
		return "", fmt.Errorf("not writing digest file: %v", err)
	}

	logger.V(1).Info("Image written to the filesystem")

	digest, err := img.Digest()
	if err != nil {
		return "", fmt.Errorf("could not get the digest of the pulled image: %v", err)
	}

	digestStr := digest.String()

	logger.V(1).Info("Writing digest", "digest", digestStr)

	if err = os.WriteFile(digestPath, []byte(digestStr), 0644); err != nil {
		return "", fmt.Errorf("could not write the digest file at %s: %v", digestPath, err)
	}

	return dstDirFS, nil
}
