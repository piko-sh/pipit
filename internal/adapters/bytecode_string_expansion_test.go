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
	"strings"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"

	"pipit.sh/pipit/internal/schema/schemagen"
	"pipit.sh/pipit/internal/symtab"
)

func stringExpansionPayload(source string, copies int) []byte {
	builder := flatbuffers.NewBuilder(32768)
	text := builder.CreateString(strings.Repeat("x", 32768))
	entry := text
	switch source {
	case "methods":
		builder.StartVector(4, 1, 4)
		builder.PrependUOffsetT(text)
		methods := builder.EndVector(1)
		schemagen.InterfaceMethodSetStart(builder)
		schemagen.InterfaceMethodSetAddMethods(builder, methods)
		entry = schemagen.InterfaceMethodSetEnd(builder)
	case "types":
		schemagen.TypeDescriptorStart(builder)
		schemagen.TypeDescriptorAddName(builder, text)
		entry = schemagen.TypeDescriptorEnd(builder)
	case "fields":
		schemagen.TypeDescFieldStart(builder)
		schemagen.TypeDescFieldAddTag(builder, text)
		field := schemagen.TypeDescFieldEnd(builder)
		builder.StartVector(4, 1, 4)
		builder.PrependUOffsetT(field)
		fields := builder.EndVector(1)
		schemagen.TypeDescriptorStart(builder)
		schemagen.TypeDescriptorAddFields(builder, fields)
		entry = schemagen.TypeDescriptorEnd(builder)
	case "variables":
		schemagen.PackageVariableEntryStart(builder)
		schemagen.PackageVariableEntryAddPackagePath(builder, text)
		entry = schemagen.PackageVariableEntryEnd(builder)
	case "entrypoints":
		schemagen.EntrypointEntryStart(builder)
		schemagen.EntrypointEntryAddName(builder, text)
		entry = schemagen.EntrypointEntryEnd(builder)
	case "constants":
		schemagen.GeneralConstantDescriptorStart(builder)
		schemagen.GeneralConstantDescriptorAddSymbolName(builder, text)
		entry = schemagen.GeneralConstantDescriptorEnd(builder)
	case "names":
		schemagen.TypeNameEntryStart(builder)
		schemagen.TypeNameEntryAddName(builder, text)
		entry = schemagen.TypeNameEntryEnd(builder)
	case "method-table":
		schemagen.MethodTableEntryStart(builder)
		schemagen.MethodTableEntryAddName(builder, text)
		entry = schemagen.MethodTableEntryEnd(builder)
	case "functions":
		schemagen.CompiledFunctionStart(builder)
		schemagen.CompiledFunctionAddSourceFile(builder, text)
		entry = schemagen.CompiledFunctionEnd(builder)
	}
	builder.StartVector(4, copies, 4)
	for range copies {
		builder.PrependUOffsetT(entry)
	}
	entries := builder.EndVector(copies)
	schemagen.CompiledFunctionStart(builder)
	switch source {
	case "strings":
		schemagen.CompiledFunctionAddStringConstants(builder, entries)
	case "methods":
		schemagen.CompiledFunctionAddTypeTableInterfaceMethods(builder, entries)
	case "types", "fields":
		schemagen.CompiledFunctionAddTypeTableDescriptors(builder, entries)
	case "constants":
		schemagen.CompiledFunctionAddGeneralConstantDescriptors(builder, entries)
	case "names":
		schemagen.CompiledFunctionAddTypeNames(builder, entries)
	case "method-table":
		schemagen.CompiledFunctionAddMethodTable(builder, entries)
	case "functions":
		schemagen.CompiledFunctionAddFunctions(builder, entries)
	}
	function := schemagen.CompiledFunctionEnd(builder)
	schemagen.CompiledFileSetStart(builder)
	schemagen.CompiledFileSetAddRoot(builder, function)
	switch source {
	case "variables":
		schemagen.CompiledFileSetAddPackageVariables(builder, entries)
	case "entrypoints":
		schemagen.CompiledFileSetAddEntrypoints(builder, entries)
	}
	root := schemagen.CompiledFileSetEnd(builder)
	builder.Finish(root)
	return builder.FinishedBytes()
}

func TestBytecodeRejectsStringAliasAmplification(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"strings", "methods", "variables", "entrypoints", "constants", "names", "method-table", "functions"} {
		t.Run(source, func(t *testing.T) {
			payload := stringExpansionPayload(source, 257)
			if len(payload) > 40000 {
				t.Fatal("fixture is no longer compact")
			}
			loaded, err := decodeCompiledFileSet(context.Background(), payload, symtab.NewSymbolRegistry(nil))
			if loaded != nil || !errors.Is(err, errCorruptBytecodePayload) || !strings.Contains(err.Error(), "string expansion exceeds") {
				t.Fatal("aliased strings reached reconstruction:", err)
			}
		})
	}
}

func TestBytecodeSharedDescriptorStringMaterialisesOnce(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"types", "fields"} {
		t.Run(source, func(t *testing.T) {
			payload := stringExpansionPayload(source, 257)
			loaded, err := decodeCompiledFileSet(context.Background(), payload, symtab.NewSymbolRegistry(nil))
			if err != nil || loaded == nil {
				t.Fatal("shared descriptor string rejected:", err)
			}
		})
	}
}

func TestBytecodeStringAliasesAtBudgetDecode(t *testing.T) {
	t.Parallel()
	payload := stringExpansionPayload("strings", 256)
	loaded, err := decodeCompiledFileSet(context.Background(), payload, symtab.NewSymbolRegistry(nil))
	if loaded == nil || err != nil {
		t.Fatal("bounded string aliases rejected:", err)
	}
}

func TestBytecodeStringBudgetCountsBytes(t *testing.T) {
	t.Parallel()
	budget := bytecodeExpansionBudget{strings: 4}
	if err := budget.chargeStrings([]byte("é"), []byte("é")); err != nil || budget.strings != 0 {
		t.Fatal(err)
	}
	if err := budget.chargeStrings([]byte("x")); !errors.Is(err, errCorruptBytecodePayload) || budget.strings != 0 {
		t.Fatal("exhausted byte budget wrapped:", err)
	}
	if err := budget.chargeStrings(nil, []byte{}); err != nil {
		t.Fatal(err)
	}
}

func TestBytecodeStringScanCancellation(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := checkStringEntries(ctx, 1, func(int) error { called = true; return nil })
	if !errors.Is(err, context.Canceled) || called {
		t.Fatal("cancelled scan accessed metadata:", err)
	}
}
