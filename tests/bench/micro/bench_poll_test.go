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
	"reflect"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab"
)

const pollSpinSource = `package main

func run() int {
	total := 0
	for i := 0; i < 100000; i++ {
		total += i & 7
	}
	return total
}
`

const pollForeverSource = `package main

func run() int {
	total := 0
	for {
		total += total & 7
	}
}
`

func compilePollProgram(b *testing.B, source string, symbols *symtab.SymbolRegistry, options ...app.Option) (*app.Service, func(context.Context) error) {
	b.Helper()
	service := app.NewService(options...)
	if symbols != nil {
		service.UseSymbols(symbols)
	}
	cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	require.NoError(b, err)
	return service, func(ctx context.Context) error {
		_, err := service.ExecuteEntrypoint(ctx, cfs, "run")
		return err
	}
}

func BenchmarkPollSpinLoop(b *testing.B) {
	_, run := compilePollProgram(b, pollSpinSource, nil)
	b.ReportAllocs()
	for b.Loop() {
		if err := run(context.Background()); err != nil {
			require.NoError(b, err)
		}
	}
}

func BenchmarkPollNativeCallLoop(b *testing.B) {
	_, run := compilePollProgram(b, `package main

import "strings"

func run() int {
	count := 0
	for i := 0; i < 10000; i++ {
		if strings.HasPrefix("/api/v1/users", "/api") {
			count++
		}
	}
	return count
}
`, nativeSymbols())
	b.ReportAllocs()
	for b.Loop() {
		if err := run(context.Background()); err != nil {
			require.NoError(b, err)
		}
	}
}

func BenchmarkCallbackComparatorSort(b *testing.B) {
	symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{
		"host": {
			"Sort": reflect.ValueOf(func(values []int, less func(a, b int) bool) {
				sort.Slice(values, func(i, j int) bool { return less(values[i], values[j]) })
			}),
		},
	})
	const source = `package main

import "host"

func run() int {
	values := make([]int, 512)
	seed := 7
	for index := range values {
		seed = (seed*1103515245 + 12345) & 0x7fffffff
		values[index] = seed
	}
	host.Sort(values, func(a, b int) bool { return a < b })
	return values[0]
}
`
	for _, timeout := range []struct {
		name  string
		value time.Duration
	}{
		{name: "no_timeout", value: 0},
		{name: "timeout_1s", value: time.Second},
	} {
		b.Run(timeout.name, func(b *testing.B) {
			_, run := compilePollProgram(b, source, symbols, app.WithCallbackTimeout(timeout.value))
			b.ReportAllocs()
			for b.Loop() {
				if err := run(context.Background()); err != nil {
					require.NoError(b, err)
				}
			}
		})
	}
}

func BenchmarkParallelSpinVMs(b *testing.B) {
	_, run := compilePollProgram(b, pollSpinSource, nil)
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := run(context.Background()); err != nil {
				require.NoError(b, err)
			}
		}
	})
}

func BenchmarkGCWhileVMsSpin(b *testing.B) {
	_, run := compilePollProgram(b, pollForeverSource, nil)
	ctx, cancel := context.WithCancel(context.Background())
	var group sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		group.Go(func() {
			_ = run(ctx)
		})
	}
	time.Sleep(10 * time.Millisecond)
	started := time.Now()
	for b.Loop() {
		runtime.GC()
	}
	elapsed := time.Since(started)
	cancel()
	group.Wait()
	b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/gc")
}

func BenchmarkGCWhileGoSpins(b *testing.B) {
	ctx, cancel := context.WithCancel(context.Background())
	var group sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		group.Go(func() {
			total := 0
			for ctx.Err() == nil {
				for range 1 << 16 {
					total += total & 7
				}
			}
			runtime.KeepAlive(total)
		})
	}
	time.Sleep(10 * time.Millisecond)
	started := time.Now()
	for b.Loop() {
		runtime.GC()
	}
	elapsed := time.Since(started)
	cancel()
	group.Wait()
	b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/gc")
}
