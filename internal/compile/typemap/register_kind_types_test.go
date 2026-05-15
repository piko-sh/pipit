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

package typemap

import (
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func namedOf(t *testing.T, name string, underlying types.Type) types.Type {
	t.Helper()
	pkg := types.NewPackage("example.com/main", "main")
	object := types.NewTypeName(0, pkg, name, nil)
	return types.NewNamed(object, underlying, nil)
}

func TestKindForBasicRoutesEveryBasicKindToItsBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind types.BasicKind
		want isa.RegisterKind
	}{
		{name: "a bool", kind: types.Bool, want: isa.RegisterBool},
		{name: "an untyped bool", kind: types.UntypedBool, want: isa.RegisterBool},
		{name: "an int", kind: types.Int, want: isa.RegisterInt},
		{name: "an int8", kind: types.Int8, want: isa.RegisterInt},
		{name: "an int16", kind: types.Int16, want: isa.RegisterInt},
		{name: "an int32", kind: types.Int32, want: isa.RegisterInt},
		{name: "an int64", kind: types.Int64, want: isa.RegisterInt},
		{name: "an untyped int", kind: types.UntypedInt, want: isa.RegisterInt},
		{name: "an untyped rune", kind: types.UntypedRune, want: isa.RegisterInt},
		{name: "a uint", kind: types.Uint, want: isa.RegisterUint},
		{name: "a uint8", kind: types.Uint8, want: isa.RegisterUint},
		{name: "a uint16", kind: types.Uint16, want: isa.RegisterUint},
		{name: "a uint32", kind: types.Uint32, want: isa.RegisterUint},
		{name: "a uint64", kind: types.Uint64, want: isa.RegisterUint},
		{name: "a uintptr", kind: types.Uintptr, want: isa.RegisterUint},
		{name: "a float32", kind: types.Float32, want: isa.RegisterFloat},
		{name: "a float64", kind: types.Float64, want: isa.RegisterFloat},
		{name: "an untyped float", kind: types.UntypedFloat, want: isa.RegisterFloat},
		{name: "a string", kind: types.String, want: isa.RegisterString},
		{name: "an untyped string", kind: types.UntypedString, want: isa.RegisterString},
		{name: "a complex64", kind: types.Complex64, want: isa.RegisterComplex},
		{name: "a complex128", kind: types.Complex128, want: isa.RegisterComplex},
		{name: "an untyped complex", kind: types.UntypedComplex, want: isa.RegisterComplex},
		{name: "an untyped nil", kind: types.UntypedNil, want: isa.RegisterGeneral},
		{name: "an unsafe pointer", kind: types.UnsafePointer, want: isa.RegisterGeneral},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, KindForBasic(tt.kind))
		})
	}
}

func TestKindForTypeLooksThroughToTheUnderlyingBasic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		build func(*testing.T) types.Type
		want  isa.RegisterKind
	}{
		{name: "a plain int", build: func(*testing.T) types.Type { return types.Typ[types.Int] }, want: isa.RegisterInt},
		{name: "a named integer", build: func(t *testing.T) types.Type { return namedOf(t, "counter", types.Typ[types.Int]) }, want: isa.RegisterInt},
		{name: "a named string", build: func(t *testing.T) types.Type { return namedOf(t, "label", types.Typ[types.String]) }, want: isa.RegisterString},
		{name: "a slice is boxed", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.Int]) }, want: isa.RegisterGeneral},
		{name: "a map is boxed", build: func(*testing.T) types.Type { return types.NewMap(types.Typ[types.Int], types.Typ[types.Int]) }, want: isa.RegisterGeneral},
		{name: "a pointer is boxed", build: func(*testing.T) types.Type { return types.NewPointer(types.Typ[types.Int]) }, want: isa.RegisterGeneral},
		{name: "a channel is boxed", build: func(*testing.T) types.Type { return types.NewChan(types.SendRecv, types.Typ[types.Int]) }, want: isa.RegisterGeneral},
		{name: "an empty struct is boxed", build: func(*testing.T) types.Type { return types.NewStruct(nil, nil) }, want: isa.RegisterGeneral},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, KindForType(tt.build(t)),
				"only a basic underlying type has a scalar bank; everything else is boxed")
		})
	}
}

