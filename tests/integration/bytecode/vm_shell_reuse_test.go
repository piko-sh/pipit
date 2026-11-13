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
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/symtab"
)

const vmShellBoundSource = `package main

import "host"

type label struct {
	name string
	n    int
}

func (l label) String() string {
	parts := []string{}
	add := func(s string) { parts = append(parts, s) }
	for i := 0; i < l.n%3+1; i++ {
		add(l.name)
	}
	out := ""
	for _, part := range parts {
		out += part
	}
	return out
}

type risky struct{ n int }

func (r risky) String() (s string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			s = "recovered-" + host.Itoa(r.n)
		}
	}()
	if r.n%2 == 0 {
		panic("boom")
	}
	return "plain-" + host.Itoa(r.n)
}

type keeper struct{ inner label }

func (k keeper) String() string {
	host.Keep(k.inner)
	return "kept"
}

type bomb struct{ n int }

func (b bomb) String() string {
	panic("uncaught " + host.Itoa(b.n))
}

type byN []int

func (b byN) Len() int           { return len(b) }
func (b byN) Less(i, j int) bool { return b[i] < b[j] }
func (b byN) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }

func run(round int) int {
	total := len(host.Stringify(label{name: "ab", n: round}))
	total += len(host.Stringify(risky{n: round}))
	xs := make(byN, 32)
	for i := range xs {
		xs[i] = (i*7919 + round) % 32
	}
	host.SortIt(xs)
	for i := 1; i < len(xs); i++ {
		if xs[i-1] > xs[i] {
			return -1
		}
	}
	if round%10 == 0 {
		total += len(host.Stringify(keeper{inner: label{name: "k", n: round}}))
	}
	return total
}

func explode(round int) int {
	return len(host.Stringify(bomb{n: round}))
}
`

type vmShellKept struct {
	stringer fmt.Stringer
	round    int
}

const vmShellCallbackSource = `package main

import "host"

var prefix = "row-"

func render(i int) string {
	cells := []string{}
	push := func(s string) { cells = append(cells, s) }
	for k := 0; k < i%4+1; k++ {
		push(prefix)
	}
	out := ""
	for _, cell := range cells {
		out += cell
	}
	if i%5 == 0 {
		func() {
			defer func() { _ = recover() }()
			panic("swallowed")
		}()
	}
	return out + host.Itoa(i)
}

func pair(i int) (string, int) {
	return host.Itoa(i), i * 2
}

func init() {
	host.Register(render)
	host.RegisterPair(pair)
}

func run(n int) int {
	return host.Each(n, render)
}
`

const vmShellEntrySource = `package main

import "host"

var calls int
var greeting = "hello " + host.Itoa(len(host.Itoa(42)))

func one(i int) int {
	calls++
	return i*2 + len(greeting)
}

func two(i int) (int, string) {
	return i + 1, "s" + host.Itoa(i)
}

func spawn(i int) int {
	done := make(chan int)
	go func() { done <- i + 1 }()
	return <-done
}

func goPanic(i int) int {
	done := make(chan bool)
	go func() {
		defer close(done)
		panic("goroutine boom")
	}()
	<-done
	return i
}

func spin(i int) int {
	for {
		i++
	}
}

func hold(i int) int {
	host.Hold(func() int { return i * 3 })
	return i
}
`

func vmShellBoundWant(round int) int {
	total := 2 * (round%3 + 1)
	if round%2 == 0 {
		total += len("recovered-" + strconv.Itoa(round))
	} else {
		total += len("plain-" + strconv.Itoa(round))
	}
	if round%10 == 0 {
		total += len("kept")
	}
	return total
}

