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

//go:build bench

package bench

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib"
)

const nativeBuilderSource = `package main

import "strings"

func run() int {
	var sb strings.Builder
	p := &sb
	for i := 0; i < 100; i++ {
		p.WriteString("x")
	}
	return p.Len()
}
`

const nativeSprintSource = `package main

import "fmt"

func run() int {
	n := 0
	for i := 0; i < 100; i++ {
		n += len(fmt.Sprint("a", "b"))
	}
	return n
}
`

func benchNativeMethod(b *testing.B, src string) {
	b.Helper()
	service := app.NewService()
	service.UseSymbols(symtab.NewSymbolRegistry(stdlib.Exports()))
	ctx := context.WithoutCancel(b.Context())
	cfs, err := service.CompileFileSet(ctx, map[string]string{"main.go": src})
	require.NoError(b, err)
	b.ReportAllocs()
	for b.Loop() {
		_, err = service.ExecuteEntrypoint(ctx, cfs, "run")
		require.NoError(b, err)
	}
}

func BenchmarkNativeBuilderMethod(b *testing.B) { benchNativeMethod(b, nativeBuilderSource) }
func BenchmarkNativeSprint(b *testing.B)        { benchNativeMethod(b, nativeSprintSource) }
