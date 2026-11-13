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

var boxedMapStorePrograms = []struct{ name, source string }{
	{"string_keys_string_values", `package main

var keys = []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}

func run() int {
	total := 0
	for round := 0; round < 200; round++ {
		m := map[string]any{}
		for i, key := range keys {
			m[key] = keys[(i+round)%len(keys)]
		}
		total += len(m)
	}
	return total
}
`},
	{"string_keys_mixed_values", `package main

type point struct{ x, y int }

var keys = []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}

func run() int {
	total := 0
	shared := &point{x: 1, y: 2}
	numbers := []int{1, 2, 3}
	for round := 0; round < 200; round++ {
		m := map[string]any{}
		for i, key := range keys {
			switch i % 4 {
			case 0:
				m[key] = round
			case 1:
				m[key] = numbers
			case 2:
				m[key] = shared
			default:
				m[key] = key
			}
		}
		total += len(m)
	}
	return total
}
`},
	{"int_keys_string_values", `package main

var words = []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}

func run() int {
	total := 0
	for round := 0; round < 200; round++ {
		m := map[int]any{}
		for i := 0; i < 8; i++ {
			m[i] = words[(i+round)%len(words)]
		}
		total += len(m)
	}
	return total
}
`},
	{"kept_map_overwritten", `package main

var keys = []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta"}

var kept = map[string]any{}

func run() int {
	for round := 0; round < 200; round++ {
		for i, key := range keys {
			kept[key] = keys[(i+round)%len(keys)]
		}
	}
	return len(kept)
}
`},
}

func BenchmarkBoxedMapStores(b *testing.B) { runEntrypointPrograms(b, boxedMapStorePrograms) }
