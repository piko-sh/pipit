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
	"context"
	"reflect"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/schema/schemagen"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

func repeatedTypeGraph(builder *flatbuffers.Builder, depth int, edge string) flatbuffers.UOffsetT {
	schemagen.TypeDescriptorStart(builder)
	schemagen.TypeDescriptorAddBasicKind(builder, byte(reflect.Int))
	child := schemagen.TypeDescriptorEnd(builder)
	for range depth {
		var entries flatbuffers.UOffsetT
		switch edge {
		case "fields":
			schemagen.TypeDescFieldStart(builder)
			schemagen.TypeDescFieldAddTypeDescriptor(builder, child)
			field := schemagen.TypeDescFieldEnd(builder)
			schemagen.TypeDescriptorStartFieldsVector(builder, 2)
			builder.PrependUOffsetT(field)
			builder.PrependUOffsetT(field)
			entries = builder.EndVector(2)
		case "params", "results":
			builder.StartVector(4, 2, 4)
			builder.PrependUOffsetT(child)
			builder.PrependUOffsetT(child)
			entries = builder.EndVector(2)
		}
		schemagen.TypeDescriptorStart(builder)
		schemagen.TypeDescriptorAddBasicKind(builder, byte(reflect.Int))
		switch edge {
		case "fields":
			schemagen.TypeDescriptorAddFields(builder, entries)
		case "params":
			schemagen.TypeDescriptorAddParams(builder, entries)
		case "results":
			schemagen.TypeDescriptorAddResults(builder, entries)
		default:
			schemagen.TypeDescriptorAddElement(builder, child)
			schemagen.TypeDescriptorAddKey(builder, child)
			schemagen.TypeDescriptorAddValue(builder, child)
		}
		child = schemagen.TypeDescriptorEnd(builder)
	}
	return child
}

func typeExpansionPayload(depth int, edge, source string, bothRoots bool) []byte {
	builder := flatbuffers.NewBuilder(1024)
	typeDescriptor := repeatedTypeGraph(builder, depth, edge)
	entry := typeDescriptor
	switch source {
	case "constants":
		schemagen.GeneralConstantDescriptorStart(builder)
		schemagen.GeneralConstantDescriptorAddTypeDescriptor(builder, typeDescriptor)
		entry = schemagen.GeneralConstantDescriptorEnd(builder)
	case "names":
		schemagen.TypeNameEntryStart(builder)
		schemagen.TypeNameEntryAddTypeDescriptor(builder, typeDescriptor)
		entry = schemagen.TypeNameEntryEnd(builder)
	case "variables":
		schemagen.PackageVariableEntryStart(builder)
		schemagen.PackageVariableEntryAddTypeDescriptor(builder, typeDescriptor)
		entry = schemagen.PackageVariableEntryEnd(builder)
	}
	builder.StartVector(4, 1, 4)
	builder.PrependUOffsetT(entry)
	entries := builder.EndVector(1)
	schemagen.CompiledFunctionStart(builder)
	switch source {
	case "table":
		schemagen.CompiledFunctionAddTypeTableDescriptors(builder, entries)
	case "constants":
		schemagen.CompiledFunctionAddGeneralConstantDescriptors(builder, entries)
	case "names":
		schemagen.CompiledFunctionAddTypeNames(builder, entries)
	}
	function := schemagen.CompiledFunctionEnd(builder)
	schemagen.CompiledFileSetStart(builder)
	schemagen.CompiledFileSetAddRoot(builder, function)
	if bothRoots {
		schemagen.CompiledFileSetAddVariableInitFunction(builder, function)
	}
	if source == "variables" {
		schemagen.CompiledFileSetAddPackageVariables(builder, entries)
	}
	fileSet := schemagen.CompiledFileSetEnd(builder)
	builder.Finish(fileSet)
	return builder.FinishedBytes()
}

func TestBytecodeSharedTypeGraphExpandsOnce(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"table", "constants", "names", "variables"} {
		for _, edge := range []string{"direct", "fields", "params", "results"} {
			t.Run(source+"/"+edge, func(t *testing.T) {
				t.Parallel()
				payload := typeExpansionPayload(100, edge, source, false)
				require.LessOrEqual(t, len(payload), 8192, "the fixture stays compact")
				_, err := decodeCompiledFileSet(t.Context(), payload, symtab.NewSymbolRegistry(nil))
				if err != nil {
					require.NotContains(t, err.Error(), "type expansion exceeds", "a shared type graph is charged once, not per reference")
				}
			})
		}
	}
}

func TestBytecodeRejectsDistinctTypeExpansion(t *testing.T) {
	t.Parallel()

	payload := distinctTypePayload(maximumDecodedTypeUnits + 1024)
	loaded, err := decodeCompiledFileSet(t.Context(), payload, symtab.NewSymbolRegistry(nil))
	require.Nil(t, loaded, "distinct type expansion stops before reconstruction")
	require.ErrorIs(t, err, errCorruptBytecodePayload)
	require.Contains(t, err.Error(), "type expansion exceeds")
}