func TestKindForCallSlotPromotesASliceOfACanonicalWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		build func(*testing.T) types.Type
		want  isa.RegisterKind
	}{
		{name: "a slice of int", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.Int]) }, want: isa.RegisterSliceInt},
		{name: "a slice of int64", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.Int64]) }, want: isa.RegisterSliceInt},
		{name: "a slice of float64", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.Float64]) }, want: isa.RegisterSliceFloat},
		{name: "a slice of string", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.String]) }, want: isa.RegisterSliceString},
		{name: "a slice of bool", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.Bool]) }, want: isa.RegisterSliceBool},
		{name: "a slice of uint", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.Uint]) }, want: isa.RegisterSliceUint},
		{name: "a slice of uint64", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.Uint64]) }, want: isa.RegisterSliceUint},
		{name: "a slice of byte", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.Uint8]) }, want: isa.RegisterSliceByte},
		{name: "a slice of float32 is boxed", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.Float32]) }, want: isa.RegisterGeneral},
		{name: "a slice of int32 is boxed", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.Int32]) }, want: isa.RegisterGeneral},
		{name: "a slice of a non-basic element is boxed", build: func(*testing.T) types.Type { return types.NewSlice(types.NewSlice(types.Typ[types.Int])) }, want: isa.RegisterGeneral},
		{name: "a named slice keeps its own name and is not promoted", build: func(t *testing.T) types.Type { return namedOf(t, "row", types.NewSlice(types.Typ[types.Int])) }, want: isa.RegisterGeneral},
		{name: "a value that is not a slice falls through to its bank", build: func(*testing.T) types.Type { return types.Typ[types.Int] }, want: isa.RegisterInt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, KindForCallSlot(tt.build(t)))
		})
	}
}

func TestKindForTypedSliceLooksThroughANameThatCallSlotsDoNot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		build func(*testing.T) types.Type
		want  isa.RegisterKind
	}{
		{name: "a slice of int", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.Int]) }, want: isa.RegisterSliceInt},
		{name: "a named slice of int", build: func(t *testing.T) types.Type { return namedOf(t, "row", types.NewSlice(types.Typ[types.Int])) }, want: isa.RegisterSliceInt},
		{name: "a named slice of byte", build: func(t *testing.T) types.Type { return namedOf(t, "blob", types.NewSlice(types.Typ[types.Uint8])) }, want: isa.RegisterSliceByte},
		{name: "a slice of a named byte still reads as a byte slice", build: func(t *testing.T) types.Type { return types.NewSlice(namedOf(t, "octet", types.Typ[types.Uint8])) }, want: isa.RegisterSliceByte},
		{name: "a slice of a named integer is boxed because the element is not a bare basic", build: func(t *testing.T) types.Type { return types.NewSlice(namedOf(t, "counter", types.Typ[types.Int])) }, want: isa.RegisterGeneral},
		{name: "a slice of a narrow element is boxed", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.Int16]) }, want: isa.RegisterGeneral},
		{name: "a value that is not a slice is boxed", build: func(*testing.T) types.Type { return types.Typ[types.Int] }, want: isa.RegisterGeneral},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, KindForTypedSlice(tt.build(t)))
		})
	}
}

func TestNarrowIntegerBitWidthReportsOnlyTheSubWordWidths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		build func(*testing.T) types.Type
		want  uint8
	}{
		{name: "an int8", build: func(*testing.T) types.Type { return types.Typ[types.Int8] }, want: isa.NarrowIntegerBitWidth8},
		{name: "a uint8", build: func(*testing.T) types.Type { return types.Typ[types.Uint8] }, want: isa.NarrowIntegerBitWidth8},
		{name: "an int16", build: func(*testing.T) types.Type { return types.Typ[types.Int16] }, want: isa.NarrowIntegerBitWidth16},
		{name: "a uint16", build: func(*testing.T) types.Type { return types.Typ[types.Uint16] }, want: isa.NarrowIntegerBitWidth16},
		{name: "an int32", build: func(*testing.T) types.Type { return types.Typ[types.Int32] }, want: isa.NarrowIntegerBitWidth32},
		{name: "a uint32", build: func(*testing.T) types.Type { return types.Typ[types.Uint32] }, want: isa.NarrowIntegerBitWidth32},
		{name: "a named int8 is still narrow", build: func(t *testing.T) types.Type { return namedOf(t, "small", types.Typ[types.Int8]) }, want: isa.NarrowIntegerBitWidth8},
		{name: "an int is a whole word", build: func(*testing.T) types.Type { return types.Typ[types.Int] }, want: 0},
		{name: "an int64 is a whole word", build: func(*testing.T) types.Type { return types.Typ[types.Int64] }, want: 0},
		{name: "a uint64 is a whole word", build: func(*testing.T) types.Type { return types.Typ[types.Uint64] }, want: 0},
		{name: "a float is not an integer at all", build: func(*testing.T) types.Type { return types.Typ[types.Float64] }, want: 0},
		{name: "a slice is not a basic type", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.Int8]) }, want: 0},
		{name: "no type at all", build: func(*testing.T) types.Type { return nil }, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, NarrowIntegerBitWidth(tt.build(t)),
				"a non-zero width is what makes the compiler emit a truncation after an arithmetic op")
		})
	}
}

