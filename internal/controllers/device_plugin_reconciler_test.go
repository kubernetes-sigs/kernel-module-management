package controllers

import (
	"context"
	"fmt"
	"github.com/kubernetes-sigs/kernel-module-management/internal/node"
	"strings"

	"github.com/google/go-cmp/cmp"
	kmmv1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	"github.com/kubernetes-sigs/kernel-module-management/internal/client"
	"github.com/kubernetes-sigs/kernel-module-management/internal/constants"
	"github.com/kubernetes-sigs/kernel-module-management/internal/metrics"
	"github.com/kubernetes-sigs/kernel-module-management/internal/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("DevicePluginReconciler_Reconcile", func() {
	var (
		ctrl            *gomock.Controller
		mockReconHelper *MockdevicePluginReconcilerHelperAPI
		mod             *kmmv1beta1.Module
		dpr             *DevicePluginReconciler
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockReconHelper = NewMockdevicePluginReconcilerHelperAPI(ctrl)

		mod = &kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: moduleName},
		}

		dpr = &DevicePluginReconciler{
			reconHelperAPI: mockReconHelper,
		}
	})

	ctx := context.Background()

	DescribeTable("check error flows", func(getDSError, handlePluginError, gcError bool) {
		devicePluginDS := []appsv1.DaemonSet{appsv1.DaemonSet{}}
		returnedError := fmt.Errorf("some error")
		if getDSError {
			mockReconHelper.EXPECT().getModuleDevicePluginDaemonSets(ctx, mod.Name, mod.Namespace).Return(nil, returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().getModuleDevicePluginDaemonSets(ctx, mod.Name, mod.Namespace).Return(devicePluginDS, nil)
		mockReconHelper.EXPECT().setKMMOMetrics(ctx)
		if handlePluginError {
			mockReconHelper.EXPECT().handleDevicePlugin(ctx, mod, devicePluginDS).Return(returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().handleDevicePlugin(ctx, mod, devicePluginDS).Return(nil)
		if gcError {
			mockReconHelper.EXPECT().garbageCollect(ctx, mod, devicePluginDS).Return(returnedError)
			goto executeTestFunction
		}
		mockReconHelper.EXPECT().garbageCollect(ctx, mod, devicePluginDS).Return(nil)
		mockReconHelper.EXPECT().moduleUpdateDevicePluginStatus(ctx, mod, devicePluginDS).Return(returnedError)

	executeTestFunction:
		res, err := dpr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).To(HaveOccurred())

	},
		Entry("getDevicePluginDaemonSets failed", true, false, false),
		Entry("handleDevicePlugin failed", false, true, false),
		Entry("garbageCollect failed", false, false, true),
		Entry("devicePluginUpdateStatus failed", false, false, false),
	)

	It("Good flow", func() {
		devicePluginDS := []appsv1.DaemonSet{appsv1.DaemonSet{}}
		gomock.InOrder(
			mockReconHelper.EXPECT().getModuleDevicePluginDaemonSets(ctx, mod.Name, mod.Namespace).Return(devicePluginDS, nil),
			mockReconHelper.EXPECT().setKMMOMetrics(ctx),
			mockReconHelper.EXPECT().handleDevicePlugin(ctx, mod, devicePluginDS).Return(nil),
			mockReconHelper.EXPECT().garbageCollect(ctx, mod, devicePluginDS).Return(nil),
			mockReconHelper.EXPECT().moduleUpdateDevicePluginStatus(ctx, mod, devicePluginDS).Return(nil),
		)

		res, err := dpr.Reconcile(ctx, mod)

		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())
	})

	It("module deletion flow", func() {
		mod.SetDeletionTimestamp(&metav1.Time{})
		devicePluginDS := []appsv1.DaemonSet{appsv1.DaemonSet{}}

		By("good flow")
		gomock.InOrder(
			mockReconHelper.EXPECT().getModuleDevicePluginDaemonSets(ctx, mod.Name, mod.Namespace).Return(devicePluginDS, nil),
			mockReconHelper.EXPECT().handleModuleDeletion(ctx, devicePluginDS).Return(nil),
		)

		res, err := dpr.Reconcile(ctx, mod)
		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).NotTo(HaveOccurred())

		By("error flow")
		gomock.InOrder(
			mockReconHelper.EXPECT().getModuleDevicePluginDaemonSets(ctx, mod.Name, mod.Namespace).Return(devicePluginDS, nil),
			mockReconHelper.EXPECT().handleModuleDeletion(ctx, devicePluginDS).Return(fmt.Errorf("some error")),
		)

		res, err = dpr.Reconcile(ctx, mod)
		Expect(res).To(Equal(reconcile.Result{}))
		Expect(err).To(HaveOccurred())
	})

})

