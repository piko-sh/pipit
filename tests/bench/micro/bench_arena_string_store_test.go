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

	"pipit.sh/pipit/internal/app"
)

func benchmarkEntrypoint(b *testing.B, source string) {
	b.Helper()
	service := app.NewService()
	cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err = service.ExecuteEntrypoint(context.Background(), cfs, "run"); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMapSetArenaString(b *testing.B) {
	benchmarkEntrypoint(b, `package main

func run() int {
	m := map[string]string{}
	for i := 0; i < 1000; i++ {
		k := "key-" + string(rune('a'+i%26))
		m[k] = "value-" + string(rune('a'+i%26))
	}
	return len(m)
}
func main() {}
`)
}

func BenchmarkSliceSetArenaString(b *testing.B) {
	b.Run("arena_backing", func(b *testing.B) {
		benchmarkEntrypoint(b, `package main

func run() int {
	s := make([]string, 1000)
	for i := 0; i < 1000; i++ {
		s[i] = "value-" + string(rune('a'+i%26))
	}
	return len(s)
}
func main() {}
`)
	})
	b.Run("heap_backing", func(b *testing.B) {
		benchmarkEntrypoint(b, `package main

var g = make([]string, 1000)

func run() int {
	for i := 0; i < 1000; i++ {
		g[i] = "value-" + string(rune('a'+i%26))
	}
	return len(g)
}
func main() {}
`)
	})
}
