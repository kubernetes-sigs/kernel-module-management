// Code generated by MockGen. DO NOT EDIT.
// Source: mbsc.go
//
// Generated by this command:
//
//	mockgen -source=mbsc.go -package=mbsc -destination=mock_mbsc.go
//
// Package mbsc is a generated GoMock package.
package mbsc

import (
	reflect "reflect"

	v1beta1 "github.com/kubernetes-sigs/kernel-module-management/api/v1beta1"
	gomock "go.uber.org/mock/gomock"
)

// MockMBSC is a mock of MBSC interface.
type MockMBSC struct {
	ctrl     *gomock.Controller
	recorder *MockMBSCMockRecorder
}

// MockMBSCMockRecorder is the mock recorder for MockMBSC.
type MockMBSCMockRecorder struct {
	mock *MockMBSC
}

// NewMockMBSC creates a new mock instance.
func NewMockMBSC(ctrl *gomock.Controller) *MockMBSC {
	mock := &MockMBSC{ctrl: ctrl}
	mock.recorder = &MockMBSCMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockMBSC) EXPECT() *MockMBSCMockRecorder {
	return m.recorder
}

// SetModuleImageSpec mocks base method.
func (m *MockMBSC) SetModuleImageSpec(mbscObj *v1beta1.ModuleBuildSignConfig, moduleImageSpec *v1beta1.ModuleImageSpec) {
	m.ctrl.T.Helper()
	m.ctrl.Call(m, "SetModuleImageSpec", mbscObj, moduleImageSpec)
}

// SetModuleImageSpec indicates an expected call of SetModuleImageSpec.
func (mr *MockMBSCMockRecorder) SetModuleImageSpec(mbscObj, moduleImageSpec any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SetModuleImageSpec", reflect.TypeOf((*MockMBSC)(nil).SetModuleImageSpec), mbscObj, moduleImageSpec)
}