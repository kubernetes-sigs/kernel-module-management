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

package controllers

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kubernetes-sigs/kernel-module-management/internal/test"
	//+kubebuilder:scaffold:imports
)

var scheme *runtime.Scheme

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	var err error

	scheme, err = test.TestScheme()
	Expect(err).NotTo(HaveOccurred())

	RunSpecs(t, "Controller Suite")
}
