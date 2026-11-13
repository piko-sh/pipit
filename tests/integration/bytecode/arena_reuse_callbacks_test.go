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
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib"
)

type CallbackRec struct {
	S string
	L []string
	N int
}

type CallbackBox struct{ V any }

type callbackStoreFunc = func(int, []string, map[string]string, *CallbackRec, any)

type callbackContainers struct {
	seed    int
	slice   []string
	mapping map[string]string
	rec     *CallbackRec
	box     *CallbackBox
}

type callbackHost struct {
	store  callbackStoreFunc
	result func(int) string
	pair   func(int) (string, []string)
	keep   func(string) string
	filled []callbackContainers
	kept   []any
}

const callbackSource = `package main

import (
	"callbackhost"
	"strconv"
)

var last string

func mk(n int) string { return "cb-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*7) }

func store(n int, s []string, m map[string]string, p *callbackhost.Rec, v any) {
	s[0] = mk(n)
	s[1] = mk(n + 1)
	m["k"] = mk(n + 2)
	p.S = mk(n + 3)
	p.L = []string{mk(n + 4), mk(n + 5)}
	p.N = n
	v.(*callbackhost.Box).V = mk(n + 6)
}

func result(n int) string { return mk(n) }

func pair(n int) (string, []string) { return mk(n), []string{mk(n + 1), mk(n + 2)} }

func keepLast(s string) string {
	last = s
	return s
}

type W struct{ name string }

func (w W) String() string { return w.name + "!" }

type PW struct{ name string }

func (w *PW) String() string { return w.name + "?" }

type E struct{ a, b string }

func (e E) Error() string { return e.a + "," + e.b }

func init() {
	callbackhost.Register(store)
	callbackhost.RegisterResult(result)
	callbackhost.RegisterPair(pair)
	callbackhost.RegisterKeep(keepLast)
}

func Fill(n int) int { callbackhost.Fill(n, store); return n }

func FillLiteral(n int) int {
	k := n
	callbackhost.Fill(n, func(n int, s []string, m map[string]string, p *callbackhost.Rec, v any) { store(k, s, m, p, v) })
	return n
}

func Map(n int) int { return callbackhost.Map(n, result) }

func MapClosure(n int) int {
	k := mk(n)
	return callbackhost.Map(n, func(i int) string { return k + "/" + strconv.Itoa(i) })
}

func MapPair(n int) int { return callbackhost.MapPair(n, pair) }

func Stringify(n int) int { callbackhost.Stringify(W{name: mk(n)}); return n }

func StringifyPointer(n int) int { callbackhost.Stringify(&PW{name: mk(n)}); return n }

func Describe(n int) int { return callbackhost.Describe(E{a: mk(n), b: mk(n + 1)}) }

func Invoke(n int) string { return callbackhost.Invoke(n) }

func InvokeKeep(n int) int {
	s := mk(n)
	callbackhost.InvokeKeep(s)
	return len(s)
}

func Last() string { return last }

func Churn(n int) int {
	t := 0
	for i := 0; i < 64; i++ {
		t += len("ZZ-" + strconv.Itoa(n+i) + "-" + strconv.Itoa(i*7))
	}
	return t
}
`

var callbackWorkloadNames = func() []string {
	names := make([]string, 256)
	for index := range names {
		names[index] = "host-name-" + strconv.Itoa(index)
	}
	return names
}()

const (
	callbackStringStoresPrelude = `package main
import "host"
func run() int {
	names := host.Names()
	local := make([]string, 256)
	for index := range local {
		local[index] = "p" + string(rune('a'+index%26))
	}
	out := make([]string, 256)
	host.Each(256, func(index int) {
		for step := 0; step < 16; step++ {
			slot := (index*16 + step) & 255
`
	callbackStringStoresEpilogue = `
		}
	})
	return len(out[0]) + len(local) + len(names)
}
`
)

