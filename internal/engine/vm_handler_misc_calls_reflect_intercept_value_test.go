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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestInterceptPipitReflectNumFieldHidesTheSentinel(t *testing.T) {
	t.Parallel()

	script := syntheticScriptType(t, "Point")

	tests := []struct {
		name        string
		inner       reflect.Value
		wantHandled bool
	}{
		{name: "a synthesised struct value", inner: reflect.New(script).Elem(), wantHandled: true},
		{name: "a pointer to one", inner: reflect.New(script), wantHandled: true},
		{name: "an ordinary struct is left to reflect", inner: reflect.ValueOf(identityNone{})},
		{name: "a non-struct is left to reflect", inner: reflect.ValueOf(3)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, registers := newStandardVM(t)
			site := reflectInterceptSite(isa.RegisterInt, program.VarLocation{Kind: isa.RegisterInt, Register: 0})

			got, handled := interceptPipitReflectNumField(registers, site, tt.inner)
			require.Equal(t, opContinue, got)
			require.Equal(t, tt.wantHandled, handled)
			if tt.wantHandled {
				require.Equal(t, int64(1), registers.Ints[0], "the identity field is pipit's, not the program's")
			}
		})
	}
}

func TestInterceptPipitReflectFieldBoundsChecksTheUserFields(t *testing.T) {
	t.Parallel()

	script := syntheticScriptType(t, "Point")

	t.Run("an in-range index answers with the field value", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := reflectInterceptSite(isa.RegisterInt, program.VarLocation{Kind: isa.RegisterGeneral, Register: 0})
		receiver := reflect.New(script).Elem()
		receiver.Field(0).SetInt(4)
		registers.Ints[2] = 0

		got, handled := interceptPipitReflectField(vm, registers, site, receiver)
		require.Equal(t, opContinue, got)
		require.True(t, handled)

		field, ok := reflect.TypeAssert[reflect.Value](registers.General[0])
		require.True(t, ok)
		require.Equal(t, int64(4), field.Int())
	})

	t.Run("the sentinel index raises the reflect panic", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := reflectInterceptSite(isa.RegisterInt, program.VarLocation{Kind: isa.RegisterGeneral, Register: 0})
		registers.Ints[2] = 1

		_, handled := interceptPipitReflectField(vm, registers, site, reflect.New(script).Elem())
		require.True(t, handled)
		require.Error(t, vm.evalError)
	})

	t.Run("a nil pointer receiver raises the reflect panic", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := reflectInterceptSite(isa.RegisterInt, program.VarLocation{Kind: isa.RegisterGeneral, Register: 0})
		registers.Ints[2] = 0

		_, handled := interceptPipitReflectField(vm, registers, site, reflect.Zero(reflect.PointerTo(script)))
		require.True(t, handled)
		require.Error(t, vm.evalError)
	})

	t.Run("a site with no index operand is left to reflect", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := &program.CallSite{Arguments: []program.VarLocation{{Kind: isa.RegisterGeneral}}}

		_, handled := interceptPipitReflectField(vm, registers, site, reflect.New(script).Elem())
		require.False(t, handled)
	})

	t.Run("an ordinary struct is left to reflect", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := reflectInterceptSite(isa.RegisterInt, program.VarLocation{Kind: isa.RegisterGeneral, Register: 0})

		_, handled := interceptPipitReflectField(vm, registers, site, reflect.ValueOf(identityNone{}))
		require.False(t, handled)
	})
}

