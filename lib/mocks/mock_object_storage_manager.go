// Automatically generated by MockGen. DO NOT EDIT!
// Source: github.com/dollarshaveclub/furan/lib/s3 (interfaces: ObjectStorageManager)

package mocks

import (
	s3 "github.com/dollarshaveclub/furan/lib/s3"
	gomock "github.com/golang/mock/gomock"
	io "io"
)

// Mock of ObjectStorageManager interface
type MockObjectStorageManager struct {
	ctrl     *gomock.Controller
	recorder *_MockObjectStorageManagerRecorder
}

// Recorder for MockObjectStorageManager (not exported)
type _MockObjectStorageManagerRecorder struct {
	mock *MockObjectStorageManager
}

func NewMockObjectStorageManager(ctrl *gomock.Controller) *MockObjectStorageManager {
	mock := &MockObjectStorageManager{ctrl: ctrl}
	mock.recorder = &_MockObjectStorageManagerRecorder{mock}
	return mock
}

func (_m *MockObjectStorageManager) EXPECT() *_MockObjectStorageManagerRecorder {
	return _m.recorder
}

func (_m *MockObjectStorageManager) Exists(_param0 s3.ImageDescription, _param1 interface{}) (bool, error) {
	ret := _m.ctrl.Call(_m, "Exists", _param0, _param1)
	ret0, _ := ret[0].(bool)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockObjectStorageManagerRecorder) Exists(arg0, arg1 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Exists", arg0, arg1)
}

func (_m *MockObjectStorageManager) Pull(_param0 s3.ImageDescription, _param1 io.WriterAt, _param2 interface{}) error {
	ret := _m.ctrl.Call(_m, "Pull", _param0, _param1, _param2)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockObjectStorageManagerRecorder) Pull(arg0, arg1, arg2 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Pull", arg0, arg1, arg2)
}

func (_m *MockObjectStorageManager) Push(_param0 s3.ImageDescription, _param1 io.Reader, _param2 interface{}) error {
	ret := _m.ctrl.Call(_m, "Push", _param0, _param1, _param2)
	ret0, _ := ret[0].(error)
	return ret0
}

func (_mr *_MockObjectStorageManagerRecorder) Push(arg0, arg1, arg2 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Push", arg0, arg1, arg2)
}

func (_m *MockObjectStorageManager) Size(_param0 s3.ImageDescription, _param1 interface{}) (int64, error) {
	ret := _m.ctrl.Call(_m, "Size", _param0, _param1)
	ret0, _ := ret[0].(int64)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockObjectStorageManagerRecorder) Size(arg0, arg1 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "Size", arg0, arg1)
}

func (_m *MockObjectStorageManager) WriteFile(_param0 string, _param1 s3.ImageDescription, _param2 string, _param3 io.Reader, _param4 interface{}) (string, error) {
	ret := _m.ctrl.Call(_m, "WriteFile", _param0, _param1, _param2, _param3, _param4)
	ret0, _ := ret[0].(string)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

func (_mr *_MockObjectStorageManagerRecorder) WriteFile(arg0, arg1, arg2, arg3, arg4 interface{}) *gomock.Call {
	return _mr.mock.ctrl.RecordCall(_mr.mock, "WriteFile", arg0, arg1, arg2, arg3, arg4)
}