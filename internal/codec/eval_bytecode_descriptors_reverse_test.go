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

package codec

import (
	"math"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/descriptor"

	"github.com/stretchr/testify/require"
)

func TestDescriptorToReflectTypeBasicKinds(t *testing.T) {
	t.Parallel()

	registry := symtab.NewSymbolRegistry(nil)

	tests := []struct {
		expected  reflect.Type
		name      string
		basicKind reflect.Kind
	}{
		{name: "Bool", basicKind: reflect.Bool, expected: reflect.TypeFor[bool]()},
		{name: "Int", basicKind: reflect.Int, expected: reflect.TypeFor[int]()},
		{name: "Int8", basicKind: reflect.Int8, expected: reflect.TypeFor[int8]()},
		{name: "Int16", basicKind: reflect.Int16, expected: reflect.TypeFor[int16]()},
		{name: "Int32", basicKind: reflect.Int32, expected: reflect.TypeFor[int32]()},
		{name: "Int64", basicKind: reflect.Int64, expected: reflect.TypeFor[int64]()},
		{name: "Uint", basicKind: reflect.Uint, expected: reflect.TypeFor[uint]()},
		{name: "Uint8", basicKind: reflect.Uint8, expected: reflect.TypeFor[uint8]()},
		{name: "Uint16", basicKind: reflect.Uint16, expected: reflect.TypeFor[uint16]()},
		{name: "Uint32", basicKind: reflect.Uint32, expected: reflect.TypeFor[uint32]()},
		{name: "Uint64", basicKind: reflect.Uint64, expected: reflect.TypeFor[uint64]()},
		{name: "Uintptr", basicKind: reflect.Uintptr, expected: reflect.TypeFor[uintptr]()},
		{name: "Float32", basicKind: reflect.Float32, expected: reflect.TypeFor[float32]()},
		{name: "Float64", basicKind: reflect.Float64, expected: reflect.TypeFor[float64]()},
		{name: "Complex64", basicKind: reflect.Complex64, expected: reflect.TypeFor[complex64]()},
		{name: "Complex128", basicKind: reflect.Complex128, expected: reflect.TypeFor[complex128]()},
		{name: "String", basicKind: reflect.String, expected: reflect.TypeFor[string]()},
		{name: "UnsafePointer", basicKind: reflect.UnsafePointer, expected: reflect.TypeFor[unsafe.Pointer]()},
		{name: "UnknownKind", basicKind: reflect.Kind(255), expected: reflect.TypeFor[any]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			typeDescriptor := descriptor.TypeDescriptor{
				Kind:      descriptor.KindBasic,
				BasicKind: uint8(tt.basicKind),
			}

			got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
			require.NoError(t, err)
			require.Equal(t, tt.expected, got)
		})
	}
}

