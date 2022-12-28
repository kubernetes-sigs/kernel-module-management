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

package hub

import (
	"context"
	"errors"

	"github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/golang/mock/gomock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/cluster"
	"github.com/kubernetes-sigs/kernel-module-management/internal/manifestwork"
)

var _ = Describe("ManagedClusterModuleReconciler_Reconcile", func() {
	var (
		ctrl           *gomock.Controller
		clnt           *client.MockClient
		mockMW         *manifestwork.MockManifestWorkCreator
		mockClusterAPI *cluster.MockClusterAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockMW = manifestwork.NewMockManifestWorkCreator(ctrl)
		mockClusterAPI = cluster.NewMockClusterAPI(ctrl)
	})

	const (
		mcmName = "test-module"
	)

	nsn := types.NamespacedName{
		Name: mcmName,
	}

	req := reconcile.Request{NamespacedName: nsn}

	ctx := context.Background()

	It("should do nothing if the ManagedClusterModule is not available anymore", func() {
		mockClusterAPI.EXPECT().RequestedManagedClusterModule(ctx, req.NamespacedName).
			Return(
				nil,
				apierrors.NewNotFound(schema.GroupResource{}, mcmName),
			)

		mcmr := NewManagedClusterModuleReconciler(clnt, nil, mockClusterAPI, nil)
		Expect(
			mcmr.Reconcile(ctx, req),
		).To(
			Equal(reconcile.Result{}),
		)
	})

	It("should return an error when fetching the requested ManagedClusterModule fails", func() {
		mockClusterAPI.EXPECT().RequestedManagedClusterModule(ctx, req.NamespacedName).
			Return(nil, errors.New("test"))

		mr := NewManagedClusterModuleReconciler(clnt, nil, mockClusterAPI, nil)

		res, err := mr.Reconcile(ctx, req)
		Expect(err).To(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should return an error when fetching selected managed clusters fails", func() {
		mcm := &v1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: v1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{},
				Selector:   map[string]string{"key": "value"},
			},
		}

		gomock.InOrder(
			mockClusterAPI.EXPECT().RequestedManagedClusterModule(ctx, req.NamespacedName).Return(mcm, nil),
			mockClusterAPI.EXPECT().SelectedManagedClusters(ctx, gomock.Any()).
				Return(
					&clusterv1.ManagedClusterList{},
					apierrors.NewServiceUnavailable("Service unavailable"),
				),
		)

		mr := NewManagedClusterModuleReconciler(clnt, nil, mockClusterAPI, nil)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).To(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should do nothing but garbage collect any obsolete ManifestWorks and Builds when no managed clusters match the selector", func() {
		mcm := &v1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: v1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{},
				Selector:   map[string]string{"key": "value"},
			},
		}

		clusterList := clusterv1.ManagedClusterList{}

		gomock.InOrder(
			mockClusterAPI.EXPECT().RequestedManagedClusterModule(ctx, req.NamespacedName).Return(mcm, nil),
			mockClusterAPI.EXPECT().SelectedManagedClusters(ctx, gomock.Any()).Return(&clusterList, nil),
			mockMW.EXPECT().GarbageCollect(ctx, clusterList, *mcm),
			mockClusterAPI.EXPECT().GarbageCollectBuilds(ctx, *mcm),
		)

		mr := NewManagedClusterModuleReconciler(clnt, mockMW, mockClusterAPI, nil)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should return an error when ManifestWork garbage collection fails", func() {
		mcm := &v1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: v1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{},
				Selector:   map[string]string{"key": "value"},
			},
		}

		clusterList := clusterv1.ManagedClusterList{}

		gomock.InOrder(
			mockClusterAPI.EXPECT().RequestedManagedClusterModule(ctx, req.NamespacedName).Return(mcm, nil),
			mockClusterAPI.EXPECT().SelectedManagedClusters(ctx, gomock.Any()).Return(&clusterList, nil),
			mockMW.EXPECT().GarbageCollect(ctx, clusterList, *mcm).Return(errors.New("test")),
		)

		mr := NewManagedClusterModuleReconciler(clnt, mockMW, mockClusterAPI, nil)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).To(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should return an error when Builds garbage collection fails", func() {
		mcm := &v1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: v1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{},
				Selector:   map[string]string{"key": "value"},
			},
		}

		clusterList := clusterv1.ManagedClusterList{}

		gomock.InOrder(
			mockClusterAPI.EXPECT().RequestedManagedClusterModule(ctx, req.NamespacedName).Return(mcm, nil),
			mockClusterAPI.EXPECT().SelectedManagedClusters(ctx, gomock.Any()).Return(&clusterList, nil),
			mockMW.EXPECT().GarbageCollect(ctx, clusterList, *mcm),
			mockClusterAPI.EXPECT().GarbageCollectBuilds(ctx, *mcm).Return(nil, errors.New("test")),
		)

		mr := NewManagedClusterModuleReconciler(clnt, mockMW, mockClusterAPI, nil)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).To(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should requeue the request when build is specified and a managed cluster matches the selector", func() {
		mcm := v1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: v1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{
					ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
						Container: kmmv1beta1.ModuleLoaderContainerSpec{
							Build: &kmmv1beta1.Build{},
						},
					},
				},
				Selector: map[string]string{"key": "value"},
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

		gomock.InOrder(
			mockClusterAPI.EXPECT().RequestedManagedClusterModule(ctx, req.NamespacedName).Return(&mcm, nil),
			mockClusterAPI.EXPECT().SelectedManagedClusters(ctx, gomock.Any()).Return(&clusterList, nil),
			mockClusterAPI.EXPECT().BuildAndSign(gomock.Any(), mcm, clusterList.Items[0]).Return(true, nil),
			mockMW.EXPECT().GarbageCollect(ctx, clusterList, mcm),
			mockClusterAPI.EXPECT().GarbageCollectBuilds(ctx, mcm),
		)

		mr := NewManagedClusterModuleReconciler(clnt, mockMW, mockClusterAPI, nil)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).ToNot(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{Requeue: true}))
	})

	It("should create a ManifestWork if a managed cluster matches the selector", func() {
		mcm := v1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: v1beta1.ManagedClusterModuleSpec{
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
			mockClusterAPI.EXPECT().RequestedManagedClusterModule(ctx, req.NamespacedName).Return(&mcm, nil),
			mockClusterAPI.EXPECT().SelectedManagedClusters(ctx, gomock.Any()).Return(&clusterList, nil),
			mockClusterAPI.EXPECT().BuildAndSign(gomock.Any(), mcm, clusterList.Items[0]).Return(false, nil),
			clnt.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(apierrors.NewNotFound(schema.GroupResource{}, "whatever")),
			mockMW.EXPECT().SetManifestWorkAsDesired(context.Background(), &mw, gomock.AssignableToTypeOf(mcm)),
			clnt.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil),
			mockMW.EXPECT().GarbageCollect(ctx, clusterList, mcm),
			mockClusterAPI.EXPECT().GarbageCollectBuilds(ctx, mcm),
		)

		mr := NewManagedClusterModuleReconciler(clnt, mockMW, mockClusterAPI, nil)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})

	It("should patch the ManifestWork if it already exists", func() {
		mcm := v1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: v1beta1.ManagedClusterModuleSpec{
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
			mockClusterAPI.EXPECT().RequestedManagedClusterModule(ctx, req.NamespacedName).Return(&mcm, nil),
			mockClusterAPI.EXPECT().SelectedManagedClusters(ctx, gomock.Any()).Return(&clusterList, nil),
			mockClusterAPI.EXPECT().BuildAndSign(gomock.Any(), mcm, clusterList.Items[0]).Return(false, nil),
			clnt.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()),
			mockMW.EXPECT().SetManifestWorkAsDesired(context.Background(), &mw, gomock.AssignableToTypeOf(mcm)).Do(
				func(ctx context.Context, m *workv1.ManifestWork, _ v1beta1.ManagedClusterModule) {
					m.SetLabels(map[string]string{"test": "test"})
				}),
			clnt.EXPECT().Patch(gomock.Any(), gomock.Any(), gomock.Any()),
			mockMW.EXPECT().GarbageCollect(ctx, clusterList, mcm),
			mockClusterAPI.EXPECT().GarbageCollectBuilds(ctx, mcm),
		)

		mr := NewManagedClusterModuleReconciler(clnt, mockMW, mockClusterAPI, nil)

		res, err := mr.Reconcile(context.Background(), req)
		Expect(err).NotTo(HaveOccurred())
		Expect(res).To(Equal(reconcile.Result{}))
	})
})