var _ = Describe("DevicePluginReconciler_handleDevicePlugin", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		mockDSHelper *MockdaemonSetCreator
		dprh         devicePluginReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockDSHelper = NewMockdaemonSetCreator(ctrl)
		dprh = devicePluginReconcilerHelper{
			client:          clnt,
			daemonSetHelper: mockDSHelper,
		}
	})

	It("device plugin not defined", func() {
		ctx := context.Background()
		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "namespace",
			},
		}

		err := dprh.handleDevicePlugin(ctx, &mod, nil)

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
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{},
			},
		}

		newDS := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Namespace: mod.Namespace, GenerateName: mod.Name + "-device-plugin-"},
		}
		gomock.InOrder(
			clnt.EXPECT().Get(ctx, gomock.Any(), gomock.Any()).Return(apierrors.NewNotFound(schema.GroupResource{}, "whatever")),
			mockDSHelper.EXPECT().setDevicePluginAsDesired(ctx, newDS, &mod).Return(nil),
			clnt.EXPECT().Create(ctx, gomock.Any()).Return(nil),
		)

		err := dprh.handleDevicePlugin(ctx, &mod, nil)

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
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{},
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
			mockDSHelper.EXPECT().setDevicePluginAsDesired(ctx, &existingDS, &mod).Return(nil),
		)

		err := dprh.handleDevicePlugin(ctx, &mod, []appsv1.DaemonSet{existingDS})

		Expect(err).NotTo(HaveOccurred())
	})
})

var _ = Describe("DevicePluginReconciler_garbageCollect", func() {
	const currentModuleVersion = "current label"

	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		dprh devicePluginReconcilerHelperAPI
		mn   node.Node
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mn = node.NewMockNode(ctrl)
		dprh = newDevicePluginReconcilerHelper(clnt, nil, mn, nil)
	})

	mod := &kmmv1beta1.Module{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "moduleName",
			Namespace: "namespace",
		},
		Spec: kmmv1beta1.ModuleSpec{
			ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
				Container: kmmv1beta1.ModuleLoaderContainerSpec{
					Version: currentModuleVersion,
				},
			},
		},
	}
	devicePluginVersionLabel := utils.GetDevicePluginVersionLabelName(mod.Namespace, mod.Name)

	DescribeTable("device-plugin GC", func(devicePluginFormerLabel bool, devicePluginFormerDesired int) {
		devicePluginDS := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "devicePlugin",
				Namespace: "namespace",
				Labels: map[string]string{
					devicePluginVersionLabel:  currentModuleVersion,
					constants.ModuleNameLabel: mod.Name,
				},
			},
		}
		devicePluginFormerVersionDS := &appsv1.DaemonSet{}

		existingDS := []appsv1.DaemonSet{devicePluginDS}
		if devicePluginFormerLabel {
			devicePluginFormerVersionDS = devicePluginDS.DeepCopy()
			devicePluginFormerVersionDS.SetName("devicePluginFormer")
			devicePluginFormerVersionDS.Labels[devicePluginVersionLabel] = "former label"
			devicePluginFormerVersionDS.Status.DesiredNumberScheduled = int32(devicePluginFormerDesired)
			existingDS = append(existingDS, *devicePluginFormerVersionDS)
		}
		if devicePluginFormerLabel && devicePluginFormerDesired == 0 {
			clnt.EXPECT().Delete(context.Background(), devicePluginFormerVersionDS).Return(nil)
		}

		err := dprh.garbageCollect(context.Background(), mod, existingDS)

		Expect(err).NotTo(HaveOccurred())
	},
		Entry("no deletes", false, 0),
		Entry("former device plugin", true, 0),
		Entry("former device plugin has desired", true, 1),
	)

	It("should return an error if a deletion failed", func() {
		deleteDS := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "devicePlugin",
				Namespace: "namespace",
				Labels:    map[string]string{constants.ModuleNameLabel: mod.Name, devicePluginVersionLabel: "formerVersion"},
			},
		}
		clnt.EXPECT().Delete(context.Background(), &deleteDS).Return(fmt.Errorf("some error"))

		existingDS := []appsv1.DaemonSet{deleteDS}

		err := dprh.garbageCollect(context.Background(), mod, existingDS)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("DevicePluginReconciler_handleModuleDeletion", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		dprh devicePluginReconcilerHelperAPI
		mn   node.Node
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mn = node.NewMockNode(ctrl)
		dprh = newDevicePluginReconcilerHelper(clnt, nil, mn, nil)
	})

	existingDevicePluginDS := []appsv1.DaemonSet{appsv1.DaemonSet{}}
	ctx := context.Background()

	It("good flow", func() {
		clnt.EXPECT().Delete(ctx, &existingDevicePluginDS[0]).Return(nil)

		err := dprh.handleModuleDeletion(ctx, existingDevicePluginDS)
		Expect(err).NotTo(HaveOccurred())
	})

	It("error flow", func() {
		clnt.EXPECT().Delete(ctx, &existingDevicePluginDS[0]).Return(fmt.Errorf("some error"))

		err := dprh.handleModuleDeletion(ctx, existingDevicePluginDS)
		Expect(err).To(HaveOccurred())
	})
})

