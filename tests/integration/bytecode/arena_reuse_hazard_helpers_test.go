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
	"context"
	"fmt"
	"maps"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/sdk/stdlib"
)

const (
	testGoDispatchEnvVar = "PIPIT_TEST_GO_DISPATCH"
	hazardDetailLimit    = 1024
	hazardPoisonPair     = "\xdd\xdd"
	hazardPoisonWord     = "-2459565876494606883"
)

const clobberSource = `package main

import "strconv"

type pair struct {
	A int
	B int
}

func Clobber(seed int) int {
	total := 0
	for round := 0; round < 64; round++ {
		s := "ZZZZZZZZ" + strconv.Itoa(seed+round) + "ZZZZZZZZZZZZZZZZZZZZZZZZ"
		total += len(s)
		ints := []int{seed, round, seed + round}
		total += ints[2]
		ps := make([]pair, 8)
		ps[0].A = round
		total += ps[0].A
		names := make([]string, 4)
		for i := range names {
			names[i] = "YYYYYYYY" + strconv.Itoa(i+round)
		}
		total += len(names[3])
		box := &pair{A: round, B: seed}
		total += box.B
	}
	return total
}
`

type hazardObservations struct {
	fired   []string
	details []string
}

type auditWindow struct {
	before engine.AuditCounters
}

type hazardWindows struct {
	census censusWindow
	audit  *auditWindow
}

type censusWindow struct{ before map[string]int64 }

const hazardRounds = 12

type replayRecorder struct {
	mu       sync.Mutex
	held     []any
	want     []string
	kept     string
	keptWant string
}

type replayCase struct {
	source          string
	entry           string
	budget          uint64
	needsCompaction bool
	rounds          int
	guestCheck      string
}

type routeHost struct {
	mu         sync.Mutex
	stringChan chan string
	anyChan    chan any
	keeper     *routeKeeper
	reports    map[string][2]string
}

type routeKeeper struct {
	kept []any
}

const routeChurnSource = `type churnBox struct {
	S  string
	Xs []string
}

func Churn(seed int) int {
	total := 0
	for round := 0; round < 64; round++ {
		s := "QQ-" + strconv.Itoa(seed+round) + "-QQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQQ"
		xs := []string{s, s + "R", s + "S", s + "T"}
		anys := []any{s, xs, round}
		box := &churnBox{S: s, Xs: xs}
		m := map[string]any{"k": xs}
		ints := []int{seed, round, seed + round}
		total += len(xs[3]) + len(anys) + len(box.Xs) + len(m) + ints[2]
	}
	return total
}

`

const routeChurnGoroutines = `
func churnWorker(seed int, done chan bool) {
	Churn(seed)
	done <- true
}

func churnInGoroutines(seed int) {
	done := make(chan bool)
	for i := 0; i < 8; i++ {
		go churnWorker(seed+i*100, done)
	}
	for i := 0; i < 8; i++ {
		<-done
	}
}
`

type handOffRoute struct {
	t            *testing.T
	window       *auditWindow
	host         *routeHost
	service      *app.Service
	compiled     *program.CompiledFileSet
	observations hazardObservations
}

func hazardServiceOptions(extra ...app.Option) []app.Option {
	options := append([]app.Option(nil), extra...)
	if os.Getenv(testGoDispatchEnvVar) == "1" {
		options = append(options, app.WithForceGoDispatch())
	}
	return options
}

func newHazardService(t *testing.T, extra ...app.Option) *app.Service {
	t.Helper()
	service := app.NewService(hazardServiceOptions(extra...)...)
	service.UseSymbolProviders(stdlib.Providers()...)
	return service
}

func hazardCompile(t *testing.T, service *app.Service, source string) *program.CompiledFileSet {
	t.Helper()
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoError(t, err, "test harness: compiling the guest program")
	return compiled
}

func hazardCall(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, name string, arguments ...any) any {
	t.Helper()
	result, err := service.CallFunction(t.Context(), compiled, name, arguments...)
	require.NoError(t, err, "test harness: calling %s", name)
	return result
}

func clobberArenas(t *testing.T, service *app.Service, rounds int) {
	t.Helper()
	compiled := hazardCompile(t, service, clobberSource)
	for round := range rounds {
		hazardCall(t, service, compiled, "Clobber", round*1000)
	}
}

