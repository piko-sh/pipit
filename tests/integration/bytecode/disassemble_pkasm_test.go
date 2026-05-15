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

//go:build integration

package bytecode_test

import (
	"strings"
	"testing"

	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/engine/program"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

func TestDisassembleAssembly_header(t *testing.T) {
	t.Parallel()
	root := &program.CompiledFunction{Name: "<root>"}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, "; pkasm - pipit bytecode assembly")
}

func TestDisassembleAssembly_singleFunction(t *testing.T) {
	t.Parallel()
	child := newBytecodeBuilder().
		intRegisters(3).
		stringRegisters(1).
		Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 42).
		Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3ReturnVoid)).
		build()
	child.Name = "main"
	child.IntConstants = []int64{42}

	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{child},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, "; function main")
	assert.Contains(t, output, "int=3")
	assert.Contains(t, output, "string=1")
	assert.Contains(t, output, "LOAD_INT_CONST_SMALL")
	assert.Contains(t, output, "RETURN_VOID")
	assert.Contains(t, output, "; constants:")
	assert.Contains(t, output, "[0]=42")
}

func TestDisassembleAssembly_callSiteResolution(t *testing.T) {
	t.Parallel()
	fibonacci := &program.CompiledFunction{Name: "fibonacci"}

	b := newBytecodeBuilder()
	functionIndex := b.addSubFunction(fibonacci)
	siteIndex := b.AddCallSite(&program.CallSite{FunctionIndex: functionIndex})
	lo := uint8(siteIndex & 0xFF)
	hi := uint8(siteIndex >> 8)
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpCall), lo, hi)
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3ReturnVoid))
	b.intRegisters(3)

	main := b.build()
	main.Name = "main"

	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{main},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, "call fibonacci (site 0)")
}

func TestDisassembleAssembly_nativeCallComment(t *testing.T) {
	t.Parallel()
	b := newBytecodeBuilder()
	siteIndex := b.AddCallSite(&program.CallSite{IsNative: true, NativeRegister: 2})
	lo := uint8(siteIndex & 0xFF)
	hi := uint8(siteIndex >> 8)
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpCallNative), lo, hi)
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3ReturnVoid))
	b.generalRegisters(3)

	fn := b.build()
	fn.Name = "main"

	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{fn},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, "call native general[2] (site 0)")
}

func TestDisassembleAssembly_closureComment(t *testing.T) {
	t.Parallel()
	closure := &program.CompiledFunction{Name: "closure$1"}
	b := newBytecodeBuilder()
	functionIndex := b.addSubFunction(closure)
	lo := uint8(functionIndex & 0xFF)
	hi := uint8(functionIndex >> 8)
	b.Emit(isa.OpMakeClosure, 0, lo, hi)
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3ReturnVoid))
	b.generalRegisters(2)

	fn := b.build()
	fn.Name = "main"

	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{fn},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, "closure closure$1 (func 0)")
}

func TestDisassembleAssembly_nestedFunctionsIndented(t *testing.T) {
	t.Parallel()
	inner := &program.CompiledFunction{
		Name: "inner",
		Body: []isa.Instruction{
			{Op: isa.OpDrillTier1, A: uint8(isa.SubOpDrillTier2), B: uint8(isa.SubOpTier2DrillTier3), C: uint8(isa.SubOpTier3ReturnVoid)},
		},
	}
	outer := &program.CompiledFunction{
		Name:      "outer",
		Functions: []*program.CompiledFunction{inner},
		Body: []isa.Instruction{
			{Op: isa.OpDrillTier1, A: uint8(isa.SubOpDrillTier2), B: uint8(isa.SubOpTier2DrillTier3), C: uint8(isa.SubOpTier3ReturnVoid)},
		},
	}

	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{outer},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	lines := strings.SplitSeq(output, "\n")
	foundOuterHeader := false
	foundInnerHeader := false
	for line := range lines {
		if strings.Contains(line, "; function outer") {
			foundOuterHeader = true
			assert.False(t, strings.HasPrefix(line, "  "),
				"outer function should not be indented")
		}
		if strings.Contains(line, "; function inner") {
			foundInnerHeader = true
			assert.True(t, strings.HasPrefix(line, "  "),
				"inner function should be indented")
		}
	}
	assert.True(t, foundOuterHeader, "should contain outer function header")
	assert.True(t, foundInnerHeader, "should contain inner function header")
}

