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
	"os"
	"os/exec"
	"reflect"
	"slices"
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

type GoroutineNode struct {
	Name string
	N    int
}

type GoroutineHolder struct {
	Names []string
	Label string
}

type goroutineHost struct {
	mu      sync.Mutex
	kept    []any
	bufs    [][]string
	anyBufs [][]any
	holders []*GoroutineHolder
	funcs   []func() string
}

const goroutineSource = `package main

import (
	"goroutinehost"
	"strconv"
	"sync"
)

func mk(n int) string { return "go-" + strconv.Itoa(n) + "-" + strconv.Itoa(n*5) }

func GoStrings(n int) int {
	ch := make(chan string, 4)
	for i := 0; i < 4; i++ {
		go func(prefix string, k int) { ch <- prefix + mk(k) }("g", n+i)
	}
	for i := 0; i < 4; i++ {
		goroutinehost.Keep(<-ch)
	}
	return n
}

func WorkersMap(n int) int {
	var mu sync.Mutex
	var wg sync.WaitGroup
	m := map[string]string{}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			v := mk(k)
			mu.Lock()
			m["k"+strconv.Itoa(k)] = v
			mu.Unlock()
		}(n + i)
	}
	wg.Wait()
	goroutinehost.Keep(m)
	return len(m)
}

func ChanNodes(n int) int {
	ch := make(chan *goroutinehost.Node, 4)
	go func() {
		for i := 0; i < 4; i++ {
			ch <- &goroutinehost.Node{Name: mk(n + i), N: i}
		}
		close(ch)
	}()
	for node := range ch {
		goroutinehost.Keep(node)
	}
	return n
}

func ParentSlice(n int) int {
	parts := []string{mk(n), mk(n + 1)}
	parts = append(parts, mk(n+2))
	done := make(chan int)
	go func(p []string) {
		goroutinehost.KeepStrings(p)
		done <- 1
	}(parts)
	<-done
	return n
}

func ToGoroutine(n int) int {
	ch := make(chan string, 4)
	done := make(chan int)
	go func() {
		for s := range ch {
			goroutinehost.Keep(s)
		}
		done <- 1
	}()
	for i := 0; i < 4; i++ {
		ch <- mk(n+i) + "~"
	}
	close(ch)
	<-done
	return n
}

func Captured(n int) int {
	parts := []string{mk(n), mk(n + 1)}
	label := mk(n+2) + "!"
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		goroutinehost.KeepStrings(parts)
		goroutinehost.Keep(label)
	}()
	wg.Wait()
	return n
}

func Blind(n int) int {
	parts := []string{mk(n), mk(n + 1)}
	done := make(chan int)
	go func(p []string) {
		goroutinehost.Fn(func() string { return "opaque " + p[0] })
		own := mk(n+3) + "?"
		goroutinehost.Keep(own)
		goroutinehost.KeepStrings(p)
		goroutinehost.Keep(&goroutinehost.Node{Name: mk(n + 4), N: n})
		done <- 1
	}(parts)
	<-done
	for i := 0; i < 8; i++ {
		_ = mk(n + 100 + i)
	}
	return n
}

func MemoShared(n int) int {
	h := goroutinehost.Holder()
	goroutinehost.Keep(h)
	release := make(chan int)
	done := make(chan int)
	go func() { <-release; done <- 1 }()
	goroutinehost.Keep(h)
	h.Label = mk(n)
	h.Names = []string{mk(n + 1), mk(n + 2)}
	goroutinehost.Keep(h)
	release <- 1
	<-done
	return n
}

func CopyPrivate(n int) int {
	buf := goroutinehost.Buf()
	goroutinehost.KeepStrings(buf)
	src := []string{mk(n), mk(n + 1)}
	copy(buf, src)
	goroutinehost.KeepStrings(buf)
	return n
}

func CopyMakePrivate(n int) int {
	buf := goroutinehost.Buf()
	goroutinehost.KeepStrings(buf)
	src := make([]string, 2)
	for i := range src {
		src[i] = mk(n + i)
	}
	copy(buf, src)
	goroutinehost.KeepStrings(buf)
	return n
}

func CopyShared(n int) int {
	buf := goroutinehost.Buf()
	goroutinehost.KeepStrings(buf)
	release := make(chan int)
	done := make(chan int)
	go func() { <-release; done <- 1 }()
	src := []string{mk(n), mk(n + 1)}
	copy(buf, src)
	goroutinehost.KeepStrings(buf)
	release <- 1
	<-done
	return n
}

func CopyAnyShared(n int) int {
	buf := goroutinehost.AnyBuf()
	goroutinehost.KeepAny(buf)
	release := make(chan int)
	done := make(chan int)
	go func() { <-release; done <- 1 }()
	src := []any{mk(n), mk(n + 1)}
	copy(buf, src)
	goroutinehost.KeepAny(buf)
	release <- 1
	<-done
	return n
}

func NativeGoroutine(n int) int {
	parts := []string{mk(n), mk(n + 1)}
	return len(goroutinehost.RunAsync(func(k int) string {
		goroutinehost.Fn(func() string { return "opaque " + parts[0] })
		s := mk(k) + "@" + parts[1]
		goroutinehost.Keep(s)
		goroutinehost.KeepStrings(parts)
		return s
	}, n))
}

func Churn(n int) int {
	total := 0
	for i := 0; i < 64; i++ {
		total += len(mk(n+i) + "-churn-" + strconv.Itoa(i))
	}
	return total
}
`

