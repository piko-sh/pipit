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
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("writer refused") }

type genericPair struct {
	_pipitID_Pair struct{} `pipitTypeArgs:"int;string"`
	A             int
}

func TestReadOneBuiltinArgReadsTheNamedBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind isa.RegisterKind
		load func(*Registers)
		want any
	}{
		{name: "the int bank", kind: isa.RegisterInt, load: func(r *Registers) { r.Ints[1] = 3 }, want: int64(3)},
		{name: "the float bank", kind: isa.RegisterFloat, load: func(r *Registers) { r.Floats[1] = 1.5 }, want: 1.5},
		{name: "the string bank", kind: isa.RegisterString, load: func(r *Registers) { r.Strings[1] = "s" }, want: "s"},
		{name: "the bool bank", kind: isa.RegisterBool, load: func(r *Registers) { r.Bools[1] = true }, want: true},
		{name: "the uint bank", kind: isa.RegisterUint, load: func(r *Registers) { r.Uints[1] = 4 }, want: uint64(4)},
		{name: "the complex bank", kind: isa.RegisterComplex, load: func(r *Registers) { r.Complex[1] = 2i }, want: 2i},
		{
			name: "a populated general register", kind: isa.RegisterGeneral,
			load: func(r *Registers) { r.General[1] = reflect.ValueOf("boxed") }, want: "boxed",
		},
		{name: "an unset general register", kind: isa.RegisterGeneral, load: func(*Registers) {}, want: nil},
		{name: "a slice bank has no print form", kind: isa.RegisterSliceInt, load: func(*Registers) {}, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := standardRegisters()
			tt.load(&registers)

			require.Equal(t, tt.want, readOneBuiltinArg(&registers, op(1, uint8(tt.kind), 0)))
		})
	}
}

func TestCallBuiltinPrintsItsArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		builtinID uint8
		want      string
	}{
		{name: "print joins its operands with no separator", builtinID: isa.BuiltinPrint, want: "7hello"},
		{name: "println separates its operands and ends the line", builtinID: isa.BuiltinPrintln, want: "7 hello\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newExtWordFrame(t,
				op(1, uint8(isa.RegisterInt), 0),
				op(2, uint8(isa.RegisterString), 0),
			)
			output := &strings.Builder{}
			vm.SetStderrWriter(output)
			registers.Ints[1] = 7
			registers.Strings[2] = "hello"

			got := handleCallBuiltin(vm, frame, registers, op(0, tt.builtinID, 2))
			require.Equal(t, opContinue, got)
			require.Equal(t, 3, frame.ProgramCounter, "each argument consumes one extension word")
			require.Equal(t, tt.want, output.String())
		})
	}
}

func TestCallBuiltinSurfacesAWriteFailure(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newExtWordFrame(t, op(1, uint8(isa.RegisterInt), 0))
	vm.SetStderrWriter(failingWriter{})
	registers.Ints[1] = 1

	got := handleCallBuiltin(vm, frame, registers, op(0, isa.BuiltinPrint, 1))
	requireEvalError(t, vm, got, nil)
	require.Contains(t, vm.evalError.Error(), "writer refused")
}

func TestCallBuiltinClearEmptiesTheCollection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		argumentCount uint8
		collection    any
		inspect       func(*testing.T, reflect.Value)
	}{
		{
			name: "a map is emptied", argumentCount: 1, collection: map[string]int{"a": 1},
			inspect: func(t *testing.T, v reflect.Value) { require.Zero(t, v.Len()) },
		},
		{
			name: "a slice is zeroed without shrinking", argumentCount: 1, collection: []int{1, 2},
			inspect: func(t *testing.T, v reflect.Value) {
				require.Equal(t, []int{0, 0}, v.Interface())
			},
		},
		{
			name: "a value of another kind is left alone", argumentCount: 1, collection: "text",
			inspect: func(t *testing.T, v reflect.Value) { require.Equal(t, "text", v.Interface()) },
		},
		{
			name: "a call with no argument does nothing", argumentCount: 0, collection: map[string]int{"a": 1},
			inspect: func(t *testing.T, v reflect.Value) { require.Equal(t, 1, v.Len()) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newExtWordFrame(t, op(1, uint8(isa.RegisterGeneral), 0))
			collection := reflect.ValueOf(tt.collection)
			registers.General[1] = collection

			got := handleCallBuiltin(vm, frame, registers, op(0, isa.BuiltinClear, tt.argumentCount))
			require.Equal(t, opContinue, got)
			tt.inspect(t, collection)
		})
	}
}