func (o *hazardObservations) fire(observable string, format string, arguments ...any) {
	o.fired = append(o.fired, observable)
	o.details = append(o.details, observable+": "+fmt.Sprintf(format, arguments...))
}

func (o *hazardObservations) note(format string, arguments ...any) {
	o.details = append(o.details, fmt.Sprintf(format, arguments...))
}

func (o *hazardObservations) compareString(label, want, got string) {
	switch {
	case strings.Contains(got, hazardPoisonPair) || strings.Contains(got, hazardPoisonWord):
		o.fire("poison-bytes", "%s read %q, want %q", label, got, want)
	case got != want:
		o.fire("host-value-changed", "%s read %q, want %q", label, got, want)
	default:
		o.note("%s unchanged (%q)", label, got)
	}
}

func assertNoHazard(t *testing.T, observations *hazardObservations, inconclusiveReason string) {
	t.Helper()
	switch {
	case len(observations.fired) > 0:
		require.Failf(t, "hazard reproduced", "seen by %s: %s", strings.Join(uniqueInOrder(observations.fired), ", "), observations.summary())
	case inconclusiveReason != "":
		t.Skipf("precondition not reached, so the case tested nothing: %s; %s", inconclusiveReason, observations.summary())
	default:
		t.Logf("no hazard observed: %s", observations.summary())
	}
}

func (o *hazardObservations) summary() string {
	parts := make([]string, len(o.details))
	for index, detail := range o.details {
		if len(detail) > hazardDetailLimit {
			detail = fmt.Sprintf("%s... (%d bytes)", detail[:hazardDetailLimit], len(detail))
		}
		parts[index] = detail
	}
	return strings.Join(parts, "; ")
}

func uniqueInOrder(values []string) []string {
	seen := map[string]bool{}
	unique := values[:0:0]
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			unique = append(unique, value)
		}
	}
	return unique
}

func openAuditWindow(t *testing.T, midRunEvery uint64) *auditWindow {
	t.Helper()
	engine.AuditReuse(engine.AuditOptions{MaxPasses: 0, KeepRecords: false, IncludeActiveRuns: true})
	previous := engine.AuditSetMidRunEvery(midRunEvery)
	t.Cleanup(func() { engine.AuditSetMidRunEvery(previous) })
	return &auditWindow{before: engine.AuditStats()}
}

func (w *auditWindow) settle(observations *hazardObservations) {
	if !engine.ActiveDiagnosticBuilds().ReuseAudit {
		return
	}
	hits := engine.AuditReuse(engine.AuditOptions{MaxPasses: 0, KeepRecords: false, IncludeActiveRuns: true})
	after := engine.AuditStats()
	explained, unexplained := 0, 0
	for _, hit := range hits {
		switch hit.Class {
		case engine.AuditReleaseHit:
			observations.fire("audit-release-hit", "%s", hit)
		case engine.AuditOutsideReach:
			observations.fire("audit-outside-reach", "%s", hit)
		case engine.AuditInRunOnly:
			if hit.ArenaHeaderRefs > 0 {
				explained++
				continue
			}
			unexplained++
			observations.note("in-run-only hit without arena header references: %s", hit)
		default:
			observations.note("audit hit: %s", hit)
		}
	}
	observations.note("audit window: recorded release=%d chunkRecycle=%d markDeadDrop=%d midRunChecks=%d inRunUnrooted=%d inRunOnly(explained=%d unexplained=%d)",
		after.RecordedRelease-w.before.RecordedRelease,
		after.RecordedChunkRecycle-w.before.RecordedChunkRecycle,
		after.RecordedMarkDeadDrop-w.before.RecordedMarkDeadDrop,
		after.MidRunChecks-w.before.MidRunChecks,
		after.InRunUnrootedHits-w.before.InRunUnrootedHits,
		explained, unexplained)
}

func itoa(value int) string {
	return fmt.Sprint(value)
}

func fmtInts(values []int) string {
	return fmt.Sprint(values)
}

func hazardCallWithDeadline(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, deadline time.Duration, name string, arguments ...any) (any, bool, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), deadline)
	defer cancel()
	type outcome struct {
		result any
		err    error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := service.CallFunction(ctx, compiled, name, arguments...)
		done <- outcome{result: result, err: err}
	}()
	select {
	case finished := <-done:
		return finished.result, true, finished.err
	case <-time.After(deadline + 5*time.Second):
		return nil, false, nil
	}
}

