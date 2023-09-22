package meta

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var _ = Describe("SetLabel", func() {
	const key = "test-key"

	DescribeTable(
		"should work as expected",
		func(labels map[string]string, key, value string) {
			obj := &unstructured.Unstructured{}

			obj.SetLabels(labels)

			SetLabel(obj, key, value)

			Expect(
				obj.GetLabels(),
			).To(
				HaveKeyWithValue(key, value),
			)
		},
		Entry("nil labels", nil, key, "test value"),
		Entry("empty labels", make(map[string]string), key, "test value"),
		Entry("existing label", map[string]string{key: "some-other-value"}, key, "test value"),
	)
})

var _ = Describe("RemoveLabel", func() {
	const key = "test-key"

	DescribeTable(
		"should work as expected",
		func(labels map[string]string, key string) {
			obj := &unstructured.Unstructured{}

			obj.SetLabels(labels)

			RemoveLabel(obj, key)

			Expect(
				obj.GetLabels(),
			).NotTo(
				HaveKey(key),
			)
		},
		Entry("nil labels", nil, key),
		Entry("empty labels", make(map[string]string), key),
		Entry("existing label", map[string]string{key: "some-other-value"}, key),
	)
})