type goroutineCase struct {
	id, entry string
	want      func(n int) []string
	counter   string
}

var goroutineCases = []goroutineCase{
	{id: "go-strings", entry: "GoStrings", want: func(n int) []string {
		return []string{"g" + goroutineMk(n), "g" + goroutineMk(n+1), "g" + goroutineMk(n+2), "g" + goroutineMk(n+3)}
	}},
	{id: "workers-map", entry: "WorkersMap", want: func(seed int) []string {
		keys := make([]string, 0, 4)
		for i := range 4 {
			keys = append(keys, "k"+strconv.Itoa(seed+i))
		}
		slices.Sort(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			k, _ := strconv.Atoi(strings.TrimPrefix(key, "k"))
			parts = append(parts, key+"="+goroutineMk(k))
		}
		return []string{strings.Join(parts, ";")}
	}},
	{id: "chan-nodes", entry: "ChanNodes", want: func(n int) []string {
		return []string{goroutineMk(n) + "#0", goroutineMk(n+1) + "#1", goroutineMk(n+2) + "#2", goroutineMk(n+3) + "#3"}
	}},
	{id: "parent-slice", entry: "ParentSlice", want: func(n int) []string {
		return []string{goroutineMk(n) + "," + goroutineMk(n+1) + "," + goroutineMk(n+2)}
	}},
	{id: "to-goroutine", entry: "ToGoroutine", want: func(n int) []string {
		return []string{goroutineMk(n) + "~", goroutineMk(n+1) + "~", goroutineMk(n+2) + "~", goroutineMk(n+3) + "~"}
	}},
	{id: "captured", entry: "Captured", want: func(n int) []string {
		return []string{goroutineMk(n) + "," + goroutineMk(n+1), goroutineMk(n+2) + "!"}
	}},
	{id: "blind", entry: "Blind", counter: "local-blind", want: func(n int) []string {
		return []string{goroutineMk(n+3) + "?", goroutineMk(n) + "," + goroutineMk(n+1), goroutineMk(n+4) + "#" + strconv.Itoa(n)}
	}},
	{id: "memo-shared", entry: "MemoShared", counter: "memo", want: func(n int) []string {
		final := goroutineMk(n) + "|" + goroutineMk(n+1) + "," + goroutineMk(n+2)
		return []string{final, final, final}
	}},
}

var goroutineContainerCases = []goroutineCase{
	{id: "copy-private", entry: "CopyPrivate", want: func(n int) []string {
		both := goroutineMk(n) + "," + goroutineMk(n+1)
		return []string{both, both}
	}},
	{id: "copy-make-private", entry: "CopyMakePrivate", want: func(n int) []string {
		both := goroutineMk(n) + "," + goroutineMk(n+1)
		return []string{both, both}
	}},
	{id: "copy-shared", entry: "CopyShared", counter: "memo", want: func(n int) []string {
		both := goroutineMk(n) + "," + goroutineMk(n+1)
		return []string{both, both}
	}},
	{id: "copy-any-shared", entry: "CopyAnyShared", want: func(n int) []string {
		both := goroutineMk(n) + "," + goroutineMk(n+1)
		return []string{both, both}
	}},
}

const goroutineRounds = 12

