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
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/sdk/stdlib"
)

type closureHost struct {
	mu      sync.Mutex
	ints    []func() []int
	strs    []func() []string
	kept    []any
	results [][]string
}

const closureSource = `package main

import (
	"closurehost"
	"strconv"
)

type Item struct {
	ID   int
	Name string
	Tags []string
}

type Box struct {
	xs []int
}

func (b *Box) Get() []int { return b.xs }

func (b Box) Copy() []int { return b.xs }

func mk(n int) string { return "closure-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*7%13) }

func ints(n int) []int {
	xs := make([]int, 8)
	for i := range xs {
		xs[i] = n*10 + i
	}
	return xs
}

func RetInts(n int) func() []int {
	xs := ints(n)
	return func() []int { return xs }
}

func RetStrings(n int) func() []string {
	s := []string{mk(n), mk(n + 1), mk(n + 2)}
	return func() []string { return s }
}

func RetString(n int) func() string {
	out := mk(n) + "/" + mk(n+1)
	return func() string { return out }
}

func RetStruct(n int) func() Item {
	it := Item{ID: n, Name: mk(n), Tags: []string{mk(n + 1), mk(n + 2)}}
	return func() Item { return it }
}

func RetMap(n int) func() map[string]int {
	m := map[string]int{mk(n): n, mk(n + 1): n + 1}
	return func() map[string]int { return m }
}

func RetNested(n int) func() func() []int {
	xs := ints(n)
	return func() func() []int {
		return func() []int { return xs }
	}
}

func RetCallsCaptured(n int) func() []int {
	xs := ints(n)
	g := func() []int { return xs }
	return func() []int { return g() }
}

func RetRecursive(n int) func() []int {
	xs := ints(n)
	var f func(k int) []int
	f = func(k int) []int {
		if k == 0 {
			return xs
		}
		return f(k - 1)
	}
	return func() []int { return f(3) }
}

func RetMethodPointer(n int) func() []int {
	b := &Box{xs: ints(n)}
	return b.Get
}

func RetMethodValue(n int) func() []int {
	b := Box{xs: ints(n)}
	return b.Copy
}

func RetOverMethod(n int) func() []int {
	b := &Box{xs: ints(n)}
	g := b.Get
	return func() []int { return g() }
}

func WriteAfter(n int) int {
	xs := ints(n)
	closurehost.KeepInts(func() []int { return xs })
	xs = ints(n + 100)
	return len(xs)
}

func WriteBySibling(n int) int {
	xs := ints(n)
	set := func(v []int) { xs = v }
	closurehost.KeepInts(func() []int { return xs })
	set(ints(n + 200))
	return len(xs)
}

func WriteByCallback(n int) int {
	xs := ints(n)
	closurehost.KeepInts(func() []int { return xs })
	closurehost.Each(1, func(i int) { xs = ints(n + 300 + i) })
	return len(xs)
}

func WriteByDefer(n int) int {
	xs := ints(n)
	closurehost.KeepInts(func() []int { return xs })
	defer func() { xs = ints(n + 400) }()
	return len(xs)
}

func DeferHandOff(n int) int {
	xs := ints(n)
	defer closurehost.KeepInts(func() []int { return xs })
	xs[0] = n + 500
	return len(xs)
}

func StringsAfter(n int) int {
	s := []string{mk(n)}
	closurehost.KeepStrings(func() []string { return s })
	s = append(s, mk(n+1))
	return len(s)
}

func CallInts(f func() []int) []int { return f() }

func CallStrings(f func() []string) []string { return f() }

func CallString(f func() string) string { return f() }

func CallStruct(f func() Item) Item { return f() }

func CallMap(f func() map[string]int) map[string]int { return f() }

func CallNested(f func() func() []int) []int { return f()() }

func Internal(n int) int {
	total := 0
	for i := 0; i < 32; i++ {
		k := i
		add := func(v int) int { return v + k + n }
		total += add(i)
	}
	return total
}

func mapInts(xs []int, f func(int) int) []int {
	out := make([]int, len(xs))
	for i, x := range xs {
		out[i] = f(x)
	}
	return out
}

func Helpers(n int) int {
	xs := ints(n)
	total := 0
	for round := 0; round < 8; round++ {
		scale := round + 1
		ys := mapInts(xs, func(x int) int { return x * scale })
		for _, y := range ys {
			total += y
		}
	}
	return total
}

func Counter(n int) int {
	count := 0
	inc := func() { count++ }
	for i := 0; i < n%17+3; i++ {
		inc()
	}
	get := func() int { return count }
	return get()
}

func Churn(n int) int {
	t := 0
	for i := 0; i < 64; i++ {
		xs := ints(n + i)
		f := func() int { return xs[3] + len(mk(n+i)) }
		t += f()
	}
	return t
}

func apply(f func(int) int, k int) int { return f(k) }

func CacheA(n int) int {
	base := n
	f := func(k int) int { return base + k }
	return apply(f, 1)
}

func CacheB(n int) int {
	m := n
	f := func(k int) int { return m * k * 1000 }
	return apply(f, 2)
}

func Render(n int) int {
	var out []string
	closurehost.Each(16, func(i int) {
		out = append(out, "<li>")
		out = append(out, mk(n+i))
		out = append(out, "</li>")
	})
	return closurehost.KeepResult(out)
}

func RenderNested(n int) int {
	var out []string
	closurehost.Each(4, func(i int) {
		closurehost.Each(3, func(j int) {
			out = append(out, mk(n+10*i+j))
		})
	})
	return closurehost.KeepResult(out)
}

func RenderMap(n int) int {
	seen := map[string]string{}
	var keys []string
	closurehost.Each(8, func(i int) {
		k := mk(n + i)
		seen[k] = mk(n + i + 100)
		keys = append(keys, k)
	})
	var out []string
	for _, k := range keys {
		out = append(out, k+"="+seen[k])
	}
	return closurehost.KeepResult(out)
}

func RenderParallel(n int) int {
	outs := make([][]string, 2)
	closurehost.Parallel(func(g int) {
		for i := 0; i < 32; i++ {
			outs[g] = append(outs[g], mk(n+100*g+i))
		}
	})
	closurehost.KeepResult(outs[0])
	return closurehost.KeepResult(outs[1])
}
`

