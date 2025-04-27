package signpod

import (
	"context"
	"errors"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/buildsign/pod"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("ShouldSync", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		reg  *registry.MockRegistry
		mgr  *signPodManager
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
		mgr = NewSignPodManager(clnt, nil, nil, reg)
	})

	It("should return false if there was not sign section", func() {
		ctx := context.Background()

		mld := &api.ModuleLoaderData{}
		shouldSync, err := mgr.ShouldSync(ctx, mld)

		Expect(err).ToNot(HaveOccurred())
		Expect(shouldSync).To(BeFalse())
	})

	It("should return false if image already exists", func() {
		ctx := context.Background()

		mld := &api.ModuleLoaderData{
			Name:            moduleName,
			Namespace:       namespace,
			ImageRepoSecret: &v1.LocalObjectReference{Name: "pull-push-secret"},
			ContainerImage:  imageName,
			Sign:            &kmmv1beta1.Sign{},
		}

		gomock.InOrder(
			reg.EXPECT().ImageExists(ctx, imageName, nil, gomock.Any()).Return(true, nil),
		)

		shouldSync, err := mgr.ShouldSync(ctx, mld)

		Expect(err).ToNot(HaveOccurred())
		Expect(shouldSync).To(BeFalse())
	})

	It("should return false and an error if image check fails", func() {
		ctx := context.Background()

		mld := &api.ModuleLoaderData{
			Name:            moduleName,
			Namespace:       namespace,
			ImageRepoSecret: &v1.LocalObjectReference{Name: "pull-push-secret"},
			ContainerImage:  imageName,
			Sign:            &kmmv1beta1.Sign{},
		}

		gomock.InOrder(
			reg.EXPECT().ImageExists(ctx, imageName, nil, gomock.Any()).Return(false, errors.New("generic-registry-error")),
		)

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
			ImageRepoSecret: &v1.LocalObjectReference{Name: "pull-push-secret"},
			ContainerImage:  imageName,
			Sign:            &kmmv1beta1.Sign{},
		}

		gomock.InOrder(
			reg.EXPECT().ImageExists(ctx, imageName, nil, gomock.Any()).Return(false, nil),
		)

		shouldSync, err := mgr.ShouldSync(ctx, mld)

		Expect(err).ToNot(HaveOccurred())
		Expect(shouldSync).To(BeTrue())
	})
})