var _ = Describe("DevicePluginReconciler_setKMMOMetrics", func() {
	var (
		ctrl        *gomock.Controller
		clnt        *client.MockClient
		mockMetrics *metrics.MockMetrics
		dprh        devicePluginReconcilerHelperAPI
		mn          node.Node
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		mockMetrics = metrics.NewMockMetrics(ctrl)
		mn = node.NewMockNode(ctrl)
		dprh = newDevicePluginReconcilerHelper(clnt, mockMetrics, mn, nil)
	})

	ctx := context.Background()

	It("failed to list Modules", func() {
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		dprh.setKMMOMetrics(ctx)
	})

	DescribeTable("getting metrics data", func(buildInContainer, buildInKM, signInContainer, signInKM, devicePlugin bool, modprobeArg, modprobeRawArg []string) {
		km := kmmv1beta1.KernelMapping{}
		mod1 := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "moduleName",
				Namespace: "namespace",
			},
		}
		mod2 := kmmv1beta1.Module{}
		mod3 := kmmv1beta1.Module{}
		numBuild := 0
		numSign := 0
		numDevicePlugin := 0
		if buildInContainer {
			mod1.Spec.ModuleLoader.Container.Build = &kmmv1beta1.Build{}
			numBuild = 1
		}
		if buildInKM {
			km.Build = &kmmv1beta1.Build{}
			numBuild = 1
		}
		if signInContainer {
			mod1.Spec.ModuleLoader.Container.Sign = &kmmv1beta1.Sign{}
			numSign = 1
		}
		if signInKM {
			km.Sign = &kmmv1beta1.Sign{}
			numSign = 1
		}
		if devicePlugin {
			mod1.Spec.DevicePlugin = &kmmv1beta1.DevicePluginSpec{}
			numDevicePlugin = 1
		}
		if modprobeArg != nil {
			mod1.Spec.ModuleLoader.Container.Modprobe.Args = &kmmv1beta1.ModprobeArgs{Load: modprobeArg}
		}
		if modprobeRawArg != nil {
			mod1.Spec.ModuleLoader.Container.Modprobe.RawArgs = &kmmv1beta1.ModprobeArgs{Load: modprobeRawArg}
		}
		mod1.Spec.ModuleLoader.Container.KernelMappings = []kmmv1beta1.KernelMapping{km}

		clnt.EXPECT().List(context.Background(), gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *kmmv1beta1.ModuleList, _ ...interface{}) error {
				list.Items = []kmmv1beta1.Module{mod1, mod2, mod3}
				return nil
			},
		)

		if modprobeArg != nil {
			mockMetrics.EXPECT().SetKMMModprobeArgs(mod1.Name, mod1.Namespace, strings.Join(modprobeArg, ","))
		}
		if modprobeRawArg != nil {
			mockMetrics.EXPECT().SetKMMModprobeRawArgs(mod1.Name, mod1.Namespace, strings.Join(modprobeRawArg, ","))
		}

		mockMetrics.EXPECT().SetKMMModulesNum(3)
		mockMetrics.EXPECT().SetKMMInClusterBuildNum(numBuild)
		mockMetrics.EXPECT().SetKMMInClusterSignNum(numSign)
		mockMetrics.EXPECT().SetKMMDevicePluginNum(numDevicePlugin)

		dprh.setKMMOMetrics(ctx)
	},
		Entry("build in container", true, false, false, false, false, nil, nil),
		Entry("build in KM", false, true, false, false, false, nil, nil),
		Entry("build in container and KM", true, true, false, false, false, nil, nil),
		Entry("sign in container", false, false, true, false, false, nil, nil),
		Entry("sign in KM", false, false, false, true, false, nil, nil),
		Entry("sign in container and KM", false, false, true, true, false, nil, nil),
		Entry("device plugin", false, false, false, false, true, nil, nil),
		Entry("modprobe args", false, false, false, false, false, []string{"param1", "param2"}, nil),
		Entry("modprobe raw args", false, false, false, false, false, nil, []string{"rawparam1", "rawparam2"}),
		Entry("altogether", true, true, true, true, true, []string{"param1", "param2"}, []string{"rawparam1", "rawparam2"}),
	)
})