func TestKindForPromotedSlotRefusesWhatTheCalleeCannotTake(t *testing.T) {
	t.Parallel()

	sliceOfInt := types.NewSlice(types.Typ[types.Int])

	tests := []struct {
		name      string
		slotType  types.Type
		context   *KindPromotionContext
		want      isa.RegisterKind
		wantTyped bool
	}{
		{name: "no context at all", slotType: sliceOfInt, context: nil, want: isa.RegisterSliceInt, wantTyped: true},
		{name: "an empty context", slotType: sliceOfInt, context: &KindPromotionContext{}, want: isa.RegisterSliceInt, wantTyped: true},
		{name: "a slot that has no typed bank", slotType: types.Typ[types.Int], context: &KindPromotionContext{}, want: isa.RegisterInt, wantTyped: false},
		{
			name:      "a binding the caller has disqualified",
			slotType:  sliceOfInt,
			context:   &KindPromotionContext{BindingName: "xs", Disqualified: map[string]bool{"xs": true}},
			want:      isa.RegisterGeneral,
			wantTyped: false,
		},
		{
			name:      "a binding that is not on the disqualified list",
			slotType:  sliceOfInt,
			context:   &KindPromotionContext{BindingName: "ys", Disqualified: map[string]bool{"xs": true}},
			want:      isa.RegisterSliceInt,
			wantTyped: true,
		},
		{
			name:      "a callee parameter that refuses promotion",
			slotType:  sliceOfInt,
			context:   &KindPromotionContext{CalleeParamPromotions: []bool{false}, CalleeParamIndex: 0},
			want:      isa.RegisterGeneral,
			wantTyped: false,
		},
		{
			name:      "a callee parameter that accepts promotion",
			slotType:  sliceOfInt,
			context:   &KindPromotionContext{CalleeParamPromotions: []bool{true}, CalleeParamIndex: 0},
			want:      isa.RegisterSliceInt,
			wantTyped: true,
		},
		{
			name:      "a parameter index past the callee's list",
			slotType:  sliceOfInt,
			context:   &KindPromotionContext{CalleeParamPromotions: []bool{true}, CalleeParamIndex: 7},
			want:      isa.RegisterSliceInt,
			wantTyped: true,
		},
		{
			name:      "a negative parameter index",
			slotType:  sliceOfInt,
			context:   &KindPromotionContext{CalleeParamPromotions: []bool{true}, CalleeParamIndex: -1},
			want:      isa.RegisterSliceInt,
			wantTyped: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, typed := KindForPromotedSlot(tt.slotType, tt.context)

			require.Equal(t, tt.want, got)
			require.Equal(t, tt.wantTyped, typed,
				"only a slot that stays in a typed bank may be reported as promoted")
		})
	}
}

func TestIsTypeParameterLooksThroughNamesAndAliases(t *testing.T) {
	t.Parallel()

	typeParameter := types.NewTypeParam(types.NewTypeName(0, nil, "T", nil), types.NewInterfaceType(nil, nil))

	tests := []struct {
		name  string
		build func(*testing.T) types.Type
		want  bool
	}{
		{name: "a bare type parameter", build: func(*testing.T) types.Type { return typeParameter }, want: true},
		{name: "a plain int", build: func(*testing.T) types.Type { return types.Typ[types.Int] }, want: false},
		{name: "no type at all", build: func(*testing.T) types.Type { return nil }, want: false},
		{name: "a slice of a type parameter is not itself one", build: func(*testing.T) types.Type { return types.NewSlice(typeParameter) }, want: false},
		{name: "a named type over an int", build: func(t *testing.T) types.Type { return namedOf(t, "counter", types.Typ[types.Int]) }, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, IsTypeParameter(tt.build(t)))
		})
	}
}

