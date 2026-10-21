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
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestErrorsNewTextSurvivesArenaReuse(t *testing.T) {
	window := openAuditWindow(t, 0)
	service := newHazardService(t)
	compiled := hazardCompile(t, service, `package main

import (
	"errors"
	"strconv"
)

func Entry(id int) error {
	return errors.New("bad id " + strconv.Itoa(id) + " returned through errors.New")
}
`)
	result := hazardCall(t, service, compiled, "Entry", 4242)
	err, ok := result.(error)
	require.True(t, ok, "test harness: Entry returned %T", result)
	const want = "bad id 4242 returned through errors.New"
	observations := &hazardObservations{}
	observations.compareString("err.Error() straight after the run", want, err.Error())

	for id := range 32 {
		hazardCall(t, service, compiled, "Entry", 90000+id)
	}
	clobberArenas(t, service, 8)
	observations.compareString("err.Error() after 32 Entry runs and 8 clobber runs", want, err.Error())
	window.settle(observations)
	runtime.KeepAlive(err)
	assertNoHazard(t, observations, "")
}

func TestCopyIntoAGlobalSliceSurvivesArenaReuse(t *testing.T) {
	window := openAuditWindow(t, 0)
	service := newHazardService(t)
	compiled := hazardCompile(t, service, `package main

import "strconv"

var names = make([]string, 4)

func Fill(n int) int {
	local := make([]string, 4)
	for i := range local {
		local[i] = "k" + strconv.Itoa(n+i) + "-copied-into-a-global"
	}
	copy(names, local)
	return len(names)
}

func Names() []string { return names }
`)
	want := make([]string, 4)
	for i := range want {
		want[i] = "k" + itoa(100+i) + "-copied-into-a-global"
	}
	require.NoError(t, service.ExecuteInits(t.Context(), compiled), "test harness: running the package initialisers")
	hazardCall(t, service, compiled, "Fill", 100)
	held, ok := hazardCall(t, service, compiled, "Names").([]string)
	require.True(t, ok, "test harness: Names did not return []string")
	observations := &hazardObservations{}
	for i := range want {
		observations.compareString("Names()["+itoa(i)+"] after Fill", want[i], held[i])
	}

	clobberArenas(t, service, 8)
	again, ok := hazardCall(t, service, compiled, "Names").([]string)
	require.True(t, ok, "test harness: Names did not return []string")
	for i := range want {
		observations.compareString("host-held Names()["+itoa(i)+"] after 8 clobber runs", want[i], held[i])
		observations.compareString("global names["+itoa(i)+"] read by a later run", want[i], again[i])
	}
	window.settle(observations)
	runtime.KeepAlive(held)
	assertNoHazard(t, observations, "")
}

func TestSessionGlobalPointerToASliceLocalSurvivesReuse(t *testing.T) {
	window := openAuditWindow(t, 0)
	session := newHazardService(t).NewSession()
	submit := func(code string) any {
		t.Helper()
		result, err := session.Submit(t.Context(), code)
		require.NoError(t, err, "test harness: submitting %q", code)
		return result
	}
	submit(`var keep *[]int`)
	submit(`func publish() { s := []int{11, 22, 33}; keep = &s }`)
	submit(`publish()`)
	held, ok := submit(`keep`).(*[]int)
	require.True(t, ok, "test harness: keep is not *[]int")
	observations := &hazardObservations{}
	describe := func(p *[]int) string {
		if p == nil {
			return "<nil>"
		}
		return fmtInts(*p)
	}
	const want = "[11 22 33]"
	observations.compareString("host-held *keep straight after publish", want, describe(held))

	submit(`func churn(n int) int { t := 0; for i := 0; i < n; i++ { xs := []string{"aaaaaaaa", "bbbbbbbb", "cccccccc", "dddddddd"}; p := &xs; ys := []float64{1.5, 2.5}; q := &ys; t += len(*p) + len(*q) }; return t }`)
	for range 8 {
		submit(`churn(4096)`)
	}
	observations.compareString("host-held *keep after 8 churn submissions", want, describe(held))
	got, err := session.Submit(t.Context(), `len(*keep) * 1000 + (*keep)[0] + (*keep)[1] + (*keep)[2]`)
	switch {
	case err != nil:
		observations.fire("guest-value-changed", "reading *keep in a later submission failed: %v", err)
	case got != any(3066):
		observations.fire("guest-value-changed", "len(*keep)*1000 + sum read %v, want 3066", got)
	default:
		observations.note("guest read of *keep unchanged (3066)")
	}
	window.settle(observations)
	runtime.KeepAlive(held)
	assertNoHazard(t, observations, "")
}