var callbackWorkloads = []struct {
	id, source, entry string
	want              any
}{
	{id: "CallbackRender-rows", entry: "run", want: 896, source: `package main

import "host"

func run() int {
	names := host.Names()
	var out []string
	host.Each(len(names), func(index int) {
		out = append(out, "<li class=\"row\">")
		out = append(out, names[index])
		if index%2 == 0 {
			out = append(out, " (even)")
		}
		out = append(out, "</li>")
	})
	return len(out)
}
`},
	{id: "CallbackStringStores-constants", entry: "run", want: 520, source: callbackStringStoresPrelude + "\t\t\tout[slot] = \"constant\"" + callbackStringStoresEpilogue},
	{id: "CallbackStringStores-host", entry: "run", want: 523, source: callbackStringStoresPrelude + "\t\t\tout[slot] = names[slot]" + callbackStringStoresEpilogue},
	{id: "CallbackStringStores-parent_slice", entry: "run", want: 514, source: callbackStringStoresPrelude + "\t\t\tout[slot] = local[slot]" + callbackStringStoresEpilogue},
	{id: "CallbackStringStores-mixed", entry: "run", want: 520, source: callbackStringStoresPrelude + `			switch slot % 3 {
			case 0:
				out[slot] = "constant"
			case 1:
				out[slot] = names[slot]
			default:
				out[slot] = local[slot]
			}` + callbackStringStoresEpilogue},
	{id: "CallbackStringStores-parent_arena", entry: "run", want: 8, source: `package main

import "host"

func run() int {
	key := "parent-" + string(rune('a'+len(host.Names())%26))
	out := make([]string, 256)
	host.EachWith(key, 256, func(index int, s string) {
		for step := 0; step < 16; step++ {
			out[(index*16+step)&255] = s
		}
	})
	return len(out[0])
}
`},
	{id: "BoundSort64", entry: "sorter", want: 0, source: callbackBoundSource},
	{id: "BoundStringer16", entry: "stringer", want: 64, source: callbackBoundSource},
}

const callbackBoundSource = `package main

import "host"

type item struct{ name string; n int }

func (i item) String() string { return i.name + "!" }

type byN []int

func (b byN) Len() int           { return len(b) }
func (b byN) Less(i, j int) bool { return b[i] < b[j] }
func (b byN) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }

func stringer() int {
	it := item{name: "abc", n: 1}
	t := 0
	for k := 0; k < 16; k++ {
		t += len(host.Stringify(it))
	}
	return t
}

func sorter() int {
	xs := make(byN, 64)
	for i := range xs {
		xs[i] = (i * 7919) % 64
	}
	host.SortIt(xs)
	return xs[0]
}
`

func newContainers(n int) callbackContainers {
	return callbackContainers{seed: n, slice: make([]string, 2), mapping: map[string]string{}, rec: &CallbackRec{}, box: &CallbackBox{}}
}

func (h *callbackHost) fill(n int, fn callbackStoreFunc) {
	c := newContainers(n)
	fn(n, c.slice, c.mapping, c.rec, c.box)
	h.filled = append(h.filled, c)
}

func callbackMk(n int) string { return fmt.Sprintf("cb-%d-%d", n, n*7) }

func callbackService(t *testing.T, declare bool) (*app.Service, *program.CompiledFileSet, *callbackHost) {
	t.Helper()
	host := &callbackHost{}
	exports := maps.Clone(stdlib.Exports())
	exports["callbackhost"] = map[string]reflect.Value{
		"Rec":            reflect.ValueOf((*CallbackRec)(nil)),
		"Box":            reflect.ValueOf((*CallbackBox)(nil)),
		"Register":       reflect.ValueOf(func(fn callbackStoreFunc) { host.store = fn }),
		"RegisterResult": reflect.ValueOf(func(fn func(int) string) { host.result = fn }),
		"RegisterPair":   reflect.ValueOf(func(fn func(int) (string, []string)) { host.pair = fn }),
		"RegisterKeep":   reflect.ValueOf(func(fn func(string) string) { host.keep = fn }),
		"Fill":           reflect.ValueOf(func(n int, fn callbackStoreFunc) { host.fill(n, fn) }),
		"Map": reflect.ValueOf(func(n int, fn func(int) string) int {
			r := fn(n)
			host.kept = append(host.kept, r)
			return len(r)
		}),
		"MapPair": reflect.ValueOf(func(n int, fn func(int) (string, []string)) int {
			a, b := fn(n)
			host.kept = append(host.kept, a, b)
			return len(b)
		}),
		"Stringify": reflect.ValueOf(func(s fmt.Stringer) string {
			r := s.String()
			host.kept = append(host.kept, r)
			return r
		}),
		"Describe": reflect.ValueOf(func(err error) int {
			r := err.Error()
			host.kept = append(host.kept, r)
			return len(r)
		}),
		"Invoke":     reflect.ValueOf(func(n int) string { return host.result(n) }),
		"InvokeKeep": reflect.ValueOf(func(s string) { host.keep(s) }),
	}
	service := app.NewService(hazardServiceOptions()...)
	service.UseSymbols(symtab.NewSymbolRegistry(exports))
	compiled := hazardCompile(t, service, callbackSource)
	if declare {
		service.DeclareTopLevelCalls(compiled)
	}
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	require.NotNil(t, host.store)
	return service, compiled, host
}

