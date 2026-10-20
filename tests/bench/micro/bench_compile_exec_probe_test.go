//go:build bench

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

package bench

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
)

func BenchmarkRecursiveFibExecOnly(b *testing.B) {
	const source = `package main
func fib(n int) int {
	if n < 2 { return n }
	return fib(n-1) + fib(n-2)
}
func EntrypointRun() int { return fib(20) }`
	service := app.NewService(app.WithMaxCallDepth(200000))
	compiled, err := service.CompileFileSet(context.Background(),
		map[string]string{"main.go": source})
	require.NoError(b, err, "compile")
	_, err = service.ExecuteEntrypoint(context.Background(), compiled, "EntrypointRun")
	require.NoError(b, err, "warmup")

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_, err := service.ExecuteEntrypoint(context.Background(), compiled, "EntrypointRun")
		if err != nil {
			require.NoError(b, err, "exec")
		}
	}
}