func TestDescriptorToReflectTypeComposite(t *testing.T) {
	t.Parallel()

	registry := symtab.NewSymbolRegistry(nil)

	basicIntDesc := descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int)}
	basicStringDesc := descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.String)}
	basicBoolDesc := descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Bool)}

	t.Run("Pointer", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{
			Kind:    descriptor.KindPtr,
			Element: &basicIntDesc,
		}
		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, reflect.TypeFor[*int](), got)
	})

	t.Run("Slice", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{
			Kind:    descriptor.KindSlice,
			Element: &basicIntDesc,
		}
		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, reflect.TypeFor[[]int](), got)
	})

	t.Run("Array", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{
			Kind:    descriptor.KindArray,
			Element: &basicStringDesc,
			Length:  3,
		}
		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, reflect.TypeFor[[3]string](), got)
	})

	t.Run("Map", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{
			Kind:  descriptor.KindMap,
			Key:   &basicStringDesc,
			Value: &basicIntDesc,
		}
		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, reflect.TypeFor[map[string]int](), got)
	})

	t.Run("ChanBidirectional", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{
			Kind:    descriptor.KindChan,
			Element: &basicIntDesc,
			Dir:     int(reflect.BothDir),
		}
		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, reflect.TypeFor[chan int](), got)
	})

	t.Run("ChanSendOnly", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{
			Kind:    descriptor.KindChan,
			Element: &basicIntDesc,
			Dir:     int(reflect.SendDir),
		}
		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		expected := reflect.ChanOf(reflect.SendDir, reflect.TypeFor[int]())
		require.Equal(t, expected, got)
	})

	t.Run("ChanRecvOnly", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{
			Kind:    descriptor.KindChan,
			Element: &basicIntDesc,
			Dir:     int(reflect.RecvDir),
		}
		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		expected := reflect.ChanOf(reflect.RecvDir, reflect.TypeFor[int]())
		require.Equal(t, expected, got)
	})

	t.Run("FunctionNonVariadic", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{
			Kind:       descriptor.KindFunc,
			Params:     []descriptor.TypeDescriptor{basicIntDesc, basicStringDesc},
			Results:    []descriptor.TypeDescriptor{basicBoolDesc},
			IsVariadic: false,
		}
		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		expected := reflect.FuncOf(
			[]reflect.Type{reflect.TypeFor[int](), reflect.TypeFor[string]()},
			[]reflect.Type{reflect.TypeFor[bool]()},
			false,
		)
		require.Equal(t, expected, got)
	})

	t.Run("FunctionVariadic", func(t *testing.T) {
		t.Parallel()
		sliceOfStringDesc := descriptor.TypeDescriptor{
			Kind:    descriptor.KindSlice,
			Element: &basicStringDesc,
		}
		typeDescriptor := descriptor.TypeDescriptor{
			Kind:       descriptor.KindFunc,
			Params:     []descriptor.TypeDescriptor{basicIntDesc, sliceOfStringDesc},
			Results:    []descriptor.TypeDescriptor{basicBoolDesc},
			IsVariadic: true,
		}
		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		expected := reflect.FuncOf(
			[]reflect.Type{reflect.TypeFor[int](), reflect.TypeFor[[]string]()},
			[]reflect.Type{reflect.TypeFor[bool]()},
			true,
		)
		require.Equal(t, expected, got)
	})

	t.Run("Struct", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{
			Kind: descriptor.KindStruct,
			Fields: []descriptor.TypeDescriptorField{
				{Name: "X", Typ: basicIntDesc},
				{Name: "Y", Tag: `json:"y"`, Typ: basicStringDesc},
			},
		}
		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		expected := reflect.StructOf([]reflect.StructField{
			{Name: "X", Type: reflect.TypeFor[int]()},
			{Name: "Y", Type: reflect.TypeFor[string](), Tag: `json:"y"`},
		})
		require.Equal(t, expected, got)
	})

	t.Run("InterfaceEmpty", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{Kind: descriptor.KindInterface}
		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, reflect.TypeFor[any](), got)
	})

	t.Run("UnknownKindFallback", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{Kind: descriptor.TypeDescriptorKind(255)}
		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, reflect.TypeFor[any](), got)
	})

	t.Run("NilType", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{Kind: descriptor.KindNil}
		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("NilTypeNestedIsCorrupt", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{Kind: descriptor.KindPtr, Element: &descriptor.TypeDescriptor{Kind: descriptor.KindNil}}
		_, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.Error(t, err)
	})
}

func TestReflectTypeToDescriptorNil(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		typ  reflect.Type
		want descriptor.TypeDescriptorKind
	}{
		{name: "nil type", typ: nil, want: descriptor.KindNil},
		{name: "empty interface", typ: reflect.TypeFor[any](), want: descriptor.KindInterface},
		{name: "error interface", typ: reflect.TypeFor[error](), want: descriptor.KindInterface},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := descriptor.ReflectTypeToDescriptor(tc.typ)
			require.Equal(t, tc.want, got.Kind)
			back, err := symtab.ReflectTypeFor(got, symtab.NewSymbolRegistry(nil))
			require.NoError(t, err)
			if tc.typ == nil {
				require.Nil(t, back)
				return
			}
			require.Equal(t, reflect.TypeFor[any](), back)
		})
	}
}

