package job

import (
	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ootov1alpha1 "github.com/qbarrand/oot-operator/api/v1alpha1"
	"github.com/qbarrand/oot-operator/internal/build"
	"github.com/qbarrand/oot-operator/internal/constants"
	"golang.org/x/exp/slices"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var _ = Describe("Maker", func() {
	Describe("MakeJob", func() {
		const (
			containerImage = "my.registry/my/image"
			dockerfile     = "FROM test"
			kernelVersion  = "1.2.3"
			moduleName     = "module-name"
			namespace      = "some-namespace"
		)

		var (
			m  Maker
			mh *build.MockHelper
		)

		mod := ootov1alpha1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
		}

		buildArgs := []ootov1alpha1.BuildArg{
			{Name: "name1", Value: "value1"},
		}

		km := ootov1alpha1.KernelMapping{
			Build: &ootov1alpha1.Build{
				BuildArgs:  buildArgs,
				Dockerfile: dockerfile,
				Secrets: []v1.LocalObjectReference{
					{Name: "s1"},
				},
			},
			ContainerImage: containerImage,
		}

		BeforeEach(func() {
			ctrl := gomock.NewController(GinkgoT())
			mh = build.NewMockHelper(ctrl)
			m = NewMaker(mh, scheme)
		})

		It("should set fields correctly", func() {
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
							APIVersion:         "ooto.sigs.k8s.io/v1alpha1",
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
										{
											Name:      "secret-s1",
											ReadOnly:  true,
											MountPath: "/run/secrets/s1",
										},
									},
								},
							},
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
								{
									Name: "secret-s1",
									VolumeSource: v1.VolumeSource{
										Secret: &v1.SecretVolumeSource{SecretName: "s1"},
									},
								},
							},
						},
					},
				},
			}

			override := ootov1alpha1.BuildArg{Name: "KERNEL_VERSION", Value: kernelVersion}
			mh.EXPECT().ApplyBuildArgOverrides(buildArgs, override).Return(append(slices.Clone(buildArgs), override))

			actual, err := m.MakeJob(mod, km.Build, kernelVersion, km.ContainerImage)
			Expect(err).NotTo(HaveOccurred())

			Expect(
				cmp.Diff(expected, actual),
			).To(
				BeEmpty(),
			)
		})
	})

	Describe("MakeSecretVolumes", func() {
		It("should return an empty list if there are no secrets", func() {
			Expect(
				MakeSecretVolumes(nil),
			).To(
				BeEmpty(),
			)
		})

		It("should two volumes for two secrets", func() {
			secretRefs := []v1.LocalObjectReference{
				{Name: "s1"},
				{Name: "s2"},
			}

			Expect(
				MakeSecretVolumes(secretRefs),
			).To(
				Equal([]v1.Volume{
					{
						Name: "secret-s1",
						VolumeSource: v1.VolumeSource{
							Secret: &v1.SecretVolumeSource{SecretName: "s1"},
						},
					},
					{
						Name: "secret-s2",
						VolumeSource: v1.VolumeSource{
							Secret: &v1.SecretVolumeSource{SecretName: "s2"},
						},
					},
				}),
			)
		})
	})

	Describe("MakeSecretVolumeMounts", func() {
		It("should return an empty list if there are no secrets", func() {
			Expect(
				MakeSecretVolumeMounts(nil),
			).To(
				BeEmpty(),
			)
		})

		It("should two volumes for two secrets", func() {
			secretRefs := []v1.LocalObjectReference{
				{Name: "s1"},
				{Name: "s2"},
			}

			Expect(
				MakeSecretVolumeMounts(secretRefs),
			).To(
				Equal([]v1.VolumeMount{
					{
						Name:      "secret-s1",
						ReadOnly:  true,
						MountPath: "/run/secrets/s1",
					},
					{
						Name:      "secret-s2",
						ReadOnly:  true,
						MountPath: "/run/secrets/s2",
					},
				}),
			)
		})
	})
})
