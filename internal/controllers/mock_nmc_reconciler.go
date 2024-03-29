// Code generated by MockGen. DO NOT EDIT.
// Source: nmc_reconciler.go
//
// Generated by this command:
//
//	mockgen -source=nmc_reconciler.go -package=controllers -destination=mock_nmc_reconciler.go pullSecretHelper
//
// Package controllers is a generated GoMock package.
package controllers

import (
	context "context"
	reflect "reflect"

	v1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	gomock "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

// MocknmcReconcilerHelper is a mock of nmcReconcilerHelper interface.
type MocknmcReconcilerHelper struct {
	ctrl     *gomock.Controller
	recorder *MocknmcReconcilerHelperMockRecorder
}

// MocknmcReconcilerHelperMockRecorder is the mock recorder for MocknmcReconcilerHelper.
type MocknmcReconcilerHelperMockRecorder struct {
	mock *MocknmcReconcilerHelper
}

// NewMocknmcReconcilerHelper creates a new mock instance.
func NewMocknmcReconcilerHelper(ctrl *gomock.Controller) *MocknmcReconcilerHelper {
	mock := &MocknmcReconcilerHelper{ctrl: ctrl}
	mock.recorder = &MocknmcReconcilerHelperMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MocknmcReconcilerHelper) EXPECT() *MocknmcReconcilerHelperMockRecorder {
	return m.recorder
}

// GarbageCollectInUseLabels mocks base method.
func (m *MocknmcReconcilerHelper) GarbageCollectInUseLabels(ctx context.Context, nmc *v1beta1.NodeModulesConfig) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GarbageCollectInUseLabels", ctx, nmc)
	ret0, _ := ret[0].(error)
	return ret0
}

// GarbageCollectInUseLabels indicates an expected call of GarbageCollectInUseLabels.
func (mr *MocknmcReconcilerHelperMockRecorder) GarbageCollectInUseLabels(ctx, nmc any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GarbageCollectInUseLabels", reflect.TypeOf((*MocknmcReconcilerHelper)(nil).GarbageCollectInUseLabels), ctx, nmc)
}

// ProcessModuleSpec mocks base method.
func (m *MocknmcReconcilerHelper) ProcessModuleSpec(ctx context.Context, nmc *v1beta1.NodeModulesConfig, spec *v1beta1.NodeModuleSpec, status *v1beta1.NodeModuleStatus) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ProcessModuleSpec", ctx, nmc, spec, status)
	ret0, _ := ret[0].(error)
	return ret0
}

// ProcessModuleSpec indicates an expected call of ProcessModuleSpec.
func (mr *MocknmcReconcilerHelperMockRecorder) ProcessModuleSpec(ctx, nmc, spec, status any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ProcessModuleSpec", reflect.TypeOf((*MocknmcReconcilerHelper)(nil).ProcessModuleSpec), ctx, nmc, spec, status)
}

// ProcessUnconfiguredModuleStatus mocks base method.
func (m *MocknmcReconcilerHelper) ProcessUnconfiguredModuleStatus(ctx context.Context, nmc *v1beta1.NodeModulesConfig, status *v1beta1.NodeModuleStatus) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ProcessUnconfiguredModuleStatus", ctx, nmc, status)
	ret0, _ := ret[0].(error)
	return ret0
}

// ProcessUnconfiguredModuleStatus indicates an expected call of ProcessUnconfiguredModuleStatus.
func (mr *MocknmcReconcilerHelperMockRecorder) ProcessUnconfiguredModuleStatus(ctx, nmc, status any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ProcessUnconfiguredModuleStatus", reflect.TypeOf((*MocknmcReconcilerHelper)(nil).ProcessUnconfiguredModuleStatus), ctx, nmc, status)
}

// RemovePodFinalizers mocks base method.
func (m *MocknmcReconcilerHelper) RemovePodFinalizers(ctx context.Context, nodeName string) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "RemovePodFinalizers", ctx, nodeName)
	ret0, _ := ret[0].(error)
	return ret0
}