var goroutineMicros = []struct {
	id, source string
	want       int
}{
	{id: "go_string_args", want: 100, source: `package main

import "sync"

func run() int {
	var wg sync.WaitGroup
	var mu sync.Mutex
	total := 0
	for i := 0; i < 8; i++ {
		b := []byte{'k', byte('a' + i)}
		key := string(b) + "-suffix"
		wg.Add(1)
		go func(k string, n int) {
			defer wg.Done()
			mu.Lock()
			total += len(k) + n
			mu.Unlock()
		}(key, i)
	}
	wg.Wait()
	return total
}
`},
	{id: "workers_results_map", want: 32, source: `package main

import "sync"

type Item struct{ K string; V int }

func run() int {
	var wg sync.WaitGroup
	var mu sync.Mutex
	m := map[string]int{}
	for w := 0; w < 4; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < 8; i++ {
				b := []byte{'w', byte('a' + w), byte('a' + i)}
				it := &Item{K: string(b), V: i}
				mu.Lock()
				m[it.K] = it.V
				mu.Unlock()
			}
		}(w)
	}
	wg.Wait()
	return len(m)
}
`},
	{id: "chan_ptr_from_goroutine", want: 136, source: `package main

type Node struct{ Name string; N int; Next *Node }

func run() int {
	ch := make(chan *Node, 16)
	go func() {
		for i := 0; i < 16; i++ {
			ch <- &Node{Name: "n", N: i}
		}
		close(ch)
	}()
	s := 0
	for n := range ch {
		s += n.N + len(n.Name)
	}
	return s
}
`},
	{id: "go_closure_capture_only", want: 56, source: `package main

import "sync"

func run() int {
	var wg sync.WaitGroup
	results := make([]int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = i * 2
		}()
	}
	wg.Wait()
	s := 0
	for _, v := range results {
		s += v
	}
	return s
}
`},
}

const mapRaceChildEnvVar = "PIPIT_TEST_MAP_RACE_CHILD"

const mapRaceSource = `package main

import "sync"

func Run(n int) int {
	byKey := map[int][]int{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 64; j++ {
				mu.Lock()
				byKey[i*64+j] = []int{i, j}
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	return len(byKey)
}
`

func (h *goroutineHost) keep(v any) {
	h.mu.Lock()
	h.kept = append(h.kept, v)
	h.mu.Unlock()
}

func renderGoroutineKept(kept any) string {
	switch value := kept.(type) {
	case string:
		return value
	case []string:
		return strings.Join(value, ",")
	case map[string]string:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		slices.Sort(keys)
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, key+"="+value[key])
		}
		return strings.Join(parts, ";")
	case *GoroutineNode:
		return value.Name + "#" + strconv.Itoa(value.N)
	case []any:
		parts := make([]string, 0, len(value))
		for _, element := range value {
			parts = append(parts, fmt.Sprint(element))
		}
		return strings.Join(parts, ",")
	case *GoroutineHolder:
		return value.Label + "|" + strings.Join(value.Names, ",")
	default:
		return fmt.Sprintf("%T:%v", kept, kept)
	}
}

func goroutineMk(n int) string { return fmt.Sprintf("go-%d-%d", n, n*5) }

func goroutineService(t *testing.T) (*app.Service, *program.CompiledFileSet, *goroutineHost) {
	t.Helper()
	host := &goroutineHost{}
	exports := maps.Clone(stdlib.Exports())
	exports["goroutinehost"] = map[string]reflect.Value{
		"Node":        reflect.ValueOf((*GoroutineNode)(nil)),
		"Keep":        reflect.ValueOf(func(v any) { host.keep(v) }),
		"KeepStrings": reflect.ValueOf(func(v []string) { host.keep(v) }),
		"KeepAny":     reflect.ValueOf(func(v []any) { host.keep(v) }),
		"Fn": reflect.ValueOf(func(fn func() string) {
			host.mu.Lock()
			host.funcs = append(host.funcs, fn)
			host.mu.Unlock()
		}),
		"Buf": reflect.ValueOf(func() []string {
			b := make([]string, 2)
			host.mu.Lock()
			host.bufs = append(host.bufs, b)
			host.mu.Unlock()
			return b
		}),
		"AnyBuf": reflect.ValueOf(func() []any {
			b := make([]any, 2)
			host.mu.Lock()
			host.anyBufs = append(host.anyBufs, b)
			host.mu.Unlock()
			return b
		}),
		"Holder": reflect.ValueOf(func() *GoroutineHolder {
			holder := &GoroutineHolder{Names: []string{"host-a", "host-b"}, Label: "host"}
			host.mu.Lock()
			host.holders = append(host.holders, holder)
			host.mu.Unlock()
			return holder
		}),
		"RunAsync": reflect.ValueOf(func(fn func(int) string, n int) string {
			result := make(chan string)
			go func() { result <- fn(n) }()
			return <-result
		}),
	}
	service := app.NewService(hazardServiceOptions()...)
	service.UseSymbols(symtab.NewSymbolRegistry(exports))
	compiled := hazardCompile(t, service, goroutineSource)
	return service, compiled, host
}