func TestContainsTypeParameterWalksIntoEveryComposite(t *testing.T) {
	t.Parallel()

	typeParameter := types.NewTypeParam(types.NewTypeName(0, nil, "T", nil), types.NewInterfaceType(nil, nil))
	pkg := types.NewPackage("example.com/main", "main")

	tests := []struct {
		name  string
		build func(*testing.T) types.Type
		want  bool
	}{
		{name: "a bare type parameter", build: func(*testing.T) types.Type { return typeParameter }, want: true},
		{name: "a slice of a type parameter", build: func(*testing.T) types.Type { return types.NewSlice(typeParameter) }, want: true},
		{name: "an array of a type parameter", build: func(*testing.T) types.Type { return types.NewArray(typeParameter, 3) }, want: true},
		{name: "a map keyed by a type parameter", build: func(*testing.T) types.Type { return types.NewMap(typeParameter, types.Typ[types.Int]) }, want: true},
		{name: "a map valued by a type parameter", build: func(*testing.T) types.Type { return types.NewMap(types.Typ[types.Int], typeParameter) }, want: true},
		{name: "a channel of a type parameter", build: func(*testing.T) types.Type { return types.NewChan(types.SendRecv, typeParameter) }, want: true},
		{name: "a pointer to a type parameter", build: func(*testing.T) types.Type { return types.NewPointer(typeParameter) }, want: true},
		{
			name: "a struct with a field of a type parameter",
			build: func(*testing.T) types.Type {
				return types.NewStruct([]*types.Var{types.NewField(0, pkg, "X", typeParameter, false)}, nil)
			},
			want: true,
		},
		{
			name: "a signature taking a type parameter",
			build: func(*testing.T) types.Type {
				params := types.NewTuple(types.NewParam(0, pkg, "v", typeParameter))
				return types.NewSignatureType(nil, nil, nil, params, nil, false)
			},
			want: true,
		},
		{
			name: "a signature returning a type parameter",
			build: func(*testing.T) types.Type {
				results := types.NewTuple(types.NewParam(0, pkg, "", typeParameter))
				return types.NewSignatureType(nil, nil, nil, nil, results, false)
			},
			want: true,
		},
		{name: "a slice of a concrete element", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.Int]) }, want: false},
		{name: "a fully concrete map", build: func(*testing.T) types.Type { return types.NewMap(types.Typ[types.String], types.Typ[types.Int]) }, want: false},
		{
			name: "a struct whose second field is abstract",
			build: func(*testing.T) types.Type {
				return types.NewStruct([]*types.Var{
					types.NewField(0, pkg, "N", types.Typ[types.Int], false),
					types.NewField(0, pkg, "V", typeParameter, false),
				}, nil)
			},
			want: true,
		},
		{
			name: "a named type over a struct with an abstract field",
			build: func(t *testing.T) types.Type {
				return namedOf(t, "holder", types.NewStruct([]*types.Var{types.NewField(0, pkg, "V", typeParameter, false)}, nil))
			},
			want: true,
		},
		{
			name: "a slice of a named type over an abstract struct",
			build: func(t *testing.T) types.Type {
				return types.NewSlice(namedOf(t, "holder", types.NewStruct([]*types.Var{types.NewField(0, pkg, "V", typeParameter, false)}, nil)))
			},
			want: true,
		},
		{
			name: "an alias of a slice of a type parameter",
			build: func(*testing.T) types.Type {
				return types.NewAlias(types.NewTypeName(0, pkg, "row", nil), types.NewSlice(typeParameter))
			},
			want: true,
		},
		{
			name: "a self-referential struct that also holds a type parameter",
			build: func(t *testing.T) types.Type {
				named, ok := namedOf(t, "node", nil).(*types.Named)
				require.True(t, ok)
				named.SetUnderlying(types.NewStruct([]*types.Var{
					types.NewField(0, pkg, "Next", types.NewPointer(named), false),
					types.NewField(0, pkg, "V", typeParameter, false),
				}, nil))
				return named
			},
			want: true,
		},
		{
			name: "a concrete struct",
			build: func(*testing.T) types.Type {
				return types.NewStruct([]*types.Var{types.NewField(0, pkg, "N", types.Typ[types.Int], false)}, nil)
			},
			want: false,
		},
		{
			name: "a named type over a concrete struct",
			build: func(t *testing.T) types.Type {
				return namedOf(t, "point", types.NewStruct([]*types.Var{types.NewField(0, pkg, "X", types.Typ[types.Int], false)}, nil))
			},
			want: false,
		},
		{
			name: "a self-referential struct terminates without a parameter",
			build: func(t *testing.T) types.Type {
				named, ok := namedOf(t, "node", nil).(*types.Named)
				require.True(t, ok)
				named.SetUnderlying(types.NewStruct([]*types.Var{types.NewField(0, pkg, "Next", types.NewPointer(named), false)}, nil))
				return named
			},
			want: false,
		},
		{name: "no type at all", build: func(*testing.T) types.Type { return nil }, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, ContainsTypeParameter(tt.build(t)),
				"a type still carrying a parameter cannot be lowered to a register bank yet")
		})
	}
}

