package kernel

import "strings"

func replaceInvalidChar(r rune) rune {
	if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' && r != '.' && r != '_' {
		return '_'
	}

	return r
}

func NormalizeVersion(version string) string {
	return strings.Map(replaceInvalidChar, version)
}
