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

func reflectInterceptSite(argumentKind isa.RegisterKind, returns ...program.VarLocation) *program.CallSite {
	return &program.CallSite{
		Arguments: []program.VarLocation{
			{Kind: isa.RegisterGeneral, Register: 1},
			{Kind: argumentKind, Register: 2},
		},
		Returns: returns,
	}
}

func TestInterceptReflectTypeNumFieldHidesTheSentinel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		reflectType reflect.Type
		wantHandled bool
		want        any
	}{
		{name: "a synthesised struct hides its identity field", reflectType: syntheticScriptType(t, "Point"), wantHandled: true, want: 1},
		{name: "an ordinary struct is left to reflect", reflectType: reflect.TypeFor[identityNone]()},
		{name: "a non-struct is left to reflect", reflectType: reflect.TypeFor[int]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, registers := newStandardVM(t)
			site := reflectInterceptSite(isa.RegisterInt, program.VarLocation{Kind: isa.RegisterInt, Register: 0})

			got, handled := interceptReflectTypeNumField(registers, site, tt.reflectType)
			require.Equal(t, opContinue, got)
			require.Equal(t, tt.wantHandled, handled)
			if tt.wantHandled {
				require.Equal(t, int64(1), registers.Ints[0])
			}
		})
	}
}

func TestInterceptReflectTypeFieldAnswersFromTheUserFields(t *testing.T) {
	t.Parallel()

	script := syntheticScriptType(t, "Point")

	t.Run("an in-range index answers with the field", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := reflectInterceptSite(isa.RegisterInt, program.VarLocation{Kind: isa.RegisterGeneral, Register: 0})
		registers.Ints[2] = 0

		got, handled := interceptReflectTypeField(vm, registers, site, script)
		require.Equal(t, opContinue, got)
		require.True(t, handled)

		field, ok := reflect.TypeAssert[reflect.StructField](registers.General[0])
		require.True(t, ok)
		require.Equal(t, "X", field.Name)
	})

	t.Run("an index past the user fields raises the reflect panic", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := reflectInterceptSite(isa.RegisterInt, program.VarLocation{Kind: isa.RegisterGeneral, Register: 0})
		registers.Ints[2] = 1

		_, handled := interceptReflectTypeField(vm, registers, site, script)
		require.True(t, handled, "the identity field is pipit's, so index one is past the end")
		require.Error(t, vm.evalError)
	})

	t.Run("a negative index raises the reflect panic", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := reflectInterceptSite(isa.RegisterInt, program.VarLocation{Kind: isa.RegisterGeneral, Register: 0})
		registers.Ints[2] = -1

		_, handled := interceptReflectTypeField(vm, registers, site, script)
		require.True(t, handled)
		require.Error(t, vm.evalError)
	})

	t.Run("an ordinary struct is left to reflect", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := reflectInterceptSite(isa.RegisterInt, program.VarLocation{Kind: isa.RegisterGeneral, Register: 0})

		_, handled := interceptReflectTypeField(vm, registers, site, reflect.TypeFor[identityNone]())
		require.False(t, handled)
	})

	t.Run("a non-struct is left to reflect", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := reflectInterceptSite(isa.RegisterInt, program.VarLocation{Kind: isa.RegisterGeneral, Register: 0})

		_, handled := interceptReflectTypeField(vm, registers, site, reflect.TypeFor[int]())
		require.False(t, handled)
	})

	t.Run("a site with no index operand is left to reflect", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := &program.CallSite{Arguments: []program.VarLocation{{Kind: isa.RegisterGeneral}}}

		_, handled := interceptReflectTypeField(vm, registers, site, script)
		require.False(t, handled)
	})
}

func TestInterceptReflectTypeFieldByNameUsesSourceNames(t *testing.T) {
	t.Parallel()

	script := syntheticScriptType(t, "Point")

	t.Run("a user field is found under its own name", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := reflectInterceptSite(isa.RegisterString,
			program.VarLocation{Kind: isa.RegisterGeneral, Register: 0},
			program.VarLocation{Kind: isa.RegisterBool, Register: 0},
		)
		registers.Strings[2] = "X"

		got, handled := interceptReflectTypeFieldByName(vm, registers, site, script)
		require.Equal(t, opContinue, got)
		require.True(t, handled)
		require.True(t, registers.Bools[0])

		field, ok := reflect.TypeAssert[reflect.StructField](registers.General[0])
		require.True(t, ok)
		require.Equal(t, "X", field.Name)
	})

	t.Run("a marker-prefixed name is reported as absent", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := reflectInterceptSite(isa.RegisterString,
			program.VarLocation{Kind: isa.RegisterGeneral, Register: 0},
			program.VarLocation{Kind: isa.RegisterBool, Register: 0},
		)
		registers.Strings[2] = isa.EmbeddedUnexportedPrefix + "base"

		_, handled := interceptReflectTypeFieldByName(vm, registers, site, script)
		require.True(t, handled, "no such field exists in the program being run")
		require.False(t, registers.Bools[0])
	})

	t.Run("an unknown plain name is left to reflect's own promotion", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := reflectInterceptSite(isa.RegisterString,
			program.VarLocation{Kind: isa.RegisterGeneral, Register: 0},
			program.VarLocation{Kind: isa.RegisterBool, Register: 0},
		)
		registers.Strings[2] = "Absent"

		_, handled := interceptReflectTypeFieldByName(vm, registers, site, script)
		require.False(t, handled)
	})

	t.Run("an ordinary struct is left to reflect", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)
		site := reflectInterceptSite(isa.RegisterString, program.VarLocation{Kind: isa.RegisterGeneral, Register: 0})

		_, handled := interceptReflectTypeFieldByName(vm, registers, site, reflect.TypeFor[identityNone]())
		require.False(t, handled)
	})
}

