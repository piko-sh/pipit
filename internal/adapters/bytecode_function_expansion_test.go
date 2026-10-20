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
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/schema/schemagen"
	"pipit.sh/pipit/internal/symtab"
)

func repeatedFunctionGraph(depth int, bothRoots, initializer bool) []byte {
	builder := flatbuffers.NewBuilder(1024)
	schemagen.CompiledFunctionStart(builder)
	child := schemagen.CompiledFunctionEnd(builder)
	for range depth {
		width := 2
		if initializer {
			width = 1
		}
		schemagen.CompiledFunctionStartFunctionsVector(builder, width)
		for range width {
			builder.PrependUOffsetT(child)
		}
		children := builder.EndVector(width)
		schemagen.CompiledFunctionStart(builder)
		schemagen.CompiledFunctionAddFunctions(builder, children)
		if initializer {
			schemagen.CompiledFunctionAddVariableInitFunction(builder, child)
		}
		child = schemagen.CompiledFunctionEnd(builder)
	}
	schemagen.CompiledFileSetStart(builder)
	schemagen.CompiledFileSetAddRoot(builder, child)
	if bothRoots {
		schemagen.CompiledFileSetAddVariableInitFunction(builder, child)
	}
	root := schemagen.CompiledFileSetEnd(builder)
	builder.Finish(root)
	return builder.FinishedBytes()
}

func TestBytecodeRejectsExponentialFunctionExpansion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		initializer bool
	}{
		{name: "nested functions", initializer: false},
		{name: "variable initialisers", initializer: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := repeatedFunctionGraph(24, false, tt.initializer)
			require.LessOrEqual(t, len(payload), 4096, "the fixture stays compact")
			loaded, err := decodeCompiledFileSet(t.Context(), payload, symtab.NewSymbolRegistry(nil))
			require.ErrorIs(t, err, errCorruptBytecodePayload, "an expansion bomb stops before reconstruction")
			require.Nil(t, loaded)
		})
	}
}

func TestBytecodeFunctionExpansionSharesRootBudget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		bothRoots bool
		wantErr   error
	}{
		{name: "one root stays within the budget", bothRoots: false, wantErr: nil},
		{name: "both file roots share one budget", bothRoots: true, wantErr: errCorruptBytecodePayload},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := repeatedFunctionGraph(15, tt.bothRoots, false)
			fileSet := schemagen.GetRootAsCompiledFileSet(payload, 0)
			require.ErrorIs(t, validateFunctionExpansion(t.Context(), fileSet, len(payload)), tt.wantErr)
		})
	}
}

func TestBytecodeFunctionExpansionCancellation(t *testing.T) {
	t.Parallel()
	payload := repeatedFunctionGraph(24, false, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	loaded, err := decodeCompiledFileSet(ctx, payload, symtab.NewSymbolRegistry(nil))
	require.Nil(t, loaded, "a cancelled expansion stops before reconstruction")
	require.ErrorIs(t, err, context.Canceled)
}

func TestBytecodeBoundedFunctionAliasesStillDecode(t *testing.T) {
	t.Parallel()
	payload := repeatedFunctionGraph(4, true, true)
	loaded, err := decodeCompiledFileSet(t.Context(), payload, symtab.NewSymbolRegistry(nil))
	require.NoError(t, err, "bounded shared references decode")
	require.NotNil(t, loaded)
}
