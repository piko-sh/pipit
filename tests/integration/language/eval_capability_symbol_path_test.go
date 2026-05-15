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

//go:build integration

package language_test

import (
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func closureFactory(captured int) func() int {
	return func() int { return captured }
}

func otherClosureFactory() func() int {
	return func() int { return -1 }
}

func TestNativeSymbolPathIdentity(t *testing.T) {
	t.Parallel()

	symbolPath := func(function any) (string, uintptr) {
		value := reflect.ValueOf(function)
		pointer := value.Pointer()
		resolved := runtime.FuncForPC(pointer)
		require.NotNil(t, resolved, "every function value must resolve to a symbol")
		return resolved.Name(), pointer
	}

	t.Run("top level functions name themselves uniquely", func(t *testing.T) {
		t.Parallel()
		name, _ := symbolPath(closureFactory)
		require.Equal(t, "pipit.sh/pipit/tests/integration/language_test.closureFactory", name)
	})

	t.Run("same site closures are one identity", func(t *testing.T) {
		t.Parallel()
		firstName, firstPointer := symbolPath(closureFactory(1))
		secondName, secondPointer := symbolPath(closureFactory(2))

		require.Equal(t, firstName, secondName,
			"a closure's identity is its code, which both instances share")
		require.Equal(t, firstPointer, secondPointer,
			"Go 1.27 shares the code pointer between same-site closures; a capability "+
				"allowlist cannot distinguish them")
		factoryName, _ := symbolPath(closureFactory)
		require.True(t, strings.HasPrefix(firstName, factoryName+"."),
			"a closure symbol is its enclosing function's symbol plus a suffix, got %q for %q",
			firstName, factoryName)
		require.NotEqual(t, factoryName, firstName)
	})

	t.Run("different site closures stay distinct", func(t *testing.T) {
		t.Parallel()
		firstName, firstPointer := symbolPath(closureFactory(1))
		secondName, secondPointer := symbolPath(otherClosureFactory())

		require.NotEqual(t, firstName, secondName)
		require.NotEqual(t, firstPointer, secondPointer)
	})
}
