package signpod

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/budougumi0617/cmpmock"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/registry"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
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
		ctrl      *gomock.Controller
		maker     *MockSigner
		podhelper *utils.MockPodHelper
		mgr       *signPodManager
	)

	const (
		imageName         = "image-name"
		previousImageName = "previous-image"
		namespace         = "some-namespace"

		moduleName    = "module-name"
		kernelVersion = "1.2.3"
		podName       = "some-pod"
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		maker = NewMockSigner(ctrl)
		podhelper = utils.NewMockPodHelper(ctrl)
		mgr = NewSignPodManager(nil, maker, podhelper, nil)
	})

	labels := map[string]string{"kmm.node.kubernetes.io/pod-type": "sign",
		"kmm.node.kubernetes.io/module.name":   moduleName,
		"kmm.node.kubernetes.io/target-kernel": kernelVersion,
	}

	mld := &api.ModuleLoaderData{
		Name:           moduleName,
		ContainerImage: imageName,
		Build:          &kmmv1beta1.Build{},
		Owner:          &kmmv1beta1.Module{},
		KernelVersion:  kernelVersion,
	}

	DescribeTable("should return the correct status depending on the pod status",
		func(s v1.PodStatus, podStatus utils.Status, expectsErr bool) {
			j := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"kmm.node.kubernetes.io/pod-type": "sign",
						"kmm.node.kubernetes.io/module.name":   moduleName,
						"kmm.node.kubernetes.io/target-kernel": kernelVersion,
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
						"kmm.node.kubernetes.io/target-kernel": kernelVersion,
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
				podhelper.EXPECT().PodLabels(mld.Name, kernelVersion, "sign").Return(labels),
				maker.EXPECT().MakePodTemplate(ctx, mld, labels, previousImageName, true, mld.Owner).Return(&j, nil),
				podhelper.EXPECT().GetModulePodByKernel(ctx, mld.Name, mld.Namespace, kernelVersion, utils.PodTypeSign, mld.Owner).Return(&newPod, nil),
				podhelper.EXPECT().IsPodChanged(&j, &newPod).Return(false, nil),
				podhelper.EXPECT().GetPodStatus(&newPod).Return(podStatus, poderr),
			)

			res, err := mgr.Sync(ctx, mld, previousImageName, true, mld.Owner)

			if expectsErr {
				Expect(err).To(HaveOccurred())
				return
			}

			Expect(res).To(Equal(podStatus))
		},
		Entry("active", v1.PodStatus{Phase: v1.PodRunning}, utils.Status(utils.StatusInProgress), false),
		Entry("active", v1.PodStatus{Phase: v1.PodRunning}, utils.Status(utils.StatusInProgress), false),
		Entry("succeeded", v1.PodStatus{Phase: v1.PodSucceeded}, utils.Status(utils.StatusCompleted), false),
		Entry("failed", v1.PodStatus{Phase: v1.PodFailed}, utils.Status(""), true),
	)

	It("should return an error if there was an error creating the pod template", func() {
		ctx := context.Background()

		gomock.InOrder(
			podhelper.EXPECT().PodLabels(mld.Name, kernelVersion, "sign").Return(labels),
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
			podhelper.EXPECT().PodLabels(mld.Name, kernelVersion, "sign").Return(labels),
			maker.EXPECT().MakePodTemplate(ctx, mld, labels, previousImageName, true, mld.Owner).Return(&j, nil),
			podhelper.EXPECT().GetModulePodByKernel(ctx, mld.Name, mld.Namespace, kernelVersion, utils.PodTypeSign, mld.Owner).Return(nil, errors.New("random error")),
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
			podhelper.EXPECT().PodLabels(mld.Name, kernelVersion, "sign").Return(labels),
			maker.EXPECT().MakePodTemplate(ctx, mld, labels, previousImageName, true, mld.Owner).Return(&j, nil),
			podhelper.EXPECT().GetModulePodByKernel(ctx, mld.Name, mld.Namespace, kernelVersion, utils.PodTypeSign, mld.Owner).Return(nil, utils.ErrNoMatchingPod),
			podhelper.EXPECT().CreatePod(ctx, &j).Return(errors.New("unable to create pod")),
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
			podhelper.EXPECT().PodLabels(mld.Name, kernelVersion, "sign").Return(labels),
			maker.EXPECT().MakePodTemplate(ctx, mld, labels, previousImageName, true, mld.Owner).Return(&j, nil),
			podhelper.EXPECT().GetModulePodByKernel(ctx, mld.Name, mld.Namespace, kernelVersion, utils.PodTypeSign, mld.Owner).Return(nil, utils.ErrNoMatchingPod),
			podhelper.EXPECT().CreatePod(ctx, &j).Return(nil),
		)

		Expect(
			mgr.Sync(ctx, mld, previousImageName, true, mld.Owner),
		).To(
			Equal(utils.Status(utils.StatusCreated)),
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
			podhelper.EXPECT().PodLabels(mld.Name, kernelVersion, "sign").Return(labels),
			maker.EXPECT().MakePodTemplate(ctx, mld, labels, previousImageName, true, mld.Owner).Return(&newPod, nil),
			podhelper.EXPECT().GetModulePodByKernel(ctx, mld.Name, mld.Namespace, kernelVersion, utils.PodTypeSign, mld.Owner).Return(&newPod, nil),
			podhelper.EXPECT().IsPodChanged(&newPod, &newPod).Return(true, nil),
			podhelper.EXPECT().RemoveFinalizer(ctx, &newPod, constants.GCDelayFinalizer),
			podhelper.EXPECT().DeletePod(ctx, &newPod).Return(nil),
		)

		Expect(
			mgr.Sync(ctx, mld, previousImageName, true, mld.Owner),
		).To(
			Equal(utils.Status(utils.StatusInProgress)),
		)
	})
})