func scopingCounterMoved(id string, before, after engine.BlindScopeStats) (bool, int64) {
	switch id {
	case "memo":
		delta := after.SharedMemoHits - before.SharedMemoHits
		return delta > 0, delta
	case "local-blind":
		delta := after.LocalBlindHandOffs + after.LocalByteBlindHandOffs - before.LocalBlindHandOffs - before.LocalByteBlindHandOffs
		return delta > 0, delta
	case "foreign-skip":
		delta := after.ForeignLocalSkips - before.ForeignLocalSkips
		return delta > 0, delta
	default:
		return true, 0
	}
}

func runGoroutineCases(t *testing.T, cases []goroutineCase) {
	t.Helper()
	for _, testCase := range cases {
		t.Run(testCase.id, func(t *testing.T) {
			window := openAuditWindow(t, 0)
			census := openCensusWindow()
			service, compiled, host := goroutineService(t)
			observations := &hazardObservations{}
			before := engine.LicenceDiagnostics().BlindScope
			want := make([]string, 0, goroutineRounds)
			for round := range goroutineRounds {
				n := round * 10
				hazardCall(t, service, compiled, testCase.entry, n)
				want = append(want, testCase.want(n)...)
			}
			after := engine.LicenceDiagnostics().BlindScope
			clobberArenas(t, service, 16)
			for round := range 8 {
				hazardCall(t, service, compiled, "Churn", 1000*round)
			}
			host.mu.Lock()
			got := make([]string, 0, len(host.kept))
			for _, v := range host.kept {
				got = append(got, renderGoroutineKept(v))
			}
			host.mu.Unlock()
			slices.Sort(got)
			slices.Sort(want)
			observations.compareString(testCase.entry+" kept", strings.Join(want, " / "), strings.Join(got, " / "))
			observations.note("scoping: sharedMemoHits=%d localBlind=%d localByteBlind=%d foreignLocalSkips=%d",
				after.SharedMemoHits-before.SharedMemoHits, after.LocalBlindHandOffs-before.LocalBlindHandOffs,
				after.LocalByteBlindHandOffs-before.LocalByteBlindHandOffs, after.ForeignLocalSkips-before.ForeignLocalSkips)
			window.settle(observations)
			census.settle(observations)
			assertNoHazard(t, observations, "")
			if testCase.counter != "" {
				moved, delta := scopingCounterMoved(testCase.counter, before, after)
				require.True(t, moved, "the %s counter must move on %s (delta %d)", testCase.counter, testCase.entry, delta)
			}
			runtimeKeepAlive(host)
		})
	}
}

func runtimeKeepAlive(h *goroutineHost) {
	h.mu.Lock()
	_ = len(h.kept) + len(h.bufs) + len(h.anyBufs) + len(h.holders) + len(h.funcs)
	h.mu.Unlock()
}

func TestGoroutineStoresSurviveReuse(t *testing.T) {
	runGoroutineCases(t, goroutineCases)
}

func TestCopyIntoHostContainersSurvivesReuse(t *testing.T) {
	runGoroutineCases(t, goroutineContainerCases)
}

func TestCallbackOnANativeStartedGoroutineCountsItsBlindHandOffs(t *testing.T) {
	window := openAuditWindow(t, 0)
	census := openCensusWindow()
	service, compiled, host := goroutineService(t)
	observations := &hazardObservations{}
	before, reuseBefore := engine.LicenceDiagnostics().BlindScope, engine.LicenceDiagnostics().Reuse
	want := make([]string, 0, 2*goroutineRounds)
	for round := range goroutineRounds {
		n := round * 10
		hazardCall(t, service, compiled, "NativeGoroutine", n)
		s := goroutineMk(n) + "@" + goroutineMk(n+1)
		want = append(want, s, goroutineMk(n)+","+goroutineMk(n+1))
	}
	after, reuseAfter := engine.LicenceDiagnostics().BlindScope, engine.LicenceDiagnostics().Reuse
	clobberArenas(t, service, 16)
	host.mu.Lock()
	got := make([]string, 0, len(host.kept))
	for _, v := range host.kept {
		got = append(got, renderGoroutineKept(v))
	}
	host.mu.Unlock()
	slices.Sort(got)
	slices.Sort(want)
	observations.compareString("NativeGoroutine kept", strings.Join(want, " / "), strings.Join(got, " / "))
	callbackBlind := reuseAfter.BlindCallbackHandOffs - reuseBefore.BlindCallbackHandOffs
	observations.note("blindCallbackHandOffs=%d localBlind=%d", callbackBlind, after.LocalBlindHandOffs-before.LocalBlindHandOffs)
	window.settle(observations)
	census.settle(observations)
	assertNoHazard(t, observations, "")

	require.Positive(t, callbackBlind, "a callback VM on a native-started goroutine still counts its blind hand-offs process-wide")

	runtimeKeepAlive(host)
}

