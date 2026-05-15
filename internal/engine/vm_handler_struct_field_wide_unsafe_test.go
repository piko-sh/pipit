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

//go:build !safe && !(js && wasm)

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func populatedWideFields() *wideFields {
	return &wideFields{
		Signed:   42,
		Unsigned: 7,
		Ratio:    1.5,
		Flag:     true,
		Label:    "text",
		Ints:     []int64{1, 2},
		Floats:   []float64{1.5},
		Uints:    []uint64{3},
		Strings:  []string{"a"},
		Bools:    []bool{true},
		Bytes:    []byte{9},
	}
}

func TestWideStructFieldUnsafeReadsCoverEveryBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		field   int
		handler opcodeHandler
		inspect func(*testing.T, *Registers)
	}{
		{
			name: "a signed field", field: 0, handler: handleGetStructFieldUnsafeInt,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(42), r.Ints[0]) },
		},
		{
			name: "an unsigned field", field: 1, handler: handleGetStructFieldUnsafeUint,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, uint64(7), r.Uints[0]) },
		},
		{
			name: "a bool field", field: 3, handler: handleGetStructFieldUnsafeBool,
			inspect: func(t *testing.T, r *Registers) { require.True(t, r.Bools[0]) },
		},
		{
			name: "a string field", field: 4, handler: handleGetStructFieldUnsafeString,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "text", r.Strings[0]) },
		},
		{
			name: "an int-slice field", field: 5, handler: handleGetStructFieldUnsafeSliceInt,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []int64{1, 2}, r.SlicesInt[0]) },
		},
		{
			name: "a float-slice field", field: 6, handler: handleGetStructFieldUnsafeSliceFloat,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []float64{1.5}, r.slicesFloat[0]) },
		},
		{
			name: "a uint-slice field", field: 7, handler: handleGetStructFieldUnsafeSliceUint,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []uint64{3}, r.slicesUint[0]) },
		},
		{
			name: "a string-slice field", field: 8, handler: handleGetStructFieldUnsafeSliceString,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []string{"a"}, r.slicesString[0]) },
		},
		{
			name: "a bool-slice field", field: 9, handler: handleGetStructFieldUnsafeSliceBool,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []bool{true}, r.slicesBool[0]) },
		},
		{
			name: "a byte-slice field", field: 10, handler: handleGetStructFieldUnsafeSliceByte,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []byte{9}, r.slicesByte[0]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newWideFieldFrame(t, tt.field, populatedWideFields())

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 0, 1)))
			require.Equal(t, 2, frame.ProgramCounter, "the layout word must be stepped over")
			tt.inspect(t, registers)
		})
	}
}

func TestWideStructFieldUnsafeWritesReachTheReceiver(t *testing.T) {
	t.Parallel()

	t.Run("a signed field", func(t *testing.T) {
		t.Parallel()

		receiver := &wideFields{}
		vm, frame, registers := newWideFieldFrame(t, 0, receiver)
		registers.Ints[2] = 11

		require.Equal(t, opContinue, handleSetStructFieldUnsafeInt(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, int64(11), receiver.Signed)
	})

	t.Run("an unsigned field", func(t *testing.T) {
		t.Parallel()

		receiver := &wideFields{}
		vm, frame, registers := newWideFieldFrame(t, 1, receiver)
		registers.Uints[2] = 12

		require.Equal(t, opContinue, handleSetStructFieldUnsafeUint(vm, frame, registers, op(0, 1, 2)))
		require.Equal(t, uint64(12), receiver.Unsigned)
	})
}

func TestWideStructFieldUnsafeReadsWalkANonPointerReceiver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		field   int
		handler opcodeHandler
		inspect func(*testing.T, *Registers)
	}{
		{
			name: "a signed field", field: 0, handler: handleGetStructFieldUnsafeInt,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(42), r.Ints[0]) },
		},
		{
			name: "an unsigned field", field: 1, handler: handleGetStructFieldUnsafeUint,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, uint64(7), r.Uints[0]) },
		},
		{
			name: "a bool field", field: 3, handler: handleGetStructFieldUnsafeBool,
			inspect: func(t *testing.T, r *Registers) { require.True(t, r.Bools[0]) },
		},
		{
			name: "a string field", field: 4, handler: handleGetStructFieldUnsafeString,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "text", r.Strings[0]) },
		},
		{
			name: "an int-slice field", field: 5, handler: handleGetStructFieldUnsafeSliceInt,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, []int64{1, 2}, r.SlicesInt[0]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newWideFieldFrame(t, tt.field, populatedWideFields())

			registers.General[1] = reflect.ValueOf(*populatedWideFields())

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 0, 1)))
			tt.inspect(t, registers)
		})
	}
}

