package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/docker/docker/pkg/idtools"
	"github.com/go-logr/logr"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/v1"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/moby/moby/pkg/archive"
)

//go:generate mockgen -source=imagemounter.go -package=worker -destination=mock_imagemounter.go ImageMounter

type ImageMounter interface {
	MountImage(ctx context.Context, imageName string, cfg *kmmv1beta1.ModuleConfig) (string, error)
}

//go:generate mockgen -source=imagemounter.go -package=worker -destination=mock_imagemounter.go ociImageMounterHelperAPI

type ociImageMounterHelperAPI interface {
	mountOCIImage(image v1.Image, dstDirFS string) error
}

type ociImageMounterHelper struct {
	logger logr.Logger
}

func newOCIImageMounterHelper(logger logr.Logger) ociImageMounterHelperAPI {
	return &ociImageMounterHelper{logger: logger}
}

func (oimh *ociImageMounterHelper) mountOCIImage(ociImage v1.Image, dstDirFS string) error {
	errs := make(chan error, 2)

	wg := sync.WaitGroup{}
	wg.Add(2)

	rd, wr := io.Pipe()

	go func() {
		defer wg.Done()
		defer wr.Close()

		oimh.logger.V(1).Info("Starting to export OCI image")

		if err := crane.Export(ociImage, wr); err != nil {
			errs <- err
			return
		}

		oimh.logger.V(1).Info("Done exporting OCI image")
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

		oimh.logger.V(1).Info("Done writing tar archive")
	}()

	wg.Wait()
	close(errs)
	chErrs := make([]error, 0)

	for chErr := range errs {
		chErrs = append(chErrs, chErr)
	}

	if err := errors.Join(chErrs...); err != nil {
		return fmt.Errorf("got one or more errors while writing the image: %v", err)
	}
	return nil
}
