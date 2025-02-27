package pod

import (
	"context"
	"errors"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/buildsign/pod"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
)

var _ = Describe("ShouldSync", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		reg  *registry.MockRegistry
	)
	const (
		moduleName = "module-name"
		imageName  = "image-name"
		namespace  = "some-namespace"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		reg = registry.NewMockRegistry(ctrl)
	})

	It("should return false if there was not build section", func() {
		ctx := context.Background()

		mld := &api.ModuleLoaderData{}

		mgr := NewBuildManager(clnt, nil, nil, reg)

		shouldSync, err := mgr.ShouldSync(ctx, mld)

		Expect(err).ToNot(HaveOccurred())
		Expect(shouldSync).To(BeFalse())
	})

	It("should return false if image already exists", func() {
		ctx := context.Background()

		mld := &api.ModuleLoaderData{
			Name:            moduleName,
			Namespace:       namespace,
			Build:           &kmmv1beta1.Build{},
			ContainerImage:  imageName,
			ImageRepoSecret: &v1.LocalObjectReference{Name: "pull-push-secret"},
		}

		gomock.InOrder(
			reg.EXPECT().ImageExists(ctx, imageName, nil, gomock.Any()).Return(true, nil),
		)

		mgr := NewBuildManager(clnt, nil, nil, reg)

		shouldSync, err := mgr.ShouldSync(ctx, mld)

		Expect(err).ToNot(HaveOccurred())
		Expect(shouldSync).To(BeFalse())
	})

	It("should return false and an error if image check fails", func() {
		ctx := context.Background()

		mld := &api.ModuleLoaderData{
			Name:            moduleName,
			Namespace:       namespace,
			Build:           &kmmv1beta1.Build{},
			ContainerImage:  imageName,
			ImageRepoSecret: &v1.LocalObjectReference{Name: "pull-push-secret"},
		}

		gomock.InOrder(
			reg.EXPECT().ImageExists(ctx, imageName, nil, gomock.Any()).Return(false, errors.New("generic-registry-error")),
		)

		mgr := NewBuildManager(clnt, nil, nil, reg)

		shouldSync, err := mgr.ShouldSync(ctx, mld)

		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("generic-registry-error"))
		Expect(shouldSync).To(BeFalse())
	})

	It("should return true if image does not exist", func() {
		ctx := context.Background()

		mld := &api.ModuleLoaderData{
			Name:            moduleName,
			Namespace:       namespace,
			Build:           &kmmv1beta1.Build{},
			ContainerImage:  imageName,
			ImageRepoSecret: &v1.LocalObjectReference{Name: "pull-push-secret"},
		}

		gomock.InOrder(
			reg.EXPECT().ImageExists(ctx, imageName, nil, gomock.Any()).Return(false, nil),
		)

		mgr := NewBuildManager(clnt, nil, nil, reg)

		shouldSync, err := mgr.ShouldSync(ctx, mld)

		Expect(err).ToNot(HaveOccurred())
		Expect(shouldSync).To(BeTrue())
	})
})