var _ = Describe("Sync", func() {
	var (
		ctrl                    *gomock.Controller
		maker                   *MockSigner
		mockBuildSignPodManager *pod.MockBuildSignPodManager
		mgr                     *signPodManager
	)

	const (
		imageName         = "image-name"
		previousImageName = "previous-image"
		namespace         = "some-namespace"

		moduleName              = "module-name"
		kernelVersion           = "1.2.3+4"
		kernelNormalizedVersion = "1.2.3_4"
		podName                 = "some-pod"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		maker = NewMockSigner(ctrl)
		mockBuildSignPodManager = pod.NewMockBuildSignPodManager(ctrl)
		mgr = NewSignPodManager(nil, maker, mockBuildSignPodManager, nil)
	})

	labels := map[string]string{"kmm.node.kubernetes.io/pod-type": "sign",
		"kmm.node.kubernetes.io/module.name":   moduleName,
		"kmm.node.kubernetes.io/target-kernel": kernelVersion,
	}

	mld := &api.ModuleLoaderData{
		Name:                    moduleName,
		ContainerImage:          imageName,
		Build:                   &kmmv1beta1.Build{},
		Owner:                   &kmmv1beta1.Module{},
		KernelVersion:           kernelVersion,
		KernelNormalizedVersion: kernelNormalizedVersion,
	}

	DescribeTable("should return the correct status depending on the pod status",
		func(s v1.PodStatus, podStatus pod.Status, expectsErr bool) {
			j := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"kmm.node.kubernetes.io/pod-type": "sign",
						"kmm.node.kubernetes.io/module.name":   moduleName,
						"kmm.node.kubernetes.io/target-kernel": kernelNormalizedVersion,
					},
					Namespace:   namespace,
					Annotations: map[string]string{constants.PodHashAnnotation: "some hash"},
				},
				Status: s,
			}
			newPod := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"kmm.node.kubernetes.io/pod-type": "sign",
						"kmm.node.kubernetes.io/module.name":   moduleName,
						"kmm.node.kubernetes.io/target-kernel": kernelNormalizedVersion,
					},
					Namespace:   namespace,
					Annotations: map[string]string{constants.PodHashAnnotation: "some hash"},
				},
				Status: s,
			}
			ctx := context.Background()

			var poderr error
			if expectsErr {
				poderr = errors.New("random error")
			}

			gomock.InOrder(
				mockBuildSignPodManager.EXPECT().PodLabels(mld.Name, kernelNormalizedVersion, "sign").Return(labels),
				maker.EXPECT().MakePodTemplate(ctx, mld, labels, previousImageName, true, mld.Owner).Return(&j, nil),
				mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mld.Name, mld.Namespace,
					kernelNormalizedVersion, PodTypeSign, mld.Owner).Return(&newPod, nil),
				mockBuildSignPodManager.EXPECT().IsPodChanged(&j, &newPod).Return(false, nil),
				mockBuildSignPodManager.EXPECT().GetPodStatus(&newPod).Return(podStatus, poderr),
			)

			res, err := mgr.Sync(ctx, mld, previousImageName, true, mld.Owner)

			if expectsErr {
				Expect(err).To(HaveOccurred())
				return
			}

			Expect(res).To(Equal(podStatus))
		},
		Entry("active", v1.PodStatus{Phase: v1.PodRunning}, pod.Status(pod.StatusInProgress), false),
		Entry("active", v1.PodStatus{Phase: v1.PodRunning}, pod.Status(pod.StatusInProgress), false),
		Entry("succeeded", v1.PodStatus{Phase: v1.PodSucceeded}, pod.Status(pod.StatusCompleted), false),
		Entry("failed", v1.PodStatus{Phase: v1.PodFailed}, pod.Status(""), true),
	)

	It("should return an error if there was an error creating the pod template", func() {
		ctx := context.Background()

		gomock.InOrder(
			mockBuildSignPodManager.EXPECT().PodLabels(mld.Name, kernelNormalizedVersion, "sign").Return(labels),
			maker.EXPECT().MakePodTemplate(ctx, mld, labels, previousImageName, true, mld.Owner).
				Return(nil, errors.New("random error")),
		)

		Expect(
			mgr.Sync(ctx, mld, previousImageName, true, mld.Owner),
		).Error().To(
			HaveOccurred(),
		)
	})

	It("should return an error if there was an error getting running pods", func() {
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
			mockBuildSignPodManager.EXPECT().PodLabels(mld.Name, kernelNormalizedVersion, "sign").Return(labels),
			maker.EXPECT().MakePodTemplate(ctx, mld, labels, previousImageName, true, mld.Owner).Return(&j, nil),
			mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mld.Name, mld.Namespace,
				kernelNormalizedVersion, PodTypeSign, mld.Owner).Return(nil, errors.New("random error")),
		)

		Expect(
			mgr.Sync(ctx, mld, previousImageName, true, mld.Owner),
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
			mockBuildSignPodManager.EXPECT().PodLabels(mld.Name, kernelNormalizedVersion, "sign").Return(labels),
			maker.EXPECT().MakePodTemplate(ctx, mld, labels, previousImageName, true, mld.Owner).Return(&j, nil),
			mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mld.Name, mld.Namespace,
				kernelNormalizedVersion, PodTypeSign, mld.Owner).Return(nil, pod.ErrNoMatchingPod),
			mockBuildSignPodManager.EXPECT().CreatePod(ctx, &j).Return(errors.New("unable to create pod")),
		)

		Expect(
			mgr.Sync(ctx, mld, previousImageName, true, mld.Owner),
		).Error().To(
			HaveOccurred(),
		)
	})

	It("should create the pod and return without error if it doesn't exist", func() {
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
			mockBuildSignPodManager.EXPECT().PodLabels(mld.Name, kernelNormalizedVersion, "sign").Return(labels),
			maker.EXPECT().MakePodTemplate(ctx, mld, labels, previousImageName, true, mld.Owner).Return(&j, nil),
			mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mld.Name, mld.Namespace,
				kernelNormalizedVersion, PodTypeSign, mld.Owner).Return(nil, pod.ErrNoMatchingPod),
			mockBuildSignPodManager.EXPECT().CreatePod(ctx, &j).Return(nil),
		)

		Expect(
			mgr.Sync(ctx, mld, previousImageName, true, mld.Owner),
		).To(
			Equal(pod.Status(pod.StatusCreated)),
		)
	})

	It("should delete the pod if it was edited", func() {
		ctx := context.Background()

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
			mockBuildSignPodManager.EXPECT().PodLabels(mld.Name, kernelNormalizedVersion, "sign").Return(labels),
			maker.EXPECT().MakePodTemplate(ctx, mld, labels, previousImageName, true, mld.Owner).Return(&newPod, nil),
			mockBuildSignPodManager.EXPECT().GetModulePodByKernel(ctx, mld.Name, mld.Namespace,
				kernelNormalizedVersion, PodTypeSign, mld.Owner).Return(&newPod, nil),
			mockBuildSignPodManager.EXPECT().IsPodChanged(&newPod, &newPod).Return(true, nil),
			mockBuildSignPodManager.EXPECT().DeletePod(ctx, &newPod).Return(nil),
		)

		Expect(
			mgr.Sync(ctx, mld, previousImageName, true, mld.Owner),
		).To(
			Equal(pod.Status(pod.StatusInProgress)),
		)
	})
})

var _ = Describe("GarbageCollect", func() {
	var (
		ctrl                    *gomock.Controller
		clnt                    *client.MockClient
		mockBuildSignPodManager *pod.MockBuildSignPodManager
		mgr                     *signPodManager
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockBuildSignPodManager = pod.NewMockBuildSignPodManager(ctrl)
		mgr = NewSignPodManager(clnt, nil, mockBuildSignPodManager, nil)
	})

	mld := api.ModuleLoaderData{
		Name:  "moduleName",
		Owner: &kmmv1beta1.Module{},
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

			mockBuildSignPodManager.EXPECT().GetModulePods(context.Background(), mld.Name, mld.Namespace,
				PodTypeSign, mld.Owner).Return([]v1.Pod{pod1, pod2}, returnedError)
			if !expectsErr {
				if pod1.Status.Phase == v1.PodSucceeded {
					mockBuildSignPodManager.EXPECT().DeletePod(context.Background(), &pod1).Return(nil)
				}
				if pod2.Status.Phase == v1.PodSucceeded {
					mockBuildSignPodManager.EXPECT().DeletePod(context.Background(), &pod2).Return(nil)
				}
			}

			names, err := mgr.GarbageCollect(context.Background(), mld.Name, mld.Namespace, mld.Owner)

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
