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

func TestBoxTypedAsDeclaredTypeSharesTheBackingArray(t *testing.T) {
	t.Parallel()

	tests := []struct {
		declared reflect.Type
		load     func(r *Registers)
		read     func(r *Registers) any
		name     string
		kind     isa.RegisterKind
	}{
		{
			name: "an int slice held as int64 storage", kind: isa.RegisterSliceInt64, declared: reflect.TypeFor[[]int](),
			load: func(r *Registers) { r.SlicesInt[0] = []int64{3, 1, 2} },
			read: func(r *Registers) any { return r.SlicesInt[0] },
		},
		{
			name: "an int64 slice", kind: isa.RegisterSliceInt64, declared: reflect.TypeFor[[]int64](),
			load: func(r *Registers) { r.SlicesInt[0] = []int64{3, 1, 2} },
			read: func(r *Registers) any { return r.SlicesInt[0] },
		},
		{
			name: "a uint slice held as uint64 storage", kind: isa.RegisterSliceUint64, declared: reflect.TypeFor[[]uint](),
			load: func(r *Registers) { r.slicesUint[0] = []uint64{3, 1, 2} },
			read: func(r *Registers) any { return r.slicesUint[0] },
		},
		{
			name: "a float slice", kind: isa.RegisterSliceFloat64, declared: reflect.TypeFor[[]float64](),
			load: func(r *Registers) { r.slicesFloat[0] = []float64{3, 1, 2} },
			read: func(r *Registers) any { return r.slicesFloat[0] },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := NewRegisters(typedSliceRegisterCounts())
			tt.load(&registers)

			boxed, ok := boxTypedAsDeclaredType(nil, tt.declared, &registers, tt.kind, 0)

			require.True(t, ok)
			require.Equal(t, tt.declared, boxed.Type(), "the boxed value has the declared type, not the bank's storage type")
			boxed.Index(0).Set(reflect.ValueOf(int64(9)).Convert(tt.declared.Elem()))
			require.Equal(t, reflect.ValueOf(9).Convert(reflect.TypeOf(tt.read(&registers)).Elem()).Interface(),
				reflect.ValueOf(tt.read(&registers)).Index(0).Interface(), "a write through the boxed value reaches the register's backing array")
		})
	}
}

func TestBoxTypedAsDeclaredTypeGivesAScalarItsDeclaredType(t *testing.T) {
	t.Parallel()

	registers := NewRegisters(typedSliceRegisterCounts())
	registers.Ints[0] = 7
	registers.Uints[0] = 8

	narrow, ok := boxTypedAsDeclaredType(nil, reflect.TypeFor[int32](), &registers, isa.RegisterInt64, 0)
	require.True(t, ok)
	require.Equal(t, int32(7), narrow.Interface())

	byteValue, ok := boxTypedAsDeclaredType(nil, reflect.TypeFor[uint8](), &registers, isa.RegisterUint64, 0)
	require.True(t, ok)
	require.Equal(t, uint8(8), byteValue.Interface())
}

func TestBoxTypedAsDeclaredTypeDeclinesWhatTheBankCannotHold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		declared reflect.Type
		name     string
		kind     isa.RegisterKind
	}{
		{name: "no declared type", declared: nil, kind: isa.RegisterSliceInt64},
		{name: "the general bank", declared: reflect.TypeFor[[]int](), kind: isa.RegisterGeneral},
		{name: "a slice bank declared as a non-slice", declared: reflect.TypeFor[int](), kind: isa.RegisterSliceInt64},
		{name: "an interface type", declared: reflect.TypeFor[any](), kind: isa.RegisterInt64},
		{name: "a string declared for the int bank", declared: reflect.TypeFor[string](), kind: isa.RegisterInt64},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			registers := NewRegisters(typedSliceRegisterCounts())

			_, ok := boxTypedAsDeclaredType(nil, tt.declared, &registers, tt.kind, 0)

			require.False(t, ok)
		})
	}
}

func typedSliceRegisterCounts() [isa.NumRegisterKinds]uint32 {
	counts := testRegCounts()
	for _, kind := range []isa.RegisterKind{
		isa.RegisterSliceInt64, isa.RegisterSliceFloat64, isa.RegisterSliceString,
		isa.RegisterSliceBool, isa.RegisterSliceUint64, isa.RegisterSliceByte,
	} {
		counts[kind] = 4
	}
	return counts
}