func reuseCallbackArenas(t *testing.T, service *app.Service, compiled *program.CompiledFileSet) {
	t.Helper()
	clobberArenas(t, service, 16)
	for round := range 8 {
		hazardCall(t, service, compiled, "Churn", 1000*round)
	}
}

func checkCallbackContainers(observations *hazardObservations, label string, filled []callbackContainers) {
	for _, containers := range filled {
		seed := containers.seed
		observations.compareString(fmt.Sprintf("%s n=%d slice", label, seed), callbackMk(seed)+","+callbackMk(seed+1), strings.Join(containers.slice, ","))
		observations.compareString(fmt.Sprintf("%s n=%d map", label, seed), callbackMk(seed+2), containers.mapping["k"])
		observations.compareString(fmt.Sprintf("%s n=%d struct pointer", label, seed), fmt.Sprintf("%s|%s,%s|%d", callbackMk(seed+3), callbackMk(seed+4), callbackMk(seed+5), seed),
			fmt.Sprintf("%s|%s|%d", containers.rec.S, strings.Join(containers.rec.L, ","), containers.rec.N))
		observations.compareString(fmt.Sprintf("%s n=%d interface", label, seed), callbackMk(seed+6), fmt.Sprint(containers.box.V))
	}
}

func TestCallbackStoresIntoHostContainersSurviveReuse(t *testing.T) {
	shapes := []struct {
		id      string
		declare bool
		run     func(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, h *callbackHost, n int)
	}{
		{id: "live-named", run: func(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, _ *callbackHost, n int) {
			hazardCall(t, service, compiled, "Fill", n)
		}},
		{id: "live-literal", run: func(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, _ *callbackHost, n int) {
			hazardCall(t, service, compiled, "FillLiteral", n)
		}},
		{id: "orphan", run: func(_ *testing.T, _ *app.Service, _ *program.CompiledFileSet, h *callbackHost, n int) {
			h.fill(n, h.store)
		}},
		{id: "top-level", declare: true, run: func(_ *testing.T, _ *app.Service, _ *program.CompiledFileSet, h *callbackHost, n int) {
			h.fill(n, h.store)
		}},
	}
	for _, shape := range shapes {
		t.Run(shape.id, func(t *testing.T) {
			window := openAuditWindow(t, 0)
			census := openCensusWindow()
			service, compiled, host := callbackService(t, shape.declare)
			before := engine.LicenceDiagnostics().CallbackRelease
			for round := range hazardRounds {
				shape.run(t, service, compiled, host, 10*round+1)
			}
			stats := engine.LicenceDiagnostics().CallbackRelease
			observations := &hazardObservations{}
			checkCallbackContainers(observations, "straight after the runs", host.filled)
			reuseCallbackArenas(t, service, compiled)
			checkCallbackContainers(observations, "after reuse", host.filled)
			observations.note("callback release: callbackRuns=%d cachedArenaReleases=%d topLevelRuns=%d topLevelFallbacks=%d",
				stats.CallbackRuns-before.CallbackRuns, stats.CachedArenaReleases-before.CachedArenaReleases,
				stats.TopLevelRuns-before.TopLevelRuns, stats.TopLevelFallbacks-before.TopLevelFallbacks)
			if !shape.declare && stats.CallbackRuns == before.CallbackRuns {
				observations.fire("callback-release-not-applied", "no callback run took the callback release")
			}
			if shape.declare && stats.TopLevelRuns-before.TopLevelRuns != hazardRounds {
				observations.fire("top-level-not-applied", "%d of %d declared top-level calls ran as root executions", stats.TopLevelRuns-before.TopLevelRuns, hazardRounds)
			}
			window.settle(observations)
			census.settle(observations)
			runtime.KeepAlive(host)
			assertNoHazard(t, observations, "")
		})
	}
}

