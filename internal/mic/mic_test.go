package mic

import (
	"context"
	"fmt"

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

var _ = Describe("CreateOrPatch", func() {

	const (
		micName      = "my-name"
		micNamespace = "my-namespace"
	)

	var (
		ctx        context.Context
		ctrl       *gomock.Controller
		mockClient *client.MockClient
		micAPI     MIC
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockClient = client.NewMockClient(ctrl)
		micAPI = New(mockClient, scheme)
		utilruntime.Must(v1beta1.AddToScheme(scheme))
	})

	It("should fail if we failed to create or patch", func() {

		mockClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		err := micAPI.CreateOrPatch(ctx, micName, micNamespace, []v1beta1.ModuleImageSpec{}, nil, "", false, nil, nil, &kmmv1beta1.Module{})

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to create or patch"))
		Expect(err.Error()).To(ContainSubstring("some error"))
	})

	It("should create the MIC if it doesn't exist", func() {

		gomock.InOrder(
			mockClient.EXPECT().Get(ctx, types.NamespacedName{Name: micName, Namespace: micNamespace},
				gomock.Any()).Return(k8serrors.NewNotFound(schema.GroupResource{}, micName)),
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

		err := micAPI.CreateOrPatch(ctx, micName, micNamespace, images, imageRepoSecret, "", true, nil, nil, owner)

		Expect(err).NotTo(HaveOccurred())
	})

	It("should patch the MIC if it exists", func() {

		gomock.InOrder(
			mockClient.EXPECT().Get(ctx, types.NamespacedName{Name: micName, Namespace: micNamespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, mic *kmmv1beta1.ModuleImagesConfig, _ ...ctrlclient.GetOption) error {
					mic.ObjectMeta = metav1.ObjectMeta{
						Name:      micName,
						Namespace: micNamespace,
					}
					mic.Spec = kmmv1beta1.ModuleImagesConfigSpec{
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
						PushBuiltImage: true,
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

		err := micAPI.CreateOrPatch(ctx, micName, micNamespace, images, nil, v1.PullIfNotPresent, true, nil, nil, owner)

		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("Get", func() {

	const (
		micName      = "my-name"
		micNamespace = "my-namespace"
	)

	var (
		ctx        context.Context
		ctrl       *gomock.Controller
		mockClient *client.MockClient
		micAPI     MIC
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		mockClient = client.NewMockClient(ctrl)
		micAPI = New(mockClient, scheme)
		utilruntime.Must(v1beta1.AddToScheme(scheme))
	})

	It("should fail if we failed to get the MIC", func() {

		mockClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		_, err := micAPI.Get(ctx, micName, micNamespace)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("could not get ModuleImagesConfig"))
	})

	It("should work as expected", func() {

		mockClient.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(nil)

		_, err := micAPI.Get(ctx, micName, micNamespace)

		Expect(err).NotTo(HaveOccurred())
	})

})

var _ = Describe("GetModuleImageSpec", func() {
	var (
		micAPI MIC
	)

	BeforeEach(func() {
		micAPI = New(nil, nil)
	})

	testMic := kmmv1beta1.ModuleImagesConfig{
		Spec: kmmv1beta1.ModuleImagesConfigSpec{
			Images: []kmmv1beta1.ModuleImageSpec{
				{
					Image: "image 1",
				},
				{
					Image: "image 2",
				},
			},
		},
	}

	It("check image present and not present scenarious", func() {

		By("image spec is present")
		res := micAPI.GetModuleImageSpec(&testMic, "image 1")
		Expect(res).ToNot(BeNil())
		Expect(res.Image).To(Equal("image 1"))

		By("image spec is not present")
		res = micAPI.GetModuleImageSpec(&testMic, "image 3")
		Expect(res).To(BeNil())
	})
})

var _ = Describe("SetImageStatus", func() {
	var (
		micAPI MIC
	)

	BeforeEach(func() {
		micAPI = New(nil, nil)
	})

	testMic := kmmv1beta1.ModuleImagesConfig{
		Status: kmmv1beta1.ModuleImagesConfigStatus{
			ImagesStates: []kmmv1beta1.ModuleImageState{
				{
					Image:  "image 1",
					Status: kmmv1beta1.ImageDoesNotExist,
				},
				{
					Image:  "image 2",
					Status: kmmv1beta1.ImageExists,
				},
			},
		},
	}

	It("set images status for both present and not present statuses", func() {

		By("image status is present")
		micAPI.SetImageStatus(&testMic, "image 1", kmmv1beta1.ImageExists)
		Expect(testMic.Status.ImagesStates[0].Image).To(Equal("image 1"))
		Expect(testMic.Status.ImagesStates[0].Status).To(Equal(kmmv1beta1.ImageExists))

		By("image status is not present")
		micAPI.SetImageStatus(&testMic, "image 3", kmmv1beta1.ImageDoesNotExist)
		Expect(testMic.Status.ImagesStates[2].Image).To(Equal("image 3"))
		Expect(testMic.Status.ImagesStates[2].Status).To(Equal(kmmv1beta1.ImageDoesNotExist))
	})
})

var _ = Describe("GetImageState", func() {
	var (
		micAPI MIC
	)

	BeforeEach(func() {
		micAPI = New(nil, nil)
	})

	testMic := kmmv1beta1.ModuleImagesConfig{
		Status: kmmv1beta1.ModuleImagesConfigStatus{
			ImagesStates: []kmmv1beta1.ModuleImageState{
				{
					Image:  "image 1",
					Status: kmmv1beta1.ImageDoesNotExist,
				},
				{
					Image:  "image 2",
					Status: kmmv1beta1.ImageExists,
				},
			},
		},
	}

	It("get images status for both present and not present statuses", func() {

		By("image status is present")
		res := micAPI.GetImageState(&testMic, "image 1")
		Expect(res).To(Equal(kmmv1beta1.ImageDoesNotExist))

		By("image status is not present")
		res = micAPI.GetImageState(&testMic, "image 3")
		Expect(res).To(BeEmpty())
	})
})

var _ = Describe("DoAllImagesExist", func() {

	var micAPI MIC

	BeforeEach(func() {
		micAPI = New(nil, nil)
	})

	It("should return false if one or more images doesn't have a status", func() {

		micObj := &kmmv1beta1.ModuleImagesConfig{
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				Images: []kmmv1beta1.ModuleImageSpec{
					{
						Image: "image-1",
					},
				},
			},
		}

		Expect(micAPI.DoAllImagesExist(micObj)).To(BeFalse())
	})

	It("should return false if one or more images doesn't exists", func() {

		micObj := &kmmv1beta1.ModuleImagesConfig{
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				Images: []kmmv1beta1.ModuleImageSpec{
					{
						Image: "image-1",
					},
					{
						Image: "image-2",
					},
				},
			},
			Status: kmmv1beta1.ModuleImagesConfigStatus{
				ImagesStates: []kmmv1beta1.ModuleImageState{
					{
						Image:  "image-1",
						Status: kmmv1beta1.ImageExists,
					},
					{
						Image:  "image-2",
						Status: kmmv1beta1.ImageDoesNotExist,
					},
				},
			},
		}

		Expect(micAPI.DoAllImagesExist(micObj)).To(BeFalse())
	})

	It("should return true if all images exists", func() {

		micObj := &kmmv1beta1.ModuleImagesConfig{
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				Images: []kmmv1beta1.ModuleImageSpec{
					{
						Image: "image-1",
					},
					{
						Image: "image-2",
					},
				},
			},
			Status: kmmv1beta1.ModuleImagesConfigStatus{
				ImagesStates: []kmmv1beta1.ModuleImageState{
					{
						Image:  "image-1",
						Status: kmmv1beta1.ImageExists,
					},
					{
						Image:  "image-2",
						Status: kmmv1beta1.ImageExists,
					},
				},
			},
		}

		Expect(micAPI.DoAllImagesExist(micObj)).To(BeTrue())
	})
})

var _ = Describe("filterDuplicateImages", func() {
	It("check flow", func() {
		images := []kmmv1beta1.ModuleImageSpec{
			{Image: "example.registry.com/org/user/image1:tag"},
			{Image: "example.registry.com/org/user/image2:tag"},
			{Image: "example.registry.com/org/user/image3:tag"},
			{Image: "example.registry.com/org/user/image2:tag"},
			{Image: "example.registry.com/org/user/image4:tag"},
			{Image: "example.registry.com/org/user/image1:tag"},
		}

		expectedRes := []kmmv1beta1.ModuleImageSpec{
			{Image: "example.registry.com/org/user/image1:tag"},
			{Image: "example.registry.com/org/user/image2:tag"},
			{Image: "example.registry.com/org/user/image3:tag"},
			{Image: "example.registry.com/org/user/image4:tag"},
		}

		res := filterDuplicateImages(images)
		Expect(res).To(Equal(expectedRes))
	})
})
