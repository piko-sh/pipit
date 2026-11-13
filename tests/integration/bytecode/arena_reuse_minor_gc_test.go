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
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
)

const capturedSliceSpawnSource = `package main

import "sync"

func spawn(wg *sync.WaitGroup, ready chan struct{}, results []int, idx int) {
	s := []int{idx, idx}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-ready
		s = append(s, idx)
		results[idx] = s[0] + s[1] + s[2]
	}()
}

func Run(n int, churn int) int {
	var wg sync.WaitGroup
	ready := make(chan struct{})
	results := make([]int, n)
	warm := 0
	for j := 0; j < 100000; j++ {
		t := []string{"warm"}
		p := &t
		warm += len(*p)
	}
	for i := 0; i < n; i++ {
		spawn(&wg, ready, results, i)
	}
	sink := 0
	for j := 0; j < churn; j++ {
		t := []string{"churn", "churn"}
		p := &t
		sink += len(*p)
	}
	close(ready)
	wg.Wait()
	bad := 0
	for i := 0; i < n; i++ {
		if results[i] != 3*i {
			bad++
		}
	}
	return bad
}
`

type cachedCallbackHost struct {
	held      any
	heldAfter []string
}

const replayBudget = 16 << 20

const replayChurn = `
type P struct {
	A int
	B int
}

func warm() int {
	sink := 0
	for j := 0; j < 8192; j++ {
		tmp := make([]P, 64)
		tmp[0].A = j
		sink += tmp[0].A - j
	}
	return sink
}

func churn() int {
	sink := 0
	for j := 0; j < 200000; j++ {
		tmp := make([]P, 64)
		tmp[0].A = j
		tmp[63].B = j
		sink += tmp[0].A + tmp[63].B - 2*j
	}
	return sink
}
`

func recycleWitness(before uint64) string {
	if !engine.ActiveDiagnosticBuilds().ReuseAudit {
		return ""
	}
	after := engine.AuditStats()
	if after.RecordedChunkRecycle+after.RecordedMarkDeadDrop == before {
		return "MinorGC recycled and dropped no chunk during the case, so the hazard's precondition was not reached"
	}
	return ""
}

func recycledChunkCount() uint64 {
	stats := engine.AuditStats()
	return stats.RecordedChunkRecycle + stats.RecordedMarkDeadDrop
}

func TestMapOfSliceHeaderPointersSurvivesMinorGC(t *testing.T) {
	window := openAuditWindow(t, 1)
	recycledBefore := recycledChunkCount()
	service := newHazardService(t)
	compiled := hazardCompile(t, service, `package main

var bad int

func Build(n int, churn int) map[int]*[]int {
	m := map[int]*[]int{}
	for i := 0; i < n; i++ {
		s := []int{i, i + 1}
		m[i] = &s
	}
	sink := 0
	for j := 0; j < churn; j++ {
		t := []string{"churn", "churn"}
		p := &t
		sink += len(*p)
	}
	bad = 0
	for i := 0; i < n; i++ {
		p := m[i]
		if p == nil || len(*p) != 2 || (*p)[0] != i || (*p)[1] != i+1 {
			bad++
		}
	}
	if sink != 2*churn {
		bad += 1000000
	}
	return m
}

func Bad() int { return bad }
`)
	const entries = 70000
	held, ok := hazardCall(t, service, compiled, "Build", entries, 600000).(map[int]*[]int)
	require.True(t, ok, "test harness: Build did not return map[int]*[]int")
	observations := &hazardObservations{}
	if bad := requireAs[int](t, hazardCall(t, service, compiled, "Bad")); bad > 0 {
		observations.fire("guest-value-changed", "%d of %d map entries read wrong inside the run after MinorGC churn", bad, entries)
	} else {
		observations.note("all %d map entries intact inside the run", entries)
	}
	hostBad := countBadMapEntries(held, entries)
	observations.note("host check straight after the run: %d bad entries (reuse at release also explains these)", hostBad)
	reason := recycleWitness(recycledBefore)
	window.settle(observations)
	runtime.KeepAlive(held)
	assertNoHazard(t, observations, reason)
}

func countBadMapEntries(held map[int]*[]int, entries int) int {
	bad := 0
	for i := range entries {
		p := held[i]
		if p == nil || len(*p) != 2 || (*p)[0] != i || (*p)[1] != i+1 {
			bad++
		}
	}
	return bad
}

func TestGoroutineCapturedSliceSurvivesTheParentsMinorGC(t *testing.T) {
	window := openAuditWindow(t, 1)
	recycledBefore := recycledChunkCount()
	observations := &hazardObservations{}
	const goroutines = 64
	clean := 0
	for round := range 3 {
		service := newHazardService(t)
		compiled := hazardCompile(t, service, capturedSliceSpawnSource)
		result, finished, err := hazardCallWithDeadline(t, service, compiled, 60*time.Second, "Run", goroutines, 600000)
		switch {
		case !finished:
			observations.fire("guest-hang", "round %d did not finish within 60s", round)
		case err != nil:
			observations.fire("guest-value-changed", "round %d failed: %v", round, err)
		case result != any(0):
			observations.fire("guest-value-changed", "round %d: %v of %d goroutines read a wrong captured slice", round, result, goroutines)
		default:
			clean++
		}
	}
	observations.note("%d of 3 rounds clean", clean)
	reason := recycleWitness(recycledBefore)
	window.settle(observations)
	assertNoHazard(t, observations, reason)
}

