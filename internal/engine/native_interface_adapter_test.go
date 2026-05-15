// Copyright 2026 PolitePixels Limited
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// This project stands against fascism, authoritarianism, and all forms of
// oppression. We built this to empower people, not to enable those who would
// strip others of their rights and dignity.

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPipitErrorAdapterImplementsErrorInterface(t *testing.T) {
	t.Parallel()

	adapter := &pipitErrorAdapter{}
	var asErr error = adapter
	require.NotNil(t, asErr,
		"the adapter must satisfy Go's error interface at compile time so reflect.Implements(errorInterface) returns true at runtime")
}

func TestErrorReflectTypeMatchesInterface(t *testing.T) {
	t.Parallel()

	require.Equal(t, reflect.Interface, errorReflectType.Kind(),
		"errorReflectType must be cached as the reflect.Type of the error interface so tryBuildInterfaceAdapter can compare expectedType against it directly")
	require.Equal(t, "error", errorReflectType.Name())
}

func TestPointerReceiverForReturnsAddressableForStruct(t *testing.T) {
	t.Parallel()

	type sample struct{ Value int }
	value := reflect.ValueOf(sample{Value: 5})
	require.False(t, value.CanAddr(),
		"baseline: reflect.ValueOf(struct) is not addressable")

	receiver := pointerReceiverFor(value)
	require.Equal(t, reflect.Pointer, receiver.Kind(),
		"a value-shaped struct receiver must be wrapped into a pointer so bound-method dispatch can take Addr() in the standard way")
}

func TestPointerReceiverForPassesPointerThrough(t *testing.T) {
	t.Parallel()

	type sample struct{ Value int }
	original := &sample{Value: 7}
	receiver := pointerReceiverFor(reflect.ValueOf(original))
	require.Equal(t, reflect.Pointer, receiver.Kind())
	require.Equal(t, uintptr(reflect.ValueOf(original).Pointer()), receiver.Pointer(),
		"already-pointer receivers must pass through without indirection so the bound method sees the caller's exact pointer")
}

func TestTryBuildInterfaceAdapterRequiresPipitSynthesisedType(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	standalone := struct{ Code int }{Code: 42}
	result := tryBuildInterfaceAdapter(vm, reflect.ValueOf(standalone), errorReflectType, argumentTypeContext{})
	require.False(t, result.IsValid(),
		"argument types not registered in rootFunction.typeNames must yield an invalid result so coerceReflectArgument falls back to its existing Convert path")
}

func TestTryBuildInterfaceAdapterRequiresErrorInterface(t *testing.T) {
	t.Parallel()

	type sample struct{}
	otherInterface := reflect.TypeFor[interface{ Foo() }]()
	vm := newTestVM(t)
	result := tryBuildInterfaceAdapter(vm, reflect.ValueOf(sample{}), otherInterface, argumentTypeContext{})
	require.False(t, result.IsValid(),
		"only the error interface is supported in this iteration; other interfaces fall through to the Convert path until per-interface adapters are added")
}

func TestTryBuildInterfaceAdapterHandlesNilVM(t *testing.T) {
	t.Parallel()

	type sample struct{}
	result := tryBuildInterfaceAdapter(nil, reflect.ValueOf(sample{}), errorReflectType, argumentTypeContext{})
	require.False(t, result.IsValid(),
		"a nil VM must short-circuit the adapter check rather than panic")
}

type pipitErrorsAsTestErr struct{ code int }

func (e *pipitErrorsAsTestErr) Error() string { return "test-err" }

type pipitErrorsAsWrappedErr struct{ inner error }

func (e *pipitErrorsAsWrappedErr) Error() string { return "wrapped" }
func (e *pipitErrorsAsWrappedErr) Unwrap() error { return e.inner }

func TestPipitErrorsAsDirectMatch(t *testing.T) {
	t.Parallel()

	inner := &pipitErrorsAsTestErr{code: 42}
	var target *pipitErrorsAsTestErr
	matched := pipitErrorsAs(nil, reflect.ValueOf(inner), reflect.ValueOf(&target))
	require.True(t, matched,
		"errors.As must succeed when the chain's concrete type is identical to *target's element type so direct *MyErr targets resolve without consulting Unwrap")
	require.NotNil(t, target)
	require.Equal(t, 42, target.code)
}

func TestPipitErrorsAsThroughUnwrapChain(t *testing.T) {
	t.Parallel()

	leaf := &pipitErrorsAsTestErr{code: 7}
	wrapped := &pipitErrorsAsWrappedErr{inner: leaf}
	var target *pipitErrorsAsTestErr
	matched := pipitErrorsAs(nil, reflect.ValueOf(wrapped), reflect.ValueOf(&target))
	require.True(t, matched,
		"errors.As must walk Unwrap chains so targets nested behind wrappers (the common stdlib pattern) still resolve")
	require.Equal(t, 7, target.code)
}

func TestPipitErrorsAsReturnsFalseOnMissing(t *testing.T) {
	t.Parallel()

	leaf := &pipitErrorsAsTestErr{code: 1}
	var target *pipitErrorsAsWrappedErr
	matched := pipitErrorsAs(nil, reflect.ValueOf(leaf), reflect.ValueOf(&target))
	require.False(t, matched,
		"errors.As must return false (and leave target untouched) when no chain element matches the target type")
	require.Nil(t, target)
}

func TestPipitErrorsAsPanicsOnNilTarget(t *testing.T) {
	t.Parallel()

	require.PanicsWithValue(t,
		"errors: target must be a non-nil pointer",
		func() {
			var target *pipitErrorsAsTestErr
			pipitErrorsAs(nil, reflect.ValueOf(&pipitErrorsAsTestErr{}), reflect.ValueOf(target))
		},
		"the panic message must match stdlib so user code that asserts on the message keeps working under pipit's intercepted dispatch")
}

func TestPipitErrorsIsDirectEqual(t *testing.T) {
	t.Parallel()

	target := &pipitErrorsAsTestErr{code: 9}
	matched := pipitErrorsIs(nil, reflect.ValueOf(target), reflect.ValueOf(target))
	require.True(t, matched,
		"errors.Is for the same pointer at both positions must return true via comparable-equality")
}

func TestPipitErrorsIsThroughChain(t *testing.T) {
	t.Parallel()

	target := &pipitErrorsAsTestErr{code: 11}
	wrapped := &pipitErrorsAsWrappedErr{inner: target}
	matched := pipitErrorsIs(nil, reflect.ValueOf(wrapped), reflect.ValueOf(target))
	require.True(t, matched,
		"errors.Is must walk Unwrap chains so sentinels detected after wrapping still match")
}

func TestPipitErrorsIsReturnsFalseOnDifferent(t *testing.T) {
	t.Parallel()

	a := &pipitErrorsAsTestErr{code: 1}
	b := &pipitErrorsAsTestErr{code: 2}
	matched := pipitErrorsIs(nil, reflect.ValueOf(a), reflect.ValueOf(b))
	require.False(t, matched,
		"distinct sentinel pointers (even of the same type) must not match without an Is method registered between them")
}
