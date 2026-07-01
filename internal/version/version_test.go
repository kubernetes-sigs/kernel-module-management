/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package version

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Version Suite")
}

var _ = Describe("AtLeast", func() {
	DescribeTable(
		"should compare versions correctly",
		func(major, minor, minMajor, minMinor int, expected bool) {
			kv := KubeVersion{Major: major, Minor: minor}
			Expect(kv.AtLeast(minMajor, minMinor)).To(Equal(expected))
		},
		Entry("exact match", 1, 34, 1, 34, true),
		Entry("higher minor", 1, 35, 1, 34, true),
		Entry("lower minor", 1, 33, 1, 34, false),
		Entry("higher major, lower minor", 2, 0, 1, 34, true),
		Entry("lower major", 0, 99, 1, 34, false),
	)
})

var _ = Describe("ParseKubeVersion", func() {
	DescribeTable(
		"should parse valid version strings",
		func(gitVersion string, expectedMajor, expectedMinor int) {
			kv, err := ParseKubeVersion(gitVersion)
			Expect(err).NotTo(HaveOccurred())
			Expect(kv.Major).To(Equal(expectedMajor))
			Expect(kv.Minor).To(Equal(expectedMinor))
		},
		Entry("standard version", "v1.34.0", 1, 34),
		Entry("with build metadata", "v1.34.0+k3s1", 1, 34),
		Entry("pre-release version", "v1.33.0-alpha.1", 1, 33),
		Entry("higher minor", "v1.35.1", 1, 35),
		Entry("without v prefix", "1.34.0", 1, 34),
		Entry("hypothetical k8s 2.0", "v2.0.0", 2, 0),
		Entry("hypothetical k8s 2.5", "v2.5.3", 2, 5),
	)

	DescribeTable(
		"should return error for invalid strings",
		func(gitVersion string) {
			_, err := ParseKubeVersion(gitVersion)
			Expect(err).To(HaveOccurred())
		},
		Entry("empty string", ""),
		Entry("garbage", "invalid"),
		Entry("no minor", "v1"),
	)
})
