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
	"context"
	"fmt"

	"github.com/google/go-cmp/cmp"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/node"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	resourcev1 "k8s.io/api/resource/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("DRAReconciler_Reconcile", func() {
	const draModuleName = "dra-module"

	var (
		ctrl            *gomock.Controller
		mockReconHelper *MockdraReconcilerHelperAPI
		mod             *kmmv1beta1.Module
		dr              *DRAReconciler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockReconHelper = NewMockdraReconcilerHelperAPI(ctrl)

		mod = &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: draModuleName},
			Spec: kmmv1beta1.ModuleSpec{
				DRA: &kmmv1beta1.DRASpec{},
			},
		}

		dr = &DRAReconciler{
			reconHelperAPI: mockReconHelper,
		}
	})

	ctx := context.Background()

	DescribeTable("check error flows", func(getDSError, getDCError, handleDRAError, gcError, handleDCError bool) {
		draDS := []appsv1.DaemonSet{{}}
		returnedError := fmt.Errorf("some error")
		if getDSError {
			mockReconHelper.EXPECT().getModuleDRADaemonSets(ctx, mod.Name, mod.Namespace).Return(nil, returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().getModuleDRADaemonSets(ctx, mod.Name, mod.Namespace).Return(draDS, nil)
		if getDCError {
			mockReconHelper.EXPECT().getModuleDeviceClasses(ctx, mod.Name, mod.Namespace).Return(nil, returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().getModuleDeviceClasses(ctx, mod.Name, mod.Namespace).Return(nil, nil)
		if handleDRAError {
			mockReconHelper.EXPECT().handleDRA(ctx, mod, draDS).Return(returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().handleDRA(ctx, mod, draDS).Return(nil)
		if gcError {
			mockReconHelper.EXPECT().garbageCollectDRADaemonSets(ctx, mod, draDS).Return(returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().garbageCollectDRADaemonSets(ctx, mod, draDS).Return(nil)
		if handleDCError {
			mockReconHelper.EXPECT().handleDeviceClasses(ctx, mod, []resourcev1.DeviceClass(nil)).Return(returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().handleDeviceClasses(ctx, mod, []resourcev1.DeviceClass(nil)).Return(nil)
		mockReconHelper.EXPECT().moduleUpdateDRAStatus(ctx, mod, draDS).Return(returnedError)

	executeTestFunction:
		res, err := dr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).To(HaveOccurred())

	},
		Entry("getModuleDRADaemonSets failed", true, false, false, false, false),
		Entry("getModuleDeviceClasses failed", false, true, false, false, false),
		Entry("handleDRA failed", false, false, true, false, false),
		Entry("garbageCollectDRADaemonSets failed", false, false, false, true, false),
		Entry("handleDeviceClasses failed", false, false, false, false, true),
		Entry("moduleUpdateDRAStatus failed", false, false, false, false, false),
	)

	It("Good flow", func() {
		draDS := []appsv1.DaemonSet{{}}
		gomock.InOrder(
			mockReconHelper.EXPECT().getModuleDRADaemonSets(ctx, mod.Name, mod.Namespace).Return(draDS, nil),
			mockReconHelper.EXPECT().getModuleDeviceClasses(ctx, mod.Name, mod.Namespace).Return(nil, nil),
			mockReconHelper.EXPECT().handleDRA(ctx, mod, draDS).Return(nil),
			mockReconHelper.EXPECT().garbageCollectDRADaemonSets(ctx, mod, draDS).Return(nil),
			mockReconHelper.EXPECT().handleDeviceClasses(ctx, mod, []resourcev1.DeviceClass(nil)).Return(nil),
			mockReconHelper.EXPECT().moduleUpdateDRAStatus(ctx, mod, draDS).Return(nil),
		)

		res, err := dr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("module deletion flow", func() {
		mod.SetDeletionTimestamp(&metav1.Time{})
		draDS := []appsv1.DaemonSet{{}}
		existingDCs := []resourcev1.DeviceClass{{ObjectMeta: metav1.ObjectMeta{Name: "gpu"}}}

		By("good flow")
		gomock.InOrder(
			mockReconHelper.EXPECT().getModuleDRADaemonSets(ctx, mod.Name, mod.Namespace).Return(draDS, nil),
			mockReconHelper.EXPECT().getModuleDeviceClasses(ctx, mod.Name, mod.Namespace).Return(existingDCs, nil),
			mockReconHelper.EXPECT().deleteDRAResources(ctx, mod.Name, mod.Namespace).Return(nil),
		)

		res, err := dr.Reconcile(ctx, mod)
		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())

		By("error flow - deleteDRAResources fails")
		gomock.InOrder(
			mockReconHelper.EXPECT().getModuleDRADaemonSets(ctx, mod.Name, mod.Namespace).Return(draDS, nil),
			mockReconHelper.EXPECT().getModuleDeviceClasses(ctx, mod.Name, mod.Namespace).Return(existingDCs, nil),
			mockReconHelper.EXPECT().deleteDRAResources(ctx, mod.Name, mod.Namespace).Return(fmt.Errorf("some error")),
		)

		res, err = dr.Reconcile(ctx, mod)
		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).To(HaveOccurred())
	})

	It("no-op when spec.dra is nil and no existing DaemonSets or DeviceClasses", func() {
		mod.Spec.DRA = nil
		gomock.InOrder(
			mockReconHelper.EXPECT().getModuleDRADaemonSets(ctx, mod.Name, mod.Namespace).Return(nil, nil),
			mockReconHelper.EXPECT().getModuleDeviceClasses(ctx, mod.Name, mod.Namespace).Return(nil, nil),
			mockReconHelper.EXPECT().deleteDRAResources(ctx, mod.Name, mod.Namespace).Return(nil),
			mockReconHelper.EXPECT().clearDRAStatus(ctx, mod).Return(nil),
		)

		res, err := dr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("cleanup when spec.dra is nil but existing DaemonSets and DeviceClasses present", func() {
		mod.Spec.DRA = nil
		draDS := []appsv1.DaemonSet{{}}
		existingDCs := []resourcev1.DeviceClass{{ObjectMeta: metav1.ObjectMeta{Name: "gpu"}}}

		gomock.InOrder(
			mockReconHelper.EXPECT().getModuleDRADaemonSets(ctx, mod.Name, mod.Namespace).Return(draDS, nil),
			mockReconHelper.EXPECT().getModuleDeviceClasses(ctx, mod.Name, mod.Namespace).Return(existingDCs, nil),
			mockReconHelper.EXPECT().deleteDRAResources(ctx, mod.Name, mod.Namespace).Return(nil),
			mockReconHelper.EXPECT().clearDRAStatus(ctx, mod).Return(nil),
		)

		res, err := dr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("cleanup when spec.dra is nil and clearDRAStatus fails", func() {
		mod.Spec.DRA = nil

		gomock.InOrder(
			mockReconHelper.EXPECT().getModuleDRADaemonSets(ctx, mod.Name, mod.Namespace).Return(nil, nil),
			mockReconHelper.EXPECT().getModuleDeviceClasses(ctx, mod.Name, mod.Namespace).Return(nil, nil),
			mockReconHelper.EXPECT().deleteDRAResources(ctx, mod.Name, mod.Namespace).Return(nil),
			mockReconHelper.EXPECT().clearDRAStatus(ctx, mod).Return(fmt.Errorf("some error")),
		)

		res, err := dr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("DRAReconciler_handleDRA", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		mockDSHelper *MockdraDaemonSetCreator
		drh          draReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockDSHelper = NewMockdraDaemonSetCreator(ctrl)
		drh = draReconcilerHelper{
			client:          clnt,
			daemonSetHelper: mockDSHelper,
		}
	})

	It("DRA not defined", func() {
		ctx := context.Background()
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "namespace",
			},
		}

		err := drh.handleDRA(ctx, &mod, nil)

		Expect(err).NotTo(HaveOccurred())
	})

	It("new daemonset", func() {
		ctx := context.Background()
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "namespace",
			},
			Spec: kmmv1beta1.ModuleSpec{
				DRA: &kmmv1beta1.DRASpec{},
			},
		}

		newDS := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: mod.Namespace, GenerateName: mod.Name + "-dra-"},
		}
		gomock.InOrder(
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(apierrors.NewNotFound(schema.GroupResource{}, "whatever")),
			mockDSHelper.EXPECT().setDRAAsDesired(ctx, newDS, &mod).Return(nil),
			clnt.EXPECT().Create(ctx, gomock.Any()).Return(nil),
		)

		err := drh.handleDRA(ctx, &mod, nil)

		Expect(err).NotTo(HaveOccurred())
	})

	It("existing daemonset", func() {
		ctx := context.Background()
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "namespace",
			},
			Spec: kmmv1beta1.ModuleSpec{
				DRA: &kmmv1beta1.DRASpec{},
			},
		}

		const name = "some name"
		existingDS := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: mod.Namespace, Name: name},
		}
		gomock.InOrder(
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, _ interface{}, ds *appsv1.DaemonSet, _ ...ctrlclient.GetOption) error {
					ds.SetName(name)
					ds.SetNamespace(mod.Namespace)
					return nil
				},
			),
			mockDSHelper.EXPECT().setDRAAsDesired(ctx, &existingDS, &mod).Return(nil),
		)

		err := drh.handleDRA(ctx, &mod, []appsv1.DaemonSet{existingDS})

		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("DRAReconciler_moduleUpdateDRAStatus", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		statusWriter *client.MockStatusWriter
		mn           node.Node
		drh          draReconcilerHelperAPI
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		statusWriter = client.NewMockStatusWriter(ctrl)
		mn = node.NewNode(clnt)
		drh = newDRAReconcilerHelper(clnt, mn, nil)
	})

	ctx := context.Background()

	It("DRA not defined in the module", func() {
		mod := kmmv1beta1.Module{}
		err := drh.moduleUpdateDRAStatus(ctx, &mod, nil)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return an error when GetNumTargetedNodes fails", func() {
		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				DRA: &kmmv1beta1.DRASpec{},
			},
		}

		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		err := drh.moduleUpdateDRAStatus(ctx, &mod, nil)
		Expect(err).To(HaveOccurred())
	})

	DescribeTable("DRA status update",
		func(numTargetedNodes int, numAvailableInDaemonSets []int, nodesMatchingNumber, availableNumber int) {
			mod := kmmv1beta1.Module{
				Spec: kmmv1beta1.ModuleSpec{
					DRA: &kmmv1beta1.DRASpec{},
				},
			}
			expectedMod := mod.DeepCopy()
			expectedMod.Status.DRA.NodesMatchingSelectorNumber = int32(nodesMatchingNumber)
			expectedMod.Status.DRA.DesiredNumber = int32(nodesMatchingNumber)
			expectedMod.Status.DRA.AvailableNumber = int32(availableNumber)

			nodesList := []v1.Node{}
			for i := 0; i < numTargetedNodes; i++ {
				nodesList = append(nodesList, v1.Node{})
			}
			daemonSetsList := []appsv1.DaemonSet{}
			for _, numAvailable := range numAvailableInDaemonSets {
				ds := appsv1.DaemonSet{
					Status: appsv1.DaemonSetStatus{
						NumberAvailable: int32(numAvailable),
					},
				}
				daemonSetsList = append(daemonSetsList, ds)
			}

			clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ interface{}, list *v1.NodeList, _ ...interface{}) error {
					list.Items = nodesList
					return nil
				},
			)
			clnt.EXPECT().Status().Return(statusWriter)
			statusWriter.EXPECT().Patch(ctx, expectedMod, gomock.Any())

			err := drh.moduleUpdateDRAStatus(ctx, &mod, daemonSetsList)
			Expect(err).NotTo(HaveOccurred())
		},
		Entry("0 target node, 0 ds", 0, nil, 0, 0),
		Entry("0 target node, 1 ds", 0, []int{1}, 0, 1),
		Entry("0 target node, 2 ds", 0, []int{3, 6}, 0, 9),
		Entry("3 target node, 0 ds", 3, nil, 3, 0),
		Entry("2 target node, 3 ds", 2, []int{3, 6, 8}, 2, 17),
	)
})