func openHazardWindows(t *testing.T) hazardWindows {
	t.Helper()
	return hazardWindows{census: openCensusWindow(), audit: openAuditWindow(t, 0)}
}

func (w hazardWindows) settle(observations *hazardObservations) {
	w.census.settle(observations)
	w.audit.settle(observations)
}

func compileCountingMnemonics(t *testing.T, service *app.Service, source string, mnemonics ...string) (*program.CompiledFileSet, map[string]int) {
	t.Helper()
	compiled := hazardCompile(t, service, source)
	listing := debug.DisassembleAssembly(compiled)
	counts := map[string]int{}
	for _, mnemonic := range mnemonics {
		counts[mnemonic] = strings.Count(listing, ":"+mnemonic+" ")
	}
	return compiled, counts
}

func compareRendering(observations *hazardObservations, label string, want, got any) {
	wantText, gotText := fmt.Sprint(want), fmt.Sprint(got)
	switch {
	case strings.Contains(gotText, hazardPoisonPair) || strings.Contains(gotText, hazardPoisonWord):
		observations.fire("poison-bytes", "%s read %q, want %q", label, gotText, wantText)
	case wantText != gotText:
		observations.fire("value-diverged", "%s read %q, want %q", label, gotText, wantText)
	}
}

func missingMnemonic(counts map[string]int, required ...string) string {
	for _, mnemonic := range required {
		if counts[mnemonic] == 0 {
			return "the listing has no " + mnemonic
		}
	}
	return ""
}

func openCensusWindow() censusWindow {
	return censusWindow{before: engine.StoreCensusViolations()}
}

func (w censusWindow) settle(observations *hazardObservations) {
	after := engine.StoreCensusViolations()
	if after == nil {
		return
	}
	var other []string
	for key, count := range after {
		delta := count - w.before[key]
		if delta <= 0 {
			continue
		}
		kind, _, _ := strings.Cut(key, "\t")
		if engine.StoreCensusGates(kind) {
			observations.fire("census", "%s x%d", strings.ReplaceAll(key, "\t", " | "), delta)
		} else {
			other = append(other, fmt.Sprintf("%s x%d", strings.ReplaceAll(key, "\t", " | "), delta))
		}
	}
	slices.Sort(other)
	observations.note("census: %d non-gating entries %s", len(other), strings.Join(other, "; "))
}

func noteLicenceDelta(observations *hazardObservations, before engine.LicenceCounts) {
	after := engine.LicenceDiagnostics().Reuse
	delta := func(later, earlier map[string]int64) map[string]int64 {
		out := map[string]int64{}
		for key, value := range later {
			if value-earlier[key] != 0 {
				out[key] = value - earlier[key]
			}
		}
		return out
	}
	observations.note("licence delta: releases=%d clean=%d partial=%d epoch=%d foreignRevokes=%d blindHandOffs=%d blindByteSlabReleases=%d partialByCause=%v epochByReason=%v epochByCause=%v handOffs=%v",
		after.Releases-before.Releases, after.ReleasesClean-before.ReleasesClean, after.ReleasesPartial-before.ReleasesPartial,
		after.ReleasesEpoch-before.ReleasesEpoch, after.ForeignRevokes-before.ForeignRevokes, after.BlindHandOffs-before.BlindHandOffs,
		after.BlindByteSlabReleases-before.BlindByteSlabReleases, delta(after.PartialByCause, before.PartialByCause),
		delta(after.EpochByReason, before.EpochByReason), delta(after.EpochByCause, before.EpochByCause),
		delta(after.HandOffs, before.HandOffs))
}

func revokedSlabsSince(before engine.LicenceCounts) int64 {
	var total int64
	for key, value := range engine.LicenceDiagnostics().Reuse.RevokedSlabs {
		total += value - before.RevokedSlabs[key]
	}
	return total
}

func renderRetained(value any) string {
	reflected := reflect.ValueOf(value)
	if reflected.Kind() == reflect.Pointer && !reflected.IsNil() {
		return fmt.Sprint(reflected.Elem().Interface())
	}
	return fmt.Sprint(value)
}

