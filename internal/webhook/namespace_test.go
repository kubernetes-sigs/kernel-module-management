package webhook

import (
	"context"

	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var validator = &NamespaceValidator{}

var _ = Describe("NamespaceDeletion_ValidateCreate", func() {
	It("should always error", func() {
		_, err := validator.ValidateCreate(context.TODO(), nil)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("NamespaceDeletion_ValidateDelete", func() {
	ctx := context.TODO()
	validator := validator
	It("should return an error if the label is present", func() {
		ns := v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{constants.NamespaceLabelKey: ""},
			},
		}

		_, err := validator.ValidateDelete(ctx, &ns)
		Expect(err).To(HaveOccurred())
	})

	It("should return nil if the label is not present", func() {
		ns := v1.Namespace{}

		_, err := validator.ValidateDelete(ctx, &ns)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("NamespaceDeletion_ValidateUpdate", func() {
	It("should always error", func() {
		_, err := validator.ValidateUpdate(context.TODO(), nil, nil)
		Expect(err).To(HaveOccurred())
	})
})
