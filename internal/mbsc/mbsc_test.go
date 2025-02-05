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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("SetModuleImageSpec", func() {
	var (
		mbsc MBSC
	)

	BeforeEach(func() {
		mbsc = NewMBSC(nil)
	})

	It("MBSC does not have any images in spec", func() {
		mbscObj := kmmv1beta1.ModuleBuildSignConfig{
			Spec: kmmv1beta1.ModuleImagesConfigSpec{},
		}
		imageSpec := kmmv1beta1.ModuleImageSpec{Image: "some image"}

		mbsc.SetModuleImageSpec(&mbscObj, &imageSpec)

		Expect(len(mbscObj.Spec.Images)).To(Equal(1))
	})

	It("MBSC has different images in spec", func() {
		mbscObj := kmmv1beta1.ModuleBuildSignConfig{
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				Images: []kmmv1beta1.ModuleImageSpec{
					kmmv1beta1.ModuleImageSpec{Image: "image 1"},
					kmmv1beta1.ModuleImageSpec{Image: "image 2"},
				},
			},
		}
		imageSpec := kmmv1beta1.ModuleImageSpec{Image: "some image"}

		mbsc.SetModuleImageSpec(&mbscObj, &imageSpec)

		Expect(len(mbscObj.Spec.Images)).To(Equal(3))
	})

	It("MBSC has the image already in spec", func() {
		mbscObj := kmmv1beta1.ModuleBuildSignConfig{
			Spec: kmmv1beta1.ModuleImagesConfigSpec{
				Images: []kmmv1beta1.ModuleImageSpec{
					kmmv1beta1.ModuleImageSpec{Image: "image 1"},
					kmmv1beta1.ModuleImageSpec{Image: "image 2"},
				},
			},
		}
		imageSpec := kmmv1beta1.ModuleImageSpec{Image: "image 2"}

		mbsc.SetModuleImageSpec(&mbscObj, &imageSpec)

		Expect(len(mbscObj.Spec.Images)).To(Equal(2))
	})
})

var _ = Describe("GetMBSC", func() {
	var (
		ctrl       *gomock.Controller
		mockClient *client.MockClient
		mbsc       MBSC
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockClient = client.NewMockClient(ctrl)
		mbsc = NewMBSC(mockClient)
	})

	ctx := context.Background()

	It("mbsc object does not exists", func() {
		mockClient.EXPECT().Get(ctx, types.NamespacedName{Name: "some name", Namespace: "some namespace"}, gomock.Any()).Return(
			k8serrors.NewNotFound(schema.GroupResource{}, "owner name"))

		res, err := mbsc.GetMBSC(ctx, "some name", "some namespace")
		Expect(err).To(BeNil())
		Expect(res).To(BeNil())
	})

	It("client get returns some error", func() {
		mockClient.EXPECT().Get(ctx, types.NamespacedName{Name: "some name", Namespace: "some namespace"}, gomock.Any()).Return(fmt.Errorf("some error"))

		res, err := mbsc.GetMBSC(ctx, "some name", "some namespace")
		Expect(err).To(HaveOccurred())
		Expect(res).To(BeNil())
	})

	It("mbsc object exists", func() {
		mockClient.EXPECT().Get(ctx, types.NamespacedName{Name: "some name", Namespace: "some namespace"}, gomock.Any()).Return(nil)

		res, err := mbsc.GetMBSC(ctx, "some name", "some namespace")
		Expect(err).To(BeNil())
		Expect(res).ToNot(BeNil())
	})
})
