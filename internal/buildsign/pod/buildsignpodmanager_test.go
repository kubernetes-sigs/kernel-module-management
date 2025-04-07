package pod

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/go-cmp/cmp"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	"go.uber.org/mock/gomock"
	sigclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("PodLabels", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		mockCombiner *module.MockCombiner
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockCombiner = module.NewMockCombiner(ctrl)
	})

	It("get pod labels", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName"},
		}
		mgr := NewBuildSignPodManager(clnt, mockCombiner, scheme)
		labels := mgr.PodLabels(mod.Name, "targetKernel", "podType")

		expected := map[string]string{
			"app.kubernetes.io/name":      "kmm",
			"app.kubernetes.io/component": "podType",
			"app.kubernetes.io/part-of":   "kmm",
			constants.ModuleNameLabel:     "moduleName",
			constants.TargetKernelTarget:  "targetKernel",
			constants.PodType:             "podType",
		}

		Expect(labels).To(Equal(expected))
	})
})

var _ = Describe("GetModulePodByKernel", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		bspm         BuildSignPodManager
		mockCombiner *module.MockCombiner
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockCombiner = module.NewMockCombiner(ctrl)
		bspm = NewBuildSignPodManager(clnt, mockCombiner, scheme)
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
			constants.PodType:            "podType",
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

		pod, err := bspm.GetModulePodByKernel(ctx, mod.Name, mod.Namespace, "targetKernel", "podType", &mod)

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
			constants.PodType:            "podType",
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace("moduleNamespace"),
		}
		podList := v1.PodList{}

		clnt.EXPECT().List(ctx, &podList, opts).Return(errors.New("random error"))

		_, err := bspm.GetModulePodByKernel(ctx, mod.Name, mod.Namespace, "targetKernel", "podType", &mod)

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
			constants.PodType:            "podType",
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

		_, err = bspm.GetModulePodByKernel(ctx, mod.Name, mod.Namespace, "targetKernel", "podType", &mod)

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
			constants.PodType:            "podType",
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

		pod, err := bspm.GetModulePodByKernel(ctx, mod.Name, mod.Namespace, "targetKernel", "podType", &mod)

		Expect(err).NotTo(HaveOccurred())
		Expect(pod).To(Equal(&j1))
	})
})

var _ = Describe("GetModulePods", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		bspm         BuildSignPodManager
		mockCombiner *module.MockCombiner
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		bspm = NewBuildSignPodManager(clnt, mockCombiner, scheme)
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
			constants.PodType:         "podType",
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

		pods, err := bspm.GetModulePods(ctx, mod.Name, mod.Namespace, "podType", &mod)

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
			constants.PodType:         "podType",
		}

		opts := []sigclient.ListOption{
			sigclient.MatchingLabels(labels),
			sigclient.InNamespace("moduleNamespace"),
		}

		clnt.EXPECT().List(ctx, gomock.Any(), opts).Return(fmt.Errorf("some error"))

		_, err := bspm.GetModulePods(ctx, mod.Name, mod.Namespace, "podType", &mod)

		Expect(err).To(HaveOccurred())
	})

	It("zero pods found", func() {
		ctx := context.Background()

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName", Namespace: "moduleNamespace"},
		}

		labels := map[string]string{
			constants.ModuleNameLabel: "moduleName",
			constants.PodType:         "podType",
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

		pods, err := bspm.GetModulePods(ctx, mod.Name, mod.Namespace, "podType", &mod)

		Expect(err).NotTo(HaveOccurred())
		Expect(len(pods)).To(Equal(0))
	})
})

var _ = Describe("DeletePod", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		bspm         BuildSignPodManager
		mockCombiner *module.MockCombiner
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		bspm = NewBuildSignPodManager(clnt, mockCombiner, scheme)
	})

	It("good flow", func() {
		ctx := context.Background()

		pod := v1.Pod{}
		opts := []sigclient.DeleteOption{
			sigclient.PropagationPolicy(metav1.DeletePropagationBackground),
		}
		clnt.EXPECT().Delete(ctx, &pod, opts).Return(nil)

		err := bspm.DeletePod(ctx, &pod)

		Expect(err).NotTo(HaveOccurred())

	})

	It("error flow", func() {
		ctx := context.Background()

		pod := v1.Pod{}
		opts := []sigclient.DeleteOption{
			sigclient.PropagationPolicy(metav1.DeletePropagationBackground),
		}
		clnt.EXPECT().Delete(ctx, &pod, opts).Return(errors.New("random error"))

		err := bspm.DeletePod(ctx, &pod)

		Expect(err).To(HaveOccurred())

	})
})