var _ = Describe("DRAReconciler_clearDRAStatus", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		statusWriter *client.MockStatusWriter
		drh          draReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		statusWriter = client.NewMockStatusWriter(ctrl)
		drh = draReconcilerHelper{
			client: clnt,
		}
	})

	ctx := context.Background()

	It("should be a no-op when status.dra is already empty", func() {
		mod := kmmv1beta1.Module{}
		err := drh.clearDRAStatus(ctx, &mod)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should clear status.dra when it has values", func() {
		mod := kmmv1beta1.Module{
			Status: kmmv1beta1.ModuleStatus{
				DRA: kmmv1beta1.DaemonSetStatus{
					NodesMatchingSelectorNumber: 3,
					DesiredNumber:               3,
					AvailableNumber:             2,
				},
			},
		}

		expectedMod := mod.DeepCopy()
		expectedMod.Status.DRA = kmmv1beta1.DaemonSetStatus{}

		clnt.EXPECT().Status().Return(statusWriter)
		statusWriter.EXPECT().Patch(ctx, expectedMod, gomock.Any())

		err := drh.clearDRAStatus(ctx, &mod)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("DRAReconciler_setDRAAsDesired", func() {
	const (
		draImage      = "dra-image"
		draModuleName = "dra-module"
	)

	var (
		dsc draDaemonSetCreator
	)

	BeforeEach(func() {
		dsc = newDRADaemonSetCreator(scheme)
	})

	It("should return an error if the DaemonSet is nil", func() {
		Expect(
			dsc.setDRAAsDesired(context.Background(), nil, &kmmv1beta1.Module{}),
		).To(
			HaveOccurred(),
		)
	})

	It("should return an error if DRA not set in the Spec", func() {
		ds := appsv1.DaemonSet{}
		mod := kmmv1beta1.Module{}
		Expect(
			dsc.setDRAAsDesired(context.Background(), &ds, &mod),
		).To(
			HaveOccurred(),
		)
	})

	It("should add additional volumes after mandatory volumes", func() {
		vol := v1.Volume{Name: "test-volume"}

		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				DRA: &kmmv1beta1.DRASpec{
					Container: kmmv1beta1.CommonContainerSpec{Image: draImage},
					Volumes:   []v1.Volume{vol},
				},
			},
		}

		ds := appsv1.DaemonSet{}

		err := dsc.setDRAAsDesired(context.Background(), &ds, &mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(3))
		Expect(ds.Spec.Template.Spec.Volumes[0].Name).To(Equal("kubelet-plugins"))
		Expect(ds.Spec.Template.Spec.Volumes[1].Name).To(Equal("kubelet-plugins-registry"))
		Expect(ds.Spec.Template.Spec.Volumes[2]).To(Equal(vol))
	})

	DescribeTable("should work as expected",
		func(withInitContainer bool) {
			const (
				dsName             = "ds-name"
				serviceAccountName = "some-service-account"
			)

			draVol := v1.Volume{
				Name:         "test-volume",
				VolumeSource: v1.VolumeSource{},
			}

			draVolMount := v1.VolumeMount{
				Name:      "some-dra-volume-mount",
				MountPath: "/some/path",
			}

			repoSecret := v1.LocalObjectReference{Name: "pull-secret-name"}

			env := []v1.EnvVar{
				{
					Name:  "ENV_KEY",
					Value: "ENV_VALUE",
				},
			}

			resources := v1.ResourceRequirements{
				Limits: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU:    resource.MustParse("200m"),
					v1.ResourceMemory: resource.MustParse("4G"),
				},
				Requests: map[v1.ResourceName]resource.Quantity{
					v1.ResourceCPU:    resource.MustParse("100m"),
					v1.ResourceMemory: resource.MustParse("2G"),
				},
			}

			args := []string{"some", "args"}
			command := []string{"some", "command"}

			testToleration := v1.Toleration{
				Key:    "test-key",
				Value:  "test-value",
				Effect: v1.TaintEffectNoExecute,
			}

			const ipp = v1.PullIfNotPresent

			initContainer := &kmmv1beta1.CommonContainerSpec{
				Args:         args,
				Command:      command,
				Env:          env,
				Image:        draImage,
				Resources:    resources,
				VolumeMounts: []v1.VolumeMount{draVolMount},
			}
			if !withInitContainer {
				initContainer = nil
			}

			mod := kmmv1beta1.Module{
				TypeMeta: metav1.TypeMeta{
					APIVersion: kmmv1beta1.GroupVersion.String(),
					Kind:       "Module",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      draModuleName,
					Namespace: namespace,
				},
				Spec: kmmv1beta1.ModuleSpec{
					DRA: &kmmv1beta1.DRASpec{
						InitContainer: initContainer,
						Container: kmmv1beta1.CommonContainerSpec{
							Args:            args,
							Command:         command,
							Env:             env,
							Image:           draImage,
							ImagePullPolicy: ipp,
							Resources:       resources,
							VolumeMounts:    []v1.VolumeMount{draVolMount},
						},
						ServiceAccountName:           serviceAccountName,
						Volumes:                      []v1.Volume{draVol},
						AutomountServiceAccountToken: ptr.To(false),
					},
					ImageRepoSecret: &repoSecret,
					Selector:        map[string]string{"has-feature-x": "true"},
					Tolerations:     []v1.Toleration{testToleration},
				},
			}
			ds := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dsName,
					Namespace: namespace,
				},
			}

			err := dsc.setDRAAsDesired(context.Background(), &ds, &mod)
			Expect(err).NotTo(HaveOccurred())

			podLabels := map[string]string{
				constants.ModuleNameLabel: draModuleName,
				constants.DaemonSetRole:   constants.DRARoleLabelValue,
			}

			expectedInitContainer := []v1.Container{
				{
					Args:      args,
					Command:   command,
					Env:       env,
					Image:     draImage,
					Name:      "dra-init",
					Resources: resources,
					SecurityContext: &v1.SecurityContext{
						Privileged: ptr.To(true),
					},
					VolumeMounts: []v1.VolumeMount{
						draVolMount,
					},
				},
			}

			if !withInitContainer {
				expectedInitContainer = nil
			}

			hostPathDirOrCreate := v1.HostPathDirectoryOrCreate
			hostPathDir := v1.HostPathDirectory

			expected := appsv1.DaemonSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      dsName,
					Namespace: namespace,
					Labels:    podLabels,
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         mod.APIVersion,
							BlockOwnerDeletion: ptr.To(true),
							Controller:         ptr.To(true),
							Kind:               mod.Kind,
							Name:               draModuleName,
							UID:                mod.UID,
						},
					},
				},
				Spec: appsv1.DaemonSetSpec{
					Selector: &metav1.LabelSelector{MatchLabels: podLabels},
					Template: v1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels:     podLabels,
							Finalizers: []string{constants.NodeLabelerFinalizer},
						},
						Spec: v1.PodSpec{
							InitContainers: expectedInitContainer,
							Containers: []v1.Container{
								{
									Args:            args,
									Command:         command,
									Env:             env,
									Image:           draImage,
									ImagePullPolicy: ipp,
									Name:            "dra",
									Resources:       resources,
									SecurityContext: &v1.SecurityContext{
										Privileged: ptr.To(true),
									},
									VolumeMounts: []v1.VolumeMount{
										draVolMount,
										{
											Name:      "kubelet-plugins",
											MountPath: "/var/lib/kubelet/plugins/",
										},
										{
											Name:      "kubelet-plugins-registry",
											MountPath: "/var/lib/kubelet/plugins_registry/",
										},
									},
								},
							},
							ImagePullSecrets: []v1.LocalObjectReference{repoSecret},
							NodeSelector: map[string]string{
								utils.GetKernelModuleReadyNodeLabel(namespace, draModuleName): "",
							},
							PriorityClassName:  "system-node-critical",
							ServiceAccountName: serviceAccountName,
							Volumes: []v1.Volume{
								{
									Name: "kubelet-plugins",
									VolumeSource: v1.VolumeSource{
										HostPath: &v1.HostPathVolumeSource{
											Path: "/var/lib/kubelet/plugins/",
											Type: &hostPathDirOrCreate,
										},
									},
								},
								{
									Name: "kubelet-plugins-registry",
									VolumeSource: v1.VolumeSource{
										HostPath: &v1.HostPathVolumeSource{
											Path: "/var/lib/kubelet/plugins_registry/",
											Type: &hostPathDir,
										},
									},
								},
								draVol,
							},
							Tolerations:                  []v1.Toleration{testToleration},
							AutomountServiceAccountToken: ptr.To(false),
						},
					},
				},
			}
			Expect(
				cmp.Equal(expected, ds),
			).To(
				BeTrue(), cmp.Diff(expected, ds),
			)
		},
		Entry("without init container", false),
		Entry("with init container", true),
	)

	It("should include the version-dra label in the DaemonSet labels and node selector when version is set", func() {
		mod := kmmv1beta1.Module{
			TypeMeta: metav1.TypeMeta{
				APIVersion: kmmv1beta1.GroupVersion.String(),
				Kind:       "Module",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      draModuleName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: &kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						Version: "1",
						Modprobe: kmmv1beta1.ModprobeSpec{
							ModuleName: "test-mod",
						},
						KernelMappings: []kmmv1beta1.KernelMapping{
							{Regexp: "^.+$", ContainerImage: "some-image"},
						},
					},
				},
				DRA: &kmmv1beta1.DRASpec{
					Container: kmmv1beta1.CommonContainerSpec{
						Image: draImage,
					},
					DriverName: "test.driver",
				},
			},
		}

		ds := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-ds",
				Namespace: namespace,
			},
		}

		err := dsc.setDRAAsDesired(context.Background(), &ds, &mod)
		Expect(err).NotTo(HaveOccurred())

		versionLabel := utils.GetSchedulePluginVersionLabelName(namespace, draModuleName)

		Expect(ds.Labels).To(HaveKeyWithValue(versionLabel, "1"))
		Expect(ds.Spec.Template.Spec.NodeSelector).To(HaveKeyWithValue(versionLabel, "1"))
		Expect(ds.Spec.Template.Spec.NodeSelector).To(HaveKeyWithValue(
			utils.GetKernelModuleReadyNodeLabel(namespace, draModuleName), "",
		))
	})
})