func (r *replayRecorder) symbols() map[string]reflect.Value {
	return map[string]reflect.Value{
		"Retain": reflect.ValueOf(func(value any) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.held = append(r.held, value)
			r.want = append(r.want, renderRetained(value))
		}),
		"Settle": reflect.ValueOf(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if last := len(r.held) - 1; last >= 0 {
				r.want[last] = renderRetained(r.held[last])
			}
		}),
		"Snapshot": reflect.ValueOf(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			for _, value := range slices.Backward(r.held) {
				switch p := value.(type) {
				case *string:
					r.kept = *p
				case *[]string:
					r.kept = (*p)[1]
				default:
					continue
				}
				r.keptWant = strings.Clone(r.kept)
				return
			}
		}),
	}
}

func (r *replayRecorder) compare(observations *hazardObservations, label string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index, value := range r.held {
		observations.compareString(fmt.Sprintf("%s: host-held value %d (%T)", label, index, value), r.want[index], renderRetained(value))
	}
	if r.keptWant != "" {
		observations.compareString(label+": host snapshot string", r.keptWant, r.kept)
	}
}

func (r *replayRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.held)
}

func runReplay(t *testing.T, testCase replayCase) {
	t.Helper()
	window := openAuditWindow(t, 0)
	before := engine.LicenceDiagnostics().Reuse
	recorder := &replayRecorder{}
	var options []app.Option
	if testCase.budget != 0 {
		options = append(options, app.WithMaxArenaSizeBytes(testCase.budget))
	}
	service := newHazardService(t, options...)
	service.RegisterPackage("retainhost", recorder.symbols())
	compiled := hazardCompile(t, service, testCase.source)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled), "test harness: running the package initialisers")
	observations := &hazardObservations{}
	rounds := max(testCase.rounds, 1)
	for round := range rounds {
		callReplay(t, service, compiled, testCase, observations, round)
	}
	recorder.compare(observations, "after the runs")
	clobberArenas(t, service, 8)
	recorder.compare(observations, "after 8 clobber runs")
	settleReplayAudit(window, observations)
	noteLicenceDelta(observations, before)
	after := engine.LicenceDiagnostics().Reuse
	compactions := after.Compactions - before.Compactions
	observations.note("revokedSlabs=%d compactions=%d deadChunksKeptFromReuse=%d closureRevokes=%d lateDetaches=%d acquireDetaches=%d",
		revokedSlabsSince(before), compactions, after.DeadChunksKeptFromReuse-before.DeadChunksKeptFromReuse,
		after.ClosureRevokes-before.ClosureRevokes, after.LateDetaches-before.LateDetaches,
		after.AcquireDetaches-before.AcquireDetaches)
	runtime.KeepAlive(recorder)
	require.Positive(t, recorder.count(), "test harness: the host retained nothing, so the trace never put arena storage outside")
	if testCase.needsCompaction {
		require.Positive(t, compactions, "test harness: no minor collection ran, so the trace never reached compaction")
	}
	assertNoHazard(t, observations, "")
}

func settleReplayAudit(window *auditWindow, observations *hazardObservations) {
	if !engine.ActiveDiagnosticBuilds().ReuseAudit {
		return
	}
	hits := engine.AuditReuse(engine.AuditOptions{MaxPasses: 0, KeepRecords: false, IncludeActiveRuns: true})
	dropped, headerExplained := 0, 0
	for _, hit := range hits {
		switch {
		case hit.Class == engine.AuditReleaseHit:
			observations.fire("audit-release-hit", "%s", hit)
		case hit.Class == engine.AuditOutsideReach && hit.Event == engine.AuditEventMarkDeadDrop:
			dropped++
		case hit.Class == engine.AuditOutsideReach && hit.ArenaHeaderRefs > 0:
			headerExplained++
		case hit.Class == engine.AuditOutsideReach:
			observations.fire("audit-outside-reach", "%s", hit)
		case hit.Class == engine.AuditInRunOnly && hit.ArenaHeaderRefs > 0:
		default:
			observations.note("audit hit: %s", hit)
		}
	}
	auditExcuse(dropped + headerExplained)
	after := engine.AuditStats()
	observations.note("audit window: recorded release=%d chunkRecycle=%d markDeadDrop=%d; outside-reach noted: dropped=%d headerExplained=%d",
		after.RecordedRelease-window.before.RecordedRelease,
		after.RecordedChunkRecycle-window.before.RecordedChunkRecycle,
		after.RecordedMarkDeadDrop-window.before.RecordedMarkDeadDrop, dropped, headerExplained)
}