func TestHostArgumentStoresPastTheWalkBoundsSurviveReuse(t *testing.T) {
	window := openAuditWindow(t, 0)
	service := newHazardService(t)
	compiled := hazardCompile(t, service, `package main

import "strconv"

func fill(dst []string, n int) {
	local := make([]string, len(dst))
	for i := range local {
		local[i] = "walk-" + strconv.Itoa(n+i) + "-host-argument-string"
	}
	copy(dst, local)
}

func Shallow(dst *[]string, n int) int { fill(*dst, n); return len(*dst) }

func Deep(dst *******[]string, n int) int { fill(*******dst, n); return len(*******dst) }

func Wide(dst []string, n int) int { fill(dst[len(dst)-4:], n); return len(dst) }
`)
	want := func(seed int) []string {
		out := make([]string, 4)
		for i := range out {
			out[i] = "walk-" + itoa(seed+i) + "-host-argument-string"
		}
		return out
	}
	observations := &hazardObservations{}

	shallow := make([]string, 4)
	hazardCall(t, service, compiled, "Shallow", &shallow, 100)

	deepSlice := make([]string, 4)
	p1 := &deepSlice
	p2 := &p1
	p3 := &p2
	p4 := &p3
	p5 := &p4
	p6 := &p5
	p7 := &p6
	hazardCall(t, service, compiled, "Deep", p7, 200)

	wide := make([]string, 70000)
	hazardCall(t, service, compiled, "Wide", wide, 300)
	wideTail := wide[len(wide)-4:]

	clobberArenas(t, service, 8)
	shallowWant, deepWant, wideWant := want(100), want(200), want(300)
	controlChanged := false
	for i := range 4 {
		if shallow[i] != shallowWant[i] {
			controlChanged = true
		}
		observations.compareString("control (1 pointer level, 4 leaves)["+itoa(i)+"]", shallowWant[i], shallow[i])
		observations.compareString("deep (7 pointer levels)["+itoa(i)+"]", deepWant[i], deepSlice[i])
		observations.compareString("wide (leaves 69996..69999 of 70000)["+itoa(i)+"]", wideWant[i], wideTail[i])
	}
	if controlChanged {
		observations.note("the in-bounds control changed too, so the walk bound is not the only cause")
	}
	window.settle(observations)
	runtime.KeepAlive(p7)
	runtime.KeepAlive(wide)
	assertNoHazard(t, observations, "")
}

func nativeGoroutineSliceArgument() int {
	values := []int{1, 2, 3}
	done := make(chan bool)
	go func(x []int) {
		x[0] = 100
		done <- true
	}(values)
	<-done
	return values[0]
}

func nativeGlobalSliceAlias() int {
	type pair struct{ A, B int }
	var global []pair
	xs := make([]pair, 4)
	global = xs
	xs[0].A = 7
	return global[0].A
}