func TestCanonicalTypeStripsAliasesAndRebuildsComposites(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		build func(*testing.T) types.Type
		want  string
	}{
		{name: "a plain int", build: func(*testing.T) types.Type { return types.Typ[types.Int] }, want: "int"},
		{name: "a byte canonicalises to the uint8 it aliases", build: func(*testing.T) types.Type { return types.Typ[types.Byte] }, want: "uint8"},
		{name: "a slice of int", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.Int]) }, want: "[]int"},
		{name: "a map of string to int", build: func(*testing.T) types.Type { return types.NewMap(types.Typ[types.String], types.Typ[types.Int]) }, want: "map[string]int"},
		{name: "a pointer to int", build: func(*testing.T) types.Type { return types.NewPointer(types.Typ[types.Int]) }, want: "*int"},
		{name: "an array of int", build: func(*testing.T) types.Type { return types.NewArray(types.Typ[types.Int], 4) }, want: "[4]int"},
		{name: "a channel of int", build: func(*testing.T) types.Type { return types.NewChan(types.SendRecv, types.Typ[types.Int]) }, want: "chan int"},
		{name: "a named type keeps its name", build: func(t *testing.T) types.Type { return namedOf(t, "counter", types.Typ[types.Int]) }, want: "main.counter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := CanonicalType(tt.build(t))

			require.NotNil(t, got)
			require.Equal(t, tt.want, types.TypeString(got, func(p *types.Package) string { return p.Name() }))
		})
	}
}

func TestCanonicalTypeOfNothingIsNothing(t *testing.T) {
	t.Parallel()

	require.Nil(t, CanonicalType(nil),
		"a caller with no type has nothing to canonicalise, and must not be handed a substitute")
}

func TestRuntimeTypeStringRendersWhatTheEngineStoresAsATypeName(t *testing.T) {
	t.Parallel()

	pkg := types.NewPackage("example.com/main", "main")

	tests := []struct {
		name  string
		build func(*testing.T) types.Type
		want  string
	}{
		{name: "a plain int", build: func(*testing.T) types.Type { return types.Typ[types.Int] }, want: "int"},
		{name: "a slice of string", build: func(*testing.T) types.Type { return types.NewSlice(types.Typ[types.String]) }, want: "[]string"},
		{name: "a map of string to int", build: func(*testing.T) types.Type { return types.NewMap(types.Typ[types.String], types.Typ[types.Int]) }, want: "map[string]int"},
		{name: "a named type is qualified by its package name", build: func(t *testing.T) types.Type { return namedOf(t, "counter", types.Typ[types.Int]) }, want: "main.counter"},
		{
			name: "a function type drops its parameter names",
			build: func(*testing.T) types.Type {
				params := types.NewTuple(types.NewParam(0, pkg, "value", types.Typ[types.Int]))
				results := types.NewTuple(types.NewParam(0, pkg, "answer", types.Typ[types.Bool]))
				return types.NewSignatureType(nil, nil, nil, params, results, false)
			},
			want: "func(int) bool",
		},
		{
			name: "a variadic function keeps its ellipsis",
			build: func(*testing.T) types.Type {
				params := types.NewTuple(types.NewParam(0, pkg, "values", types.NewSlice(types.Typ[types.Int])))
				return types.NewSignatureType(nil, nil, nil, params, nil, true)
			},
			want: "func(...int)",
		},
		{
			name: "a struct renders the way reflect prints one",
			build: func(*testing.T) types.Type {
				return types.NewStruct([]*types.Var{types.NewField(0, pkg, "X", types.Typ[types.Int], false)}, nil)
			},
			want: "struct { X int }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, RuntimeTypeString(tt.build(t)))
		})
	}
}
