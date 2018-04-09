// Code generated by mockery v1.0.0
package mocks

import btcec "github.com/btcsuite/btcd/btcec"
import hdkeychain "github.com/btcsuite/btcutil/hdkeychain"
import mock "github.com/stretchr/testify/mock"

// KeyStoreWithoutKeyDerivation is an autogenerated mock type for the KeyStoreWithoutKeyDerivation type
type KeyStoreWithoutKeyDerivation struct {
	mock.Mock
}

// DisplayAddress provides a mock function with given fields: keyPath
func (_m *KeyStoreWithoutKeyDerivation) DisplayAddress(keyPath string) {
	_m.Called(keyPath)
}

// Sign provides a mock function with given fields: hashes, relativeKeyPaths
func (_m *KeyStoreWithoutKeyDerivation) Sign(hashes [][]byte, relativeKeyPaths []string) ([]btcec.Signature, error) {
	ret := _m.Called(hashes, relativeKeyPaths)

	var r0 []btcec.Signature
	if rf, ok := ret.Get(0).(func([][]byte, []string) []btcec.Signature); ok {
		r0 = rf(hashes, relativeKeyPaths)
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).([]btcec.Signature)
		}
	}

	var r1 error
	if rf, ok := ret.Get(1).(func([][]byte, []string) error); ok {
		r1 = rf(hashes, relativeKeyPaths)
	} else {
		r1 = ret.Error(1)
	}

	return r0, r1
}

// XPub provides a mock function with given fields:
func (_m *KeyStoreWithoutKeyDerivation) XPub() *hdkeychain.ExtendedKey {
	ret := _m.Called()

	var r0 *hdkeychain.ExtendedKey
	if rf, ok := ret.Get(0).(func() *hdkeychain.ExtendedKey); ok {
		r0 = rf()
	} else {
		if ret.Get(0) != nil {
			r0 = ret.Get(0).(*hdkeychain.ExtendedKey)
		}
	}

	return r0
}