var _ = Describe("DevicePluginReconciler_moduleUpdateDevicePluginStatus", func() {
	var (
		ctrl         *gomock.Controller
		clnt         *client.MockClient
		statusWriter *client.MockStatusWriter
		dprh         devicePluginReconcilerHelperAPI
		mn           node.Node
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		statusWriter = client.NewMockStatusWriter(ctrl)
		mn = node.NewNode(clnt)
		dprh = newDevicePluginReconcilerHelper(clnt, nil, mn, nil)
	})

	ctx := context.Background()

	It("device plugin not defined in the module", func() {
		mod := kmmv1beta1.Module{}
		err := dprh.moduleUpdateDevicePluginStatus(ctx, &mod, nil)
		Expect(err).NotTo(HaveOccurred())
	})

	DescribeTable("device-plugin status update",
		func(numTargetedNodes int, numAvailableInDaemonSets []int, nodesMatchingNumber, availableNumber int) {
			mod := kmmv1beta1.Module{
				Spec: kmmv1beta1.ModuleSpec{
					DevicePlugin: &kmmv1beta1.DevicePluginSpec{},
				},
			}
			expectedMod := mod.DeepCopy()
			expectedMod.Status.DevicePlugin.NodesMatchingSelectorNumber = int32(nodesMatchingNumber)
			expectedMod.Status.DevicePlugin.DesiredNumber = int32(nodesMatchingNumber)
			expectedMod.Status.DevicePlugin.AvailableNumber = int32(availableNumber)

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

			err := dprh.moduleUpdateDevicePluginStatus(ctx, &mod, daemonSetsList)
			Expect(err).NotTo(HaveOccurred())
		},
		Entry("0 target node, 0 ds", 0, nil, 0, 0),
		Entry("0 target node, 1 ds", 0, []int{1}, 0, 1),
		Entry("0 target node, 2 ds", 0, []int{3, 6}, 0, 9),
		Entry("3 target node, 0 ds", 3, nil, 3, 0),
		Entry("2 target node, 3 ds", 2, []int{3, 6, 8}, 2, 17),
	)
})

