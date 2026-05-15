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
	"pipit.sh/pipit/internal/compile/inline"
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/isa"
)

const (
	inlinerDebugSource = `package main

type pair struct {
	a int
	b int
}

func sum(p pair) int {
	return p.a + p.b
}

func run() int {
	p := pair{a: 1, b: 2}
	total := 0
	for i := 0; i < 10; i++ {
		total = total + sum(p)
	}
	return total
}
`
)

func compileInlinerDebugFileSet(t *testing.T) *program.CompiledFileSet {
	t.Helper()
	service := app.NewService(app.WithDebugInfo())
	cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": inlinerDebugSource})
	require.NoError(t, err)
	return cfs
}

func findInlineAnnotation(t *testing.T, compiledFunction *program.CompiledFunction) (int, program.PeepholeAnnotation) {
	t.Helper()
	for pc := range compiledFunction.Body {
		annotation := passes.PeepholeAnnotationAt(compiledFunction, pc)
		if annotation.Kind == passes.PeepholeRewriteInline {
			return pc, annotation
		}
	}
	t.Fatalf("function %q was not inlined into under debug info", compiledFunction.Name)
	return 0, program.PeepholeAnnotation{}
}

func TestInlinerKeepsSourceMapParallel(t *testing.T) {
	t.Parallel()
	cfs := compileInlinerDebugFileSet(t)
	root := cfs.Root()
	require.NoError(t, inline.VerifySourceMapsParallel(root, "test"))

	run := program.ExportFunctions(root)[cfs.Entrypoints()["run"]]
	require.NotNil(t, run.DebugSourceMap)
	require.Len(t, run.DebugSourceMap.Positions, len(run.Body))

	bodyStart, annotation := findInlineAnnotation(t, run)
	_, line, _ := run.DebugSourcePosition(bodyStart)
	require.Equal(t, 9, line, "first inlined instruction must carry the callee's source line")
	_, callLine, _ := run.DebugSourcePosition(annotation.Origin)
	require.Equal(t, 16, callLine, "the replaced opCall keeps the call-site line")
	_, synthLine, _ := run.DebugSourcePosition(bodyStart - 1)
	require.Equal(t, 16, synthLine, "synthetic trampoline instructions inherit the call-site line")
	require.Equal(t, int(cfs.Entrypoints()["sum"]), annotation.OriginFunction)
}

func TestSpliceRollbackRestoresSourceMap(t *testing.T) {
	t.Parallel()
	files := []string{"main.go"}
	caller := &program.CompiledFunction{
		Body:           []isa.Instruction{isa.NewInstruction(isa.OpNop, 0, 0, 0)},
		DebugSourceMap: &program.SourceMap{Files: &files, Positions: []program.SourcePosition{{Line: 3}}},
	}
	snapshot := inline.CaptureSpliceSnapshot(caller)
	caller.Body = append(caller.Body, isa.NewInstruction(isa.OpNop, 0, 0, 0))
	caller.DebugSourceMap.Positions = append(caller.DebugSourceMap.Positions, program.SourcePosition{Line: 9})
	inline.RestoreSpliceSnapshot(caller, snapshot)
	require.Len(t, caller.Body, 1)
	require.Len(t, caller.DebugSourceMap.Positions, 1)
}

func TestVerifySourceMapsParallelReportsDrift(t *testing.T) {
	t.Parallel()
	files := []string{"main.go"}
	root := &program.CompiledFunction{
		Name:           "root",
		Body:           []isa.Instruction{isa.NewInstruction(isa.OpNop, 0, 0, 0), isa.NewInstruction(isa.OpNop, 0, 0, 0)},
		DebugSourceMap: &program.SourceMap{Files: &files, Positions: []program.SourcePosition{{Line: 1}}},
	}
	err := inline.VerifySourceMapsParallel(root, "probe")
	require.ErrorIs(t, err, inline.ErrSourceMapNotParallel)
	require.Contains(t, err.Error(), "probe")
}
