package registry

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
)

//go:generate mockgen -source=getter.go -package=registry -destination=mock_getter.go

type Getter interface {
	ImageExists(ctx context.Context, containerImage string, po ootov1alpha1.PullOptions) (bool, error)
}

type getter struct{}

func NewGetter() Getter {
	return &getter{}
}

func (getter) ImageExists(ctx context.Context, containerImage string, po ootov1alpha1.PullOptions) (bool, error) {
	opts := make([]name.Option, 0)

	if po.Insecure {
		opts = append(opts, name.Insecure)
	}

	ref, err := name.ParseReference(containerImage, opts...)
	if err != nil {
		return false, fmt.Errorf("could not parse the container image name: %v", err)
	}

	if _, err = remote.Get(ref, remote.WithContext(ctx)); err != nil {
		te := &transport.Error{}

		if errors.As(err, &te) && te.StatusCode == http.StatusNotFound {
			return false, nil
		}

		return false, fmt.Errorf("could not get image: %v", err)
	}

	return true, nil
}
