package worker

import (
	"archive/tar"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

type remoteImageMounter struct {
	baseDir  string
	keyChain authn.Keychain
	logger   logr.Logger
}

func NewRemoteImageMounter(baseDir string, keyChain authn.Keychain, logger logr.Logger) ImageMounter {
	return &remoteImageMounter{
		baseDir:  baseDir,
		keyChain: keyChain,
		logger:   logger,
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

	errs := make(chan error, 2)

	wg := sync.WaitGroup{}
	wg.Add(2)

	rd, wr := io.Pipe()

	go func() {
		defer wg.Done()
		defer wr.Close()

		logger.V(1).Info("Starting to export image")

		if err := crane.Export(img, wr); err != nil {
			errs <- err
			return
		}

		logger.V(1).Info("Done exporting image")
	}()

	go func() {
		defer wg.Done()
		defer rd.Close()

		if err := extractTarToDisk(rd, dstDirFS); err != nil {
			errs <- err
			return
		}

		logger.V(1).Info("Done writing tar archive")
	}()

	wg.Wait()
	close(errs)

	chErrs := make([]error, 0)

	for chErr := range errs {
		chErrs = append(chErrs, chErr)
	}

	if err = errors.Join(chErrs...); err != nil {
		return "", fmt.Errorf("got one or more errors while writing the image: %v", err)
	}

	logger.V(1).Info("Image written to the filesystem")

	if err = ctx.Err(); err != nil {
		return "", fmt.Errorf("not writing digest file: %v", err)
	}

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

func extractTarToDisk(r io.Reader, dst string) error {
	tr := tar.NewReader(r)

	for {
		header, err := tr.Next()

		switch {
		case err == io.EOF:
			return nil
		case err != nil:
			return err
		case header == nil:
			continue
		}

		target := filepath.Join(dst, header.Name)

		// the following switch could also be done using fi.Mode(), not sure if there
		// a benefit of using one vs. the other.
		// fi := header.FileInfo()

		// check the file type
		switch header.Typeflag {
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}
		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR, os.FileMode(header.Mode))
			if err != nil {
				return err
			}

			if _, err := io.Copy(f, tr); err != nil {
				return err
			}

			if err = f.Close(); err != nil {
				return fmt.Errorf("could not close %s: %v", target, err)
			}
		}
	}
}
