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

package adapters

import (
	"reflect"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/codec"
	"pipit.sh/pipit/internal/schema/schemagen"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

func TestBytecodeZeroValueBudgetBeforeAllocation(t *testing.T) {
	t.Parallel()
	t.Run("array zero values are charged before allocation", func(t *testing.T) {
		t.Parallel()
		budget := bytecodeValueBudget{remaining: 16}
		first, err := budget.zero(reflect.TypeFor[[1]uint64]())
		require.NoError(t, err)
		require.True(t, first.CanSet())
		require.True(t, first.IsZero())
		second, err := budget.zero(reflect.TypeFor[[1]uint64]())
		require.NoError(t, err)
		require.Equal(t, uintptr(0), budget.remaining)
		first.Index(0).SetUint(9)
		require.True(t, second.IsZero(), "zero values do not share mutable storage")
		value, err := budget.zero(reflect.TypeFor[[1]uint64]())
		require.False(t, value.IsValid(), "an exhausted budget neither allocates nor wraps")
		require.ErrorIs(t, err, errCorruptBytecodePayload)
		require.Equal(t, uintptr(0), budget.remaining)
	})
	t.Run("a slice zero value is the non-addressable nil", func(t *testing.T) {
		t.Parallel()
		budget := bytecodeValueBudget{remaining: 24}
		slice, err := budget.zero(reflect.TypeFor[[]int]())
		require.NoError(t, err)
		require.False(t, slice.CanSet(), "no in-place append can grow a shared slice zero value")
		require.True(t, slice.IsNil())
		require.Equal(t, uintptr(0), budget.remaining)
	})
	t.Run("an empty struct still costs one unit", func(t *testing.T) {
		t.Parallel()
		budget := bytecodeValueBudget{remaining: 1}
		_, err := budget.zero(reflect.TypeFor[struct{}]())
		require.NoError(t, err)
		require.Equal(t, uintptr(0), budget.remaining)
		value, err := budget.zero(reflect.TypeFor[struct{}]())
		require.False(t, value.IsValid())
		require.ErrorIs(t, err, errCorruptBytecodePayload)
	})
}

func TestBytecodeNamedZeroCannotBypassBudget(t *testing.T) {
	t.Parallel()
	type large [maximumDecodedValueBytes + 1]byte
	registry := symtab.NewSymbolRegistry(symtab.SymbolExports{"host": {
		"Large":   reflect.ValueOf((*large)(nil)),
		"NotType": reflect.ValueOf(1),
		"NonNil":  reflect.ValueOf(new(int)),
	}})
	for _, name := range []string{"Large", "NotType", "NonNil", "Missing"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			budget := bytecodeValueBudget{remaining: maximumDecodedValueBytes}
			data := codec.GeneralConstantDescriptorData{
				Kind: uint8(descriptor.GeneralConstantNamedTypeZero), PackagePath: "host", SymbolName: name,
			}
			value, err := budget.reconstruct(data, registry)
			require.Error(t, err, "a named zero value needs valid authority and budget")
			require.False(t, value.IsValid())
			require.Equal(t, uintptr(maximumDecodedValueBytes), budget.remaining)
		})
	}
}

func TestBytecodeBorrowedSymbolsDoNotAllocateZeros(t *testing.T) {
	t.Parallel()
	original := reflect.ValueOf([16]byte{1})
	registry := symtab.NewSymbolRegistry(symtab.SymbolExports{"host": {"Value": original}})
	budget := bytecodeValueBudget{remaining: 0}
	data := codec.GeneralConstantDescriptorData{
		Kind: uint8(descriptor.GeneralConstantPackageSymbol), PackagePath: "host", SymbolName: "Value",
	}
	value, err := budget.reconstruct(data, registry)
	require.NoError(t, err)
	require.Equal(t, original, value, "a borrowed symbol is returned as it is")
	require.Zero(t, budget.remaining, "borrowing a symbol costs no budget")
	data.Kind = 255
	value, err = budget.reconstruct(data, registry)
	require.Error(t, err, "an unknown constant kind is rejected")
	require.False(t, value.IsValid())
}

