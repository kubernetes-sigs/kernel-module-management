package resource

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/go-cmp/cmp"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/mitchellh/hashstructure/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/module"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("makeBuildTemplate", func() {
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
		ctrl *gomock.Controller
		clnt *client.MockClient
		mc   *module.MockCombiner
		rm   *resourceManager
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mc = module.NewMockCombiner(ctrl)
		rm = &resourceManager{
			client:   clnt,
			combiner: mc,
			scheme:   scheme,
		}
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
				Labels:       resourceLabels(mld.Name, mld.KernelNormalizedVersion, kmmv1beta1.BuildImage),
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
		dataToHash := struct {
			BuildSpec  *v1.PodSpec
			Dockerfile string
		}{
			BuildSpec:  &expected.Spec,
			Dockerfile: dockerfile,
		}
		hash, err := hashstructure.Hash(dataToHash, hashstructure.FormatV2, nil)
		Expect(err).NotTo(HaveOccurred())
		annotations := map[string]string{constants.ResourceHashAnnotation: fmt.Sprintf("%d", hash)}
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

		actual, err := rm.makeBuildTemplate(ctx, &mld, mld.Owner, true)
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

		actual, err := rm.makeBuildTemplate(ctx, &mld, mld.Owner, pushImage)
		actualPod, ok := actual.(*v1.Pod)
		Expect(ok).To(BeTrue())

		Expect(err).NotTo(HaveOccurred())
		Expect(actualPod.Spec.Containers[0].Args).To(ContainElement(kanikoFlag))

		if pushImage {
			Expect(actualPod.Spec.Containers[0].Args).To(ContainElement("--destination"))
		} else {
			Expect(actualPod.Spec.Containers[0].Args).To(ContainElement("--no-push"))
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

		actual, err := rm.makeBuildTemplate(ctx, &mld, mld.Owner, false)
		actualPod, ok := actual.(*v1.Pod)
		Expect(ok).To(BeTrue())

		Expect(err).NotTo(HaveOccurred())
		Expect(actualPod.Spec.Containers[0].Image).To(Equal("some-build-image:" + customTag))
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

		actual, err := rm.makeBuildTemplate(ctx, &mld, mld.Owner, true)
		actualPod, ok := actual.(*v1.Pod)
		Expect(ok).To(BeTrue())

		Expect(err).NotTo(HaveOccurred())
		Expect(actualPod.Spec.Containers[0].Args).To(ContainElement("--destination"))
		Expect(actualPod.Spec.Containers[0].Args).To(ContainElement(expectedImageName))
	})
})

var _ = Describe("makeSignTemplate", func() {
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
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		mld          api.ModuleLoaderData
		mockCombiner *module.MockCombiner
		rm           *resourceManager

		filesToSign = []string{
			"/modules/simple-kmod.ko",
			"/modules/simple-procfs-kmod.ko",
		}
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockCombiner = module.NewMockCombiner(ctrl)
		rm = &resourceManager{
			client:   clnt,
			combiner: mockCombiner,
			scheme:   scheme,
		}
		mld = api.ModuleLoaderData{
			Name:      moduleName,
			Namespace: namespace,
			Owner: &kmmv1beta1.Module{
				ObjectMeta: metav1.ObjectMeta{
					Name:      moduleName,
					Namespace: namespace,
				},
			},
			KernelVersion:           kernelVersion,
			KernelNormalizedVersion: kernelVersion,
		}
	})

	publicSignData := map[string][]byte{constants.PublicSignDataKey: []byte(publicKey)}
	privateSignData := map[string][]byte{constants.PrivateSignDataKey: []byte(privateKey)}

	DescribeTable("should set fields correctly", func(imagePullSecret *v1.LocalObjectReference) {
		GinkgoT().Setenv("RELATED_IMAGE_BUILD", buildImage)
		GinkgoT().Setenv("RELATED_IMAGE_SIGN", "some-sign-image:some-tag")

		ctx := context.Background()
		nodeSelector := map[string]string{"arch": "x64"}

		mld.Sign = &kmmv1beta1.Sign{
			UnsignedImage: unsignedImage,
			KeySecret:     &v1.LocalObjectReference{Name: "securebootkey"},
			CertSecret:    &v1.LocalObjectReference{Name: "securebootcert"},
			FilesToSign:   filesToSign,
		}
		mld.ContainerImage = signedImage
		mld.RegistryTLS = &kmmv1beta1.TLSOptions{}

		certMount := v1.VolumeMount{
			Name:      "secret-securebootcert",
			ReadOnly:  true,
			MountPath: "/run/secrets/cert",
		}
		secretMount := v1.VolumeMount{
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
				Labels:       resourceLabels(mld.Name, mld.KernelNormalizedVersion, kmmv1beta1.SignImage),
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
						Name:  "kaniko",
						Image: buildImage,
						Args:  []string{"--destination", signedImage},
						VolumeMounts: []v1.VolumeMount{
							certMount,
							secretMount,
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
					certsecret,
					keysecret,
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

		dataToHash := struct {
			SignSpec       *v1.PodSpec
			PrivateKeyData []byte
			PublicKeyData  []byte
			SignConfig     []byte
		}{
			SignSpec:       &expected.Spec,
			PrivateKeyData: []byte(privateKey),
			PublicKeyData:  []byte(publicKey),
			SignConfig:     []byte(dockerfile),
		}
		hash, err := hashstructure.Hash(dataToHash, hashstructure.FormatV2, nil)
		Expect(err).NotTo(HaveOccurred())
		annotations := map[string]string{
			constants.ResourceHashAnnotation: fmt.Sprintf("%d", hash),
			"dockerfile":                     dockerfile,
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

		actual, err := rm.makeSignTemplate(ctx, &mld, mld.Owner, true)
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

		actual, err := rm.makeSignTemplate(ctx, &mld, mld.Owner, pushImage)
		Expect(err).NotTo(HaveOccurred())
		actualPod, ok := actual.(*v1.Pod)
		Expect(ok).To(BeTrue())

		if pushImage {
			Expect(actualPod.Spec.Containers[0].Args).To(ContainElement("--destination"))
		} else {
			Expect(actualPod.Spec.Containers[0].Args).To(ContainElement("--no-push"))
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

		actual, err := rm.makeSignTemplate(ctx, &mld, mld.Owner, true)
		Expect(err).NotTo(HaveOccurred())
		actualPod, ok := actual.(*v1.Pod)
		Expect(ok).To(BeTrue())
		Expect(actualPod.Spec.Containers[0].Args).To(ContainElement(expectedFlag))
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
var _ = Describe("resourceLabels", func() {

	It("get pod labels", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "moduleName"},
		}
		expected := map[string]string{
			"app.kubernetes.io/name":      "kmm",
			"app.kubernetes.io/component": "podType",
			"app.kubernetes.io/part-of":   "kmm",
			constants.ModuleNameLabel:     "moduleName",
			constants.TargetKernelTarget:  "targetKernel",
			constants.ResourceType:        "podType",
		}

		labels := resourceLabels(mod.Name, "targetKernel", "podType")
		Expect(labels).To(Equal(expected))
	})
})