const closureGoSource = `package main

import (
	"closurehost"
	"strconv"
)

func mk(n int) string { return "closure-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*7%13) }

func ints(n int) []int {
	xs := make([]int, 8)
	for i := range xs {
		xs[i] = n*10 + i
	}
	return xs
}

func GoHandOff(n int) int {
	xs := ints(n)
	done := make(chan bool)
	go func() {
		closurehost.KeepInts(func() []int { return xs })
		done <- true
	}()
	<-done
	return len(xs)
}

func GoMadeBefore(n int) int {
	xs := ints(n)
	f := func() {
		closurehost.KeepInts(func() []int { return xs })
	}
	done := make(chan bool)
	go func() {
		f()
		done <- true
	}()
	<-done
	return len(xs)
}

func Churn(n int) int {
	t := 0
	for i := 0; i < 64; i++ {
		xs := ints(n + i)
		f := func() int { return xs[3] + len(mk(n+i)) }
		t += f()
	}
	return t
}

func Internal(n int) int {
	total := 0
	for i := 0; i < 32; i++ {
		k := i
		add := func(v int) int { return v + k + n }
		total += add(i)
	}
	return total
}
`

type closureItem struct {
	ID   int
	Name string
	Tags []string
}

type heldFunction struct {
	function any
	caller   string
}

type closureCase struct {
	id     string
	run    func(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, h *closureHost, n int) (held []heldFunction, want []string)
	source string
}