var _ = Describe("DRAReconciler_garbageCollectDRADaemonSets", func() {
	const currentModuleVersion = "current"

	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		drh  draReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		drh = draReconcilerHelper{client: clnt}
	})

	mod := &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "moduleName",
			Namespace: "namespace",
		},
		Spec: kmmv1beta1.ModuleSpec{
			ModuleLoader: &kmmv1beta1.ModuleLoaderSpec{
				Container: kmmv1beta1.ModuleLoaderContainerSpec{
					Version: currentModuleVersion,
				},
			},
		},
	}
	schedulePluginVersionLabel := utils.GetSchedulePluginVersionLabelName(mod.Namespace, mod.Name)

	DescribeTable("DRA GC", func(formerDSExists bool, formerDesired int) {
		currentDS := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dra-current",
				Namespace: "namespace",
				Labels: map[string]string{
					schedulePluginVersionLabel: currentModuleVersion,
					constants.ModuleNameLabel:  mod.Name,
				},
			},
		}
		formerDS := &appsv1.DaemonSet{}

		existingDS := []appsv1.DaemonSet{currentDS}
		if formerDSExists {
			formerDS = currentDS.DeepCopy()
			formerDS.SetName("dra-former")
			formerDS.Labels[schedulePluginVersionLabel] = "former"
			formerDS.Status.DesiredNumberScheduled = int32(formerDesired)
			existingDS = append(existingDS, *formerDS)
		}
		if formerDSExists && formerDesired == 0 {
			clnt.EXPECT().Delete(context.Background(), formerDS).Return(nil)
		}

		err := drh.garbageCollectDRADaemonSets(context.Background(), mod, existingDS)
		Expect(err).NotTo(HaveOccurred())
	},
		Entry("no former DS to delete", false, 0),
		Entry("former DS with zero desired — deleted", true, 0),
		Entry("former DS still has desired pods — kept", true, 1),
	)

	It("should return an error if a deletion failed", func() {
		deleteDS := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dra-old",
				Namespace: "namespace",
				Labels:    map[string]string{constants.ModuleNameLabel: mod.Name, schedulePluginVersionLabel: "formerVersion"},
			},
		}
		clnt.EXPECT().Delete(context.Background(), &deleteDS).Return(fmt.Errorf("some error"))

		err := drh.garbageCollectDRADaemonSets(context.Background(), mod, []appsv1.DaemonSet{deleteDS})
		Expect(err).To(HaveOccurred())
	})

	It("should pass if moduleLoader is not defined", func() {
		modWithoutModuleLoader := mod.DeepCopy()
		modWithoutModuleLoader.Spec.ModuleLoader = nil
		oldDS := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "dra-old",
				Namespace: "namespace",
				Labels:    map[string]string{constants.ModuleNameLabel: mod.Name, schedulePluginVersionLabel: "formerVersion"},
			},
		}

		err := drh.garbageCollectDRADaemonSets(context.Background(), modWithoutModuleLoader, []appsv1.DaemonSet{oldDS})
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("DRAReconciler_getModuleDRADaemonSets", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		drh  draReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		drh = draReconcilerHelper{
			client: clnt,
		}
	})

	ctx := context.Background()

	It("list failed", func() {
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		dsList, err := drh.getModuleDRADaemonSets(ctx, "name", "namespace")

		Expect(err).ToNot(BeNil())
		Expect(dsList).To(BeNil())
	})

	It("good flow, returns DRA-role DSes", func() {
		ds1 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					constants.ModuleNameLabel: "some name",
					constants.DaemonSetRole:   constants.DRARoleLabelValue,
				},
			},
		}
		ds2 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					constants.ModuleNameLabel: "some name",
					constants.DaemonSetRole:   constants.DRARoleLabelValue,
				},
			},
		}
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *appsv1.DaemonSetList, _ ...interface{}) error {
				list.Items = []appsv1.DaemonSet{ds1, ds2}
				return nil
			},
		)

		dsList, err := drh.getModuleDRADaemonSets(ctx, "name", "namespace")

		Expect(err).NotTo(HaveOccurred())
		Expect(dsList).To(Equal([]appsv1.DaemonSet{ds1, ds2}))
	})
})

