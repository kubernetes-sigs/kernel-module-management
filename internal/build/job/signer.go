package job

import (
	"fmt"
	"strings"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	batchv1 "k8s.io/api/batch/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

////go:generate mockgen -source=signer.go -package=signer -destination=mock_signer.go

type signer struct {
	name   string
	scheme *runtime.Scheme
}

func NewSigner(scheme *runtime.Scheme) Maker {
	return &signer{name: "sign", scheme: scheme}
}

func (m *signer) GetName() string {
	return m.name
}

func (m *signer) ShouldRun(mod *kmmv1beta1.Module, km *kmmv1beta1.KernelMapping) bool {
	if (mod.Spec.ModuleLoader.Container.Build == nil || mod.Spec.ModuleLoader.Container.Build.Sign == nil) && (km.Build == nil || km.Build.Sign == nil) {
		return false
	}
	return true
}

func (m *signer) MakeJob(mod kmmv1beta1.Module, buildConfig *kmmv1beta1.Build, targetKernel string, previousImage string, containerImage string, pushImage bool) (*batchv1.Job, error) {
	var args []string

	if pushImage {
		args = []string{"-signedimage", containerImage}
	} else {
		args = append(args, "-no-push")
	}

	if previousImage != "" {
		args = append(args, "-unsignedimage", previousImage)
	} else if buildConfig.Sign.UnsignedImage != "" {
		args = append(args, "-unsignedimage", buildConfig.Sign.UnsignedImage)
	} else {
		return nil, fmt.Errorf("no image to sign given")
	}
	args = append(args, "-key", "/signingkey/key.priv")
	args = append(args, "-cert", "/signingcert/public.der")

	if len(buildConfig.Sign.FileList) > 0 {
		args = append(args, "-filestosign", strings.Join(buildConfig.Sign.FileList, ":"))
	}

	fmt.Printf("buildConfig == %#v\n", buildConfig)
	fmt.Printf("buildConfig.Sign == %#v\n", buildConfig.Sign)

	volumes := []v1.Volume{}
	volumeMounts := []v1.VolumeMount{}
	volumes = append(volumes, m.makeImageSigningSecretVolume(buildConfig.Sign.KeySecret, "key", "key.priv"))
	volumes = append(volumes, m.makeImageSigningSecretVolume(buildConfig.Sign.CertSecret, "cert", "public.der"))

	volumeMounts = append(volumeMounts, m.makeImageSigningSecretVolumeMount(buildConfig.Sign.CertSecret, "/signingcert"))
	volumeMounts = append(volumeMounts, m.makeImageSigningSecretVolumeMount(buildConfig.Sign.KeySecret, "/signingkey"))

	if mod.Spec.ImageRepoSecret != nil {
		args = append(args, "-pullsecret", "/docker_config/config.json")
		volumes = append(volumes, m.makeImagePullSecretVolume(mod.Spec.ImageRepoSecret))
		volumeMounts = append(volumeMounts, m.makeImagePullSecretVolumeMount(mod.Spec.ImageRepoSecret))
	}

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: mod.Name + "-" + m.GetName() + "-",
			Namespace:    mod.Namespace,
			Labels:       labels(mod, targetKernel, m.GetName()),
		},
		Spec: batchv1.JobSpec{
			Completions: pointer.Int32(1),
			Template: v1.PodTemplateSpec{
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:         "signimage",
							Image:        "quay.io/chrisp262/kmod-signer:latest",
							Args:         args,
							VolumeMounts: volumeMounts,
						},
					},
					RestartPolicy: v1.RestartPolicyOnFailure,
					Volumes:       volumes,
					NodeSelector:  mod.Spec.Selector,
				},
			},
		},
	}

	if err := controllerutil.SetControllerReference(&mod, job, m.scheme); err != nil {
		return nil, fmt.Errorf("could not set the owner reference: %v", err)
	}

	return job, nil
}

func (m *signer) makeImageSigningSecretVolume(secretRef *v1.LocalObjectReference, key string, path string) v1.Volume {

	return v1.Volume{
		Name: m.volumeNameFromSecretRef(*secretRef),
		VolumeSource: v1.VolumeSource{
			Secret: &v1.SecretVolumeSource{
				SecretName: secretRef.Name,
				Items: []v1.KeyToPath{
					{
						Key:  key,
						Path: path,
					},
				},
			},
		},
	}
}

func (m *signer) makeImageSigningSecretVolumeMount(secretRef *v1.LocalObjectReference, mountpoint string) v1.VolumeMount {

	return v1.VolumeMount{
		Name:      m.volumeNameFromSecretRef(*secretRef),
		ReadOnly:  true,
		MountPath: mountpoint,
	}
}

func (m *signer) makeImagePullSecretVolume(secretRef *v1.LocalObjectReference) v1.Volume {

	if secretRef == nil {
		return v1.Volume{}
	}

	return v1.Volume{
		Name: m.volumeNameFromSecretRef(*secretRef),
		VolumeSource: v1.VolumeSource{
			Secret: &v1.SecretVolumeSource{
				SecretName: secretRef.Name,
				Items: []v1.KeyToPath{
					{
						Key:  v1.DockerConfigJsonKey,
						Path: "config.json",
					},
				},
			},
		},
	}
}

func (m *signer) makeImagePullSecretVolumeMount(secretRef *v1.LocalObjectReference) v1.VolumeMount {

	if secretRef == nil {
		return v1.VolumeMount{}
	}

	return v1.VolumeMount{
		Name:      m.volumeNameFromSecretRef(*secretRef),
		ReadOnly:  true,
		MountPath: "/docker_config",
	}
}

func (m *signer) volumeNameFromSecretRef(ref v1.LocalObjectReference) string {
	return "secret-" + ref.Name
}