func TestNestedDeclaredTopLevelCallTakesTheFallback(t *testing.T) {
	window := openAuditWindow(t, 0)
	census := openCensusWindow()
	service, compiled, host := callbackService(t, true)
	observations := &hazardObservations{}
	before := engine.LicenceDiagnostics().CallbackRelease
	got := make([]string, 0, hazardRounds)
	for round := range hazardRounds {
		got = append(got, fmt.Sprint(hazardCall(t, service, compiled, "Invoke", round)))
	}
	stats := engine.LicenceDiagnostics().CallbackRelease
	fallbacks, roots := stats.TopLevelFallbacks-before.TopLevelFallbacks, stats.TopLevelRuns-before.TopLevelRuns
	observations.note("nested: topLevelFallbacks=%d topLevelRuns=%d", fallbacks, roots)
	if fallbacks != hazardRounds || roots != 0 {
		observations.fire("top-level-check-missed", "%d nested calls: %d fallbacks, %d root runs", hazardRounds, fallbacks, roots)
	}
	direct := host.result(7)
	after := engine.LicenceDiagnostics().CallbackRelease
	if after.TopLevelRuns-stats.TopLevelRuns != 1 {
		observations.fire("top-level-root-not-taken", "a genuine top-level call did not run as a root execution")
	}
	reuseCallbackArenas(t, service, compiled)
	for round, value := range got {
		observations.compareString(fmt.Sprintf("nested Invoke(%d) after reuse", round), callbackMk(round), value)
	}
	observations.compareString("top-level call after reuse", callbackMk(7), direct)
	window.settle(observations)
	census.settle(observations)
	assertNoHazard(t, observations, "")
}

func TestDeclaredTopLevelCallRunsAsARootExecution(t *testing.T) {
	window := openAuditWindow(t, 0)
	service, compiled, host := callbackService(t, true)
	observations := &hazardObservations{}
	before := engine.LicenceDiagnostics().CallbackRelease
	got := make([]string, 0, hazardRounds)
	for round := range hazardRounds {
		got = append(got, host.result(round))
	}
	stats := engine.LicenceDiagnostics().CallbackRelease
	if stats.TopLevelRuns-before.TopLevelRuns != hazardRounds || stats.TopLevelFallbacks != before.TopLevelFallbacks {
		observations.fire("top-level-root-not-taken", "%d top-level calls: %d root runs, %d fallbacks", hazardRounds,
			stats.TopLevelRuns-before.TopLevelRuns, stats.TopLevelFallbacks-before.TopLevelFallbacks)
	}
	reuseCallbackArenas(t, service, compiled)
	for round, value := range got {
		observations.compareString(fmt.Sprintf("top-level result(%d) after reuse", round), callbackMk(round), value)
	}
	window.settle(observations)
	assertNoHazard(t, observations, "")
}

func runNestedDeclaredKeeps(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, observations *hazardObservations) {
	t.Helper()
	for round := range hazardRounds {
		n := 100 + round
		hazardCall(t, service, compiled, "InvokeKeep", n)
		reuseCallbackArenas(t, service, compiled)
		observations.compareString(fmt.Sprintf("global after nested InvokeKeep(%d) and reuse", n), callbackMk(n), fmt.Sprint(hazardCall(t, service, compiled, "Last")))
	}
}

func TestNestedDeclaredTopLevelKeepTakesTheFallback(t *testing.T) {
	window := openAuditWindow(t, 0)
	service, compiled, _ := callbackService(t, true)
	observations := &hazardObservations{}
	before := engine.LicenceDiagnostics().CallbackRelease
	runNestedDeclaredKeeps(t, service, compiled, observations)
	stats := engine.LicenceDiagnostics().CallbackRelease
	roots, fallbacks := stats.TopLevelRuns-before.TopLevelRuns, stats.TopLevelFallbacks-before.TopLevelFallbacks
	observations.note("topLevelRuns=%d topLevelFallbacks=%d", roots, fallbacks)
	if roots != 0 || fallbacks != hazardRounds {
		observations.fire("top-level-check-missed", "%d nested calls: %d fallbacks, %d root runs", hazardRounds, fallbacks, roots)
	}
	window.settle(observations)
	assertNoHazard(t, observations, "")
}

