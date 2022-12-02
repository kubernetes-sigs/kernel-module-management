package job

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	"golang.org/x/exp/slices"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/build"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

var _ = Describe("MakeJobTemplate", func() {
	const (
		image         = "my.registry/my/image"
		dockerfile    = "FROM test"
		kernelVersion = "1.2.3"
		moduleName    = "module-name"
		namespace     = "some-namespace"
	)

	var (
		ctrl      *gomock.Controller
		clnt      *client.MockClient
		m         Maker
		mh        *build.MockHelper
		jobhelper *utils.MockJobHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mh = build.NewMockHelper(ctrl)
		jobhelper = utils.NewMockJobHelper(ctrl)
		m = NewMaker(clnt, mh, jobhelper, scheme)
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

	buildArgs := []kmmv1beta1.BuildArg{
		{Name: "name1", Value: "value1"},
	}

	dockerfileConfigMap := v1.LocalObjectReference{Name: "configMapName"}
	dockerfileCMData := map[string]string{constants.DockerfileCMKey: dockerfile}

	DescribeTable("should set fields correctly", func(buildSecrets []v1.LocalObjectReference, imagePullSecret *v1.LocalObjectReference) {
		ctx := context.Background()
		nodeSelector := map[string]string{"arch": "x64"}

		km := kmmv1beta1.KernelMapping{
			Build: &kmmv1beta1.Build{
				BuildArgs:           buildArgs,
				DockerfileConfigMap: &dockerfileConfigMap,
			},
			ContainerImage: image,
		}

		labels := map[string]string{
			constants.ModuleNameLabel:    moduleName,
			constants.TargetKernelTarget: kernelVersion,
			constants.JobType:            "build",
		}

		expected := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: mod.Name + "-build-",
				Namespace:    namespace,
				Labels:       labels,
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         "kmm.sigs.k8s.io/v1beta1",
						Kind:               "Module",
						Name:               moduleName,
						Controller:         pointer.Bool(true),
						BlockOwnerDeletion: pointer.Bool(true),
					},
				},
			},
			Spec: batchv1.JobSpec{
				Completions: pointer.Int32(1),
				Template: v1.PodTemplateSpec{
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Args: []string{
									"--destination", image,
									"--build-arg", "name1=value1",
									"--build-arg", "KERNEL_VERSION=" + kernelVersion,
								},
								Name:  "kaniko",
								Image: "gcr.io/kaniko-project/executor:latest",
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
						RestartPolicy: v1.RestartPolicyOnFailure,
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
				},
			},
		}

		if imagePullSecret != nil {
			mod.Spec.ImageRepoSecret = imagePullSecret

			expected.Spec.Template.Spec.Containers[0].VolumeMounts =
				append(expected.Spec.Template.Spec.Containers[0].VolumeMounts,
					v1.VolumeMount{
						Name:      "secret-pull-push-secret",
						ReadOnly:  true,
						MountPath: "/kaniko/.docker",
					},
				)

			expected.Spec.Template.Spec.Volumes =
				append(expected.Spec.Template.Spec.Volumes,
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

			km.Build.Secrets = buildSecrets

			expected.Spec.Template.Spec.Containers[0].VolumeMounts =
				append(expected.Spec.Template.Spec.Containers[0].VolumeMounts,
					v1.VolumeMount{
						Name:      "secret-s1",
						ReadOnly:  true,
						MountPath: "/run/secrets/s1",
					},
				)

			expected.Spec.Template.Spec.Volumes =
				append(expected.Spec.Template.Spec.Volumes,
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
		hash, err := getHashValue(&expected.Spec.Template, dockerfile)
		Expect(err).NotTo(HaveOccurred())
		annotations := map[string]string{constants.JobHashAnnotation: fmt.Sprintf("%d", hash)}
		expected.SetAnnotations(annotations)

		mod := mod.DeepCopy()
		mod.Spec.Selector = nodeSelector

		override := kmmv1beta1.BuildArg{Name: "KERNEL_VERSION", Value: kernelVersion}
		gomock.InOrder(
			mh.EXPECT().GetRelevantBuild(mod.Spec, km).Return(km.Build),
			mh.EXPECT().ApplyBuildArgOverrides(buildArgs, override).Return(append(slices.Clone(buildArgs), override)),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: dockerfileConfigMap.Name, Namespace: mod.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, cm *v1.ConfigMap, _ ...ctrlclient.GetOption) error {
					cm.Data = dockerfileCMData
					return nil
				},
			),
			jobhelper.EXPECT().JobLabels(mod.Name, kernelVersion, utils.JobTypeBuild).Return(labels),
		)

		actual, err := m.MakeJobTemplate(ctx, *mod, km, kernelVersion, mod, true)
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
		),
		Entry(
			"only buidSecrets",
			[]v1.LocalObjectReference{{Name: "s1"}},
			nil,
		),
		Entry(
			"only imagePullSecrets",
			[]v1.LocalObjectReference{},
			&v1.LocalObjectReference{Name: "pull-push-secret"},
		),
		Entry(
			"buildSecrets and imagePullSecrets",
			[]v1.LocalObjectReference{{Name: "s1"}},
			&v1.LocalObjectReference{Name: "pull-push-secret"},
		),
	)

	DescribeTable("should set correct kaniko flags", func(tls *kmmv1beta1.TLSOptions, b *kmmv1beta1.Build, kanikoFlag string, pushImage bool) {
		ctx := context.Background()

		km := kmmv1beta1.KernelMapping{
			Build:          b,
			ContainerImage: image,
			RegistryTLS:    tls,
		}

		gomock.InOrder(
			mh.EXPECT().GetRelevantBuild(mod.Spec, km).Return(km.Build),
			mh.EXPECT().ApplyBuildArgOverrides(nil, kmmv1beta1.BuildArg{Name: "KERNEL_VERSION", Value: kernelVersion}),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: dockerfileConfigMap.Name, Namespace: mod.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, cm *v1.ConfigMap, _ ...ctrlclient.GetOption) error {
					cm.Data = dockerfileCMData
					return nil
				},
			),
			jobhelper.EXPECT().JobLabels(mod.Name, kernelVersion, utils.JobTypeBuild).Return(map[string]string{}),
		)

		actual, err := m.MakeJobTemplate(ctx, mod, km, kernelVersion, &mod, pushImage)

		Expect(err).NotTo(HaveOccurred())
		Expect(actual.Spec.Template.Spec.Containers[0].Args).To(ContainElement(kanikoFlag))

		if pushImage {
			Expect(actual.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--destination"))
		} else {
			Expect(actual.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--no-push"))
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
			true,
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
			false,
		),
	)

	It("use a custom given tag", func() {
		const customTag = "some-tag"
		ctx := context.Background()

		km := kmmv1beta1.KernelMapping{
			Build: &kmmv1beta1.Build{
				BuildArgs:           buildArgs,
				DockerfileConfigMap: &dockerfileConfigMap,
				KanikoParams:        &kmmv1beta1.KanikoParams{Tag: customTag},
			},
			ContainerImage: image,
		}

		override := kmmv1beta1.BuildArg{Name: "KERNEL_VERSION", Value: kernelVersion}
		gomock.InOrder(
			mh.EXPECT().GetRelevantBuild(mod.Spec, km).Return(km.Build),
			mh.EXPECT().ApplyBuildArgOverrides(buildArgs, override),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: dockerfileConfigMap.Name, Namespace: mod.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, cm *v1.ConfigMap, _ ...ctrlclient.GetOption) error {
					cm.Data = dockerfileCMData
					return nil
				},
			),
			jobhelper.EXPECT().JobLabels(mod.Name, kernelVersion, utils.JobTypeBuild).Return(map[string]string{}),
		)

		actual, err := m.MakeJobTemplate(ctx, mod, km, kernelVersion, &mod, false)

		Expect(err).NotTo(HaveOccurred())
		Expect(actual.Spec.Template.Spec.Containers[0].Image).To(Equal("gcr.io/kaniko-project/executor:" + customTag))
	})

	It("should add the kmm_unsigned suffix to the target image if sign is defined", func() {
		ctx := context.Background()

		override := kmmv1beta1.BuildArg{Name: "KERNEL_VERSION", Value: kernelVersion}
		km := kmmv1beta1.KernelMapping{
			Sign: &kmmv1beta1.Sign{},
			Build: &kmmv1beta1.Build{
				BuildArgs:           buildArgs,
				DockerfileConfigMap: &dockerfileConfigMap,
			},
			ContainerImage: image,
		}

		expectedImageName := km.ContainerImage + ":" + mod.Namespace + "_" + mod.Name + "_kmm_unsigned"

		gomock.InOrder(
			mh.EXPECT().GetRelevantBuild(mod.Spec, km).Return(km.Build),
			mh.EXPECT().ApplyBuildArgOverrides(buildArgs, override),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: dockerfileConfigMap.Name, Namespace: mod.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, cm *v1.ConfigMap, _ ...ctrlclient.GetOption) error {
					cm.Data = dockerfileCMData
					return nil
				},
			),
			jobhelper.EXPECT().JobLabels(mod.Name, kernelVersion, utils.JobTypeBuild).Return(map[string]string{}),
		)

		actual, err := m.MakeJobTemplate(ctx, mod, km, kernelVersion, &mod, true)

		Expect(err).NotTo(HaveOccurred())
		Expect(actual.Spec.Template.Spec.Containers[0].Args).To(ContainElement("--destination"))
		Expect(actual.Spec.Template.Spec.Containers[0].Args).To(ContainElement(expectedImageName))
	})
})
