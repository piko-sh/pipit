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
	"context"
	"testing"

	"pipit.sh/pipit/internal/app"

	"pipit.sh/pipit/internal/compile/escape"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/debug"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

const inPlaceHeaderReuseSource = `package main

type point struct {
	x int
	y int
}

var sink []point

func keep(ps []point) { sink = ps }

func build(n int) int {
	ps := []point{}
	for i := 0; i < n; i++ {
		ps = append(ps, point{x: i, y: i})
	}
	return len(ps)
}

func leak(n int) int {
	ps := []point{}
	for i := 0; i < n; i++ {
		ps = append(ps, point{x: i, y: i})
	}
	keep(ps)
	return len(ps)
}

func ret(n int) []point {
	ps := []point{}
	for i := 0; i < n; i++ {
		ps = append(ps, point{x: i, y: i})
	}
	return ps
}

func EntrypointRun() int { return build(3) + leak(3) + len(ret(3)) }
`

func sameSlotAppendPCs(compiledFunction *program.CompiledFunction) []int {
	var pcs []int
	for pc, inst := range compiledFunction.Body {
		if escape.IsSameSlotAppendOp(inst.Op) && inst.A == inst.B {
			pcs = append(pcs, pc)
		}
	}
	return pcs
}

func TestEscapePassInPlaceHeaderReuseAnnotation(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": inPlaceHeaderReuseSource})
	require.NoError(t, err)

	build := findCompiledFunctionByName(t, compiled, "build")
	buildPCs := sameSlotAppendPCs(build)
	require.NotEmpty(t, buildPCs, "build must compile its append back into the same register; disasm:\n%s", debug.DisassembleAssembly(compiled))
	for _, pc := range buildPCs {
		require.True(t, build.InPlaceHeaderReusePCs[pc], "an unaliased local slice must be annotated at pc %d", pc)
	}

	for _, name := range []string{"leak", "ret"} {
		compiledFunction := findCompiledFunctionByName(t, compiled, name)
		for _, pc := range sameSlotAppendPCs(compiledFunction) {
			require.False(t, compiledFunction.InPlaceHeaderReusePCs[pc], "%s shares its header beyond the frame and must not be annotated at pc %d", name, pc)
		}
	}
}

func TestAnalyseGeneralRegisterAliasFreeRejectsCopies(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{Body: []isa.Instruction{
		{Op: isa.OpAppendInPlace, A: 1, B: 1, C: 2},
		{Op: isa.OpMoveGeneral, A: 3, B: 1},
	}}
	require.False(t, escape.AnalyseGeneralRegisterAliasFree(compiledFunction, 1))
	require.True(t, escape.AnalyseGeneralRegisterAliasFree(compiledFunction, 2))

	indexed := &program.CompiledFunction{Body: []isa.Instruction{
		{Op: isa.OpAppendInPlace, A: 1, B: 1, C: 2},
		{Op: isa.OpIndex, A: 4, B: 1, C: 0},
		{Op: isa.OpIndexSet, A: 1, B: 0, C: 2},
	}}
	require.True(t, escape.AnalyseGeneralRegisterAliasFree(indexed, 1))
	require.False(t, escape.AnalyseGeneralRegisterAliasFree(indexed, 2), "storing the slot as an element value shares it")

	param := &program.CompiledFunction{ParameterKinds: []isa.RegisterKind{isa.RegisterGeneral}, Body: []isa.Instruction{
		{Op: isa.OpAppendInPlace, A: 0, B: 0, C: 2},
	}}
	require.False(t, escape.AnalyseGeneralRegisterAliasFree(param, 0), "a parameter's header is shared with the caller")
}

func TestAppendInPlaceAliasingPreservesLen(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	result, err := service.Eval(context.Background(), `
type point struct{ x int }
func run() int {
	ps := make([]point, 0, 8)
	for i := 0; i < 3; i++ {
		ps = append(ps, point{x: i})
	}
	saved := ps
	ps = append(ps, point{x: 9})
	return len(saved)*100 + len(ps)*10 + saved[2].x
}
run()`)
	require.NoError(t, err)
	require.EqualValues(t, 342, result)
}

func TestAppendInPlaceReuseKeepsValues(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	result, err := service.Eval(context.Background(), `
type point struct{ x, y int }
func run() int {
	ps := make([]point, 0, 16)
	bs := make([]byte, 0, 16)
	for i := 0; i < 10; i++ {
		ps = append(ps, point{x: i, y: i * 2})
		bs = append(bs, byte('a'+i))
	}
	total := 0
	for _, p := range ps {
		total += p.x + p.y
	}
	return total*1000 + len(ps)*10 + int(bs[9]-'a')
}
run()`)
	require.NoError(t, err)
	require.EqualValues(t, 135109, result)
}
