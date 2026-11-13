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
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/symtab"
)

const heapDataGlobalsSource = `package main

import "host"

type point struct {
	X, Y int
	Name string
}

var seed = 7
var word = "w" + host.Itoa(seed*3)
var joined = word + "-" + strings2(seed)
var words = []string{word, joined, "lit"}
var ints = []int{seed, seed * 2, seed * 3}
var bytesValue = []byte(joined)
var points = []point{{X: seed, Y: 1, Name: word}, {X: 2, Y: seed, Name: joined}}
var byName = map[string]int{word: seed, joined: seed + 1}
var anyByName = map[string]any{"n": seed, "s": joined, "f": 1.5}
var boxed any = seed * 11
var boxedString any = "boxed-" + word
var cell = &point{X: seed, Name: word}
var counter = makeCounter(seed)
var nested = [][]string{{word}, {joined, word}}

func strings2(n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += host.Itoa(i)
	}
	return out
}

func makeCounter(start int) func() int {
	n := start
	return func() int {
		n++
		return n
	}
}

func summary() string {
	out := word + "|" + joined + "|" + words[0] + words[1] + words[2]
	for _, v := range ints {
		out += "|" + host.Itoa(v)
	}
	out += "|" + string(bytesValue)
	for _, p := range points {
		out += "|" + host.Itoa(p.X) + "," + host.Itoa(p.Y) + "," + p.Name
	}
	out += "|" + host.Itoa(byName[word]) + "," + host.Itoa(byName[joined])
	out += "|" + host.Itoa(anyByName["n"].(int)) + "," + anyByName["s"].(string)
	out += "|" + host.Itoa(boxed.(int)) + "|" + boxedString.(string)
	out += "|" + host.Itoa(cell.X) + cell.Name
	out += "|" + nested[0][0] + nested[1][0] + nested[1][1]
	return out
}

func tick() int { return counter() }

func churn(n int) int {
	t := 0
	for i := 0; i < n; i++ {
		s := host.Itoa(i) + "-" + word
		xs := []string{s, s + "x"}
		t += len(xs[1])
	}
	return t
}
`

func heapDataGlobalsWant() string {
	const seed = 7
	word := "w" + strconv.Itoa(seed*3)
	digits := ""
	for i := range seed {
		digits += strconv.Itoa(i)
	}
	joined := word + "-" + digits
	parts := []string{word, joined, word + joined + "lit",
		strconv.Itoa(seed), strconv.Itoa(seed * 2), strconv.Itoa(seed * 3), joined,
		strconv.Itoa(seed) + ",1," + word, "2," + strconv.Itoa(seed) + "," + joined,
		strconv.Itoa(seed) + "," + strconv.Itoa(seed+1), strconv.Itoa(seed) + "," + joined,
		strconv.Itoa(seed * 11), "boxed-" + word, strconv.Itoa(seed) + word, word + joined + word}
	return strings.Join(parts, "|")
}

func TestVarInitHeapDataGlobalsSurviveReuse(t *testing.T) {
	want := heapDataGlobalsWant()
	symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{"host": {"Itoa": reflect.ValueOf(strconv.Itoa)}})
	service := app.NewService()
	service.UseSymbols(symbols)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": heapDataGlobalsSource})
	require.NoError(t, err)
	other := app.NewService()
	other.UseSymbols(symbols)
	otherCompiled, err := other.CompileFileSet(t.Context(), map[string]string{"main.go": heapDataGlobalsSource})
	require.NoError(t, err)

	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	for round := range 60 {
		result, runErr := service.CallFunction(t.Context(), compiled, "summary")
		require.NoError(t, runErr)
		require.Equal(t, want, result, "round %d: the globals of the first initialisation", round)
		ticked, runErr := service.CallFunction(t.Context(), compiled, "tick")
		require.NoError(t, runErr)
		require.EqualValues(t, 7+round+1, ticked, "round %d: the closure global keeps its cell", round)

		fresh, runErr := other.ExecuteEntrypoint(t.Context(), otherCompiled, "summary")
		require.NoError(t, runErr, "round %d: a later initialiser run reuses the pooled register files", round)
		require.Equal(t, want, fresh)
		_, runErr = service.CallFunction(t.Context(), compiled, "churn", 200)
		require.NoError(t, runErr)
		if round%10 == 0 {
			runtime.GC()
		}
	}
	auditHeapDataGlobals(t)
}

func auditHeapDataGlobals(t *testing.T) {
	t.Helper()
	for _, hit := range engine.AuditReuse(engine.AuditOptions{}) {
		require.NotEqual(t, engine.AuditReleaseHit, hit.Class, "%s", hit)
		require.NotEqual(t, engine.AuditOutsideReach, hit.Class, "%s", hit)
	}
}