func TestCallBuiltinClearIgnoresAnUnsetRegister(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newExtWordFrame(t, op(1, uint8(isa.RegisterGeneral), 0))

	got := handleCallBuiltin(vm, frame, registers, op(0, isa.BuiltinClear, 1))
	require.Equal(t, opContinue, got, "an unset register is nothing to clear, not a fault")
}

func TestCallBuiltinIgnoresAnUnknownIdentifier(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newExtWordFrame(t, op(1, uint8(isa.RegisterInt), 0))

	got := handleCallBuiltin(vm, frame, registers, op(0, 250, 1))
	require.Equal(t, opContinue, got)
}

func TestRecoverOnlyFiresInTheEligibleFrame(t *testing.T) {
	t.Parallel()

	t.Run("a recover outside a panic yields an invalid value", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)

		got := handleRecover(vm, frame, registers, op(0, 0, 0))
		require.Equal(t, opContinue, got)
		require.False(t, registers.General[0].IsValid(), "recover() outside a panic returns nil")
	})

	t.Run("a recover in the eligible frame takes the panic value", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		vm.panicking = true
		vm.panicValue = "boom"
		vm.recoverEligibleFrame = vm.FramePointer

		got := handleRecover(vm, frame, registers, op(0, 0, 0))
		require.Equal(t, opContinue, got)
		require.Equal(t, "boom", registers.General[0].Interface())
		require.False(t, vm.panicking, "the panic is over once it has been recovered")
		require.Nil(t, vm.panicValue)
		require.NoError(t, vm.evalError)
	})

	t.Run("a recover in another frame leaves the panic running", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		vm.panicking = true
		vm.panicValue = "boom"
		vm.recoverEligibleFrame = vm.FramePointer + 1

		got := handleRecover(vm, frame, registers, op(0, 0, 0))
		require.Equal(t, opContinue, got)
		require.False(t, registers.General[0].IsValid())
		require.True(t, vm.panicking, "only the frame that deferred the recover may catch it")
	})

	t.Run("an eligible recover of a nil panic value yields an invalid value", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		vm.panicking = true
		vm.panicValue = nil
		vm.recoverEligibleFrame = vm.FramePointer

		got := handleRecover(vm, frame, registers, op(0, 0, 0))
		require.Equal(t, opContinue, got)
		require.False(t, registers.General[0].IsValid())
	})
}

func TestPanicWithNoRecoverUnwindsToTheHost(t *testing.T) {
	t.Parallel()

	t.Run("a value panic carries the value out", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[0] = reflect.ValueOf("boom")

		got := handlePanic(vm, frame, registers, op(0, 0, 0))
		require.NotEqual(t, opContinue, got, "an unrecovered panic must not continue dispatch")
		require.Equal(t, "boom", vm.panicValue)
	})

	t.Run("an unset register panics with Go's nil-panic error", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)

		got := handlePanic(vm, frame, registers, op(0, 0, 0))
		require.NotEqual(t, opContinue, got)
		require.NotNil(t, vm.panicValue, "a bare panic(nil) still carries Go's placeholder error")
	})

	t.Run("a panic raised while already panicking replaces the value", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		vm.panicking = true
		vm.panicValue = "first"
		registers.General[0] = reflect.ValueOf("second")

		got := handlePanic(vm, frame, registers, op(0, 0, 0))
		require.Equal(t, opPanicError, got)
		require.Equal(t, "second", vm.panicValue)
		require.Error(t, vm.evalError, "a panic during a panic is terminal")
	})
}

func TestRaiseNativePanicAsInterpretedRecordsTheValue(t *testing.T) {
	t.Parallel()

	t.Run("a recovered value becomes the panic value", func(t *testing.T) {
		t.Parallel()

		vm, _, _ := newStandardVM(t)

		got := raiseNativePanicAsInterpreted(vm, "native boom")
		require.NotEqual(t, opContinue, got)
		require.True(t, vm.panicking || vm.evalError != nil, "the panic must be visible one way or the other")
	})

	t.Run("a nil recovery becomes Go's nil-panic error", func(t *testing.T) {
		t.Parallel()

		vm, _, _ := newStandardVM(t)

		got := raiseNativePanicAsInterpreted(vm, nil)
		require.NotEqual(t, opContinue, got)
		require.NotNil(t, vm.panicValue)
		require.Empty(t, vm.callbackPanicStack)
	})
}

