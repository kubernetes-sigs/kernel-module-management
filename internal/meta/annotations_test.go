package meta

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var _ = Describe("SetAnnotation", func() {
	const key = "test-key"

	DescribeTable(
		"should work as expected",
		func(annotations map[string]string, key, value string) {
			obj := &unstructured.Unstructured{}

			obj.SetAnnotations(annotations)

			SetAnnotation(obj, key, value)

			Expect(
				obj.GetAnnotations(),
			).To(
				HaveKeyWithValue(key, value),
			)
		},
		Entry("nil annotations", nil, key, "test value"),
		Entry("empty annotations", make(map[string]string), key, "test value"),
		Entry("existing annotation", map[string]string{key: "some-other-value"}, key, "test value"),
	)
})
