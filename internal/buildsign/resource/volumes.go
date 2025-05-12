package resource

import (
	"fmt"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	v1 "k8s.io/api/core/v1"
)

func makeBuildResourceVolumesAndVolumeMounts(buildConfig kmmv1beta1.Build,
	imageRepoSecret *v1.LocalObjectReference) ([]v1.Volume, []v1.VolumeMount) {

	volumes := []v1.Volume{
		{
			Name: dockerfileVolumeName,
			VolumeSource: v1.VolumeSource{
				ConfigMap: &v1.ConfigMapVolumeSource{
					LocalObjectReference: *buildConfig.DockerfileConfigMap,
					Items: []v1.KeyToPath{
						{
							Key:  constants.DockerfileCMKey,
							Path: "Dockerfile",
						},
					},
				},
			},
		},
	}

	for _, secretRef := range buildConfig.Secrets {
		vol := v1.Volume{
			Name: "secret-" + secretRef.Name,
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: secretRef.Name,
				},
			},
		}
		volumes = append(volumes, vol)
	}

	volumeMounts := []v1.VolumeMount{
		{
			Name:      dockerfileVolumeName,
			ReadOnly:  true,
			MountPath: "/workspace",
		},
	}

	for _, secretRef := range buildConfig.Secrets {
		volMount := v1.VolumeMount{
			Name:      "secret-" + secretRef.Name,
			ReadOnly:  true,
			MountPath: "/run/secrets/" + secretRef.Name,
		}

		volumeMounts = append(volumeMounts, volMount)
	}

	if imageRepoSecret != nil {

		volumes = append(volumes,
			v1.Volume{
				Name: "secret-" + imageRepoSecret.Name,
				VolumeSource: v1.VolumeSource{
					Secret: &v1.SecretVolumeSource{
						SecretName: imageRepoSecret.Name,
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

		volumeMounts = append(volumeMounts,
			v1.VolumeMount{
				Name:      "secret-" + imageRepoSecret.Name,
				ReadOnly:  true,
				MountPath: "/kaniko/.docker",
			},
		)
	}

	return volumes, volumeMounts
}

func makeSignResourceVolumesAndVolumeMounts(signConfig *kmmv1beta1.Sign,
	imageRepoSecret *v1.LocalObjectReference) ([]v1.Volume, []v1.VolumeMount) {

	volumes := []v1.Volume{
		{
			Name: "secret-" + signConfig.CertSecret.Name,
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: signConfig.CertSecret.Name,
					Items: []v1.KeyToPath{
						{
							Key:  "cert",
							Path: "cert.pem",
						},
					},
				},
			},
		},
		{
			Name: "secret-" + signConfig.KeySecret.Name,
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: signConfig.KeySecret.Name,
					Items: []v1.KeyToPath{
						{
							Key:  "key",
							Path: "key.pem",
						},
					},
				},
			},
		},
		{
			Name: dockerfileVolumeName,
			VolumeSource: v1.VolumeSource{
				DownwardAPI: &v1.DownwardAPIVolumeSource{
					Items: []v1.DownwardAPIVolumeFile{
						{
							Path: "Dockerfile",
							FieldRef: &v1.ObjectFieldSelector{
								FieldPath: fmt.Sprintf("metadata.annotations['%s']", dockerfileAnnotationKey),
							},
						},
					},
				},
			},
		},
	}

	volumeMounts := []v1.VolumeMount{
		{
			Name:      "secret-" + signConfig.CertSecret.Name,
			ReadOnly:  true,
			MountPath: "/run/secrets/cert",
		},
		{
			Name:      "secret-" + signConfig.KeySecret.Name,
			ReadOnly:  true,
			MountPath: "/run/secrets/key",
		},
		{
			Name:      dockerfileVolumeName,
			ReadOnly:  true,
			MountPath: "/workspace",
		},
	}

	if imageRepoSecret != nil {

		volumes = append(
			volumes,
			v1.Volume{
				Name: "secret-" + imageRepoSecret.Name,
				VolumeSource: v1.VolumeSource{
					Secret: &v1.SecretVolumeSource{
						SecretName: imageRepoSecret.Name,
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

		volumeMounts = append(
			volumeMounts,
			v1.VolumeMount{
				Name:      "secret-" + imageRepoSecret.Name,
				ReadOnly:  true,
				MountPath: "/kaniko/.docker",
			},
		)
	}

	return volumes, volumeMounts
}