func TestDeferRefusesATargetThatIsNotCallable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mode uint8
	}{
		{name: "the chained form", mode: 0},
		{name: "the trivial form", mode: DeferModeTrivial},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newExtWordFrame(t, op(0, 1, 0))
			registers.General[0] = reflect.ValueOf(42)

			got := handleDefer(vm, frame, registers, op(0, 0, tt.mode))
			requireEvalError(t, vm, got, nil)
			require.Contains(t, vm.evalError.Error(), "defer target is not callable")
		})
	}
}

func TestDeferChainsANativeFunction(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newExtWordFrame(t, op(0, 1, uint8(isa.RegisterInt)))
	registers.General[0] = reflect.ValueOf(func(int64) {})
	registers.Ints[1] = 9

	got := handleDefer(vm, frame, registers, op(0, 1, 0))
	require.Equal(t, opContinue, got)
	require.Len(t, vm.deferStack, 1, "the chained form pushes onto the defer stack")
	require.Equal(t, vm.FramePointer, vm.deferStack[0].frameIndex)
}

func TestDeferRefusesToGrowPastTheStackCeiling(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newExtWordFrame(t, op(0, 1, 0))
	vm.deferStack = make([]deferredCall, maxDeferStackSize)
	registers.General[0] = reflect.ValueOf(func() {})

	got := handleDefer(vm, frame, registers, op(0, 0, 0))
	requireEvalError(t, vm, got, errDeferStackExhausted)
}

func TestMaterialiseReflectArgHelpersTolerateAnAbsentArena(t *testing.T) {
	t.Parallel()

	arguments := []reflect.Value{reflect.ValueOf("s"), reflect.ValueOf([]int{1})}

	require.NotPanics(t, func() { materialiseReflectStructArgs(nil, arguments) })
	require.NotPanics(t, func() { materialiseReflectDeferArgs(nil, arguments) })
	require.Equal(t, "s", arguments[0].Interface(), "a nil arena leaves every argument alone")
}

func TestSentinelTypeArgsReadTheCompilerTag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sentinel   reflect.Type
		wantArgs   []string
		wantSuffix string
	}{
		{name: "an instantiated generic", sentinel: reflect.TypeFor[genericPair](), wantArgs: []string{"int", "string"}, wantSuffix: "[int,string]"},
		{name: "a pointer to an instantiated generic", sentinel: reflect.TypeFor[*genericPair](), wantArgs: []string{"int", "string"}, wantSuffix: "[int,string]"},
		{name: "a sentinel with no type arguments", sentinel: reflect.TypeFor[identityTrailing]()},
		{name: "a struct with no sentinel", sentinel: reflect.TypeFor[identityNone]()},
		{name: "a non-struct", sentinel: reflect.TypeFor[int]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.wantArgs, sentinelTypeArgs(tt.sentinel))
			require.Equal(t, tt.wantSuffix, sentinelTypeArgsSuffix(tt.sentinel))
		})
	}
}

func TestResolveReceiverTypeNamePrefersTheSourceLevelName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		receiverType reflect.Type
		site         *program.CallSite
		want         string
	}{
		{name: "a named struct reports its own name", receiverType: reflect.TypeFor[identityNone](), want: "identityNone"},
		{
			name: "a synthesised struct reports its sentinel name", receiverType: reflect.TypeFor[identityTrailing](),
			want: "identityTrailing",
		},
		{
			name: "an unnamed scalar falls back to the call site's static name", receiverType: reflect.TypeFor[int64](),
			site: &program.CallSite{ArgumentStaticTypeNames: []string{"Tag"}}, want: "Tag",
		},
		{name: "an unnamed scalar with no site keeps the primitive name", receiverType: reflect.TypeFor[int64](), want: "int64"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := newTestVM(t)
			require.Equal(t, tt.want, resolveReceiverTypeName(vm, tt.site, tt.receiverType))
		})
	}
}

