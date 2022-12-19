package signjob

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/golang/mock/gomock"
	"github.com/google/go-cmp/cmp"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/sign"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
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
		privateKey    = "some private key"
		publicKey     = "some public key"
	)

	var (
		ctrl      *gomock.Controller
		clnt      *client.MockClient
		m         Signer
		mod       kmmv1beta1.Module
		helper    *sign.MockHelper
		jobhelper *utils.MockJobHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		helper = sign.NewMockHelper(ctrl)
		jobhelper = utils.NewMockJobHelper(ctrl)
		m = NewSigner(clnt, scheme, helper, jobhelper)
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

	publicSignData := map[string][]byte{constants.PublicSignDataKey: []byte(publicKey)}
	privateSignData := map[string][]byte{constants.PrivateSignDataKey: []byte(privateKey)}

	DescribeTable("should set fields correctly", func(imagePullSecret *v1.LocalObjectReference) {
		ctx := context.Background()
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
						APIVersion:         "kmm.sigs.x-k8s.io/v1beta1",
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

		hash, err := getHashValue(&expected.Spec.Template, []byte(publicKey), []byte(privateKey))
		Expect(err).NotTo(HaveOccurred())
		annotations := map[string]string{constants.JobHashAnnotation: fmt.Sprintf("%d", hash)}
		expected.SetAnnotations(annotations)

		mod := mod.DeepCopy()
		mod.Spec.Selector = nodeSelector

		gomock.InOrder(
			helper.EXPECT().GetRelevantSign(mod.Spec, km).Return(km.Sign),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: km.Sign.KeySecret.Name, Namespace: mod.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, secret *v1.Secret, _ ...ctrlclient.GetOption) error {
					secret.Data = privateSignData
					return nil
				},
			),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: km.Sign.CertSecret.Name, Namespace: mod.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, secret *v1.Secret, _ ...ctrlclient.GetOption) error {
					secret.Data = publicSignData
					return nil
				},
			),
		)

		actual, err := m.MakeJobTemplate(ctx, *mod, km, kernelVersion, labels, unsignedImage, true, mod)
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
		km := kmmv1beta1.KernelMapping{
			Sign: &kmmv1beta1.Sign{
				UnsignedImage: signedImage,
				KeySecret:     &v1.LocalObjectReference{Name: "securebootkey"},
				CertSecret:    &v1.LocalObjectReference{Name: "securebootcert"},
				FilesToSign:   filelist,
			},
			ContainerImage: unsignedImage,
		}

		gomock.InOrder(
			helper.EXPECT().GetRelevantSign(mod.Spec, km).Return(km.Sign),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: km.Sign.KeySecret.Name, Namespace: mod.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, secret *v1.Secret, _ ...ctrlclient.GetOption) error {
					secret.Data = privateSignData
					return nil
				},
			),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: km.Sign.CertSecret.Name, Namespace: mod.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, secret *v1.Secret, _ ...ctrlclient.GetOption) error {
					secret.Data = publicSignData
					return nil
				},
			),
		)

		actual, err := m.MakeJobTemplate(ctx, mod, km, kernelVersion, labels, "", pushImage, &mod)

		Expect(err).NotTo(HaveOccurred())
		Expect(actual.Spec.Template.Spec.Containers[0].Args).To(ContainElement("-unsignedimage"))
		Expect(actual.Spec.Template.Spec.Containers[0].Args).To(ContainElement("-key"))
		Expect(actual.Spec.Template.Spec.Containers[0].Args).To(ContainElement("-cert"))

		if pushImage {
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

	DescribeTable("should set correct kmod-signer TLS flags", func(kmRegistryTLS,
		unsignedImageRegistryTLS kmmv1beta1.TLSOptions, expectedFlag string) {
		ctx := context.Background()
		km := kmmv1beta1.KernelMapping{
			RegistryTLS: &kmRegistryTLS,
			Sign: &kmmv1beta1.Sign{
				UnsignedImage:            signedImage,
				UnsignedImageRegistryTLS: unsignedImageRegistryTLS,
				KeySecret:                &v1.LocalObjectReference{Name: "securebootkey"},
				CertSecret:               &v1.LocalObjectReference{Name: "securebootcert"},
			},
		}

		gomock.InOrder(
			helper.EXPECT().GetRelevantSign(mod.Spec, km).Return(km.Sign),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: km.Sign.KeySecret.Name, Namespace: mod.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, secret *v1.Secret, _ ...ctrlclient.GetOption) error {
					secret.Data = privateSignData
					return nil
				},
			),
			clnt.EXPECT().Get(ctx, types.NamespacedName{Name: km.Sign.CertSecret.Name, Namespace: mod.Namespace}, gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, secret *v1.Secret, _ ...ctrlclient.GetOption) error {
					secret.Data = publicSignData
					return nil
				},
			),
		)

		actual, err := m.MakeJobTemplate(ctx, mod, km, kernelVersion, labels, "", true, &mod)

		Expect(err).NotTo(HaveOccurred())
		Expect(actual.Spec.Template.Spec.Containers[0].Args).To(ContainElement(expectedFlag))
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
