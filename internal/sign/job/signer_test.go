package signjob

import (
	"strings"

	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

var _ = Describe("MakeJobTemplate", func() {

	const (
		unsignedImage = "my.registry/my/image"
		signedImage   = "my.registry/my/image-signed"
		filesToSign   = "/modules/simple-kmod.ko:/modules/simple-procfs-kmod.ko"
		dockerfile    = "FROM test"
		kernelVersion = "1.2.3"
		moduleName    = "module-name"
		namespace     = "some-namespace"
	)

	var (
		ctrl *gomock.Controller
		m    Signer
		mod  kmmv1beta1.Module
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		m = NewSigner(scheme)
		mod = kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	labels := map[string]string{"kmm.node.kubernetes.io/job-type": "sign",
		"kmm.node.kubernetes.io/module.name":   moduleName,
		"kmm.node.kubernetes.io/target-kernel": kernelVersion,
	}

	DescribeTable("should set fields correctly", func(imagePullSecret *v1.LocalObjectReference) {
		nodeSelector := map[string]string{"arch": "x64"}

		km := kmmv1beta1.KernelMapping{
			Sign: &kmmv1beta1.Sign{
				UnsignedImage: signedImage,
				KeySecret:     &v1.LocalObjectReference{Name: "securebootkey"},
				CertSecret:    &v1.LocalObjectReference{Name: "securebootcert"},
				FilesToSign:   strings.Split(filesToSign, ","),
			},
			ContainerImage: signedImage,
		}

		secretMount := v1.VolumeMount{
			Name:      "secret-securebootcert",
			ReadOnly:  true,
			MountPath: "/signingcert",
		}
		certMount := v1.VolumeMount{
			Name:      "secret-securebootkey",
			ReadOnly:  true,
			MountPath: "/signingkey",
		}
		keysecret := v1.Volume{
			Name: "secret-securebootkey",
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: "securebootkey",
					Items: []v1.KeyToPath{
						{
							Key:  "key",
							Path: "key.priv",
						},
					},
				},
			},
		}
		certsecret := v1.Volume{
			Name: "secret-securebootcert",
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: "securebootcert",
					Items: []v1.KeyToPath{
						{
							Key:  "cert",
							Path: "public.der",
						},
					},
				},
			},
		}

		expected := &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: mod.Name + "-sign-",
				Namespace:    namespace,
				Labels: map[string]string{
					constants.ModuleNameLabel:    moduleName,
					constants.TargetKernelTarget: kernelVersion,
					constants.JobType:            "sign",
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
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "signimage",
								Image: "quay.io/chrisp262/kmod-signer:latest",
								Args: []string{
									"-signedimage", signedImage,
									"-unsignedimage", unsignedImage,
									"-key", "/signingkey/key.priv",
									"-cert", "/signingcert/public.der",
									"-filestosign", filesToSign,
								},
								VolumeMounts: []v1.VolumeMount{secretMount, certMount},
							},
						},
						NodeSelector:  nodeSelector,
						RestartPolicy: v1.RestartPolicyOnFailure,

						Volumes: []v1.Volume{keysecret, certsecret},
					},
				},
			},
		}
		if imagePullSecret != nil {
			mod.Spec.ImageRepoSecret = imagePullSecret
			expected.Spec.Template.Spec.Containers[0].Args = append(expected.Spec.Template.Spec.Containers[0].Args, "-pullsecret")
			expected.Spec.Template.Spec.Containers[0].Args = append(expected.Spec.Template.Spec.Containers[0].Args, "/docker_config/config.json")
			expected.Spec.Template.Spec.Containers[0].VolumeMounts =
				append(expected.Spec.Template.Spec.Containers[0].VolumeMounts,
					v1.VolumeMount{
						Name:      "secret-pull-push-secret",
						ReadOnly:  true,
						MountPath: "/docker_config",
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

		mod := mod.DeepCopy()
		mod.Spec.Selector = nodeSelector

		actual, err := m.MakeJobTemplate(*mod, km.Sign, kernelVersion, unsignedImage, signedImage, labels, true)
		Expect(err).NotTo(HaveOccurred())

		Expect(
			cmp.Diff(expected, actual),
		).To(
			BeEmpty(),
		)
	},
		Entry(
			"no secrets at all",
			nil,
		),
		Entry(
			"only imagePullSecrets",
			&v1.LocalObjectReference{Name: "pull-push-secret"},
		),
	)

	DescribeTable("should set correct kmod-signer flags", func(filelist []string, pushFlag bool) {

		signConfig := &kmmv1beta1.Sign{
			UnsignedImage: signedImage,
			KeySecret:     &v1.LocalObjectReference{Name: "securebootkey"},
			CertSecret:    &v1.LocalObjectReference{Name: "securebootcert"},
			FilesToSign:   filelist,
		}

		actual, err := m.MakeJobTemplate(mod, signConfig, kernelVersion, "", signedImage, labels, pushFlag)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual.Spec.Template.Spec.Containers[0].Args).To(ContainElement("-unsignedimage"))
		Expect(actual.Spec.Template.Spec.Containers[0].Args).To(ContainElement("-key"))
		Expect(actual.Spec.Template.Spec.Containers[0].Args).To(ContainElement("-cert"))
		if pushFlag {
			Expect(actual.Spec.Template.Spec.Containers[0].Args).To(ContainElement("-signedimage"))
		} else {
			Expect(actual.Spec.Template.Spec.Containers[0].Args).To(ContainElement("-no-push"))
		}

	},
		Entry(
			"filelist and push",
			[]string{"simple-kmod", "complicated-kmod"},
			true,
		),
		Entry(
			"filelist and no push",
			[]string{"simple-kmod", "complicated-kmod"},
			false,
		),
		Entry(
			"all kmods and push",
			[]string{},
			true,
		),
		Entry(
			"all kmods and dont push",
			[]string{},
			false,
		),
	)
})