var _ = Describe("DRAReconciler_getModuleDeviceClasses", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		drh  draReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		drh = draReconcilerHelper{
			client: clnt,
		}
	})

	ctx := context.Background()

	It("should return error when list fails", func() {
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		dcList, err := drh.getModuleDeviceClasses(ctx, "name", "namespace")

		Expect(err).To(HaveOccurred())
		Expect(dcList).To(BeNil())
	})

	It("should return DeviceClasses matching ownership labels", func() {
		dc1 := resourcev1.DeviceClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gpu",
				Labels: map[string]string{
					constants.ModuleNameLabel:      "my-module",
					constants.ModuleNamespaceLabel: "my-ns",
				},
			},
		}
		dc2 := resourcev1.DeviceClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "fpga",
				Labels: map[string]string{
					constants.ModuleNameLabel:      "my-module",
					constants.ModuleNamespaceLabel: "my-ns",
				},
			},
		}
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *resourcev1.DeviceClassList, _ ...interface{}) error {
				list.Items = []resourcev1.DeviceClass{dc1, dc2}
				return nil
			},
		)

		dcList, err := drh.getModuleDeviceClasses(ctx, "my-module", "my-ns")

		Expect(err).NotTo(HaveOccurred())
		Expect(dcList).To(Equal([]resourcev1.DeviceClass{dc1, dc2}))
	})
})