// RemovePodFinalizers indicates an expected call of RemovePodFinalizers.
func (mr *MocknmcReconcilerHelperMockRecorder) RemovePodFinalizers(ctx, nodeName any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "RemovePodFinalizers", reflect.TypeOf((*MocknmcReconcilerHelper)(nil).RemovePodFinalizers), ctx, nodeName)
}

// SyncStatus mocks base method.
func (m *MocknmcReconcilerHelper) SyncStatus(ctx context.Context, nmc *v1beta1.NodeModulesConfig) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SyncStatus", ctx, nmc)
	ret0, _ := ret[0].(error)
	return ret0
}

// SyncStatus indicates an expected call of SyncStatus.
func (mr *MocknmcReconcilerHelperMockRecorder) SyncStatus(ctx, nmc any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SyncStatus", reflect.TypeOf((*MocknmcReconcilerHelper)(nil).SyncStatus), ctx, nmc)
}

// UpdateNodeLabelsAndRecordEvents mocks base method.
func (m *MocknmcReconcilerHelper) UpdateNodeLabelsAndRecordEvents(ctx context.Context, nmc *v1beta1.NodeModulesConfig) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UpdateNodeLabelsAndRecordEvents", ctx, nmc)
	ret0, _ := ret[0].(error)
	return ret0
}

// UpdateNodeLabelsAndRecordEvents indicates an expected call of UpdateNodeLabelsAndRecordEvents.
func (mr *MocknmcReconcilerHelperMockRecorder) UpdateNodeLabelsAndRecordEvents(ctx, nmc any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UpdateNodeLabelsAndRecordEvents", reflect.TypeOf((*MocknmcReconcilerHelper)(nil).UpdateNodeLabelsAndRecordEvents), ctx, nmc)
}

// MockpodManager is a mock of podManager interface.
type MockpodManager struct {
	ctrl     *gomock.Controller
	recorder *MockpodManagerMockRecorder
}

// MockpodManagerMockRecorder is the mock recorder for MockpodManager.
type MockpodManagerMockRecorder struct {
	mock *MockpodManager
}

// NewMockpodManager creates a new mock instance.
func NewMockpodManager(ctrl *gomock.Controller) *MockpodManager {
	mock := &MockpodManager{ctrl: ctrl}
	mock.recorder = &MockpodManagerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockpodManager) EXPECT() *MockpodManagerMockRecorder {
	return m.recorder
}

// CreateLoaderPod mocks base method.
func (m *MockpodManager) CreateLoaderPod(ctx context.Context, nmc client.Object, nms *v1beta1.NodeModuleSpec) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateLoaderPod", ctx, nmc, nms)
	ret0, _ := ret[0].(error)
	return ret0
}

// CreateLoaderPod indicates an expected call of CreateLoaderPod.
func (mr *MockpodManagerMockRecorder) CreateLoaderPod(ctx, nmc, nms any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateLoaderPod", reflect.TypeOf((*MockpodManager)(nil).CreateLoaderPod), ctx, nmc, nms)
}

// CreateUnloaderPod mocks base method.
func (m *MockpodManager) CreateUnloaderPod(ctx context.Context, nmc client.Object, nms *v1beta1.NodeModuleStatus) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateUnloaderPod", ctx, nmc, nms)
	ret0, _ := ret[0].(error)
	return ret0
}

// CreateUnloaderPod indicates an expected call of CreateUnloaderPod.
func (mr *MockpodManagerMockRecorder) CreateUnloaderPod(ctx, nmc, nms any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateUnloaderPod", reflect.TypeOf((*MockpodManager)(nil).CreateUnloaderPod), ctx, nmc, nms)
}

// DeletePod mocks base method.
func (m *MockpodManager) DeletePod(ctx context.Context, pod *v1.Pod) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeletePod", ctx, pod)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeletePod indicates an expected call of DeletePod.
func (mr *MockpodManagerMockRecorder) DeletePod(ctx, pod any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeletePod", reflect.TypeOf((*MockpodManager)(nil).DeletePod), ctx, pod)
}

