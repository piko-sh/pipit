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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestSourceMapLookupsToleratePositionsItDoesNotHold(t *testing.T) {
	t.Parallel()

	files := []string{"main.go", "other.go"}
	sourceMap := &SourceMap{
		Files: &files,
		Positions: []SourcePosition{
			{Line: 10, Column: 4, FileID: 0},
			{Line: 0},
			{Line: 20, Column: 1, FileID: 1, Inlined: true},
		},
		Epilogue: SourcePosition{Line: 99, FileID: 0},
	}

	tests := []struct {
		name       string
		pc         int
		wantFile   string
		wantLine   int
		wantColumn int
	}{
		{name: "a recorded position resolves", pc: 0, wantFile: "main.go", wantLine: 10, wantColumn: 4},
		{name: "a synthetic instruction has no position", pc: 1},
		{name: "a position in another file resolves", pc: 2, wantFile: "other.go", wantLine: 20, wantColumn: 1},
		{name: "a position past the body has none", pc: 9},
		{name: "a negative position has none", pc: -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file, line, column := sourceMap.SourcePosition(tt.pc)

			require.Equal(t, tt.wantFile, file)
			require.Equal(t, tt.wantLine, line)
			require.Equal(t, tt.wantColumn, column)
		})
	}

	t.Run("a nil source map resolves nothing", func(t *testing.T) {
		t.Parallel()
		var absent *SourceMap

		file, line, column := absent.SourcePosition(0)

		require.Empty(t, file)
		require.Zero(t, line)
		require.Zero(t, column)
	})
}

func TestSourceMapInlinedFlagsOnlySplicedInstructions(t *testing.T) {
	t.Parallel()

	files := []string{"main.go"}
	sourceMap := &SourceMap{
		Files:     &files,
		Positions: []SourcePosition{{Line: 1}, {Line: 2, Inlined: true}},
	}

	tests := []struct {
		name string
		pc   int
		want bool
	}{
		{name: "an ordinary instruction is not inlined", pc: 0, want: false},
		{name: "a spliced instruction is inlined", pc: 1, want: true},
		{name: "a position past the body is not inlined", pc: 9, want: false},
		{name: "a negative position is not inlined", pc: -1, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, sourceMap.Inlined(tt.pc))
		})
	}

	t.Run("a nil source map reports nothing inlined", func(t *testing.T) {
		t.Parallel()
		var absent *SourceMap

		require.False(t, absent.Inlined(0))
	})
}

func TestFileIDForInternsEachPathOnce(t *testing.T) {
	t.Parallel()

	t.Run("repeated paths reuse their identifier", func(t *testing.T) {
		t.Parallel()
		sourceMap := &SourceMap{}

		first := sourceMap.FileIDFor("main.go")
		second := sourceMap.FileIDFor("other.go")
		again := sourceMap.FileIDFor("main.go")

		require.Equal(t, first, again, "a path already in the table keeps its identifier")
		require.NotEqual(t, first, second)
		require.Len(t, *sourceMap.Files, 2)
	})

	t.Run("the identifier round-trips through the path lookup", func(t *testing.T) {
		t.Parallel()
		sourceMap := &SourceMap{}

		id := sourceMap.FileIDFor("main.go")

		require.Equal(t, "main.go", sourceMap.FilePath(id))
	})

	t.Run("an identifier with no path resolves to nothing", func(t *testing.T) {
		t.Parallel()
		sourceMap := &SourceMap{}
		sourceMap.FileIDFor("main.go")

		require.Empty(t, sourceMap.FilePath(9))
	})
}

func TestDebugSourceAccessorsReportWhetherTheyHaveData(t *testing.T) {
	t.Parallel()

	t.Run("a function without debug information says so", func(t *testing.T) {
		t.Parallel()
		compiledFunction := NewNamedFunction("plain")

		require.False(t, compiledFunction.HasDebugSourceMap())
		require.False(t, compiledFunction.HasDebugVarTable())

		file, line, column := compiledFunction.DebugSourcePosition(0)
		require.Empty(t, file)
		require.Zero(t, line)
		require.Zero(t, column)
	})

	t.Run("a function with a source map resolves positions through it", func(t *testing.T) {
		t.Parallel()
		compiledFunction := NewNamedFunction("mapped")
		files := []string{"main.go"}
		compiledFunction.DebugSourceMap = &SourceMap{Files: &files, Positions: []SourcePosition{{Line: 7, Column: 2}}}

		require.True(t, compiledFunction.HasDebugSourceMap())

		file, line, column := compiledFunction.DebugSourcePosition(0)
		require.Equal(t, "main.go", file)
		require.Equal(t, 7, line)
		require.Equal(t, 2, column)
	})
}

