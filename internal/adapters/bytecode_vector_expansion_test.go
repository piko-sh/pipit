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
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/stretchr/testify/require"

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
			t.Parallel()
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
			err := validateFunctionVectors(t.Context(), schemagen.GetRootAsCompiledFunction(payload, 0), len(payload), &budget)
			require.ErrorIs(t, err, errCorruptBytecodePayload, "the vector is charged before access")
			require.Contains(t, err.Error(), "vector expansion exceeds")
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
			t.Parallel()
			payload := nestedVectorPayload(mode)
			require.LessOrEqual(t, len(payload), 32768, "the fixture stays compact")
			loaded, err := decodeCompiledFileSet(t.Context(), payload, symtab.NewSymbolRegistry(nil))
			require.Nil(t, loaded, "aliased vectors stop before reconstruction")
			require.ErrorIs(t, err, errCorruptBytecodePayload)
			require.Contains(t, err.Error(), "vector expansion exceeds")
		})
	}
}

func TestBytecodeVectorBudgetArithmetic(t *testing.T) {
	t.Parallel()
	budget := bytecodeExpansionBudget{vectors: 10}
	require.NoError(t, budget.chargeVectors(10, 4, 6))
	require.Equal(t, 0, budget.vectors)
	err := budget.chargeVectors(10, 1)
	require.ErrorIs(t, err, errCorruptBytecodePayload, "a charge past the budget is rejected")
	require.Equal(t, 0, budget.vectors, "an exhausted budget stays at zero")
	tests := []struct {
		name  string
		count int
	}{
		{name: "negative count", count: -1},
		{name: "count above the declared length", count: 11},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			budget := bytecodeExpansionBudget{vectors: 100}
			err := budget.chargeVectors(10, tt.count)
			require.ErrorIs(t, err, errCorruptBytecodePayload, "an invalid declared vector is rejected")
			require.Equal(t, 100, budget.vectors)
		})
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
			t.Parallel()
			builder := flatbuffers.NewBuilder(1024)
			entries := builder.CreateByteVector(make([]byte, maximumDecodedVectorElements+1))
			schemagen.CompiledFileSetStart(builder)
			setter(builder, entries)
			root := schemagen.CompiledFileSetEnd(builder)
			builder.Finish(root)
			loaded, err := decodeCompiledFileSet(t.Context(), builder.FinishedBytes(), symtab.NewSymbolRegistry(nil))
			require.Nil(t, loaded, "the file vector is charged before access")
			require.ErrorIs(t, err, errCorruptBytecodePayload)
			require.Contains(t, err.Error(), "vector expansion exceeds")
		})
	}
}
