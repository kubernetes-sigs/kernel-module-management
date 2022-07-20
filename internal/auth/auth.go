package auth

import (
	"context"
	"fmt"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/authn/kubernetes"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

//go:generate mockgen -source=auth.go -package=auth -destination=mock_auth.go

type RegistryAuthGetter interface {
	GetKeyChain(ctx context.Context) (authn.Keychain, error)
}

type registrySecretAuthGetter struct {
	client         client.Client
	namespacedName types.NamespacedName
}

func NewRegistryAuthGetter(client client.Client, namespacedName types.NamespacedName) RegistryAuthGetter {
	return &registrySecretAuthGetter{
		client:         client,
		namespacedName: namespacedName,
	}
}

func (rsag *registrySecretAuthGetter) GetKeyChain(ctx context.Context) (authn.Keychain, error) {

	secret := v1.Secret{}
	if err := rsag.client.Get(ctx, rsag.namespacedName, &secret); err != nil {
		return nil, fmt.Errorf("cannot find secret %s: %w", rsag.namespacedName, err)
	}

	keychain, err := kubernetes.NewFromPullSecrets(ctx, []v1.Secret{secret})
	if err != nil {
		return nil, fmt.Errorf("could not create a keycahin from secret %v: %w", secret, err)
	}

	return keychain, nil
}
