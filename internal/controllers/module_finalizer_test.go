/*
Copyright 2023.

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
	"context"
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
)

var _ = Describe("setModuleFinalizer", func() {
	const foreign = "example.com/someone-else"

	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		mod  *kmmv1beta1.Module
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mod = &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:       "ns",
				Name:            "mod",
				ResourceVersion: "42",
				Finalizers:      []string{constants.ModuleFinalizer, foreign},
			},
		}
	})

	ctx := context.Background()

	// payloadOf runs the patch the way the client would and hands back what it would send.
	payloadOf := func() *[]byte {
		var sent []byte
		clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, o ctrlclient.Object, p ctrlclient.Patch, _ ...ctrlclient.PatchOption) error {
				data, err := p.Data(o)
				Expect(err).NotTo(HaveOccurred())
				sent = data
				return nil
			},
		)
		return &sent
	}

	It("writes nothing when the finalizer is already as asked for", func() {
		changed, err := setModuleFinalizer(ctx, clnt, mod, constants.ModuleFinalizer, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeFalse())
	})

	It("locks the write to the version it read, so a concurrent finalizer is not dropped", func() {
		sent := payloadOf()

		changed, err := setModuleFinalizer(ctx, clnt, mod, constants.DRAFinalizer, true)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeTrue())

		var patch struct {
			Metadata struct {
				ResourceVersion string   `json:"resourceVersion"`
				Finalizers      []string `json:"finalizers"`
			} `json:"metadata"`
		}
		Expect(json.Unmarshal(*sent, &patch)).To(Succeed())
		Expect(patch.Metadata.ResourceVersion).To(Equal("42"))
		Expect(patch.Metadata.Finalizers).To(ContainElements(constants.ModuleFinalizer, foreign, constants.DRAFinalizer))
	})

	It("leaves the other finalizers in place when it removes its own", func() {
		mod.Finalizers = append(mod.Finalizers, constants.DevicePluginFinalizer)
		sent := payloadOf()

		_, err := setModuleFinalizer(ctx, clnt, mod, constants.DevicePluginFinalizer, false)
		Expect(err).NotTo(HaveOccurred())

		Expect(string(*sent)).To(ContainSubstring(foreign))
		Expect(string(*sent)).To(ContainSubstring(constants.ModuleFinalizer))
		Expect(string(*sent)).NotTo(ContainSubstring(constants.DevicePluginFinalizer))
		Expect(mod.Finalizers).NotTo(ContainElement(constants.DevicePluginFinalizer))
	})

	It("does not report protection the API server refused", func() {
		clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("conflict"))

		changed, err := setModuleFinalizer(ctx, clnt, mod, constants.DRAFinalizer, true)
		Expect(err).To(HaveOccurred())
		Expect(changed).To(BeFalse())
		Expect(mod.Finalizers).NotTo(ContainElement(constants.DRAFinalizer))
	})

	It("refuses to add one to a module that is already going", func() {
		now := metav1.Now()
		mod.DeletionTimestamp = &now

		// No Patch: the API server would reject it anyway.
		_, err := setModuleFinalizer(ctx, clnt, mod, constants.DRAFinalizer, true)
		Expect(err).To(HaveOccurred())
	})

	It("does not report protection on a module that has gone", func() {
		clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).
			Return(apierrors.NewNotFound(schema.GroupResource{}, "mod"))

		changed, err := setModuleFinalizer(ctx, clnt, mod, constants.DRAFinalizer, true)
		Expect(err).To(HaveOccurred())
		Expect(changed).To(BeFalse())
		Expect(mod.Finalizers).NotTo(ContainElement(constants.DRAFinalizer))
	})

	It("treats a module that has already gone as done", func() {
		clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).
			Return(apierrors.NewNotFound(schema.GroupResource{}, "mod"))

		changed, err := setModuleFinalizer(ctx, clnt, mod, constants.ModuleFinalizer, false)
		Expect(err).NotTo(HaveOccurred())
		Expect(changed).To(BeFalse())
	})
})