var _ = Describe("Sync", func() {
	var (
		ctrl                    *gomock.Controller
		clnt                    *client.MockClient
		maker                   *MockMaker
		mockBuildSignPodManager *pod.MockBuildSignPodManager
		reg                     *registry.MockRegistry
	)

	const (
		imageName = "image-name"
		namespace = "some-namespace"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		maker = NewMockMaker(ctrl)
		mockBuildSignPodManager = pod.NewMockBuildSignPodManager(ctrl)
		reg = registry.NewMockRegistry(ctrl)
	})

	const (
		moduleName              = "module-name"
		kernelVersion           = "1.2.3+4"
		kernelNormalizedVersion = "1.2.3_4"
		podName                 = "some-pod"
	)

	mod := kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: moduleName},
	}

	mld := &api.ModuleLoaderData{
		Name:                    moduleName,
		Build:                   &kmmv1beta1.Build{},
		ContainerImage:          imageName,
		Owner:                   &mod,
		KernelVersion:           kernelVersion,
		KernelNormalizedVersion: kernelNormalizedVersion,
	}

	DescribeTable("should return the correct status depending on the pod status",
		func(podStatus pod.Status, expectsErr bool) {
			j := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"label key": "some label"},
					Namespace:   namespace,
					Annotations: map[string]string{constants.PodHashAnnotation: "some hash"},
				},
			}
			ctx := context.Background()

			gomock.InOrder(
				maker.EXPECT().MakePodTemplate(ctx, mld, mld.Owner, true).Return(&j, nil),
				mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mld.Name, mld.Namespace,
					kernelNormalizedVersion, pod.PodTypeBuild, mld.Owner).Return(&j, nil),
				mockBuildSignPodManager.EXPECT().IsPodChanged(&j, &j).Return(false, nil),
				mockBuildSignPodManager.EXPECT().GetPodStatus(&j).Return(podStatus, nil),
			)

			mgr := NewBuildManager(clnt, maker, mockBuildSignPodManager, reg)

			res, err := mgr.Sync(ctx, mld, true, mld.Owner)

			if expectsErr {
				Expect(err).To(HaveOccurred())
				return
			}

			Expect(res).To(Equal(podStatus))
		},
		Entry("active", pod.Status(pod.StatusInProgress), false),
		Entry("succeeded", pod.Status(pod.StatusCompleted), false),
		Entry("failed", pod.Status(pod.StatusFailed), false),
	)

	It("should return an error if there was an error creating the pod template", func() {
		ctx := context.Background()

		gomock.InOrder(
			maker.EXPECT().MakePodTemplate(ctx, mld, mld.Owner, true).Return(nil, errors.New("random error")),
		)

		mgr := NewBuildManager(clnt, maker, mockBuildSignPodManager, reg)

		Expect(
			mgr.Sync(ctx, mld, true, mld.Owner),
		).Error().To(
			HaveOccurred(),
		)
	})

	It("should return an error if there was an error creating the pod", func() {
		ctx := context.Background()
		j := v1.Pod{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "batch/v1",
				Kind:       "Pod",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
			},
		}

		gomock.InOrder(
			maker.EXPECT().MakePodTemplate(ctx, mld, mld.Owner, true).Return(&j, nil),
			mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mld.Name, mld.Namespace,
				kernelNormalizedVersion, pod.PodTypeBuild, mld.Owner).Return(nil, pod.ErrNoMatchingPod),
			mockBuildSignPodManager.EXPECT().CreatePod(ctx, &j).Return(errors.New("some error")),
		)

		mgr := NewBuildManager(clnt, maker, mockBuildSignPodManager, reg)

		Expect(
			mgr.Sync(ctx, mld, true, mld.Owner),
		).Error().To(
			HaveOccurred(),
		)
	})

	It("should create the pod if there was no error making it", func() {
		ctx := context.Background()

		j := v1.Pod{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "batch/v1",
				Kind:       "Pod",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      podName,
				Namespace: namespace,
			},
		}

		gomock.InOrder(
			maker.EXPECT().MakePodTemplate(ctx, mld, mld.Owner, true).Return(&j, nil),
			mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mld.Name, mld.Namespace,
				kernelNormalizedVersion, pod.PodTypeBuild, mld.Owner).Return(nil, pod.ErrNoMatchingPod),
			mockBuildSignPodManager.EXPECT().CreatePod(ctx, &j).Return(nil),
		)

		mgr := NewBuildManager(clnt, maker, mockBuildSignPodManager, reg)

		Expect(
			mgr.Sync(ctx, mld, true, mld.Owner),
		).To(
			Equal(pod.Status(pod.StatusCreated)),
		)
	})

	It("should delete the pod if it was edited", func() {
		ctx := context.Background()

		j := v1.Pod{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "batch/v1",
				Kind:       "Pod",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        podName,
				Namespace:   namespace,
				Annotations: map[string]string{constants.PodHashAnnotation: "some hash"},
			},
		}

		newPod := v1.Pod{
			TypeMeta: metav1.TypeMeta{
				APIVersion: "batch/v1",
				Kind:       "Pod",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:        podName,
				Namespace:   namespace,
				Annotations: map[string]string{constants.PodHashAnnotation: "new hash"},
			},
		}

		gomock.InOrder(
			maker.EXPECT().MakePodTemplate(ctx, mld, mld.Owner, true).Return(&newPod, nil),
			mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mld.Name, mld.Namespace,
				kernelNormalizedVersion, pod.PodTypeBuild, mld.Owner).Return(&j, nil),
			mockBuildSignPodManager.EXPECT().IsPodChanged(&j, &newPod).Return(true, nil),
			mockBuildSignPodManager.EXPECT().DeletePod(ctx, &j).Return(nil),
		)

		mgr := NewBuildManager(clnt, maker, mockBuildSignPodManager, reg)

		Expect(
			mgr.Sync(ctx, mld, true, mld.Owner),
		).To(
			Equal(pod.Status(pod.StatusInProgress)),
		)
	})
})

