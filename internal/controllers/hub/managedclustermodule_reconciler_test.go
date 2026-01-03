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

	hubv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	v1beta1 "github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/cluster"
	"github.com/kubernetes-sigs/kernel-module-management/internal/manifestwork"
	"github.com/kubernetes-sigs/kernel-module-management/internal/mic"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/statusupdater"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
)

var _ = Describe("ManagedClusterModuleReconciler_Reconcile", func() {

	const (
		mcmName = "mcm-name"
	)

	var (
		ctx                   context.Context
		ctrl                  *gomock.Controller
		mockClient            *client.MockClient
		mockManifestAPI       *manifestwork.MockManifestWorkCreator
		mockClusterAPI        *cluster.MockClusterAPI
		mockStatusupdaterAPI  *statusupdater.MockManagedClusterModuleStatusUpdater
		mockMCMReconHelperAPI *MockmanagedClusterModuleReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockClient = client.NewMockClient(ctrl)
		mockManifestAPI = manifestwork.NewMockManifestWorkCreator(ctrl)
		mockClusterAPI = cluster.NewMockClusterAPI(ctrl)
		mockStatusupdaterAPI = statusupdater.NewMockManagedClusterModuleStatusUpdater(ctrl)
		mockMCMReconHelperAPI = NewMockmanagedClusterModuleReconcilerHelperAPI(ctrl)
	})

	It("should fail if we fail to get selected managed-clusters", func() {

		mcm := &v1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: v1beta1.ManagedClusterModuleSpec{},
		}

		mockClusterAPI.EXPECT().SelectedManagedClusters(ctx, mcm).
			Return(&clusterv1.ManagedClusterList{}, errors.New("some error"))

		mcmr := NewManagedClusterModuleReconciler(nil, nil, mockClusterAPI, nil, nil, nil)

		_, err := mcmr.Reconcile(context.Background(), mcm)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get selected clusters"))
	})

	It("should fail if we fail to garbage collect", func() {

		mcm := &v1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: v1beta1.ManagedClusterModuleSpec{},
		}

		expectedClusters := &clusterv1.ManagedClusterList{
			Items: []clusterv1.ManagedCluster{},
		}

		gomock.InOrder(
			mockClusterAPI.EXPECT().SelectedManagedClusters(ctx, mcm).Return(expectedClusters, nil),
			mockManifestAPI.EXPECT().GarbageCollect(ctx, *expectedClusters, *mcm).Return(errors.New("some error")),
		)

		mcmr := NewManagedClusterModuleReconciler(nil, mockManifestAPI, mockClusterAPI, nil, nil, nil)

		_, err := mcmr.Reconcile(context.Background(), mcm)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to garbage collect ManifestWorks with no matching cluster selector"))
	})

	It("should fail if we fail to get owned manifestwork", func() {

		mcm := &v1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: v1beta1.ManagedClusterModuleSpec{},
		}

		expectedClusters := &clusterv1.ManagedClusterList{
			Items: []clusterv1.ManagedCluster{},
		}

		gomock.InOrder(
			mockClusterAPI.EXPECT().SelectedManagedClusters(ctx, mcm).Return(expectedClusters, nil),
			mockManifestAPI.EXPECT().GarbageCollect(ctx, *expectedClusters, *mcm).Return(nil),
			mockManifestAPI.EXPECT().GetOwnedManifestWorks(ctx, *mcm).Return(nil, errors.New("some error")),
		)

		mcmr := NewManagedClusterModuleReconciler(nil, mockManifestAPI, mockClusterAPI, nil, nil, nil)

		_, err := mcmr.Reconcile(context.Background(), mcm)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to fetch owned ManifestWorks of the ManagedClusterModule"))
	})

	It("should fail if we fail to update the MCM status", func() {

		mcm := &v1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: v1beta1.ManagedClusterModuleSpec{},
		}

		expectedClusters := &clusterv1.ManagedClusterList{
			Items: []clusterv1.ManagedCluster{},
		}

		expectedOwnManifestWork := &workv1.ManifestWorkList{
			Items: []workv1.ManifestWork{},
		}

		gomock.InOrder(
			mockClusterAPI.EXPECT().SelectedManagedClusters(ctx, mcm).Return(expectedClusters, nil),
			mockManifestAPI.EXPECT().GarbageCollect(ctx, *expectedClusters, *mcm).Return(nil),
			mockManifestAPI.EXPECT().GetOwnedManifestWorks(ctx, *mcm).Return(expectedOwnManifestWork, nil),
			mockStatusupdaterAPI.EXPECT().ManagedClusterModuleUpdateStatus(ctx, mcm, expectedOwnManifestWork.Items).
				Return(errors.New("some error")),
		)

		mcmr := NewManagedClusterModuleReconciler(nil, mockManifestAPI, mockClusterAPI, mockStatusupdaterAPI, nil, nil)

		_, err := mcmr.Reconcile(context.Background(), mcm)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to update status of the ManagedClusterModule"))
	})

	It("should skip untargeted kernel versions", func() {

		mcm := &v1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: v1beta1.ManagedClusterModuleSpec{},
		}

		expectedClusters := &clusterv1.ManagedClusterList{
			Items: []clusterv1.ManagedCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-1",
					},
				},
			},
		}

		expectedOwnManifestWork := &workv1.ManifestWorkList{
			Items: []workv1.ManifestWork{},
		}

		gomock.InOrder(
			mockClusterAPI.EXPECT().SelectedManagedClusters(ctx, mcm).Return(expectedClusters, nil),
			mockClusterAPI.EXPECT().KernelVersions(expectedClusters.Items[0]).Return([]string{}, errors.New("some error")),
			// we expecte all the loop to be skipped with no errors
			mockManifestAPI.EXPECT().GarbageCollect(ctx, *expectedClusters, *mcm).Return(nil),
			mockManifestAPI.EXPECT().GetOwnedManifestWorks(ctx, *mcm).Return(expectedOwnManifestWork, nil),
			mockStatusupdaterAPI.EXPECT().ManagedClusterModuleUpdateStatus(ctx, mcm, expectedOwnManifestWork.Items).Return(nil),
		)

		mcmr := NewManagedClusterModuleReconciler(nil, mockManifestAPI, mockClusterAPI, mockStatusupdaterAPI, nil, nil)

		_, err := mcmr.Reconcile(context.Background(), mcm)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should skip clusters in which we failed to set MIC as desired", func() {

		mcm := &v1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: v1beta1.ManagedClusterModuleSpec{},
		}

		expectedClusters := &clusterv1.ManagedClusterList{
			Items: []clusterv1.ManagedCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-1",
					},
				},
			},
		}

		expectedKernelVersion := []string{"v1.2.3"}

		expectedOwnManifestWork := &workv1.ManifestWorkList{
			Items: []workv1.ManifestWork{},
		}

		gomock.InOrder(
			mockClusterAPI.EXPECT().SelectedManagedClusters(ctx, mcm).Return(expectedClusters, nil),
			mockClusterAPI.EXPECT().KernelVersions(expectedClusters.Items[0]).Return(expectedKernelVersion, nil),
			mockMCMReconHelperAPI.EXPECT().setMicAsDesired(ctx, mcm, "cluster-1", expectedKernelVersion).Return(errors.New("error")),
			// we expecte the rest of the loop to be skipped with no errors
			mockManifestAPI.EXPECT().GarbageCollect(ctx, *expectedClusters, *mcm).Return(nil),
			mockManifestAPI.EXPECT().GetOwnedManifestWorks(ctx, *mcm).Return(expectedOwnManifestWork, nil),
			mockStatusupdaterAPI.EXPECT().ManagedClusterModuleUpdateStatus(ctx, mcm, expectedOwnManifestWork.Items).Return(nil),
		)

		mcmr := &ManagedClusterModuleReconciler{
			manifestAPI:      mockManifestAPI,
			clusterAPI:       mockClusterAPI,
			statusupdaterAPI: mockStatusupdaterAPI,
			reconHelper:      mockMCMReconHelperAPI,
		}

		_, err := mcmr.Reconcile(context.Background(), mcm)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should skip clusters in which we failed to check if MIC is ready", func() {

		mcm := &v1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: v1beta1.ManagedClusterModuleSpec{},
		}

		expectedClusters := &clusterv1.ManagedClusterList{
			Items: []clusterv1.ManagedCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-1",
					},
				},
			},
		}

		expectedKernelVersion := []string{"v1.2.3"}

		expectedOwnManifestWork := &workv1.ManifestWorkList{
			Items: []workv1.ManifestWork{},
		}

		gomock.InOrder(
			mockClusterAPI.EXPECT().SelectedManagedClusters(ctx, mcm).Return(expectedClusters, nil),
			mockClusterAPI.EXPECT().KernelVersions(expectedClusters.Items[0]).Return(expectedKernelVersion, nil),
			mockMCMReconHelperAPI.EXPECT().setMicAsDesired(ctx, mcm, "cluster-1", expectedKernelVersion).Return(nil),
			mockMCMReconHelperAPI.EXPECT().areImagesReady(ctx, mcm.Name, "cluster-1").Return(false, errors.New("some error")),
			// we expecte the rest of the loop to be skipped with no errors
			mockManifestAPI.EXPECT().GarbageCollect(ctx, *expectedClusters, *mcm).Return(nil),
			mockManifestAPI.EXPECT().GetOwnedManifestWorks(ctx, *mcm).Return(expectedOwnManifestWork, nil),
			mockStatusupdaterAPI.EXPECT().ManagedClusterModuleUpdateStatus(ctx, mcm, expectedOwnManifestWork.Items).Return(nil),
		)

		mcmr := &ManagedClusterModuleReconciler{
			manifestAPI:      mockManifestAPI,
			clusterAPI:       mockClusterAPI,
			statusupdaterAPI: mockStatusupdaterAPI,
			reconHelper:      mockMCMReconHelperAPI,
		}

		_, err := mcmr.Reconcile(context.Background(), mcm)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should not create manifestWork for MIC that doesn't have all images in the exist state", func() {

		mcm := &v1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: v1beta1.ManagedClusterModuleSpec{},
		}

		expectedClusters := &clusterv1.ManagedClusterList{
			Items: []clusterv1.ManagedCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-1",
					},
				},
			},
		}

		expectedKernelVersion := []string{"v1.2.3"}

		expectedOwnManifestWork := &workv1.ManifestWorkList{
			Items: []workv1.ManifestWork{},
		}

		gomock.InOrder(
			mockClusterAPI.EXPECT().SelectedManagedClusters(ctx, mcm).Return(expectedClusters, nil),
			mockClusterAPI.EXPECT().KernelVersions(expectedClusters.Items[0]).Return(expectedKernelVersion, nil),
			mockMCMReconHelperAPI.EXPECT().setMicAsDesired(ctx, mcm, "cluster-1", expectedKernelVersion).Return(nil),
			mockMCMReconHelperAPI.EXPECT().areImagesReady(ctx, mcm.Name, "cluster-1").Return(false, nil),
			// we expecte the rest of the loop to be skipped with no errors
			mockManifestAPI.EXPECT().GarbageCollect(ctx, *expectedClusters, *mcm).Return(nil),
			mockManifestAPI.EXPECT().GetOwnedManifestWorks(ctx, *mcm).Return(expectedOwnManifestWork, nil),
			mockStatusupdaterAPI.EXPECT().ManagedClusterModuleUpdateStatus(ctx, mcm, expectedOwnManifestWork.Items).Return(nil),
		)

		mcmr := &ManagedClusterModuleReconciler{
			manifestAPI:      mockManifestAPI,
			clusterAPI:       mockClusterAPI,
			statusupdaterAPI: mockStatusupdaterAPI,
			reconHelper:      mockMCMReconHelperAPI,
		}

		_, err := mcmr.Reconcile(context.Background(), mcm)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should work as expected", func() {

		mcm := &v1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: v1beta1.ManagedClusterModuleSpec{},
		}

		expectedClusters := &clusterv1.ManagedClusterList{
			Items: []clusterv1.ManagedCluster{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-1",
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "cluster-2",
					},
				},
			},
		}

		expectedKernelVersion := []string{"v1.2.3"}

		expectedOwnManifestWork := &workv1.ManifestWorkList{
			Items: []workv1.ManifestWork{},
		}

		mockClusterAPI.EXPECT().SelectedManagedClusters(ctx, mcm).Return(expectedClusters, nil)
		mockClusterAPI.EXPECT().KernelVersions(expectedClusters.Items[0]).Return(expectedKernelVersion, nil)
		mockClusterAPI.EXPECT().KernelVersions(expectedClusters.Items[1]).Return(expectedKernelVersion, nil)
		mockMCMReconHelperAPI.EXPECT().setMicAsDesired(ctx, mcm, "cluster-1", expectedKernelVersion).Return(nil)
		mockMCMReconHelperAPI.EXPECT().areImagesReady(ctx, mcm.Name, "cluster-1").Return(true, nil)
		mockMCMReconHelperAPI.EXPECT().setMicAsDesired(ctx, mcm, "cluster-2", expectedKernelVersion).Return(nil)
		mockMCMReconHelperAPI.EXPECT().areImagesReady(ctx, mcm.Name, "cluster-2").Return(true, nil)
		mockClient.EXPECT().Get(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(2)
		mockManifestAPI.EXPECT().SetManifestWorkAsDesired(ctx, gomock.Any(), *mcm, expectedKernelVersion).Return(nil).Times(2)
		mockManifestAPI.EXPECT().GarbageCollect(ctx, *expectedClusters, *mcm).Return(nil)
		mockManifestAPI.EXPECT().GetOwnedManifestWorks(ctx, *mcm).Return(expectedOwnManifestWork, nil)
		mockStatusupdaterAPI.EXPECT().ManagedClusterModuleUpdateStatus(ctx, mcm, expectedOwnManifestWork.Items).Return(nil)

		mcmr := &ManagedClusterModuleReconciler{
			client:           mockClient,
			manifestAPI:      mockManifestAPI,
			clusterAPI:       mockClusterAPI,
			statusupdaterAPI: mockStatusupdaterAPI,
			reconHelper:      mockMCMReconHelperAPI,
		}

		_, err := mcmr.Reconcile(context.Background(), mcm)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("managedClusterModuleReconcilerHelperAPI_setMicAsDesired", func() {

	const (
		mcmName     = "mcm-name"
		clusterName = "cluster-name"
		defaultNs   = "openshift-kmm"
	)

	var (
		ctx               context.Context
		ctrl              *gomock.Controller
		mockClusterAPI    *cluster.MockClusterAPI
		mockMIC           *mic.MockMIC
		mcmReconHelperAPI managedClusterModuleReconcilerHelperAPI
		micName           = mcmName + "-" + clusterName
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockClusterAPI = cluster.NewMockClusterAPI(ctrl)
		mockMIC = mic.NewMockMIC(ctrl)
		mcmReconHelperAPI = newManagedClusterModuleReconcilerHelper(mockClusterAPI, mockMIC)
	})

	It("should return an error if we faild to get an MLD for a kernel", func() {

		mcm := &hubv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: hubv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{},
			},
		}
		kernelVersions := []string{"v1.1.1"}

		mockClusterAPI.EXPECT().GetModuleLoaderDataForKernel(mcm, kernelVersions[0]).Return(nil, errors.New("some error"))

		err := mcmReconHelperAPI.setMicAsDesired(ctx, mcm, clusterName, kernelVersions)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get MLD for kernel"))
	})

	It("should return an error if we faild to create or patch the MIC", func() {

		mcm := &hubv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: hubv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{
					ModuleLoader: &kmmv1beta1.ModuleLoaderSpec{},
				},
			},
		}
		kernelVersions := []string{"v1.1.1"}

		gomock.InOrder(
			mockClusterAPI.EXPECT().GetModuleLoaderDataForKernel(mcm, kernelVersions[0]).Return(&api.ModuleLoaderData{}, nil),
			mockClusterAPI.EXPECT().GetDefaultArtifactsNamespace().Return(defaultNs),
			mockMIC.EXPECT().CreateOrPatch(ctx, micName, defaultNs, gomock.Any(), nil, v1.PullPolicy(""), true, mcm.Spec.ModuleSpec.ImageRebuildTriggerGeneration, gomock.Any(), mcm).
				Return(errors.New("some error")),
		)

		err := mcmReconHelperAPI.setMicAsDesired(ctx, mcm, clusterName, kernelVersions)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to createOrPatch MIC"))
	})

	It("should skip non targeted kernel versions", func() {

		mcm := &hubv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: hubv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{
					ModuleLoader: &kmmv1beta1.ModuleLoaderSpec{},
				},
			},
		}
		kernelVersions := []string{"v1.1.1", "1.1.2"}

		expectedMLD := &api.ModuleLoaderData{
			ContainerImage: "some-image",
			KernelVersion:  "v1.1.2",
		}
		expectedImages := []kmmv1beta1.ModuleImageSpec{
			{
				Image:         "some-image",
				KernelVersion: "v1.1.2",
			},
		}

		gomock.InOrder(
			mockClusterAPI.EXPECT().GetModuleLoaderDataForKernel(mcm, kernelVersions[0]).Return(nil, module.ErrNoMatchingKernelMapping),
			mockClusterAPI.EXPECT().GetModuleLoaderDataForKernel(mcm, kernelVersions[1]).Return(expectedMLD, nil),
			mockClusterAPI.EXPECT().GetDefaultArtifactsNamespace().Return(defaultNs),
			mockMIC.EXPECT().CreateOrPatch(ctx, micName, defaultNs, expectedImages, gomock.Any(), v1.PullPolicy(""), true, mcm.Spec.ModuleSpec.ImageRebuildTriggerGeneration, gomock.Any(), mcm).Return(nil),
		)

		err := mcmReconHelperAPI.setMicAsDesired(ctx, mcm, clusterName, kernelVersions)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should work as expected", func() {

		mcm := &hubv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: hubv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{
					ModuleLoader: &kmmv1beta1.ModuleLoaderSpec{},
				},
			},
		}
		kernelVersions := []string{"v1.1.1", "1.1.2"}

		expectedMLDs := []*api.ModuleLoaderData{
			{
				ContainerImage: "some-image",
				KernelVersion:  "v1.1.1",
			},
			{
				ContainerImage: "some-image",
				KernelVersion:  "v1.1.2",
			},
		}
		expectedImages := []kmmv1beta1.ModuleImageSpec{
			{
				Image:         "some-image",
				KernelVersion: "v1.1.1",
			},
			{
				Image:         "some-image",
				KernelVersion: "v1.1.2",
			},
		}

		gomock.InOrder(
			mockClusterAPI.EXPECT().GetModuleLoaderDataForKernel(mcm, kernelVersions[0]).Return(expectedMLDs[0], nil),
			mockClusterAPI.EXPECT().GetModuleLoaderDataForKernel(mcm, kernelVersions[1]).Return(expectedMLDs[1], nil),
			mockClusterAPI.EXPECT().GetDefaultArtifactsNamespace().Return(defaultNs),
			mockMIC.EXPECT().CreateOrPatch(ctx, micName, defaultNs, expectedImages, gomock.Any(), v1.PullPolicy(""), true, mcm.Spec.ModuleSpec.ImageRebuildTriggerGeneration, gomock.Any(), mcm).Return(nil),
		)

		err := mcmReconHelperAPI.setMicAsDesired(ctx, mcm, clusterName, kernelVersions)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("managedClusterModuleReconcilerHelperAPI_isMicReady", func() {
	const (
		mcmName     = "mcm-name"
		clusterName = "cluster-name"
		defaultNs   = "openshift-kmm"
	)

	var (
		ctx               context.Context
		ctrl              *gomock.Controller
		mockClusterAPI    *cluster.MockClusterAPI
		mockMIC           *mic.MockMIC
		mcmReconHelperAPI managedClusterModuleReconcilerHelperAPI
		micName           = mcmName + "-" + clusterName
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockClusterAPI = cluster.NewMockClusterAPI(ctrl)
		mockMIC = mic.NewMockMIC(ctrl)
		mcmReconHelperAPI = newManagedClusterModuleReconcilerHelper(mockClusterAPI, mockMIC)
	})

	It("should return an error if we faild to get the MIC", func() {

		mcm := &hubv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: hubv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{},
			},
		}

		gomock.InOrder(
			mockClusterAPI.EXPECT().GetDefaultArtifactsNamespace().Return(defaultNs),
			mockMIC.EXPECT().Get(ctx, micName, defaultNs).Return(nil, errors.New("some error")),
		)

		_, err := mcmReconHelperAPI.areImagesReady(ctx, mcm.Name, clusterName)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get MIC"))
	})

	It("should return false if MIC isn't ready", func() {

		mcm := &hubv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: hubv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{},
			},
		}

		expectedImages := []kmmv1beta1.ModuleImageSpec{
			{
				Image:         "some-image",
				KernelVersion: "v1.1.1",
			},
			{
				Image:         "some-image",
				KernelVersion: "v1.1.2",
			},
		}
		expectedMIC := &kmmv1beta1.ModuleImagesConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name: micName,
			},
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				Images: expectedImages,
			},
		}

		gomock.InOrder(
			mockClusterAPI.EXPECT().GetDefaultArtifactsNamespace().Return(defaultNs),
			mockMIC.EXPECT().Get(ctx, micName, defaultNs).Return(expectedMIC, nil),
			mockMIC.EXPECT().DoAllImagesExist(expectedMIC).Return(false),
		)

		allImagesExist, err := mcmReconHelperAPI.areImagesReady(ctx, mcm.Name, clusterName)
		Expect(err).NotTo(HaveOccurred())
		Expect(allImagesExist).To(BeFalse())
	})

	It("should return true if MIC is ready", func() {

		mcm := &hubv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{
				Name: mcmName,
			},
			Spec: hubv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{},
			},
		}

		expectedImages := []kmmv1beta1.ModuleImageSpec{
			{
				Image:         "some-image",
				KernelVersion: "v1.1.1",
			},
			{
				Image:         "some-image",
				KernelVersion: "v1.1.2",
			},
		}
		expectedMIC := &kmmv1beta1.ModuleImagesConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name: micName,
			},
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				Images: expectedImages,
			},
		}

		gomock.InOrder(
			mockClusterAPI.EXPECT().GetDefaultArtifactsNamespace().Return(defaultNs),
			mockMIC.EXPECT().Get(ctx, micName, defaultNs).Return(expectedMIC, nil),
			mockMIC.EXPECT().DoAllImagesExist(expectedMIC).Return(true),
		)

		allImagesExist, err := mcmReconHelperAPI.areImagesReady(ctx, mcm.Name, clusterName)
		Expect(err).NotTo(HaveOccurred())
		Expect(allImagesExist).To(BeTrue())
	})
})
