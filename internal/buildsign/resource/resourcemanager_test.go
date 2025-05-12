package resource

import (
	"context"
	"errors"
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kubernetes-sigs/kernel-module-management/internal/buildsign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"go.uber.org/mock/gomock"
	sigclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("GetResourceByKernel", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		rm           buildsign.ResourceManager
		mockCombiner *module.MockCombiner
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockCombiner = module.NewMockCombiner(ctrl)
		rm = NewResourceManager(clnt, mockCombiner, scheme)
	})

	It("should return only one pod", func() {
		ctx := context.Background()

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName", Namespace: "moduleNamespace"},
		}
		j := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "modulePod", Namespace: "moduleNamespace"},
		}

		err := controllerutil.SetControllerReference(&mod, &j, scheme)
		Expect(err).NotTo(HaveOccurred())

		labels := map[string]string{
			constants.ModuleNameLabel:    "moduleName",
			constants.TargetKernelTarget: "targetKernel",
			constants.ResourceType:       "resourceType",
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace("moduleNamespace"),
		}

		clnt.EXPECT().List(ctx, gomock.Any(), opts).DoAndReturn(
			func(_ interface{}, list *v1.PodList, _ ...interface{}) error {
				list.Items = []v1.Pod{j}
				return nil
			},
		)

		pod, err := rm.GetResourceByKernel(ctx, mod.Name, mod.Namespace, "targetKernel", "resourceType", &mod)

		Expect(pod).To(Equal(&j))
		Expect(err).NotTo(HaveOccurred())
	})

	It("failure to fetch pods", func() {
		ctx := context.Background()
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName", Namespace: "moduleNamespace"},
		}

		labels := map[string]string{
			constants.ModuleNameLabel:    "moduleName",
			constants.TargetKernelTarget: "targetKernel",
			constants.ResourceType:       "resourceType",
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace("moduleNamespace"),
		}
		podList := v1.PodList{}

		clnt.EXPECT().List(ctx, &podList, opts).Return(errors.New("random error"))

		_, err := rm.GetResourceByKernel(ctx, mod.Name, mod.Namespace, "targetKernel", "resourceType", &mod)

		Expect(err).To(HaveOccurred())
	})

	It("should fails if more then 1 pod exists", func() {
		ctx := context.Background()

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName", Namespace: "moduleNamespace"},
		}

		j1 := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "modulePod1", Namespace: "moduleNamespace"},
		}
		j2 := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "modulePod2", Namespace: "moduleNamespace"},
		}

		err := controllerutil.SetControllerReference(&mod, &j1, scheme)
		Expect(err).NotTo(HaveOccurred())
		err = controllerutil.SetControllerReference(&mod, &j2, scheme)
		Expect(err).NotTo(HaveOccurred())

		labels := map[string]string{
			constants.ModuleNameLabel:    "moduleName",
			constants.TargetKernelTarget: "targetKernel",
			constants.ResourceType:       "resourceType",
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace("moduleNamespace"),
		}

		clnt.EXPECT().List(ctx, gomock.Any(), opts).DoAndReturn(
			func(_ interface{}, list *v1.PodList, _ ...interface{}) error {
				list.Items = []v1.Pod{j1, j2}
				return nil
			},
		)

		_, err = rm.GetResourceByKernel(ctx, mod.Name, mod.Namespace, "targetKernel", "resourceType", &mod)

		Expect(err).To(HaveOccurred())
	})
	It("more then 1 pod exists, but only one is owned by the module", func() {
		ctx := context.Background()

		mod := kmmv1beta1.Module{
			TypeMeta:   metav1.TypeMeta{Kind: "some kind", APIVersion: "some version"},
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName", Namespace: "moduleNamespace", UID: "some uuid"},
		}

		anotherMod := kmmv1beta1.Module{
			TypeMeta:   metav1.TypeMeta{Kind: "some kind", APIVersion: "some version"},
			ObjectMeta: metav1.ObjectMeta{Name: "anotherModuleName", Namespace: "moduleNamespace", UID: "another uuid"},
		}

		j1 := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "modulePod1", Namespace: "moduleNamespace"},
		}
		j2 := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "modulePod2", Namespace: "moduleNamespace"},
		}

		err := controllerutil.SetControllerReference(&mod, &j1, scheme)
		Expect(err).NotTo(HaveOccurred())
		err = controllerutil.SetControllerReference(&anotherMod, &j2, scheme)
		Expect(err).NotTo(HaveOccurred())

		labels := map[string]string{
			constants.ModuleNameLabel:    "moduleName",
			constants.TargetKernelTarget: "targetKernel",
			constants.ResourceType:       "resourceType",
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace("moduleNamespace"),
		}

		clnt.EXPECT().List(ctx, gomock.Any(), opts).DoAndReturn(
			func(_ interface{}, list *v1.PodList, _ ...interface{}) error {
				list.Items = []v1.Pod{j1, j2}
				return nil
			},
		)

		pod, err := rm.GetResourceByKernel(ctx, mod.Name, mod.Namespace, "targetKernel", "resourceType", &mod)

		Expect(err).NotTo(HaveOccurred())
		Expect(pod).To(Equal(&j1))
	})
})

