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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
)

var _ = Describe("deletePluginObjects", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
	})

	ctx := context.Background()

	It("pins the object it read, so a replacement under the same name is left alone", func() {
		ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns", Name: "ds", UID: "the-uid", ResourceVersion: "11",
		}}

		clnt.EXPECT().Delete(ctx, ds, gomock.Any()).DoAndReturn(
			func(_ context.Context, _ ctrlclient.Object, opts ...ctrlclient.DeleteOption) error {
				do := &ctrlclient.DeleteOptions{}
				for _, opt := range opts {
					opt.ApplyToDelete(do)
				}
				Expect(*do.Preconditions.UID).To(BeEquivalentTo("the-uid"))
				Expect(*do.Preconditions.ResourceVersion).To(Equal("11"))
				Expect(*do.PropagationPolicy).To(Equal(metav1.DeletePropagationForeground))
				return nil
			},
		)

		count, err := deletePluginObjects(ctx, clnt, []ctrlclient.Object{ds})
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(1))
	})

	It("leaves an object that is already going alone, and still counts it", func() {
		now := metav1.Now()
		dc := &resourcev1.DeviceClass{ObjectMeta: metav1.ObjectMeta{
			Name: "class", UID: "dc-uid", ResourceVersion: "20",
			DeletionTimestamp: &now,
			Finalizers:        []string{"tests.kmm.sigs.x-k8s.io/hold"},
		}}

		// No Delete expectation: asking again writes to the object and wakes the controller.
		count, err := deletePluginObjects(ctx, clnt, []ctrlclient.Object{dc})
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(1))
	})

	It("asks only for the ones that are not going yet, and counts both", func() {
		now := metav1.Now()
		going := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{
			Namespace: "ns", Name: "going", DeletionTimestamp: &now,
			Finalizers: []string{"tests.kmm.sigs.x-k8s.io/hold"},
		}}
		fresh := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "fresh"}}

		clnt.EXPECT().Delete(ctx, fresh, gomock.Any()).Return(nil)

		count, err := deletePluginObjects(ctx, clnt, []ctrlclient.Object{going, fresh})
		Expect(err).NotTo(HaveOccurred())
		Expect(count).To(Equal(2))
	})

	It("does not fail over an object that has already gone", func() {
		ds := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "ds"}}

		clnt.EXPECT().Delete(ctx, ds, gomock.Any()).
			Return(apierrors.NewNotFound(schema.GroupResource{}, "ds"))

		_, err := deletePluginObjects(ctx, clnt, []ctrlclient.Object{ds})
		Expect(err).NotTo(HaveOccurred())
	})

	It("goes on to the rest when one delete fails", func() {
		first := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "first"}}
		second := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Namespace: "ns", Name: "second"}}

		clnt.EXPECT().Delete(ctx, first, gomock.Any()).Return(fmt.Errorf("some error"))
		clnt.EXPECT().Delete(ctx, second, gomock.Any()).Return(nil)

		_, err := deletePluginObjects(ctx, clnt, []ctrlclient.Object{first, second})
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("pluginPodsRemain", func() {
	const (
		namespace  = "ns"
		moduleName = "mod"
	)

	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
	})

	ctx := context.Background()

	isDRA := func(role string) bool { return role == constants.DRARoleLabelValue }

	pod := func(role, ownerKind string, phase v1.PodPhase) v1.Pod {
		p := v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: namespace,
				Name:      "pod",
				Labels: map[string]string{
					constants.ModuleNameLabel: moduleName,
					constants.DaemonSetRole:   role,
				},
			},
			Status: v1.PodStatus{Phase: phase},
		}
		if ownerKind != "" {
			p.OwnerReferences = []metav1.OwnerReference{{
				Kind: ownerKind, Name: "owner", Controller: ptr.To(true),
			}}
		}
		return p
	}

	listing := func(pods ...v1.Pod) {
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, l ctrlclient.ObjectList, _ ...ctrlclient.ListOption) error {
				l.(*v1.PodList).Items = pods
				return nil
			},
		)
	}

	DescribeTable("what still counts as a pod that is there",
		func(p v1.Pod, want bool) {
			listing(p)

			remain, err := pluginPodsRemain(ctx, clnt, namespace, moduleName, isDRA)
			Expect(err).NotTo(HaveOccurred())
			Expect(remain).To(Equal(want))
		},
		Entry("running", pod(constants.DRARoleLabelValue, "DaemonSet", v1.PodRunning), true),
		Entry("succeeded", pod(constants.DRARoleLabelValue, "DaemonSet", v1.PodSucceeded), true),
		Entry("failed", pod(constants.DRARoleLabelValue, "DaemonSet", v1.PodFailed), true),
		Entry("nothing owns it", pod(constants.DRARoleLabelValue, "", v1.PodRunning), false),
		Entry("owned by something other than a DaemonSet", pod(constants.DRARoleLabelValue, "ReplicaSet", v1.PodRunning), false),
		Entry("another plugin's", pod(constants.DevicePluginRoleLabelValue, "DaemonSet", v1.PodRunning), false),
	)

	It("reports a read failure rather than an empty cluster", func() {
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		_, err := pluginPodsRemain(ctx, clnt, namespace, moduleName, isDRA)
		Expect(err).To(HaveOccurred())
	})
})
