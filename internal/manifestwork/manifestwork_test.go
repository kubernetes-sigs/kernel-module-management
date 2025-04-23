package manifestwork

import (
	"context"
	"errors"

	hubv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api-hub/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
)

var (
	ctrl         *gomock.Controller
	clnt         *client.MockClient
	mockKM       *module.MockKernelMapper
	mockRegistry *registry.MockRegistry
)

var _ = Describe("GarbageCollect", func() {
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockKM = module.NewMockKernelMapper(ctrl)
		mockRegistry = registry.NewMockRegistry(ctrl)
	})

	ctx := context.Background()

	It("should work as expected", func() {
		mcm := hubv1beta1.ManagedClusterModule{
			Spec: hubv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{
					Selector: map[string]string{"key": "value"},
				},
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
				Name:      "test",
				Namespace: clusterList.Items[0].Name,
				Labels: map[string]string{
					constants.ManagedClusterModuleNameLabel: mcm.Name,
				},
			},
		}

		mwToBeCollected := workv1.ManifestWork{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "to-be-collected",
				Namespace: "not-in-the-cluster-list",
				Labels: map[string]string{
					constants.ManagedClusterModuleNameLabel: mcm.Name,
				},
			},
		}

		manifestWorkList := workv1.ManifestWorkList{
			Items: []workv1.ManifestWork{mw, mwToBeCollected},
		}

		gomock.InOrder(
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *workv1.ManifestWorkList, _ ...interface{}) error {
					list.Items = manifestWorkList.Items
					return nil
				},
			),
			clnt.EXPECT().Delete(ctx, &mwToBeCollected),
		)

		mwc := NewCreator(clnt, scheme, nil, nil, "")

		err := mwc.GarbageCollect(context.Background(), clusterList, mcm)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("GetOwnedManifestWorks", func() {
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
	})

	ctx := context.Background()

	It("should work as expected", func() {
		mcm := hubv1beta1.ManagedClusterModule{
			Spec: hubv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{
					Selector: map[string]string{"key": "value"},
				},
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
				Name:      "test",
				Namespace: clusterList.Items[0].Name,
				Labels: map[string]string{
					constants.ManagedClusterModuleNameLabel: mcm.Name,
				},
			},
		}

		manifestWorkList := workv1.ManifestWorkList{
			Items: []workv1.ManifestWork{mw},
		}

		gomock.InOrder(
			clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *workv1.ManifestWorkList, _ ...interface{}) error {
					list.Items = manifestWorkList.Items
					return nil
				},
			),
		)

		mwc := NewCreator(clnt, scheme, nil, nil, "")

		ownedManifestWorks, err := mwc.GetOwnedManifestWorks(context.Background(), mcm)
		Expect(err).NotTo(HaveOccurred())
		Expect(ownedManifestWorks.Items).NotTo(BeEmpty())
		Expect(ownedManifestWorks.Items).To(Equal(manifestWorkList.Items))
	})
})