func (h *cachedCallbackHost) symbols() map[string]reflect.Value {
	return map[string]reflect.Value{
		"Hold": reflect.ValueOf(func(build func(int) any) int {
			h.held = build(1)
			for n := 2; n <= 3; n++ {
				build(n)
				h.heldAfter = append(h.heldAfter, renderCallbackResult(h.held))
			}
			return 0
		}),
	}
}

func renderCallbackResult(value any) string {
	switch held := value.(type) {
	case *[]int:
		if held == nil {
			return "<nil>"
		}
		return fmtInts(*held)
	case []string:
		return strings.Join(held, ",")
	default:
		return fmt.Sprintf("%T %v", value, value)
	}
}

func TestCachedCallbackResultSurvivesFreeListReuse(t *testing.T) {
	window := openAuditWindow(t, 1)
	observations := &hazardObservations{}
	for _, shape := range []struct {
		name string
		body string
		want string
	}{
		{name: "pointer to slice local", body: `s := []int{n, n * 10, n * 100}
		_ = strconv.Itoa(n)
		return &s`, want: "[1 10 100]"},
		{name: "string slice", body: `out := []string{"first-" + strconv.Itoa(n), "second-" + strconv.Itoa(n)}
		return out`, want: "first-1,second-1"},
		{name: "int slice", body: `out := []int{n, n + 1, n + 2}
		_ = strconv.Itoa(n)
		return out`, want: "[]int [1 2 3]"},
	} {
		host := &cachedCallbackHost{}
		service := newHazardService(t)
		service.RegisterPackage("holdhost", host.symbols())
		compiled := hazardCompile(t, service, `package main

import (
	"holdhost"
	"strconv"
)

func Run(churn int) int {
	return holdhost.Hold(func(n int) any {
		sink := 0
		for j := 0; j < churn; j++ {
			t := []string{"churn", "churn"}
			p := &t
			sink += len(*p)
		}
		_ = sink
		`+shape.body+`
	})
}
`)
		hazardCall(t, service, compiled, "Run", 600000)
		observations.compareString(shape.name+": first result when returned", shape.want, renderCallbackResult(host.held))
		for i, rendered := range host.heldAfter {
			observations.compareString(shape.name+": first result after callback "+itoa(i+2), shape.want, rendered)
		}
		clobberArenas(t, service, 8)
		observations.compareString(shape.name+": first result after the run and 8 clobber runs", shape.want, renderCallbackResult(host.held))
		runtime.KeepAlive(host)
	}
	window.settle(observations)
	assertNoHazard(t, observations, "")
}

func TestMinorGCDropsNoSlabTheHostStillReaches(t *testing.T) {
	window := openAuditWindow(t, 1)
	before := engine.AuditStats()
	service := newHazardService(t)
	compiled := hazardCompile(t, service, `package main

var bad int

func Build(n int, churn int) map[int]*[]int {
	m := map[int]*[]int{}
	for i := 0; i < n; i++ {
		s := []int{i, i + 1}
		m[i] = &s
	}
	sink := 0
	for j := 0; j < churn; j++ {
		t := []string{"churn", "churn"}
		p := &t
		sink += len(*p)
	}
	bad = 0
	for i := 0; i < n; i++ {
		p := m[i]
		if p == nil || len(*p) != 2 || (*p)[0] != i || (*p)[1] != i+1 {
			bad++
		}
	}
	return m
}

func Bad() int { return bad }
`)
	const entries = 1500
	held, ok := hazardCall(t, service, compiled, "Build", entries, 600000).(map[int]*[]int)
	require.True(t, ok, "test harness: Build did not return map[int]*[]int")
	observations := &hazardObservations{}
	if bad := hazardCall(t, service, compiled, "Bad"); bad != any(0) {
		observations.note("%v of %d entries read wrong in-run (a recycled chunk, not a drop)", bad, entries)
	} else {
		observations.note("all %d entries intact in-run", entries)
	}
	hits := engine.AuditReuse(engine.AuditOptions{MaxPasses: 0, KeepRecords: true, IncludeActiveRuns: true})
	drops := 0
	for _, hit := range hits {
		if hit.Event == engine.AuditEventMarkDeadDrop && (hit.Class == engine.AuditOutsideReach || hit.Class == engine.AuditInRunUnrooted) {
			drops++
			if drops <= 3 {
				observations.note("reachable mark-dead drop: %s", hit)
			}
		}
	}
	reason := ""
	switch {
	case !engine.ActiveDiagnosticBuilds().ReuseAudit:
		reason = "mark-dead drops are memory-safe in this build and only the reuse audit can observe them"
	case drops > 0:
		observations.fire("audit-mark-dead-drop-reachable", "%d slabs MinorGC dropped as dead were still reachable from the host-held map", drops)
	case engine.AuditStats().RecordedMarkDeadDrop == before.RecordedMarkDeadDrop:
		reason = "MinorGC dropped no slab during the case"
	}
	window.settle(observations)
	runtime.KeepAlive(held)
	assertNoHazard(t, observations, reason)
}