var _ = Describe("GarbageCollect", func() {
	var (
		ctrl      *gomock.Controller
		clnt      *client.MockClient
		podhelper *utils.MockPodHelper
		mgr       *signPodManager
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		podhelper = utils.NewMockPodHelper(ctrl)
		mgr = NewSignPodManager(clnt, nil, podhelper, nil)
	})

	mld := api.ModuleLoaderData{
		Name:  "moduleName",
		Owner: &kmmv1beta1.Module{},
	}

	type testCase struct {
		podPhase1, podPhase2                       v1.PodPhase
		gcDelay                                    time.Duration
		expectsErr                                 bool
		resShouldContainPod1, resShouldContainPod2 bool
	}

	DescribeTable("should return the correct error and names of the collected pods",
		func(tc testCase) {
			const (
				pod1Name = "pod-1"
				pod2Name = "pod-2"
			)

			ctx := context.Background()

			pod1 := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: pod1Name},
				Status:     v1.PodStatus{Phase: tc.podPhase1},
			}
			pod2 := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{Name: pod2Name},
				Status:     v1.PodStatus{Phase: tc.podPhase2},
			}

			returnedError := fmt.Errorf("some error")
			if !tc.expectsErr {
				returnedError = nil
			}

			podList := []v1.Pod{pod1, pod2}

			calls := []any{
				podhelper.EXPECT().GetModulePods(ctx, mld.Name, mld.Namespace, utils.PodTypeSign, mld.Owner).Return(podList, returnedError),
			}

			if !tc.expectsErr {
				now := metav1.Now()

				for i := 0; i < len(podList); i++ {
					pod := &podList[i]

					if pod.Status.Phase == v1.PodSucceeded {
						c := podhelper.
							EXPECT().
							DeletePod(ctx, cmpmock.DiffEq(pod)).
							Do(func(_ context.Context, p *v1.Pod) {
								p.DeletionTimestamp = &now
								pod.DeletionTimestamp = &now
							})

						calls = append(calls, c)

						if time.Now().After(now.Add(tc.gcDelay)) {
							calls = append(
								calls,
								podhelper.EXPECT().RemoveFinalizer(ctx, cmpmock.DiffEq(pod), constants.GCDelayFinalizer),
							)
						}
					}
				}
			}

			gomock.InOrder(calls...)

			names, err := mgr.GarbageCollect(ctx, mld.Name, mld.Namespace, mld.Owner, tc.gcDelay)

			if tc.expectsErr {
				Expect(err).To(HaveOccurred())
				return
			}

			Expect(err).NotTo(HaveOccurred())

			if tc.resShouldContainPod1 {
				Expect(names).To(ContainElements(pod1Name))
			}

			if tc.resShouldContainPod2 {
				Expect(names).To(ContainElements(pod2Name))
			}
		},
		Entry(
			"all pods succeeded",
			testCase{
				podPhase1:            v1.PodSucceeded,
				podPhase2:            v1.PodSucceeded,
				resShouldContainPod1: true,
				resShouldContainPod2: true,
			},
		),
		Entry(
			"1 pod succeeded",
			testCase{
				podPhase1:            v1.PodSucceeded,
				podPhase2:            v1.PodFailed,
				resShouldContainPod1: true,
			},
		),
		Entry(
			"0 pod succeeded",
			testCase{
				podPhase1: v1.PodFailed,
				podPhase2: v1.PodFailed,
			},
		),
		Entry(
			"error occurred",
			testCase{expectsErr: true},
		),
		Entry(
			"GC delayed",
			testCase{
				podPhase1:            v1.PodSucceeded,
				podPhase2:            v1.PodSucceeded,
				gcDelay:              time.Hour,
				resShouldContainPod1: false,
				resShouldContainPod2: false,
			},
		),
	)

})
