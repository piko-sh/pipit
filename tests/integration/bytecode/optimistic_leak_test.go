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

const optimisticLeakSource = `package main

import (
	"opthost"
	"strconv"
)

type pair struct {
	A int
	B int
}

var keptStrings []string
var keptPair *pair

func label(i int) string { return "opt-" + strconv.Itoa(i) + "-" + strconv.Itoa(i*31%97) }

// Heap-flagged make: the compiler places out on the heap because it is returned; optimistic
// placement puts it in the arena, so the stores below are own-storage stores with no copy.
func buildStrings(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = label(i)
	}
	return out
}

func buildInts(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i*7 + 1
	}
	return out
}

func ReturnStrings(n int) []string { return buildStrings(n) }
func ReturnInts(n int) []int       { return buildInts(n) }
func PassStrings(n int) int        { opthost.Keep(buildStrings(n)); return n }
func StoreStrings(m map[string]any, n int) int {
	m["v"] = buildStrings(n)
	return n
}
func GlobalStrings(n int) int { keptStrings = buildStrings(n); return n }
func ReadGlobalStrings() []string { return keptStrings }
func SendStrings(c chan []string, n int) int { c <- buildStrings(n); return n }
func FieldStrings(h *opthost.Holder, n int) int {
	h.Names = buildStrings(n)
	return n
}

// A heap-flagged make through a closure: the captured slice is placed by the rule and the
// closure crosses to the host (a function value revokes the epoch); a later call from the host
// stores a fresh optimistic slice, made in the callback's arena, into the captured cell.
func CaptureStrings(n int) int {
	s := make([]string, n)
	for i := range s {
		s[i] = label(i + 3)
	}
	opthost.Register(func(m int) []string {
		if m > 0 {
			s = make([]string, m)
			for i := range s {
				s[i] = label(i*5 + 2)
			}
		}
		return s
	})
	return n
}

// The same through a direct (not heap-promoted) capture: the closure only reads s, and the
// host reads the captured slice after the arena is reused.
func CaptureStringsRead(n int) int {
	s := make([]string, n)
	for i := range s {
		s[i] = label(i*3 + 1)
	}
	opthost.Register(func(m int) []string { return s })
	return n
}

// Pointer-free pointee: new(T) and &v of a pointer-free value the escape pass does not grant.
func NewPair(n int) *pair {
	p := new(pair)
	p.A = n
	p.B = n * 2
	return p
}

func AddrPair(n int) *pair {
	v := pair{A: n + 1, B: n + 2}
	return &v
}

func GlobalPair(n int) int { keptPair = NewPair(n); return n }
func ReadGlobalPair() *pair { return keptPair }

// Clone budget: once the epoch is revoked (here by a native function value, which the walk
// cannot see into, reaching the host), a string handed off is kept in place rather than
// copied.
func KeptAfterOpaqueFunction(n int) []string {
	opthost.Keep(strconv.Itoa)
	out := []string{}
	for i := 0; i < n; i++ {
		out = append(out, label(i)+label(i+1))
	}
	return out
}

// No global sweep: the first goroutine spawn no longer sweeps the globals for arena strings.
func GlobalThenSpawn(n int) int {
	keptStrings = buildStrings(n)
	done := make(chan bool)
	go func() { done <- true }()
	<-done
	return n
}

func KeptStringAfterOpaqueFunction(n int) string {
	opthost.Keep(strconv.Itoa)
	return label(n) + label(n+1)
}

// The heap-flagged make's veto: both slices are captured, so heap-flagged; the closure runs as
// a callback of the host and copies the parent's strings into the parent's slice, cloning each
// as a foreign string. After a run in which that followed a heap-flagged make placed in the
// arena, the program's makes stay on the heap.
func CallbackCopies(n int) []string {
	local := make([]string, n)
	for i := range local {
		local[i] = label(i)
	}
	out := make([]string, n)
	opthost.Each(n, func(i int) { out[i] = local[i] })
	return out
}
`

type optimisticHolder struct {
	Names []string
}

var optimisticKept any

var optimisticRegistered func(int) []string

type optimisticFixture struct {
	service  *app.Service
	compiled *program.CompiledFileSet
}

func optimisticLabel(i int) string {
	return "opt-" + strconv.Itoa(i) + "-" + strconv.Itoa(i*31%97)
}

func optimisticStrings(count int) []string {
	out := make([]string, count)
	for i := range out {
		out[i] = optimisticLabel(i)
	}
	return out
}