var _ = Describe("DRAReconciler_handleDeviceClasses", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		drh  draReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		drh = draReconcilerHelper{
			client: clnt,
		}
	})

	ctx := context.Background()

	It("should be a no-op when DRA is nil", func() {
		mod := kmmv1beta1.Module{}
		err := drh.handleDeviceClasses(ctx, &mod, nil)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should create a DeviceClass when desired but not existing", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "my-mod", Namespace: "my-ns"},
			Spec: kmmv1beta1.ModuleSpec{
				DRA: &kmmv1beta1.DRASpec{
					DeviceClasses: []kmmv1beta1.DeviceClassSpec{
						{Name: "gpu"},
					},
				},
			},
		}

		clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(apierrors.NewNotFound(resourcev1.Resource("deviceclasses"), "gpu"))
		clnt.EXPECT().Create(ctx, gomock.Any()).DoAndReturn(
			func(_ context.Context, dc *resourcev1.DeviceClass, _ ...ctrlclient.CreateOption) error {
				Expect(dc.Name).To(Equal("gpu"))
				Expect(dc.Labels[constants.ModuleNameLabel]).To(Equal("my-mod"))
				Expect(dc.Labels[constants.ModuleNamespaceLabel]).To(Equal("my-ns"))
				return nil
			},
		)

		err := drh.handleDeviceClasses(ctx, &mod, nil)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should patch an existing DeviceClass when spec differs", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "my-mod", Namespace: "my-ns"},
			Spec: kmmv1beta1.ModuleSpec{
				DRA: &kmmv1beta1.DRASpec{
					DeviceClasses: []kmmv1beta1.DeviceClassSpec{
						{
							Name: "gpu",
							Selectors: []resourcev1.DeviceSelector{
								{CEL: &resourcev1.CELDeviceSelector{Expression: "device.driver == 'nvidia'"}},
							},
						},
					},
				},
			},
		}

		existingDC := resourcev1.DeviceClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gpu",
				Labels: map[string]string{
					constants.ModuleNameLabel:      "my-mod",
					constants.ModuleNamespaceLabel: "my-ns",
				},
			},
			Spec: resourcev1.DeviceClassSpec{},
		}

		clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ ctrlclient.ObjectKey, obj ctrlclient.Object, _ ...ctrlclient.GetOption) error {
				existingDC.DeepCopyInto(obj.(*resourcev1.DeviceClass))
				return nil
			},
		)
		clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, dc *resourcev1.DeviceClass, _ ctrlclient.Patch, _ ...ctrlclient.PatchOption) error {
				Expect(dc.Name).To(Equal("gpu"))
				Expect(dc.Spec.Selectors).To(HaveLen(1))
				return nil
			},
		)

		err := drh.handleDeviceClasses(ctx, &mod, []resourcev1.DeviceClass{existingDC})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should delete DeviceClasses not in the desired list", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "my-mod", Namespace: "my-ns"},
			Spec: kmmv1beta1.ModuleSpec{
				DRA: &kmmv1beta1.DRASpec{
					DeviceClasses: []kmmv1beta1.DeviceClassSpec{},
				},
			},
		}

		extraDC := resourcev1.DeviceClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "stale-dc",
				Labels: map[string]string{
					constants.ModuleNameLabel:      "my-mod",
					constants.ModuleNamespaceLabel: "my-ns",
				},
			},
		}

		clnt.EXPECT().Delete(ctx, &extraDC).Return(nil)

		err := drh.handleDeviceClasses(ctx, &mod, []resourcev1.DeviceClass{extraDC})
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return error on create failure", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "my-mod", Namespace: "my-ns"},
			Spec: kmmv1beta1.ModuleSpec{
				DRA: &kmmv1beta1.DRASpec{
					DeviceClasses: []kmmv1beta1.DeviceClassSpec{
						{Name: "gpu"},
					},
				},
			},
		}

		clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(apierrors.NewNotFound(resourcev1.Resource("deviceclasses"), "gpu"))
		clnt.EXPECT().Create(ctx, gomock.Any()).Return(fmt.Errorf("create failed"))

		err := drh.handleDeviceClasses(ctx, &mod, nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("create failed"))
	})

	It("should return error on patch failure", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "my-mod", Namespace: "my-ns"},
			Spec: kmmv1beta1.ModuleSpec{
				DRA: &kmmv1beta1.DRASpec{
					DeviceClasses: []kmmv1beta1.DeviceClassSpec{
						{
							Name: "gpu",
							Selectors: []resourcev1.DeviceSelector{
								{CEL: &resourcev1.CELDeviceSelector{Expression: "device.driver == 'nvidia'"}},
							},
						},
					},
				},
			},
		}

		existingDC := resourcev1.DeviceClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "gpu",
			},
		}

		clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ context.Context, _ ctrlclient.ObjectKey, obj ctrlclient.Object, _ ...ctrlclient.GetOption) error {
				existingDC.DeepCopyInto(obj.(*resourcev1.DeviceClass))
				return nil
			},
		)
		clnt.EXPECT().Patch(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("patch failed"))

		err := drh.handleDeviceClasses(ctx, &mod, []resourcev1.DeviceClass{existingDC})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("patch failed"))
	})

	It("should return error on delete failure for extras", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "my-mod", Namespace: "my-ns"},
			Spec: kmmv1beta1.ModuleSpec{
				DRA: &kmmv1beta1.DRASpec{
					DeviceClasses: []kmmv1beta1.DeviceClassSpec{},
				},
			},
		}

		extraDC := resourcev1.DeviceClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "stale-dc",
				Labels: map[string]string{
					constants.ModuleNameLabel:      "my-mod",
					constants.ModuleNamespaceLabel: "my-ns",
				},
			},
		}

		clnt.EXPECT().Delete(ctx, &extraDC).Return(fmt.Errorf("delete failed"))

		err := drh.handleDeviceClasses(ctx, &mod, []resourcev1.DeviceClass{extraDC})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("delete failed"))
	})

	It("should be a no-op when no desired and no existing DeviceClasses", func() {
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Name: "my-mod", Namespace: "my-ns"},
			Spec: kmmv1beta1.ModuleSpec{
				DRA: &kmmv1beta1.DRASpec{
					DeviceClasses: []kmmv1beta1.DeviceClassSpec{},
				},
			},
		}

		err := drh.handleDeviceClasses(ctx, &mod, nil)
		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("DRAReconciler_deleteDRAResources", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		drh  draReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		drh = draReconcilerHelper{
			client: clnt,
		}
	})

	ctx := context.Background()

	It("should delete all DaemonSets and DeviceClasses via DeleteAllOf", func() {
		clnt.EXPECT().DeleteAllOf(ctx, &appsv1.DaemonSet{}, gomock.Any()).Return(nil)
		clnt.EXPECT().DeleteAllOf(ctx, &resourcev1.DeviceClass{}, gomock.Any()).Return(nil)

		err := drh.deleteDRAResources(ctx, "my-mod", "my-ns")
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return error when DaemonSet DeleteAllOf fails", func() {
		clnt.EXPECT().DeleteAllOf(ctx, &appsv1.DaemonSet{}, gomock.Any()).Return(fmt.Errorf("ds delete failed"))
		clnt.EXPECT().DeleteAllOf(ctx, &resourcev1.DeviceClass{}, gomock.Any()).Return(nil)

		err := drh.deleteDRAResources(ctx, "my-mod", "my-ns")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("DaemonSets"))
	})

	It("should return error when DeviceClass DeleteAllOf fails", func() {
		clnt.EXPECT().DeleteAllOf(ctx, &appsv1.DaemonSet{}, gomock.Any()).Return(nil)
		clnt.EXPECT().DeleteAllOf(ctx, &resourcev1.DeviceClass{}, gomock.Any()).Return(fmt.Errorf("dc delete failed"))

		err := drh.deleteDRAResources(ctx, "my-mod", "my-ns")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("DeviceClasses"))
	})

	It("should aggregate errors from both DeleteAllOf calls", func() {
		clnt.EXPECT().DeleteAllOf(ctx, &appsv1.DaemonSet{}, gomock.Any()).Return(fmt.Errorf("ds error"))
		clnt.EXPECT().DeleteAllOf(ctx, &resourcev1.DeviceClass{}, gomock.Any()).Return(fmt.Errorf("dc error"))

		err := drh.deleteDRAResources(ctx, "my-mod", "my-ns")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("DaemonSets"))
		Expect(err.Error()).To(ContainSubstring("DeviceClasses"))
	})
})
