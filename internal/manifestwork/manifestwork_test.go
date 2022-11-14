package manifestwork

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/golang/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
)

var (
	ctrl *gomock.Controller
	clnt *client.MockClient
)

var _ = Describe("GarbageCollect", func() {
	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
	})

	ctx := context.Background()

	It("should work as expected", func() {
		mcm := kmmv1beta1.ManagedClusterModule{
			Spec: kmmv1beta1.ManagedClusterModuleSpec{
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

		mwc := NewCreator(clnt, scheme)

		err := mwc.GarbageCollect(context.Background(), clusterList, mcm)
		Expect(err).NotTo(HaveOccurred())
	})

})

var _ = Describe("SetManifestWorkAsDesired", func() {
	mwc := NewCreator(nil, scheme)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
	})

	It("should return an error if the ManifestWork is nil", func() {
		Expect(
			mwc.SetManifestWorkAsDesired(context.Background(), nil, kmmv1beta1.ManagedClusterModule{}),
		).To(
			HaveOccurred(),
		)
	})

	It("should remove all Build and Sign sections of the Module", func() {
		mcm := kmmv1beta1.ManagedClusterModule{
			Spec: kmmv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{
					ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
						Container: kmmv1beta1.ModuleLoaderContainerSpec{
							Build: &kmmv1beta1.Build{},
							Sign:  &kmmv1beta1.Sign{},
							KernelMappings: []kmmv1beta1.KernelMapping{
								{
									Build: &kmmv1beta1.Build{},
									Sign:  &kmmv1beta1.Sign{},
								},
							},
						},
					},
					Selector: map[string]string{"key": "value"},
				},
			},
		}

		mw := &workv1.ManifestWork{}

		err := mwc.SetManifestWorkAsDesired(context.Background(), mw, mcm)
		Expect(err).NotTo(HaveOccurred())
		Expect(mw.Spec.Workload.Manifests).To(HaveLen(1))

		manifestModuleSpec := (mw.Spec.Workload.Manifests[0].RawExtension.Object).(*kmmv1beta1.Module).Spec
		Expect(manifestModuleSpec.ModuleLoader.Container.Build).To(BeNil())
		Expect(manifestModuleSpec.ModuleLoader.Container.Sign).To(BeNil())
		Expect(manifestModuleSpec.ModuleLoader.Container.KernelMappings[0].Build).To(BeNil())
		Expect(manifestModuleSpec.ModuleLoader.Container.KernelMappings[0].Sign).To(BeNil())
	})

	It("should work as expected", func() {
		mcm := kmmv1beta1.ManagedClusterModule{
			Spec: kmmv1beta1.ManagedClusterModuleSpec{
				ModuleSpec: kmmv1beta1.ModuleSpec{
					Selector: map[string]string{"key": "value"},
				},
			},
		}

		mw := &workv1.ManifestWork{}

		err := mwc.SetManifestWorkAsDesired(context.Background(), mw, mcm)
		Expect(err).NotTo(HaveOccurred())
		Expect(constants.ManagedClusterModuleNameLabel).To(BeKeyOf(mw.Labels))
		Expect(mw.Spec.Workload.Manifests).To(HaveLen(1))
		Expect((mw.Spec.Workload.Manifests[0].RawExtension.Object).(*kmmv1beta1.Module).Spec).To(Equal(mcm.Spec.ModuleSpec))
	})
})
