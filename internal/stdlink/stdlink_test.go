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

package stdlink_test

import (
	"errors"
	"fmt"
	"io/fs"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/stdlink"
)

type code int

type tagged struct{ name string }

func TestReflectTypeForReturnsItsTypeArgument(t *testing.T) {
	t.Parallel()

	require.Equal(t, reflect.TypeFor[[]string](), stdlink.ReflectTypeFor(reflect.TypeFor[[]string]()))
	require.Equal(t, reflect.TypeFor[error](), stdlink.ReflectTypeFor(reflect.TypeFor[error]()))
}

func TestReflectTypeAssertIsAnAssertionOfTheValuesInterface(t *testing.T) {
	t.Parallel()

	n, ok := stdlink.ReflectTypeAssert(reflect.TypeFor[int](), reflect.ValueOf(7))
	require.True(t, ok)
	require.Equal(t, 7, n.Interface())

	s, ok := stdlink.ReflectTypeAssert(reflect.TypeFor[string](), reflect.ValueOf(7))
	require.False(t, ok)
	require.Equal(t, "", s.Interface(), "a failed assertion gives the type's zero value")

	stringer, ok := stdlink.ReflectTypeAssert(reflect.TypeFor[fmt.Stringer](), reflect.ValueOf(time.Second))
	require.True(t, ok)
	require.Equal(t, "1s", stringer.Interface().(fmt.Stringer).String())

	_, ok = stdlink.ReflectTypeAssert(reflect.TypeFor[int](), reflect.ValueOf(code(3)))
	require.False(t, ok, "a named type is not its underlying type")

	require.Panics(t, func() { stdlink.ReflectTypeAssert(reflect.TypeFor[int](), reflect.Value{}) },
		"the zero Value panics, as Value.Interface does")
}

func TestErrorsAsTypeFindsTheFirstErrorOfItsTypeInTheChain(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("outer: %w", &fs.PathError{Op: "open", Path: "/x", Err: fs.ErrNotExist})
	found, ok := stdlink.ErrorsAsType(reflect.TypeFor[*fs.PathError](), wrapped)
	require.True(t, ok)
	require.Equal(t, "/x", found.Interface().(*fs.PathError).Path)

	_, ok = stdlink.ErrorsAsType(reflect.TypeFor[*fs.PathError](), errors.New("plain"))
	require.False(t, ok)

	missing, ok := stdlink.ErrorsAsType(reflect.TypeFor[*fs.PathError](), nil)
	require.False(t, ok)
	require.True(t, missing.IsNil())
}

func TestRandNDrawsBelowItsBoundAtTheTypeArgument(t *testing.T) {
	t.Parallel()

	for range 100 {
		value := stdlink.RandN(reflect.TypeFor[code](), code(5))
		require.IsType(t, code(0), value, "a named integer type comes back as itself")
		require.Less(t, value.(code), code(5))
		require.GreaterOrEqual(t, value.(code), code(0))
		duration := stdlink.RandN(reflect.TypeFor[time.Duration](), time.Second)
		require.Less(t, duration.(time.Duration), time.Second)
		small := stdlink.RandN(reflect.TypeFor[uint8](), uint8(3))
		require.Less(t, small.(uint8), uint8(3))
	}
	require.PanicsWithValue(t, "invalid argument to N", func() { stdlink.RandN(reflect.TypeFor[int](), 0) })
	require.Panics(t, func() { stdlink.RandN(reflect.TypeFor[string](), "x") })
}

func TestRuntimeAddCleanupRunsTheCleanupOnceTheObjectIsUnreachable(t *testing.T) {
	t.Parallel()

	done := make(chan any, 1)
	func() {
		object := &tagged{name: "file"}
		stdlink.RuntimeAddCleanup(reflect.TypeFor[tagged](), reflect.TypeFor[string](), object, func(argument any) { done <- argument }, object.name)
	}()
	var received any
	require.Eventually(t, func() bool {
		runtime.GC()
		select {
		case received = <-done:
			return true
		default:
			return false
		}
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, "file", received)
}

func TestRuntimeAddCleanupRefusesWhatAddCleanupRefuses(t *testing.T) {
	t.Parallel()

	object := &tagged{}
	require.Panics(t, func() {
		stdlink.RuntimeAddCleanup(reflect.TypeFor[tagged](), reflect.TypeFor[*tagged](), object, func(any) {}, object)
	}, "an arg equal to ptr would keep the object reachable forever")
	require.Panics(t, func() {
		stdlink.RuntimeAddCleanup(reflect.TypeFor[tagged](), reflect.TypeFor[int](), (*tagged)(nil), func(any) {}, 0)
	})

	stopped := stdlink.RuntimeAddCleanup(reflect.TypeFor[tagged](), reflect.TypeFor[int](), object, func(any) {}, 0)
	stopped.Stop()
	runtime.KeepAlive(object)
}
