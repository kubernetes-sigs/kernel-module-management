package module

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
)

var _ = Describe("AppendToTag", func() {
	It("should append a tag to the image name", func() {
		name := "some-container-image-name"
		tag := "a-kmm-tag"

		Expect(
			AppendToTag(name, tag),
		).To(
			Equal(name + ":" + tag),
		)
	})

	It("should add a suffix to the already present tag", func() {
		name := "some-container-image-name:with-a-tag"
		tag := "a-kmm-tag-suffix"

		Expect(
			AppendToTag(name, tag),
		).To(
			Equal(name + "_" + tag),
		)
	})
})

var _ = Describe("EffectiveTolerations", func() {
	It("should append the internal tolerations to the user's", func() {
		userToleration := v1.Toleration{
			Key:      v1.TaintNodeUnschedulable,
			Operator: v1.TolerationOpExists,
			Effect:   v1.TaintEffectNoSchedule,
		}

		res := EffectiveTolerations([]v1.Toleration{userToleration})

		Expect(res).To(HaveLen(1 + len(InternalTolerations)))
		Expect(res[0]).To(Equal(userToleration))
		Expect(res).To(ContainElements(InternalTolerations))
	})

	It("should work with no user tolerations", func() {
		Expect(EffectiveTolerations(nil)).To(Equal(InternalTolerations))
	})

	It("should not write into the slice it is given", func() {
		userTolerations := make([]v1.Toleration, 1, 8)
		userTolerations[0] = v1.Toleration{Key: "dedicated"}

		_ = EffectiveTolerations(userTolerations)

		Expect(userTolerations).To(HaveLen(1))
		Expect(userTolerations[:cap(userTolerations)][1]).To(Equal(v1.Toleration{}))
	})
})