func TestDescriptorToReflectTypeNamed(t *testing.T) {
	t.Parallel()

	t.Run("RegisteredNamedType", func(t *testing.T) {
		t.Parallel()

		registry := symtab.NewSymbolRegistry(symtab.SymbolExports{
			"time": {
				"Time": reflect.ValueOf((*time.Time)(nil)),
			},
		})

		typeDescriptor := descriptor.TypeDescriptor{
			Kind:        descriptor.KindNamed,
			PackagePath: "time",
			Name:        "Time",
		}

		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, reflect.TypeFor[time.Time](), got)
	})

	t.Run("UnregisteredNamedType", func(t *testing.T) {
		t.Parallel()

		registry := symtab.NewSymbolRegistry(nil)

		typeDescriptor := descriptor.TypeDescriptor{
			Kind:        descriptor.KindNamed,
			PackagePath: "nonexistent",
			Name:        "Missing",
		}

		_, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.Error(t, err)
		require.Contains(t, err.Error(), "nonexistent.Missing")
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("NamedTypeNonPointerValue", func(t *testing.T) {
		t.Parallel()

		registry := symtab.NewSymbolRegistry(symtab.SymbolExports{
			"math": {
				"Pi": reflect.ValueOf(math.Pi),
			},
		})

		typeDescriptor := descriptor.TypeDescriptor{
			Kind:        descriptor.KindNamed,
			PackagePath: "math",
			Name:        "Pi",
		}

		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, reflect.TypeFor[float64](), got)
	})
}

func TestDescriptorToReflectTypeNestedComposites(t *testing.T) {
	t.Parallel()

	registry := symtab.NewSymbolRegistry(nil)

	t.Run("SliceOfPointers", func(t *testing.T) {
		t.Parallel()

		basicIntDesc := descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int)}
		ptrDesc := descriptor.TypeDescriptor{Kind: descriptor.KindPtr, Element: &basicIntDesc}
		typeDescriptor := descriptor.TypeDescriptor{Kind: descriptor.KindSlice, Element: &ptrDesc}

		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, reflect.TypeFor[[]*int](), got)
	})

	t.Run("MapOfSlices", func(t *testing.T) {
		t.Parallel()

		basicStringDesc := descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.String)}
		basicIntDesc := descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int)}
		sliceDesc := descriptor.TypeDescriptor{Kind: descriptor.KindSlice, Element: &basicIntDesc}
		typeDescriptor := descriptor.TypeDescriptor{Kind: descriptor.KindMap, Key: &basicStringDesc, Value: &sliceDesc}

		got, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, reflect.TypeFor[map[string][]int](), got)
	})
}

func TestDescriptorToReflectTypeErrorPropagation(t *testing.T) {
	t.Parallel()

	registry := symtab.NewSymbolRegistry(nil)

	badNamed := descriptor.TypeDescriptor{
		Kind:        descriptor.KindNamed,
		PackagePath: "nonexistent",
		Name:        "Bad",
	}

	t.Run("PointerWithBadElement", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{Kind: descriptor.KindPtr, Element: &badNamed}
		_, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.Error(t, err)
		require.Contains(t, err.Error(), "nonexistent.Bad")
	})

	t.Run("SliceWithBadElement", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{Kind: descriptor.KindSlice, Element: &badNamed}
		_, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.Error(t, err)
	})

	t.Run("ArrayWithBadElement", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{Kind: descriptor.KindArray, Element: &badNamed, Length: 5}
		_, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.Error(t, err)
	})

	t.Run("MapWithBadKey", func(t *testing.T) {
		t.Parallel()
		basicIntDesc := descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int)}
		typeDescriptor := descriptor.TypeDescriptor{Kind: descriptor.KindMap, Key: &badNamed, Value: &basicIntDesc}
		_, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.Error(t, err)
	})

	t.Run("MapWithBadValue", func(t *testing.T) {
		t.Parallel()
		basicIntDesc := descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int)}
		typeDescriptor := descriptor.TypeDescriptor{Kind: descriptor.KindMap, Key: &basicIntDesc, Value: &badNamed}
		_, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.Error(t, err)
	})

	t.Run("ChanWithBadElement", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{Kind: descriptor.KindChan, Element: &badNamed, Dir: int(reflect.BothDir)}
		_, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.Error(t, err)
	})

	t.Run("FunctionWithBadParam", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{
			Kind:   descriptor.KindFunc,
			Params: []descriptor.TypeDescriptor{badNamed},
		}
		_, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.Error(t, err)
	})

	t.Run("FunctionWithBadResult", func(t *testing.T) {
		t.Parallel()
		basicIntDesc := descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int)}
		typeDescriptor := descriptor.TypeDescriptor{
			Kind:    descriptor.KindFunc,
			Params:  []descriptor.TypeDescriptor{basicIntDesc},
			Results: []descriptor.TypeDescriptor{badNamed},
		}
		_, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.Error(t, err)
	})

	t.Run("StructWithBadFieldType", func(t *testing.T) {
		t.Parallel()
		typeDescriptor := descriptor.TypeDescriptor{
			Kind: descriptor.KindStruct,
			Fields: []descriptor.TypeDescriptorField{
				{Name: "Bad", Typ: badNamed},
			},
		}
		_, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.Error(t, err)
	})
}

