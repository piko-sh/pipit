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

import "testing"

func BenchmarkInPlaceAppendBuildThenPass(b *testing.B) {
	const prelude = `package main

type point struct{ x, y int }

func first(ps []point) int {
	if len(ps) == 0 {
		return 0
	}
	return ps[0].x
}
`
	runHotspotBenchmarks(b, []struct {
		name   string
		source string
	}{
		{
			name: "build",
			source: prelude + `
func run() int {
	var ps []point
	for i := 0; i < 1000; i++ {
		ps = append(ps, point{x: i, y: i})
	}
	return len(ps)
}
`,
		},
		{
			name: "build_then_pass",
			source: prelude + `
func run() int {
	var ps []point
	for i := 0; i < 1000; i++ {
		ps = append(ps, point{x: i, y: i})
	}
	return len(ps) + first(ps)
}
`,
		},
		{
			name: "pass_each_iteration",
			source: prelude + `
func run() int {
	var ps []point
	total := 0
	for i := 0; i < 1000; i++ {
		ps = append(ps, point{x: i, y: i})
		total += first(ps)
	}
	return len(ps) + total
}
`,
		},
	})
}
