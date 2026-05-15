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

	"pipit.sh/pipit/internal/isa"
)

type recordHolder struct {
	Count  int64
	Label  string
	Flag   bool
	Items  []int64
	Nested *recordHolder
	Any    any
}

func TestGetFieldReadsThroughEveryReceiverShape(t *testing.T) {
	t.Parallel()

	holder := recordHolder{Count: 7, Label: "name"}

	tests := []struct {
		name     string
		receiver reflect.Value
		field    uint8
		want     any
	}{
		{name: "a struct value", receiver: reflect.ValueOf(holder), field: 0, want: int64(7)},
		{name: "a pointer to a struct", receiver: reflect.ValueOf(&holder), field: 1, want: "name"},
		{
			name:     "an interface holding a struct",
			receiver: reflect.ValueOf(any(holder)),
			field:    0, want: int64(7),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newStandardVM(t)
			registers.General[1] = tt.receiver

			got := handleGetField(vm, frame, registers, op(0, 1, tt.field))
			require.Equal(t, opContinue, got)
			require.Equal(t, tt.want, registers.General[0].Interface())
		})
	}
}

func TestGetFieldUnwrapsAndRefusesBrokenReceivers(t *testing.T) {
	t.Parallel()

	t.Run("a nil interface field reads back as an invalid value", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf(recordHolder{})

		got := handleGetField(vm, frame, registers, op(0, 1, 5))
		require.Equal(t, opContinue, got)
		require.False(t, registers.General[0].IsValid(), "a nil interface field has no dynamic value")
	})

	t.Run("a populated interface field reads back as its dynamic value", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf(recordHolder{Any: 4})

		got := handleGetField(vm, frame, registers, op(0, 1, 5))
		require.Equal(t, opContinue, got)
		require.Equal(t, 4, registers.General[0].Interface())
	})

	t.Run("an unset receiver raises a nil dereference", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)

		got := handleGetField(vm, frame, registers, op(0, 1, 0))
		requireRuntimePanic(t, vm, got, "nil pointer dereference")
	})

	t.Run("a nil pointer receiver raises a nil dereference", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf((*recordHolder)(nil))

		got := handleGetField(vm, frame, registers, op(0, 1, 0))
		requireRuntimePanic(t, vm, got, "nil pointer dereference")
	})

	t.Run("a receiver that is not a struct is an invariant break", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf(4)

		require.Panics(t, func() { handleGetField(vm, frame, registers, op(0, 1, 0)) })
	})

	t.Run("a field index past the struct is an invariant break", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf(recordHolder{})

		require.Panics(t, func() { handleGetField(vm, frame, registers, op(0, 1, 40)) })
	})
}

func TestSetFieldWritesThroughTheStruct(t *testing.T) {
	t.Parallel()

	t.Run("a pointer receiver has its field replaced", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		holder := &recordHolder{}
		registers.General[0] = reflect.ValueOf(holder)
		registers.General[2] = reflect.ValueOf("written")

		got := handleSetField(vm, frame, registers, op(0, 1, 2))
		require.Equal(t, opContinue, got)
		require.Equal(t, "written", holder.Label)
	})

	t.Run("an integer field takes the fast path", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		holder := &recordHolder{}
		registers.General[0] = reflect.ValueOf(holder)
		registers.General[2] = reflect.ValueOf(int64(12))

		got := handleSetField(vm, frame, registers, op(0, 0, 2))
		require.Equal(t, opContinue, got)
		require.Equal(t, int64(12), holder.Count)
	})

	t.Run("an unset receiver raises a nil dereference", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)

		got := handleSetField(vm, frame, registers, op(0, 0, 2))
		requireRuntimePanic(t, vm, got, "nil pointer dereference")
	})

	t.Run("a field index past the struct is an invariant break", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[0] = reflect.ValueOf(&recordHolder{})
		registers.General[2] = reflect.ValueOf(int64(1))

		require.Panics(t, func() { handleSetField(vm, frame, registers, op(0, 40, 2)) })
	})
}

func TestSetFieldDerefWritesThroughThePointer(t *testing.T) {
	t.Parallel()

	t.Run("a pointer to a scalar takes the value", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		target := int64(0)
		registers.General[0] = reflect.ValueOf(&target)
		registers.General[2] = reflect.ValueOf(int64(5))

		got := handleSetField(vm, frame, registers, op(0, isa.SentinelFieldDeref, 2))
		require.Equal(t, opContinue, got)
		require.Equal(t, int64(5), target)
	})

	t.Run("a pointer to a slice takes the whole header", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		target := []int64{1}
		registers.General[0] = reflect.ValueOf(&target)
		registers.General[2] = reflect.ValueOf([]int64{7, 8, 9})

		got := handleSetField(vm, frame, registers, op(0, isa.SentinelFieldDeref, 2))
		require.Equal(t, opContinue, got)
		require.Equal(t, []int64{7, 8, 9}, target)
	})

	t.Run("a nil pointer raises a nil dereference", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[0] = reflect.ValueOf((*int64)(nil))
		registers.General[2] = reflect.ValueOf(int64(5))

		got := handleSetField(vm, frame, registers, op(0, isa.SentinelFieldDeref, 2))
		requireRuntimePanic(t, vm, got, "nil pointer dereference")
	})

	t.Run("a receiver that is not a pointer is an invariant break", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[0] = reflect.ValueOf(int64(1))
		registers.General[2] = reflect.ValueOf(int64(5))

		require.Panics(t, func() { handleSetField(vm, frame, registers, op(0, isa.SentinelFieldDeref, 2)) })
	})
}

