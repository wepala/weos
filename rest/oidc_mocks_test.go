// Code generated by moq; DO NOT EDIT.
// github.com/matryer/moq

package rest_test

import (
	"context"
	"github.com/coreos/go-oidc/v3/oidc"
	"sync"
)

// Ensure, that KeySetMock does implement oidc.KeySet.
// If this is not the case, regenerate this file with moq.
var _ oidc.KeySet = &KeySetMock{}

// KeySetMock is a mock implementation of oidc.KeySet.
//
//	func TestSomethingThatUsesKeySet(t *testing.T) {
//
//		// make and configure a mocked oidc.KeySet
//		mockedKeySet := &KeySetMock{
//			VerifySignatureFunc: func(ctx context.Context, jwt string) ([]byte, error) {
//				panic("mock out the VerifySignature method")
//			},
//		}
//
//		// use mockedKeySet in code that requires oidc.KeySet
//		// and then make assertions.
//
//	}
type KeySetMock struct {
	// VerifySignatureFunc mocks the VerifySignature method.
	VerifySignatureFunc func(ctx context.Context, jwt string) ([]byte, error)

	// calls tracks calls to the methods.
	calls struct {
		// VerifySignature holds details about calls to the VerifySignature method.
		VerifySignature []struct {
			// Ctx is the ctx argument value.
			Ctx context.Context
			// Jwt is the jwt argument value.
			Jwt string
		}
	}
	lockVerifySignature sync.RWMutex
}

// VerifySignature calls VerifySignatureFunc.
func (mock *KeySetMock) VerifySignature(ctx context.Context, jwt string) ([]byte, error) {
	if mock.VerifySignatureFunc == nil {
		panic("KeySetMock.VerifySignatureFunc: method is nil but KeySet.VerifySignature was just called")
	}
	callInfo := struct {
		Ctx context.Context
		Jwt string
	}{
		Ctx: ctx,
		Jwt: jwt,
	}
	mock.lockVerifySignature.Lock()
	mock.calls.VerifySignature = append(mock.calls.VerifySignature, callInfo)
	mock.lockVerifySignature.Unlock()
	return mock.VerifySignatureFunc(ctx, jwt)
}

// VerifySignatureCalls gets all the calls that were made to VerifySignature.
// Check the length with:
//
//	len(mockedKeySet.VerifySignatureCalls())
func (mock *KeySetMock) VerifySignatureCalls() []struct {
	Ctx context.Context
	Jwt string
} {
	var calls []struct {
		Ctx context.Context
		Jwt string
	}
	mock.lockVerifySignature.RLock()
	calls = mock.calls.VerifySignature
	mock.lockVerifySignature.RUnlock()
	return calls
}