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
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"

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
	for _, initializer := range []bool{false, true} {
		payload := repeatedFunctionGraph(24, false, initializer)
		if len(payload) > 4096 {
			t.Fatal("fixture no longer demonstrates compact expansion")
		}
		loaded, err := decodeCompiledFileSet(context.Background(), payload, symtab.NewSymbolRegistry(nil))
		if !errors.Is(err, errCorruptBytecodePayload) || loaded != nil {
			t.Fatal("expansion bomb reached reconstruction:", err)
		}
	}
}

func TestBytecodeFunctionExpansionSharesRootBudget(t *testing.T) {
	t.Parallel()
	for _, bothRoots := range []bool{false, true} {
		payload := repeatedFunctionGraph(15, bothRoots, false)
		fileSet := schemagen.GetRootAsCompiledFileSet(payload, 0)
		err := validateFunctionExpansion(context.Background(), fileSet, len(payload))
		if bothRoots {
			if !errors.Is(err, errCorruptBytecodePayload) {
				t.Fatal("file roots received independent budgets:", err)
			}
		} else if err != nil {
			t.Fatal("bounded shared references rejected:", err)
		}
	}
}

func TestBytecodeFunctionExpansionCancellation(t *testing.T) {
	t.Parallel()
	payload := repeatedFunctionGraph(24, false, false)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	loaded, err := decodeCompiledFileSet(ctx, payload, symtab.NewSymbolRegistry(nil))
	if loaded != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled expansion reached reconstruction:", err)
	}
}

func TestBytecodeBoundedFunctionAliasesStillDecode(t *testing.T) {
	t.Parallel()
	payload := repeatedFunctionGraph(4, true, true)
	loaded, err := decodeCompiledFileSet(context.Background(), payload, symtab.NewSymbolRegistry(nil))
	if err != nil || loaded == nil {
		t.Fatal("bounded shared references no longer decode:", err)
	}
}