func TestGoroutineBenchmarkProgramsMatchNativeGo(t *testing.T) {
	symbols := symtab.NewSymbolRegistry(stdlib.Exports())
	for _, micro := range goroutineMicros {
		t.Run(micro.id, func(t *testing.T) {
			window := openAuditWindow(t, 0)
			census := openCensusWindow()
			service := app.NewService(hazardServiceOptions()...)
			service.UseSymbols(symbols)
			compiled := hazardCompile(t, service, micro.source)
			observations := &hazardObservations{}
			before := engine.LicenceDiagnostics().BlindScope
			for round := range 32 {
				got, err := service.ExecuteEntrypoint(t.Context(), compiled, "run")
				require.NoError(t, err)
				observations.compareString(fmt.Sprintf("run round %d", round), strconv.Itoa(micro.want), fmt.Sprint(got))
				if round%8 == 7 {
					clobberArenas(t, service, 2)
				}
			}
			after := engine.LicenceDiagnostics().BlindScope
			observations.note("scoping: sharedMemoHits=%d localBlind=%d localByteBlind=%d foreignLocalSkips=%d",
				after.SharedMemoHits-before.SharedMemoHits, after.LocalBlindHandOffs-before.LocalBlindHandOffs,
				after.LocalByteBlindHandOffs-before.LocalByteBlindHandOffs, after.ForeignLocalSkips-before.ForeignLocalSkips)
			window.settle(observations)
			census.settle(observations)
			assertNoHazard(t, observations, "")
		})
	}
}

func TestChildGoroutineStoresIntoParentContainersSurviveReuse(t *testing.T) {
	window := openAuditWindow(t, 0)
	before := engine.LicenceDiagnostics().Reuse
	service := newHazardService(t)
	compiled := hazardCompile(t, service, `package main

import (
	"strconv"
	"sync"
)

func churn(n int) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sink := 0
			for j := 0; j < 2000; j++ {
				xs := []int{-1, -1, -1, j}
				s := "ZZZZZZZZZZZZ" + strconv.Itoa(j+i)
				sink += len(xs) + len(s)
			}
			_ = sink
		}(i)
	}
	wg.Wait()
}

func Run(n int) int {
	slices := make([][]int, n)
	boxes := make([]*[3]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			xs := []int{i, i * 2, i * 3}
			slices[i] = xs
			box := [3]int{i, i * 2, i * 3}
			boxes[i] = &box
		}(i)
	}
	wg.Wait()
	churn(n)
	bad := 0
	for i := 0; i < n; i++ {
		for _, v := range [][]int{slices[i], boxes[i][:]} {
			if len(v) != 3 || v[0] != i || v[1] != 2*i || v[2] != 3*i {
				bad++
			}
		}
	}
	return bad
}
`)
	observations := &hazardObservations{}
	for round := range 4 {
		if bad := hazardCall(t, service, compiled, "Run", 32); bad != any(0) {
			observations.fire("guest-value-changed", "round %d: %v child-built values read wrong in the parent", round, bad)
		}
	}
	window.settle(observations)
	noteLicenceDelta(observations, before)
	assertNoHazard(t, observations, "")
}