var _ = Describe("GetModuleResources", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		rm           buildsign.ResourceManager
		mockCombiner *module.MockCombiner
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		rm = NewResourceManager(clnt, mockCombiner, scheme)
	})

	It("return all found pods", func() {
		ctx := context.Background()

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName", Namespace: "moduleNamespace"},
		}

		j1 := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "modulePod1", Namespace: "moduleNamespace"},
		}
		j2 := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "modulePod12", Namespace: "moduleNamespace"},
		}
		err := controllerutil.SetControllerReference(&mod, &j1, scheme)
		Expect(err).NotTo(HaveOccurred())
		err = controllerutil.SetControllerReference(&mod, &j2, scheme)
		Expect(err).NotTo(HaveOccurred())

		labels := map[string]string{
			constants.ModuleNameLabel: "moduleName",
			constants.ResourceType:    "resourceType",
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace("moduleNamespace"),
		}

		clnt.EXPECT().List(ctx, gomock.Any(), opts).DoAndReturn(
			func(_ interface{}, list *v1.PodList, _ ...interface{}) error {
				list.Items = []v1.Pod{j1, j2}
				return nil
			},
		)

		pods, err := rm.GetModuleResources(ctx, mod.Name, mod.Namespace, "resourceType", &mod)

		Expect(err).NotTo(HaveOccurred())
		Expect(len(pods)).To(Equal(2))
	})

	It("error flow", func() {
		ctx := context.Background()

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName", Namespace: "moduleNamespace"},
		}

		labels := map[string]string{
			constants.ModuleNameLabel: "moduleName",
			constants.ResourceType:    "resourceType",
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace("moduleNamespace"),
		}

		clnt.EXPECT().List(ctx, gomock.Any(), opts).Return(fmt.Errorf("some error"))

		_, err := rm.GetModuleResources(ctx, mod.Name, mod.Namespace, "resourceType", &mod)

		Expect(err).To(HaveOccurred())
	})

	It("zero pods found", func() {
		ctx := context.Background()

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName", Namespace: "moduleNamespace"},
		}

		labels := map[string]string{
			constants.ModuleNameLabel: "moduleName",
			constants.ResourceType:    "resourceType",
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace("moduleNamespace"),
		}

		clnt.EXPECT().List(ctx, gomock.Any(), opts).DoAndReturn(
			func(_ interface{}, list *v1.PodList, _ ...interface{}) error {
				list.Items = []v1.Pod{}
				return nil
			},
		)

		pods, err := rm.GetModuleResources(ctx, mod.Name, mod.Namespace, "resourceType", &mod)

		Expect(err).NotTo(HaveOccurred())
		Expect(len(pods)).To(Equal(0))
	})
})