func TestDisassembleAssembly_emptyBody(t *testing.T) {
	t.Parallel()
	fn := &program.CompiledFunction{Name: "empty"}
	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{fn},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, "; function empty")
	assert.NotContains(t, output, "0000")
}

func TestDisassembleAssembly_paramAndReturnKinds(t *testing.T) {
	t.Parallel()
	fn := &program.CompiledFunction{
		Name:           "add",
		ParameterKinds: []isa.RegisterKind{isa.RegisterInt, isa.RegisterInt},
		ResultKinds:    []isa.RegisterKind{isa.RegisterInt},
		Body: []isa.Instruction{
			{Op: isa.OpDrillTier1, A: uint8(isa.SubOpDrillTier2), B: uint8(isa.SubOpTier2DrillTier3), C: uint8(isa.SubOpTier3ReturnVoid)},
		},
	}
	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{fn},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, ";   params:    (int, int)")
	assert.Contains(t, output, ";   returns:   (int)")
}

func TestDisassembleAssembly_variadic(t *testing.T) {
	t.Parallel()
	fn := &program.CompiledFunction{
		Name:           "sprintf",
		ParameterKinds: []isa.RegisterKind{isa.RegisterString, isa.RegisterGeneral},
		IsVariadic:     true,
		Body: []isa.Instruction{
			{Op: isa.OpDrillTier1, A: uint8(isa.SubOpDrillTier2), B: uint8(isa.SubOpTier2DrillTier3), C: uint8(isa.SubOpTier3ReturnVoid)},
		},
	}
	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{fn},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, ";   variadic:  true")
}

func TestDisassembleAssembly_varInitFunction(t *testing.T) {
	t.Parallel()
	varInit := &program.CompiledFunction{
		Name: "<varinit>",
		Body: []isa.Instruction{
			{Op: isa.OpDrillTier1, A: uint8(isa.SubOpLoadIntConstSmall), B: 0, C: 10},
			{Op: isa.OpDrillTier1, A: uint8(isa.SubOpDrillTier2), B: uint8(isa.SubOpTier2DrillTier3), C: uint8(isa.SubOpTier3ReturnVoid)},
		},
	}

	root := &program.CompiledFunction{Name: "<root>"}
	cfs := program.NewCompiledFileSet(root, nil, nil, varInit)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, "; function <varinit>")
	assert.Contains(t, output, "LOAD_INT_CONST_SMALL")
}

func TestDisassembleAssembly_sourceLineAnnotations(t *testing.T) {
	t.Parallel()
	files := []string{"main.go"}
	fn := &program.CompiledFunction{
		Name: "main",
		Body: []isa.Instruction{
			{Op: isa.OpDrillTier1, A: uint8(isa.SubOpLoadIntConstSmall), B: 0, C: 42},
			{Op: isa.OpDrillTier1, A: uint8(isa.SubOpDrillTier2), B: uint8(isa.SubOpTier2DrillTier3), C: uint8(isa.SubOpTier3ReturnVoid)},
		},
		DebugSourceMap: &program.SourceMap{
			Files: &files,
			Positions: []program.SourcePosition{
				{FileID: 0, Line: 4, Column: 1},
				{FileID: 0, Line: 5, Column: 1},
			},
		},

		DebugVarTable: &program.DebugVarTable{Entries: nil},
	}

	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{fn},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, "main.go:4")
	assert.Contains(t, output, ":5")
}