func TestPendingForeignRevocationSurvivesCompaction(t *testing.T) {
	runReplay(t, replayCase{entry: "Run", budget: replayBudget, needsCompaction: true, source: `package main

import (
	"retainhost"
	"sort"
)
` + replayChurn + `
func publish(n int) {
	var ps []P
	for i := 0; i < 8; i++ {
		ps = append(ps, P{A: n, B: i})
	}
	published := false
	sort.Slice(ps, func(i, j int) bool {
		if !published {
			published = true
			retainhost.Retain(ps)
		}
		return ps[i].B < ps[j].B
	})
}

func Run(n int) int {
	warm()
	publish(n)
	return churn()
}
`})
}

func TestBlindCallbackHandOffSurvivesCompaction(t *testing.T) {
	runReplay(t, replayCase{entry: "Run", budget: replayBudget, needsCompaction: true, source: `package main

import (
	"retainhost"
	"sort"
)
` + replayChurn + `
func publish(n int) {
	var ps []P
	for i := 0; i < 8; i++ {
		ps = append(ps, P{A: n, B: i})
	}
	published := false
	sort.Slice(ps, func(i, j int) bool {
		if !published {
			published = true
			retainhost.Retain(func() int { return 0 })
			retainhost.Retain(ps)
		}
		return ps[i].B < ps[j].B
	})
}

func Run(n int) int {
	warm()
	publish(n)
	return churn()
}
`})
}

func TestStoreIntoARevokedHeaderSurvivesCompaction(t *testing.T) {
	runReplay(t, replayCase{entry: "Run", budget: replayBudget, needsCompaction: true, source: `package main

import "retainhost"
` + replayChurn + `
func build(n int) {
	var xs []P
	p := &xs
	retainhost.Retain(p)
	*p = append(*p, P{A: n, B: n + 1}, P{A: n + 2, B: n + 3})
	retainhost.Settle()
}

func Run(n int) int {
	warm()
	build(n)
	return churn()
}
`})
}

func TestValueCapturedByAFuncFieldSurvivesMinorGC(t *testing.T) {
	previous := engine.SetLicenceCountingForTest(true)
	t.Cleanup(func() { engine.SetLicenceCountingForTest(previous) })
	window := openAuditWindow(t, 1)
	before := engine.LicenceDiagnostics().Reuse
	service := newHazardService(t)
	compiled := hazardCompile(t, service, `package main

type P struct {
	A int
	B int
}

type hook struct {
	name string
	run  func() int
}

func install(n int) hook {
	if n%2 == 0 {
		p := &P{A: n, B: 2 * n}
		s := []int{n, n + 1}
		return hook{name: "early", run: func() int { return p.A + p.B + s[0] + s[1] }}
	}
	var p *P
	var s []int
	h := hook{name: "late", run: func() int { return p.A + p.B + s[0] + s[1] }}
	p = &P{A: n, B: 2 * n}
	s = []int{n, n + 1}
	return h
}

func Run(n int, churn int) int {
	hooks := make([]hook, 0, n)
	for i := 0; i < n; i++ {
		hooks = append(hooks, install(i))
	}
	sink := 0
	for j := 0; j < churn; j++ {
		tmp := make([]P, 64)
		tmp[0].A = j
		t := []string{"churn", "churn"}
		q := &t
		sink += len(*q) + tmp[0].A - j
	}
	bad := 0
	for i := 0; i < n; i++ {
		if hooks[i].run() != 5*i+1 {
			bad++
		}
	}
	if sink != 2*churn {
		bad += 1000000
	}
	return bad
}
`)
	const hooks = 2000
	observations := &hazardObservations{}
	if bad := requireAs[int](t, hazardCall(t, service, compiled, "Run", hooks, 200000)); bad > 0 {
		observations.fire("guest-value-changed", "%d of %d captured values read wrong after MinorGC churn", bad, hooks)
	} else {
		observations.note("all %d captured values intact", hooks)
	}
	after := engine.LicenceDiagnostics().Reuse
	failedOnFunction := after.CompactionsFailedClosed["function"] - before.CompactionsFailedClosed["function"]
	compacted := after.Compactions - before.Compactions - failedClosedTotal(after) + failedClosedTotal(before)
	observations.note("minor collections: %d compacted, %d failed closed on a function value", compacted, failedOnFunction)
	if engine.ClosureCaptureWalksCells() && failedOnFunction > 0 {
		observations.fire("mark-failed-closed-on-wrapper", "%d minor collections failed closed on a function value the mark should trace", failedOnFunction)
	}
	reason := ""
	if compacted == 0 {
		reason = "no minor collection compacted during the case"
	}
	window.settle(observations)
	assertNoHazard(t, observations, reason)
}

func failedClosedTotal(counts engine.LicenceCounts) int64 {
	var total int64
	for _, count := range counts.CompactionsFailedClosed {
		total += count
	}
	return total
}
