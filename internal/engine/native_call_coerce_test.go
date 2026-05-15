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

func TestFmtVerbsForCallOnlyFireForAFormatParameter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		parameterTypes []reflect.Type
		arguments      []program.VarLocation
		format         string
		want           []byte
	}{
		{
			name:           "a printf-style call pairs each operand with its verb",
			parameterTypes: []reflect.Type{reflect.TypeFor[string](), reflect.TypeFor[[]any]()},
			arguments: []program.VarLocation{
				{Kind: isa.RegisterString, Register: 1},
				{Kind: isa.RegisterGeneral},
				{Kind: isa.RegisterGeneral},
			},
			format: "%d %s", want: []byte{'d', 's'},
		},
		{
			name:           "a call with no variadic operands has no verbs",
			parameterTypes: []reflect.Type{reflect.TypeFor[string](), reflect.TypeFor[[]any]()},
			arguments:      []program.VarLocation{{Kind: isa.RegisterString, Register: 1}},
			format:         "%d",
		},
		{
			name:           "a call whose format parameter is not a string has no verbs",
			parameterTypes: []reflect.Type{reflect.TypeFor[int](), reflect.TypeFor[[]any]()},
			arguments:      []program.VarLocation{{Kind: isa.RegisterInt}, {Kind: isa.RegisterGeneral}},
			format:         "%d",
		},
		{
			name:           "a call whose format operand is not in the string bank has no verbs",
			parameterTypes: []reflect.Type{reflect.TypeFor[string](), reflect.TypeFor[[]any]()},
			arguments:      []program.VarLocation{{Kind: isa.RegisterGeneral}, {Kind: isa.RegisterGeneral}},
			format:         "%d",
		},
		{
			name:           "a call with one parameter has no format slot",
			parameterTypes: []reflect.Type{reflect.TypeFor[[]any]()},
			arguments:      []program.VarLocation{{Kind: isa.RegisterGeneral}},
			format:         "%d",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := standardRegisters()
			registers.Strings[1] = tt.format

			require.Equal(t, tt.want, fmtVerbsForCall(&registers, &program.CallSite{Arguments: tt.arguments}, tt.parameterTypes))
		})
	}
}

func TestFmtVerbAtDefaultsOutsideTheRecordedRange(t *testing.T) {
	t.Parallel()

	verbs := []byte{'d', 's'}

	require.Equal(t, byte('d'), fmtVerbAt(verbs, 0))
	require.Equal(t, byte('s'), fmtVerbAt(verbs, 1))
	require.Equal(t, byte('v'), fmtVerbAt(verbs, 2), "an operand past the format keeps the default verb")
	require.Equal(t, byte('v'), fmtVerbAt(verbs, -1))
	require.Equal(t, byte('v'), fmtVerbAt(nil, 0), "a call with no format string has no recorded verbs")
}

func TestIsNilInterfaceValueNamesTheEmptyInterface(t *testing.T) {
	t.Parallel()

	nilBoxed := reflect.ValueOf(map[string]any{"k": nil}).MapIndex(reflect.ValueOf("k"))
	liveBoxed := reflect.ValueOf(map[string]any{"k": 3}).MapIndex(reflect.ValueOf("k"))

	require.True(t, isNilInterfaceValue(nilBoxed))
	require.False(t, isNilInterfaceValue(liveBoxed))
	require.False(t, isNilInterfaceValue(reflect.ValueOf(3)))
	require.False(t, isNilInterfaceValue(reflect.ValueOf((*int)(nil))), "a nil pointer is not a nil interface")
}

func TestCoerceVariadicSpreadSliceFitsTheDeclaredElementType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		source   reflect.Value
		expected reflect.Type
		want     any
	}{
		{
			name: "a matching slice is handed straight back", source: reflect.ValueOf([]int{1, 2}),
			expected: reflect.TypeFor[[]int](), want: []int{1, 2},
		},
		{
			name: "an interface slice is narrowed element by element", source: reflect.ValueOf([]any{1, 2}),
			expected: reflect.TypeFor[[]int](), want: []int{1, 2},
		},
		{
			name: "a convertible element type is converted", source: reflect.ValueOf([]int32{1, 2}),
			expected: reflect.TypeFor[[]int64](), want: []int64{1, 2},
		},
		{
			name: "a nil element leaves the zero value", source: reflect.ValueOf([]any{nil, 2}),
			expected: reflect.TypeFor[[]int](), want: []int{0, 2},
		},
		{
			name: "an invalid source becomes the zero slice", source: reflect.Value{},
			expected: reflect.TypeFor[[]int](), want: []int(nil),
		},
		{
			name: "a source that is not a slice is handed back unchanged", source: reflect.ValueOf(3),
			expected: reflect.TypeFor[[]int](), want: 3,
		},
		{
			name: "widening into an interface slice keeps each element", source: reflect.ValueOf([]int{1}),
			expected: reflect.TypeFor[[]any](), want: []any{1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := newTestVM(t)
			require.Equal(t, tt.want, coerceVariadicSpreadSlice(vm, tt.source, tt.expected).Interface())
		})
	}
}

