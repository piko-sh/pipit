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

package debug_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const pkasmHeader = "; pkasm - pipit bytecode assembly\n\n"

func TestDisassembleAssemblyEmptyFileSetPrintsOnlyTheHeader(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		cfs  *program.CompiledFileSet
	}{
		{name: "nil root", cfs: program.NewCompiledFileSet(nil, nil, nil, nil)},
		{name: "root without body or children", cfs: program.NewCompiledFileSet(&program.CompiledFunction{Name: "root"}, map[string]uint16{}, nil, nil)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, pkasmHeader, debug.DisassembleAssembly(tc.cfs))
		})
	}
}

func TestDisassembleAssemblyListsRootVarInitAndChildren(t *testing.T) {
	t.Parallel()
	grandchild := &program.CompiledFunction{Name: "", Body: []isa.Instruction{isa.NewInstruction(isa.OpLoadIntConst, 0, 0, 0)}}
	child := &program.CompiledFunction{
		Name:      "helper",
		Body:      []isa.Instruction{isa.NewInstruction(isa.OpLoadIntConst, 0, 0, 0)},
		Functions: []*program.CompiledFunction{grandchild},
	}
	varinit := &program.CompiledFunction{Name: "varinit", Body: []isa.Instruction{isa.NewInstruction(isa.OpLoadIntConst, 0, 0, 0)}}
	root := &program.CompiledFunction{
		Name:                 "root",
		Body:                 []isa.Instruction{isa.NewInstruction(isa.OpAddInt, 0, 1, 2)},
		Functions:            []*program.CompiledFunction{child},
		VariableInitFunction: varinit,
	}
	cfs := program.NewCompiledFileSet(root, map[string]uint16{"helper": 0}, nil, varinit)

	out := debug.DisassembleAssembly(cfs)
	require.True(t, strings.HasPrefix(out, pkasmHeader))
	require.Contains(t, out, "\n; function <root>\n")
	require.Contains(t, out, "\n; function <varinit>\n")
	require.Contains(t, out, "\n; function helper\n")
	require.Contains(t, out, "\n  ; function <anonymous>\n", "grandchildren are indented one level")
	require.Less(t, strings.Index(out, "<root>"), strings.Index(out, "<varinit>"))
	require.Less(t, strings.Index(out, "<varinit>"), strings.Index(out, "helper"))
	require.Less(t, strings.Index(out, "function helper"), strings.Index(out, "<anonymous>"))
	require.Contains(t, out, fmt.Sprintf("%04d  %-26s %3d %3d %3d", 0, isa.InstructionDisplayName(root.Body[0]), 0, 1, 2))
}

func wide(op isa.Opcode, a uint8, index uint16) isa.Instruction {
	lo, hi := isa.SplitWide(index)
	return isa.NewInstruction(op, a, lo, hi)
}

func wideTier1(subOp isa.SubOpcode, index uint16) isa.Instruction {
	lo, hi := isa.SplitWide(index)
	return isa.NewTier1Instruction(subOp, lo, hi)
}

func annotatedFunction() *program.CompiledFunction {
	helper := &program.CompiledFunction{Name: "helper", Body: nil}
	anonymous := &program.CompiledFunction{Name: "", Body: nil}
	files := []string{"main.go", "util.go"}
	compiledFunction := &program.CompiledFunction{
		DebugVarTable:    &program.DebugVarTable{Entries: nil},
		Name:             "main",
		SourceFile:       "main.go",
		ParameterKinds:   []isa.RegisterKind{isa.RegisterInt},
		ResultKinds:      []isa.RegisterKind{isa.RegisterInt, isa.RegisterString},
		IsVariadic:       true,
		IntConstants:     []int64{1, -2},
		FloatConstants:   []float64{1.5},
		StringConstants:  []string{"hi", strings.Repeat("a", program.MaxDisassembleStringLen+1)},
		BoolConstants:    []bool{true},
		UintConstants:    []uint64{3},
		ComplexConstants: []complex128{1 + 2i},
		Functions:        []*program.CompiledFunction{helper, anonymous},
		CallSites: []program.CallSite{
			{FunctionIndex: 0},
			{IsClosure: true, ClosureRegister: 3},
			{FunctionIndex: 99},
			{IsClosure: true, ClosureRegister: 4},
			{IsMethod: true, NativeRegister: 2},
			{NativeRegister: 1},
		},
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpLoadIntConst, 0, 0, 0), isa.NewInstruction(isa.OpAddInt, 0, 0, 1), wideTier1(isa.SubOpCall, 0), wideTier1(isa.SubOpCall, 1), wideTier1(isa.SubOpTailCall, 2), wideTier1(isa.SubOpCallIIFE, 7), wideTier1(isa.SubOpCallNative, 3), wideTier1(isa.SubOpCallNative, 4), wideTier1(isa.SubOpCallNative, 5), wideTier1(isa.SubOpCallNative, 9), wideTier1(isa.SubOpCallMethod, 1), wideTier1(isa.SubOpCallMethodInlineable, 1), isa.NewTier1Instruction(isa.SubOpCallBuiltin, 0, 0), wide(isa.OpMakeClosure, 0, 0), wide(isa.OpMakeClosure, 0, 1), wide(isa.OpMakeClosure, 0, 5)},
	}
	compiledFunction.NumRegisters[isa.RegisterInt] = 2
	compiledFunction.NumRegisters[isa.RegisterGeneral] = 1
	positions := make([]program.SourcePosition, len(compiledFunction.Body))
	positions[0] = pos(0, 1, 1)
	positions[1] = pos(0, 1, 4)
	positions[2] = pos(0, 2, 1)
	positions[3] = pos(1, 3, 1)
	compiledFunction.DebugSourceMap = &program.SourceMap{Files: &files, Positions: positions}
	return compiledFunction
}