var _ = Describe("GarbageCollect", func() {
	var (
		ctrl                    *gomock.Controller
		clnt                    *client.MockClient
		maker                   *MockMaker
		mockBuildSignPodManager *pod.MockBuildSignPodManager
		reg                     *registry.MockRegistry
		mgr                     *podManager
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		maker = NewMockMaker(ctrl)
		mockBuildSignPodManager = pod.NewMockBuildSignPodManager(ctrl)
		reg = registry.NewMockRegistry(ctrl)
		mgr = NewBuildManager(clnt, maker, mockBuildSignPodManager, reg)
	})

	mod := kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{Name: "moduleName"},
	}

	DescribeTable("should return the correct error and names of the collected pods",
		func(podStatus1 v1.PodStatus, podStatus2 v1.PodStatus, expectsErr bool) {
			pod1 := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "podName1",
				},
				Status: podStatus1,
			}
			pod2 := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: "podName2",
				},
				Status: podStatus2,
			}
			expectedNames := []string{}
			if !expectsErr {
				if pod1.Status.Phase == v1.PodSucceeded {
					expectedNames = append(expectedNames, "podName1")
				}
				if pod2.Status.Phase == v1.PodSucceeded {
					expectedNames = append(expectedNames, "podName2")
				}
			}
			returnedError := fmt.Errorf("some error")
			if !expectsErr {
				returnedError = nil
			}

			mockBuildSignPodManager.EXPECT().GetModulePods(context.Background(), mod.Name,
				mod.Namespace, pod.PodTypeBuild, &mod).Return([]v1.Pod{pod1, pod2}, returnedError)
			if !expectsErr {
				if pod1.Status.Phase == v1.PodSucceeded {
					mockBuildSignPodManager.EXPECT().DeletePod(context.Background(), &pod1).Return(nil)
				}
				if pod2.Status.Phase == v1.PodSucceeded {
					mockBuildSignPodManager.EXPECT().DeletePod(context.Background(), &pod2).Return(nil)
				}
			}

			names, err := mgr.GarbageCollect(context.Background(), mod.Name, mod.Namespace, &mod)

			if expectsErr {
				Expect(err).To(HaveOccurred())
				Expect(names).To(BeNil())
			} else {
				Expect(err).NotTo(HaveOccurred())
				Expect(expectedNames).To(Equal(names))
			}
		},
		Entry("all pods succeeded", v1.PodStatus{Phase: v1.PodSucceeded}, v1.PodStatus{Phase: v1.PodSucceeded}, false),
		Entry("1 pod succeeded", v1.PodStatus{Phase: v1.PodSucceeded}, v1.PodStatus{Phase: v1.PodFailed}, false),
		Entry("0 pod succeeded", v1.PodStatus{Phase: v1.PodFailed}, v1.PodStatus{Phase: v1.PodFailed}, false),
		Entry("error occured", v1.PodStatus{Phase: v1.PodFailed}, v1.PodStatus{Phase: v1.PodFailed}, true),
	)
})
