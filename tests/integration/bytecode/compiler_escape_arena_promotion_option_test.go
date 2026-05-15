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

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"
)

const arenaPromotionSource = `package main

type P struct {
	A int
	B int
}

func local(n int) int {
	p := &P{A: n, B: 2}
	p.A++
	return p.A + p.B
}

func EntrypointRun() int { return local(1) }
`

func TestArenaPromotionOptionControlsEscapeAnnotations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		options        passes.Options
		wantAnnotation bool
	}{
		{name: "default options annotate", options: passes.DefaultOptions(), wantAnnotation: true},
		{name: "promotion off leaves nothing", options: withoutArenaPromotion(passes.DefaultOptions()), wantAnnotation: false},
	}
	reference := compileArenaPromotionProgram(t, passes.DefaultOptions())
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			functions := compileArenaPromotionProgram(t, tc.options)
			local := functions["local"]
			require.NotNil(t, local)
			require.Equal(t, tc.wantAnnotation, len(local.ArenaSafeAllocPCs) > 0, "ArenaSafeAllocPCs")
			require.Equal(t, reference["local"].Body, local.Body, "the option must not change the bytecode")
		})
	}
}

func withoutArenaPromotion(options passes.Options) passes.Options {
	options.ArenaPromotion = false
	return options
}

func compileArenaPromotionProgram(t *testing.T, options passes.Options) map[string]*program.CompiledFunction {
	t.Helper()
	service := app.NewService(app.WithOptimisations(options))
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": arenaPromotionSource})
	require.NoError(t, err)
	byName := make(map[string]*program.CompiledFunction)
	for _, fn := range program.ExportFunctions(compiled.Root()) {
		byName[fn.Name] = fn
	}
	return byName
}
