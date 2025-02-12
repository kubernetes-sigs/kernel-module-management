package mbsc

import (
	"context"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomock "go.uber.org/mock/gomock"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("setModuleImageSpec", func() {
	It("MBSC does not have any images in spec", func() {
		mbscObj := kmmv1beta1.ModuleBuildSignConfig{
			Spec: kmmv1beta1.ModuleBuildSignConfigSpec{},
		}
		imageSpec := kmmv1beta1.ModuleImageSpec{Image: "some image"}

		setModuleImageSpec(&mbscObj, &imageSpec, kmmv1beta1.SignImage)

		Expect(len(mbscObj.Spec.Images)).To(Equal(1))
		Expect(mbscObj.Spec.Images[0].Action).To(Equal(kmmv1beta1.SignImage))
	})

	It("MBSC has different images in spec", func() {
		mbscObj := kmmv1beta1.ModuleBuildSignConfig{
			Spec: kmmv1beta1.ModuleBuildSignConfigSpec{
				Images: []kmmv1beta1.ModuleBuildSignSpec{
					kmmv1beta1.ModuleBuildSignSpec{ModuleImageSpec: kmmv1beta1.ModuleImageSpec{Image: "image 1"}},
					kmmv1beta1.ModuleBuildSignSpec{ModuleImageSpec: kmmv1beta1.ModuleImageSpec{Image: "image 2"}},
				},
			},
		}
		imageSpec := kmmv1beta1.ModuleImageSpec{Image: "some image"}

		setModuleImageSpec(&mbscObj, &imageSpec, kmmv1beta1.BuildImage)

		Expect(len(mbscObj.Spec.Images)).To(Equal(3))
		Expect(mbscObj.Spec.Images[2].Action).To(Equal(kmmv1beta1.BuildImage))
	})

	It("MBSC has the image already in spec", func() {
		mbscObj := kmmv1beta1.ModuleBuildSignConfig{
			Spec: kmmv1beta1.ModuleBuildSignConfigSpec{
				Images: []kmmv1beta1.ModuleBuildSignSpec{
					kmmv1beta1.ModuleBuildSignSpec{ModuleImageSpec: kmmv1beta1.ModuleImageSpec{Image: "image 1"}},
					kmmv1beta1.ModuleBuildSignSpec{ModuleImageSpec: kmmv1beta1.ModuleImageSpec{Image: "image 2"}, Action: kmmv1beta1.SignImage},
				},
			},
		}
		imageSpec := kmmv1beta1.ModuleImageSpec{Image: "image 2"}

		setModuleImageSpec(&mbscObj, &imageSpec, kmmv1beta1.BuildImage)

		Expect(len(mbscObj.Spec.Images)).To(Equal(2))
		Expect(mbscObj.Spec.Images[1].Action).To(Equal(kmmv1beta1.BuildImage))
	})
})

var _ = Describe("Get", func() {
	var (
		ctrl       *gomock.Controller
		mockClient *client.MockClient
		mbsc       MBSC
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockClient = client.NewMockClient(ctrl)
		mbsc = New(mockClient, nil)
	})

	ctx := context.Background()

	It("mbsc object does not exists", func() {
		mockClient.EXPECT().Get(ctx, types.NamespacedName{Name: "some name", Namespace: "some namespace"}, gomock.Any()).Return(
			k8serrors.NewNotFound(schema.GroupResource{}, "owner name"))

		res, err := mbsc.Get(ctx, "some name", "some namespace")
		Expect(err).To(BeNil())
		Expect(res).To(BeNil())
	})

	It("client get returns some error", func() {
		mockClient.EXPECT().Get(ctx, types.NamespacedName{Name: "some name", Namespace: "some namespace"}, gomock.Any()).Return(fmt.Errorf("some error"))

		res, err := mbsc.Get(ctx, "some name", "some namespace")
		Expect(err).To(HaveOccurred())
		Expect(res).To(BeNil())
	})

	It("mbsc object exists", func() {
		mockClient.EXPECT().Get(ctx, types.NamespacedName{Name: "some name", Namespace: "some namespace"}, gomock.Any()).Return(nil)

		res, err := mbsc.Get(ctx, "some name", "some namespace")
		Expect(err).To(BeNil())
		Expect(res).ToNot(BeNil())
	})
})

var _ = Describe("CreateOrPatch", func() {
	var (
		ctrl       *gomock.Controller
		mockClient *client.MockClient
		mbsc       MBSC
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockClient = client.NewMockClient(ctrl)
		mbsc = New(mockClient, scheme)
	})

	ctx := context.Background()
	objName := "some name"
	objNamespace := "some namespace"
	imageSpec := kmmv1beta1.ModuleImageSpec{Image: "some image"}
	micObj := kmmv1beta1.ModuleImagesConfig{
		ObjectMeta: metav1.ObjectMeta{Name: objName, Namespace: objNamespace},
	}

	It("MSBC does not exists", func() {
		gomock.InOrder(
			mockClient.EXPECT().Get(ctx, types.NamespacedName{Name: objName, Namespace: objNamespace}, gomock.Any()).Return(
				k8serrors.NewNotFound(schema.GroupResource{}, "some name")),
			mockClient.EXPECT().Create(ctx, gomock.Any()).Return(nil),
		)

		err := mbsc.CreateOrPatch(ctx, &micObj, &imageSpec, kmmv1beta1.BuildImage)
		Expect(err).To(BeNil())
	})

	It("MSBC does not existsi, create fails", func() {
		gomock.InOrder(
			mockClient.EXPECT().Get(ctx, types.NamespacedName{Name: objName, Namespace: objNamespace}, gomock.Any()).Return(
				k8serrors.NewNotFound(schema.GroupResource{}, "some name")),
			mockClient.EXPECT().Create(ctx, gomock.Any()).Return(fmt.Errorf("some error")),
		)

		err := mbsc.CreateOrPatch(ctx, &micObj, &imageSpec, kmmv1beta1.BuildImage)
		Expect(err).To(HaveOccurred())
	})

	It("MSBC exists", func() {
		gomock.InOrder(
			mockClient.EXPECT().Get(ctx, types.NamespacedName{Name: objName, Namespace: objNamespace}, gomock.Any()).Return(nil),
			mockClient.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Return(nil),
		)
		err := mbsc.CreateOrPatch(ctx, &micObj, &imageSpec, kmmv1beta1.BuildImage)
		Expect(err).To(BeNil())
	})

	It("MSBC exists. patch fails", func() {
		gomock.InOrder(
			mockClient.EXPECT().Get(ctx, types.NamespacedName{Name: objName, Namespace: objNamespace}, gomock.Any()).Return(nil),
			mockClient.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error")),
		)
		err := mbsc.CreateOrPatch(ctx, &micObj, &imageSpec, kmmv1beta1.BuildImage)
		Expect(err).To(HaveOccurred())
	})
})
