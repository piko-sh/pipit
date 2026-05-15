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

package program

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

func calling(name string, targets ...uint16) *CompiledFunction {
	function := NewNamedFunction(name)
	for _, target := range targets {
		function.CallSites = append(function.CallSites, CallSite{FunctionIndex: target})
	}
	return function
}

func TestDetectRecursionReportsEveryCycleShape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		functions []*CompiledFunction
		wantCycle bool
	}{
		{name: "an empty program has no cycle"},
		{
			name:      "a straight chain has no cycle",
			functions: []*CompiledFunction{calling("a", 1), calling("b", 2), calling("c")},
		},
		{
			name:      "a diamond has no cycle",
			functions: []*CompiledFunction{calling("a", 1, 2), calling("b", 3), calling("c", 3), calling("d")},
		},
		{
			name:      "direct recursion is a cycle",
			functions: []*CompiledFunction{calling("a", 0)},
			wantCycle: true,
		},
		{
			name:      "mutual recursion is a cycle",
			functions: []*CompiledFunction{calling("a", 1), calling("b", 0)},
			wantCycle: true,
		},
		{
			name:      "a call site index past the table is ignored",
			functions: []*CompiledFunction{calling("a", 9)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := NewNamedFunction("root")
			root.Functions = tt.functions

			err := DetectRecursion(root)
			if !tt.wantCycle {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, fault.ErrFeatureNotAllowed)
		})
	}
}

func TestDetectRecursionIgnoresClosureAndMethodSites(t *testing.T) {
	t.Parallel()

	selfClosure := NewNamedFunction("a")
	selfClosure.CallSites = []CallSite{{FunctionIndex: 0, IsClosure: true}}

	selfMethod := NewNamedFunction("b")
	selfMethod.CallSites = []CallSite{{FunctionIndex: 0, IsMethod: true}}

	for name, function := range map[string]*CompiledFunction{
		"a closure call site": selfClosure,
		"a method call site":  selfMethod,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			root := NewNamedFunction("root")
			root.Functions = []*CompiledFunction{function}

			require.NoError(t, DetectRecursion(root),
				"the static call graph only covers direct calls, which is what this check is for")
		})
	}
}

func TestSpecialisationsCapFallsBackToTheDefault(t *testing.T) {
	t.Parallel()

	function := NewNamedFunction("f")
	require.Equal(t, defaultMaxSpecialisations, SpecialisationsCap(function))
}

func TestSpecialisationRegistryRemembersEachTupleOnce(t *testing.T) {
	t.Parallel()

	function := NewNamedFunction("f")
	key := SpecialisationKey{}

	t.Run("an unregistered tuple resolves to nothing", func(t *testing.T) {
		t.Parallel()

		_, ok := LookupSpecialisation(NewNamedFunction("f"), &key)
		require.False(t, ok)
	})

	require.NoError(t, RegisterSpecialisation(function, &key, 3))

	index, ok := LookupSpecialisation(function, &key)
	require.True(t, ok)
	require.Equal(t, uint16(3), index)

	require.NoError(t, RegisterSpecialisation(function, &key, 4), "re-registering the same tuple replaces it")
	index, _ = LookupSpecialisation(function, &key)
	require.Equal(t, uint16(4), index)
}

func TestEmitTier1JumpLeavesTheOffsetForPatching(t *testing.T) {
	t.Parallel()

	function := NewNamedFunction("f")

	offset := EmitTier1Jump(function)
	require.Equal(t, 0, offset)
	require.Len(t, function.Body, 1)
	require.Equal(t, isa.OpDrillTier1, function.Body[0].Op)
	require.Equal(t, uint8(isa.SubOpJump), function.Body[0].A)
}

func TestMayCreateSharedCellsLooksForAClosure(t *testing.T) {
	t.Parallel()

	t.Run("a body with no closure creates none", func(t *testing.T) {
		t.Parallel()

		function := NewNamedFunction("f")
		Emit(function, isa.OpAddInt, 0, 1, 2)

		require.False(t, MayCreateSharedCells(function))
	})

	t.Run("a body that makes a closure may create one", func(t *testing.T) {
		t.Parallel()

		function := NewNamedFunction("f")
		Emit(function, isa.OpAddInt, 0, 1, 2)
		Emit(function, isa.OpMakeClosure, 0, 0, 0)

		require.True(t, MayCreateSharedCells(function))
	})
}

func TestLiveVariablesReportsTheScopeAtAPosition(t *testing.T) {
	t.Parallel()

	table := &DebugVarTable{Entries: []DebugVarEntry{
		{Name: "early", StartPC: 0, EndPC: 4},
		{Name: "late", StartPC: 4},
		{Name: "middle", StartPC: 2, EndPC: 6},
	}}

	tests := []struct {
		name string
		pc   int
		want []string
	}{
		{name: "the first instruction", pc: 0, want: []string{"early"}},
		{name: "inside two overlapping scopes", pc: 3, want: []string{"early", "middle"}},
		{name: "past the first scope's end", pc: 4, want: []string{"late", "middle"}},
		{name: "past every bounded scope", pc: 9, want: []string{"late"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			live := table.LiveVariables(tt.pc)
			names := make([]string, 0, len(live))
			for _, entry := range live {
				names = append(names, entry.Name)
			}
			require.ElementsMatch(t, tt.want, names)
		})
	}

	t.Run("a nil table reports nothing", func(t *testing.T) {
		t.Parallel()

		var absent *DebugVarTable
		require.Nil(t, absent.LiveVariables(0))
	})
}

func TestEpiloguePositionNeedsARecordedLine(t *testing.T) {
	t.Parallel()

	t.Run("a nil source map has no position", func(t *testing.T) {
		t.Parallel()

		var absent *SourceMap
		file, line := absent.EpiloguePosition()
		require.Empty(t, file)
		require.Zero(t, line)
	})

	t.Run("an unrecorded epilogue has no position", func(t *testing.T) {
		t.Parallel()

		file, line := (&SourceMap{}).EpiloguePosition()
		require.Empty(t, file)
		require.Zero(t, line)
	})

	t.Run("a recorded epilogue names its file and line", func(t *testing.T) {
		t.Parallel()

		files := []string{"main.go"}
		sourceMap := &SourceMap{Files: &files}
		sourceMap.Epilogue.Line = 12

		file, line := sourceMap.EpiloguePosition()
		require.Equal(t, "main.go", file)
		require.Equal(t, 12, line)
	})

	t.Run("a file index past the table still reports the line", func(t *testing.T) {
		t.Parallel()

		sourceMap := &SourceMap{}
		sourceMap.Epilogue.Line = 12
		sourceMap.Epilogue.FileID = 9

		file, line := sourceMap.EpiloguePosition()
		require.Empty(t, file)
		require.Equal(t, 12, line)
	})
}
