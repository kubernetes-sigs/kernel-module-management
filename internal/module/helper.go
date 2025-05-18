package module

import (
	"strings"

	"github.com/kubernetes-sigs/kernel-module-management/internal/api"
)

// AppendToTag adds the specified tag to the image name cleanly, i.e. by avoiding messing up
// the name or getting "name:-tag"
func AppendToTag(name string, tag string) string {
	separator := ":"
	if strings.Contains(name, ":") {
		separator = "_"
	}
	return name + separator + tag
}

// IntermediateImageName returns the image name of the pre-signed module image name
func IntermediateImageName(name, namespace, targetImage string) string {
	return AppendToTag(targetImage, namespace+"_"+name+"_kmm_unsigned")
}

// ShouldBeBuilt indicates whether the specified ModuleLoaderData of the
// Module should be built or not.
func ShouldBeBuilt(mld *api.ModuleLoaderData) bool {
	return mld.Build != nil
}

// ShouldBeSigned indicates whether the specified ModuleLoaderData of the
// Module should be signed or not.
func ShouldBeSigned(mld *api.ModuleLoaderData) bool {
	return mld.Sign != nil
}
