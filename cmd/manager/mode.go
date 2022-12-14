package main

import (
	"errors"
	"fmt"
)

func defaultMode() (*mode, error) {
	m := mode{}

	return &m, m.Set("kmm")
}

type mode struct {
	value string

	builds                                 bool
	signs                                  bool
	startsManagedClusterModuleReconciler   bool
	startsModuleReconciler                 bool
	startsNodeKernelClusterClaimReconciler bool
	startsNodeKernelReconciler             bool
	startsPodNodeModuleReconciler          bool
	startsPreflightValidationReconciler    bool
}

func (m *mode) Set(s string) error {
	*m = mode{}

	m.value = s

	switch m.value {
	case "kmm":
		m.builds = true
		m.signs = true
		m.startsModuleReconciler = true
		m.startsNodeKernelReconciler = true
		m.startsPodNodeModuleReconciler = true
		m.startsPreflightValidationReconciler = true
	case "hub":
		m.builds = true
		m.signs = true
		m.startsManagedClusterModuleReconciler = true
	case "spoke":
		m.builds = false
		m.signs = false
		m.startsModuleReconciler = true
		m.startsNodeKernelClusterClaimReconciler = true
		m.startsNodeKernelReconciler = true
		m.startsPodNodeModuleReconciler = true
	case "":
		return errors.New("value required")
	default:
		return fmt.Errorf("%q: invalid mode", s)
	}

	return nil
}

func (m *mode) String() string {
	return m.value
}
