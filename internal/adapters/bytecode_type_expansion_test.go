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
	"errors"
	"reflect"
	"strings"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"

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

				payload := typeExpansionPayload(100, edge, source, false)
				if len(payload) > 8192 {
					t.Fatal("fixture no longer represents compact expansion")
				}
				_, err := decodeCompiledFileSet(context.Background(), payload, symtab.NewSymbolRegistry(nil))

				if err != nil && strings.Contains(err.Error(), "type expansion exceeds") {
					t.Fatal("shared type graph charged per reference:", err)
				}
			})
		}
	}
}

func TestBytecodeRejectsDistinctTypeExpansion(t *testing.T) {
	t.Parallel()

	payload := distinctTypePayload(maximumDecodedTypeUnits + 1024)
	loaded, err := decodeCompiledFileSet(context.Background(), payload, symtab.NewSymbolRegistry(nil))
	if loaded != nil || !errors.Is(err, errCorruptBytecodePayload) || !strings.Contains(err.Error(), "type expansion exceeds") {
		t.Fatal("distinct type expansion reached reconstruction:", err)
	}
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

	for _, bothRoots := range []bool{false, true} {
		payload := typeExpansionPayload(14, "params", "table", bothRoots)
		fileSet := schemagen.GetRootAsCompiledFileSet(payload, 0)
		if err := validateFunctionExpansion(context.Background(), fileSet, len(payload)); err != nil {
			t.Fatal("shared type references rejected:", err)
		}
	}
}

func TestBytecodeTypeExpansionChargesAbsentSlots(t *testing.T) {
	t.Parallel()
	budget := bytecodeExpansionBudget{functions: 1, types: 3}
	getter := func(int) *schemagen.TypeDescriptor { return nil }
	if err := validateTypeEntries(context.Background(), 3, getter, 0, 100, &budget); err != nil {
		t.Fatal(err)
	}
	if err := validateTypeEntries(context.Background(), 1, getter, 0, 100, &budget); !errors.Is(err, errCorruptBytecodePayload) {
		t.Fatal("absent descriptors refunded vector storage:", err)
	}
}

func TestBytecodeTypeExpansionCancellation(t *testing.T) {
	t.Parallel()
	builder := flatbuffers.NewBuilder(1024)
	root := repeatedTypeGraph(builder, 20, "fields")
	builder.Finish(root)
	payload := builder.FinishedBytes()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	budget := bytecodeExpansionBudget{functions: 1, types: maximumDecodedTypeUnits, typeAllowance: maximumDecodedTypeUnits}
	if err := validateTypeTree(ctx, schemagen.GetRootAsTypeDescriptor(payload, 0), 0, len(payload), &budget); !errors.Is(err, context.Canceled) {
		t.Fatal("type expansion ignored cancellation:", err)
	}
}

func TestBytecodeTypeExpansionRetainsDepthLimit(t *testing.T) {
	t.Parallel()
	payload := typeExpansionPayload(maxBytecodeTypeDescriptorDepth+1, "direct", "table", false)
	loaded, err := decodeCompiledFileSet(context.Background(), payload, symtab.NewSymbolRegistry(nil))
	if loaded != nil || !errors.Is(err, errBytecodeRecursionDepthExceeded) {
		t.Fatal("deep descriptors bypassed preflight depth limit:", err)
	}
}

func TestBytecodeBoundedTypeAliasesStillDecode(t *testing.T) {
	t.Parallel()
	payload := typeExpansionPayload(3, "params", "table", true)
	loaded, err := decodeCompiledFileSet(context.Background(), payload, symtab.NewSymbolRegistry(nil))
	if loaded == nil || err != nil {
		t.Fatal("bounded type aliases no longer decode:", err)
	}
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
	loaded, err := decodeCompiledFileSet(context.Background(), builder.FinishedBytes(), symtab.NewSymbolRegistry(nil))
	if loaded != nil || err == nil || !strings.Contains(err.Error(), "array layout exceeds") {
		t.Fatal("oversized decoded array reached runtime reconstruction:", err)
	}
}
