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
	"strings"
	"testing"

	flatbuffers "github.com/google/flatbuffers/go"
	"github.com/stretchr/testify/require"

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
	require.LessOrEqual(t, len(payload), 2048, "the fixture stays compact")
	inspection, err := ConvertBytecode(payload)
	require.Nil(t, inspection)
	require.ErrorIs(t, err, errInspectionLimit, "alias expansion stops before summary construction")
}

func TestInspectionSharesRootBudget(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		bothRoots bool
		wantErr   error
	}{
		{name: "one root stays within the budget", bothRoots: false, wantErr: nil},
		{name: "both roots share one budget", bothRoots: true, wantErr: errInspectionLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := inspectionGraph(15, tt.bothRoots)
			err := validateInspectionExpansion(schemagen.GetRootAsCompiledFileSet(payload, 0), len(payload))
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestInspectionBoundedAliasesStillConvert(t *testing.T) {
	t.Parallel()
	inspection, err := ConvertBytecode(inspectionGraph(3, true))
	require.NoError(t, err, "a bounded graph is inspected")
	require.NotNil(t, inspection)
	require.NotNil(t, inspection.Root)
	require.NotNil(t, inspection.VarInit)
	require.Len(t, inspection.Root.Functions, 2)
	require.Len(t, inspection.VarInit.Functions, 2)
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
			t.Parallel()
			payload := inspectionAmplification(source)
			require.LessOrEqual(t, len(payload), 40000, "the fixture stays compact")
			inspection, err := ConvertBytecode(payload)
			require.Nil(t, inspection)
			require.ErrorIs(t, err, errInspectionLimit, "alias amplification stops before summary allocation")
		})
	}
}

func TestInspectionVectorBounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		count int
	}{
		{name: "negative count", count: -1},
		{name: "count above the payload length", count: 11},
		{name: "largest count", count: int(^uint(0) >> 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			budget := inspectionBudget{payloadLen: 10, elements: 100}
			err := budget.chargeVectors(tt.count)
			require.ErrorIs(t, err, errInspectionLimit, "an impossible count is rejected")
			require.Equal(t, 100, budget.elements, "a rejected charge leaves the budget unchanged")
		})
	}
}

func TestInspectionBudgetIsSpentExactly(t *testing.T) {
	t.Parallel()
	budget := inspectionBudget{payloadLen: 100, elements: 10, strings: 4}
	require.NoError(t, budget.chargeVectors(4, 6))
	require.Equal(t, 0, budget.elements)
	err := budget.chargeVectors(1)
	require.ErrorIs(t, err, errInspectionLimit)
	require.NoError(t, budget.chargeStrings([]byte("é"), []byte("é")))
	require.Equal(t, 0, budget.strings)
	err = budget.chargeStrings([]byte("x"))
	require.ErrorIs(t, err, errInspectionLimit)
}

func TestInspectionRetainsDepthLimit(t *testing.T) {
	t.Parallel()
	inspection, err := ConvertBytecode(inspectionGraph(maxInspectedFunctionNestingDepth+1, false))
	require.Nil(t, inspection)
	require.ErrorIs(t, err, errFunctionNestingTooDeep, "the nesting depth limit still applies")
}