var _ = Describe("CreatePod", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		bspm         BuildSignPodManager
		mockCombiner *module.MockCombiner
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		bspm = NewBuildSignPodManager(clnt, mockCombiner, scheme)
	})

	It("good flow", func() {
		ctx := context.Background()

		pod := v1.Pod{}
		clnt.EXPECT().Create(ctx, &pod).Return(nil)

		err := bspm.CreatePod(ctx, &pod)

		Expect(err).NotTo(HaveOccurred())

	})

	It("error flow", func() {
		ctx := context.Background()

		pod := v1.Pod{}
		clnt.EXPECT().Create(ctx, &pod).Return(errors.New("random error"))

		err := bspm.CreatePod(ctx, &pod)

		Expect(err).To(HaveOccurred())

	})
})

var _ = Describe("PodStatus", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		bspm         BuildSignPodManager
		mockCombiner *module.MockCombiner
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		bspm = NewBuildSignPodManager(clnt, mockCombiner, scheme)
	})

	DescribeTable("should return the correct status depending on the pod status",
		func(s *v1.Pod, podStatus Status, expectsErr bool) {

			res, err := bspm.GetPodStatus(s)
			if expectsErr {
				Expect(err).To(HaveOccurred())
				return
			}

			Expect(res).To(Equal(podStatus))
		},
		Entry("succeeded", &v1.Pod{Status: v1.PodStatus{Phase: v1.PodSucceeded}}, StatusCompleted, false),
		Entry("in progress", &v1.Pod{Status: v1.PodStatus{Phase: v1.PodRunning}}, StatusInProgress, false),
		Entry("pending", &v1.Pod{Status: v1.PodStatus{Phase: v1.PodPending}}, StatusInProgress, false),
		Entry("Failed", &v1.Pod{Status: v1.PodStatus{Phase: v1.PodFailed}}, StatusFailed, false),
		Entry("Unknown", &v1.Pod{Status: v1.PodStatus{Phase: v1.PodUnknown}}, Status(""), true),
	)
})

var _ = Describe("IsPodChnaged", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		bspm         BuildSignPodManager
		mockCombiner *module.MockCombiner
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		bspm = NewBuildSignPodManager(clnt, mockCombiner, scheme)
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
					Annotations: map[string]string{constants.PodHashAnnotation: "some hash"},
				},
			}
			fmt.Println(existingPod.GetAnnotations())

			changed, err := bspm.IsPodChanged(&existingPod, &newPod)

			if expectsErr {
				Expect(err).To(HaveOccurred())
				return
			}
			Expect(expectchanged).To(Equal(changed))
		},

		Entry("should error if pod has no annotations", nil, false, true),
		Entry("should return true if pod has changed", map[string]string{constants.PodHashAnnotation: "some other hash"}, true, false),
		Entry("should return false is pod has not changed ", map[string]string{constants.PodHashAnnotation: "some hash"}, false, false),
	)
})

