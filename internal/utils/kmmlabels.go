package utils

import (
	"fmt"
	"strings"

	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
)

func GetModuleVersionLabelName(namespace, name string) string {
	return fmt.Sprintf("%s.%s.%s", constants.ModuleVersionLabelPrefix, namespace, name)
}

func GetWorkerPodVersionLabelName(namespace, name string) string {
	return fmt.Sprintf("%s.%s.%s", constants.WorkerPodVersionLabelPrefix, namespace, name)
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

func IsVersionLabel(label string) bool {
	return IsModuleVersionLabel(label) || IsWorkerPodVersionLabel(label) || IsDevicePluginVersionLabel(label)
}

func IsModuleVersionLabel(label string) bool {
	return strings.HasPrefix(label, constants.ModuleVersionLabelPrefix)
}

func IsWorkerPodVersionLabel(label string) bool {
	return strings.HasPrefix(label, constants.WorkerPodVersionLabelPrefix)
}

func IsDevicePluginVersionLabel(label string) bool {
	return strings.HasPrefix(label, constants.DevicePluginVersionLabelPrefix)
}

func GetNodesVersionLabels(nodeLabels map[string]string) map[string]string {
	versionLabels := map[string]string{}
	for label, labelValue := range nodeLabels {
		if strings.HasPrefix(label, constants.WorkerPodVersionLabelPrefix) ||
			strings.HasPrefix(label, constants.DevicePluginVersionLabelPrefix) ||
			strings.HasPrefix(label, constants.ModuleVersionLabelPrefix) {
			versionLabels[label] = labelValue
		}
	}
	return versionLabels
}

func GetNodeWorkerPodVersionLabel(nodeLabels map[string]string, namespace, name string) (string, bool) {
	if nodeLabels == nil {
		return "", false
	}
	labelValue, ok := nodeLabels[GetWorkerPodVersionLabelName(namespace, name)]
	if !ok {
		return "", false
	}
	return labelValue, true
}
