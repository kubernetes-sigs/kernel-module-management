package utils

import (
	"fmt"
	"strings"

	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
)

func GetModuleVersionLabelName(namespace, name string) string {
	return fmt.Sprintf("%s.%s.%s", constants.ModuleVersionLabelPrefix, namespace, name)
}

func GetModuleLoaderVersionLabelName(namespace, name string) string {
	return fmt.Sprintf("%s.%s.%s", constants.ModuleLoaderVersionLabelPrefix, namespace, name)
}

func GetDevicePluginVersionLabelName(namespace, name string) string {
	return fmt.Sprintf("%s.%s.%s", constants.DevicePluginVersionLabelPrefix, namespace, name)
}

func GetNamespaceNameFromVersionLabel(label string) (string, string, error) {
	parts := strings.Split(label, ".")
	if len(parts) < 2 {
		return "", "", fmt.Errorf("label %s is in incorrect format", label)
	}
	return parts[len(parts)-2], parts[len(parts)-1], nil
}

func IsModuleVersionLabel(label string) bool {
	return strings.HasPrefix(label, constants.ModuleVersionLabelPrefix)
}

func IsModuleLoaderVersionLabel(label string) bool {
	return strings.HasPrefix(label, constants.ModuleLoaderVersionLabelPrefix)
}

func IsDevicePluginVersionLabel(label string) bool {
	return strings.HasPrefix(label, constants.DevicePluginVersionLabelPrefix)
}
