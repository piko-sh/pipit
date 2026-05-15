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

func printfParameterTypes() []reflect.Type {
	return []reflect.Type{reflect.TypeFor[string](), reflect.TypeFor[[]any]()}
}

func TestRewriteFmtTypeVerbsSubstitutesTheTypeName(t *testing.T) {
	t.Parallel()

	t.Run("a type verb is rewritten with the operand's type", func(t *testing.T) {
		t.Parallel()

		arguments := []reflect.Value{reflect.ValueOf("%T"), reflect.ValueOf(3)}

		rewriteFmtTypeVerbs(&program.CallSite{}, arguments, printfParameterTypes())

		require.Equal(t, "%s", arguments[0].Interface())
		require.Equal(t, "int", arguments[1].Interface())
	})

	t.Run("a format with no type verb is left alone", func(t *testing.T) {
		t.Parallel()

		arguments := []reflect.Value{reflect.ValueOf("%d"), reflect.ValueOf(3)}

		rewriteFmtTypeVerbs(&program.CallSite{}, arguments, printfParameterTypes())

		require.Equal(t, "%d", arguments[0].Interface())
		require.Equal(t, 3, arguments[1].Interface())
	})

	t.Run("a recorded static type wins over the runtime type", func(t *testing.T) {
		t.Parallel()

		arguments := []reflect.Value{reflect.ValueOf("%T"), reflect.ValueOf(int64(3))}
		site := &program.CallSite{ArgumentStaticTypeStrings: []string{"", "main.Score"}}

		rewriteFmtTypeVerbs(site, arguments, printfParameterTypes())

		require.Equal(t, "main.Score", arguments[1].Interface())
	})

	t.Run("a call whose format parameter is not a string is left alone", func(t *testing.T) {
		t.Parallel()

		arguments := []reflect.Value{reflect.ValueOf(1), reflect.ValueOf(3)}

		require.NotPanics(t, func() {
			rewriteFmtTypeVerbs(&program.CallSite{}, arguments, []reflect.Type{reflect.TypeFor[int](), reflect.TypeFor[[]any]()})
		})
	})

	t.Run("a call with one parameter has no format slot", func(t *testing.T) {
		t.Parallel()

		arguments := []reflect.Value{reflect.ValueOf("%T")}

		require.NotPanics(t, func() {
			rewriteFmtTypeVerbs(&program.CallSite{}, arguments, []reflect.Type{reflect.TypeFor[[]any]()})
		})
	})
}

func TestRewriteFmtTypeVerbsSpreadRebuildsTheSlice(t *testing.T) {
	t.Parallel()

	t.Run("a spread operand's type verb is rewritten", func(t *testing.T) {
		t.Parallel()

		arguments := []reflect.Value{reflect.ValueOf("%T"), reflect.ValueOf([]any{3})}
		site := &program.CallSite{IsEllipsisSpread: true}

		rewriteFmtTypeVerbs(site, arguments, printfParameterTypes())

		require.Equal(t, "%s", arguments[0].Interface())
		require.Equal(t, []any{"int"}, arguments[1].Interface())
	})

	t.Run("a spread slice index past the arguments is left alone", func(t *testing.T) {
		t.Parallel()

		arguments := []reflect.Value{reflect.ValueOf("%T")}
		site := &program.CallSite{IsEllipsisSpread: true}

		require.NotPanics(t, func() { rewriteFmtTypeVerbsSpread(site, arguments, 0, 1, "%T") })
	})

	t.Run("a spread operand that is not a slice is left alone", func(t *testing.T) {
		t.Parallel()

		arguments := []reflect.Value{reflect.ValueOf("%T"), reflect.ValueOf(3)}
		site := &program.CallSite{IsEllipsisSpread: true}

		rewriteFmtTypeVerbsSpread(site, arguments, 0, 1, "%T")
		require.Equal(t, "%T", arguments[0].Interface())
	})

	t.Run("a spread format with no type verb is left alone", func(t *testing.T) {
		t.Parallel()

		arguments := []reflect.Value{reflect.ValueOf("%d"), reflect.ValueOf([]any{3})}
		site := &program.CallSite{IsEllipsisSpread: true}

		rewriteFmtTypeVerbsSpread(site, arguments, 0, 1, "%d")
		require.Equal(t, "%d", arguments[0].Interface())
	})
}

func TestInterfaceOrNilReadsOnlyWhatItMay(t *testing.T) {
	t.Parallel()

	require.Equal(t, 3, interfaceOrNil(reflect.ValueOf(3)))
	require.Nil(t, interfaceOrNil(reflect.Value{}))
	require.Nil(t, interfaceOrNil(reflect.ValueOf(identityTrailing{}).Field(1)),
		"an unexported field cannot be read as an interface")
}

func TestWrapFmtArgumentLeavesOrdinaryOperandsAlone(t *testing.T) {
	t.Parallel()

	nilBoxed := reflect.ValueOf(map[string]any{"k": nil}).MapIndex(reflect.ValueOf("k"))

	tests := []struct {
		name     string
		argument reflect.Value
	}{
		{name: "an invalid operand", argument: reflect.Value{}},
		{name: "a nil interface operand", argument: nilBoxed},
		{name: "an ordinary operand", argument: reflect.ValueOf(3)},
		{name: "an unreadable operand", argument: reflect.ValueOf(identityTrailing{}).Field(1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.argument, wrapFmtArgument(newTestVM(t), tt.argument, ""))
		})
	}
}

