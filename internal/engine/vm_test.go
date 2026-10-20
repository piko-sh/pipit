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
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/policy"
)

func TestBasicReflectTypeForKindNamesThePredeclaredTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind reflect.Kind
		want reflect.Type
	}{
		{name: "bool", kind: reflect.Bool, want: reflect.TypeFor[bool]()},
		{name: "int", kind: reflect.Int, want: reflect.TypeFor[int]()},
		{name: "int8", kind: reflect.Int8, want: reflect.TypeFor[int8]()},
		{name: "int16", kind: reflect.Int16, want: reflect.TypeFor[int16]()},
		{name: "int32", kind: reflect.Int32, want: reflect.TypeFor[int32]()},
		{name: "int64", kind: reflect.Int64, want: reflect.TypeFor[int64]()},
		{name: "uint", kind: reflect.Uint, want: reflect.TypeFor[uint]()},
		{name: "uint8", kind: reflect.Uint8, want: reflect.TypeFor[uint8]()},
		{name: "uint16", kind: reflect.Uint16, want: reflect.TypeFor[uint16]()},
		{name: "uint32", kind: reflect.Uint32, want: reflect.TypeFor[uint32]()},
		{name: "uint64", kind: reflect.Uint64, want: reflect.TypeFor[uint64]()},
		{name: "uintptr", kind: reflect.Uintptr, want: reflect.TypeFor[uintptr]()},
		{name: "float32", kind: reflect.Float32, want: reflect.TypeFor[float32]()},
		{name: "float64", kind: reflect.Float64, want: reflect.TypeFor[float64]()},
		{name: "string", kind: reflect.String, want: reflect.TypeFor[string]()},
		{name: "complex64", kind: reflect.Complex64, want: reflect.TypeFor[complex64]()},
		{name: "complex128", kind: reflect.Complex128, want: reflect.TypeFor[complex128]()},
		{name: "a kind with no predeclared scalar falls back to the empty interface", kind: reflect.Struct, want: reflect.TypeFor[any]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, basicReflectTypeForKind(tt.kind))
		})
	}
}

func TestExtractScalarRegisterValueReadsTheNamedBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind isa.RegisterKind
		load func(*Registers)
		want any
	}{
		{name: "the int bank", kind: isa.RegisterInt, load: func(r *Registers) { r.Ints[0] = 4 }, want: 4},
		{name: "the float bank", kind: isa.RegisterFloat, load: func(r *Registers) { r.Floats[0] = 1.5 }, want: 1.5},
		{name: "the bool bank", kind: isa.RegisterBool, load: func(r *Registers) { r.Bools[0] = true }, want: true},
		{name: "the uint bank", kind: isa.RegisterUint, load: func(r *Registers) { r.Uints[0] = 6 }, want: uint64(6)},
		{name: "the complex bank", kind: isa.RegisterComplex, load: func(r *Registers) { r.Complex[0] = 2i }, want: 2i},
		{
			name: "the int-slice bank", kind: isa.RegisterSliceInt,
			load: func(r *Registers) { r.SlicesInt[0] = []int64{1} }, want: []int64{1},
		},
		{
			name: "the float-slice bank", kind: isa.RegisterSliceFloat,
			load: func(r *Registers) { r.slicesFloat[0] = []float64{1.5} }, want: []float64{1.5},
		},
		{
			name: "the string-slice bank", kind: isa.RegisterSliceString,
			load: func(r *Registers) { r.slicesString[0] = []string{"s"} }, want: []string{"s"},
		},
		{
			name: "the bool-slice bank", kind: isa.RegisterSliceBool,
			load: func(r *Registers) { r.slicesBool[0] = []bool{true} }, want: []bool{true},
		},
		{
			name: "the uint-slice bank", kind: isa.RegisterSliceUint,
			load: func(r *Registers) { r.slicesUint[0] = []uint64{2} }, want: []uint64{2},
		},
		{
			name: "the byte-slice bank", kind: isa.RegisterSliceByte,
			load: func(r *Registers) { r.slicesByte[0] = []byte{3} }, want: []byte{3},
		},
		{name: "a bank with no scalar form", kind: isa.RegisterGeneral, load: func(*Registers) {}, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := standardRegisters()
			tt.load(&registers)

			require.Equal(t, tt.want, extractScalarRegisterValue(&registers, tt.kind, 0))
			require.Nil(t, extractScalarRegisterValue(&registers, tt.kind, 99),
				"a slot past the bank has nothing to read")
		})
	}
}

