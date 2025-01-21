package mic

import (
	"context"
	"errors"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	v1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomock "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("HandleModuleImagesConfig", func() {

	const (
		micName      = "my-name"
		micNamespace = "my-namespace"
	)

	var (
		ctx        context.Context
		ctrl       *gomock.Controller
		mockClient *client.MockClient
		micAPIImpl *moduleImagesConfigAPI
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockClient = client.NewMockClient(ctrl)
		micAPIImpl = &moduleImagesConfigAPI{
			client: mockClient,
			scheme: scheme,
		}
		utilruntime.Must(v1beta1.AddToScheme(micAPIImpl.scheme))
	})

	It("should fail if we failed to get the generation", func() {

		mockClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(errors.New("some error"))

		err := micAPIImpl.HandleModuleImagesConfig(ctx, micName, micNamespace, []v1beta1.ModuleImageSpec{}, nil, nil)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to create or patch"))
		Expect(err.Error()).To(ContainSubstring("some error"))
	})

	It("should fail if we fail to set owner reference", func() {

		mockClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Times(2).Return(nil)

		err := micAPIImpl.HandleModuleImagesConfig(ctx, micName, micNamespace, []v1beta1.ModuleImageSpec{}, nil, nil)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to create or patch"))
		Expect(err.Error()).To(ContainSubstring("cannot call SetOwnerReference"))
	})

	It("should create the MIC if it doesn't exist", func() {

		gomock.InOrder(
			mockClient.EXPECT().Get(ctx, types.NamespacedName{Name: micName, Namespace: micNamespace},
				gomock.Any()).Times(2).Return(k8serrors.NewNotFound(schema.GroupResource{}, micName)),
			mockClient.EXPECT().Create(ctx, gomock.Any()).Return(nil),
		)

		images := []kmmv1beta1.ModuleImageSpec{
			{
				Image: "example.registry.com/org/user/image1:tag",
			},
			{
				Image: "example.registry.com/org/user/image2:tag",
			},
		}

		imageRepoSecret :=
			&v1.LocalObjectReference{
				Name: "some-secret",
			}

		owner := &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-module",
				Namespace: micNamespace,
			},
		}

		err := micAPIImpl.HandleModuleImagesConfig(ctx, micName, micNamespace, images, imageRepoSecret, owner)

		Expect(err).NotTo(HaveOccurred())
	})

	It("should patch the MIC if it exists", func() {

		gomock.InOrder(
			mockClient.EXPECT().Get(ctx, types.NamespacedName{Name: micName, Namespace: micNamespace}, gomock.Any()).Times(2).DoAndReturn(
				func(_ interface{}, _ interface{}, mic *kmmv1beta1.ModuleImagesConfig, _ ...ctrlclient.GetOption) error {
					mic.ObjectMeta = metav1.ObjectMeta{
						Name:      micName,
						Namespace: micNamespace,
					}
					mic.Spec = kmmv1beta1.ModuleImagesConfigSpec{
						Generation: 5,
						Images: []kmmv1beta1.ModuleImageSpec{
							{
								Image: "example.registry.com/org/user/image1:tag",
							},
							{
								Image: "example.registry.com/org/user/image2:tag",
							},
						},
						ImageRepoSecret: &v1.LocalObjectReference{
							Name: "some-secret",
						},
					}
					return nil
				},
			),
			mockClient.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()),
		)

		images := []kmmv1beta1.ModuleImageSpec{
			{
				Image: "example.registry.com/org/user/image3:tag",
			},
		}

		owner := &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "my-module",
				Namespace: micNamespace,
			},
		}

		err := micAPIImpl.HandleModuleImagesConfig(ctx, micName, micNamespace, images, nil, owner)

		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("getGeneration", func() {

	const (
		micName      = "my-name"
		micNamespace = "my-namespace"
	)

	var (
		ctx        context.Context
		ctrl       *gomock.Controller
		mockClient *client.MockClient
		micAPIImpl *moduleImagesConfigAPI
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockClient = client.NewMockClient(ctrl)
		micAPIImpl = &moduleImagesConfigAPI{
			client: mockClient,
		}
	})

	It("should fail if we failed to get the MIC", func() {

		mockClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(errors.New("some error"))

		_, err := micAPIImpl.getGeneration(ctx, micName, micNamespace)

		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError(ContainSubstring("failed to get ModuleImagesConfig")))
	})

	It("should return 0 if the MIC doesn't exists", func() {

		mockClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, micName))

		gen, err := micAPIImpl.getGeneration(ctx, micName, micNamespace)

		Expect(err).NotTo(HaveOccurred())
		Expect(gen).To(Equal(int64(0)))
	})

	It("should work as expected", func() {

		mockClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, _ interface{}, mic *kmmv1beta1.ModuleImagesConfig, _ ...ctrlclient.GetOption) error {
				mic.Spec.Generation = 5
				return nil
			},
		)

		gen, err := micAPIImpl.getGeneration(ctx, micName, micNamespace)

		Expect(err).NotTo(HaveOccurred())
		Expect(gen).To(Equal(int64(5)))
	})
})