func newOptimisticFixture(t *testing.T) *optimisticFixture {
	t.Helper()
	previousCounting := engine.SetLicenceCountingForTest(true)
	t.Cleanup(func() { engine.SetLicenceCountingForTest(previousCounting) })
	exports := maps.Clone(stdlib.Exports())
	exports["opthost"] = map[string]reflect.Value{
		"Keep":     reflect.ValueOf(func(v any) { optimisticKept = v }),
		"Register": reflect.ValueOf(func(f func(int) []string) { optimisticRegistered = f }),
		"Holder":   reflect.ValueOf((*optimisticHolder)(nil)),
		"Each": reflect.ValueOf(func(n int, f func(int)) {
			for i := range n {
				f(i)
			}
		}),
	}
	service := app.NewService(hazardServiceOptions()...)
	service.UseSymbols(symtab.NewSymbolRegistry(exports))
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": optimisticLeakSource})
	require.NoError(t, err)
	return &optimisticFixture{service: service, compiled: compiled}
}

func (f *optimisticFixture) call(t *testing.T, name string, arguments ...any) any {
	t.Helper()
	result, err := f.service.CallFunction(t.Context(), f.compiled, name, arguments...)
	require.NoError(t, err, "calling %s", name)
	return result
}

func (f *optimisticFixture) reuse(t *testing.T) {
	t.Helper()
	clobberArenas(t, f.service, 24)
	for round := range 8 {
		f.call(t, "ReturnInts", 512+round)
		f.call(t, "ReturnStrings", 64+round)
	}
}

func optimisticCheck(t *testing.T, rule engine.OptimisticRule, give func(f *optimisticFixture) func() string, want string) {
	t.Helper()
	engine.ResetOptimisticStats()
	fixture := newOptimisticFixture(t)
	window := openAuditWindow(t, 0)
	read := give(fixture)
	fired := engine.LicenceDiagnostics().Optimistic[rule]
	require.Positive(t, fired, "the rule under test did not fire, so the case tests nothing")
	require.Equal(t, want, read(), "before reuse")
	fixture.reuse(t)
	observations := &hazardObservations{}
	observations.compareString("after reuse", want, read())
	window.settle(observations)
	require.Empty(t, observations.fired, "an optimistic placement leaked: %s", strings.Join(observations.details, "; "))
}

func joinStrings(value any) string {
	labels, ok := value.([]string)
	if !ok {
		return fmt.Sprintf("not a string slice: %T", value)
	}
	return strings.Join(labels, ",")
}

func TestOptimisticPlacementSurvivesReuseByEveryRoute(t *testing.T) {
	const count = 300
	wantStrings := strings.Join(optimisticStrings(count), ",")
	t.Run("host-return", func(t *testing.T) {
		optimisticCheck(t, engine.OptimisticMake, func(f *optimisticFixture) func() string {
			held := f.call(t, "ReturnStrings", count)
			return func() string { return joinStrings(held) }
		}, wantStrings)
	})
	t.Run("native-argument", func(t *testing.T) {
		optimisticCheck(t, engine.OptimisticMake, func(f *optimisticFixture) func() string {
			f.call(t, "PassStrings", count)
			held := optimisticKept
			return func() string { return joinStrings(held) }
		}, wantStrings)
	})
	t.Run("container-store", func(t *testing.T) {
		optimisticCheck(t, engine.OptimisticMake, func(f *optimisticFixture) func() string {
			hostMap := map[string]any{}
			f.call(t, "StoreStrings", hostMap, count)
			return func() string { return joinStrings(hostMap["v"]) }
		}, wantStrings)
	})
	t.Run("global-store-then-spawn", func(t *testing.T) {
		optimisticCheck(t, engine.OptimisticGlobalSweepSkipped, func(f *optimisticFixture) func() string {
			f.call(t, "GlobalThenSpawn", count)
			return func() string { return joinStrings(f.call(t, "ReadGlobalStrings")) }
		}, wantStrings)
	})
	t.Run("global-store", func(t *testing.T) {
		optimisticCheck(t, engine.OptimisticMake, func(f *optimisticFixture) func() string {
			f.call(t, "GlobalStrings", count)
			return func() string { return joinStrings(f.call(t, "ReadGlobalStrings")) }
		}, wantStrings)
	})
	t.Run("channel-send", func(t *testing.T) {
		optimisticCheck(t, engine.OptimisticMake, func(f *optimisticFixture) func() string {
			channel := make(chan []string, 1)
			f.call(t, "SendStrings", channel, count)
			held := <-channel
			return func() string { return strings.Join(held, ",") }
		}, wantStrings)
	})
	t.Run("field-store", func(t *testing.T) {
		optimisticCheck(t, engine.OptimisticMake, func(f *optimisticFixture) func() string {
			holder := &optimisticHolder{}
			f.call(t, "FieldStrings", holder, count)
			return func() string { return strings.Join(holder.Names, ",") }
		}, wantStrings)
	})
}