var _ = Describe("SetManifestWorkAsDesired", func() {
	const (
		imageName      = "image-name"
		kernelVersion  = "1.2.3"
		mcmName        = "test"
		spokeNamespace = "test-namespace"
	)

	var (
		mcm hubv1beta1.ManagedClusterModule
		mld api.ModuleLoaderData
		mw  *workv1.ManifestWork
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockKM = module.NewMockKernelMapper(ctrl)
		mockRegistry = registry.NewMockRegistry(ctrl)

		mcm = hubv1beta1.ManagedClusterModule{
			ObjectMeta: metav1.ObjectMeta{Name: mcmName},
			Spec: hubv1beta1.ManagedClusterModuleSpec{
				SpokeNamespace: spokeNamespace,
				ModuleSpec: kmmv1beta1.ModuleSpec{
					ModuleLoader: &kmmv1beta1.ModuleLoaderSpec{
						Container: kmmv1beta1.ModuleLoaderContainerSpec{
							Build: &kmmv1beta1.Build{},
							Sign:  &kmmv1beta1.Sign{},
							KernelMappings: []kmmv1beta1.KernelMapping{
								{
									Regexp:         "^.+$",
									ContainerImage: imageName,
									Build:          &kmmv1beta1.Build{},
									Sign:           &kmmv1beta1.Sign{},
								},
							},
						},
					},
					Selector: map[string]string{"key": "value"},
				},
			},
		}

		mld = api.ModuleLoaderData{
			ContainerImage: imageName,
			KernelVersion:  kernelVersion,
		}

		mw = &workv1.ManifestWork{}
	})

	It("should return an error if the ManifestWork is nil", func() {
		mwc := NewCreator(clnt, scheme, mockKM, mockRegistry, "")

		Expect(
			mwc.SetManifestWorkAsDesired(context.Background(), nil, hubv1beta1.ManagedClusterModule{}, nil),
		).To(
			HaveOccurred(),
		)
	})

	It("should empty the Module's kernel mappings when no kernel mappings match", func() {
		gomock.InOrder(
			mockKM.EXPECT().GetModuleLoaderDataForKernel(gomock.Any(), kernelVersion).Return(nil, errors.New("no-mappings-found")),
		)

		mwc := NewCreator(clnt, scheme, mockKM, mockRegistry, "")

		err := mwc.SetManifestWorkAsDesired(context.Background(), mw, mcm, []string{kernelVersion})
		Expect(err).NotTo(HaveOccurred())
		Expect(mw.Spec.Workload.Manifests).To(HaveLen(1))

		manifestModuleSpec := (mw.Spec.Workload.Manifests[0].RawExtension.Object).(*kmmv1beta1.Module).Spec
		Expect(manifestModuleSpec.ModuleLoader.Container.Build).To(BeNil())
		Expect(manifestModuleSpec.ModuleLoader.Container.Sign).To(BeNil())
		Expect(manifestModuleSpec.ModuleLoader.Container.KernelMappings).To(BeEmpty())
	})

	It("should work as expected", func() {
		expectedResourceIdentifier := workv1.ResourceIdentifier{
			Group:     "kmm.sigs.x-k8s.io",
			Resource:  "modules",
			Name:      mcm.Name,
			Namespace: spokeNamespace,
		}

		expectedModuleSpec := kmmv1beta1.ModuleSpec{
			ModuleLoader: &kmmv1beta1.ModuleLoaderSpec{
				Container: kmmv1beta1.ModuleLoaderContainerSpec{
					Build: nil,
					Sign:  nil,
					KernelMappings: []kmmv1beta1.KernelMapping{
						{
							Literal:        kernelVersion,
							ContainerImage: imageName,
						},
					},
				},
			},
			Selector: map[string]string{"key": "value"},
		}

		gomock.InOrder(
			mockKM.EXPECT().GetModuleLoaderDataForKernel(gomock.Any(), kernelVersion).Return(&mld, nil),
		)

		mwc := NewCreator(clnt, scheme, mockKM, mockRegistry, "")

		err := mwc.SetManifestWorkAsDesired(context.Background(), mw, mcm, []string{kernelVersion})

		Expect(err).NotTo(HaveOccurred())
		Expect(constants.ManagedClusterModuleNameLabel).To(BeKeyOf(mw.Labels))
		Expect(mw.Spec.Workload.Manifests).To(HaveLen(1))
		Expect((mw.Spec.Workload.Manifests[0].RawExtension.Object).(*kmmv1beta1.Module).Spec).To(Equal(expectedModuleSpec))

		Expect(mw.Spec.ManifestConfigs).To(HaveLen(1))
		Expect(mw.Spec.ManifestConfigs[0].ResourceIdentifier).To(Equal(expectedResourceIdentifier))

		Expect(mw.Spec.ManifestConfigs[0].FeedbackRules).To(HaveLen(1))
		Expect(mw.Spec.ManifestConfigs[0].FeedbackRules[0].Type).To(Equal(workv1.JSONPathsType))
		Expect(mw.Spec.ManifestConfigs[0].FeedbackRules[0].JsonPaths).To(Equal(moduleStatusJSONPaths))
	})
})