func TestInterceptPipitReflectFieldByNameUsesSourceNames(t *testing.T) {
	t.Parallel()

	embedding := embeddingScriptType(t)

	t.Run("a field is found under its source name", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := reflectInterceptSite(isa.RegisterString, program.VarLocation{Kind: isa.RegisterGeneral, Register: 0})
		receiver := reflect.New(embedding).Elem()
		receiver.Field(1).SetString("named")
		registers.Strings[2] = "Name"

		got, handled := interceptPipitReflectFieldByName(vm, registers, site, receiver)
		require.Equal(t, opContinue, got)
		require.True(t, handled)

		field, ok := reflect.TypeAssert[reflect.Value](registers.General[0])
		require.True(t, ok)
		require.Equal(t, "named", field.String())
	})

	t.Run("a nil pointer receiver raises the reflect panic", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := reflectInterceptSite(isa.RegisterString, program.VarLocation{Kind: isa.RegisterGeneral, Register: 0})
		registers.Strings[2] = "Name"

		_, handled := interceptPipitReflectFieldByName(vm, registers, site, reflect.Zero(reflect.PointerTo(embedding)))
		require.True(t, handled)
		require.Error(t, vm.evalError)
	})

	t.Run("a non-struct receiver is left to reflect", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := reflectInterceptSite(isa.RegisterString, program.VarLocation{Kind: isa.RegisterGeneral, Register: 0})
		registers.Strings[2] = "Name"

		_, handled := interceptPipitReflectFieldByName(vm, registers, site, reflect.ValueOf(3))
		require.False(t, handled)
	})

	t.Run("a site with no name operand is left to reflect", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := &program.CallSite{Arguments: []program.VarLocation{{Kind: isa.RegisterGeneral}}}

		_, handled := interceptPipitReflectFieldByName(vm, registers, site, reflect.New(embedding).Elem())
		require.False(t, handled)
	})
}

func TestInstallPipitReflectFieldByNameOverrideBindsALookup(t *testing.T) {
	t.Parallel()

	embedding := embeddingScriptType(t)

	t.Run("a struct receiver installs the lookup", func(t *testing.T) {
		t.Parallel()

		_, _, registers := newStandardVM(t)
		receiver := reflect.New(embedding).Elem()
		receiver.Field(1).SetString("named")

		require.True(t, installPipitReflectFieldByNameOverride(registers, 0, receiver))

		lookup, ok := reflect.TypeAssert[func(string) reflect.Value](registers.General[0])
		require.True(t, ok)
		require.Equal(t, "named", lookup("Name").String())
		require.False(t, lookup("absent").IsValid())
	})

	t.Run("a non-struct receiver installs nothing", func(t *testing.T) {
		t.Parallel()

		_, _, registers := newStandardVM(t)

		require.False(t, installPipitReflectFieldByNameOverride(registers, 0, reflect.ValueOf(3)))
	})
}

func TestPipitReflectValueTypeNameResolvesTheSourceName(t *testing.T) {
	t.Parallel()

	script := syntheticScriptType(t, "Point")

	tests := []struct {
		name  string
		inner reflect.Value
		want  string
	}{
		{name: "a synthesised struct", inner: reflect.New(script).Elem(), want: "Point"},
		{name: "a pointer to one", inner: reflect.New(script), want: "Point"},
		{name: "an ordinary type has no script name", inner: reflect.ValueOf(3), want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := newTestVM(t)
			require.Equal(t, tt.want, pipitReflectValueTypeName(vm, tt.inner))
		})
	}
}

func TestTryInterceptPipitReflectValueMethodRoutesByName(t *testing.T) {
	t.Parallel()

	script := syntheticScriptType(t, "Point")
	boxed := reflect.ValueOf(reflect.New(script).Elem())

	tests := []struct {
		name        string
		receiver    reflect.Value
		method      string
		wantHandled bool
	}{
		{name: "an invalid receiver", receiver: reflect.Value{}, method: "NumField"},
		{name: "a value that is not a reflect.Value", receiver: reflect.ValueOf(3), method: "NumField"},
		{name: "a wrapped ordinary value has no script name", receiver: reflect.ValueOf(reflect.ValueOf(3)), method: "NumField"},
		{name: "the field count", receiver: boxed, method: "NumField", wantHandled: true},
		{name: "the method count", receiver: boxed, method: "NumMethod", wantHandled: true},
		{name: "a method the interceptor does not answer", receiver: boxed, method: "Kind"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, _, registers := newStandardVM(t)
			site := reflectInterceptSite(isa.RegisterInt, program.VarLocation{Kind: isa.RegisterInt, Register: 0})

			_, handled := tryInterceptPipitReflectValueMethod(vm, registers, site, tt.receiver, tt.method)
			require.Equal(t, tt.wantHandled, handled)
		})
	}
}
