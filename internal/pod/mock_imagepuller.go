// Code generated by MockGen. DO NOT EDIT.
// Source: imagepuller.go
//
// Generated by this command:
//
//	mockgen -source=imagepuller.go -package=pod -destination=mock_imagepuller.go
//
// Package pod is a generated GoMock package.
package pod

import (
	context "context"
	reflect "reflect"

	v1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	gomock "go.uber.org/mock/gomock"
	v1 "k8s.io/api/core/v1"
)

// MockImagePuller is a mock of ImagePuller interface.
type MockImagePuller struct {
	ctrl     *gomock.Controller
	recorder *MockImagePullerMockRecorder
}

// MockImagePullerMockRecorder is the mock recorder for MockImagePuller.
type MockImagePullerMockRecorder struct {
	mock *MockImagePuller
}

// NewMockImagePuller creates a new mock instance.
func NewMockImagePuller(ctrl *gomock.Controller) *MockImagePuller {
	mock := &MockImagePuller{ctrl: ctrl}
	mock.recorder = &MockImagePullerMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockImagePuller) EXPECT() *MockImagePullerMockRecorder {
	return m.recorder
}

// CreatePullPod mocks base method.
func (m *MockImagePuller) CreatePullPod(ctx context.Context, imageSpec *v1beta1.ModuleImageSpec, micObj *v1beta1.ModuleImagesConfig) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "CreatePullPod", ctx, imageSpec, micObj)
	ret0, _ := ret[0].(error)
	return ret0
}

// CreatePullPod indicates an expected call of CreatePullPod.
func (mr *MockImagePullerMockRecorder) CreatePullPod(ctx, imageSpec, micObj any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "CreatePullPod", reflect.TypeOf((*MockImagePuller)(nil).CreatePullPod), ctx, imageSpec, micObj)
}

// DeletePod mocks base method.
func (m *MockImagePuller) DeletePod(ctx context.Context, pod *v1.Pod) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "DeletePod", ctx, pod)
	ret0, _ := ret[0].(error)
	return ret0
}

// DeletePod indicates an expected call of DeletePod.
func (mr *MockImagePullerMockRecorder) DeletePod(ctx, pod any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "DeletePod", reflect.TypeOf((*MockImagePuller)(nil).DeletePod), ctx, pod)
}

// GetPullPodForImage mocks base method.
func (m *MockImagePuller) GetPullPodForImage(pods []v1.Pod, image string) *v1.Pod {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPullPodForImage", pods, image)
	ret0, _ := ret[0].(*v1.Pod)
	return ret0
}

// GetPullPodForImage indicates an expected call of GetPullPodForImage.
func (mr *MockImagePullerMockRecorder) GetPullPodForImage(pods, image any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPullPodForImage", reflect.TypeOf((*MockImagePuller)(nil).GetPullPodForImage), pods, image)
}

// GetPullPodImage mocks base method.
func (m *MockImagePuller) GetPullPodImage(pod v1.Pod) string {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPullPodImage", pod)
	ret0, _ := ret[0].(string)
	return ret0
}

// GetPullPodImage indicates an expected call of GetPullPodImage.
func (mr *MockImagePullerMockRecorder) GetPullPodImage(pod any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPullPodImage", reflect.TypeOf((*MockImagePuller)(nil).GetPullPodImage), pod)
}

// GetPullPodStatus mocks base method.
func (m *MockImagePuller) GetPullPodStatus(pod *v1.Pod) PullPodStatus {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetPullPodStatus", pod)
	ret0, _ := ret[0].(PullPodStatus)
	return ret0
}

// GetPullPodStatus indicates an expected call of GetPullPodStatus.
func (mr *MockImagePullerMockRecorder) GetPullPodStatus(pod any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetPullPodStatus", reflect.TypeOf((*MockImagePuller)(nil).GetPullPodStatus), pod)
}

// ListPullPods mocks base method.
func (m *MockImagePuller) ListPullPods(ctx context.Context, micObj *v1beta1.ModuleImagesConfig) ([]v1.Pod, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "ListPullPods", ctx, micObj)
	ret0, _ := ret[0].([]v1.Pod)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// ListPullPods indicates an expected call of ListPullPods.
func (mr *MockImagePullerMockRecorder) ListPullPods(ctx, micObj any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "ListPullPods", reflect.TypeOf((*MockImagePuller)(nil).ListPullPods), ctx, micObj)
}
