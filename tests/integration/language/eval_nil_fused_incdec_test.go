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

package language_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
)

func nilFusedIncDecProgram(statement string) string {
	return `package main

type counters struct {
	signed   int
	unsigned uint
}

func exercise() (out string) {
	var p *counters
	defer func() {
		if recovered, ok := recover().(error); ok {
			out = recovered.Error()
		}
	}()
	` + statement + `
	return "no panic"
}

func run() string {
	return exercise()
}
`
}

func TestEvalNilFusedIncDecRaisesNilDereference(t *testing.T) {
	t.Parallel()

	const wantMessage = "runtime error: invalid memory address or nil pointer dereference"
	ctx := context.Background()

	tests := []struct {
		name      string
		statement string
	}{
		{name: "int increment", statement: "p.signed++"},
		{name: "int decrement", statement: "p.signed--"},
		{name: "uint increment", statement: "p.unsigned++"},
		{name: "uint decrement", statement: "p.unsigned--"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			service := app.NewService()
			compiled, err := service.CompileFileSet(ctx, map[string]string{
				"main.go": nilFusedIncDecProgram(testCase.statement),
			})
			require.NoError(t, err)

			result, err := service.ExecuteEntrypoint(ctx, compiled, "run")
			require.NoError(t, err)

			message, ok := result.(string)
			require.Truef(t, ok, "entrypoint result was %T, want string", result)
			require.Containsf(t, message, wantMessage,
				"fused nil %s did not raise the nil-dereference panic", testCase.statement)
		})
	}
}

func TestEvalNonNilFusedIncDecMutatesInPlace(t *testing.T) {
	t.Parallel()

	const program = `package main

type counters struct {
	signed   int
	unsigned uint
}

func run() int {
	c := &counters{signed: 10, unsigned: 3}
	c.signed++
	c.signed++
	c.signed--
	c.unsigned++
	return c.signed*100 + int(c.unsigned)
}
`
	ctx := context.Background()
	service := app.NewService()
	compiled, err := service.CompileFileSet(ctx, map[string]string{"main.go": program})
	require.NoError(t, err)

	result, err := service.ExecuteEntrypoint(ctx, compiled, "run")
	require.NoError(t, err)
	require.Equal(t, 1104, result,
		"fused inc/dec on a non-nil receiver produced %v; want signed=11, unsigned=4", result)
}
