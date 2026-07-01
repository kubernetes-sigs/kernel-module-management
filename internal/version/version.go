/*
Copyright 2022.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package version

import (
	"fmt"
	"regexp"
	"strconv"

	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

var kubeVersionRe = regexp.MustCompile(`^v?(\d+)\.(\d+)`)

// KubeVersion holds the parsed major and minor version of a Kubernetes cluster.
type KubeVersion struct {
	Major int
	Minor int
}

// AtLeast returns true if kv is greater than or equal to the given major.minor version.
func (kv KubeVersion) AtLeast(major, minor int) bool {
	return kv.Major > major || (kv.Major == major && kv.Minor >= minor)
}

// DiscoverKubeVersion queries the Kubernetes API server and returns its version.
func DiscoverKubeVersion(cfg *rest.Config) (KubeVersion, error) {
	discClient, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return KubeVersion{}, fmt.Errorf("failed to create discovery client: %v", err)
	}

	serverVersion, err := discClient.ServerVersion()
	if err != nil {
		return KubeVersion{}, fmt.Errorf("failed to query Kubernetes server version: %v", err)
	}

	return ParseKubeVersion(serverVersion.GitVersion)
}

// ParseKubeVersion extracts the major and minor version from a Kubernetes
// GitVersion string such as "v1.34.0" or "v1.34.0+k3s1".
func ParseKubeVersion(gitVersion string) (KubeVersion, error) {
	m := kubeVersionRe.FindStringSubmatch(gitVersion)
	if m == nil {
		return KubeVersion{}, fmt.Errorf("cannot parse Kubernetes version from %q", gitVersion)
	}

	major, err := strconv.Atoi(m[1])
	if err != nil {
		return KubeVersion{}, fmt.Errorf("cannot parse Kubernetes major version from %q: %w", gitVersion, err)
	}
	minor, err := strconv.Atoi(m[2])
	if err != nil {
		return KubeVersion{}, fmt.Errorf("cannot parse Kubernetes minor version from %q: %w", gitVersion, err)
	}
	return KubeVersion{Major: major, Minor: minor}, nil
}