var closureCases = []closureCase{
	closureReturned("ret-ints", "RetInts", "CallInts", func(n int) string { return fmt.Sprint(closureInts(n)) }),
	closureReturned("ret-strings", "RetStrings", "CallStrings", func(n int) string {
		return strings.Join([]string{closureMk(n), closureMk(n + 1), closureMk(n + 2)}, ",")
	}),
	closureReturned("ret-string", "RetString", "CallString", func(n int) string { return closureMk(n) + "/" + closureMk(n+1) }),
	closureReturned("ret-struct", "RetStruct", "CallStruct", func(n int) string {
		return fmt.Sprintf("%d/%s/%v", n, closureMk(n), []string{closureMk(n + 1), closureMk(n + 2)})
	}),
	closureReturned("ret-map", "RetMap", "CallMap", func(n int) string {
		return renderClosureResult(map[string]int{closureMk(n): n, closureMk(n + 1): n + 1})
	}),
	closureReturned("nested", "RetNested", "CallNested", func(n int) string { return fmt.Sprint(closureInts(n)) }),
	closureReturned("calls-captured", "RetCallsCaptured", "CallInts", func(n int) string { return fmt.Sprint(closureInts(n)) }),
	closureReturned("recursive", "RetRecursive", "CallInts", func(n int) string { return fmt.Sprint(closureInts(n)) }),
	closureReturned("method-pointer", "RetMethodPointer", "CallInts", func(n int) string { return fmt.Sprint(closureInts(n)) }),
	closureReturned("method-value", "RetMethodValue", "CallInts", func(n int) string { return fmt.Sprint(closureInts(n)) }),
	closureReturned("over-method", "RetOverMethod", "CallInts", func(n int) string { return fmt.Sprint(closureInts(n)) }),
	closureKept("write-after", "WriteAfter", func(n int) string { return fmt.Sprint(closureInts(n + 100)) }),
	closureKept("write-by-sibling", "WriteBySibling", func(n int) string { return fmt.Sprint(closureInts(n + 200)) }),
	closureKept("write-by-callback", "WriteByCallback", func(n int) string { return fmt.Sprint(closureInts(n + 300)) }),
	closureKept("write-by-defer", "WriteByDefer", func(n int) string { return fmt.Sprint(closureInts(n + 400)) }),
	closureKept("defer-hand-off", "DeferHandOff", func(n int) string {
		xs := closureInts(n)
		xs[0] = n + 500
		return fmt.Sprint(xs)
	}),
	closureKept("strings-after", "StringsAfter", func(n int) string { return closureMk(n) + "," + closureMk(n+1) }),
	closureOnGoroutines(closureKept("go-hand-off", "GoHandOff", func(n int) string { return fmt.Sprint(closureInts(n)) })),
	closureOnGoroutines(closureKept("go-made-before", "GoMadeBefore", func(n int) string { return fmt.Sprint(closureInts(n)) })),
}

func (h *closureHost) keepInts(fn func() []int) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ints = append(h.ints, fn)
	return len(h.ints)
}

func (h *closureHost) keepStrings(fn func() []string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.strs = append(h.strs, fn)
	return len(h.strs)
}

func (h *closureHost) keepResult(s []string) int {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.results = append(h.results, s)
	return len(s)
}

func (h *closureHost) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ints, h.strs, h.kept, h.results = nil, nil, nil, nil
}

func closureInts(count int) []int {
	xs := make([]int, 8)
	for i := range xs {
		xs[i] = count*10 + i
	}
	return xs
}