func TestSpawnWalkNeverIteratesAGuestLockedMap(t *testing.T) {
	if os.Getenv(mapRaceChildEnvVar) == "1" {
		service := newHazardService(t)
		compiled := hazardCompile(t, service, mapRaceSource)
		for range 20 {
			if got := hazardCall(t, service, compiled, "Run", 64); got != any(64*64) {
				t.Logf("map-race wrong-count %v", got)
			}
		}
		return
	}
	observations := &hazardObservations{}
	const runs = 5
	fatal, wrong, clean := 0, 0, 0
	for run := range runs {
		command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestSpawnWalkNeverIteratesAGuestLockedMap$", "-test.count=1", "-test.v")
		command.Env = append(os.Environ(), mapRaceChildEnvVar+"=1")
		output, err := command.CombinedOutput()
		text := string(output)
		switch {
		case strings.Contains(text, "concurrent map iteration and map write") || strings.Contains(text, "concurrent map read and map write"):
			fatal++
			if fatal == 1 {
				frame := "unknown"
				if index := strings.Index(text, "pipit.sh/pipit/internal/engine."); index >= 0 {
					frame = strings.SplitN(text[index:], "\n", 2)[0]
				}
				observations.fire("fatal-concurrent-map-iteration", "run %d aborted the process; first engine frame %s", run, frame)
			}
		case strings.Contains(text, "WARNING: DATA RACE"):
			observations.fire("race-detected", "run %d: the race detector reported a data race", run)
		case strings.Contains(text, "map-race wrong-count"):
			wrong++
		case err == nil:
			clean++
		default:
			observations.note("run %d failed otherwise: %v", run, err)
		}
	}
	if wrong > 0 {
		observations.fire("guest-value-changed", "%d runs counted the wrong number of map entries", wrong)
	}
	observations.note("%d child runs: %d aborted on a concurrent map access, %d clean", runs, fatal, clean)
	assertNoHazard(t, observations, "")
}

func TestChildStoreIntoTheParentsCellSurvivesReuse(t *testing.T) {
	runReplay(t, replayCase{entry: "Run", rounds: 4, guestCheck: "Bad", source: `package main

import (
	"retainhost"
	"sync"
)

var bad int

func churn(n int) int {
	var wg sync.WaitGroup
	total := make([]int, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := 0
			for j := 0; j < 2000; j++ {
				xs := []int{-1, -2, -3, j}
				s += xs[3]
			}
			total[i] = s
		}(i)
	}
	wg.Wait()
	return total[0]
}

func Run(n int) int {
	var shared *[]int
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		var ys []int
		ys = append(ys, n*1000, n*1000+1, n*1000+2)
		shared = &ys
	}()
	wg.Wait()
	for k := 0; k < 4; k++ {
		churn(k)
	}
	if len(*shared) != 3 || (*shared)[0] != n*1000 {
		bad++
	}
	retainhost.Retain(shared)
	return len(*shared)
}

func Bad() int { return bad }
`})
}

func TestChildGoroutineStoresIntoCapturedVariablesSurviveReuse(t *testing.T) {
	route := newHandOffRoute(t, routeChurnGoroutines+`type pair struct {
	S string
	N int
}

func Fill(n int) int {
	xs := make([]string, 0, 2)
	ints := make([]int, 0, 2)
	var a any
	var arr any
	var v pair
	done := make(chan bool)
	go func() {
		local := make([]string, 2)
		local[0] = mk(n)
		local[1] = mk(n + 1)
		xs = local
		localInts := make([]int, 3)
		localInts[0] = n * 7
		localInts[2] = n * 11
		ints = localInts
		a = []string{mk(n + 2)}
		arr = [2]string{mk(n + 3), mk(n + 4)}
		v = pair{S: mk(n + 5), N: n}
		done <- true
	}()
	<-done
	churnInGoroutines(n)
	mhost.Report("slice", xs[0]+"|"+xs[1], mk(n)+"|"+mk(n+1))
	mhost.Report("any", a.([]string)[0], mk(n+2))
	pair2 := arr.([2]string)
	mhost.Report("array", pair2[0]+"|"+pair2[1], mk(n+3)+"|"+mk(n+4))
	mhost.Report("struct", v.S, mk(n+5))
	mhost.Report("ints", strconv.Itoa(ints[0])+"|"+strconv.Itoa(ints[2]), strconv.Itoa(n*7)+"|"+strconv.Itoa(n*11))
	return n
}
`)
	route.call("Fill", 4242)
	route.reuse()
	route.finish("shared-store")
}