func TestInterceptReflectTypeNameSubstitutesTheSourceName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		reflectType reflect.Type
		wantHandled bool
		want        string
	}{
		{name: "a synthesised struct", reflectType: syntheticScriptType(t, "Point"), wantHandled: true, want: "Point"},
		{name: "a named Go type keeps reflect's answer", reflectType: reflect.TypeFor[identityNone]()},
		{name: "an unnamed type with no sentinel", reflectType: reflect.TypeFor[[]int]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, registers := newStandardVM(t)
			site := reflectInterceptSite(isa.RegisterInt, program.VarLocation{Kind: isa.RegisterString, Register: 0})

			got, handled := interceptReflectTypeName(registers, site, tt.reflectType)
			require.Equal(t, opContinue, got)
			require.Equal(t, tt.wantHandled, handled)
			if tt.wantHandled {
				require.Equal(t, tt.want, registers.Strings[0])
			}
		})
	}
}

func TestInterceptReflectTypeStringRendersTheQualifiedName(t *testing.T) {
	t.Parallel()

	script := syntheticScriptType(t, "Point")

	tests := []struct {
		name        string
		reflectType reflect.Type
		wantHandled bool
		want        string
	}{
		{name: "a synthesised struct", reflectType: script, wantHandled: true, want: "main.Point"},
		{name: "a slice of one", reflectType: reflect.SliceOf(script), wantHandled: true, want: "[]main.Point"},
		{name: "an ordinary type is left to reflect", reflectType: reflect.TypeFor[int]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, registers := newStandardVM(t)
			site := reflectInterceptSite(isa.RegisterInt, program.VarLocation{Kind: isa.RegisterString, Register: 0})

			got, handled := interceptReflectTypeString(registers, site, tt.reflectType)
			require.Equal(t, opContinue, got)
			require.Equal(t, tt.wantHandled, handled)
			if tt.wantHandled {
				require.Equal(t, tt.want, registers.Strings[0])
			}
		})
	}
}

func TestInterceptReflectTypePkgPathNamesTheQualifier(t *testing.T) {
	t.Parallel()

	script := syntheticScriptType(t, "Point")

	tests := []struct {
		name        string
		reflectType reflect.Type
		wantHandled bool
		want        string
	}{
		{name: "a synthesised struct reports its package", reflectType: script, wantHandled: true, want: "main"},
		{name: "a named Go type is left to reflect", reflectType: reflect.TypeFor[identityNone]()},
		{name: "a pointer has no package of its own", reflectType: reflect.PointerTo(script)},
		{name: "a slice has no package of its own", reflectType: reflect.SliceOf(script)},
		{name: "an unrelated unnamed type is left to reflect", reflectType: reflect.TypeFor[[]int]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, _, registers := newStandardVM(t)
			site := reflectInterceptSite(isa.RegisterInt, program.VarLocation{Kind: isa.RegisterString, Register: 0})

			got, handled := interceptReflectTypePkgPath(registers, site, tt.reflectType)
			require.Equal(t, opContinue, got)
			require.Equal(t, tt.wantHandled, handled)
			if tt.wantHandled {
				require.Equal(t, tt.want, registers.Strings[0])
			}
		})
	}
}

func TestTryInterceptPipitReflectTypeMethodRoutesByName(t *testing.T) {
	t.Parallel()

	script := syntheticScriptType(t, "Point")

	tests := []struct {
		name        string
		receiver    reflect.Value
		method      string
		wantHandled bool
	}{
		{name: "an invalid receiver is not a reflect.Type", receiver: reflect.Value{}, method: "NumField"},
		{name: "a value that is not a reflect.Type", receiver: reflect.ValueOf(3), method: "NumField"},
		{name: "a method the interceptor does not answer", receiver: reflect.ValueOf(script), method: "Kind"},
		{name: "the field count", receiver: reflect.ValueOf(script), method: "NumField", wantHandled: true},
		{name: "the type name", receiver: reflect.ValueOf(script), method: "Name", wantHandled: true},
		{name: "the rendered string", receiver: reflect.ValueOf(script), method: "String", wantHandled: true},
		{name: "the package path", receiver: reflect.ValueOf(script), method: "PkgPath", wantHandled: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, _, registers := newStandardVM(t)
			site := reflectInterceptSite(isa.RegisterInt,
				program.VarLocation{Kind: isa.RegisterString, Register: 0},
			)

			_, handled := tryInterceptPipitReflectTypeMethod(vm, registers, site, tt.receiver, tt.method)
			require.Equal(t, tt.wantHandled, handled)
		})
	}
}