func TestRelativiseGlobalOperandsSubtractsTheBundleBase(t *testing.T) {
	t.Parallel()

	t.Run("a narrow global operand is rebased", func(t *testing.T) {
		t.Parallel()
		root := NewNamedFunction("root")
		root.Body = []isa.Instruction{
			{Op: isa.OpGetGlobal, A: 0, B: 7, C: uint8(isa.RegisterInt)},
		}
		base := SlotAllocation{}
		base[isa.RegisterInt] = 3

		require.NoError(t, RelativiseGlobalOperands(root, base))
		require.Equal(t, uint8(4), root.Body[0].B, "the operand becomes an offset from the bundle's own base")
	})

	t.Run("an operand below the base is refused", func(t *testing.T) {
		t.Parallel()
		root := NewNamedFunction("root")
		root.Body = []isa.Instruction{
			{Op: isa.OpSetGlobal, A: 0, B: 1, C: uint8(isa.RegisterInt)},
		}
		base := SlotAllocation{}
		base[isa.RegisterInt] = 5

		err := RelativiseGlobalOperands(root, base)

		require.Error(t, err, "a reference below the bundle base cannot be rebased")
		require.Contains(t, err.Error(), "bundle base")
	})

	t.Run("a bank outside the global range is refused", func(t *testing.T) {
		t.Parallel()
		root := NewNamedFunction("root")
		root.Body = []isa.Instruction{
			{Op: isa.OpGetGlobal, A: 0, B: 0, C: 200},
		}

		require.Error(t, RelativiseGlobalOperands(root, SlotAllocation{}))
	})

	t.Run("an instruction that touches no global is left alone", func(t *testing.T) {
		t.Parallel()
		root := NewNamedFunction("root")
		root.Body = []isa.Instruction{{Op: isa.OpAddInt, A: 0, B: 1, C: 2}}

		require.NoError(t, RelativiseGlobalOperands(root, SlotAllocation{}))
		require.Equal(t, uint8(1), root.Body[0].B)
	})

	t.Run("a nil root is accepted", func(t *testing.T) {
		t.Parallel()
		require.NoError(t, RelativiseGlobalOperands(nil, SlotAllocation{}))
	})

	t.Run("sub-functions are rebased too", func(t *testing.T) {
		t.Parallel()
		root := NewNamedFunction("root")
		child := NewNamedFunction("child")
		child.Body = []isa.Instruction{{Op: isa.OpGetGlobal, A: 0, B: 7, C: uint8(isa.RegisterInt)}}
		root.AppendFunction(child)
		base := SlotAllocation{}
		base[isa.RegisterInt] = 2

		require.NoError(t, RelativiseGlobalOperands(root, base))
		require.Equal(t, uint8(5), child.Body[0].B, "the walk must reach every function in the bundle")
	})
}

func TestSetGlobalBasesToleratesMissingInput(t *testing.T) {
	t.Parallel()

	t.Run("a nil root is accepted", func(t *testing.T) {
		t.Parallel()
		require.NotPanics(t, func() { SetGlobalBases(nil, &SlotAllocation{}) })
	})

	t.Run("nil bases are accepted", func(t *testing.T) {
		t.Parallel()
		require.NotPanics(t, func() { SetGlobalBases(NewNamedFunction("root"), nil) })
	})

	t.Run("the bases reach every function in the bundle", func(t *testing.T) {
		t.Parallel()
		root := NewNamedFunction("root")
		child := NewNamedFunction("child")
		root.AppendFunction(child)
		bases := SlotAllocation{}
		bases[isa.RegisterInt] = 4

		SetGlobalBases(root, &bases)

		require.Equal(t, &bases, root.GlobalBases)
		require.Equal(t, &bases, child.GlobalBases)
	})
}

func TestDisassembleRendersEveryInstruction(t *testing.T) {
	t.Parallel()

	compiledFunction := NewNamedFunction("example")
	compiledFunction.Body = []isa.Instruction{
		{Op: isa.OpAddInt, A: 0, B: 1, C: 2},
		{Op: isa.OpSubInt, A: 0, B: 1, C: 2},
		returnInstruction(1),
	}

	rendered := compiledFunction.Disassemble()

	require.Contains(t, rendered, "ADD_INT")
	require.Contains(t, rendered, "SUB_INT")
	require.Equal(t, 3, strings.Count(rendered, "\n"), "one line per instruction")
}

func TestDisassembleRangeRendersOnlyTheRequestedWindow(t *testing.T) {
	t.Parallel()

	compiledFunction := NewNamedFunction("example")
	compiledFunction.Body = []isa.Instruction{
		{Op: isa.OpAddInt}, {Op: isa.OpSubInt}, {Op: isa.OpMulInt},
	}

	tests := []struct {
		name     string
		start    int
		end      int
		contains []string
		absent   []string
	}{
		{name: "the first instruction only", start: 0, end: 1, contains: []string{"ADD_INT"}, absent: []string{"SUB_INT", "MUL_INT"}},
		{name: "a middle window", start: 1, end: 2, contains: []string{"SUB_INT"}, absent: []string{"ADD_INT", "MUL_INT"}},
		{name: "the whole body", start: 0, end: 3, contains: []string{"ADD_INT", "SUB_INT", "MUL_INT"}},
		{name: "an empty window", start: 1, end: 1, absent: []string{"ADD_INT", "SUB_INT", "MUL_INT"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rendered := compiledFunction.DisassembleRange(tt.start, tt.end)

			for _, want := range tt.contains {
				require.Contains(t, rendered, want)
			}
			for _, unwanted := range tt.absent {
				require.NotContains(t, rendered, unwanted)
			}
		})
	}

	t.Run("an empty body renders nothing", func(t *testing.T) {
		t.Parallel()
		require.Empty(t, NewNamedFunction("empty").Disassemble())
	})
}