func distinctTypePayload(count int) []byte {
	builder := flatbuffers.NewBuilder(1024)
	offsets := make([]flatbuffers.UOffsetT, count)
	for i := range count {
		schemagen.TypeDescriptorStart(builder)
		schemagen.TypeDescriptorAddBasicKind(builder, byte(reflect.Int))
		schemagen.TypeDescriptorAddLength(builder, int32(i))
		offsets[i] = schemagen.TypeDescriptorEnd(builder)
	}
	builder.StartVector(4, count, 4)
	for i := count - 1; i >= 0; i-- {
		builder.PrependUOffsetT(offsets[i])
	}
	entries := builder.EndVector(count)
	schemagen.CompiledFunctionStart(builder)
	schemagen.CompiledFunctionAddTypeTableDescriptors(builder, entries)
	function := schemagen.CompiledFunctionEnd(builder)
	schemagen.CompiledFileSetStart(builder)
	schemagen.CompiledFileSetAddRoot(builder, function)
	fileSet := schemagen.CompiledFileSetEnd(builder)
	builder.Finish(fileSet)
	return builder.FinishedBytes()
}

func TestBytecodeTypeExpansionSharesFunctionBudget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		bothRoots bool
	}{
		{name: "one root", bothRoots: false},
		{name: "both roots", bothRoots: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := typeExpansionPayload(14, "params", "table", tt.bothRoots)
			fileSet := schemagen.GetRootAsCompiledFileSet(payload, 0)
			require.NoError(t, validateFunctionExpansion(t.Context(), fileSet, len(payload)), "shared type references are accepted")
		})
	}
}

func TestBytecodeTypeExpansionChargesAbsentSlots(t *testing.T) {
	t.Parallel()
	budget := bytecodeExpansionBudget{functions: 1, types: 3}
	getter := func(int) *schemagen.TypeDescriptor { return nil }
	require.NoError(t, validateTypeEntries(t.Context(), 3, getter, 0, 100, &budget))
	err := validateTypeEntries(t.Context(), 1, getter, 0, 100, &budget)
	require.ErrorIs(t, err, errCorruptBytecodePayload, "absent descriptors do not refund vector storage")
}

func TestBytecodeTypeExpansionCancellation(t *testing.T) {
	t.Parallel()
	builder := flatbuffers.NewBuilder(1024)
	root := repeatedTypeGraph(builder, 20, "fields")
	builder.Finish(root)
	payload := builder.FinishedBytes()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	budget := bytecodeExpansionBudget{functions: 1, types: maximumDecodedTypeUnits, typeAllowance: maximumDecodedTypeUnits}
	require.ErrorIs(t, validateTypeTree(ctx, schemagen.GetRootAsTypeDescriptor(payload, 0), 0, len(payload), &budget), context.Canceled, "type expansion honours cancellation")
}

func TestBytecodeTypeExpansionRetainsDepthLimit(t *testing.T) {
	t.Parallel()
	payload := typeExpansionPayload(maxBytecodeTypeDescriptorDepth+1, "direct", "table", false)
	loaded, err := decodeCompiledFileSet(t.Context(), payload, symtab.NewSymbolRegistry(nil))
	require.Nil(t, loaded, "the preflight depth limit applies to deep descriptors")
	require.ErrorIs(t, err, errBytecodeRecursionDepthExceeded)
}

func TestBytecodeBoundedTypeAliasesStillDecode(t *testing.T) {
	t.Parallel()
	payload := typeExpansionPayload(3, "params", "table", true)
	loaded, err := decodeCompiledFileSet(t.Context(), payload, symtab.NewSymbolRegistry(nil))
	require.NotNil(t, loaded, "bounded type aliases decode")
	require.NoError(t, err)
}

func TestBytecodeRejectsOversizedArrayLayout(t *testing.T) {
	t.Parallel()
	builder := flatbuffers.NewBuilder(256)
	schemagen.TypeDescriptorStart(builder)
	schemagen.TypeDescriptorAddKind(builder, schemagen.TypeDescKind(descriptor.KindBasic))
	schemagen.TypeDescriptorAddBasicKind(builder, byte(reflect.Uint8))
	element := schemagen.TypeDescriptorEnd(builder)
	schemagen.TypeDescriptorStart(builder)
	schemagen.TypeDescriptorAddKind(builder, schemagen.TypeDescKind(descriptor.KindArray))
	schemagen.TypeDescriptorAddLength(builder, 1<<30)
	schemagen.TypeDescriptorAddElement(builder, element)
	array := schemagen.TypeDescriptorEnd(builder)
	schemagen.CompiledFunctionStartTypeTableDescriptorsVector(builder, 1)
	builder.PrependUOffsetT(array)
	types := builder.EndVector(1)
	schemagen.CompiledFunctionStart(builder)
	schemagen.CompiledFunctionAddTypeTableDescriptors(builder, types)
	function := schemagen.CompiledFunctionEnd(builder)
	schemagen.CompiledFileSetStart(builder)
	schemagen.CompiledFileSetAddRoot(builder, function)
	root := schemagen.CompiledFileSetEnd(builder)
	builder.Finish(root)
	loaded, err := decodeCompiledFileSet(t.Context(), builder.FinishedBytes(), symtab.NewSymbolRegistry(nil))
	require.Nil(t, loaded, "an oversized decoded array stops before runtime reconstruction")
	require.ErrorContains(t, err, "array layout exceeds")
}
