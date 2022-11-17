/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/golang/mock/gomock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/manifestwork"
)

var _ = Describe("ManagedClusterModuleReconciler_Reconcile", func() {
	var (
		ctrl   *gomock.Controller
		clnt   *client.MockClient
		mockMW *manifestwork.MockManifestWorkCreator
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockMW = manifestwork.NewMockManifestWorkCreator(ctrl)
	})

	const (
		mcmName   = "test-module"
		namespace = "namespace"
	)

	nsn := types.NamespacedName{
		Name:      mcmName,
		Namespace: namespace,
	}

	req := reconcile.Request{NamespacedName: nsn}

	ctx := context.Background()

	It("should do nothing if the ManagedClusterModule is not available anymore", func() {
		clnt.
			EXPECT().
			Get(ctx, nsn, &kmmv1beta1.ManagedClusterModule{}).
			Return(
				apierrors.NewNotFound(schema.GroupResource{}, mcmName),
			)

		mcmr := NewManagedClusterModuleReconciler(clnt, nil, nil)
		Expect(
			mcmr.Reconcile(ctx, req),
		).To(
			Equal(reconcile.Result{}),
		)
	})

	It("should return an error when fetching selected managed clusters fails", func() {
		mcm := kmmv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mcmName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{},
				Selector:   map[string]string{"key": "value"},
			},
		}

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, req.NamespacedName, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, m *kmmv1beta1.ManagedClusterModule, _ ...ctrlclient.GetOption) error {
					m.ObjectMeta = mcm.ObjectMeta
					m.Spec = mcm.Spec
					return nil
				},
			),
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).
				Return(apierrors.NewServiceUnavailable("Service unavailable")),
		)

		mr := NewManagedClusterModuleReconciler(clnt, nil, nil)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).To(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
		Expect(res.Requeue).To(BeFalse())
	})

	It("should do nothing but garbage collect any obsolete ManifestWorks when no managed clusters match the selector", func() {
		mcm := kmmv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mcmName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{},
				Selector:   map[string]string{"key": "value"},
			},
		}

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, req.NamespacedName, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, m *kmmv1beta1.ManagedClusterModule, _ ...ctrlclient.GetOption) error {
					m.ObjectMeta = mcm.ObjectMeta
					m.Spec = mcm.Spec
					return nil
				},
			),
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *clusterv1.ManagedClusterList, _ ...interface{}) error {
					list.Items = []clusterv1.ManagedCluster{}
					return nil
				},
			),
			mockMW.EXPECT().GarbageCollect(ctx, gomock.Any(), mcm),
		)

		mr := NewManagedClusterModuleReconciler(clnt, mockMW, nil)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should return an error when garbage collect fails", func() {
		mcm := kmmv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mcmName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{},
				Selector:   map[string]string{"key": "value"},
			},
		}

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, req.NamespacedName, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, m *kmmv1beta1.ManagedClusterModule, _ ...ctrlclient.GetOption) error {
					m.ObjectMeta = mcm.ObjectMeta
					m.Spec = mcm.Spec
					return nil
				},
			),
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *clusterv1.ManagedClusterList, _ ...interface{}) error {
					list.Items = []clusterv1.ManagedCluster{}
					return nil
				},
			),
			mockMW.EXPECT().GarbageCollect(ctx, gomock.Any(), mcm).Return(errors.New("test")),
		)

		mr := NewManagedClusterModuleReconciler(clnt, mockMW, nil)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).To(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should create a ManifestWork if a managed cluster matches the selector", func() {
		mcm := kmmv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mcmName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{},
				Selector:   map[string]string{"key": "value"},
			},
		}

		clusterList := clusterv1.ManagedClusterList{
			Items: []clusterv1.ManagedCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "default",
						Labels: map[string]string{"key": "value"},
					},
				},
			},
		}

		mw := workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mcm.Name,
				Namespace: clusterList.Items[0].Name,
			},
		}

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, req.NamespacedName, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, m *kmmv1beta1.ManagedClusterModule, _ ...ctrlclient.GetOption) error {
					m.ObjectMeta = mcm.ObjectMeta
					m.Spec = mcm.Spec
					return nil
				},
			),
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *clusterv1.ManagedClusterList, _ ...interface{}) error {
					list.Items = clusterList.Items
					return nil
				},
			),
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(apierrors.NewNotFound(schema.GroupResource{}, "whatever")),
			mockMW.EXPECT().SetManifestWorkAsDesired(context.Background(), &mw, gomock.AssignableToTypeOf(mcm)),
			clnt.EXPECT().Create(ctx, gomock.Any()).Return(nil),
			mockMW.EXPECT().GarbageCollect(ctx, gomock.Any(), mcm),
		)

		mr := NewManagedClusterModuleReconciler(clnt, mockMW, nil)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should patch the ManifestWork if it already exists", func() {
		mcm := kmmv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mcmName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{},
				Selector:   map[string]string{"key": "value"},
			},
		}

		clusterList := clusterv1.ManagedClusterList{
			Items: []clusterv1.ManagedCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "default",
						Labels: map[string]string{"key": "value"},
					},
				},
			},
		}

		mw := workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      mcm.Name,
				Namespace: clusterList.Items[0].Name,
			},
		}

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, req.NamespacedName, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, m *kmmv1beta1.ManagedClusterModule, _ ...ctrlclient.GetOption) error {
					m.ObjectMeta = mcm.ObjectMeta
					m.Spec = mcm.Spec
					return nil
				},
			),
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *clusterv1.ManagedClusterList, _ ...interface{}) error {
					list.Items = clusterList.Items
					return nil
				},
			),
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()),
			mockMW.EXPECT().SetManifestWorkAsDesired(context.Background(), &mw, gomock.AssignableToTypeOf(mcm)).Do(
				func(ctx context.Context, m *workv1.ManifestWork, _ kmmv1beta1.ManagedClusterModule) {
					m.SetLabels(map[string]string{"test": "test"})
				}),
			clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()),
			mockMW.EXPECT().GarbageCollect(ctx, gomock.Any(), mcm),
		)

		mr := NewManagedClusterModuleReconciler(clnt, mockMW, nil)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})
})
