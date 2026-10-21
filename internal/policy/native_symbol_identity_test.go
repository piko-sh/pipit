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

package policy_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/policy"
)

func TestNativeSymbolIdentityNamesTheRegisteredSymbol(t *testing.T) {
	t.Parallel()
	wrapper := func(name string) error { return nil }
	policy.RecordNativeSymbols("example.com/files", map[string]reflect.Value{
		"Write":    reflect.ValueOf(wrapper),
		"Constant": reflect.ValueOf(42),
		"Missing":  {},
		"NilFunc":  reflect.ValueOf((func())(nil)),
	})

	identity, ok := policy.NativeSymbolIdentity(reflect.ValueOf(wrapper))
	require.True(t, ok)
	require.Equal(t, "example.com/files.Write", identity)

	policy.RecordNativeSymbols("example.com/files", map[string]reflect.Value{"Write": reflect.ValueOf(wrapper)})
	identity, ok = policy.NativeSymbolIdentity(reflect.ValueOf(wrapper))
	require.True(t, ok, "registering the same identity again keeps it")
	require.Equal(t, "example.com/files.Write", identity)
}

func TestNativeSymbolIdentityRefusesToGuessBetweenTwoNames(t *testing.T) {
	t.Parallel()
	shared := func(value string) string { return value }
	policy.RecordNativeSymbols("example.com/first", map[string]reflect.Value{"Shared": reflect.ValueOf(shared)})
	policy.RecordNativeSymbols("example.com/second", map[string]reflect.Value{"Shared": reflect.ValueOf(shared)})

	_, ok := policy.NativeSymbolIdentity(reflect.ValueOf(shared))
	require.False(t, ok, "one code pointer under two names is ambiguous")
}

func TestNativeSymbolIdentityIgnoresUnrecordedAndInvalidValues(t *testing.T) {
	t.Parallel()
	_, ok := policy.NativeSymbolIdentity(reflect.ValueOf(strings.Fields))
	require.False(t, ok)
	_, ok = policy.NativeSymbolIdentity(reflect.Value{})
	require.False(t, ok)
	_, ok = policy.NativeSymbolIdentity(reflect.ValueOf("not a function"))
	require.False(t, ok)
	_, ok = policy.NativeSymbolIdentity(reflect.ValueOf((func())(nil)))
	require.False(t, ok)
}
