// Code generated by mockery v2.42.1. DO NOT EDIT.

package model

import mock "github.com/stretchr/testify/mock"

// MockUpdateFn is an autogenerated mock type for the UpdateFn type
type MockUpdateFn struct {
	mock.Mock
}

type MockUpdateFn_Expecter struct {
	mock *mock.Mock
}

func (_m *MockUpdateFn) EXPECT() *MockUpdateFn_Expecter {
	return &MockUpdateFn_Expecter{mock: &_m.Mock}
}

// Execute provides a mock function with given fields: _a0
func (_m *MockUpdateFn) Execute(_a0 string) {
	_m.Called(_a0)
}

// MockUpdateFn_Execute_Call is a *mock.Call that shadows Run/Return methods with type explicit version for method 'Execute'
type MockUpdateFn_Execute_Call struct {
	*mock.Call
}

// Execute is a helper method to define mock.On call
//   - _a0 string
func (_e *MockUpdateFn_Expecter) Execute(_a0 interface{}) *MockUpdateFn_Execute_Call {
	return &MockUpdateFn_Execute_Call{Call: _e.mock.On("Execute", _a0)}
}

func (_c *MockUpdateFn_Execute_Call) Run(run func(_a0 string)) *MockUpdateFn_Execute_Call {
	_c.Call.Run(func(args mock.Arguments) {
		run(args[0].(string))
	})
	return _c
}

func (_c *MockUpdateFn_Execute_Call) Return() *MockUpdateFn_Execute_Call {
	_c.Call.Return()
	return _c
}

func (_c *MockUpdateFn_Execute_Call) RunAndReturn(run func(string)) *MockUpdateFn_Execute_Call {
	_c.Call.Return(run)
	return _c
}

// NewMockUpdateFn creates a new instance of MockUpdateFn. It also registers a testing interface on the mock and a cleanup function to assert the mocks expectations.
// The first argument is typically a *testing.T value.
func NewMockUpdateFn(t interface {
	mock.TestingT
	Cleanup(func())
}) *MockUpdateFn {
	mock := &MockUpdateFn{}
	mock.Mock.Test(t)

	t.Cleanup(func() { mock.AssertExpectations(t) })

	return mock
}