func TestReconstructGeneralConstant(t *testing.T) {
	t.Parallel()

	t.Run("PackageSymbolFound", func(t *testing.T) {
		t.Parallel()

		registry := symtab.NewSymbolRegistry(symtab.SymbolExports{
			"math": {
				"Pi": reflect.ValueOf(math.Pi),
			},
		})

		constDescriptor := descriptor.GeneralConstantDescriptor{
			Kind:        descriptor.GeneralConstantPackageSymbol,
			PackagePath: "math",
			SymbolName:  "Pi",
		}

		got, err := symtab.ReconstructConstant(constDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, math.Pi, got.Float())
	})

	t.Run("PackageSymbolNotFound", func(t *testing.T) {
		t.Parallel()

		registry := symtab.NewSymbolRegistry(nil)

		constDescriptor := descriptor.GeneralConstantDescriptor{
			Kind:        descriptor.GeneralConstantPackageSymbol,
			PackagePath: "nonexistent",
			SymbolName:  "Foo",
		}

		_, err := symtab.ReconstructConstant(constDescriptor, registry)
		require.Error(t, err)
		require.Contains(t, err.Error(), "nonexistent.Foo")
		require.Contains(t, err.Error(), "not found")
	})

	t.Run("NamedTypeZeroFound", func(t *testing.T) {
		t.Parallel()

		registry := symtab.NewSymbolRegistry(symtab.SymbolExports{
			"time": {
				"Duration": reflect.ValueOf((*time.Duration)(nil)),
			},
		})

		constDescriptor := descriptor.GeneralConstantDescriptor{
			Kind:        descriptor.GeneralConstantNamedTypeZero,
			PackagePath: "time",
			SymbolName:  "Duration",
		}

		got, err := symtab.ReconstructConstant(constDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, reflect.TypeFor[time.Duration](), got.Type())
		require.True(t, got.IsZero())
	})

	t.Run("NamedTypeZeroNotFound", func(t *testing.T) {
		t.Parallel()

		registry := symtab.NewSymbolRegistry(nil)

		constDescriptor := descriptor.GeneralConstantDescriptor{
			Kind:        descriptor.GeneralConstantNamedTypeZero,
			PackagePath: "nonexistent",
			SymbolName:  "Missing",
		}

		_, err := symtab.ReconstructConstant(constDescriptor, registry)
		require.Error(t, err)
		require.Contains(t, err.Error(), "nonexistent.Missing")
	})

	t.Run("CompositeZero", func(t *testing.T) {
		t.Parallel()

		registry := symtab.NewSymbolRegistry(nil)

		basicIntDesc := descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int)}
		basicStringDesc := descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.String)}

		structDesc := descriptor.TypeDescriptor{
			Kind: descriptor.KindStruct,
			Fields: []descriptor.TypeDescriptorField{
				{Name: "X", Typ: basicIntDesc},
				{Name: "Y", Typ: basicStringDesc},
			},
		}

		constDescriptor := descriptor.GeneralConstantDescriptor{
			Kind:           descriptor.GeneralConstantCompositeZero,
			TypeDescriptor: structDesc,
		}

		got, err := symtab.ReconstructConstant(constDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, reflect.Struct, got.Kind())
		require.Equal(t, 2, got.NumField())
		require.True(t, got.IsZero())
	})

	t.Run("CompositeZeroWithBadType", func(t *testing.T) {
		t.Parallel()

		registry := symtab.NewSymbolRegistry(nil)

		constDescriptor := descriptor.GeneralConstantDescriptor{
			Kind: descriptor.GeneralConstantCompositeZero,
			TypeDescriptor: descriptor.TypeDescriptor{
				Kind:        descriptor.KindNamed,
				PackagePath: "nonexistent",
				Name:        "Bad",
			},
		}

		_, err := symtab.ReconstructConstant(constDescriptor, registry)
		require.Error(t, err)
		require.Contains(t, err.Error(), "reconstructing composite type")
	})

	t.Run("UnknownKind", func(t *testing.T) {
		t.Parallel()

		registry := symtab.NewSymbolRegistry(nil)

		constDescriptor := descriptor.GeneralConstantDescriptor{
			Kind: descriptor.GeneralConstantKind(255),
		}

		_, err := symtab.ReconstructConstant(constDescriptor, registry)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unknown general constant kind")
	})
}

