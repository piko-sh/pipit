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

func TestBytecodeFunctionVectorCensus(t *testing.T) {
	t.Parallel()
	setters := map[string]func(*flatbuffers.Builder, flatbuffers.UOffsetT){
		"registers":           schemagen.CompiledFunctionAddRegisterCounts,
		"parameters":          schemagen.CompiledFunctionAddParameterKinds,
		"parameter-registers": schemagen.CompiledFunctionAddParameterRegisters,
		"results":             schemagen.CompiledFunctionAddResultKinds,
		"body":                schemagen.CompiledFunctionAddBody,
		"bools":               schemagen.CompiledFunctionAddBoolConstants,
		"ints":                schemagen.CompiledFunctionAddIntConstants,
		"floats":              schemagen.CompiledFunctionAddFloatConstants,
		"uints":               schemagen.CompiledFunctionAddUintConstants,
		"complex":             schemagen.CompiledFunctionAddComplexConstants,
		"strings":             schemagen.CompiledFunctionAddStringConstants,
		"general":             schemagen.CompiledFunctionAddGeneralConstantDescriptors,
		"types":               schemagen.CompiledFunctionAddTypeTableDescriptors,
		"type-names":          schemagen.CompiledFunctionAddTypeNames,
		"calls":               schemagen.CompiledFunctionAddCallSites,
		"upvalues":            schemagen.CompiledFunctionAddUpvalueDescriptors,
		"functions":           schemagen.CompiledFunctionAddFunctions,
		"named-results":       schemagen.CompiledFunctionAddNamedResultLocations,
		"methods":             schemagen.CompiledFunctionAddMethodTable,
		"layouts":             schemagen.CompiledFunctionAddStructLayoutTable,
		"interfaces":          schemagen.CompiledFunctionAddTypeTableInterfaceMethods,
	}
	for name, setter := range setters {
		t.Run(name, func(t *testing.T) {
			builder := flatbuffers.NewBuilder(128)
			builder.StartVector(4, 3, 4)
			for range 3 {
				builder.PrependUint32(0)
			}
			vector := builder.EndVector(3)
			schemagen.CompiledFunctionStart(builder)
			setter(builder, vector)
			function := schemagen.CompiledFunctionEnd(builder)
			builder.Finish(function)
			payload := builder.FinishedBytes()
			budget := bytecodeExpansionBudget{vectors: 2}
			err := validateFunctionVectors(context.Background(), schemagen.GetRootAsCompiledFunction(payload, 0), len(payload), &budget)
			if !errors.Is(err, errCorruptBytecodePayload) || !strings.Contains(err.Error(), "vector expansion exceeds") {
				t.Fatal("vector was not charged before access:", err)
			}
		})
	}
}

func nestedVectorPayload(mode string) []byte {
	builder := flatbuffers.NewBuilder(32768)
	var leaf flatbuffers.UOffsetT
	switch mode {
	case "calls":
		schemagen.VarLocationStart(builder)
		location := schemagen.VarLocationEnd(builder)
		builder.StartVector(4, 4096, 4)
		for range 4096 {
			builder.PrependUOffsetT(location)
		}
		arguments := builder.EndVector(4096)
		schemagen.CallSiteStart(builder)
		schemagen.CallSiteAddArguments(builder, arguments)
		schemagen.CallSiteAddReturns(builder, arguments)
		leaf = schemagen.CallSiteEnd(builder)
	case "interfaces":
		name := builder.CreateString("Method")
		builder.StartVector(4, 4096, 4)
		for range 4096 {
			builder.PrependUOffsetT(name)
		}
		methods := builder.EndVector(4096)
		schemagen.InterfaceMethodSetStart(builder)
		schemagen.InterfaceMethodSetAddMethods(builder, methods)
		leaf = schemagen.InterfaceMethodSetEnd(builder)
	default:
		constants := builder.CreateByteVector(make([]byte, 4096))
		schemagen.CompiledFunctionStart(builder)
		schemagen.CompiledFunctionAddBoolConstants(builder, constants)
		leaf = schemagen.CompiledFunctionEnd(builder)
	}
	builder.StartVector(4, 512, 4)
	for range 512 {
		builder.PrependUOffsetT(leaf)
	}
	entries := builder.EndVector(512)
	schemagen.CompiledFunctionStart(builder)
	switch mode {
	case "calls":
		schemagen.CompiledFunctionAddCallSites(builder, entries)
	case "interfaces":
		schemagen.CompiledFunctionAddTypeTableInterfaceMethods(builder, entries)
	default:
		schemagen.CompiledFunctionAddFunctions(builder, entries)
	}
	function := schemagen.CompiledFunctionEnd(builder)
	schemagen.CompiledFileSetStart(builder)
	schemagen.CompiledFileSetAddRoot(builder, function)
	root := schemagen.CompiledFileSetEnd(builder)
	builder.Finish(root)
	return builder.FinishedBytes()
}

func TestBytecodeRejectsNestedVectorAmplification(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"calls", "interfaces", "functions"} {
		t.Run(mode, func(t *testing.T) {
			payload := nestedVectorPayload(mode)
			if len(payload) > 32768 {
				t.Fatal("fixture is no longer compact")
			}
			loaded, err := decodeCompiledFileSet(context.Background(), payload, symtab.NewSymbolRegistry(nil))
			if loaded != nil || !errors.Is(err, errCorruptBytecodePayload) || !strings.Contains(err.Error(), "vector expansion exceeds") {
				t.Fatal("aliased vectors reached reconstruction:", err)
			}
		})
	}
}

func TestBytecodeVectorBudgetArithmetic(t *testing.T) {
	t.Parallel()
	budget := bytecodeExpansionBudget{vectors: 10}
	if err := budget.chargeVectors(10, 4, 6); err != nil || budget.vectors != 0 {
		t.Fatal(err)
	}
	if err := budget.chargeVectors(10, 1); !errors.Is(err, errCorruptBytecodePayload) || budget.vectors != 0 {
		t.Fatal("exhausted budget wrapped:", err)
	}
	for _, count := range []int{-1, 11} {
		budget.vectors = 100
		if err := budget.chargeVectors(10, count); !errors.Is(err, errCorruptBytecodePayload) || budget.vectors != 100 {
			t.Fatal("invalid declared vector admitted:", err)
		}
	}
}

func TestBytecodeFileVectorCensus(t *testing.T) {
	t.Parallel()
	setters := map[string]func(*flatbuffers.Builder, flatbuffers.UOffsetT){
		"entrypoints":  schemagen.CompiledFileSetAddEntrypoints,
		"initialisers": schemagen.CompiledFileSetAddInitialisationFunctions,
		"variables":    schemagen.CompiledFileSetAddPackageVariables,
	}
	for name, setter := range setters {
		t.Run(name, func(t *testing.T) {
			builder := flatbuffers.NewBuilder(1024)
			entries := builder.CreateByteVector(make([]byte, maximumDecodedVectorElements+1))
			schemagen.CompiledFileSetStart(builder)
			setter(builder, entries)
			root := schemagen.CompiledFileSetEnd(builder)
			builder.Finish(root)
			loaded, err := decodeCompiledFileSet(context.Background(), builder.FinishedBytes(), symtab.NewSymbolRegistry(nil))
			if loaded != nil || !errors.Is(err, errCorruptBytecodePayload) || !strings.Contains(err.Error(), "vector expansion exceeds") {
				t.Fatal("file vector not charged before access:", err)
			}
		})
	}
}
