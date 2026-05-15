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
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestPointerToPointerFreeStructSurvivesAcrossSubmits(t *testing.T) {
	t.Parallel()
	session := app.NewService().NewSession()
	submit := func(code string) any {
		result, err := session.Submit(context.Background(), code)
		require.NoError(t, err, code)
		return result
	}
	submit(`type P struct{ A int; B int }`)
	submit(`func mk(a int) *P { return &P{A: a, B: a * 2} }`)
	submit(`p := mk(7)`)
	submit(`q := &P{A: 9, B: 10}`)
	submit(`box := []any{&P{A: 11, B: 12}}`)
	require.EqualValues(t, 21, submit(`p.A + p.B`))
	submit(`s := ""; for i := 0; i < 4096; i++ { s = s + "0123456789abcdef" }; len(s)`)
	submit(`func churn() int { sink := 0; for j := 0; j < 200000; j++ { t := &P{A: j, B: 1}; sink += t.B }; return sink }`)
	require.EqualValues(t, 200000, submit(`churn()`))
	require.EqualValues(t, 21, submit(`p.A + p.B`), "pointer returned from a function")
	require.EqualValues(t, 19, submit(`q.A + q.B`), "pointer taken at top level")
	require.EqualValues(t, 23, submit(`box[0].(*P).A + box[0].(*P).B`), "pointer stored in a heap container")
}

func TestEscapePassAnnotatesAddrSites(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": `package main
type P struct {
	A int
	B int
}
func local(n int) int {
	p := &P{A: n, B: 2}
	p.A++
	return p.A + p.B
}
func stored(values []*P, n int) {
	values[0] = &P{A: n}
}
func returned(n int) *P {
	return &P{A: n}
}
func loopCarried(values []*P, n int) {
	var p *P
	for i := 0; i < n; i++ {
		if p != nil {
			values[i] = p
		}
		p = &P{A: i}
	}
}
func EntrypointRun() int { return local(1) }
`})
	require.NoError(t, err)
	byName := map[string]*program.CompiledFunction{}
	for _, fn := range program.ExportFunctions(compiled.Root()) {
		byName[fn.Name] = fn
	}
	require.Len(t, addrSites(byName["local"]), 1)
	require.True(t, byName["local"].ArenaSafeAllocPCs[addrSites(byName["local"])[0]], "a frame-local pointer keeps its pointee in the arena")
	for _, name := range []string{"stored", "returned", "loopCarried"} {
		fn := byName[name]
		sites := addrSites(fn)
		require.NotEmptyf(t, sites, "%s has an opAddr site", name)
		for _, pc := range sites {
			require.Falsef(t, fn.ArenaSafeAllocPCs[pc], "%s: an escaping pointer must not be arena-safe", name)
		}
	}
}

func addrSites(fn *program.CompiledFunction) []int {
	var sites []int
	for pc := 0; pc < len(fn.Body); pc++ {
		if fn.Body[pc].Op == isa.OpAddr && fn.Body[pc].C != engine.AddrSourceStable {
			sites = append(sites, pc)
		}
	}
	return sites
}
