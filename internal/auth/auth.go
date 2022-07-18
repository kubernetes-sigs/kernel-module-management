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

type RegistryAuth interface {
	GetKeyChainFromSecret(ctx context.Context, secretName, secretNamespace string) (authn.Keychain, error)
}

type registryAuth struct {
	client client.Client
}

func NewRegistryAuth(client client.Client) RegistryAuth {
	return &registryAuth{
		client: client,
	}
}

func (a *registryAuth) GetKeyChainFromSecret(ctx context.Context, secretName, secretNamespace string) (authn.Keychain, error) {

	secret := v1.Secret{}
	secretNamespacedName := types.NamespacedName{
		Name:      secretName,
		Namespace: secretNamespace,
	}
	if err := a.client.Get(ctx, secretNamespacedName, &secret); err != nil {
		return nil, fmt.Errorf("cannot find secret %s: %w", secretNamespacedName.String(), err)
	}

	keychain, err := kubernetes.NewFromPullSecrets(ctx, []v1.Secret{secret})
	if err != nil {
		return nil, fmt.Errorf("could not create a keycahin from secret %v: %w", secret, err)
	}

	return keychain, nil
}