func TestResolveInterfaceBaseReachesTheStorage(t *testing.T) {
	t.Parallel()

	pointerBoxed := reflect.ValueOf(map[string]any{"k": populatedWideFields()}).MapIndex(reflect.ValueOf("k"))
	nilBoxed := reflect.ValueOf(map[string]any{"k": (*wideFields)(nil)}).MapIndex(reflect.ValueOf("k"))
	valueBoxed := reflect.ValueOf(map[string]any{"k": wideFields{}}).MapIndex(reflect.ValueOf("k"))

	tests := []struct {
		name  string
		value reflect.Value
		want  bool
	}{
		{name: "an interface holding a pointer", value: pointerBoxed, want: true},
		{name: "an interface holding a nil pointer", value: nilBoxed, want: false},
		{name: "an interface holding an unaddressable value", value: valueBoxed, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			base, ok := resolveInterfaceBase(tt.value)
			require.Equal(t, tt.want, ok)
			if tt.want {
				require.NotNil(t, base)
			}
		})
	}
}

func TestSliceIndexStructFieldReadsEveryLeafBank(t *testing.T) {
	t.Parallel()

	elements := []wideFields{*populatedWideFields(), {Signed: 99, Unsigned: 8, Ratio: 2.5, Flag: false, Label: "second"}}

	tests := []struct {
		name    string
		field   int
		index   int64
		handler opcodeHandler
		inspect func(*testing.T, *Registers)
	}{
		{
			name: "a signed leaf", field: 0, index: 0, handler: handleSliceIndexStructFieldInt,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(42), r.Ints[0]) },
		},
		{
			name: "a signed leaf at the second element", field: 0, index: 1, handler: handleSliceIndexStructFieldInt,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(99), r.Ints[0]) },
		},
		{
			name: "an unsigned leaf", field: 1, index: 0, handler: handleSliceIndexStructFieldUint,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, uint64(7), r.Uints[0]) },
		},
		{
			name: "a float leaf", field: 2, index: 0, handler: handleSliceIndexStructFieldFloat,
			inspect: func(t *testing.T, r *Registers) { require.InDelta(t, 1.5, r.Floats[0], 0) },
		},
		{
			name: "a bool leaf", field: 3, index: 0, handler: handleSliceIndexStructFieldBool,
			inspect: func(t *testing.T, r *Registers) { require.True(t, r.Bools[0]) },
		},
		{
			name: "a string leaf", field: 4, index: 1, handler: handleSliceIndexStructFieldString,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "second", r.Strings[0]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newWideFieldFrame(t, tt.field, &wideFields{})
			registers.General[1] = reflect.ValueOf(elements)
			registers.Ints[2] = tt.index

			require.Equal(t, opContinue, tt.handler(vm, frame, registers, op(0, 1, 2)))
			require.Equal(t, 2, frame.ProgramCounter, "the layout word must be stepped over")
			tt.inspect(t, registers)
		})
	}
}

func TestSliceIndexStructFieldRefusesBadAccess(t *testing.T) {
	t.Parallel()

	elements := []wideFields{*populatedWideFields()}

	t.Run("an index past the end raises the runtime index error", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newWideFieldFrame(t, 0, &wideFields{})
		registers.General[1] = reflect.ValueOf(elements)
		registers.Ints[2] = 9

		got := handleSliceIndexStructFieldInt(vm, frame, registers, op(0, 1, 2))
		require.NotEqual(t, opContinue, got)
		require.Error(t, vm.evalError)
	})

	t.Run("an unset slice register raises a nil dereference", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newWideFieldFrame(t, 0, &wideFields{})
		registers.General[1] = reflect.Value{}

		got := handleSliceIndexStructFieldInt(vm, frame, registers, op(0, 1, 2))
		require.NotEqual(t, opContinue, got)
		require.Error(t, vm.evalError)
	})

	t.Run("a layout index past the table is refused", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newWideFieldFrame(t, 0, &wideFields{})
		frame.Function.Body[1] = isa.NewInstruction(isa.OpExt, 99, 0, 0)
		registers.General[1] = reflect.ValueOf(elements)

		require.Equal(t, opPanicError, handleSliceIndexStructFieldInt(vm, frame, registers, op(0, 1, 2)))
	})
}

func TestSliceIndexStructFieldWalksAnInterfaceElement(t *testing.T) {
	t.Parallel()

	vm, frame, registers := newWideFieldFrame(t, 0, &wideFields{})
	registers.General[1] = reflect.ValueOf([]any{*populatedWideFields()})
	registers.Ints[2] = 0

	require.Equal(t, opContinue, handleSliceIndexStructFieldInt(vm, frame, registers, op(0, 1, 2)),
		"an element stored as an interface still has to reach the leaf, through reflect")
	require.Equal(t, int64(42), registers.Ints[0])
}

func TestWalkStructFieldByLayoutFollowsThePath(t *testing.T) {
	t.Parallel()

	receiver := reflect.ValueOf(&generalFields{Label: "walked"}).Elem()

	t.Run("a single-step path reaches the field", func(t *testing.T) {
		t.Parallel()

		field := walkStructFieldByLayout(receiver, generalFieldLayout(t, 5))
		require.True(t, field.IsValid())
		require.Equal(t, "walked", field.String())
	})

	t.Run("a path step past the field count declines", func(t *testing.T) {
		t.Parallel()

		layout := generalFieldLayout(t, 5)
		layout.Path[0] = 99

		require.False(t, walkStructFieldByLayout(receiver, layout).IsValid())
	})
}