func TestCallbackResultsKeptByTheHostSurviveReuse(t *testing.T) {
	cases := []struct {
		id    string
		entry string
		want  func(n int) []string
	}{
		{id: "callback-named", entry: "Map", want: func(n int) []string { return []string{callbackMk(n)} }},
		{id: "callback-closure", entry: "MapClosure", want: func(n int) []string { return []string{callbackMk(n) + "/" + strconv.Itoa(n)} }},
		{id: "callback-multi", entry: "MapPair", want: func(n int) []string { return []string{callbackMk(n), callbackMk(n+1) + "," + callbackMk(n+2)} }},
		{id: "bound-stringer", entry: "Stringify", want: func(n int) []string { return []string{callbackMk(n) + "!"} }},
		{id: "bound-pointer-stringer", entry: "StringifyPointer", want: func(n int) []string { return []string{callbackMk(n) + "?"} }},
		{id: "bound-error", entry: "Describe", want: func(n int) []string { return []string{callbackMk(n) + "," + callbackMk(n+1)} }},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			window := openAuditWindow(t, 0)
			census := openCensusWindow()
			service, compiled, host := callbackService(t, false)
			before := engine.LicenceDiagnostics().CallbackRelease
			want := make([]string, 0, hazardRounds)
			for round := range hazardRounds {
				n := 10*round + 3
				hazardCall(t, service, compiled, tc.entry, n)
				want = append(want, tc.want(n)...)
			}
			stats := engine.LicenceDiagnostics().CallbackRelease
			observations := &hazardObservations{}
			render := func() []string {
				var out []string
				for _, value := range host.kept {
					if xs, ok := value.([]string); ok {
						out = append(out, strings.Join(xs, ","))
					} else {
						out = append(out, fmt.Sprint(value))
					}
				}
				return out
			}
			observations.compareString("kept results straight after the runs", strings.Join(want, ";"), strings.Join(render(), ";"))
			reuseCallbackArenas(t, service, compiled)
			for round := range 4 {
				hazardCall(t, service, compiled, tc.entry, 9000+round)
			}
			observations.compareString("kept results after reuse", strings.Join(want, ";"), strings.Join(render()[:len(want)], ";"))
			handed := stats.CallbackRuns - before.CallbackRuns + stats.BoundRuns - before.BoundRuns
			observations.note("callback release: callbackRuns=%d boundRuns=%d cachedArenaReleases=%d", stats.CallbackRuns-before.CallbackRuns,
				stats.BoundRuns-before.BoundRuns, stats.CachedArenaReleases-before.CachedArenaReleases)
			if handed == 0 {
				observations.fire("callback-release-not-applied", "no callback or bound-method run took the callback release")
			}
			window.settle(observations)
			census.settle(observations)
			runtime.KeepAlive(host)
			assertNoHazard(t, observations, "")
		})
	}
}

func TestCallbackBenchmarkWorkloadsMatchNativeGo(t *testing.T) {
	exports := maps.Clone(stdlib.Exports())
	exports["host"] = map[string]reflect.Value{
		"Each": reflect.ValueOf(func(n int, fn func(int)) {
			for index := range n {
				fn(index)
			}
		}),
		"EachWith": reflect.ValueOf(func(s string, n int, fn func(int, string)) {
			for index := range n {
				fn(index, s)
			}
		}),
		"Names":     reflect.ValueOf(func() []string { return callbackWorkloadNames }),
		"Stringify": reflect.ValueOf(func(s fmt.Stringer) string { return s.String() }),
		"Stringer":  reflect.ValueOf((*fmt.Stringer)(nil)),
		"SortIt":    reflect.ValueOf(func(x sort.Interface) { sort.Sort(x) }),
		"Interface": reflect.ValueOf((*sort.Interface)(nil)),
	}
	symbols := symtab.NewSymbolRegistry(exports)
	for _, workload := range callbackWorkloads {
		t.Run(workload.id, func(t *testing.T) {
			window := openAuditWindow(t, 0)
			census := openCensusWindow()
			service := app.NewService(hazardServiceOptions()...)
			service.UseSymbols(symbols)
			compiled := hazardCompile(t, service, workload.source)
			observations := &hazardObservations{}
			before := engine.LicenceDiagnostics().CallbackRelease
			for round := range 16 {
				got, err := service.ExecuteEntrypoint(t.Context(), compiled, workload.entry)
				require.NoError(t, err, "test harness: running %s", workload.entry)
				observations.compareString(fmt.Sprintf("%s round %d", workload.entry, round), fmt.Sprint(workload.want), fmt.Sprint(got))
				if round%4 == 3 {
					clobberArenas(t, service, 2)
				}
			}
			stats := engine.LicenceDiagnostics().CallbackRelease
			observations.note("callback release: callbackRuns=%d boundRuns=%d cachedArenaReleases=%d", stats.CallbackRuns-before.CallbackRuns,
				stats.BoundRuns-before.BoundRuns, stats.CachedArenaReleases-before.CachedArenaReleases)
			window.settle(observations)
			census.settle(observations)
			assertNoHazard(t, observations, "")
		})
	}
}

func TestCallbackPublishingTheCallersBackingSurvivesReuse(t *testing.T) {
	runReplay(t, replayCase{entry: "Run", rounds: 4, source: `package main

import (
	"retainhost"
	"sort"
)

type P struct {
	A int
	B int
}

func Run(n int) int {
	var row []P
	for i := 0; i < 4; i++ {
		row = append(row, P{A: n, B: 3 - i})
	}
	published := false
	sort.Slice(row, func(i, j int) bool {
		if !published {
			published = true
			retainhost.Retain(row)
		}
		return row[i].B < row[j].B
	})
	retainhost.Settle()
	return len(row)
}
`})
}