func TestExtractStringRegisterValueMaterialisesTheSlot(t *testing.T) {
	t.Parallel()

	registers := standardRegisters()
	registers.Strings[0] = "value"
	arena := newTestArena(t)

	require.Equal(t, "value", extractStringRegisterValue(arena, &registers, 0))
	require.Nil(t, extractStringRegisterValue(arena, &registers, 99))
}

func TestExtractGeneralRegisterValueBoxesTheSlot(t *testing.T) {
	t.Parallel()

	registers := standardRegisters()
	registers.General[0] = reflect.ValueOf("boxed")

	require.Equal(t, "boxed", extractGeneralRegisterValue(&registers, 0))
	require.Nil(t, extractGeneralRegisterValue(&registers, 99))
	require.Nil(t, extractGeneralRegisterValue(&registers, 1), "an unset register holds nothing")
}

func TestSaturatingProductClampsAtTheArenaCeiling(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    int
		b    int
		want int
	}{
		{name: "an ordinary product", a: 3, b: 4, want: 12},
		{name: "a zero multiplicand", a: 0, b: 4, want: 0},
		{name: "a negative multiplicand", a: -1, b: 4, want: 0},
		{name: "a product past the ceiling clamps", a: maxArenaPreSizeBacking, b: 2, want: maxArenaPreSizeBacking},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, saturatingProduct(tt.a, tt.b))
		})
	}

	t.Run("the three-way form saturates in two steps", func(t *testing.T) {
		t.Parallel()

		require.Equal(t, 24, saturatingProduct3(2, 3, 4))
		require.Equal(t, maxArenaPreSizeBacking, saturatingProduct3(maxArenaPreSizeBacking, 2, 2))
	})
}

func TestClampAtMostNormalisesNegatives(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   int
		ceiling int
		want    int
	}{
		{name: "a value inside the ceiling", value: 4, ceiling: 8, want: 4},
		{name: "a value at the ceiling", value: 8, ceiling: 8, want: 8},
		{name: "a value past the ceiling", value: 9, ceiling: 8, want: 8},
		{name: "a negative value", value: -3, ceiling: 8, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, clampAtMost(tt.value, tt.ceiling))
		})
	}
}

func TestBundleUsesTypedSliceBanksWalksTheWholeBundle(t *testing.T) {
	t.Parallel()

	typed := program.NewNamedFunction("typed")
	typed.NonZeroBankMask = typedSliceBankMask

	t.Run("a nil bundle uses nothing", func(t *testing.T) {
		t.Parallel()

		require.False(t, bundleUsesTypedSliceBanks(nil))
	})

	t.Run("a root that uses one reports true", func(t *testing.T) {
		t.Parallel()

		require.True(t, bundleUsesTypedSliceBanks(typed))
	})

	t.Run("a sibling that uses one reports true", func(t *testing.T) {
		t.Parallel()

		root := program.NewNamedFunction("root")
		root.Functions = []*program.CompiledFunction{nil, typed}

		require.True(t, bundleUsesTypedSliceBanks(root))
	})

	t.Run("a bundle that uses none reports false", func(t *testing.T) {
		t.Parallel()

		root := program.NewNamedFunction("root")
		root.Functions = []*program.CompiledFunction{program.NewNamedFunction("plain")}

		require.False(t, bundleUsesTypedSliceBanks(root))
	})
}

func TestClassifyRecoveredPanicKeepsOnlyTheArenaBudgetError(t *testing.T) {
	t.Parallel()

	t.Run("nothing recovered is no error", func(t *testing.T) {
		t.Parallel()

		require.NoError(t, ClassifyRecoveredPanic(nil))
	})

	t.Run("an arena-budget panic becomes the error", func(t *testing.T) {
		t.Parallel()

		budget := fault.ErrArenaBudgetExceeded

		require.ErrorIs(t, ClassifyRecoveredPanic(budget), fault.ErrArenaBudgetExceeded)
	})

	t.Run("any other panic is re-raised", func(t *testing.T) {
		t.Parallel()

		require.Panics(t, func() { _ = ClassifyRecoveredPanic("boom") })
		require.Panics(t, func() { _ = ClassifyRecoveredPanic(errors.New("other")) })
	})
}