func TestPlaceOneReflectArgumentWritesTheNamedBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		kind     isa.RegisterKind
		argument any
		inspect  func(*testing.T, *Registers)
	}{
		{
			name: "the int bank", kind: isa.RegisterInt, argument: int64(4),
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(4), r.Ints[0]) },
		},
		{
			name: "the float bank", kind: isa.RegisterFloat, argument: 1.5,
			inspect: func(t *testing.T, r *Registers) { require.InDelta(t, 1.5, r.Floats[0], 0) },
		},
		{
			name: "the string bank", kind: isa.RegisterString, argument: "s",
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "s", r.Strings[0]) },
		},
		{
			name: "the general bank", kind: isa.RegisterGeneral, argument: "boxed",
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "boxed", r.General[0].Interface()) },
		},
		{
			name: "the bool bank", kind: isa.RegisterBool, argument: true,
			inspect: func(t *testing.T, r *Registers) { require.True(t, r.Bools[0]) },
		},
		{
			name: "the uint bank", kind: isa.RegisterUint, argument: uint64(6),
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, uint64(6), r.Uints[0]) },
		},
		{
			name: "the complex bank", kind: isa.RegisterComplex, argument: 2i,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, 2i, r.Complex[0]) },
		},
		{
			name: "the int-slice bank", kind: isa.RegisterSliceInt, argument: []int64{1},
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []int64{1}, r.SlicesInt[0]) },
		},
		{
			name: "the int-slice bank from a platform-width slice", kind: isa.RegisterSliceInt, argument: []int{2},
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []int64{2}, r.SlicesInt[0]) },
		},
		{
			name: "the float-slice bank", kind: isa.RegisterSliceFloat, argument: []float64{1.5},
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []float64{1.5}, r.slicesFloat[0]) },
		},
		{
			name: "the string-slice bank", kind: isa.RegisterSliceString, argument: []string{"s"},
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []string{"s"}, r.slicesString[0]) },
		},
		{
			name: "the bool-slice bank", kind: isa.RegisterSliceBool, argument: []bool{true},
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []bool{true}, r.slicesBool[0]) },
		},
		{
			name: "the uint-slice bank", kind: isa.RegisterSliceUint, argument: []uint64{3},
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []uint64{3}, r.slicesUint[0]) },
		},
		{
			name: "the byte-slice bank", kind: isa.RegisterSliceByte, argument: []byte{4},
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []byte{4}, r.slicesByte[0]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := standardRegisters()
			placeOneReflectArgument(&registers, reflect.ValueOf(tt.argument), tt.kind, 0, nil)
			tt.inspect(t, &registers)
		})
	}

	t.Run("a slice of the wrong element type leaves the bank alone", func(t *testing.T) {
		t.Parallel()

		registers := standardRegisters()
		placeOneReflectArgument(&registers, reflect.ValueOf([]string{"s"}), isa.RegisterSliceFloat, 0, nil)

		require.Nil(t, registers.slicesFloat[0])
	})
}

func TestPlaceReflectArgsFillsEachBankInOrder(t *testing.T) {
	t.Parallel()

	registers := standardRegisters()
	kinds := []isa.RegisterKind{isa.RegisterInt, isa.RegisterString, isa.RegisterInt}

	placeReflectArgs(&registers, []reflect.Value{
		reflect.ValueOf(int64(1)),
		reflect.ValueOf("s"),
		reflect.ValueOf(int64(2)),
	}, kinds, nil)

	require.Equal(t, int64(1), registers.Ints[0])
	require.Equal(t, int64(2), registers.Ints[1])
	require.Equal(t, "s", registers.Strings[0])
}

func TestPlaceReflectArgsStopsAtTheDeclaredParameters(t *testing.T) {
	t.Parallel()

	registers := standardRegisters()

	placeReflectArgs(&registers, []reflect.Value{
		reflect.ValueOf(int64(1)),
		reflect.ValueOf(int64(2)),
	}, []isa.RegisterKind{isa.RegisterInt}, nil)

	require.Equal(t, int64(1), registers.Ints[0])
	require.Zero(t, registers.Ints[1], "an operand the callee does not declare is dropped")
}

func TestCoerceNativeGoroutineArgsFitsTheParameterTypes(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	arguments := []reflect.Value{reflect.ValueOf(int64(1)), reflect.ValueOf(int64(2)), reflect.ValueOf(int64(3))}

	coerceNativeGoroutineArgs(vm, reflect.ValueOf(func(int32, bool) {}), arguments)

	require.Equal(t, int32(1), arguments[0].Interface())
	require.Equal(t, true, arguments[1].Interface())
	require.Equal(t, int64(3), arguments[2].Interface(), "an operand past the signature is left alone")
}