func TestReconstructGeneralConstantExportedAPI(t *testing.T) {
	t.Parallel()

	registry := symtab.NewSymbolRegistry(symtab.SymbolExports{
		"math": {
			"Pi": reflect.ValueOf(math.Pi),
		},
	})

	data := GeneralConstantDescriptorData{
		PackagePath: "math",
		SymbolName:  "Pi",
		Kind:        uint8(descriptor.GeneralConstantPackageSymbol),
	}

	got, err := ReconstructGeneralConstant(data, registry)
	require.NoError(t, err)
	require.Equal(t, math.Pi, got.Float())
}

func TestDescriptorToReflectTypeExportedAPI(t *testing.T) {
	t.Parallel()

	registry := symtab.NewSymbolRegistry(nil)

	data := descriptor.TypeDescriptorData{
		Kind:      uint8(descriptor.KindBasic),
		BasicKind: uint8(reflect.Int),
	}

	got, err := DescriptorToReflectType(data, registry)
	require.NoError(t, err)
	require.Equal(t, reflect.TypeFor[int](), got)
}

func TestDescriptorRoundTrip(t *testing.T) {
	t.Parallel()

	registry := symtab.NewSymbolRegistry(nil)

	tests := []struct {
		goType reflect.Type
		name   string
	}{
		{name: "Int", goType: reflect.TypeFor[int]()},
		{name: "String", goType: reflect.TypeFor[string]()},
		{name: "Bool", goType: reflect.TypeFor[bool]()},
		{name: "PointerToInt", goType: reflect.TypeFor[*int]()},
		{name: "SliceOfString", goType: reflect.TypeFor[[]string]()},
		{name: "ArrayOfInt", goType: reflect.TypeFor[[3]int]()},
		{name: "MapStringInt", goType: reflect.TypeFor[map[string]int]()},
		{name: "ChanInt", goType: reflect.TypeFor[chan int]()},
		{name: "FuncIntToString", goType: reflect.FuncOf(
			[]reflect.Type{reflect.TypeFor[int]()},
			[]reflect.Type{reflect.TypeFor[string]()},
			false,
		)},
		{name: "EmptyStruct", goType: reflect.TypeFor[struct{}]()},
		{name: "Float64", goType: reflect.TypeFor[float64]()},
		{name: "Complex128", goType: reflect.TypeFor[complex128]()},
		{name: "SliceOfSlice", goType: reflect.TypeFor[[][]int]()},
		{name: "PointerToSlice", goType: reflect.TypeFor[*[]string]()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			typeDescriptor := descriptor.ReflectTypeToDescriptor(tt.goType)

			reconstructed, err := symtab.ReflectTypeFor(typeDescriptor, registry)
			require.NoError(t, err)
			require.Equal(t, tt.goType, reconstructed,
				"round-trip failed for %s: expected %v, got %v", tt.name, tt.goType, reconstructed)
		})
	}
}

func TestDescriptorRoundTripStruct(t *testing.T) {
	t.Parallel()

	registry := symtab.NewSymbolRegistry(nil)

	original := reflect.StructOf([]reflect.StructField{
		{Name: "Name", Type: reflect.TypeFor[string](), Tag: `json:"name"`},
		{Name: "Age", Type: reflect.TypeFor[int]()},
		{Name: "Active", Type: reflect.TypeFor[bool](), Tag: `db:"active"`},
	})

	typeDescriptor := descriptor.ReflectTypeToDescriptor(original)
	reconstructed, err := symtab.ReflectTypeFor(typeDescriptor, registry)
	require.NoError(t, err)
	require.Equal(t, original.NumField(), reconstructed.NumField())

	for i := range original.NumField() {
		originalField := original.Field(i)
		reconstructedField := reconstructed.Field(i)
		require.Equal(t, originalField.Name, reconstructedField.Name)
		require.Equal(t, originalField.Type, reconstructedField.Type)
		require.Equal(t, originalField.Tag, reconstructedField.Tag)
	}
}

