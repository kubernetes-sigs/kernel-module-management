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
	testName := "some name"
	testNamespace := "some namespace"

	It("list succeeded", func() {
		hl := ctrlclient.HasLabels{pullPodTypeLabelKey}
		ml := ctrlclient.MatchingLabels{imageOwnerLabelKey: testName}

		clnt.EXPECT().List(context.Background(), gomock.Any(), ctrlclient.InNamespace(testNamespace), hl, ml).DoAndReturn(
			func(_ interface{}, podList *v1.PodList, _ ...interface{}) error {
				podList.Items = []v1.Pod{v1.Pod{}, v1.Pod{}}
				return nil
			},
		)

		pullPods, err := ip.ListPullPods(ctx, testName, testNamespace)
		Expect(err).To(BeNil())
		Expect(pullPods).ToNot(BeNil())
	})

	It("list failed", func() {
		hl := ctrlclient.HasLabels{pullPodTypeLabelKey}
		ml := ctrlclient.MatchingLabels{imageOwnerLabelKey: testName}

		clnt.EXPECT().List(context.Background(), gomock.Any(), ctrlclient.InNamespace(testNamespace), hl, ml).Return(fmt.Errorf("some error"))

		pullPods, err := ip.ListPullPods(ctx, testName, testNamespace)
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
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Image: "image 1",
					},
				},
			},
		},
		{
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Image: "image 2",
					},
				},
			},
		},
	}

	It("there is a pull pod for that image", func() {
		res := ip.GetPullPodForImage(pullPods, "image 2")
		Expect(res).ToNot(BeNil())
		Expect(res.Spec.Containers[0].Image).To(Equal("image 2"))
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
	testName := "some name"
	testNamespace := "some namespace"
	testImage := "some image"
	testMic := kmmv1beta1.ModuleImagesConfig{}
	testRepoSecret := v1.LocalObjectReference{}
	imagePullPolicy := v1.PullAlways

	It("check the pod fields", func() {
		expectedPod := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: testName + "-pull-pod-",
				Namespace:    testNamespace,
				Labels: map[string]string{
					imageOwnerLabelKey:  testName,
					pullPodTypeLabelKey: pullPodUntilSuccess,
				},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:            pullerContainerName,
						Image:           testImage,
						Command:         []string{"/bin/sh", "-c", "exit 0"},
						ImagePullPolicy: imagePullPolicy,
					},
				},
				RestartPolicy:    v1.RestartPolicyNever,
				ImagePullSecrets: []v1.LocalObjectReference{testRepoSecret},
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
		err := ip.CreatePullPod(ctx, testName, testNamespace, testImage, false, &testRepoSecret, imagePullPolicy, &testMic)
		Expect(err).To(BeNil())
	})
})

var _ = Describe("GetPullPodStatus", func() {
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

	testPod := v1.Pod{
		Status: v1.PodStatus{
			Phase: v1.PodSucceeded,
		},
	}

	It("check different phases except for PodPending", func() {
		By("phase Succeeded")

		res := ip.GetPullPodStatus(&testPod)
		Expect(res).To(Equal(PullImageSuccess))

		By("phase PodFailed")
		testPod.Status.Phase = v1.PodFailed
		res = ip.GetPullPodStatus(&testPod)
		Expect(res).To(Equal(PullImageUnexpectedErr))

		By("phase PodUnknown")
		testPod.Status.Phase = v1.PodUnknown
		res = ip.GetPullPodStatus(&testPod)
		Expect(res).To(Equal(PullImageUnexpectedErr))

		By("phase PodRunning")
		testPod.Status.Phase = v1.PodRunning
		res = ip.GetPullPodStatus(&testPod)
		Expect(res).To(Equal(PullImageInProcess))
	})

	It("check container statuses for PodPending", func() {
		testPod.Status.Phase = v1.PodPending
		By("container statuses missing")
		testPod.Status.ContainerStatuses = nil
		res := ip.GetPullPodStatus(&testPod)
		Expect(res).To(Equal(PullImageInProcess))

		By("container statuses waiting is nil")
		testPod.Status.ContainerStatuses = []v1.ContainerStatus{v1.ContainerStatus{}}
		res = ip.GetPullPodStatus(&testPod)
		Expect(res).To(Equal(PullImageInProcess))

		By("container statuses waiting is not nil and pod is not one time pull pod")
		testPod.SetLabels(map[string]string{pullPodTypeLabelKey: pullPodUntilSuccess})
		testPod.Status.ContainerStatuses[0].State = v1.ContainerState{
			Waiting: &v1.ContainerStateWaiting{},
		}
		res = ip.GetPullPodStatus(&testPod)
		Expect(res).To(Equal(PullImageInProcess))

		By("container statuses waiting is not nil and pod is one time pull pod and reason is not image pull error")
		testPod.SetLabels(map[string]string{pullPodTypeLabelKey: pullPodTypeOneTime})
		testPod.Status.ContainerStatuses[0].State.Waiting.Reason = "some reason"
		res = ip.GetPullPodStatus(&testPod)
		Expect(res).To(Equal(PullImageUnexpectedErr))

		By("container statuses waiting is not nil and pod is one time pull pod and reason is ImagePullBackOff")
		testPod.Status.ContainerStatuses[0].State.Waiting.Reason = imagePullBackOffReason
		res = ip.GetPullPodStatus(&testPod)
		Expect(res).To(Equal(PullImageFailed))

		By("container statuses waiting is not nil and pod is one time pull pod and reason is errImagePullReason")
		testPod.Status.ContainerStatuses[0].State.Waiting.Reason = errImagePullReason
		res = ip.GetPullPodStatus(&testPod)
		Expect(res).To(Equal(PullImageFailed))
	})
})
