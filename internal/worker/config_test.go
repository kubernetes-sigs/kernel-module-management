package worker_test

import (
	"github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/worker"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ConfigHelper_ReadConfigFile", func() {
	It("should populate the ModuleConfig correctly", func() {
		cfg, err := worker.NewConfigHelper().ReadConfigFile("testdata/config.yaml")
		Expect(err).NotTo(HaveOccurred())

		expected := v1beta1.ModuleConfig{
			ContainerImage:        "registry.local/org/img:tag",
			InsecurePull:          true,
			InTreeModulesToRemove: []string{"intree1", "intree2"},
			Modprobe: v1beta1.ModprobeSpec{
				ModuleName: "test",
				Parameters: []string{"key0=value0", "key1=value1"},
				DirName:    "/path",
				Args: &v1beta1.ModprobeArgs{
					Load:   []string{"load", "args"},
					Unload: []string{"unload", "args"},
				},
				RawArgs: &v1beta1.ModprobeArgs{
					Load:   []string{"load", "rawargs"},
					Unload: []string{"unload", "rawargs"},
				},
				FirmwarePath:        "/some/path",
				ModulesLoadingOrder: []string{"mod1", "mod2"},
			},
		}

		Expect(*cfg).To(BeComparableTo(expected))
	})
})
