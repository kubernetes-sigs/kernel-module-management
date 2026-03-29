package module

import (
	v1 "k8s.io/api/core/v1"
	"strings"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
)

var InternalTolerations = []v1.Toleration{
	{
		Key:      v1.TaintNodeDiskPressure,
		Operator: v1.TolerationOpExists,
		Effect:   v1.TaintEffectNoSchedule,
	},
	{
		Key:      v1.TaintNodeMemoryPressure,
		Operator: v1.TolerationOpExists,
		Effect:   v1.TaintEffectNoSchedule,
	},
	{
		Key:      v1.TaintNodePIDPressure,
		Operator: v1.TolerationOpExists,
		Effect:   v1.TaintEffectNoSchedule,
	},
}

// AppendToTag adds the specified tag to the image name cleanly, i.e. by avoiding messing up
// the name or getting "name:-tag"
func AppendToTag(name string, tag string) string {
	separator := ":"
	if strings.Contains(name, ":") {
		separator = "_"
	}
	return name + separator + tag
}

// ShouldBeBuilt indicates whether the specified ModuleLoaderData of the
// Module should be built or not.
func ShouldBeBuilt(mld *api.ModuleLoaderData) bool {
	return mld.Build != nil
}