func TestCopySemanticsMatchNativeGo(t *testing.T) {
	service := newHazardService(t)
	compiled := hazardCompile(t, service, `package main

type pair struct {
	A int
	B int
}

var global []pair

func Look(cfg map[string]map[string]int) int { return len(cfg) }

func GoroutineSlice() int {
	s := []int{1, 2, 3}
	done := make(chan bool)
	go func(x []int) {
		x[0] = 100
		done <- true
	}(s)
	<-done
	return s[0]
}

func GlobalAlias() int {
	xs := make([]pair, 4)
	global = xs
	xs[0].A = 7
	return global[0].A
}
`)
	observations := &hazardObservations{}

	inner := map[string]int{"x": 1}
	config := map[string]map[string]int{"a": inner}
	hazardCall(t, service, compiled, "Look", config)
	sameIdentity := reflect.ValueOf(config["a"]).UnsafePointer() == reflect.ValueOf(inner).UnsafePointer()
	inner["y"] = 2
	_, visible := config["a"]["y"]
	if !sameIdentity || !visible {
		observations.fire("native-go-divergence", "host inner map: identity kept=%t, host write through inner visible via cfg[\"a\"]=%t; native Go keeps both true", sameIdentity, visible)
	} else {
		observations.note("host inner map identity kept")
	}

	nativeGoroutine := nativeGoroutineSliceArgument()
	gotGoroutine := hazardCall(t, service, compiled, "GoroutineSlice")
	if gotGoroutine != any(nativeGoroutine) {
		observations.fire("native-go-divergence", "goroutine slice argument: pipit s[0]=%v, native Go s[0]=%d", gotGoroutine, nativeGoroutine)
	} else {
		observations.note("goroutine slice argument shares the backing (s[0]=%d as native Go)", nativeGoroutine)
	}

	nativeGlobal := nativeGlobalSliceAlias()
	gotGlobal := hazardCall(t, service, compiled, "GlobalAlias")
	if gotGlobal != any(nativeGlobal) {
		observations.fire("native-go-divergence", "slice stored to a global: pipit global[0].A=%v, native Go %d", gotGlobal, nativeGlobal)
	} else {
		observations.note("global slice store aliases the local (global[0].A=%d as native Go)", nativeGlobal)
	}
	assertNoHazard(t, observations, "")
}

func TestIdentityBearingKindsMatchNativeGo(t *testing.T) {
	window := openAuditWindow(t, 0)
	service := newHazardService(t)
	compiled := hazardCompile(t, service, copyRuleSource)
	observations := &hazardObservations{}
	for _, outcome := range runCopyRuleProbes(copyRuleCall(t, service, compiled)) {
		if !reflect.DeepEqual(outcome.got, outcome.want) {
			observations.fire("native-go-divergence", "%s: pipit %v, native Go %v", outcome.name, outcome.got, outcome.want)
		}
	}
	if len(observations.fired) == 0 {
		observations.note("every copy-rule probe matches native Go")
	}

	kept := hazardCompile(t, service, `package main

type pair struct {
	A int
	B int
}

func Kept(n int) []pair {
	var out []pair
	func() {
		t := append(out, pair{A: n, B: n * 2})
		out = t
		t[0].B = n * 3
	}()
	return out
}
`)
	held := reflect.ValueOf(hazardCall(t, service, kept, "Kept", 41))
	describe := func() string {
		if held.Len() == 0 {
			return "<empty>"
		}
		element := held.Index(0)
		return itoa(int(element.FieldByName("A").Int())) + "/" + itoa(int(element.FieldByName("B").Int()))
	}
	observations.compareString("slice kept by a captured variable straight after the run", "41/123", describe())
	for n := range 32 {
		hazardCall(t, service, kept, "Kept", 9000+n)
	}
	clobberArenas(t, service, 8)
	observations.compareString("slice kept by a captured variable after 32 runs and 8 clobber runs", "41/123", describe())
	window.settle(observations)
	runtime.KeepAlive(held)
	assertNoHazard(t, observations, "")
}