func TestRecoverArenaBudgetErrorFillsTheCallerSlot(t *testing.T) {
	t.Parallel()

	t.Run("an arena-budget panic lands in the error slot", func(t *testing.T) {
		t.Parallel()

		captured := func() (err error) {
			defer RecoverArenaBudgetError(&err)
			panic(fault.ErrArenaBudgetExceeded)
		}()

		require.ErrorIs(t, captured, fault.ErrArenaBudgetExceeded)
	})

	t.Run("a clean return leaves the slot alone", func(t *testing.T) {
		t.Parallel()

		captured := func() (err error) {
			defer RecoverArenaBudgetError(&err)
			return errors.New("original")
		}()

		require.EqualError(t, captured, "original")
	})
}

func TestGuardCallDepthPanicsAtTheCeiling(t *testing.T) {
	t.Parallel()

	t.Run("a shallow stack passes", func(t *testing.T) {
		t.Parallel()

		vm, _, _ := newStandardVM(t)

		require.NotPanics(t, vm.guardCallDepth)
	})

	t.Run("a stack at the limit raises the stack-overflow error", func(t *testing.T) {
		t.Parallel()

		vm, _, _ := newStandardVM(t)
		vm.FramePointer = vm.callDepthLimit()

		require.PanicsWithError(t, vm.stackOverflowError().Error(), vm.guardCallDepth)
	})
}

func TestEnsureGoroutineLimitInstallsATracker(t *testing.T) {
	t.Parallel()

	t.Run("a VM with no limit takes the policy default", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)

		require.Equal(t, int32(policy.DefaultMaxGoroutines), vm.ensureGoroutineLimit())
		require.NotNil(t, vm.Limits.Tracker, "the cap is only enforceable with a tracker to count against")
	})

	t.Run("a configured limit is used as given", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		vm.Limits.MaxGoroutines = 7

		require.Equal(t, int32(7), vm.ensureGoroutineLimit())
	})
}

func TestCheckOutputLimitOnlyFiresWhenConfigured(t *testing.T) {
	t.Parallel()

	t.Run("no limit never fires", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)

		require.False(t, vm.checkOutputLimit())
	})

	t.Run("a limit with no tracker never fires", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		vm.Limits.MaxOutputSize = 1

		require.False(t, vm.checkOutputLimit())
	})

	t.Run("output within the limit passes", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		vm.Limits.MaxOutputSize = 16
		vm.Limits.Tracker = &ResourceTracker{}

		require.False(t, vm.checkOutputLimit())
	})

	t.Run("output past the limit records the fault", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		vm.Limits.MaxOutputSize = 2
		vm.Limits.Tracker = &ResourceTracker{}
		vm.Limits.Tracker.outputBytes.Store(9)

		require.True(t, vm.checkOutputLimit())
		require.ErrorIs(t, vm.evalError, fault.ErrOutputLimit)
	})
}

func TestLimitedStderrCountsOnlyWhenABudgetExists(t *testing.T) {
	t.Parallel()

	t.Run("no budget hands back the plain writer", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		output := &strings.Builder{}
		vm.SetStderrWriter(output)

		require.Same(t, output, vm.limitedStderr())
	})

	t.Run("a budget wraps the writer in a counter", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		vm.Limits.MaxOutputSize = 16
		vm.Limits.Tracker = &ResourceTracker{}
		output := &strings.Builder{}
		vm.SetStderrWriter(output)

		_, err := vm.limitedStderr().Write([]byte("abc"))
		require.NoError(t, err)
		require.Equal(t, "abc", output.String())
		require.Equal(t, int64(3), vm.Limits.Tracker.outputBytes.Load())
	})
}

func TestBindFileSetSwapsTheFunctionTable(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	root := program.NewNamedFunction("root")
	functions := []*program.CompiledFunction{program.NewNamedFunction("one")}

	vm.BindFileSet(functions, root)

	require.Same(t, root, vm.rootFunction)
	require.Equal(t, functions, vm.functions)
}

func TestGoroutinePanicIsEmptyUntilOneIsRecorded(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)

	require.Nil(t, vm.GoroutinePanic())
}

func TestSetExecutionCancelSharesTheCancelWithTheTracker(t *testing.T) {
	t.Parallel()

	vm := newTestVM(t)
	vm.Limits.Tracker = &ResourceTracker{}
	cancelled := false

	vm.SetExecutionCancel(func(error) { cancelled = true })

	require.NotNil(t, vm.executionCancel)
	vm.executionCancel(nil)
	require.True(t, cancelled)
	require.NotNil(t, vm.Limits.Tracker.executionCancel.Load(), "a spawned goroutine cancels through the tracker")
}
