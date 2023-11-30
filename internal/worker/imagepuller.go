package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/docker/docker/pkg/idtools"
	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	"github.com/moby/moby/pkg/archive"
)

type PullResult struct {
	fsDir  string
	pulled bool
}

//go:generate mockgen -source=imagepuller.go -package=worker -destination=mock_imagepuller.go

type ImagePuller interface {
	PullAndExtract(ctx context.Context, imageName string, insecurePull bool) (PullResult, error)
}

type imagePuller struct {
	baseDir  string
	keyChain authn.Keychain
	logger   logr.Logger
}

func NewImagePuller(baseDir string, keyChain authn.Keychain, logger logr.Logger) ImagePuller {
	return &imagePuller{
		baseDir:  baseDir,
		keyChain: keyChain,
		logger:   logger,
	}
}

func (i *imagePuller) PullAndExtract(ctx context.Context, imageName string, insecurePull bool) (PullResult, error) {
	logger := i.logger.V(1).WithValues("image name", imageName)

	opts := []crane.Option{
		crane.WithContext(ctx),
		crane.WithAuthFromKeychain(i.keyChain),
	}

	if insecurePull {
		logger.Info(utils.WarnString("Pulling without TLS"))
		opts = append(opts, crane.Insecure)
	}

	logger.V(1).Info("Getting digest")

	remoteDigest, err := crane.Digest(imageName, opts...)
	if err != nil {
		return PullResult{}, fmt.Errorf("could not get the digest for %s: %v", imageName, err)
	}

	dstDir := filepath.Join(i.baseDir, imageName)
	digestPath := filepath.Join(dstDir, "digest")

	dstDirFS := filepath.Join(dstDir, "fs")
	res := PullResult{fsDir: dstDirFS}
	cleanup := false

	logger.Info("Reading digest file", "path", digestPath)

	b, err := os.ReadFile(digestPath)
	if err != nil {
		if os.IsNotExist(err) {
			cleanup = true
		} else {
			return PullResult{}, fmt.Errorf("could not open the digest file %s: %v", digestPath, err)
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
			return res, nil
		} else {
			logger.Info("Local file and remote digest differ; pulling image")
			cleanup = true
		}
	}

	if cleanup {
		logger.Info("Cleaning up image directory", "path", dstDir)

		if err = os.RemoveAll(dstDir); err != nil {
			return PullResult{}, fmt.Errorf("could not cleanup %s: %v", dstDir, err)
		}
	}

	if err = os.MkdirAll(dstDirFS, os.ModeDir|0755); err != nil {
		return res, fmt.Errorf("could not create the filesystem directory %s: %v", dstDirFS, err)
	}

	logger.V(1).Info("Pulling image")

	img, err := crane.Pull(imageName, opts...)
	if err != nil {
		return PullResult{}, fmt.Errorf("could not pull %s: %v", imageName, err)
	}

	res.pulled = true

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

		id := idtools.CurrentIdentity()

		tarOpts := &archive.TarOptions{ChownOpts: &id}

		if err := archive.Untar(rd, dstDirFS, tarOpts); err != nil {
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
		return res, fmt.Errorf("got one or more errors while writing the image: %v", err)
	}

	if err = ctx.Err(); err != nil {
		return res, fmt.Errorf("not writing digest file: %v", err)
	}

	logger.V(1).Info("Image written to the filesystem")

	digest, err := img.Digest()
	if err != nil {
		return PullResult{}, fmt.Errorf("could not get the digest of the pulled image: %v", err)
	}

	digestStr := digest.String()

	logger.V(1).Info("Writing digest", "digest", digestStr)

	if err = os.WriteFile(digestPath, []byte(digestStr), 0644); err != nil {
		return res, fmt.Errorf("could not write the digest file at %s: %v", digestPath, err)
	}

	return res, nil
}
