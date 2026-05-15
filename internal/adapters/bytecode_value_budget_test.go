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

	"pipit.sh/pipit/internal/codec"
	"pipit.sh/pipit/internal/schema/schemagen"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

func TestBytecodeZeroValueBudgetBeforeAllocation(t *testing.T) {
	t.Parallel()
	budget := bytecodeValueBudget{remaining: 16}
	first, err := budget.zero(reflect.TypeFor[uint64]())
	if err != nil || !first.CanSet() || !first.IsZero() {
		t.Fatal(first, err)
	}
	second, err := budget.zero(reflect.TypeFor[uint64]())
	if err != nil || budget.remaining != 0 {
		t.Fatal(second, err)
	}
	first.SetUint(9)
	if !second.IsZero() {
		t.Fatal("zero values share mutable storage")
	}
	if value, err := budget.zero(reflect.TypeFor[uint64]()); value.IsValid() || !errors.Is(err, errCorruptBytecodePayload) || budget.remaining != 0 {
		t.Fatal("exhausted budget allocated or wrapped:", err)
	}
	budget.remaining = 1
	if _, err := budget.zero(reflect.TypeFor[struct{}]()); err != nil || budget.remaining != 0 {
		t.Fatal(err)
	}
	if value, err := budget.zero(reflect.TypeFor[struct{}]()); value.IsValid() || !errors.Is(err, errCorruptBytecodePayload) {
		t.Fatal(err)
	}
}

func TestBytecodeNamedZeroCannotBypassBudget(t *testing.T) {
	t.Parallel()
	type large [maximumDecodedValueBytes + 1]byte
	registry := symtab.NewSymbolRegistry(symtab.SymbolExports{"host": {
		"Large":   reflect.ValueOf((*large)(nil)),
		"NotType": reflect.ValueOf(1),
		"NonNil":  reflect.ValueOf(new(int)),
	}})
	budget := bytecodeValueBudget{remaining: maximumDecodedValueBytes}
	for _, name := range []string{"Large", "NotType", "NonNil", "Missing"} {
		data := codec.GeneralConstantDescriptorData{
			Kind: uint8(descriptor.GeneralConstantNamedTypeZero), PackagePath: "host", SymbolName: name,
		}
		value, err := budget.reconstruct(data, registry)
		if err == nil || value.IsValid() || budget.remaining != maximumDecodedValueBytes {
			t.Fatal("named type allocated without valid authority and budget:", name, err)
		}
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
	if err != nil || value != original || budget.remaining != 0 {
		t.Fatal("borrowed symbol changed:", err)
	}
	data.Kind = 255
	if value, err := budget.reconstruct(data, registry); err == nil || value.IsValid() {
		t.Fatal("unknown constant kind accepted")
	}
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
		if err != nil || !value.CanSet() || !value.IsZero() {
			t.Fatal("valid zero reconstruction changed:", err)
		}
	}
	if budget.remaining != 0 {
		t.Fatal("constant kinds received separate budgets")
	}
	if value, err := budget.reconstruct(named, registry); value.IsValid() || !errors.Is(err, errCorruptBytecodePayload) {
		t.Fatal("named zero bypassed exhausted shared budget:", err)
	}
}

func TestBytecodeZeroValueBudgetSharedAcrossFunctionEdges(t *testing.T) {
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
	if len(builder.FinishedBytes()) > 2048 {
		t.Fatal("fixture is no longer compact")
	}
	loaded, err := decodeCompiledFileSet(context.Background(), builder.FinishedBytes(), symtab.NewSymbolRegistry(nil))
	if loaded != nil || !errors.Is(err, errCorruptBytecodePayload) || !strings.Contains(err.Error(), "zero-value storage exceeds") {
		t.Fatal("function edges reset value allocation budget:", err)
	}
}