const handedOffBlocksSource = `package main

type Block [256]int

var keptBlocks []*Block

func churn(depth int) int {
	var scratch [4096]byte
	scratch[depth] = byte(depth)
	if depth == 0 {
		return int(scratch[0])
	}
	return churn(depth-1) + int(scratch[depth])
}

func churnRounds(rounds int) {
	for r := 0; r < rounds; r++ {
		churn(16)
	}
}

func fillBlock(index int) *Block {
	block := new(Block)
	for j := range block {
		block[j] = index*1000 + j
	}
	return block
}

func blockErrors(block *Block, index int) int {
	bad := 0
	for j := range block {
		if block[j] != index*1000+j {
			bad++
		}
	}
	return bad
}

func sendBlocks(count, rounds int, out chan *Block, errors chan int) {
	churnRounds(rounds)
	own := fillBlock(-1)
	churnRounds(rounds)
	for i := 0; i < count; i++ {
		out <- fillBlock(i)
		churnRounds(rounds)
	}
	close(out)
	errors <- blockErrors(own, -1)
}

func keepGlobal(i int, block *Block) {
	keptBlocks[i] = block
}

func storeGlobals(count, rounds int, errors chan int) {
	churnRounds(rounds)
	own := fillBlock(-1)
	churnRounds(rounds)
	for i := 0; i < count; i++ {
		keepGlobal(i, fillBlock(i))
		churnRounds(rounds)
	}
	errors <- blockErrors(own, -1)
}

func Run(route string, count, rounds int) int {
	blocks := make([]*Block, count)
	errors := make(chan int, 1)
	var last *Block
	switch route {
	case "channel":
		out := make(chan *Block)
		go sendBlocks(count, rounds, out, errors)
		i := 0
		for block := range out {
			blocks[i] = block
			i++
		}
	case "captured variable":
		keep := func(i int, block *Block) {
			last = block
			blocks[i] = block
		}
		go func() {
			churnRounds(rounds)
	own := fillBlock(-1)
	churnRounds(rounds)
			for i := 0; i < count; i++ {
				keep(i, fillBlock(i))
				churnRounds(rounds)
			}
			errors <- blockErrors(own, -1)
		}()
	case "global":
		keptBlocks = make([]*Block, count)
		go storeGlobals(count, rounds, errors)
	}
	bad := <-errors
	if route == "global" {
		copy(blocks, keptBlocks)
	}
	if last != nil {
		bad += blockErrors(last, count-1)
	}
	for i, block := range blocks {
		bad += blockErrors(block, i)
	}
	return bad
}
`

func TestValuesAGoroutineHandsOffSurviveCollectionsThatReclaim(t *testing.T) {
	previous := engine.SetLicenceCountingForTest(true)
	t.Cleanup(func() { engine.SetLicenceCountingForTest(previous) })
	for _, route := range []string{"channel", "captured variable", "global"} {
		t.Run(route, func(t *testing.T) {
			window := openAuditWindow(t, 0)
			service := newHazardService(t)
			compiled := hazardCompile(t, service, handedOffBlocksSource)
			before := engine.LicenceDiagnostics().Reuse

			bad := hazardCall(t, service, compiled, "Run", route, 8, 64)

			after := engine.LicenceDiagnostics().Reuse
			require.EqualValues(t, 0, bad, "every block, handed off or kept by the goroutine, reads as it was written")
			observations := &hazardObservations{}
			settleReplayAudit(window, observations)
			assertNoHazard(t, observations, "")
			failedClosed := func(counts engine.LicenceCounts) (total int64) {
				for _, count := range counts.CompactionsFailedClosed {
					total += count
				}
				return total
			}
			require.Positive(t, after.Compactions-before.Compactions-(failedClosed(after)-failedClosed(before)),
				"test harness: the goroutine's collections must reclaim while the other goroutine holds its blocks")
		})
	}
}

func TestGoroutineCollectionsReclaimWhileTheGoroutinesHoldCellsBoxesAndZeroValues(t *testing.T) {
	previous := engine.SetLicenceCountingForTest(true)
	t.Cleanup(func() { engine.SetLicenceCountingForTest(previous) })
	for _, testCase := range []struct {
		name  string
		shape string
	}{
		{"goroutines that made a captured variable", "cell"},
		{"goroutines that hold the address of a slice variable", "slice"},
		{"goroutines that hold a struct with pointers", "struct"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			require.Positive(t, runGoroutineChurn(t, testCase.shape), "the goroutines' collections reclaim while their memory is shared")
		})
	}
}

