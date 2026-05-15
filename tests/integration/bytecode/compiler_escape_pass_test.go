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

const escapeSinkSource = `package main

type P struct {
	A int
	B int
}

func spawnedArgument(fn func(*P, chan int), done chan int, n int) {
	p := &P{A: n, B: 2}
	go fn(p, done)
}

func selectSend(out chan *P, other chan int, n int) {
	p := &P{A: n, B: 2}
	select {
	case out <- p:
	case <-other:
	}
}

func mapStoredIntKey(m map[int]*P, n int) {
	p := &P{A: n, B: 2}
	m[n] = p
}

func mapStoredStringKey(m map[string]*P, key string, n int) {
	p := &P{A: n, B: 2}
	m[key] = p
}

func frameLocal(n int) int {
	p := &P{A: n, B: 2}
	p.A++
	return p.A + p.B
}

func goNearbyKeepsPromotion(fn func(chan int), done chan int, n int) int {
	p := &P{A: n, B: 2}
	go fn(done)
	return p.A + p.B
}

func selectNearbyKeepsPromotion(out chan int, other chan int, n int) int {
	p := &P{A: n, B: 2}
	select {
	case out <- n:
	case <-other:
	}
	return p.A + p.B
}

func EntrypointRun() int { return frameLocal(1) }
`

func TestEscapePassDoesNotArenaPromotePointersHandedToOutlivingSinks(t *testing.T) {
	t.Parallel()
	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": escapeSinkSource})
	require.NoError(t, err)

	byName := map[string]*program.CompiledFunction{}
	for _, fn := range program.ExportFunctions(compiled.Root()) {
		byName[fn.Name] = fn
	}

	for _, testCase := range []struct {
		function string
		sink     isa.Instruction
	}{
		{function: "spawnedArgument", sink: isa.NewInstruction(isa.OpGo, 0, 0, 0)},
		{function: "selectSend", sink: isa.NewTier1Instruction(isa.SubOpSelect, 0, 0)},
		{function: "mapStoredIntKey", sink: isa.NewInstruction(isa.OpMapSetIntGeneral, 0, 0, 0)},
		{function: "mapStoredStringKey", sink: isa.NewInstruction(isa.OpMapSetStringGeneral, 0, 0, 0)},
	} {
		t.Run(testCase.function, func(t *testing.T) {
			t.Parallel()
			fn := byName[testCase.function]
			require.NotNilf(t, fn, "%s is compiled", testCase.function)
			require.Truef(t, bodyContainsOperation(fn, testCase.sink),
				"%s must compile to %s, otherwise the escape assertion below proves nothing",
				testCase.function, isa.InstructionDisplayName(testCase.sink))

			sites := arenaCandidateSites(fn)
			require.NotEmptyf(t, sites, "%s has an allocation site", testCase.function)
			for _, pc := range sites {
				require.Falsef(t, fn.ArenaSafeAllocPCs[pc],
					"%s: a pointer handed to %s outlives the frame and must not be arena-promoted",
					testCase.function, isa.InstructionDisplayName(testCase.sink))
			}
		})
	}

	for _, name := range []string{"frameLocal", "goNearbyKeepsPromotion", "selectNearbyKeepsPromotion"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn := byName[name]
			require.NotNilf(t, fn, "%s is compiled", name)
			sites := arenaCandidateSites(fn)
			require.Lenf(t, sites, 1, "%s has exactly one allocation site", name)
			require.Truef(t, fn.ArenaSafeAllocPCs[sites[0]],
				"%s: the pointer never leaves the frame and must stay arena-promoted", name)
		})
	}
}

func bodyContainsOperation(fn *program.CompiledFunction, want isa.Instruction) bool {
	for _, inst := range fn.Body {
		if isa.FlatIndexFor(inst) == isa.FlatIndexFor(want) {
			return true
		}
	}
	return false
}

func arenaCandidateSites(fn *program.CompiledFunction) []int {
	var sites []int
	for pc := 0; pc < len(fn.Body); pc++ {
		inst := fn.Body[pc]
		if inst.Op == isa.OpAllocIndirect || (inst.Op == isa.OpAddr && inst.C != engine.AddrSourceStable) {
			sites = append(sites, pc)
		}
	}
	return sites
}
