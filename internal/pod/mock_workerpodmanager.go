// Code generated by MockGen. DO NOT EDIT.
// Source: workerpodmanager.go
//
// Generated by this command:
//
//	mockgen -source=workerpodmanager.go -package=pod -destination=mock_workerpodmanager.go
//
// Package pod is a generated GoMock package.
package pod

import (
	context "context"
	reflect "reflect"

	v1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	gomock "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

// MockWorkerPodManager is a mock of WorkerPodManager interface.
type MockWorkerPodManager struct {
	ctrl     *gomock.Controller
	recorder *MockWorkerPodManagerMockRecorder
}

// MockWorkerPodManagerMockRecorder is the mock recorder for MockWorkerPodManager.
type MockWorkerPodManagerMockRecorder struct {
	mock *MockWorkerPodManager
}

// NewMockWorkerPodManager creates a new mock instance.
func NewMockWorkerPodManager(ctrl *gomock.Controller) *MockWorkerPodManager {
	mock := &MockWorkerPodManager{ctrl: ctrl}
	mock.recorder = &MockWorkerPodManagerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockWorkerPodManager) EXPECT() *MockWorkerPodManagerMockRecorder {
	return m.recorder
}

// CreateLoaderPod mocks base method.
func (m *MockWorkerPodManager) CreateLoaderPod(ctx context.Context, nmc client.Object, nms *v1beta1.NodeModuleSpec) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateLoaderPod", ctx, nmc, nms)
	ret0, _ := ret[0].(error)
	return ret0
}

// CreateLoaderPod indicates an expected call of CreateLoaderPod.
func (mr *MockWorkerPodManagerMockRecorder) CreateLoaderPod(ctx, nmc, nms any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateLoaderPod", reflect.TypeOf((*MockWorkerPodManager)(nil).CreateLoaderPod), ctx, nmc, nms)
}

// CreateUnloaderPod mocks base method.
func (m *MockWorkerPodManager) CreateUnloaderPod(ctx context.Context, nmc client.Object, nms *v1beta1.NodeModuleStatus) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreateUnloaderPod", ctx, nmc, nms)
	ret0, _ := ret[0].(error)
	return ret0
}

// CreateUnloaderPod indicates an expected call of CreateUnloaderPod.
func (mr *MockWorkerPodManagerMockRecorder) CreateUnloaderPod(ctx, nmc, nms any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreateUnloaderPod", reflect.TypeOf((*MockWorkerPodManager)(nil).CreateUnloaderPod), ctx, nmc, nms)
}

// DeletePod mocks base method.
func (m *MockWorkerPodManager) DeletePod(ctx context.Context, pod *v1.Pod) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeletePod", ctx, pod)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeletePod indicates an expected call of DeletePod.
func (mr *MockWorkerPodManagerMockRecorder) DeletePod(ctx, pod any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeletePod", reflect.TypeOf((*MockWorkerPodManager)(nil).DeletePod), ctx, pod)
}

// GetWorkerPod mocks base method.
func (m *MockWorkerPodManager) GetWorkerPod(ctx context.Context, podName, namespace string) (*v1.Pod, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetWorkerPod", ctx, podName, namespace)
	ret0, _ := ret[0].(*v1.Pod)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetWorkerPod indicates an expected call of GetWorkerPod.
func (mr *MockWorkerPodManagerMockRecorder) GetWorkerPod(ctx, podName, namespace any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetWorkerPod", reflect.TypeOf((*MockWorkerPodManager)(nil).GetWorkerPod), ctx, podName, namespace)
}

// ListWorkerPodsOnNode mocks base method.
func (m *MockWorkerPodManager) ListWorkerPodsOnNode(ctx context.Context, nodeName string) ([]v1.Pod, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListWorkerPodsOnNode", ctx, nodeName)
	ret0, _ := ret[0].([]v1.Pod)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ListWorkerPodsOnNode indicates an expected call of ListWorkerPodsOnNode.
func (mr *MockWorkerPodManagerMockRecorder) ListWorkerPodsOnNode(ctx, nodeName any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListWorkerPodsOnNode", reflect.TypeOf((*MockWorkerPodManager)(nil).ListWorkerPodsOnNode), ctx, nodeName)
}

// LoaderPodTemplate mocks base method.
func (m *MockWorkerPodManager) LoaderPodTemplate(ctx context.Context, nmc client.Object, nms *v1beta1.NodeModuleSpec) (*v1.Pod, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "LoaderPodTemplate", ctx, nmc, nms)
	ret0, _ := ret[0].(*v1.Pod)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// LoaderPodTemplate indicates an expected call of LoaderPodTemplate.
func (mr *MockWorkerPodManagerMockRecorder) LoaderPodTemplate(ctx, nmc, nms any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "LoaderPodTemplate", reflect.TypeOf((*MockWorkerPodManager)(nil).LoaderPodTemplate), ctx, nmc, nms)
}

// UnloaderPodTemplate mocks base method.
func (m *MockWorkerPodManager) UnloaderPodTemplate(ctx context.Context, nmc client.Object, nms *v1beta1.NodeModuleStatus) (*v1.Pod, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "UnloaderPodTemplate", ctx, nmc, nms)
	ret0, _ := ret[0].(*v1.Pod)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// UnloaderPodTemplate indicates an expected call of UnloaderPodTemplate.
func (mr *MockWorkerPodManagerMockRecorder) UnloaderPodTemplate(ctx, nmc, nms any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "UnloaderPodTemplate", reflect.TypeOf((*MockWorkerPodManager)(nil).UnloaderPodTemplate), ctx, nmc, nms)
}
