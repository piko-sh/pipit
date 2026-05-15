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

package isaselect

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestRegisterKindForReflectKindRoutesEveryScalar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind reflect.Kind
		want isa.RegisterKind
	}{
		{name: "a plain int uses the integer bank", kind: reflect.Int, want: isa.RegisterInt},
		{name: "an int8 uses the integer bank", kind: reflect.Int8, want: isa.RegisterInt},
		{name: "an int16 uses the integer bank", kind: reflect.Int16, want: isa.RegisterInt},
		{name: "an int32 uses the integer bank", kind: reflect.Int32, want: isa.RegisterInt},
		{name: "an int64 uses the integer bank", kind: reflect.Int64, want: isa.RegisterInt},
		{name: "a plain uint uses the unsigned bank", kind: reflect.Uint, want: isa.RegisterUint},
		{name: "a uint8 uses the unsigned bank", kind: reflect.Uint8, want: isa.RegisterUint},
		{name: "a uint16 uses the unsigned bank", kind: reflect.Uint16, want: isa.RegisterUint},
		{name: "a uint32 uses the unsigned bank", kind: reflect.Uint32, want: isa.RegisterUint},
		{name: "a uint64 uses the unsigned bank", kind: reflect.Uint64, want: isa.RegisterUint},
		{name: "a uintptr uses the unsigned bank", kind: reflect.Uintptr, want: isa.RegisterUint},
		{name: "a float32 uses the float bank", kind: reflect.Float32, want: isa.RegisterFloat},
		{name: "a float64 uses the float bank", kind: reflect.Float64, want: isa.RegisterFloat},
		{name: "a bool uses the boolean bank", kind: reflect.Bool, want: isa.RegisterBool},
		{name: "a string uses the string bank", kind: reflect.String, want: isa.RegisterString},
		{name: "a complex value falls back to the general bank", kind: reflect.Complex128, want: isa.RegisterGeneral},
		{name: "a slice falls back to the general bank", kind: reflect.Slice, want: isa.RegisterGeneral},
		{name: "a map falls back to the general bank", kind: reflect.Map, want: isa.RegisterGeneral},
		{name: "a struct falls back to the general bank", kind: reflect.Struct, want: isa.RegisterGeneral},
		{name: "a pointer falls back to the general bank", kind: reflect.Pointer, want: isa.RegisterGeneral},
		{name: "an interface falls back to the general bank", kind: reflect.Interface, want: isa.RegisterGeneral},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, RegisterKindForReflectKind(tt.kind))
		})
	}
}

func TestTypedSliceSubOpSelectorsCoverEveryTypedBank(t *testing.T) {
	t.Parallel()

	typedBanks := []struct {
		name string
		kind isa.RegisterKind
	}{
		{name: "the integer slice bank", kind: isa.RegisterSliceInt},
		{name: "the float slice bank", kind: isa.RegisterSliceFloat},
		{name: "the string slice bank", kind: isa.RegisterSliceString},
		{name: "the boolean slice bank", kind: isa.RegisterSliceBool},
		{name: "the unsigned slice bank", kind: isa.RegisterSliceUint},
		{name: "the byte slice bank", kind: isa.RegisterSliceByte},
	}

	selectors := []struct {
		pick func(isa.RegisterKind) (isa.SubOpcode, bool)
		name string
	}{
		{name: "make", pick: TypedMakeSliceSubOp},
		{name: "length", pick: TypedSliceDirectLenSubOp},
		{name: "capacity", pick: TypedSliceDirectCapSubOp},
		{name: "copy", pick: TypedSliceDirectCopySubOp},
	}

	for _, selector := range selectors {
		t.Run("the "+selector.name+" selector", func(t *testing.T) {
			t.Parallel()
			seen := make(map[isa.SubOpcode]string, len(typedBanks))
			for _, bank := range typedBanks {
				subOp, ok := selector.pick(bank.kind)

				require.Truef(t, ok, "%s must have a %s sub-opcode", bank.name, selector.name)
				require.Emptyf(t, seen[subOp], "%s reuses the sub-opcode of %s", bank.name, seen[subOp])
				seen[subOp] = bank.name
			}

			for _, scalar := range []isa.RegisterKind{isa.RegisterInt, isa.RegisterFloat, isa.RegisterString, isa.RegisterGeneral, isa.RegisterBool, isa.RegisterUint, isa.RegisterComplex} {
				_, ok := selector.pick(scalar)
				require.Falsef(t, ok, "a scalar bank has no typed-slice %s sub-opcode", selector.name)
			}
		})
	}
}

