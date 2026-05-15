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
	"os"
	"runtime"
	"runtime/pprof"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"

	"pipit.sh/pipit/sdk/stdlib"
)

const (
	fibInnerLoopSource = `
	package main

	const fibTermsToCompute = 100000
	const fibModulusBitmask = (1 << 64) - 1

	func computeFib(n int) uint64 {
		previous := uint64(0)
		current := uint64(1)
		for index := 0; index < n; index++ {
			next := (previous + current) & fibModulusBitmask
			previous = current
			current = next
		}
		return current
	}

	func RunInner(k int) uint64 {
		var last uint64
		for index := 0; index < k; index++ {
			last = computeFib(fibTermsToCompute)
		}
		return last
	}

	func EntrypointRun() uint64 {
		return RunInner(5)
	}
	`
)

func TestProfileFibInnerLoop(t *testing.T) {
	if testing.Short() {
		t.Skip("profile run")
	}
	service := app.NewService(app.WithMaxCallDepth(200000))
	symbolRegistry := symtab.NewSymbolRegistry(stdlib.Exports())
	service.UseSymbols(symbolRegistry)

	compiledFileSet, err := service.CompileFileSet(context.Background(), map[string]string{
		"main.go": fibInnerLoopSource,
	})
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	_, err = service.ExecuteEntrypoint(context.Background(), compiledFileSet, "EntrypointRun")
	if err != nil {
		t.Fatalf("warmup execute: %v", err)
	}

	cpuFile, err := os.Create("/tmp/pipit_fib.prof")
	if err != nil {
		t.Fatalf("create profile: %v", err)
	}
	defer cpuFile.Close()

	memBefore := readAllocs()

	if err := pprof.StartCPUProfile(cpuFile); err != nil {
		t.Fatalf("start cpu profile: %v", err)
	}

	for range 5 {
		_, err := service.ExecuteEntrypoint(context.Background(), compiledFileSet, "EntrypointRun")
		if err != nil {
			t.Fatalf("execute: %v", err)
		}
	}

	pprof.StopCPUProfile()

	memAfter := readAllocs()
	t.Logf("Allocs delta: total=%d bytes, count=%d", memAfter.totalAlloc-memBefore.totalAlloc, memAfter.mallocs-memBefore.mallocs)

	memFile, err := os.Create("/tmp/pipit_fib.memprof")
	if err != nil {
		t.Fatalf("create memprof: %v", err)
	}
	defer memFile.Close()

	runtime.GC()
	if err := pprof.WriteHeapProfile(memFile); err != nil {
		t.Fatalf("write heap profile: %v", err)
	}

	t.Logf("wrote /tmp/pipit_fib.prof (CPU) and /tmp/pipit_fib.memprof (heap)")
}

type allocSnapshot struct {
	totalAlloc uint64
	mallocs    uint64
}

func readAllocs() allocSnapshot {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return allocSnapshot{ms.TotalAlloc, ms.Mallocs}
}