func TestDisassembleFunctionAssemblyHeaderAndConstants(t *testing.T) {
	t.Parallel()
	out := debug.DisassembleFunctionAssembly(annotatedFunction())

	separator := "; " + strings.Repeat("═", 61) + "\n"
	fences := strings.Count(out, separator)
	require.GreaterOrEqual(t, fences, 2, "the header is fenced top and bottom")
	require.Zero(t, fences%2, "every function block is fenced top and bottom")
	for _, line := range []string{
		"; function main\n",
		";   source:    main.go\n",
		";   registers: int=2 general=1\n",
		";   params:    (int)\n",
		";   returns:   (int, string)\n",
		";   variadic:  true\n",
		"; constants:\n",
		";   ints:    [0]=1  [1]=-2\n",
		";   floats:  [0]=1.5\n",
		`;   strings: [0]="hi"  [1]="` + strings.Repeat("a", program.TruncatedDisassembleStringLen) + `..."` + "\n",
		";   bools:   [0]=true\n",
		";   uints:   [0]=3\n",
		";   complex: [0]=(1+2i)\n",
	} {
		require.Contains(t, out, line)
	}
}

func TestDisassembleFunctionAssemblySourceAnnotations(t *testing.T) {
	t.Parallel()
	compiledFunction := annotatedFunction()
	out := debug.DisassembleFunctionAssembly(compiledFunction)

	fileChange := func(file string, line int) string { return fmt.Sprintf("%52s; %s:%d\n", "", file, line) }
	sameFile := func(line int) string { return fmt.Sprintf("%52s; :%d\n", "", line) }

	require.Equal(t, 1, strings.Count(out, fileChange("main.go", 1)), "the first instruction names its file")
	require.Equal(t, 1, strings.Count(out, sameFile(2)), "a line change within the file omits the name")
	require.Equal(t, 1, strings.Count(out, fileChange("util.go", 3)), "a file change names the new file")
	require.Equal(t, 3, strings.Count(out, "; main.go:1")+strings.Count(out, "; :2")+strings.Count(out, "; util.go:3"))
	require.NotContains(t, out, "; :1\n", "pc 1 shares pc 0's line and is not re-annotated")

	instruction := func(pc int) string {
		instr := compiledFunction.Body[pc]
		return fmt.Sprintf("%04d  %-26s %3d %3d %3d", pc, isa.InstructionDisplayName(instr), instr.A, instr.B, instr.C)
	}
	require.Less(t, strings.Index(out, fileChange("main.go", 1)), strings.Index(out, instruction(0)))
	require.Less(t, strings.Index(out, instruction(1)), strings.Index(out, sameFile(2)))
	require.Less(t, strings.Index(out, sameFile(2)), strings.Index(out, instruction(2)))
	require.Less(t, strings.Index(out, instruction(2)), strings.Index(out, fileChange("util.go", 3)))
	require.Less(t, strings.Index(out, fileChange("util.go", 3)), strings.Index(out, instruction(3)))
	for pc := range compiledFunction.Body {
		require.Contains(t, out, instruction(pc), "pc %d", pc)
	}
}

func TestDisassembleFunctionAssemblyWithoutSourceMapHasNoAnnotations(t *testing.T) {
	t.Parallel()
	compiledFunction := annotatedFunction()
	compiledFunction.DebugSourceMap = nil
	out := debug.DisassembleFunctionAssembly(compiledFunction)
	require.NotContains(t, out, "; main.go:")
	require.NotContains(t, out, "; util.go:")
	require.NotContains(t, out, "; :")
}

func TestDisassembleFunctionAssemblyCallComments(t *testing.T) {
	t.Parallel()
	out := debug.DisassembleFunctionAssembly(annotatedFunction())
	cases := []struct {
		name string
		want string
	}{
		{name: "call resolves to a named function", want: "; call helper (site 0)"},
		{name: "call through a closure register", want: "; call closure general[3] (site 1)"},
		{name: "tail call with unknown function index", want: "; tail call (site 2)"},
		{name: "iife with out-of-range site", want: "; iife (site 7)"},
		{name: "native call through a closure", want: "; call closure general[4] (site 3)"},
		{name: "native method call", want: "; call native method general[2] (site 4)"},
		{name: "plain native call", want: "; call native general[1] (site 5)"},
		{name: "native call with out-of-range site", want: "; call native\n"},
		{name: "method call", want: "; call method (site 1)"},
		{name: "inlineable method call", want: "; call method inlineable (site 1)"},
		{name: "builtin call", want: "; call builtin"},
		{name: "closure over a named function", want: "; closure helper (func 0)"},
		{name: "closure over an anonymous function", want: "; closure <anonymous> (func 1)"},
		{name: "closure with unknown function index", want: "; closure (func 5)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Contains(t, out, tc.want)
		})
	}
	require.Contains(t, out, "\n  ; function helper\n", "children follow the parent, indented")
	require.Contains(t, out, "\n  ; function <anonymous>\n")
}

func TestDisassembleFunctionAssemblyMinimalFunction(t *testing.T) {
	t.Parallel()
	out := debug.DisassembleFunctionAssembly(&program.CompiledFunction{Name: "", Body: nil})
	require.Contains(t, out, "; function <anonymous>\n")
	require.Contains(t, out, ";   params:    (none)\n")
	require.Contains(t, out, ";   returns:   (none)\n")
	require.NotContains(t, out, "registers:")
	require.NotContains(t, out, "source:")
	require.NotContains(t, out, "variadic:")
	require.NotContains(t, out, "constants:")
	require.NotContains(t, out, "0000")
}
