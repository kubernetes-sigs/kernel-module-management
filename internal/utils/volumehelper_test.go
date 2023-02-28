package utils

import (
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
)

var _ = Describe("MakeSecretVolumeMount", func() {
	It("should return a valid volumeMount", func() {
		signConfig := &kmmv1beta1.Sign{
			CertSecret: &v1.LocalObjectReference{Name: "securebootcert"},
		}
		secretMount := v1.VolumeMount{
			Name:      "secret-securebootcert",
			ReadOnly:  true,
			MountPath: "/signingcert",
		}

		volMount := MakeSecretVolumeMount(signConfig.CertSecret, "/signingcert", true)
		Expect(volMount).To(Equal(secretMount))
	})

	It("should return an empty volumeMount if signConfig is empty", func() {
		Expect(
			MakeSecretVolumeMount(nil, "/signingcert", true),
		).To(
			Equal(v1.VolumeMount{}),
		)
	})
})
