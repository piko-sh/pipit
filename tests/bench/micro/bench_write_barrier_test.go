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
	"runtime"
	"runtime/debug"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
)

type pressureNode struct {
	next  *pressureNode
	value []byte
}

var pressureSink []byte

func startMarkingPressure() func() {
	previous := debug.SetGCPercent(1)
	heap := make([]*pressureNode, 1<<20)
	for index := range heap {
		heap[index] = &pressureNode{next: nil, value: nil}
		if index > 0 {
			heap[index].next = heap[index-1]
		}
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				pressureSink = make([]byte, 4096)
			}
		}
	}()
	return func() {
		close(stop)
		<-done
		runtime.KeepAlive(heap)
		debug.SetGCPercent(previous)
	}
}

func BenchmarkWriteBarrierGuardedOps(b *testing.B) {
	programs := []struct {
		name   string
		source string
	}{
		{name: "string_move", source: `package main

var words = []string{"alpha", "beta", "gamma", "delta"}

func run() int {
	a, c := words[0], words[1]
	n := 0
	for i := 0; i < 1000; i++ {
		a, c = c, a
		n += len(a)
	}
	return n
}
`},
		{name: "load_string_const", source: `package main

func run() int {
	n := 0
	for i := 0; i < 1000; i++ {
		s := "abc"
		if i&1 == 0 {
			s = "defgh"
		}
		n += len(s)
	}
	return n
}
`},
		{name: "typed_move", source: `package main

func run() int {
	xs := []int{1, 2, 3}
	ys := []int{4, 5}
	n := 0
	for i := 0; i < 1000; i++ {
		xs, ys = ys, xs
		n += len(xs)
	}
	return n
}
`},
		{name: "typed_subslice", source: `package main

func run() int {
	xs := make([]int, 16)
	n := 0
	for i := 0; i < 1000; i++ {
		v := xs[1 : i&7+2]
		n += len(v)
	}
	return n
}
`},
		{name: "string_slice", source: `package main

var text = "the quick brown fox jumps over"

func run() int {
	n := 0
	for i := 0; i < 1000; i++ {
		s := text[i&7 : i&7+4]
		n += len(s)
	}
	return n
}
`},
	}
	for _, mode := range []struct {
		name    string
		marking bool
	}{
		{name: "quiet", marking: false},
		{name: "marking", marking: true},
	} {
		b.Run(mode.name, func(b *testing.B) {
			for _, program := range programs {
				b.Run(program.name, func(b *testing.B) {
					service := app.NewService()
					cfs, err := service.CompileFileSet(b.Context(), map[string]string{"main.go": program.source})
					require.NoError(b, err)
					if mode.marking {
						defer startMarkingPressure()()
					}
					if !mode.marking {
						b.ReportAllocs()
					}
					var before runtime.MemStats
					runtime.ReadMemStats(&before)
					b.ResetTimer()
					for b.Loop() {
						_, err = service.ExecuteEntrypoint(b.Context(), cfs, "run")
						if err != nil {
							require.NoError(b, err)
						}
					}
					b.StopTimer()
					var after runtime.MemStats
					runtime.ReadMemStats(&after)
					b.ReportMetric(float64(after.NumGC-before.NumGC)/float64(b.N), "gc/op")
				})
			}
		})
	}
}