func runVMShellBoundWorkload(t *testing.T, rounds int) {
	t.Helper()
	var keptMu sync.Mutex
	var kept []vmShellKept
	currentRound := 0
	symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{"host": {
		"Stringify": reflect.ValueOf(func(s fmt.Stringer) string { return s.String() }),
		"SortIt":    reflect.ValueOf(func(x sort.Interface) { sort.Sort(x) }),
		"Itoa":      reflect.ValueOf(strconv.Itoa),
		"Keep": reflect.ValueOf(func(s fmt.Stringer) {
			keptMu.Lock()
			kept = append(kept, vmShellKept{stringer: s, round: currentRound})
			keptMu.Unlock()
		}),
	}})
	service := app.NewService()
	service.UseSymbols(symbols)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": vmShellBoundSource})
	require.NoError(t, err)

	checkKept := func() {
		keptMu.Lock()
		snapshot := append([]vmShellKept(nil), kept...)
		keptMu.Unlock()
		for _, entry := range snapshot {
			require.Equal(t, strings.Repeat("k", entry.round%3+1), entry.stringer.String(),
				"a stringer kept from round %d", entry.round)
		}
	}

	for round := range rounds {
		currentRound = round
		if round%7 == 3 {
			_, explodeErr := service.CallFunction(t.Context(), compiled, "explode", round)
			require.Error(t, explodeErr, "round %d: an uncaught panic in a bound method fails the execution", round)
			continue
		}
		result, runErr := service.CallFunction(t.Context(), compiled, "run", round)
		require.NoError(t, runErr, "round %d", round)
		require.EqualValues(t, vmShellBoundWant(round), result, "round %d", round)
		if round%25 == 24 {
			checkKept()
		}
	}

	var waitGroup sync.WaitGroup
	for range 4 {
		waitGroup.Go(checkKept)
	}
	for round := rounds; round < rounds+40; round++ {
		if round%10 == 0 {
			continue
		}
		result, runErr := service.CallFunction(t.Context(), compiled, "run", round)
		require.NoError(t, runErr, "round %d", round)
		require.EqualValues(t, vmShellBoundWant(round), result, "round %d", round)
	}
	waitGroup.Wait()
	require.NotEmpty(t, kept)
}

func vmShellRenderWant(i int) string {
	return strings.Repeat("row-", i%4+1) + strconv.Itoa(i)
}

func runVMShellCallbackWorkload(t *testing.T, calls int) {
	t.Helper()
	var render func(int) string
	var pair func(int) (string, int)
	symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{"host": {
		"Register":     reflect.ValueOf(func(fn func(int) string) { render = fn }),
		"RegisterPair": reflect.ValueOf(func(fn func(int) (string, int)) { pair = fn }),
		"Itoa":         reflect.ValueOf(strconv.Itoa),
		"Each": reflect.ValueOf(func(count int, each func(int) string) int {
			total := 0
			for i := range count {
				total += len(each(i))
			}
			return total
		}),
	}})
	service := app.NewService()
	service.UseSymbols(symbols)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": vmShellCallbackSource})
	require.NoError(t, err)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	require.NotNil(t, render)
	require.NotNil(t, pair)

	var waitGroup sync.WaitGroup
	failures := make(chan string, 16)
	for worker := range 4 {
		waitGroup.Go(func() {
			for call := range calls {
				i := worker*calls + call
				if got := render(i); got != vmShellRenderWant(i) {
					failures <- fmt.Sprintf("render(%d) = %q", i, got)
					return
				}
				if text, doubled := pair(i); text != strconv.Itoa(i) || doubled != 2*i {
					failures <- fmt.Sprintf("pair(%d) = %q, %d", i, text, doubled)
					return
				}
			}
		})
	}
	for round := range 8 {
		want := 0
		for i := range round * 5 {
			want += len(vmShellRenderWant(i))
		}
		result, runErr := service.CallFunction(t.Context(), compiled, "run", round*5)
		require.NoError(t, runErr)
		require.EqualValues(t, want, result)
	}
	waitGroup.Wait()
	close(failures)
	var observed []string
	for failure := range failures {
		observed = append(observed, failure)
	}
	require.Empty(t, observed, "every concurrent run renders its own values")
}

