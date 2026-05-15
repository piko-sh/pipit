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
	"strings"
	"testing"

	"pipit.sh/pipit/internal/app"

	"github.com/stretchr/testify/require"
)

func TestAppendStructFastEmittedForPointerFreeStruct(t *testing.T) {
	t.Parallel()
	dump := compileSourceForOpcodeCheck(t, `package main
type point struct {
	x int
	y float64
}
type named struct {
	label string
}
func EntrypointRun(n int) int {
	ps := []point{}
	ns := []named{}
	for i := 0; i < n; i++ {
		ps = append(ps, point{x: i, y: 0.5})
		ns = append(ns, named{label: "l"})
	}
	return len(ps) + len(ns)
}`)
	require.Equalf(t, 1, strings.Count(dump, "APPEND_STRUCT_FAST"),
		"only the pointer-free struct append may use the fast opcode; disasm:\n%s", dump)
}

func TestAppendTypedFastEmittedForGeneralBankSlices(t *testing.T) {
	t.Parallel()
	dump := compileSourceForOpcodeCheck(t, `package main
var ints []int
var floats []float64
var strs []string
func EntrypointRun(n int) int {
	m := map[string][]int{}
	for i := 0; i < n; i++ {
		ints = append(ints, i)
		floats = append(floats, float64(i))
		strs = append(strs, "s")
		m["k"] = append(m["k"], i)
	}
	return len(ints) + len(floats) + len(strs) + len(m["k"])
}`)
	require.Positivef(t, strings.Count(dump, "APPEND_INT_FAST"), "disasm:\n%s", dump)
	require.Positivef(t, strings.Count(dump, "APPEND_FLOAT_FAST"), "disasm:\n%s", dump)
	require.Positivef(t, strings.Count(dump, "APPEND_STRING_FAST"), "disasm:\n%s", dump)
}

func TestAppendStructFastGrowthMatchesReflectAppend(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	result, err := service.Eval(context.Background(), `
type point struct {
	x int
	y int
}
func run(n int) int {
	ps := make([]point, 0, 2)
	total := 0
	for i := 0; i < n; i++ {
		before := len(ps)
		ps = append(ps, point{x: i, y: i * 3})
		if len(ps) != before+1 || cap(ps) < len(ps) {
			return -1
		}
	}
	for i, p := range ps {
		if p.x != i || p.y != i*3 {
			return -2
		}
		total += p.x + p.y
	}
	return total*1000 + len(ps)
}
run(50)`)
	require.NoError(t, err)
	expected := 0
	for i := range 50 {
		expected += i + i*3
	}
	require.EqualValues(t, expected*1000+50, result)
}

func TestAppendStructFastAliasedSourceKeepsLength(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	result, err := service.Eval(context.Background(), `
type point struct{ x int }
func run() int {
	ps := make([]point, 0, 8)
	ps = append(ps, point{x: 1})
	saved := ps
	ps = append(ps, point{x: 2})
	grown := append(saved, point{x: 3})
	return len(saved)*100 + len(ps)*10 + len(grown)
}
run()`)
	require.NoError(t, err)
	require.EqualValues(t, 122, result)
}

func TestAppendIntFastNilSlice(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	result, err := service.Eval(context.Background(), `
type bag struct {
	ints    []int
	floats  []float64
	strings []string
}
var global []int
func run() int {
	b := &bag{}
	b.ints = append(b.ints, 7)
	b.floats = append(b.floats, 2.5)
	b.strings = append(b.strings, "a"+"b")
	global = append(global, 9)
	return b.ints[0]*1000 + int(b.floats[0]*2)*100 + len(b.strings[0])*10 + global[0]
}
run()`)
	require.NoError(t, err)
	require.EqualValues(t, 7529, result)
}

func TestAppendStructFastArrayElements(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	result, err := service.Eval(context.Background(), `
func run() int {
	rows := [][2]int{}
	for i := 0; i < 20; i++ {
		rows = append(rows, [2]int{i, i * 2})
	}
	sum := 0
	for _, r := range rows {
		sum += r[0] + r[1]
	}
	return sum
}
run()`)
	require.NoError(t, err)
	require.EqualValues(t, 570, result)
}