func TestWrapFmtArgumentForVerbWrapsASynthesisedOperand(t *testing.T) {
	t.Parallel()

	vm := adapterHostVM(t, "at.String", stringReturningMethod(t, "String", "rendered"))

	wrapped := wrapFmtArgumentForVerb(vm, reflect.ValueOf(identityTrailing{}), 'v', "at")
	require.NotEqual(t, reflect.TypeFor[identityTrailing](), wrapped.Type(),
		"a synthesised operand reaches fmt through a wrapper that hides the identity field")
}

func TestCoerceReflectArgumentAppliesTheRightRule(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		argument reflect.Value
		expected reflect.Type
		want     any
	}{
		{name: "a matching type passes through", argument: reflect.ValueOf(3), expected: reflect.TypeFor[int](), want: 3},
		{
			name: "an invalid argument becomes the zero value", argument: reflect.Value{},
			expected: reflect.TypeFor[string](), want: "",
		},
		{
			name: "a narrow slice is widened", argument: reflect.ValueOf([]int64{1, 2}),
			expected: reflect.TypeFor[[]int](), want: []int{1, 2},
		},
		{
			name: "an int flag becomes a bool", argument: reflect.ValueOf(int64(1)),
			expected: reflect.TypeFor[bool](), want: true,
		},
		{
			name: "a convertible scalar is converted", argument: reflect.ValueOf(int64(5)),
			expected: reflect.TypeFor[int32](), want: int32(5),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := newTestVM(t)
			got := coerceReflectArgument(vm, tt.argument, tt.expected, argumentTypeContext{})
			require.Equal(t, tt.want, got.Interface())
		})
	}

	t.Run("a closure bound for an interface may be kept whole", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		closure := reflect.ValueOf(&RuntimeClosure{})

		kept := coerceReflectArgument(vm, closure, reflect.TypeFor[any](), argumentTypeContext{keepClosures: true})
		require.Equal(t, closure.Interface(), kept.Interface())
	})
}

func TestCopyCallArgsWithVariadicPackingFillsTheTrailingSlice(t *testing.T) {
	t.Parallel()

	callee := program.NewNamedFunction("f")
	callee.ParameterKinds = []isa.RegisterKind{isa.RegisterInt, isa.RegisterGeneral}
	callee.NumRegisters = wideRegCounts(8)

	site := &program.CallSite{
		Arguments: []program.VarLocation{
			{Kind: isa.RegisterInt, Register: 1},
			{Kind: isa.RegisterInt, Register: 2},
			{Kind: isa.RegisterInt, Register: 3},
		},
		RuntimeVariadicSliceType: reflect.TypeFor[[]int](),
		RuntimeVariadicNumFixed:  1,
	}

	caller := standardRegisters()
	caller.Ints[1] = 1
	caller.Ints[2] = 2
	caller.Ints[3] = 3
	frame := CallFrame{Registers: NewRegisters(callee.NumRegisters)}

	copyCallArgsWithVariadicPacking(&caller, &frame, site, callee, nil)

	require.Equal(t, int64(1), frame.Registers.Ints[0], "the fixed parameter keeps its own bank")
	require.Equal(t, []int{2, 3}, frame.Registers.General[0].Interface(),
		"the trailing operands are packed into the declared slice type")
}

func TestCopyCallArgsWithVariadicPackingHonoursAScatteredLayout(t *testing.T) {
	t.Parallel()

	callee := program.NewNamedFunction("f")
	callee.ParameterKinds = []isa.RegisterKind{isa.RegisterInt, isa.RegisterGeneral}
	callee.ParameterRegisters = []uint8{4, 5}
	callee.NumRegisters = wideRegCounts(8)

	site := &program.CallSite{
		Arguments: []program.VarLocation{
			{Kind: isa.RegisterInt, Register: 1},
			{Kind: isa.RegisterInt, Register: 2},
		},
		RuntimeVariadicSliceType: reflect.TypeFor[[]int](),
		RuntimeVariadicNumFixed:  1,
	}

	caller := standardRegisters()
	caller.Ints[1] = 7
	caller.Ints[2] = 8
	frame := CallFrame{Registers: NewRegisters(callee.NumRegisters)}

	copyCallArgsWithVariadicPacking(&caller, &frame, site, callee, nil)

	require.Equal(t, int64(7), frame.Registers.Ints[4])
	require.Equal(t, []int{8}, frame.Registers.General[5].Interface())
}

func TestOwnsInterpreterStorageRefusesPlainHeapPointers(t *testing.T) {
	t.Parallel()

	vm, _, _ := newStandardVM(t)
	value := 3

	require.False(t, vm.ownsInterpreterStorage(nil))
	require.False(t, vm.ownsInterpreterStorage(reflect.ValueOf(&value).UnsafePointer()),
		"a plain Go allocation is the host's, not the interpreter's")
}