const handedOffHoldersSource = `package main

type Block [256]int

type Box struct {
	block *Block
	label string
}

type Outer struct {
	inner *Box
}

var keptBoxes []*Box

func churn(depth int) int {
	var scratch [4096]byte
	scratch[depth] = byte(depth)
	if depth == 0 {
		return int(scratch[0])
	}
	return churn(depth-1) + int(scratch[depth])
}

func churnRounds(rounds int) {
	for r := 0; r < rounds; r++ {
		churn(16)
	}
}

func fillBlock(index int) *Block {
	block := new(Block)
	for j := range block {
		block[j] = index*1000 + j
	}
	return block
}

func blockErrors(block *Block, index int) int {
	if block == nil {
		return 1
	}
	bad := 0
	for j := range block {
		if block[j] != index*1000+j {
			bad++
		}
	}
	return bad
}

func boxErrors(box *Box, index int) int {
	if box == nil || box.label != "box" {
		return 1
	}
	return blockErrors(box.block, index)
}

func keepGlobal(i int, box *Box) {
	keptBoxes[i] = box
}

// makeBox returns a box that alone holds its block once this frame returns.
func makeBox(index int) *Box {
	return &Box{block: fillBlock(index), label: "box"}
}

// makeGetter returns a closure whose cell alone holds the block once this frame returns.
func makeGetter(index int) func() *Block {
	block := fillBlock(index)
	return func() *Block { return block }
}

// makeVariableGetter is makeGetter for a captured variable that a second closure assigns.
func makeVariableGetter(index int) func() *Block {
	var block *Block
	set := func(i int) { block = fillBlock(i) }
	set(index)
	return func() *Block { return block }
}

func sendBoxes(route string, count, rounds int, boxes chan *Box, outers chan *Outer, keep func(int, *Box), errors chan int) {
	own := makeBox(-1)
	churnRounds(8 * rounds)
	for i := 0; i < count; i++ {
		box := makeBox(i)
		churnRounds(rounds)
		switch route {
		case "channel":
			boxes <- box
		case "box in a box":
			outers <- &Outer{inner: box}
		case "captured variable":
			keep(i, box)
		case "global":
			keepGlobal(i, box)
		}
		churnRounds(rounds)
	}
	close(boxes)
	close(outers)
	errors <- boxErrors(own, -1)
}

func sendCells(route string, count, rounds int, getters chan func() *Block, errors chan int) {
	build := makeGetter
	if route == "cell holding a variable" {
		build = makeVariableGetter
	}
	ownGetter := build(-1)
	churnRounds(8 * rounds)
	for i := 0; i < count; i++ {
		get := build(i)
		churnRounds(rounds)
		getters <- get
		churnRounds(rounds)
	}
	close(getters)
	errors <- blockErrors(ownGetter(), -1)
}

func Run(route string, count, rounds int) int {
	errors := make(chan int, 1)
	bad := 0
	switch route {
	case "cell holding a value", "cell holding a variable":
		getters := make(chan func() *Block)
		go sendCells(route, count, rounds, getters, errors)
		i := 0
		for get := range getters {
			churnRounds(rounds)
			bad += blockErrors(get(), i)
			i++
		}
		return bad + <-errors
	}
	boxes := make([]*Box, count)
	keptBoxes = make([]*Box, count)
	sent := make(chan *Box)
	outers := make(chan *Outer, count)
	keep := func(i int, box *Box) { boxes[i] = box }
	go sendBoxes(route, count, rounds, sent, outers, keep, errors)
	i := 0
	for box := range sent {
		boxes[i] = box
		i++
	}
	for outer := range outers {
		boxes[i] = outer.inner
		i++
	}
	bad += <-errors
	if route == "global" {
		copy(boxes, keptBoxes)
	}
	churnRounds(rounds)
	for i, box := range boxes {
		bad += boxErrors(box, i)
	}
	return bad
}
`

func TestBoxesAndCellsAGoroutineHandsOffSurviveCollectionsThatReclaim(t *testing.T) {
	previous := engine.SetLicenceCountingForTest(true)
	t.Cleanup(func() { engine.SetLicenceCountingForTest(previous) })
	for _, route := range []string{"channel", "box in a box", "captured variable", "global", "cell holding a value", "cell holding a variable"} {
		t.Run(route, func(t *testing.T) {
			window := openAuditWindow(t, 0)
			service := newHazardService(t)
			compiled := hazardCompile(t, service, handedOffHoldersSource)
			before := engine.LicenceDiagnostics().Reuse

			bad := hazardCall(t, service, compiled, "Run", route, 8, 64)

			after := engine.LicenceDiagnostics().Reuse
			require.EqualValues(t, 0, bad, "every block, handed off or kept by the goroutine, reads as it was written")
			observations := &hazardObservations{}
			settleReplayAudit(window, observations)
			assertNoHazard(t, observations, "")
			failedClosed := func(counts engine.LicenceCounts) (total int64) {
				for _, count := range counts.CompactionsFailedClosed {
					total += count
				}
				return total
			}
			require.Positive(t, after.Compactions-before.Compactions-(failedClosed(after)-failedClosed(before)),
				"test harness: the goroutine's collections must reclaim while it holds the box or cell")
		})
	}
}
