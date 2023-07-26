// Code generated by MockGen. DO NOT EDIT.
// Source: podhelper.go

// Package utils is a generated GoMock package.
package utils

import (
	context "context"
	reflect "reflect"

	gomock "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
	v10 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// MockPodHelper is a mock of PodHelper interface.
type MockPodHelper struct {
	ctrl     *gomock.Controller
	recorder *MockPodHelperMockRecorder
}

// MockPodHelperMockRecorder is the mock recorder for MockPodHelper.
type MockPodHelperMockRecorder struct {
	mock *MockPodHelper
}

// NewMockPodHelper creates a new mock instance.
func NewMockPodHelper(ctrl *gomock.Controller) *MockPodHelper {
	mock := &MockPodHelper{ctrl: ctrl}
	mock.recorder = &MockPodHelperMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockPodHelper) EXPECT() *MockPodHelperMockRecorder {
	return m.recorder
}

// CreatePod mocks base method.
func (m *MockPodHelper) CreatePod(ctx context.Context, podSpec *v1.Pod) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreatePod", ctx, podSpec)
	ret0, _ := ret[0].(error)
	return ret0
}

// CreatePod indicates an expected call of CreatePod.
func (mr *MockPodHelperMockRecorder) CreatePod(ctx, podSpec interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreatePod", reflect.TypeOf((*MockPodHelper)(nil).CreatePod), ctx, podSpec)
}

// DeletePod mocks base method.
func (m *MockPodHelper) DeletePod(ctx context.Context, pod *v1.Pod) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeletePod", ctx, pod)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeletePod indicates an expected call of DeletePod.
func (mr *MockPodHelperMockRecorder) DeletePod(ctx, pod interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeletePod", reflect.TypeOf((*MockPodHelper)(nil).DeletePod), ctx, pod)
}

// GetModulePodByKernel mocks base method.
func (m *MockPodHelper) GetModulePodByKernel(ctx context.Context, modName, namespace, targetKernel, podType string, owner v10.Object) (*v1.Pod, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetModulePodByKernel", ctx, modName, namespace, targetKernel, podType, owner)
	ret0, _ := ret[0].(*v1.Pod)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetModulePodByKernel indicates an expected call of GetModulePodByKernel.
func (mr *MockPodHelperMockRecorder) GetModulePodByKernel(ctx, modName, namespace, targetKernel, podType, owner interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetModulePodByKernel", reflect.TypeOf((*MockPodHelper)(nil).GetModulePodByKernel), ctx, modName, namespace, targetKernel, podType, owner)
}

// GetModulePods mocks base method.
func (m *MockPodHelper) GetModulePods(ctx context.Context, modName, namespace, podType string, owner v10.Object) ([]v1.Pod, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetModulePods", ctx, modName, namespace, podType, owner)
	ret0, _ := ret[0].([]v1.Pod)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetModulePods indicates an expected call of GetModulePods.
func (mr *MockPodHelperMockRecorder) GetModulePods(ctx, modName, namespace, podType, owner interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetModulePods", reflect.TypeOf((*MockPodHelper)(nil).GetModulePods), ctx, modName, namespace, podType, owner)
}

// GetPodStatus mocks base method.
func (m *MockPodHelper) GetPodStatus(pod *v1.Pod) (Status, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPodStatus", pod)
	ret0, _ := ret[0].(Status)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetPodStatus indicates an expected call of GetPodStatus.
func (mr *MockPodHelperMockRecorder) GetPodStatus(pod interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPodStatus", reflect.TypeOf((*MockPodHelper)(nil).GetPodStatus), pod)
}

// IsPodChanged mocks base method.
func (m *MockPodHelper) IsPodChanged(existingPod, newPod *v1.Pod) (bool, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "IsPodChanged", existingPod, newPod)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// IsPodChanged indicates an expected call of IsPodChanged.
func (mr *MockPodHelperMockRecorder) IsPodChanged(existingPod, newPod interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "IsPodChanged", reflect.TypeOf((*MockPodHelper)(nil).IsPodChanged), existingPod, newPod)
}

// PodLabels mocks base method.
func (m *MockPodHelper) PodLabels(modName, targetKernel, podType string) map[string]string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PodLabels", modName, targetKernel, podType)
	ret0, _ := ret[0].(map[string]string)
	return ret0
}

// PodLabels indicates an expected call of PodLabels.
func (mr *MockPodHelperMockRecorder) PodLabels(modName, targetKernel, podType interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PodLabels", reflect.TypeOf((*MockPodHelper)(nil).PodLabels), modName, targetKernel, podType)
}
