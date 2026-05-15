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
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

type multiUnwrapError struct{ inner []error }

func (multiUnwrapError) Error() string { return "multi" }

func (e multiUnwrapError) Unwrap() []error { return e.inner }

type singleUnwrapError struct{ inner error }

func (singleUnwrapError) Error() string { return "single" }

func (e singleUnwrapError) Unwrap() error { return e.inner }

type comparableError struct{ code int }

func (e comparableError) Error() string { return fmt.Sprintf("code %d", e.code) }

func TestLookupMultiUnwrapMethodAcceptsOnlyTheSliceForm(t *testing.T) {
	t.Parallel()

	var boxed error = multiUnwrapError{inner: []error{errors.New("a")}}
	multiInterface := reflect.ValueOf(&boxed).Elem()

	var emptyError error
	nilInterface := reflect.ValueOf(&emptyError).Elem()

	tests := []struct {
		value reflect.Value
		name  string
		want  bool
	}{
		{name: "a value whose Unwrap returns a slice", value: reflect.ValueOf(multiUnwrapError{}), want: true},
		{name: "an interface holding one is peeled first", value: multiInterface, want: true},
		{name: "a pointer to one is followed", value: reflect.ValueOf(&multiUnwrapError{}), want: true},
		{name: "a value whose Unwrap returns a single error is refused", value: reflect.ValueOf(singleUnwrapError{}), want: false},
		{name: "a value with no Unwrap is refused", value: reflect.ValueOf(comparableError{}), want: false},
		{name: "a non-error value is refused", value: reflect.ValueOf(42), want: false},
		{name: "a nil interface is refused", value: nilInterface, want: false},
		{name: "an invalid value is refused", value: reflect.Value{}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, ok := lookupMultiUnwrapMethod(tt.value)

			require.Equal(t, tt.want, ok,
				"only an Unwrap returning []error joins an error tree")
		})
	}
}

func TestPeelInterfaceUnwrapsOrRaises(t *testing.T) {
	t.Parallel()

	var boxed any = 42
	interfaceValue := reflect.ValueOf(&boxed).Elem()

	t.Run("a non-nil interface is peeled to its dynamic value", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, reflect.Int, peelInterface(interfaceValue, "nil").Kind())
	})

	t.Run("a concrete value passes through", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, reflect.String, peelInterface(reflect.ValueOf("x"), "nil").Kind())
	})

	t.Run("an invalid value passes through", func(t *testing.T) {
		t.Parallel()
		require.False(t, peelInterface(reflect.Value{}, "nil").IsValid())
	})

	t.Run("a nil interface raises the message it was given", func(t *testing.T) {
		t.Parallel()
		var empty any
		nilInterface := reflect.ValueOf(&empty).Elem()

		require.PanicsWithValue(t, "nil target", func() {
			peelInterface(nilInterface, "nil target")
		}, "a nil operand must raise the caller's own diagnostic")
	})
}

func TestUnwrapErrorInterfaceValuePeelsOneLayer(t *testing.T) {
	t.Parallel()

	var boxed error = comparableError{code: 1}
	interfaceValue := reflect.ValueOf(&boxed).Elem()

	var empty error
	nilInterface := reflect.ValueOf(&empty).Elem()

	tests := []struct {
		value reflect.Value
		check func(t *testing.T, got reflect.Value)
		name  string
	}{
		{
			name:  "an interface is peeled to its concrete error",
			value: interfaceValue,
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, reflect.Struct, got.Kind()) },
		},
		{
			name:  "a nil interface becomes the invalid value",
			value: nilInterface,
			check: func(t *testing.T, got reflect.Value) { require.False(t, got.IsValid()) },
		},
		{
			name:  "a concrete value passes through",
			value: reflect.ValueOf(comparableError{code: 2}),
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, reflect.Struct, got.Kind()) },
		},
		{
			name:  "an invalid value stays invalid",
			value: reflect.Value{},
			check: func(t *testing.T, got reflect.Value) { require.False(t, got.IsValid()) },
		},
		{
			name:  "an ordinary pointer passes through",
			value: reflect.ValueOf(&comparableError{}),
			check: func(t *testing.T, got reflect.Value) { require.Equal(t, reflect.Pointer, got.Kind()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t, unwrapErrorInterfaceValue(tt.value))
		})
	}
}

func TestErrorsIsDirectEqualNeedsTheSameComparableType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		concrete reflect.Value
		target   reflect.Value
		name     string
		want     bool
	}{
		{
			name:     "identical comparable errors match",
			concrete: reflect.ValueOf(comparableError{code: 1}), target: reflect.ValueOf(comparableError{code: 1}), want: true,
		},
		{
			name:     "different values of the same type do not match",
			concrete: reflect.ValueOf(comparableError{code: 1}), target: reflect.ValueOf(comparableError{code: 2}), want: false,
		},
		{
			name:     "different types never match",
			concrete: reflect.ValueOf(comparableError{}), target: reflect.ValueOf(singleUnwrapError{}), want: false,
		},
		{
			name:     "an uncomparable type is refused rather than compared",
			concrete: reflect.ValueOf(multiUnwrapError{inner: []error{}}), target: reflect.ValueOf(multiUnwrapError{inner: []error{}}), want: false,
		},
		{
			name:     "an invalid operand never matches",
			concrete: reflect.Value{}, target: reflect.ValueOf(comparableError{}), want: false,
		},
		{
			name:     "two invalid operands never match",
			concrete: reflect.Value{}, target: reflect.Value{}, want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, errorsIsDirectEqual(tt.concrete, tt.target))
		})
	}
}

func TestInt64ResultReadsOnlyAnIntegerFirstResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		results []reflect.Value
		name    string
		want    int64
	}{
		{name: "an integer first result", results: []reflect.Value{reflect.ValueOf(42)}, want: 42},
		{name: "a negative first result", results: []reflect.Value{reflect.ValueOf(-1)}, want: -1},
		{name: "later results are ignored", results: []reflect.Value{reflect.ValueOf(7), reflect.ValueOf("x")}, want: 7},
		{name: "no results yield zero", results: nil, want: 0},
		{name: "an invalid first result yields zero", results: []reflect.Value{{}}, want: 0},
		{name: "a non-integer first result yields zero", results: []reflect.Value{reflect.ValueOf("x")}, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, int64Result(tt.results))
		})
	}
}
