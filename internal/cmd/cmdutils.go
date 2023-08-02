package cmd

import (
	"errors"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/go-logr/logr"
)

func FatalError(l logr.Logger, err error, msg string, fields ...interface{}) {
	l.Error(err, msg, fields...)
	os.Exit(1)
}

func GetEnvOrFatalError(name string, logger logr.Logger) string {
	val := os.Getenv(name)
	if val == "" {
		FatalError(logger, errors.New("empty value"), "Could not get the environment variable", "name", name)
	}

	return val
}

func GitCommit() (string, error) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", errors.New("build info is not available")
	}

	const vcsRevisionKey = "vcs.revision"

	for _, s := range bi.Settings {
		if s.Key == vcsRevisionKey {
			return s.Value, nil
		}
	}

	return "", fmt.Errorf("%s not found in build info settings", vcsRevisionKey)
}