func TestDescriptorRoundTripFunc(t *testing.T) {
	t.Parallel()

	registry := symtab.NewSymbolRegistry(nil)

	t.Run("NonVariadic", func(t *testing.T) {
		t.Parallel()
		original := reflect.FuncOf(
			[]reflect.Type{reflect.TypeFor[int](), reflect.TypeFor[string]()},
			[]reflect.Type{reflect.TypeFor[bool]()},
			false,
		)
		typeDescriptor := descriptor.ReflectTypeToDescriptor(original)
		reconstructed, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, original.Kind(), reconstructed.Kind())
		require.Equal(t, original.NumIn(), reconstructed.NumIn())
		require.Equal(t, original.NumOut(), reconstructed.NumOut())
		require.False(t, reconstructed.IsVariadic())
		for i := range original.NumIn() {
			require.Equal(t, original.In(i), reconstructed.In(i))
		}
		for i := range original.NumOut() {
			require.Equal(t, original.Out(i), reconstructed.Out(i))
		}
	})

	t.Run("Variadic", func(t *testing.T) {
		t.Parallel()
		original := reflect.FuncOf(
			[]reflect.Type{reflect.TypeFor[string](), reflect.TypeFor[[]any]()},
			[]reflect.Type{reflect.TypeFor[int]()},
			true,
		)
		typeDescriptor := descriptor.ReflectTypeToDescriptor(original)
		reconstructed, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, original.Kind(), reconstructed.Kind())
		require.Equal(t, original.NumIn(), reconstructed.NumIn())
		require.Equal(t, original.NumOut(), reconstructed.NumOut())
		require.True(t, reconstructed.IsVariadic())
	})

	t.Run("NoParamsNoResults", func(t *testing.T) {
		t.Parallel()
		original := reflect.FuncOf(nil, nil, false)
		typeDescriptor := descriptor.ReflectTypeToDescriptor(original)
		reconstructed, err := symtab.ReflectTypeFor(typeDescriptor, registry)
		require.NoError(t, err)
		require.Equal(t, reflect.Func, reconstructed.Kind())
		require.Equal(t, 0, reconstructed.NumIn())
		require.Equal(t, 0, reconstructed.NumOut())
	})
}

func TestBasicKindToReflectDirect(t *testing.T) {
	t.Parallel()

	require.Equal(t, reflect.TypeFor[bool](), descriptor.BasicKindToReflect(reflect.Bool))
	require.Equal(t, reflect.TypeFor[int](), descriptor.BasicKindToReflect(reflect.Int))
	require.Equal(t, reflect.TypeFor[string](), descriptor.BasicKindToReflect(reflect.String))
	require.Equal(t, reflect.TypeFor[unsafe.Pointer](), descriptor.BasicKindToReflect(reflect.UnsafePointer))

	require.Equal(t, reflect.TypeFor[any](), descriptor.BasicKindToReflect(reflect.Kind(200)))
}

func TestImportExportTypeDescriptorRoundTrip(t *testing.T) {
	t.Parallel()

	registry := symtab.NewSymbolRegistry(nil)

	original := reflect.FuncOf(
		[]reflect.Type{
			reflect.TypeFor[map[string][]int](),
			reflect.TypeFor[*bool](),
		},
		[]reflect.Type{
			reflect.ChanOf(reflect.BothDir, reflect.TypeFor[float64]()),
		},
		false,
	)

	internalDesc := descriptor.ReflectTypeToDescriptor(original)

	exported := ExportTypeDescriptor(internalDesc)

	imported := ImportTypeDescriptor(exported)

	reconstructed, err := symtab.ReflectTypeFor(imported, registry)
	require.NoError(t, err)

	require.Equal(t, original.Kind(), reconstructed.Kind())
	require.Equal(t, original.NumIn(), reconstructed.NumIn())
	require.Equal(t, original.NumOut(), reconstructed.NumOut())
	for i := range original.NumIn() {
		require.Equal(t, original.In(i), reconstructed.In(i))
	}
	for i := range original.NumOut() {
		require.Equal(t, original.Out(i), reconstructed.Out(i))
	}
}
