package signpod

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	"github.com/google/go-cmp/cmp"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

var _ = Describe("MakePodTemplate", func() {
	const (
		unsignedImage = "my.registry/my/image"
		signedImage   = unsignedImage + "-signed"
		buildImage    = "some-kaniko-image:some-tag"
		kernelVersion = "1.2.3"
		moduleName    = "module-name"
		namespace     = "some-namespace"
		privateKey    = "some private key"
		publicKey     = "some public key"

		dockerfile = `FROM my.registry/my/image as source

FROM some-sign-image:some-tag AS signimage

RUN mkdir -p /tmp/signroot

COPY --from=source /modules/simple-kmod.ko /tmp/signroot/modules/simple-kmod.ko
RUN /usr/local/bin/sign-file sha256 /run/secrets/key/key.pem /run/secrets/cert/cert.pem /tmp/signroot/modules/simple-kmod.ko
COPY --from=source /modules/simple-procfs-kmod.ko /tmp/signroot/modules/simple-procfs-kmod.ko
RUN /usr/local/bin/sign-file sha256 /run/secrets/key/key.pem /run/secrets/cert/cert.pem /tmp/signroot/modules/simple-procfs-kmod.ko

FROM source

COPY --from=signimage /tmp/signroot/modules/simple-kmod.ko /modules/simple-kmod.ko
COPY --from=signimage /tmp/signroot/modules/simple-procfs-kmod.ko /modules/simple-procfs-kmod.ko
`
	)

	var (
		ctrl      *gomock.Controller
		clnt      *client.MockClient
		mld       api.ModuleLoaderData
		m         Signer
		podhelper *utils.MockPodHelper

		filesToSign = []string{
			"/modules/simple-kmod.ko",
			"/modules/simple-procfs-kmod.ko",
		}
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		podhelper = utils.NewMockPodHelper(ctrl)
		m = NewSigner(clnt, scheme, podhelper)
		mld = api.ModuleLoaderData{
			Name:      moduleName,
			Namespace: namespace,
			Owner: &kmmv1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: namespace,
				},
			},
			KernelVersion: kernelVersion,
		}
	})

	labels := map[string]string{"kmm.node.kubernetes.io/pod-type": "sign",
		"kmm.node.kubernetes.io/module.name":   moduleName,
		"kmm.node.kubernetes.io/target-kernel": kernelVersion,
	}

	publicSignData := map[string][]byte{constants.PublicSignDataKey: []byte(publicKey)}
	privateSignData := map[string][]byte{constants.PrivateSignDataKey: []byte(privateKey)}

	DescribeTable("should set fields correctly", func(imagePullSecret *v1.LocalObjectReference) {
		GinkgoT().Setenv("RELATED_IMAGE_BUILD", buildImage)
		GinkgoT().Setenv("RELATED_IMAGE_SIGN", "some-sign-image:some-tag")

		ctx := context.Background()
		nodeSelector := map[string]string{"arch": "x64"}

		mld.Sign = &kmmv1beta1.Sign{
			UnsignedImage: signedImage,
			KeySecret:     &v1.LocalObjectReference{Name: "securebootkey"},
			CertSecret:    &v1.LocalObjectReference{Name: "securebootcert"},
			FilesToSign:   filesToSign,
		}
		mld.ContainerImage = signedImage
		mld.RegistryTLS = &kmmv1beta1.TLSOptions{}

		secretMount := v1.VolumeMount{
			Name:      "secret-securebootcert",
			ReadOnly:  true,
			MountPath: "/run/secrets/cert",
		}
		certMount := v1.VolumeMount{
			Name:      "secret-securebootkey",
			ReadOnly:  true,
			MountPath: "/run/secrets/key",
		}
		keysecret := v1.Volume{
			Name: "secret-securebootkey",
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: "securebootkey",
					Items: []v1.KeyToPath{
						{
							Key:  "key",
							Path: "key.pem",
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
							Path: "cert.pem",
						},
					},
				},
			},
		}

		expected := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: mld.Name + "-sign-",
				Namespace:    namespace,
				Labels: map[string]string{
					constants.ModuleNameLabel:    moduleName,
					constants.TargetKernelTarget: kernelVersion,
					constants.PodType:            "sign",
				},
				OwnerReferences: []metav1.OwnerReference{
					{
						APIVersion:         "kmm.sigs.x-k8s.io/v1beta1",
						Kind:               "Module",
						Name:               moduleName,
						Controller:         ptr.To(true),
						BlockOwnerDeletion: ptr.To(true),
					},
				},
				Finalizers: []string{constants.JobEventFinalizer},
			},
			Spec: v1.PodSpec{
				Containers: []v1.Container{
					{
						Name:  "kaniko",
						Image: buildImage,
						Args:  []string{"--destination", signedImage},
						VolumeMounts: []v1.VolumeMount{
							secretMount,
							certMount,
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
					keysecret,
					certsecret,
					{
						Name: "dockerfile",
						VolumeSource: v1.VolumeSource{
							DownwardAPI: &v1.DownwardAPIVolumeSource{
								Items: []v1.DownwardAPIVolumeFile{
									{
										Path: "Dockerfile",
										FieldRef: &v1.ObjectFieldSelector{
											FieldPath: "metadata.annotations['dockerfile']",
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
									{Key: ".dockerconfigjson", Path: "config.json"},
								},
							},
						},
					},
				)
		}

		hash, err := getHashValue(&expected.Spec, []byte(publicKey), []byte(privateKey), []byte(dockerfile))
		Expect(err).NotTo(HaveOccurred())
		annotations := map[string]string{
			constants.PodHashAnnotation: fmt.Sprintf("%d", hash),
			"dockerfile":                dockerfile,
		}
		expected.SetAnnotations(annotations)

		mld.Selector = nodeSelector

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: mld.Sign.KeySecret.Name, Namespace: mld.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, secret *v1.Secret, _ ...ctrlclient.GetOption) error {
					secret.Data = privateSignData
					return nil
				},
			),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: mld.Sign.CertSecret.Name, Namespace: mld.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, secret *v1.Secret, _ ...ctrlclient.GetOption) error {
					secret.Data = publicSignData
					return nil
				},
			),
		)

		actual, err := m.MakePodTemplate(ctx, &mld, labels, unsignedImage, true, mld.Owner)
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

	DescribeTable("should set correct kmod-signer flags", func(filelist []string, pushImage bool) {
		ctx := context.Background()
		mld.Sign = &kmmv1beta1.Sign{
			UnsignedImage: signedImage,
			KeySecret:     &v1.LocalObjectReference{Name: "securebootkey"},
			CertSecret:    &v1.LocalObjectReference{Name: "securebootcert"},
			FilesToSign:   filelist,
		}
		mld.ContainerImage = unsignedImage
		mld.RegistryTLS = &kmmv1beta1.TLSOptions{}

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: mld.Sign.KeySecret.Name, Namespace: mld.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, secret *v1.Secret, _ ...ctrlclient.GetOption) error {
					secret.Data = privateSignData
					return nil
				},
			),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: mld.Sign.CertSecret.Name, Namespace: mld.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, secret *v1.Secret, _ ...ctrlclient.GetOption) error {
					secret.Data = publicSignData
					return nil
				},
			),
		)

		actual, err := m.MakePodTemplate(ctx, &mld, labels, "", pushImage, mld.Owner)

		Expect(err).NotTo(HaveOccurred())

		if pushImage {
			Expect(actual.Spec.Containers[0].Args).To(ContainElement("--destination"))
		} else {
			Expect(actual.Spec.Containers[0].Args).To(ContainElement("-no-push"))
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

	DescribeTable("should set correct kmod-signer TLS flags", func(kmRegistryTLS,
		unsignedImageRegistryTLS kmmv1beta1.TLSOptions, expectedFlag string) {
		ctx := context.Background()
		mld.Sign = &kmmv1beta1.Sign{
			UnsignedImage:            signedImage,
			UnsignedImageRegistryTLS: unsignedImageRegistryTLS,
			KeySecret:                &v1.LocalObjectReference{Name: "securebootkey"},
			CertSecret:               &v1.LocalObjectReference{Name: "securebootcert"},
		}
		mld.RegistryTLS = &kmRegistryTLS

		gomock.InOrder(
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: mld.Sign.KeySecret.Name, Namespace: mld.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, secret *v1.Secret, _ ...ctrlclient.GetOption) error {
					secret.Data = privateSignData
					return nil
				},
			),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: mld.Sign.CertSecret.Name, Namespace: mld.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, secret *v1.Secret, _ ...ctrlclient.GetOption) error {
					secret.Data = publicSignData
					return nil
				},
			),
		)

		actual, err := m.MakePodTemplate(ctx, &mld, labels, "", true, mld.Owner)

		Expect(err).NotTo(HaveOccurred())
		Expect(actual.Spec.Containers[0].Args).To(ContainElement(expectedFlag))
	},
		Entry(
			"filelist and push",
			kmmv1beta1.TLSOptions{
				Insecure: true,
			},
			kmmv1beta1.TLSOptions{},
			"--insecure",
		),
		Entry(
			"filelist and push",
			kmmv1beta1.TLSOptions{
				InsecureSkipTLSVerify: true,
			},
			kmmv1beta1.TLSOptions{},
			"--skip-tls-verify",
		),
		Entry(
			"filelist and push",
			kmmv1beta1.TLSOptions{},
			kmmv1beta1.TLSOptions{
				Insecure: true,
			},
			"--insecure-pull",
		),
		Entry(
			"filelist and push",
			kmmv1beta1.TLSOptions{},
			kmmv1beta1.TLSOptions{
				InsecureSkipTLSVerify: true,
			},
			"--skip-tls-verify-pull",
		),
	)
})