func requireVMShellsSafe(t *testing.T, counts engine.VMShellCounts) {
	t.Helper()
	recorded, reachable := engine.AuditVMShells()
	t.Logf("shells: parked %d, reused %d, refused pinned %d, refused state %d; audit recorded %d, reachable %d",
		counts.Parked, counts.Reused, counts.RefusedPinned, counts.RefusedState, recorded, reachable)
	require.Positive(t, counts.Parked, "the workload parks VM shells")
	if recorded > 0 {
		require.Zero(t, reachable, "a recycled VM shell is still reachable: a pinning site is missing")
		return
	}
	require.Positive(t, counts.Reused, "NewVM reuses parked shells")
}

func TestVMShellBoundMethodsKeepRunsApart(t *testing.T) {
	read := engine.InstallVMShellProbe()
	runVMShellBoundWorkload(t, 200)
	counts := read()
	require.Positive(t, counts.RefusedPinned, "a bound method that handed the host an adapter is never recycled")
	requireVMShellsSafe(t, counts)
}

func TestVMShellCallbacksKeepRunsApart(t *testing.T) {
	read := engine.InstallVMShellProbe()
	runVMShellCallbackWorkload(t, 300)
	requireVMShellsSafe(t, read())
}

func TestVMShellEntryExecutionsKeepRunsApart(t *testing.T) {
	var heldMu sync.Mutex
	held := map[int]func() int{}
	holdRound := 0
	symbols := symtab.NewSymbolRegistry(symtab.SymbolExports{"host": {
		"Itoa": reflect.ValueOf(strconv.Itoa),
		"Hold": reflect.ValueOf(func(fn func() int) {
			heldMu.Lock()
			held[holdRound] = fn
			heldMu.Unlock()
		}),
	}})
	service := app.NewService()
	service.UseSymbols(symbols)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": vmShellEntrySource})
	require.NoError(t, err)

	read := engine.InstallVMShellProbe()
	greetingLength := len("hello 2")
	for round := range 120 {
		switch round % 6 {
		case 0:
			result, runErr := service.ExecuteEntrypoint(t.Context(), compiled, "one")
			require.Error(t, runErr, "one takes an argument")
			require.Nil(t, result)
			result, runErr = service.CallFunction(t.Context(), compiled, "one", round)
			require.NoError(t, runErr)
			require.EqualValues(t, round*2+greetingLength, result, "round %d", round)
		case 1:
			result, runErr := service.CallFunction(t.Context(), compiled, "two", round)
			require.NoError(t, runErr)
			require.EqualValues(t, round+1, result, "round %d", round)
		case 2:
			result, runErr := service.CallFunction(t.Context(), compiled, "spawn", round)
			require.NoError(t, runErr)
			require.EqualValues(t, round+1, result, "round %d", round)
		case 3:
			_, runErr := service.CallFunction(t.Context(), compiled, "goPanic", round)
			require.Error(t, runErr, "round %d: a goroutine panic fails the execution", round)
		case 4:
			if round%12 == 4 {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Millisecond)
				_, runErr := service.CallFunction(ctx, compiled, "spin", round)
				cancel()
				require.Error(t, runErr, "round %d: a cancelled spin fails", round)
				continue
			}
			result, runErr := service.CallFunction(t.Context(), compiled, "one", round)
			require.NoError(t, runErr)
			require.EqualValues(t, round*2+greetingLength, result, "round %d", round)
		case 5:
			holdRound = round
			result, runErr := service.CallFunction(t.Context(), compiled, "hold", round)
			require.NoError(t, runErr)
			require.EqualValues(t, round, result)
		}
	}
	heldMu.Lock()
	defer heldMu.Unlock()
	require.NotEmpty(t, held)
	for round, fn := range held {
		require.Equal(t, round*3, fn(), "a closure held from round %d", round)
	}
	counts := read()
	require.Positive(t, counts.RefusedPinned, "an entry VM that handed out a closure is never recycled")
	require.Positive(t, counts.RefusedState, "an entry VM whose context was cancelled is never recycled")
	requireVMShellsSafe(t, counts)
}