var _ = Describe("DevicePluginReconciler_setDevicePluginAsDesired", func() {
	const (
		devicePluginImage = "device-plugin-image"
	)

	var (
		dsc daemonSetCreator
	)

	BeforeEach(func() {
		dsc = newDaemonSetCreator(scheme)
	})

	It("should return an error if the DaemonSet is nil", func() {
		Expect(
			dsc.setDevicePluginAsDesired(context.Background(), nil, &kmmv1beta1.Module{}),
		).To(
			HaveOccurred(),
		)
	})

	It("should return an error if DevicePlugin not set in the Spec", func() {
		ds := appsv1.DaemonSet{}
		Expect(
			dsc.setDevicePluginAsDesired(context.Background(), &ds, &kmmv1beta1.Module{}),
		).To(
			HaveOccurred(),
		)
	})

	It("should add additional volumes if there are any", func() {
		vol := v1.Volume{Name: "test-volume"}

		mod := kmmv1beta1.Module{
			Spec: kmmv1beta1.ModuleSpec{
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{
					Container: kmmv1beta1.DevicePluginContainerSpec{Image: devicePluginImage},
					Volumes:   []v1.Volume{vol},
				},
			},
		}

		ds := appsv1.DaemonSet{}

		err := dsc.setDevicePluginAsDesired(context.Background(), &ds, &mod)
		Expect(err).NotTo(HaveOccurred())
		Expect(ds.Spec.Template.Spec.Volumes).To(HaveLen(2))
		Expect(ds.Spec.Template.Spec.Volumes[1]).To(Equal(vol))
	})

	It("should add module version if it was defined in the Module", func() {
		vol := v1.Volume{Name: "test-volume"}

		mod := kmmv1beta1.Module{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ModuleSpec{
				ModuleLoader: kmmv1beta1.ModuleLoaderSpec{
					Container: kmmv1beta1.ModuleLoaderContainerSpec{
						Version: "some version",
					},
				},
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{
					Container: kmmv1beta1.DevicePluginContainerSpec{Image: devicePluginImage},
					Volumes:   []v1.Volume{vol},
				},
			},
		}

		ds := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
		}

		err := dsc.setDevicePluginAsDesired(context.Background(), &ds, &mod)

		Expect(err).NotTo(HaveOccurred())
		versionLabel := utils.GetDevicePluginVersionLabelName(namespace, moduleName)
		Expect(ds.GetLabels()).Should(HaveKeyWithValue(versionLabel, "some version"))
	})

	It("should work as expected", func() {
		const (
			dsName             = "ds-name"
			serviceAccountName = "some-service-account"
		)

		dpVol := v1.Volume{
			Name:         "test-volume",
			VolumeSource: v1.VolumeSource{},
		}

		dpVolMount := v1.VolumeMount{
			Name:      "some-dp-volume-mount",
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

		mod := kmmv1beta1.Module{
			TypeMeta: metav1.TypeMeta{
				APIVersion: kmmv1beta1.GroupVersion.String(),
				Kind:       "Module",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      moduleName,
				Namespace: namespace,
			},
			Spec: kmmv1beta1.ModuleSpec{
				DevicePlugin: &kmmv1beta1.DevicePluginSpec{
					Container: kmmv1beta1.DevicePluginContainerSpec{
						Args:            args,
						Command:         command,
						Env:             env,
						Image:           devicePluginImage,
						ImagePullPolicy: ipp,
						Resources:       resources,
						VolumeMounts:    []v1.VolumeMount{dpVolMount},
					},
					ServiceAccountName: serviceAccountName,
					Volumes:            []v1.Volume{dpVol},
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

		err := dsc.setDevicePluginAsDesired(context.Background(), &ds, &mod)
		Expect(err).NotTo(HaveOccurred())

		podLabels := map[string]string{constants.ModuleNameLabel: moduleName}

		directory := v1.HostPathDirectory
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
						Name:               moduleName,
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
						Containers: []v1.Container{
							{
								Args:            args,
								Command:         command,
								Env:             env,
								Image:           devicePluginImage,
								ImagePullPolicy: ipp,
								Name:            "device-plugin",
								Resources:       resources,
								SecurityContext: &v1.SecurityContext{
									Privileged: ptr.To(true),
								},
								VolumeMounts: []v1.VolumeMount{
									dpVolMount,
									{
										Name:      "kubelet-device-plugins",
										MountPath: "/var/lib/kubelet/device-plugins",
									},
								},
							},
						},
						ImagePullSecrets: []v1.LocalObjectReference{repoSecret},
						NodeSelector: map[string]string{
							utils.GetKernelModuleReadyNodeLabel(mod.Namespace, mod.Name): "",
						},
						PriorityClassName:  "system-node-critical",
						ServiceAccountName: serviceAccountName,
						Volumes: []v1.Volume{
							{
								Name: "kubelet-device-plugins",
								VolumeSource: v1.VolumeSource{
									HostPath: &v1.HostPathVolumeSource{
										Path: "/var/lib/kubelet/device-plugins",
										Type: &directory,
									},
								},
							},
							dpVol,
						},
						Tolerations: []v1.Toleration{testToleration},
					},
				},
			},
		}
		Expect(
			cmp.Equal(expected, ds),
		).To(
			BeTrue(), cmp.Diff(expected, ds),
		)
	})
})