func callReplay(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, testCase replayCase, observations *hazardObservations, round int) {
	t.Helper()
	hazardCall(t, service, compiled, testCase.entry, 7+round)
	if testCase.guestCheck == "" {
		return
	}
	if bad := requireAs[int](t, hazardCall(t, service, compiled, testCase.guestCheck)); bad != 0 {
		observations.fire("guest-value-changed", "round %d: the guest read %d values wrong", round, bad)
	}
}

func (k *routeKeeper) Keep(value any) { k.kept = append(k.kept, value) }

func newRouteHost() *routeHost {
	return &routeHost{stringChan: make(chan string, 16), anyChan: make(chan any, 16), keeper: &routeKeeper{}, reports: map[string][2]string{}}
}

func (h *routeHost) symbols() map[string]reflect.Value {
	return map[string]reflect.Value{
		"StringChan": reflect.ValueOf(func() chan string { return h.stringChan }),
		"AnyChan":    reflect.ValueOf(func() chan any { return h.anyChan }),
		"Keeper":     reflect.ValueOf(func() interface{ Keep(value any) } { return h.keeper }),
		"Report": reflect.ValueOf(func(label string, got string, want string) {
			h.mu.Lock()
			h.reports[strings.Clone(label)] = [2]string{strings.Clone(got), strings.Clone(want)}
			h.mu.Unlock()
		}),
	}
}

func routePrelude(imports ...string) string {
	var head strings.Builder
	head.WriteString("package main\n\nimport (\n\t\"mhost\"\n\t\"strconv\"\n")
	for _, name := range imports {
		head.WriteString("\t\"" + name + "\"\n")
	}
	head.WriteString(")\n\nvar _ = mhost.Keeper\n\n")
	head.WriteString("func mk(n int) string { return \"route-\" + strconv.Itoa(n) + \"-a-value-that-must-survive-arena-reuse\" }\n\n")
	head.WriteString(routeChurnSource)
	return head.String()
}

func newHandOffRoute(t *testing.T, body string, imports ...string) *handOffRoute {
	t.Helper()
	route := &handOffRoute{t: t, window: openAuditWindow(t, 0), host: newRouteHost()}
	route.service = newHazardService(t)
	route.service.RegisterPackage("mhost", route.host.symbols())
	route.compiled = hazardCompile(t, route.service, routePrelude(imports...)+body)
	return route
}

func (r *handOffRoute) call(name string, arguments ...any) any {
	r.t.Helper()
	result, err := r.service.CallFunction(r.t.Context(), r.compiled, name, arguments...)
	if err != nil {
		r.t.Logf("hand-off route: %s returned %v", name, err)
		return nil
	}
	return result
}

func (r *handOffRoute) reuse() {
	r.t.Helper()
	for seed := range 8 {
		r.call("Churn", seed*1000)
	}
	clobberArenas(r.t, r.service, 8)
}

func (r *handOffRoute) keeps(read func() map[string]string) {
	r.t.Helper()
	before := read()
	r.reuse()
	after := read()
	for _, label := range slices.Sorted(maps.Keys(before)) {
		r.observations.compareString(label, before[label], after[label])
	}
}

func (r *handOffRoute) finish(route string, keep ...any) {
	r.t.Helper()
	r.host.mu.Lock()
	for _, label := range slices.Sorted(maps.Keys(r.host.reports)) {
		report := r.host.reports[label]
		r.observations.compareString("report "+label, report[1], report[0])
	}
	r.host.mu.Unlock()
	r.window.settle(&r.observations)
	runtime.KeepAlive(keep)
	runtime.KeepAlive(r.host)
	require.Emptyf(r.t, uniqueInOrder(r.observations.fired), "route %s must keep the host's values intact: %s",
		route, strings.Join(r.observations.details, "; "))
}

func routeString(value any) string {
	if value == nil {
		return "<nil>"
	}
	return fmt.Sprint(value)
}