func TestDisassembleFunctionAssembly_standalone(t *testing.T) {
	t.Parallel()
	fn := newBytecodeBuilder().
		intRegisters(2).
		Emit(isa.OpDrillTier1, uint8(isa.SubOpLoadIntConstSmall), 0, 1).
		Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3ReturnVoid)).
		build()
	fn.Name = "helper"

	output := debug.DisassembleFunctionAssembly(fn)

	assert.Contains(t, output, "; function helper")
	assert.Contains(t, output, "LOAD_INT_CONST_SMALL")
	assert.Contains(t, output, "RETURN_VOID")
}

func TestDisassembleAssembly_constantPools(t *testing.T) {
	t.Parallel()
	fn := &program.CompiledFunction{
		Name:             "constants",
		IntConstants:     []int64{42, -1},
		FloatConstants:   []float64{3.14},
		StringConstants:  []string{"hello"},
		BoolConstants:    []bool{true, false},
		UintConstants:    []uint64{255},
		ComplexConstants: []complex128{1 + 2i},
		Body: []isa.Instruction{
			{Op: isa.OpDrillTier1, A: uint8(isa.SubOpDrillTier2), B: uint8(isa.SubOpTier2DrillTier3), C: uint8(isa.SubOpTier3ReturnVoid)},
		},
	}

	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{fn},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, ";   ints:    [0]=42  [1]=-1")
	assert.Contains(t, output, ";   floats:  [0]=3.14")
	assert.Contains(t, output, ";   strings: [0]=\"hello\"")
	assert.Contains(t, output, ";   bools:   [0]=true  [1]=false")
	assert.Contains(t, output, ";   uints:   [0]=255")
	assert.Contains(t, output, ";   complex:")
}

func TestDisassembleAssembly_nilRoot(t *testing.T) {
	t.Parallel()
	cfs := program.NewCompiledFileSet(nil, nil, nil, nil)
	output := debug.DisassembleAssembly(cfs)
	require.Contains(t, output, "; pkasm")
}

func TestDisassembleAssembly_tailCallComment(t *testing.T) {
	t.Parallel()
	target := &program.CompiledFunction{Name: "recurse"}
	b := newBytecodeBuilder()
	functionIndex := b.addSubFunction(target)
	siteIndex := b.AddCallSite(&program.CallSite{FunctionIndex: functionIndex})
	lo := uint8(siteIndex & 0xFF)
	hi := uint8(siteIndex >> 8)
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpTailCall), lo, hi)
	b.intRegisters(1)

	fn := b.build()
	fn.Name = "main"

	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{fn},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, "tail call recurse (site 0)")
}

func TestDisassembleAssembly_closureCallSite(t *testing.T) {
	t.Parallel()
	b := newBytecodeBuilder()
	siteIndex := b.AddCallSite(&program.CallSite{IsClosure: true, ClosureRegister: 5})
	lo := uint8(siteIndex & 0xFF)
	hi := uint8(siteIndex >> 8)
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpCall), lo, hi)
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3ReturnVoid))
	b.generalRegisters(6)

	fn := b.build()
	fn.Name = "main"

	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{fn},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, "call closure general[5] (site 0)")
}

func TestDisassembleAssembly_methodCallComment(t *testing.T) {
	t.Parallel()
	b := newBytecodeBuilder()
	b.AddCallSite(&program.CallSite{IsMethod: true})
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpCallMethod), 0, 0)
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3ReturnVoid))

	fn := b.build()
	fn.Name = "main"

	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{fn},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, "call method (site 0)")
}

func TestDisassembleAssembly_builtinCallComment(t *testing.T) {
	t.Parallel()
	b := newBytecodeBuilder()
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpCallBuiltin), 0, 0)
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3ReturnVoid))

	fn := b.build()
	fn.Name = "main"

	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{fn},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, "call builtin")
}

