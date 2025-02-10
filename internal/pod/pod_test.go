package pod

import (
	context "context"
	"fmt"
	reflect "reflect"

	"github.com/google/go-cmp/cmp"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	gomock "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("ListPullPods", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		ip   ImagePuller
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		ip = NewImagePuller(clnt, scheme)
	})

	ctx := context.Background()
	testMic := kmmv1beta1.ModuleImagesConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some name",
			Namespace: "some namespace",
		},
	}

	It("list succeeded", func() {
		hl := ctrlclient.HasLabels{imageLabelKey}
		ml := ctrlclient.MatchingLabels{moduleImageLabelKey: testMic.Name}

		clnt.EXPECT().List(context.Background(), gomock.Any(), ctrlclient.InNamespace(testMic.Namespace), hl, ml).DoAndReturn(
			func(_ interface{}, podList *v1.PodList, _ ...interface{}) error {
				podList.Items = []v1.Pod{v1.Pod{}, v1.Pod{}}
				return nil
			},
		)

		pullPods, err := ip.ListPullPods(ctx, &testMic)
		Expect(err).To(BeNil())
		Expect(pullPods).ToNot(BeNil())
	})

	It("list failed", func() {
		hl := ctrlclient.HasLabels{imageLabelKey}
		ml := ctrlclient.MatchingLabels{moduleImageLabelKey: testMic.Name}

		clnt.EXPECT().List(context.Background(), gomock.Any(), ctrlclient.InNamespace(testMic.Namespace), hl, ml).Return(fmt.Errorf("some error"))

		pullPods, err := ip.ListPullPods(ctx, &testMic)
		Expect(err).To(HaveOccurred())
		Expect(pullPods).To(BeNil())
	})
})

var _ = Describe("DeletePod", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		ip   ImagePuller
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		ip = NewImagePuller(clnt, scheme)
	})

	ctx := context.Background()

	It("good flow", func() {
		pod := v1.Pod{}
		clnt.EXPECT().Delete(ctx, &pod).Return(nil)
		err := ip.DeletePod(ctx, &pod)
		Expect(err).To(BeNil())
	})

	It("error flow", func() {
		pod := v1.Pod{}
		clnt.EXPECT().Delete(ctx, &pod).Return(fmt.Errorf("some error"))
		err := ip.DeletePod(ctx, &pod)
		Expect(err).To(HaveOccurred())
	})

	It("deletion timestamp set", func() {
		pod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				DeletionTimestamp: &metav1.Time{},
			},
		}
		err := ip.DeletePod(ctx, &pod)
		Expect(err).To(BeNil())
	})
})

var _ = Describe("GetPullPodForImage", func() {
	var (
		ip ImagePuller
	)

	BeforeEach(func() {
		ip = NewImagePuller(nil, nil)
	})

	pullPods := []v1.Pod{
		{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{imageLabelKey: "image 1"},
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{imageLabelKey: "image 2"},
			},
		},
	}

	It("there is a pull pod for that image", func() {
		res := ip.GetPullPodForImage(pullPods, "image 2")
		Expect(res).ToNot(BeNil())
		Expect(res.Labels[imageLabelKey]).To(Equal("image 2"))
	})

	It("there is no pull pod for that image", func() {
		res := ip.GetPullPodForImage(pullPods, "image 23")
		Expect(res).To(BeNil())
	})
})

var _ = Describe("CreatePullPod", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		ip   ImagePuller
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		ip = NewImagePuller(clnt, scheme)
	})

	ctx := context.Background()
	testMic := kmmv1beta1.ModuleImagesConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some name",
			Namespace: "some namespace",
		},
	}
	testImageSpec := kmmv1beta1.ModuleImageSpec{
		Image: "some image",
	}

	It("check the pod fields", func() {
		expectedPod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: testMic.Name + "-pull-pod-",
				Namespace:    testMic.Namespace,
				Labels: map[string]string{
					moduleImageLabelKey: "some name",
					imageLabelKey:       "some image",
				},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:    pullerContainerName,
						Image:   "some image",
						Command: []string{"/bin/sh", "-c", "exit 0"},
					},
				},
				RestartPolicy:    v1.RestartPolicyOnFailure,
				ImagePullSecrets: []v1.LocalObjectReference{},
			},
		}

		clnt.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(
			func(ctx context.Context, obj ctrlclient.Object, opts ...ctrlclient.CreateOption) error {
				if pullPod, ok := obj.(*v1.Pod); ok {
					pullPod.OwnerReferences = nil
					if !reflect.DeepEqual(&expectedPod, pullPod) {
						diff := cmp.Diff(expectedPod, *pullPod)
						fmt.Println("Differences:\n", diff)
						return fmt.Errorf("pods not equal")
					}
				}
				return nil
			})
		err := ip.CreatePullPod(ctx, &testImageSpec, &testMic)
		Expect(err).To(BeNil())
	})
})