func TestStructFieldTier0SelectorsCoverTheScalarBanks(t *testing.T) {
	t.Parallel()

	admitted := []struct {
		name string
		kind isa.RegisterKind
	}{
		{name: "the integer bank", kind: isa.RegisterInt},
		{name: "the unsigned bank", kind: isa.RegisterUint},
		{name: "the float bank", kind: isa.RegisterFloat},
		{name: "the boolean bank", kind: isa.RegisterBool},
		{name: "the general bank", kind: isa.RegisterGeneral},
	}

	t.Run("reading a field", func(t *testing.T) {
		t.Parallel()
		seen := make(map[isa.Opcode]string, len(admitted))
		for _, bank := range admitted {
			op, ok := PickGetStructFieldTier0Op(bank.kind)

			require.Truef(t, ok, "%s must have a tier-zero field read", bank.name)
			require.Emptyf(t, seen[op], "%s reuses the opcode of %s", bank.name, seen[op])
			seen[op] = bank.name
		}

		_, ok := PickGetStructFieldTier0Op(isa.RegisterString)
		require.False(t, ok, "the string bank has no tier-zero field read")
	})

	t.Run("writing a field", func(t *testing.T) {
		t.Parallel()
		for _, bank := range admitted {
			_, ok := PickSetStructFieldTier0Op(bank.kind)
			require.Truef(t, ok, "%s must have a tier-zero field write", bank.name)
		}
	})
}

func TestUnsafeStructFieldSelectorsAgreeOnTheirAdmittedBanks(t *testing.T) {
	t.Parallel()

	banks := []isa.RegisterKind{
		isa.RegisterInt, isa.RegisterFloat, isa.RegisterString, isa.RegisterGeneral,
		isa.RegisterBool, isa.RegisterUint, isa.RegisterComplex,
		isa.RegisterSliceInt, isa.RegisterSliceFloat, isa.RegisterSliceString,
		isa.RegisterSliceBool, isa.RegisterSliceUint, isa.RegisterSliceByte,
	}

	for _, bank := range banks {
		_, readOK := PickGetStructFieldUnsafeSubOp(bank)
		_, writeOK := PickSetStructFieldUnsafeSubOp(bank)

		require.Equalf(t, readOK, writeOK,
			"bank %v must admit an unsafe read exactly when it admits an unsafe write", bank)
	}
}

func TestTypedDirectAppendCoversTheTypedBanksOnly(t *testing.T) {
	t.Parallel()

	typed := []isa.RegisterKind{
		isa.RegisterSliceInt, isa.RegisterSliceFloat, isa.RegisterSliceString,
		isa.RegisterSliceBool, isa.RegisterSliceUint, isa.RegisterSliceByte,
	}

	seen := make(map[isa.SubOpcode]bool, len(typed))
	for _, bank := range typed {
		subOp, ok := TypedDirectAppendSubOp(bank)

		require.Truef(t, ok, "bank %v must have a direct append", bank)
		require.Falsef(t, seen[subOp], "bank %v reuses a direct-append sub-opcode", bank)
		seen[subOp] = true
	}

	for _, scalar := range []isa.RegisterKind{isa.RegisterInt, isa.RegisterGeneral, isa.RegisterString} {
		_, ok := TypedDirectAppendSubOp(scalar)
		require.Falsef(t, ok, "scalar bank %v has no direct append", scalar)
	}
}