func TestPublishedAddressOfAnArenaValueSurvivesReuse(t *testing.T) {
	window := openAuditWindow(t, 0)
	service := newHazardService(t)
	compiled := hazardCompile(t, service, `package main

type P struct {
	A int
	B int
	S [4]int
}

var keep *P

func Local(n int) *P {
	v := P{A: n, B: n * 2}
	v.S[3] = n * 3
	p := &v
	return p
}

func Element(n int) *P {
	xs := make([]P, 4)
	xs[2] = P{A: n, B: n * 2}
	xs[2].S[3] = n * 3
	v := xs[2]
	p := &v
	keep = p
	return p
}

func CopyProbe(n int) int {
	xs := make([]P, 4)
	xs[1].A = n
	v := xs[1]
	p := &v
	p.A = -1
	return xs[1].A
}
`)
	observations := &hazardObservations{}
	local := hazardCall(t, service, compiled, "Local", 41)
	element := hazardCall(t, service, compiled, "Element", 43)
	require.Equal(t, reflect.Pointer, reflect.ValueOf(local).Kind(), "test harness: Local did not return a pointer")
	require.Equal(t, reflect.Pointer, reflect.ValueOf(element).Kind(), "test harness: Element did not return a pointer")
	describe := renderGuestP
	observations.compareString("Local(41) straight after the run", "41/82/123", describe(local))
	observations.compareString("Element(43) straight after the run", "43/86/129", describe(element))
	for n := range 32 {
		hazardCall(t, service, compiled, "Local", 9000+n)
		hazardCall(t, service, compiled, "Element", 9000+n)
	}
	clobberArenas(t, service, 8)
	observations.compareString("Local(41) after 64 runs and 8 clobber runs", "41/82/123", describe(local))
	observations.compareString("Element(43) after 64 runs and 8 clobber runs", "43/86/129", describe(element))

	if got := hazardCall(t, service, compiled, "CopyProbe", 5); got != any(5) {
		observations.fire("native-go-divergence", "v := xs[1]; (&v).A = -1 changed xs[1].A to %v; native Go keeps 5", got)
	} else {
		observations.note("v := xs[i] is a copy, as native Go")
	}
	window.settle(observations)
	runtime.KeepAlive(local)
	runtime.KeepAlive(element)
	assertNoHazard(t, observations, "")
}

func renderGuestP(value any) string {
	pointer := reflect.ValueOf(value)
	if !pointer.IsValid() || pointer.IsNil() {
		return "<nil>"
	}
	pointee := pointer.Elem()
	return itoa(int(pointee.FieldByName("A").Int())) + "/" + itoa(int(pointee.FieldByName("B").Int())) + "/" +
		itoa(int(pointee.FieldByName("S").Index(3).Int()))
}

func TestStringsFieldsPublishedThroughAHeapContainerSurvivesReuse(t *testing.T) {
	window := openAuditWindow(t, 0)
	service := newHazardService(t)
	compiled := hazardCompile(t, service, `package main

import (
	"strconv"
	"strings"
)

var keep []string

var box = map[string][]string{}

func Publish(n int) []string {
	s := "alpha" + strconv.Itoa(n) + " beta" + strconv.Itoa(n) + " gamma-split-by-fields"
	parts := strings.Fields(s)
	keep = parts
	box["k"] = parts
	return parts
}

func Keep() []string { return keep }

func Box() []string { return box["k"] }
`)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled), "test harness: running the package initialisers")
	want := []string{"alpha77", "beta77", "gamma-split-by-fields"}
	returned, ok := hazardCall(t, service, compiled, "Publish", 77).([]string)
	require.True(t, ok, "test harness: Publish did not return []string")
	keep, ok := hazardCall(t, service, compiled, "Keep").([]string)
	require.True(t, ok, "test harness: Keep did not return []string")
	box, ok := hazardCall(t, service, compiled, "Box").([]string)
	require.True(t, ok, "test harness: Box did not return []string")
	observations := &hazardObservations{}
	observations.compareString("returned parts straight after the run", strings.Join(want, ","), strings.Join(returned, ","))
	clobberArenas(t, service, 8)
	observations.compareString("returned parts after 8 clobber runs", strings.Join(want, ","), strings.Join(returned, ","))
	observations.compareString("global keep after 8 clobber runs", strings.Join(want, ","), strings.Join(keep, ","))
	observations.compareString("heap map box[\"k\"] after 8 clobber runs", strings.Join(want, ","), strings.Join(box, ","))
	window.settle(observations)
	runtime.KeepAlive(returned)
	runtime.KeepAlive(keep)
	runtime.KeepAlive(box)
	assertNoHazard(t, observations, "")
}

