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

//go:build integration && !safe && !(js && wasm)

package bytecode_test

import (
	"fmt"
	"maps"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib"
)

const servedChainSource = `package main

import (
	"chainhost"
	"strconv"
)

func mk(n int) string { return "chain-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*7) }

func Flat(n int) []string {
	var out []string
	chainhost.Each(8, func(i int) {
		out = append(out, mk(n+i))
	})
	chainhost.Keep(out)
	return out
}

func Nested(n int) []string {
	var out []string
	chainhost.Each(4, func(i int) {
		chainhost.Each(3, func(j int) {
			out = append(out, mk(n+10*i+j))
		})
	})
	chainhost.Keep(out)
	return out
}

func Plain(n int) []string {
	out := make([]string, 0, 16)
	for i := 0; i < 16; i++ {
		out = append(out, mk(n+i))
	}
	chainhost.Keep(out)
	return out
}

func Churn(n int) int {
	t := 0
	for i := 0; i < 64; i++ {
		t += len("ZZ-" + strconv.Itoa(n+i) + "-" + strconv.Itoa(i*7) + "-ZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZZ")
	}
	return t
}
`

type servedChainHost struct {
	mu   sync.Mutex
	kept [][]string
}

func servedChainMk(n int) string { return fmt.Sprintf("chain-%d-%d", n, n*7) }

func servedChainWant(entry string, seed int) []string {
	var out []string
	switch entry {
	case "Flat":
		for i := range 8 {
			out = append(out, servedChainMk(seed+i))
		}
	case "Nested":
		for i := range 4 {
			for j := range 3 {
				out = append(out, servedChainMk(seed+10*i+j))
			}
		}
	default:
		for i := range 16 {
			out = append(out, servedChainMk(seed+i))
		}
	}
	return out
}

func servedChainSymbols(host *servedChainHost) *symtab.SymbolRegistry {
	exports := maps.Clone(stdlib.Exports())
	exports["chainhost"] = map[string]reflect.Value{
		"Each": reflect.ValueOf(func(n int, fn func(int)) {
			for index := range n {
				fn(index)
			}
		}),
		"Keep": reflect.ValueOf(func(xs []string) {
			host.mu.Lock()
			host.kept = append(host.kept, xs)
			host.mu.Unlock()
		}),
	}
	return symtab.NewSymbolRegistry(exports)
}

func checkServedChainKept(observations *hazardObservations, label string, host *servedChainHost, want [][]string) {
	host.mu.Lock()
	defer host.mu.Unlock()
	for index, wanted := range want {
		got := "<missing>"
		if index < len(host.kept) {
			got = strings.Join(host.kept[index], ",")
		}
		observations.compareString(fmt.Sprintf("%s #%d", label, index), strings.Join(wanted, ","), got)
	}
}

func TestCallbackChainValuesSurviveReuse(t *testing.T) {
	previous := engine.SetLicenceCountingForTest(true)
	t.Cleanup(func() { engine.SetLicenceCountingForTest(previous) })
	for _, entry := range []string{"Flat", "Nested"} {
		t.Run(entry, func(t *testing.T) {
			window := openAuditWindow(t, 0)
			census := openCensusWindow()
			host := &servedChainHost{}
			service := app.NewService(hazardServiceOptions()...)
			service.UseSymbols(servedChainSymbols(host))
			compiled := hazardCompile(t, service, servedChainSource)
			before := engine.LicenceDiagnostics().ServedChain
			want := make([][]string, 0, hazardRounds)
			for round := range hazardRounds {
				n := 1000*round + 1
				hazardCall(t, service, compiled, entry, n)
				want = append(want, servedChainWant(entry, n))
			}
			stats := engine.LicenceDiagnostics().ServedChain
			observations := &hazardObservations{}
			checkServedChainKept(observations, "kept straight after the runs", host, want)
			clobberArenas(t, service, 16)
			for round := range 8 {
				hazardCall(t, service, compiled, "Churn", 1000*round)
			}
			checkServedChainKept(observations, "kept after reuse", host, want)
			observations.note("served chain: chainCounted=%d globalFallbacks=%d", stats.ChainCounted-before.ChainCounted, stats.GlobalFallbacks-before.GlobalFallbacks)
			if stats.ChainCounted == before.ChainCounted && stats.GlobalFallbacks != before.GlobalFallbacks {
				observations.fire("served-chain-not-applied", "every callback blind hand-off fell back to the process-wide word")
			}
			window.settle(observations)
			census.settle(observations)
			runtime.KeepAlive(host)
			assertNoHazard(t, observations, "")
		})
	}
}

func TestCrossTreeParallelRunsSurviveReuse(t *testing.T) {
	window := openAuditWindow(t, 0)
	census := openCensusWindow()
	host := &servedChainHost{}
	service := app.NewService(hazardServiceOptions()...)
	service.UseSymbols(servedChainSymbols(host))
	compiled := hazardCompile(t, service, servedChainSource)
	const workers, rounds = 8, 24
	type result struct {
		got  []string
		want []string
	}
	results := make([][]result, workers)
	var done sync.WaitGroup
	for worker := range workers {
		own := service.Clone()
		done.Go(func() {
			entries := []string{"Plain", "Flat", "Plain", "Nested"}
			for round := range rounds {
				entry := entries[(worker+round)%len(entries)]
				seed := 100000*worker + 100*round
				got, err := own.CallFunction(t.Context(), compiled, entry, seed)
				if err != nil {
					results[worker] = append(results[worker], result{got: []string{"error: " + err.Error()}, want: servedChainWant(entry, seed)})
					continue
				}
				xs, ok := got.([]string)
				if !ok {
					xs = []string{fmt.Sprintf("not a string slice: %T", got)}
				}
				results[worker] = append(results[worker], result{got: xs, want: servedChainWant(entry, seed)})
			}
		})
	}
	done.Wait()
	observations := &hazardObservations{}
	check := func(label string) {
		for worker := range workers {
			for index, r := range results[worker] {
				observations.compareString(fmt.Sprintf("%s worker %d run %d", label, worker, index), strings.Join(r.want, ","), strings.Join(r.got, ","))
			}
		}
	}
	check("results straight after the runs")
	clobberArenas(t, service, 16)
	check("results after reuse")
	window.settle(observations)
	census.settle(observations)
	runtime.KeepAlive(host)
	runtime.KeepAlive(results)
	assertNoHazard(t, observations, "")
}
