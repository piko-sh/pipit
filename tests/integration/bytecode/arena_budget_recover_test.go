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
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"

	"github.com/stretchr/testify/require"
)

const (
	arenaBudgetProbeSource = `package main

func accumulate() int {
	total := 0
	for i := 0; i < 1000000; i++ {
		scratch := make([]int, 4096)
		total += len(scratch)
	}
	return total
}

var overBudget = accumulate()

func run() int {
	return overBudget
}
`
)

func TestArenaBudgetDuringVarInitSurfacesAsError(t *testing.T) {
	t.Parallel()
	const tinyBudget = 1 << 20
	ctx := context.Background()

	compile := func(t *testing.T) (*app.Service, *program.CompiledFileSet) {
		t.Helper()
		service := app.NewService(app.WithMaxArenaSizeBytes(tinyBudget))
		compiled, err := service.CompileFileSet(ctx, map[string]string{"main.go": arenaBudgetProbeSource})
		require.NoError(t, err)
		return service, compiled
	}

	t.Run("ExecuteEntrypoint", func(t *testing.T) {
		t.Parallel()
		service, compiled := compile(t)
		_, err := service.ExecuteEntrypoint(ctx, compiled, "run")
		require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
	})

	t.Run("ExecuteInits", func(t *testing.T) {
		t.Parallel()
		service, compiled := compile(t)
		err := service.ExecuteInits(ctx, compiled)
		require.ErrorIs(t, err, fault.ErrArenaBudgetExceeded)
	})
}