func TestFieldIntHandlersSkipTheGeneralBank(t *testing.T) {
	t.Parallel()

	t.Run("an int field reads straight into the int bank", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf(recordHolder{Count: 9})

		got := handleGetFieldInt(vm, frame, registers, op(0, 1, 0))
		require.Equal(t, opContinue, got)
		require.Equal(t, int64(9), registers.Ints[0])
	})

	t.Run("a bool field reads as one or zero", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name string
			flag bool
			want int64
		}{
			{name: "a set flag reads as one", flag: true, want: 1},
			{name: "a clear flag reads as zero", flag: false, want: 0},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				vm, frame, registers := newStandardVM(t)
				registers.General[1] = reflect.ValueOf(recordHolder{Flag: tt.flag})

				got := handleGetFieldInt(vm, frame, registers, op(0, 1, 2))
				require.Equal(t, opContinue, got)
				require.Equal(t, tt.want, registers.Ints[0])
			})
		}
	})

	t.Run("a pointer receiver is dereferenced", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf(&recordHolder{Count: 3})

		got := handleGetFieldInt(vm, frame, registers, op(0, 1, 0))
		require.Equal(t, opContinue, got)
		require.Equal(t, int64(3), registers.Ints[0])
	})

	t.Run("an unset receiver raises a nil dereference", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)

		got := handleGetFieldInt(vm, frame, registers, op(0, 1, 0))
		requireRuntimePanic(t, vm, got, "nil pointer dereference")
	})

	t.Run("a nil pointer receiver is an invariant break", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf((*recordHolder)(nil))

		require.Panics(t, func() { handleGetFieldInt(vm, frame, registers, op(0, 1, 0)) })
	})

	t.Run("an int field takes a write straight from the int bank", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		holder := &recordHolder{}
		registers.General[0] = reflect.ValueOf(holder)
		registers.Ints[2] = 11

		got := handleSetFieldInt(vm, frame, registers, op(0, 0, 2))
		require.Equal(t, opContinue, got)
		require.Equal(t, int64(11), holder.Count)
	})

	t.Run("a bool field takes any non-zero as true", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		holder := &recordHolder{}
		registers.General[0] = reflect.ValueOf(holder)
		registers.Ints[2] = 5

		got := handleSetFieldInt(vm, frame, registers, op(0, 2, 2))
		require.Equal(t, opContinue, got)
		require.True(t, holder.Flag)
	})

	t.Run("an unset receiver raises a nil dereference on write", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)

		got := handleSetFieldInt(vm, frame, registers, op(0, 0, 2))
		requireRuntimePanic(t, vm, got, "nil pointer dereference")
	})

	t.Run("a field index past the struct is an invariant break on write", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		registers.General[0] = reflect.ValueOf(&recordHolder{})

		require.Panics(t, func() { handleSetFieldInt(vm, frame, registers, op(0, 40, 2)) })
	})
}

func TestDerefSliceIntHandlersReadAndWriteThroughThePointer(t *testing.T) {
	t.Parallel()

	t.Run("an in-range read takes the element", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		backing := []int64{4, 5, 6}
		registers.General[1] = reflect.ValueOf(&backing)
		registers.Ints[2] = 1

		got := handleDerefSliceGetInt(vm, frame, registers, op(0, 1, 2))
		require.Equal(t, opContinue, got)
		require.Equal(t, int64(5), registers.Ints[0])
	})

	t.Run("an in-range write lands in the caller's slice", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		backing := []int64{4, 5, 6}
		registers.General[0] = reflect.ValueOf(&backing)
		registers.Ints[1] = 2
		registers.Ints[2] = 99

		got := handleDerefSliceSetInt(vm, frame, registers, op(0, 1, 2))
		require.Equal(t, opContinue, got)
		require.Equal(t, []int64{4, 5, 99}, backing)
	})

	t.Run("a read past the end raises the runtime index error", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		backing := []int64{4}
		registers.General[1] = reflect.ValueOf(&backing)
		registers.Ints[2] = 3

		got := handleDerefSliceGetInt(vm, frame, registers, op(0, 1, 2))
		requireRuntimePanic(t, vm, got, "index out of range [3] with length 1")
	})

	t.Run("a negative read index raises the runtime index error", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		backing := []int64{4}
		registers.General[1] = reflect.ValueOf(&backing)
		registers.Ints[2] = -1

		got := handleDerefSliceGetInt(vm, frame, registers, op(0, 1, 2))
		requireRuntimePanic(t, vm, got, "index out of range [-1]")
	})

	t.Run("a write past the end raises the runtime index error", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		backing := []int64{4}
		registers.General[0] = reflect.ValueOf(&backing)
		registers.Ints[1] = 9

		got := handleDerefSliceSetInt(vm, frame, registers, op(0, 1, 2))
		requireRuntimePanic(t, vm, got, "index out of range [9] with length 1")
	})

	t.Run("a receiver that is not a pointer falls back to reflect", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)

		got := handleDerefSliceGetInt(vm, frame, registers, op(0, 1, 2))
		requireRuntimePanic(t, vm, got, "nil pointer dereference")
	})
}

func TestRaiseNilDereferenceCarriesGoWording(t *testing.T) {
	t.Parallel()

	vm, _, _ := newStandardVM(t)

	got := raiseNilDereference(vm)
	requireRuntimePanic(t, vm, got, nilDereferenceMessage)
}
