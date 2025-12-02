package labels

import (
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
)

func GetKernelModuleReadyNodeLabel(namespace, moduleName string) string {
	return utils.GetKernelModuleReadyNodeLabel(namespace, moduleName)
}

func GetDevicePluginNodeLabel(namespace, moduleName string) string {
	return utils.GetDevicePluginNodeLabel(namespace, moduleName)
}

func GetModuleVersionLabelName(namespace, name string) string {
	return utils.GetModuleVersionLabelName(namespace, name)
}

func GetKernelModuleVersionReadyNodeLabel(namespace, moduleName string) string {
	return utils.GetKernelModuleVersionReadyNodeLabel(namespace, moduleName)
}
