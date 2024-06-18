package utils

import (
	"fmt"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/selection"
	"regexp"
	"strings"

	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
)

var reKernelModuleReadyLabel = regexp.MustCompile(`^kmm\.node\.kubernetes\.io/([a-zA-Z0-9-]+)\.([a-zA-Z0-9-]+)\.ready$`)
var reDeprecatedKernelModuleReadyLabel = regexp.MustCompile(`^kmm\.node\.kubernetes\.io/[a-zA-Z0-9-]+\.ready$`)

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

func GetKernelModuleReadyNodeLabel(namespace, moduleName string) string {
	return fmt.Sprintf("kmm.node.kubernetes.io/%s.%s.ready", namespace, moduleName)
}

func GetDevicePluginNodeLabel(namespace, moduleName string) string {
	return fmt.Sprintf("kmm.node.kubernetes.io/%s.%s.device-plugin-ready", namespace, moduleName)
}

func IsDeprecatedKernelModuleReadyNodeLabel(label string) bool {
	return reDeprecatedKernelModuleReadyLabel.MatchString(label)
}

func IsKernelModuleReadyNodeLabel(label string) (bool, string, string) {
	matches := reKernelModuleReadyLabel.FindStringSubmatch(label)

	if len(matches) != 3 {
		return false, "", ""
	}

	return true, matches[1], matches[2]
}

func IsObjectSelectedByLabels(objectLabels map[string]string, selectorLabels map[string]string) (bool, error) {
	objectLabelsSet := labels.Set(objectLabels)
	sel := labels.NewSelector()

	for k, v := range selectorLabels {
		requirement, err := labels.NewRequirement(k, selection.Equals, []string{v})
		if err != nil {
			return false, fmt.Errorf("failed to create new label requirements: %v", err)
		}
		sel = sel.Add(*requirement)
	}

	return sel.Matches(objectLabelsSet), nil
}