var _ = Describe("MakeBuildResourceTemplate", func() {
	const (
		image                   = "my.registry/my/image"
		dockerfile              = "FROM test"
		kanikoImage             = "some-kaniko-image:some-tag"
		kernelVersion           = "1.2.3+4"
		kernelNormalizedVersion = "1.2.3_4"
		moduleName              = "module-name"
		namespace               = "some-namespace"
		relatedImageEnvVar      = "RELATED_IMAGE_BUILD"
	)

	var (
		ctrl                *gomock.Controller
		clnt                *client.MockClient
		mc                  *module.MockCombiner
		buildSignPodManager BuildSignPodManager
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mc = module.NewMockCombiner(ctrl)
		buildSignPodManager = NewBuildSignPodManager(clnt, mc, scheme)
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	mod := kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{
			Name:      moduleName,
			Namespace: namespace,
		},
	}

	defaultBuildArgs := []kmmv1beta1.BuildArg{
		{Name: "KERNEL_VERSION", Value: kernelVersion},
		{Name: "KERNEL_FULL_VERSION", Value: kernelVersion},
		{Name: "MOD_NAME", Value: moduleName},
		{Name: "MOD_NAMESPACE", Value: namespace},
	}

	buildArgs := []kmmv1beta1.BuildArg{
		{Name: "name1", Value: "value1"},
	}

	dockerfileConfigMap := v1.LocalObjectReference{Name: "configMapName"}
	dockerfileCMData := map[string]string{constants.DockerfileCMKey: dockerfile}

	DescribeTable("should set fields correctly", func(
		buildSecrets []v1.LocalObjectReference,
		imagePullSecret *v1.LocalObjectReference,
		useBuildSelector bool) {
		GinkgoT().Setenv(relatedImageEnvVar, kanikoImage)

		ctx := context.Background()
		nodeSelector := map[string]string{"arch": "x64"}

		mld := api.ModuleLoaderData{
			Owner:     &mod,
			Name:      mod.Name,
			Namespace: mod.Namespace,
			Build: &kmmv1beta1.Build{
				BuildArgs:           buildArgs,
				DockerfileConfigMap: &dockerfileConfigMap,
			},
			ContainerImage:          image,
			RegistryTLS:             &kmmv1beta1.TLSOptions{},
			Selector:                nodeSelector,
			KernelVersion:           kernelVersion,
			KernelNormalizedVersion: kernelNormalizedVersion,
		}

		if useBuildSelector {
			mld.Selector = nil
			mld.Build.Selector = nodeSelector
		}

		expected := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: mld.Name + "-build-",
				Namespace:    namespace,
				Labels:       buildSignPodManager.PodLabels(mld.Name, mld.KernelNormalizedVersion, PodTypeBuild),
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         "kmm.sigs.x-k8s.io/v1beta1",
						Kind:               "Module",
						Name:               moduleName,
						Controller:         ptr.To(true),
						BlockOwnerDeletion: ptr.To(true),
					},
				},
				Finalizers: []string{constants.GCDelayFinalizer, constants.JobEventFinalizer},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Args: []string{
							"--destination", image,
							"--build-arg", "name1=value1",
							"--build-arg", "KERNEL_VERSION=" + kernelVersion,
							"--build-arg", "KERNEL_FULL_VERSION=" + kernelVersion,
							"--build-arg", "MOD_NAME=" + moduleName,
							"--build-arg", "MOD_NAMESPACE=" + namespace,
						},
						Name:  "kaniko",
						Image: kanikoImage,
						VolumeMounts: []v1.VolumeMount{
							{
								Name:      "dockerfile",
								ReadOnly:  true,
								MountPath: "/workspace",
							},
						},
					},
				},
				NodeSelector:  nodeSelector,
				RestartPolicy: v1.RestartPolicyNever,
				Volumes: []v1.Volume{
					{
						Name: "dockerfile",
						VolumeSource: v1.VolumeSource{
							ConfigMap: &v1.ConfigMapVolumeSource{
								LocalObjectReference: dockerfileConfigMap,
								Items: []v1.KeyToPath{
									{
										Key:  constants.DockerfileCMKey,
										Path: "Dockerfile",
									},
								},
							},
						},
					},
				},
			},
		}

		if imagePullSecret != nil {
			mld.ImageRepoSecret = imagePullSecret

			expected.Spec.Containers[0].VolumeMounts =
				append(expected.Spec.Containers[0].VolumeMounts,
					v1.VolumeMount{
						Name:      "secret-pull-push-secret",
						ReadOnly:  true,
						MountPath: "/kaniko/.docker",
					},
				)

			expected.Spec.Volumes =
				append(expected.Spec.Volumes,
					v1.Volume{
						Name: "secret-pull-push-secret",
						VolumeSource: v1.VolumeSource{
							Secret: &v1.SecretVolumeSource{
								SecretName: "pull-push-secret",
								Items: []v1.KeyToPath{
									{
										Key:  v1.DockerConfigJsonKey,
										Path: "config.json",
									},
								},
							},
						},
					},
				)
		}

		if len(buildSecrets) > 0 {

			mld.Build.Secrets = buildSecrets

			expected.Spec.Containers[0].VolumeMounts =
				append(expected.Spec.Containers[0].VolumeMounts,
					v1.VolumeMount{
						Name:      "secret-s1",
						ReadOnly:  true,
						MountPath: "/run/secrets/s1",
					},
				)

			expected.Spec.Volumes =
				append(expected.Spec.Volumes,
					v1.Volume{
						Name: "secret-s1",
						VolumeSource: v1.VolumeSource{
							Secret: &v1.SecretVolumeSource{
								SecretName: "s1",
							},
						},
					},
				)
		}
		hash, err := getBuildHashValue(&expected.Spec, dockerfile)
		Expect(err).NotTo(HaveOccurred())
		annotations := map[string]string{constants.PodHashAnnotation: fmt.Sprintf("%d", hash)}
		expected.SetAnnotations(annotations)

		gomock.InOrder(
			mc.EXPECT().ApplyBuildArgOverrides(buildArgs, defaultBuildArgs).Return(append(slices.Clone(buildArgs), defaultBuildArgs...)),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: dockerfileConfigMap.Name, Namespace: mld.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, cm *v1.ConfigMap, _ ...ctrlclient.GetOption) error {
					cm.Data = dockerfileCMData
					return nil
				},
			),
		)

		actual, err := buildSignPodManager.MakeBuildResourceTemplate(ctx, &mld, mld.Owner, true)
		Expect(err).NotTo(HaveOccurred())

		Expect(
			cmp.Diff(expected, actual),
		).To(
			BeEmpty(),
		)
	},
		Entry(
			"no secrets at all",
			[]v1.LocalObjectReference{},
			nil,
			false,
		),
		Entry(
			"no secrets at all with build.Selector property",
			[]v1.LocalObjectReference{},
			nil,
			true,
		),
		Entry(
			"only buidSecrets",
			[]v1.LocalObjectReference{{Name: "s1"}},
			nil,
			false,
		),
		Entry(
			"only imagePullSecrets",
			[]v1.LocalObjectReference{},
			&v1.LocalObjectReference{Name: "pull-push-secret"},
			false,
		),
		Entry(
			"buildSecrets and imagePullSecrets",
			[]v1.LocalObjectReference{{Name: "s1"}},
			&v1.LocalObjectReference{Name: "pull-push-secret"},
			false,
		),
	)

	DescribeTable("should set correct kaniko flags", func(tls *kmmv1beta1.TLSOptions, b *kmmv1beta1.Build, kanikoFlag string, pushImage bool) {
		ctx := context.Background()

		mld := api.ModuleLoaderData{
			Build:                   b,
			ContainerImage:          image,
			RegistryTLS:             tls,
			Name:                    mod.Name,
			Namespace:               mod.Namespace,
			Owner:                   &mod,
			KernelVersion:           kernelVersion,
			KernelNormalizedVersion: kernelNormalizedVersion,
		}

		gomock.InOrder(
			mc.EXPECT().ApplyBuildArgOverrides(nil, defaultBuildArgs),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: dockerfileConfigMap.Name, Namespace: mod.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, cm *v1.ConfigMap, _ ...ctrlclient.GetOption) error {
					cm.Data = dockerfileCMData
					return nil
				},
			),
		)

		actual, err := buildSignPodManager.MakeBuildResourceTemplate(ctx, &mld, mld.Owner, pushImage)

		Expect(err).NotTo(HaveOccurred())
		Expect(actual.Spec.Containers[0].Args).To(ContainElement(kanikoFlag))

		if pushImage {
			Expect(actual.Spec.Containers[0].Args).To(ContainElement("--destination"))
		} else {
			Expect(actual.Spec.Containers[0].Args).To(ContainElement("--no-push"))
		}
	},
		Entry(
			"BaseImageRegistryTLS.Insecure",
			nil,
			&kmmv1beta1.Build{
				BaseImageRegistryTLS: kmmv1beta1.TLSOptions{Insecure: true},
				DockerfileConfigMap:  &dockerfileConfigMap,
			},
			"--insecure-pull",
			false,
		),
		Entry(
			"BaseImageRegistryTLS.InsecureSkipTLSVerify",
			nil,
			&kmmv1beta1.Build{
				BaseImageRegistryTLS: kmmv1beta1.TLSOptions{InsecureSkipTLSVerify: true},
				DockerfileConfigMap:  &dockerfileConfigMap,
			},
			"--skip-tls-verify-pull",
			false,
		),
		Entry(
			"RegistryTLS.Insecure",
			&kmmv1beta1.TLSOptions{Insecure: true},
			&kmmv1beta1.Build{DockerfileConfigMap: &dockerfileConfigMap},
			"--insecure",
			true,
		),
		Entry(
			"RegistryTLS.InsecureSkipTLSVerify",
			&kmmv1beta1.TLSOptions{InsecureSkipTLSVerify: true},
			&kmmv1beta1.Build{DockerfileConfigMap: &dockerfileConfigMap},
			"--skip-tls-verify",
			true,
		),
	)

	It("use a custom given tag", func() {
		const customTag = "some-tag"
		ctx := context.Background()

		GinkgoT().Setenv(relatedImageEnvVar, "some-build-image:original-tag")

		mld := api.ModuleLoaderData{
			Name:      mod.Name,
			Namespace: mod.Namespace,
			Owner:     &mod,
			Build: &kmmv1beta1.Build{
				BuildArgs:           buildArgs,
				DockerfileConfigMap: &dockerfileConfigMap,
				KanikoParams:        &kmmv1beta1.KanikoParams{Tag: customTag},
			},
			ContainerImage:          image,
			RegistryTLS:             &kmmv1beta1.TLSOptions{},
			KernelVersion:           kernelVersion,
			KernelNormalizedVersion: kernelNormalizedVersion,
		}

		gomock.InOrder(
			mc.EXPECT().ApplyBuildArgOverrides(buildArgs, defaultBuildArgs),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: dockerfileConfigMap.Name, Namespace: mld.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, cm *v1.ConfigMap, _ ...ctrlclient.GetOption) error {
					cm.Data = dockerfileCMData
					return nil
				},
			),
		)

		actual, err := buildSignPodManager.MakeBuildResourceTemplate(ctx, &mld, mld.Owner, false)

		Expect(err).NotTo(HaveOccurred())
		Expect(actual.Spec.Containers[0].Image).To(Equal("some-build-image:" + customTag))
	})

	It("should add the kmm_unsigned suffix to the target image if sign is defined", func() {
		ctx := context.Background()

		mld := api.ModuleLoaderData{
			Name:      mod.Name,
			Namespace: mod.Namespace,
			Owner:     &mod,
			Build: &kmmv1beta1.Build{
				BuildArgs:           buildArgs,
				DockerfileConfigMap: &dockerfileConfigMap,
			},
			Sign:                    &kmmv1beta1.Sign{},
			ContainerImage:          image,
			RegistryTLS:             &kmmv1beta1.TLSOptions{},
			KernelVersion:           kernelVersion,
			KernelNormalizedVersion: kernelNormalizedVersion,
		}

		expectedImageName := mld.ContainerImage + ":" + mld.Namespace + "_" + mld.Name + "_kmm_unsigned"

		gomock.InOrder(
			mc.EXPECT().ApplyBuildArgOverrides(buildArgs, defaultBuildArgs),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: dockerfileConfigMap.Name, Namespace: mld.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, cm *v1.ConfigMap, _ ...ctrlclient.GetOption) error {
					cm.Data = dockerfileCMData
					return nil
				},
			),
		)

		actual, err := buildSignPodManager.MakeBuildResourceTemplate(ctx, &mld, mld.Owner, true)

		Expect(err).NotTo(HaveOccurred())
		Expect(actual.Spec.Containers[0].Args).To(ContainElement("--destination"))
		Expect(actual.Spec.Containers[0].Args).To(ContainElement(expectedImageName))
	})
})