func closureMk(n int) string { return "closure-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*7%13) }

func closureService(t *testing.T) (*app.Service, *program.CompiledFileSet, *closureHost) {
	t.Helper()
	return closureServiceFor(t, closureSource)
}

func closureServiceFor(t *testing.T, source string) (*app.Service, *program.CompiledFileSet, *closureHost) {
	t.Helper()
	host := &closureHost{}
	exports := maps.Clone(stdlib.Exports())
	exports["closurehost"] = map[string]reflect.Value{
		"KeepInts":    reflect.ValueOf(func(fn func() []int) int { return host.keepInts(fn) }),
		"KeepStrings": reflect.ValueOf(func(fn func() []string) int { return host.keepStrings(fn) }),
		"KeepResult":  reflect.ValueOf(func(s []string) int { return host.keepResult(s) }),
		"Each": reflect.ValueOf(func(n int, fn func(int)) {
			for index := range n {
				fn(index)
			}
		}),
		"Parallel": reflect.ValueOf(func(body func(int)) {
			var wg sync.WaitGroup
			var start sync.WaitGroup
			start.Add(1)
			wg.Go(func() {
				start.Wait()
				body(1)
			})
			start.Done()
			body(0)
			wg.Wait()
		}),
	}
	service := app.NewService(hazardServiceOptions()...)
	service.UseSymbols(symtab.NewSymbolRegistry(exports))
	return service, hazardCompile(t, service, source), host
}

func reuseClosureArenas(t *testing.T, service *app.Service, compiled *program.CompiledFileSet) {
	t.Helper()
	clobberArenas(t, service, 16)
	for round := range 8 {
		hazardCall(t, service, compiled, "Churn", 1000*round)
		hazardCall(t, service, compiled, "Internal", round)
	}
}

func renderClosureResult(value any) string {
	switch x := value.(type) {
	case []int:
		return fmt.Sprint(x)
	case []string:
		return strings.Join(x, ",")
	case map[string]int:
		keys := make([]string, 0, len(x))
		for key := range x {
			keys = append(keys, key)
		}
		slicesSort(keys)
		var out []string
		for _, key := range keys {
			out = append(out, key+":"+strconv.Itoa(x[key]))
		}
		return strings.Join(out, ",")
	default:
		rv := reflect.ValueOf(value)
		if rv.Kind() == reflect.Struct && rv.NumField() >= 3 {
			return fmt.Sprintf("%d/%s/%v", rv.Field(0).Int(), rv.Field(1).String(), rv.Field(2).Interface())
		}
		return fmt.Sprint(value)
	}
}

func slicesSort(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

func callHeldFunction(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, held heldFunction) string {
	t.Helper()
	if held.caller != "" {
		return renderClosureResult(hazardCall(t, service, compiled, held.caller, held.function))
	}
	function := reflect.ValueOf(held.function)
	if function.Kind() != reflect.Func {
		return fmt.Sprintf("not a function: %T", held.function)
	}
	results := function.Call(nil)
	if len(results) != 1 {
		return fmt.Sprintf("results=%d", len(results))
	}
	if results[0].Kind() == reflect.Func {
		return callHeldFunction(t, service, compiled, heldFunction{function: results[0].Interface()})
	}
	return renderClosureResult(results[0].Interface())
}

func closureReturned(id, entry, caller string, want func(n int) string) closureCase {
	return closureCase{id: id, run: func(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, _ *closureHost, n int) ([]heldFunction, []string) {
		result := hazardCall(t, service, compiled, entry, n)
		return []heldFunction{{function: result, caller: caller}}, []string{want(n)}
	}}
}

func closureKept(id, entry string, want func(n int) string) closureCase {
	return closureCase{id: id, run: func(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, host *closureHost, seed int) ([]heldFunction, []string) {
		host.mu.Lock()
		ints, strs := len(host.ints), len(host.strs)
		host.mu.Unlock()
		hazardCall(t, service, compiled, entry, seed)
		host.mu.Lock()
		defer host.mu.Unlock()
		held := make([]heldFunction, 0, len(host.ints)-ints+len(host.strs)-strs)
		for _, fn := range host.ints[ints:] {
			held = append(held, heldFunction{function: fn})
		}
		for _, fn := range host.strs[strs:] {
			held = append(held, heldFunction{function: fn})
		}
		return held, []string{want(seed)}
	}}
}

func closureOnGoroutines(c closureCase) closureCase {
	c.source = closureGoSource
	return c
}

func TestClosuresKeptByTheHostSurviveReuse(t *testing.T) {
	previous := engine.SetLicenceCountingForTest(true)
	t.Cleanup(func() { engine.SetLicenceCountingForTest(previous) })
	for _, tc := range closureCases {
		t.Run(tc.id, func(t *testing.T) {
			window := openAuditWindow(t, 0)
			census := openCensusWindow()
			source := tc.source
			if source == "" {
				source = closureSource
			}
			service, compiled, host := closureServiceFor(t, source)
			beforeWalks, beforeChunks := engine.LicenceDiagnostics().CellWalks, engine.LicenceDiagnostics().ClosureChunks
			var held []heldFunction
			var want []string
			for round := range hazardRounds {
				fns, wants := tc.run(t, service, compiled, host, 100*round+7)
				require.Len(t, fns, len(wants), "test harness: the case must hand the host one function per value")
				held = append(held, fns...)
				want = append(want, wants...)
			}
			afterWalks, afterChunks := engine.LicenceDiagnostics().CellWalks, engine.LicenceDiagnostics().ClosureChunks
			read := func() string {
				out := make([]string, len(held))
				for index, fn := range held {
					out[index] = callHeldFunction(t, service, compiled, fn)
				}
				return strings.Join(out, " | ")
			}
			observations := &hazardObservations{}
			observations.compareString("kept closures straight after the runs", strings.Join(want, " | "), read())
			reuseClosureArenas(t, service, compiled)
			observations.compareString("kept closures after reuse", strings.Join(want, " | "), read())
			found := afterWalks.WrappersFound - beforeWalks.WrappersFound
			observations.note("cell walks: closuresWalked=%d cellsWalked=%d wrappersFound=%d opaque=%d handedOffStores=%d; closure chunks: closures=%d cells=%d chunks=%d revoked=%d",
				afterWalks.ClosuresWalked-beforeWalks.ClosuresWalked, afterWalks.CellsWalked-beforeWalks.CellsWalked, found,
				afterWalks.WrappersOpaque-beforeWalks.WrappersOpaque, afterWalks.HandedOffStores-beforeWalks.HandedOffStores,
				afterChunks.Closures-beforeChunks.Closures, afterChunks.Cells-beforeChunks.Cells, afterChunks.Chunks-beforeChunks.Chunks,
				afterChunks.ChunksRevoked-beforeChunks.ChunksRevoked)
			if engine.ClosureCaptureWalksCells() &&
				afterWalks.ClosuresWalked == beforeWalks.ClosuresWalked && found == 0 {
				observations.fire("closure-walk-not-applied", "no closure or method value was walked through its cells")
			}
			window.settle(observations)
			census.settle(observations)
			runtime.KeepAlive(held)
			runtime.KeepAlive(host)
			assertNoHazard(t, observations, "")
		})
	}
}

func TestClosureCallSiteCacheRunsEachClosuresOwnCode(t *testing.T) {
	previous := engine.SetLicenceCountingForTest(true)
	t.Cleanup(func() { engine.SetLicenceCountingForTest(previous) })
	window := openAuditWindow(t, 0)
	census := openCensusWindow()
	service, compiled, _ := closureService(t)
	before := engine.LicenceDiagnostics().ClosureChunks
	got, want := make([]string, 0, 9*hazardRounds), make([]string, 0, 9*hazardRounds)
	for round := range 3 * hazardRounds {
		seed := 10*round + 3
		if round%2 == 0 {
			got = append(got, fmt.Sprint(hazardCall(t, service, compiled, "CacheA", seed)))
			want = append(want, strconv.Itoa(seed+1))
		} else {
			got = append(got, fmt.Sprint(hazardCall(t, service, compiled, "CacheB", seed)))
			want = append(want, strconv.Itoa(seed*2000))
		}
	}
	after := engine.LicenceDiagnostics().ClosureChunks
	observations := &hazardObservations{}
	observations.compareString("closures through one cached call site", strings.Join(want, ","), strings.Join(got, ","))
	observations.note("closure chunks: closures=%d chunks=%d", after.Closures-before.Closures, after.Chunks-before.Chunks)
	if after.Closures == before.Closures {
		observations.fire("closure-chunks-not-applied", "no closure was placed in a closure chunk")
	}
	window.settle(observations)
	census.settle(observations)
	assertNoHazard(t, observations, "")
}

func TestCallbacksWritingTheirCallerSurviveReuse(t *testing.T) {
	previous := engine.SetLicenceCountingForTest(true)
	t.Cleanup(func() { engine.SetLicenceCountingForTest(previous) })
	cases := []struct {
		id, entry string
		want      func(n int) [][]string
	}{
		{id: "render", entry: "Render", want: func(seed int) [][]string {
			out := make([]string, 0, 48)
			for i := range 16 {
				out = append(out, "<li>", closureMk(seed+i), "</li>")
			}
			return [][]string{out}
		}},
		{id: "render-nested", entry: "RenderNested", want: func(seed int) [][]string {
			out := make([]string, 0, 12)
			for i := range 4 {
				for j := range 3 {
					out = append(out, closureMk(seed+10*i+j))
				}
			}
			return [][]string{out}
		}},
		{id: "render-map", entry: "RenderMap", want: func(seed int) [][]string {
			out := make([]string, 0, 8)
			for i := range 8 {
				out = append(out, closureMk(seed+i)+"="+closureMk(seed+i+100))
			}
			return [][]string{out}
		}},
		{id: "render-parallel", entry: "RenderParallel", want: func(seed int) [][]string {
			outs := make([][]string, 2)
			for g := range 2 {
				for i := range 32 {
					outs[g] = append(outs[g], closureMk(seed+100*g+i))
				}
			}
			return outs
		}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			window := openAuditWindow(t, 0)
			census := openCensusWindow()
			service, compiled, host := closureService(t)
			before := engine.LicenceDiagnostics().ParkedCallers
			var want []string
			for round := range hazardRounds {
				seed := 100*round + 11
				hazardCall(t, service, compiled, tc.entry, seed)
				for _, s := range tc.want(seed) {
					want = append(want, strings.Join(s, ","))
				}
			}
			after := engine.LicenceDiagnostics().ParkedCallers
			read := func() string {
				host.mu.Lock()
				defer host.mu.Unlock()
				out := make([]string, len(host.results))
				for index, s := range host.results {
					out[index] = strings.Join(s, ",")
				}
				return strings.Join(out, " | ")
			}
			observations := &hazardObservations{}
			observations.compareString("results straight after the runs", strings.Join(want, " | "), read())
			reuseClosureArenas(t, service, compiled)
			observations.compareString("results after reuse", strings.Join(want, " | "), read())
			observations.note("parked callers: parkedWalks=%d parkedReads=%d fallbacks=%d declined=%d",
				after.ParkedWalks-before.ParkedWalks, after.ParkedReads-before.ParkedReads,
				after.Fallbacks-before.Fallbacks, after.Declined-before.Declined)
			window.settle(observations)
			census.settle(observations)
			runtime.KeepAlive(host)
			assertNoHazard(t, observations, "")
		})
	}
}

func TestInRunClosuresMatchNativeGo(t *testing.T) {

	previous := engine.SetLicenceCountingForTest(true)
	t.Cleanup(func() { engine.SetLicenceCountingForTest(previous) })
	window := openAuditWindow(t, 0)
	census := openCensusWindow()
	service, compiled, _ := closureService(t)
	before := engine.LicenceDiagnostics().ClosureChunks
	nativeHelpers := func(seed int) int {
		total := 0
		for round := range 8 {
			for _, x := range closureInts(seed) {
				total += x * (round + 1)
			}
		}
		return total
	}
	got, want := make([]string, 0, 9*hazardRounds), make([]string, 0, 9*hazardRounds)
	for round := range 3 * hazardRounds {
		seed := 7*round + 1
		got = append(got, fmt.Sprint(hazardCall(t, service, compiled, "Internal", seed)), fmt.Sprint(hazardCall(t, service, compiled, "Helpers", seed)),
			fmt.Sprint(hazardCall(t, service, compiled, "Counter", seed)))
		want = append(want, strconv.Itoa(992+32*seed), strconv.Itoa(nativeHelpers(seed)), strconv.Itoa(seed%17+3))
		clobberArenas(t, service, 1)
	}
	after := engine.LicenceDiagnostics().ClosureChunks
	observations := &hazardObservations{}
	observations.compareString("in-run closure results", strings.Join(want, ","), strings.Join(got, ","))
	observations.note("closure chunks: closures=%d cells=%d chunks=%d revoked=%d", after.Closures-before.Closures, after.Cells-before.Cells,
		after.Chunks-before.Chunks, after.ChunksRevoked-before.ChunksRevoked)
	if after.Closures == before.Closures {
		observations.fire("closure-chunks-not-applied", "no closure was placed in a closure chunk")
	}
	window.settle(observations)
	census.settle(observations)
	assertNoHazard(t, observations, "")
}
