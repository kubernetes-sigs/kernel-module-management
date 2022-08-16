package job

import (
	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	kmmv1beta1 "github.com/qbarrand/oot-operator/api/v1beta1"
	"github.com/qbarrand/oot-operator/internal/build"
	"github.com/qbarrand/oot-operator/internal/constants"
	"golang.org/x/exp/slices"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var _ = Describe("MakeJob", func() {

	const (
		containerImage = "my.registry/my/image"
		dockerfile     = "FROM test"
		kernelVersion  = "1.2.3"
		moduleName     = "module-name"
		namespace      = "some-namespace"
	)

	var (
		ctrl *gomock.Controller
		m    Maker
		mh   *build.MockHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mh = build.NewMockHelper(ctrl)
		m = NewMaker(mh, scheme)
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

	DescribeTable("should set fields correctly", func(buildSecrets []v1.LocalObjectReference, imagePullSecret *v1.LocalObjectReference) {
		nodeSelector := map[string]string{"arch": "x64"}

		km := kmmv1beta1.KernelMapping{
			Build: &kmmv1beta1.Build{
				BuildArgs:  buildArgs,
				Dockerfile: dockerfile,
			},
			ContainerImage: containerImage,
		}

		expected := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: mod.Name + "-build-",
				Namespace:    namespace,
				Labels: map[string]string{
					constants.ModuleNameLabel:    moduleName,
					constants.TargetKernelTarget: kernelVersion,
				},
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
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{"Dockerfile": dockerfile},
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Args: []string{
									"--destination", containerImage,
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
									DownwardAPI: &v1.DownwardAPIVolumeSource{
										Items: []v1.DownwardAPIVolumeFile{
											{
												Path: "Dockerfile",
												FieldRef: &v1.ObjectFieldSelector{
													FieldPath: "metadata.annotations['Dockerfile']",
												},
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

		mod := mod.DeepCopy()
		mod.Spec.Selector = nodeSelector

		override := kmmv1beta1.BuildArg{Name: "KERNEL_VERSION", Value: kernelVersion}
		mh.EXPECT().ApplyBuildArgOverrides(buildArgs, override).Return(append(slices.Clone(buildArgs), override))

		actual, err := m.MakeJob(*mod, km.Build, kernelVersion, km.ContainerImage)
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

	DescribeTable("should set correct kaniko flags", func(b kmmv1beta1.Build, flag string) {

		km := kmmv1beta1.KernelMapping{
			Build: &kmmv1beta1.Build{
				BuildArgs:  buildArgs,
				Dockerfile: dockerfile,
			},
			ContainerImage: containerImage,
		}

		mh.EXPECT().ApplyBuildArgOverrides(nil, kmmv1beta1.BuildArg{Name: "KERNEL_VERSION", Value: kernelVersion})

		actual, err := m.MakeJob(mod, &b, kernelVersion, km.ContainerImage)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual.Spec.Template.Spec.Containers[0].Args).To(ContainElement(flag))

	},
		Entry(
			"PullOptions.Insecure",
			kmmv1beta1.Build{Pull: kmmv1beta1.PullOptions{Insecure: true}},
			"--insecure-pull",
		),
		Entry(
			"PullOptions.InsecureSkipTLSVerify",
			kmmv1beta1.Build{Pull: kmmv1beta1.PullOptions{InsecureSkipTLSVerify: true}},
			"--skip-tls-verify-pull",
		),
		Entry(
			"PushOptions.Insecure",
			kmmv1beta1.Build{Push: kmmv1beta1.PushOptions{Insecure: true}},
			"--insecure",
		),
		Entry(
			"PushOptions.InsecureSkipTLSVerify",
			kmmv1beta1.Build{Push: kmmv1beta1.PushOptions{InsecureSkipTLSVerify: true}},
			"--skip-tls-verify",
		),
	)
})
