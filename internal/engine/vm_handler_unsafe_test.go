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
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
)

func TestUnsafeStringRefusesOperandsItCannotTrust(t *testing.T) {
	t.Parallel()

	backing := []byte("pipit")

	t.Run("a pointer and a length produce the string", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf(&backing[0])
		registers.Ints[2] = 5

		require.Equal(t, opContinue, handleUnsafeString(vm, nil, registers, op(0, 1, 2)))
		require.Equal(t, "pipit", registers.Strings[0])
	})

	t.Run("a zero length yields the empty string without reading the pointer", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf((*byte)(nil))
		registers.Ints[2] = 0

		require.Equal(t, opContinue, handleUnsafeString(vm, nil, registers, op(0, 1, 2)))
		require.Empty(t, registers.Strings[0])
	})

	t.Run("the result does not alias the source memory", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		source := []byte("abc")
		registers.General[1] = reflect.ValueOf(&source[0])
		registers.Ints[2] = 3

		require.Equal(t, opContinue, handleUnsafeString(vm, nil, registers, op(0, 1, 2)))
		source[0] = 'z'
		require.Equal(t, "abc", registers.Strings[0],
			"the handler copies, so a later write to the source cannot mutate a Go string")
	})

	refusals := []struct {
		build   func(registers *Registers)
		name    string
		message string
	}{
		{
			name:    "a non-pointer operand",
			build:   func(r *Registers) { r.General[1] = reflect.ValueOf(42); r.Ints[2] = 1 },
			message: "non-pointer",
		},
		{
			name:    "an invalid operand",
			build:   func(r *Registers) { r.Ints[2] = 1 },
			message: "non-pointer",
		},
		{
			name:    "a negative length",
			build:   func(r *Registers) { r.General[1] = reflect.ValueOf(&backing[0]); r.Ints[2] = -1 },
			message: "len out of range",
		},
		{
			name:    "a nil pointer with a length",
			build:   func(r *Registers) { r.General[1] = reflect.ValueOf((*byte)(nil)); r.Ints[2] = 3 },
			message: "ptr is nil",
		},
	}

	for _, tt := range refusals {
		t.Run(tt.name+" is refused", func(t *testing.T) {
			t.Parallel()
			vm, _, registers := newStandardVM(t)
			tt.build(registers)

			got := handleUnsafeString(vm, nil, registers, op(0, 1, 2))

			require.NotEqual(t, opContinue, got)
			require.Error(t, vm.evalError)
			require.Contains(t, vm.evalError.Error(), tt.message)
		})
	}

	t.Run("a length past the allocation cap is refused", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		vm.Limits.MaxAllocSize = 2
		registers.General[1] = reflect.ValueOf(&backing[0])
		registers.Ints[2] = 5

		require.Equal(t, opPanicError, handleUnsafeString(vm, nil, registers, op(0, 1, 2)))
		require.ErrorIs(t, vm.evalError, fault.ErrAllocationLimit)
	})
}

func TestUnsafeStringDataCopiesAwayFromTheStringBacking(t *testing.T) {
	t.Parallel()

	t.Run("a populated string yields a pointer to a copy", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		registers.Strings[1] = "pipit"

		require.Equal(t, opContinue, handleUnsafeStringData(vm, nil, registers, op(0, 1, 0)))

		pointer := registers.General[0]
		require.Equal(t, reflect.Pointer, pointer.Kind())
		require.False(t, pointer.IsNil())
		require.Equal(t, byte('p'), *(*byte)(pointer.UnsafePointer()))
	})

	t.Run("an empty string yields a typed nil pointer", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		registers.Strings[1] = ""

		require.Equal(t, opContinue, handleUnsafeStringData(vm, nil, registers, op(0, 1, 0)))

		pointer := registers.General[0]
		require.Equal(t, reflect.Pointer, pointer.Kind())
		require.True(t, pointer.IsNil(), "an empty string has no first byte to address")
	})
}

func TestUnsafeAddRefusesOperandsItCannotTrust(t *testing.T) {
	t.Parallel()

	backing := []byte("abcd")

	t.Run("a positive offset moves the pointer forward", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf(unsafe.Pointer(&backing[0]))
		registers.Ints[2] = 2

		require.Equal(t, opContinue, handleUnsafeAdd(vm, nil, registers, op(0, 1, 2)))
		require.Equal(t, byte('c'), *(*byte)(registers.General[0].UnsafePointer()))
	})

	t.Run("a zero offset leaves the pointer where it was", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf(unsafe.Pointer(&backing[0]))
		registers.Ints[2] = 0

		require.Equal(t, opContinue, handleUnsafeAdd(vm, nil, registers, op(0, 1, 2)))
		require.Equal(t, byte('a'), *(*byte)(registers.General[0].UnsafePointer()))
	})

	t.Run("a nil pointer yields a nil pointer without arithmetic", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf(unsafe.Pointer(nil))
		registers.Ints[2] = 8

		require.Equal(t, opContinue, handleUnsafeAdd(vm, nil, registers, op(0, 1, 2)))
		require.True(t, registers.General[0].IsNil())
	})

	t.Run("a non-pointer operand is refused", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		registers.General[1] = reflect.ValueOf(42)

		require.Equal(t, opPanicError, handleUnsafeAdd(vm, nil, registers, op(0, 1, 2)))
		require.Contains(t, vm.evalError.Error(), "non-pointer")
	})

	t.Run("safe mode refuses a pointer with no tracked origin", func(t *testing.T) {
		t.Parallel()
		vm, _, registers := newStandardVM(t)
		vm.Limits.SafeMode = true
		registers.General[1] = reflect.ValueOf(unsafe.Pointer(&backing[0]))
		registers.Ints[2] = 1

		require.Equal(t, opPanicError, handleUnsafeAdd(vm, nil, registers, op(0, 1, 2)))
		require.Contains(t, vm.evalError.Error(), "no tracked origin",
			"safe mode only permits arithmetic on a pointer whose extent it recorded")
	})
}
