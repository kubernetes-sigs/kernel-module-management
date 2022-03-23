//go:build toast

package test

import (
	"context"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ client.Client = &ReactingClient{}

type ReactingClient struct {
	c client.Client
}

func (r *ReactingClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	//TODO implement me
	panic("implement me")
}

func (r *ReactingClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	//TODO implement me
	panic("implement me")
}

func (r *ReactingClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	//TODO implement me
	panic("implement me")
}

func (r *ReactingClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	//TODO implement me
	panic("implement me")
}

func (r *ReactingClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	//TODO implement me
	panic("implement me")
}

func (r *ReactingClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	//TODO implement me
	panic("implement me")
}

func (r *ReactingClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	//TODO implement me
	panic("implement me")
}

func (r *ReactingClient) Status() client.StatusWriter {
	//TODO implement me
	panic("implement me")
}

func (r *ReactingClient) Scheme() *runtime.Scheme {
	return r.c.Scheme()
	panic("implement me")
}

func (r *ReactingClient) RESTMapper() meta.RESTMapper {
	//TODO implement me
	panic("implement me")
}