func TestChildGoroutineValuesSurviveTheirArenasReuse(t *testing.T) {
	window := openAuditWindow(t, 0)
	service := newHazardService(t)
	compiled := hazardCompile(t, service, `package main

import (
	"strconv"
	"sync"
)

func churnWave(n int) {
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sink := 0
			for j := 0; j < 2000; j++ {
				s := "ZZZZZZZZZZZZZZZZZZZZ" + strconv.Itoa(j+i) + "ZZZZZZZZZZZZZZZZZZZZZZZZZZ"
				xs := []int{j, j, j}
				p := &xs
				sink += len(s) + len(*p)
			}
			_ = sink
		}(i)
	}
	wg.Wait()
}

func Channel(n int) []string {
	ch := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ch <- "child-" + strconv.Itoa(i) + "-sent-this-string-over-a-channel"
		}(i)
	}
	wg.Wait()
	close(ch)
	churnWave(n)
	out := make([]string, 0, n)
	for s := range ch {
		out = append(out, s)
	}
	return out
}

func Store(n int) []string {
	results := make([]string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = "child-" + strconv.Itoa(i) + "-stored-this-string-in-the-parent"
		}(i)
	}
	wg.Wait()
	churnWave(n)
	return results
}

func Pointer(n int) int {
	ch := make(chan *[]int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			s := []int{i, i * 2, i * 3}
			ch <- &s
		}(i)
	}
	wg.Wait()
	close(ch)
	churnWave(n)
	bad := 0
	for p := range ch {
		v := *p
		if len(v) != 3 || v[1] != 2*v[0] || v[2] != 3*v[0] {
			bad++
		}
	}
	return bad
}
`)
	const children = 32
	observations := &hazardObservations{}
	check := func(label, suffix string, values []string) {
		seen := map[string]bool{}
		for _, value := range values {
			seen[value] = true
		}
		missing := 0
		for i := range children {
			if !seen["child-"+itoa(i)+suffix] {
				missing++
			}
		}
		poisoned := false
		for _, value := range values {
			if strings.Contains(value, hazardPoisonPair) {
				poisoned = true
			}
		}
		switch {
		case poisoned:
			observations.fire("poison-bytes", "%s: a child-produced string holds poison bytes", label)
		case missing > 0 || len(values) != children:
			observations.fire("host-value-changed", "%s: %d of %d child-produced strings wrong (got %d values)", label, missing, children, len(values))
		default:
			observations.note("%s: all %d child-produced strings intact", label, children)
		}
	}
	for round := range 3 {
		sent, ok := hazardCall(t, service, compiled, "Channel", children).([]string)
		require.True(t, ok, "test harness: Channel did not return []string")
		stored, ok := hazardCall(t, service, compiled, "Store", children).([]string)
		require.True(t, ok, "test harness: Store did not return []string")
		check("round "+itoa(round)+" channel, read in-run", "-sent-this-string-over-a-channel", sent)
		check("round "+itoa(round)+" parent store, read in-run", "-stored-this-string-in-the-parent", stored)
		clobberArenas(t, service, 4)
		check("round "+itoa(round)+" channel, after 4 clobber runs", "-sent-this-string-over-a-channel", sent)
		check("round "+itoa(round)+" parent store, after 4 clobber runs", "-stored-this-string-in-the-parent", stored)
		if bad := hazardCall(t, service, compiled, "Pointer", children); bad != any(0) {
			observations.fire("guest-value-changed", "round %d: %v of %d child-produced *[]int read wrong in the parent", round, bad, children)
		}
		runtime.KeepAlive(sent)
		runtime.KeepAlive(stored)
	}
	window.settle(observations)
	assertNoHazard(t, observations, "")
}
