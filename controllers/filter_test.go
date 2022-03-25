package controllers_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/qbarrand/oot-operator/controllers"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

var _ = Describe("SkipDeletions", func() {
	It("should return false for delete events", func() {
		Expect(
			controllers.SkipDeletions().Delete(event.DeleteEvent{}),
		).To(
			BeFalse(),
		)
	})
})