func TestBytecodeNamedAndCompositeZerosShareBudget(t *testing.T) {
	t.Parallel()
	registry := symtab.NewSymbolRegistry(symtab.SymbolExports{"host": {"Array": reflect.ValueOf((*[8]byte)(nil))}})
	budget := bytecodeValueBudget{remaining: 16}
	named := codec.GeneralConstantDescriptorData{
		Kind: uint8(descriptor.GeneralConstantNamedTypeZero), PackagePath: "host", SymbolName: "Array",
	}
	composite := codec.GeneralConstantDescriptorData{
		Kind: uint8(descriptor.GeneralConstantCompositeZero),
		TypeDescriptor: descriptor.TypeDescriptorData{
			Kind: uint8(descriptor.KindArray), Length: 8,
			Elem: &descriptor.TypeDescriptorData{Kind: uint8(descriptor.KindBasic), BasicKind: uint8(reflect.Uint8)},
		},
	}
	for _, data := range []codec.GeneralConstantDescriptorData{named, composite} {
		value, err := budget.reconstruct(data, registry)
		require.NoError(t, err, "a valid zero value is reconstructed")
		require.True(t, value.CanSet())
		require.True(t, value.IsZero())
	}
	require.Equal(t, uintptr(0), budget.remaining, "constant kinds share one budget")
	value, err := budget.reconstruct(named, registry)
	require.False(t, value.IsValid(), "a named zero value respects the exhausted shared budget")
	require.ErrorIs(t, err, errCorruptBytecodePayload)
}

func TestBytecodeZeroValueBudgetSharedAcrossFunctionEdges(t *testing.T) {
	t.Parallel()
	builder := flatbuffers.NewBuilder(2048)
	data := codec.GeneralConstantDescriptorData{
		Kind: uint8(descriptor.GeneralConstantCompositeZero),
		TypeDescriptor: descriptor.TypeDescriptorData{
			Kind: uint8(descriptor.KindArray), Length: 1 << 20,
			Elem: &descriptor.TypeDescriptorData{Kind: uint8(descriptor.KindBasic), BasicKind: uint8(reflect.Uint8)},
		},
	}
	constants := packGeneralConstantDescriptors(builder, []codec.GeneralConstantDescriptorData{data})
	schemagen.CompiledFunctionStart(builder)
	schemagen.CompiledFunctionAddGeneralConstantDescriptors(builder, constants)
	leaf := schemagen.CompiledFunctionEnd(builder)
	parent := func(count int, withInitialiser bool) flatbuffers.UOffsetT {
		schemagen.CompiledFunctionStartFunctionsVector(builder, count)
		for range count {
			builder.PrependUOffsetT(leaf)
		}
		children := builder.EndVector(count)
		schemagen.CompiledFunctionStart(builder)
		schemagen.CompiledFunctionAddFunctions(builder, children)
		if withInitialiser {
			schemagen.CompiledFunctionAddVariableInitFunction(builder, leaf)
		}
		return schemagen.CompiledFunctionEnd(builder)
	}
	root := parent(31, true)
	initialiser := parent(33, false)
	schemagen.CompiledFileSetStart(builder)
	schemagen.CompiledFileSetAddRoot(builder, root)
	schemagen.CompiledFileSetAddVariableInitFunction(builder, initialiser)
	fileSet := schemagen.CompiledFileSetEnd(builder)
	builder.Finish(fileSet)
	require.LessOrEqual(t, len(builder.FinishedBytes()), 2048, "the fixture stays compact")
	loaded, err := decodeCompiledFileSet(t.Context(), builder.FinishedBytes(), symtab.NewSymbolRegistry(nil))
	require.Nil(t, loaded, "function edges do not reset the value allocation budget")
	require.ErrorIs(t, err, errCorruptBytecodePayload)
	require.Contains(t, err.Error(), "zero-value storage exceeds")
}
