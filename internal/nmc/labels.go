package nmc

import (
	"fmt"
	"regexp"
)

var (
	reConfiguredLabel = regexp.MustCompile(`^beta\.kmm\.node\.kubernetes\.io/([a-zA-Z0-9-]+)\.([a-zA-Z0-9-\.]+)\.module-configured$`)
	reInUseLabel      = regexp.MustCompile(`^beta\.kmm\.node\.kubernetes\.io/([a-zA-Z0-9-]+)\.([a-zA-Z0-9-\.]+)\.module-in-use$`)
)

func IsModuleConfiguredLabel(s string) (bool, string, string) {
	res := reConfiguredLabel.FindStringSubmatch(s)

	if len(res) != 3 {
		return false, "", ""
	}

	return true, res[1], res[2]
}

func IsModuleInUseLabel(s string) (bool, string, string) {
	res := reInUseLabel.FindStringSubmatch(s)

	if len(res) != 3 {
		return false, "", ""
	}

	return true, res[1], res[2]
}

func ModuleConfiguredLabel(namespace, name string) string {
	return fmt.Sprintf("beta.kmm.node.kubernetes.io/%s.%s.module-configured", namespace, name)
}

func ModuleInUseLabel(namespace, name string) string {
	return fmt.Sprintf("beta.kmm.node.kubernetes.io/%s.%s.module-in-use", namespace, name)
}