var _ = Describe("DevicePluginReconciler_getExistingDS", func() {
	const (
		moduleName      = "moduleName"
		moduleNamespace = "moduleNamespace"
		kernelVersion   = "kernelVersion"
		moduleVersion   = "moduleVersion"
	)

	devicePluginLabels := map[string]string{
		utils.GetDevicePluginVersionLabelName(moduleNamespace, moduleName): moduleVersion,
	}

	ds := appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Namespace: moduleNamespace, Name: moduleName},
	}

	It("various scenarios", func() {
		By("empty daemonset list")
		res := getExistingDS(nil, moduleNamespace, moduleName, moduleVersion)
		Expect(res).To(BeNil())

		By("device plugin, module version equal")
		ds.SetLabels(devicePluginLabels)
		res = getExistingDS([]appsv1.DaemonSet{ds}, moduleNamespace, moduleName, moduleVersion)
		Expect(res).To(Equal(&ds))

		By("device plugin, module version not equal")
		res = getExistingDS([]appsv1.DaemonSet{ds}, moduleNamespace, moduleName, "some version")
		Expect(res).To(BeNil())

		By("device plugin, module version label missing, and module version parameter is empty")
		ds.SetLabels(map[string]string{})
		res = getExistingDS([]appsv1.DaemonSet{ds}, moduleNamespace, moduleName, "")
		Expect(res).To(Equal(&ds))
	})
})

var _ = Describe("DevicePluginReconciler_getModuleDevicePluginDaemonSets", func() {
	var (
		ctrl *gomock.Controller
		clnt *client.MockClient
		dprh devicePluginReconcilerHelper
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		clnt = client.NewMockClient(ctrl)
		dprh = devicePluginReconcilerHelper{
			client: clnt,
		}
	})

	ctx := context.Background()

	It("list failed", func() {
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).Return(fmt.Errorf("some error"))

		dsList, err := dprh.getModuleDevicePluginDaemonSets(ctx, "name", "namespace")

		Expect(err).ToNot(BeNil())
		Expect(dsList).To(BeNil())
	})

	It("good flow, return only device plugin DSs, either v2 or v1", func() {
		ds1 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					constants.ModuleNameLabel: "some name",
				},
			},
		}
		ds2 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					constants.ModuleNameLabel: "some name",
					constants.DaemonSetRole:   constants.ModuleLoaderRoleLabelValue,
				},
			},
		}
		ds3 := appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					constants.ModuleNameLabel: "some name",
					constants.DaemonSetRole:   "some role",
				},
			},
		}
		clnt.EXPECT().List(ctx, gomock.Any(), gomock.Any()).DoAndReturn(
			func(_ interface{}, list *appsv1.DaemonSetList, _ ...interface{}) error {
				list.Items = []appsv1.DaemonSet{ds1, ds2, ds3}
				return nil
			},
		)

		dsList, err := dprh.getModuleDevicePluginDaemonSets(ctx, "name", "namespace")

		Expect(err).NotTo(HaveOccurred())
		Expect(dsList).To(Equal([]appsv1.DaemonSet{ds1, ds3}))
	})
})
