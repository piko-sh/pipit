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

package schema

import (
	"errors"
	"strings"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"

	"pipit.sh/pipit/internal/schema/schemagen"
)

func inspectionGraph(depth int, bothRoots bool) []byte {
	builder := flatbuffers.NewBuilder(1024)
	schemagen.CompiledFunctionStart(builder)
	child := schemagen.CompiledFunctionEnd(builder)
	for range depth {
		schemagen.CompiledFunctionStartFunctionsVector(builder, 2)
		builder.PrependUOffsetT(child)
		builder.PrependUOffsetT(child)
		children := builder.EndVector(2)
		schemagen.CompiledFunctionStart(builder)
		schemagen.CompiledFunctionAddFunctions(builder, children)
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

func TestInspectionRejectsExponentialGraph(t *testing.T) {
	t.Parallel()
	payload := inspectionGraph(24, false)
	if len(payload) > 2048 {
		t.Fatal("fixture is no longer compact")
	}
	inspection, err := ConvertBytecode(payload)
	if inspection != nil || !errors.Is(err, errInspectionLimit) {
		t.Fatal("expansion reached summary construction:", err)
	}
}

func TestInspectionSharesRootBudget(t *testing.T) {
	t.Parallel()
	for _, bothRoots := range []bool{false, true} {
		payload := inspectionGraph(15, bothRoots)
		err := validateInspectionExpansion(schemagen.GetRootAsCompiledFileSet(payload, 0), len(payload))
		if bothRoots {
			if !errors.Is(err, errInspectionLimit) {
				t.Fatal("root budget reset:", err)
			}
		} else if err != nil {
			t.Fatal("bounded alias expansion rejected:", err)
		}
	}
}

func TestInspectionBoundedAliasesStillConvert(t *testing.T) {
	t.Parallel()
	inspection, err := ConvertBytecode(inspectionGraph(3, true))
	if err != nil || inspection == nil || inspection.Root == nil || inspection.VarInit == nil ||
		len(inspection.Root.Functions) != 2 || len(inspection.VarInit.Functions) != 2 {
		t.Fatal("bounded graph inspection changed:", err)
	}
}

func inspectionAmplification(source string) []byte {
	builder := flatbuffers.NewBuilder(32768)
	var name, body flatbuffers.UOffsetT
	if source == "strings" {
		name = builder.CreateString(strings.Repeat("x", 32768))
	} else {
		schemagen.CompiledFunctionStartBodyVector(builder, 4096)
		for range 4096 {
			builder.PrependUint32(0)
		}
		body = builder.EndVector(4096)
	}
	schemagen.CompiledFunctionStart(builder)
	if source == "strings" {
		schemagen.CompiledFunctionAddName(builder, name)
	} else {
		schemagen.CompiledFunctionAddBody(builder, body)
	}
	child := schemagen.CompiledFunctionEnd(builder)
	schemagen.CompiledFunctionStartFunctionsVector(builder, 512)
	for range 512 {
		builder.PrependUOffsetT(child)
	}
	children := builder.EndVector(512)
	schemagen.CompiledFunctionStart(builder)
	schemagen.CompiledFunctionAddFunctions(builder, children)
	function := schemagen.CompiledFunctionEnd(builder)
	schemagen.CompiledFileSetStart(builder)
	schemagen.CompiledFileSetAddRoot(builder, function)
	root := schemagen.CompiledFileSetEnd(builder)
	builder.Finish(root)
	return builder.FinishedBytes()
}

func TestInspectionRejectsAliasAmplification(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"strings", "vectors"} {
		t.Run(source, func(t *testing.T) {
			payload := inspectionAmplification(source)
			if len(payload) > 40000 {
				t.Fatal("fixture is no longer compact")
			}
			inspection, err := ConvertBytecode(payload)
			if inspection != nil || !errors.Is(err, errInspectionLimit) {
				t.Fatal("alias amplification reached summary allocation:", err)
			}
		})
	}
}

func TestInspectionVectorBounds(t *testing.T) {
	t.Parallel()
	for _, count := range []int{-1, 11, int(^uint(0) >> 1)} {
		budget := inspectionBudget{payloadLen: 10, elements: 100}
		if err := budget.chargeVectors(count); !errors.Is(err, errInspectionLimit) || budget.elements != 100 {
			t.Fatal("impossible count admitted:", err)
		}
	}
	budget := inspectionBudget{payloadLen: 100, elements: 10, strings: 4}
	if err := budget.chargeVectors(4, 6); err != nil || budget.elements != 0 {
		t.Fatal(err)
	}
	if err := budget.chargeVectors(1); !errors.Is(err, errInspectionLimit) {
		t.Fatal(err)
	}
	if err := budget.chargeStrings([]byte("é"), []byte("é")); err != nil || budget.strings != 0 {
		t.Fatal(err)
	}
	if err := budget.chargeStrings([]byte("x")); !errors.Is(err, errInspectionLimit) {
		t.Fatal(err)
	}
}

func TestInspectionRetainsDepthLimit(t *testing.T) {
	t.Parallel()
	inspection, err := ConvertBytecode(inspectionGraph(maxInspectedFunctionNestingDepth+1, false))
	if inspection != nil || !errors.Is(err, errFunctionNestingTooDeep) {
		t.Fatal("depth limit bypassed:", err)
	}
}