func TestOptimisticCapturedSliceSurvivesCallbackStores(t *testing.T) {
	const count, wantCount = 300, 260
	want := make([]string, wantCount)
	for i := range want {
		want[i] = optimisticLabel(i*5 + 2)
	}
	t.Run("callback-store", func(t *testing.T) {
		optimisticCheck(t, engine.OptimisticMake, func(f *optimisticFixture) func() string {
			f.call(t, "CaptureStrings", count)
			registered := optimisticRegistered
			registered(wantCount)
			return func() string { return strings.Join(registered(0), ",") }
		}, strings.Join(want, ","))
	})
	t.Run("capture-read", func(t *testing.T) {
		read := make([]string, count)
		for i := range read {
			read[i] = optimisticLabel(i*3 + 1)
		}
		optimisticCheck(t, engine.OptimisticMake, func(f *optimisticFixture) func() string {
			f.call(t, "CaptureStringsRead", count)
			registered := optimisticRegistered
			return func() string { return strings.Join(registered(0), ",") }
		}, strings.Join(read, ","))
	})
}

func TestOptimisticPointeesSurviveReuse(t *testing.T) {
	render := func(value any) string {
		pointer := reflect.ValueOf(value)
		if !pointer.IsValid() || pointer.IsNil() {
			return "nil"
		}
		return fmt.Sprintf("%d/%d", pointer.Elem().Field(0).Int(), pointer.Elem().Field(1).Int())
	}
	t.Run("new-host-return", func(t *testing.T) {
		optimisticCheck(t, engine.OptimisticPointee, func(f *optimisticFixture) func() string {
			held := f.call(t, "NewPair", 41)
			return func() string { return render(held) }
		}, "41/82")
	})
	t.Run("address-host-return", func(t *testing.T) {
		optimisticCheck(t, engine.OptimisticPointee, func(f *optimisticFixture) func() string {
			held := f.call(t, "AddrPair", 41)
			return func() string { return render(held) }
		}, "42/43")
	})
	t.Run("new-global-store", func(t *testing.T) {
		optimisticCheck(t, engine.OptimisticPointee, func(f *optimisticFixture) func() string {
			f.call(t, "GlobalPair", 51)
			return func() string { return render(f.call(t, "ReadGlobalPair")) }
		}, "51/102")
	})
}

func TestOptimisticKeptStringsSurviveReuse(t *testing.T) {
	t.Run("strings-in-a-slice", func(t *testing.T) {
		const count = 400
		want := make([]string, count)
		for i := range want {
			want[i] = optimisticLabel(i) + optimisticLabel(i+1)
		}
		optimisticCheck(t, engine.OptimisticKeptString, func(f *optimisticFixture) func() string {
			held := f.call(t, "KeptAfterOpaqueFunction", count)
			return func() string { return joinStrings(held) }
		}, strings.Join(want, ","))
	})
	t.Run("returned-string", func(t *testing.T) {
		optimisticCheck(t, engine.OptimisticKeptString, func(f *optimisticFixture) func() string {
			held := requireAs[string](t, f.call(t, "KeptStringAfterOpaqueFunction", 77))
			return func() string { return held }
		}, optimisticLabel(77)+optimisticLabel(78))
	})
}

func TestOptimisticMakeVetoedAfterCallbackClones(t *testing.T) {
	const count, runs = 300, 12
	want := strings.Join(optimisticStrings(count), ",")
	engine.ResetOptimisticStats()
	fixture := newOptimisticFixture(t)
	window := openAuditWindow(t, 0)
	placed := make([]int64, runs)
	held := make([]any, 0, runs)
	for run := range runs {
		before := engine.LicenceDiagnostics().Optimistic[engine.OptimisticMake]
		result := fixture.call(t, "CallbackCopies", count)
		require.Equal(t, want, joinStrings(result), "run %d", run)
		held = append(held, result)
		placed[run] = engine.LicenceDiagnostics().Optimistic[engine.OptimisticMake] - before
	}

	require.Positive(t, placed[0], "the heap-flagged make placed nothing in the first run, so the case tests nothing")
	require.Equal(t, make([]int64, runs-1), placed[1:], "the heap-flagged make kept placing after its callbacks cloned the parent's strings")
	fixture.reuse(t)
	observations := &hazardObservations{}
	for run, result := range held {
		observations.compareString(fmt.Sprintf("run %d after reuse", run), want, joinStrings(result))
	}
	window.settle(observations)
	require.Empty(t, observations.fired, "a vetoed or placed slice leaked: %s", strings.Join(observations.details, "; "))
}
