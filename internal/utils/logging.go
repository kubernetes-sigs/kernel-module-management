package utils

import "fmt"

func WarnString(str string) string {
	return fmt.Sprintf("WARNING: %s", str)
}
