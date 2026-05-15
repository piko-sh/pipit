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
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	goldenShapesDir = "../golden/testdata/shapes"
	shapeSourceName = "main.go"
)

type shapeFunction struct {
	shape            string
	compiledFunction *program.CompiledFunction
}

func compileGoldenShapes(t *testing.T) []shapeFunction {
	t.Helper()
	entries, err := os.ReadDir(goldenShapesDir)
	require.NoError(t, err, "reading golden shapes")
	var out []shapeFunction
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		source, readErr := os.ReadFile(filepath.Join(goldenShapesDir, entry.Name(), shapeSourceName))
		require.NoError(t, readErr)
		compiled, compileErr := app.NewService().CompileFileSet(context.Background(), map[string]string{shapeSourceName: string(source)})
		require.NoError(t, compileErr, "compiling shape %s", entry.Name())
		out = appendFunctions(out, entry.Name(), compiled.Root())
		out = appendFunctions(out, entry.Name(), compiled.VariableInitFunction())
	}
	require.NotEmpty(t, out, "golden shapes produced no functions")
	return out
}

func appendFunctions(out []shapeFunction, shape string, compiledFunction *program.CompiledFunction) []shapeFunction {
	if compiledFunction == nil {
		return out
	}
	out = append(out, shapeFunction{shape: shape, compiledFunction: compiledFunction})
	for _, child := range compiledFunction.Functions {
		out = appendFunctions(out, shape, child)
	}
	return out
}

func TestJumpTargetDecodersAgree(t *testing.T) {
	t.Parallel()
	functions := compileGoldenShapes(t)
	compared, decodedJumps := 0, 0
	for _, f := range functions {
		body := f.compiledFunction.Body
		if len(body) == 0 {
			continue
		}
		compared++
		require.Equalf(t, passes.BuildAllJumpTargets(body), f.compiledFunction.BuildJumpTargets(body),
			"%s/%s: the peephole and pass-level target sets differ", f.shape, f.compiledFunction.Name)
		for pc := range body {
			target, decoded := program.JumpTargetAt(body, pc)
			if !decoded {
				continue
			}
			decodedJumps++
			require.NotEqualf(t, isa.OpExt, body[pc].Op, "%s/%s pc %d: an extension word decoded as a jump", f.shape, f.compiledFunction.Name, pc)

			require.Truef(t, target >= 0 && target <= len(body), "%s/%s pc %d: target %d is outside the body", f.shape, f.compiledFunction.Name, pc, target)
			if target < len(body) {
				require.NotEqualf(t, isa.OpExt, body[target].Op, "%s/%s pc %d: target %d lands on an extension word", f.shape, f.compiledFunction.Name, pc, target)
			}
			layout, trailingNops := isa.JumpLayoutOf(body[pc])
			for pad := 1; pad <= trailingNops; pad++ {
				padPC := pc + layout.WordCount() + pad - 1
				require.Truef(t, padPC < len(body) && body[padPC].Op == isa.OpNop,
					"%s/%s pc %d: layout promises %d nop(s) after the jump but word %d is %v", f.shape, f.compiledFunction.Name, pc, trailingNops, padPC, body[padPC])
			}
		}
	}
	t.Logf("compared %d functions; decoded %d jumps", compared, decodedJumps)
	require.Positive(t, decodedJumps, "the golden shapes must exercise the decoder")
}