var _ = Describe("DeleteResource", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		rm           buildsign.ResourceManager
		mockCombiner *module.MockCombiner
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		rm = NewResourceManager(clnt, mockCombiner, scheme)
	})

	It("good flow", func() {
		ctx := context.Background()

		pod := v1.Pod{}
		opts := []sigclient.DeleteOption{
			sigclient.PropagationPolicy(metav1.DeletePropagationBackground),
		}
		clnt.EXPECT().Delete(ctx, &pod, opts).Return(nil)

		err := rm.DeleteResource(ctx, &pod)

		Expect(err).NotTo(HaveOccurred())

	})

	It("error flow", func() {
		ctx := context.Background()

		pod := v1.Pod{}
		opts := []sigclient.DeleteOption{
			sigclient.PropagationPolicy(metav1.DeletePropagationBackground),
		}
		clnt.EXPECT().Delete(ctx, &pod, opts).Return(errors.New("random error"))

		err := rm.DeleteResource(ctx, &pod)

		Expect(err).To(HaveOccurred())

	})
})

var _ = Describe("CreateResource", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		rm           buildsign.ResourceManager
		mockCombiner *module.MockCombiner
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		rm = NewResourceManager(clnt, mockCombiner, scheme)
	})

	It("good flow", func() {
		ctx := context.Background()

		pod := v1.Pod{}
		clnt.EXPECT().Create(ctx, &pod).Return(nil)

		err := rm.CreateResource(ctx, &pod)

		Expect(err).NotTo(HaveOccurred())

	})

	It("error flow", func() {
		ctx := context.Background()

		pod := v1.Pod{}
		clnt.EXPECT().Create(ctx, &pod).Return(errors.New("random error"))

		err := rm.CreateResource(ctx, &pod)

		Expect(err).To(HaveOccurred())

	})
})

var _ = Describe("PodStatus", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		rm           buildsign.ResourceManager
		mockCombiner *module.MockCombiner
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		rm = NewResourceManager(clnt, mockCombiner, scheme)
	})

	DescribeTable("should return the correct status depending on the pod status",
		func(s *v1.Pod, podStatus buildsign.Status, expectsErr bool) {

			res, err := rm.GetResourceStatus(s)
			if expectsErr {
				Expect(err).To(HaveOccurred())
				return
			}

			Expect(res).To(Equal(podStatus))
		},
		Entry("succeeded", &v1.Pod{Status: v1.PodStatus{Phase: v1.PodSucceeded}}, buildsign.StatusCompleted, false),
		Entry("in progress", &v1.Pod{Status: v1.PodStatus{Phase: v1.PodRunning}}, buildsign.StatusInProgress, false),
		Entry("pending", &v1.Pod{Status: v1.PodStatus{Phase: v1.PodPending}}, buildsign.StatusInProgress, false),
		Entry("Failed", &v1.Pod{Status: v1.PodStatus{Phase: v1.PodFailed}}, buildsign.StatusFailed, false),
		Entry("Unknown", &v1.Pod{Status: v1.PodStatus{Phase: v1.PodUnknown}}, buildsign.Status(""), true),
	)
})

var _ = Describe("IsPodChnaged", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		rm           buildsign.ResourceManager
		mockCombiner *module.MockCombiner
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		rm = NewResourceManager(clnt, mockCombiner, scheme)
	})

	DescribeTable("should detect if a pod has changed",
		func(annotation map[string]string, expectchanged bool, expectsErr bool) {
			existingPod := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: annotation,
				},
			}
			newPod := v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{constants.ResourceHashAnnotation: "some hash"},
				},
			}
			fmt.Println(existingPod.GetAnnotations())

			changed, err := rm.IsResourceChanged(&existingPod, &newPod)

			if expectsErr {
				Expect(err).To(HaveOccurred())
				return
			}
			Expect(expectchanged).To(Equal(changed))
		},

		Entry("should error if pod has no annotations", nil, false, true),
		Entry("should return true if pod has changed", map[string]string{constants.ResourceHashAnnotation: "some other hash"}, true, false),
		Entry("should return false is pod has not changed ", map[string]string{constants.ResourceHashAnnotation: "some hash"}, false, false),
	)
})