func TestDisassembleAssembly_iifeComment(t *testing.T) {
	t.Parallel()
	iifeFn := &program.CompiledFunction{Name: "init$1"}
	b := newBytecodeBuilder()
	functionIndex := b.addSubFunction(iifeFn)
	siteIndex := b.AddCallSite(&program.CallSite{FunctionIndex: functionIndex})
	lo := uint8(siteIndex & 0xFF)
	hi := uint8(siteIndex >> 8)
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpCallIIFE), lo, hi)
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3ReturnVoid))

	fn := b.build()
	fn.Name = "main"

	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{fn},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, "iife init$1 (site 0)")
}

func TestDisassembleAssembly_existingCommentsPreserved(t *testing.T) {
	t.Parallel()
	b := newBytecodeBuilder()
	b.addIntConst(100)
	b.Emit(isa.OpLoadIntConst, 0, 0, 0)
	b.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2DrillTier3), uint8(isa.SubOpTier3ReturnVoid))
	b.intRegisters(2)

	fn := b.build()
	fn.Name = "main"

	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{fn},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, "ints[0] = 100")
}

func TestDisassembleAssembly_sourceFileInHeader(t *testing.T) {
	t.Parallel()
	fn := &program.CompiledFunction{
		Name:       "main",
		SourceFile: "cmd/server/main.go",
		Body: []isa.Instruction{
			{Op: isa.OpDrillTier1, A: uint8(isa.SubOpDrillTier2), B: uint8(isa.SubOpTier2DrillTier3), C: uint8(isa.SubOpTier3ReturnVoid)},
		},
	}
	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{fn},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.Contains(t, output, ";   source:    cmd/server/main.go")
}

func TestDisassembleAssembly_sourceFileOmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	fn := &program.CompiledFunction{
		Name: "main",
		Body: []isa.Instruction{
			{Op: isa.OpDrillTier1, A: uint8(isa.SubOpDrillTier2), B: uint8(isa.SubOpTier2DrillTier3), C: uint8(isa.SubOpTier3ReturnVoid)},
		},
	}
	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{fn},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	assert.NotContains(t, output, ";   source:")
}

func TestDisassembleAssembly_deepNesting(t *testing.T) {
	t.Parallel()
	level3 := &program.CompiledFunction{
		Name: "level3",
		Body: []isa.Instruction{{Op: isa.OpDrillTier1, A: uint8(isa.SubOpDrillTier2), B: uint8(isa.SubOpTier2DrillTier3), C: uint8(isa.SubOpTier3ReturnVoid)}},
	}
	level2 := &program.CompiledFunction{
		Name:      "level2",
		Functions: []*program.CompiledFunction{level3},
		Body:      []isa.Instruction{{Op: isa.OpDrillTier1, A: uint8(isa.SubOpDrillTier2), B: uint8(isa.SubOpTier2DrillTier3), C: uint8(isa.SubOpTier3ReturnVoid)}},
	}
	level1 := &program.CompiledFunction{
		Name:      "level1",
		Functions: []*program.CompiledFunction{level2},
		Body:      []isa.Instruction{{Op: isa.OpDrillTier1, A: uint8(isa.SubOpDrillTier2), B: uint8(isa.SubOpTier2DrillTier3), C: uint8(isa.SubOpTier3ReturnVoid)}},
	}
	root := &program.CompiledFunction{
		Name:      "<root>",
		Functions: []*program.CompiledFunction{level1},
	}
	cfs := program.NewCompiledFileSet(root, nil, nil, nil)

	output := debug.DisassembleAssembly(cfs)

	lines := strings.SplitSeq(output, "\n")
	for line := range lines {
		if strings.Contains(line, "; function level2") {
			assert.True(t, strings.HasPrefix(line, "  "),
				"level2 should be indented 2 spaces (1 level)")
		}
		if strings.Contains(line, "; function level3") {
			assert.True(t, strings.HasPrefix(line, "    "),
				"level3 should be indented 4 spaces (2 levels)")
		}
	}
}