func TestCoerceConcreteArgumentBridgesTheRegisterRepresentation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		argument reflect.Value
		expected reflect.Type
		want     any
	}{
		{name: "a non-zero int flag becomes true", argument: reflect.ValueOf(int64(1)), expected: reflect.TypeFor[bool](), want: true},
		{name: "a zero int flag becomes false", argument: reflect.ValueOf(int64(0)), expected: reflect.TypeFor[bool](), want: false},
		{name: "an int narrows to the declared width", argument: reflect.ValueOf(int64(5)), expected: reflect.TypeFor[int32](), want: int32(5)},
		{name: "a string converts to a byte slice", argument: reflect.ValueOf("ab"), expected: reflect.TypeFor[[]byte](), want: []byte("ab")},
		{
			name: "an unconvertible value is handed back unchanged", argument: reflect.ValueOf([]int{1}),
			expected: reflect.TypeFor[string](), want: []int{1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := newTestVM(t)
			require.Equal(t, tt.want, coerceConcreteArgument(vm, tt.argument, tt.expected).Interface())
		})
	}
}

func TestReinterpretPointerArgumentStaysInsideTheErasureBoundary(t *testing.T) {
	t.Parallel()

	value := 3

	tests := []struct {
		name     string
		argument reflect.Value
		expected reflect.Type
	}{
		{name: "a non-pointer parameter", argument: reflect.ValueOf(&value), expected: reflect.TypeFor[int]()},
		{name: "a non-pointer argument", argument: reflect.ValueOf(3), expected: reflect.TypeFor[*int]()},
		{name: "a matching pointer type", argument: reflect.ValueOf(&value), expected: reflect.TypeFor[*int]()},
		{
			name: "a mismatched pointer outside the boundary", argument: reflect.ValueOf(&value),
			expected: reflect.TypeFor[*string](),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := newTestVM(t)
			_, ok := reinterpretPointerArgument(vm, tt.argument, tt.expected)
			require.False(t, ok, "reinterpreting a pointer outside the erasure boundary would corrupt native code")
		})
	}
}

func TestCollectErasurePointeesIgnoresOrdinarySymbols(t *testing.T) {
	t.Parallel()

	pointees := map[reflect.Type]struct{}{}

	collectErasurePointees(reflect.Value{}, pointees)
	collectErasurePointees(reflect.ValueOf(3), pointees)

	require.Empty(t, pointees, "only a native-backed-generic sentinel delimits the boundary")
}

func TestRestoreNamedScalarForInterfaceNeedsAQualifiedStaticType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		static string
	}{
		{name: "no recorded static type"},
		{name: "an unqualified name", static: "Score"},
		{name: "a leading dot", static: ".Score"},
		{name: "a trailing dot", static: "main."},
		{name: "a slice type", static: "main.[]Score"},
		{name: "a pointer type", static: "main.*Score"},
		{name: "a type no registry holds", static: "absent.Score"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := newTestVM(t)
			ctx := argumentTypeContext{staticTypeString: tt.static}

			_, ok := restoreNamedScalarForInterface(vm, reflect.ValueOf(int64(3)), ctx)
			require.False(t, ok)
		})
	}

	t.Run("a VM with no symbol registry restores nothing", func(t *testing.T) {
		t.Parallel()

		_, ok := restoreNamedScalarForInterface(nil, reflect.ValueOf(int64(3)),
			argumentTypeContext{staticTypeString: "main.Score"})
		require.False(t, ok)
	})
}

func TestCoerceForInterfaceSlotOnlyFiresForInterfaceParameters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected reflect.Type
		typeCtx  argumentTypeContext
	}{
		{name: "a concrete parameter", expected: reflect.TypeFor[int]()},
		{
			name: "an interface parameter the site asked to skip", expected: reflect.TypeFor[any](),
			typeCtx: argumentTypeContext{skipInterfaceAdapter: true},
		},
		{name: "an interface parameter with nothing to adapt", expected: reflect.TypeFor[any]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := newTestVM(t)
			_, ok := coerceForInterfaceSlot(vm, reflect.ValueOf(3), tt.expected, tt.typeCtx)
			require.False(t, ok)
		})
	}
}

func TestArgumentTypeContextReadsWhatTheSiteRecorded(t *testing.T) {
	t.Parallel()

	site := &program.CallSite{
		ArgumentStaticTypeNames:   []string{"Score"},
		ArgumentStaticTypeStrings: []string{"main.Score"},
	}

	first := argumentTypeContextFromSite(site, 0)
	require.Equal(t, "Score", first.staticTypeName)
	require.Equal(t, "main.Score", first.staticTypeString)

	beyond := argumentTypeContextFromSite(site, 5)
	require.Empty(t, beyond.staticTypeName, "a site with no record for the position carries nothing")
	require.Empty(t, beyond.staticTypeString)
}

func TestCoerceClosureArgumentRoutesByParameterKind(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	argument := reflect.ValueOf(3)

	require.Equal(t, argument.Interface(), coerceClosureArgument(vm, argument, reflect.TypeFor[int]()).Interface(),
		"a parameter that is neither func nor interface takes the operand unchanged")
}