// GetWorkerPod mocks base method.
func (m *MockpodManager) GetWorkerPod(ctx context.Context, podName, namespace string) (*v1.Pod, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetWorkerPod", ctx, podName, namespace)
	ret0, _ := ret[0].(*v1.Pod)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetWorkerPod indicates an expected call of GetWorkerPod.
func (mr *MockpodManagerMockRecorder) GetWorkerPod(ctx, podName, namespace any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetWorkerPod", reflect.TypeOf((*MockpodManager)(nil).GetWorkerPod), ctx, podName, namespace)
}

// ListWorkerPodsOnNode mocks base method.
func (m *MockpodManager) ListWorkerPodsOnNode(ctx context.Context, nodeName string) ([]v1.Pod, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListWorkerPodsOnNode", ctx, nodeName)
	ret0, _ := ret[0].([]v1.Pod)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ListWorkerPodsOnNode indicates an expected call of ListWorkerPodsOnNode.
func (mr *MockpodManagerMockRecorder) ListWorkerPodsOnNode(ctx, nodeName any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListWorkerPodsOnNode", reflect.TypeOf((*MockpodManager)(nil).ListWorkerPodsOnNode), ctx, nodeName)
}

// LoaderPodTemplate mocks base method.
func (m *MockpodManager) LoaderPodTemplate(ctx context.Context, nmc client.Object, nms *v1beta1.NodeModuleSpec) (*v1.Pod, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LoaderPodTemplate", ctx, nmc, nms)
	ret0, _ := ret[0].(*v1.Pod)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// LoaderPodTemplate indicates an expected call of LoaderPodTemplate.
func (mr *MockpodManagerMockRecorder) LoaderPodTemplate(ctx, nmc, nms any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LoaderPodTemplate", reflect.TypeOf((*MockpodManager)(nil).LoaderPodTemplate), ctx, nmc, nms)
}

// UnloaderPodTemplate mocks base method.
func (m *MockpodManager) UnloaderPodTemplate(ctx context.Context, nmc client.Object, nms *v1beta1.NodeModuleStatus) (*v1.Pod, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UnloaderPodTemplate", ctx, nmc, nms)
	ret0, _ := ret[0].(*v1.Pod)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// UnloaderPodTemplate indicates an expected call of UnloaderPodTemplate.
func (mr *MockpodManagerMockRecorder) UnloaderPodTemplate(ctx, nmc, nms any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UnloaderPodTemplate", reflect.TypeOf((*MockpodManager)(nil).UnloaderPodTemplate), ctx, nmc, nms)
}

// MockpullSecretHelper is a mock of pullSecretHelper interface.
type MockpullSecretHelper struct {
	ctrl     *gomock.Controller
	recorder *MockpullSecretHelperMockRecorder
}

// MockpullSecretHelperMockRecorder is the mock recorder for MockpullSecretHelper.
type MockpullSecretHelperMockRecorder struct {
	mock *MockpullSecretHelper
}

// NewMockpullSecretHelper creates a new mock instance.
func NewMockpullSecretHelper(ctrl *gomock.Controller) *MockpullSecretHelper {
	mock := &MockpullSecretHelper{ctrl: ctrl}
	mock.recorder = &MockpullSecretHelperMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockpullSecretHelper) EXPECT() *MockpullSecretHelperMockRecorder {
	return m.recorder
}

// VolumesAndVolumeMounts mocks base method.
func (m *MockpullSecretHelper) VolumesAndVolumeMounts(ctx context.Context, nms *v1beta1.ModuleItem) ([]v1.Volume, []v1.VolumeMount, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "VolumesAndVolumeMounts", ctx, nms)
	ret0, _ := ret[0].([]v1.Volume)
	ret1, _ := ret[1].([]v1.VolumeMount)
	ret2, _ := ret[2].(error)
	return ret0, ret1, ret2
}

// VolumesAndVolumeMounts indicates an expected call of VolumesAndVolumeMounts.
func (mr *MockpullSecretHelperMockRecorder) VolumesAndVolumeMounts(ctx, nms any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "VolumesAndVolumeMounts", reflect.TypeOf((*MockpullSecretHelper)(nil).VolumesAndVolumeMounts), ctx, nms)
}
