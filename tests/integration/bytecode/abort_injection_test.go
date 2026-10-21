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

//go:build integration && pipit_testhooks && !safe && !(js && wasm)

package bytecode_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
)

const (
	abortShortMaxN     = 3
	abortCallDeadline  = 8 * time.Second
	abortCallTimeout   = 4 * time.Second
	abortClobberRounds = 4
)

type abortRecorder struct {
	mu   sync.Mutex
	held []any
	want []string
}

const abortCorruptLength = 1 << 24

type abortCall struct {
	name       string
	arguments  []any
	mustBeZero bool
	keep       bool
}

const abortSeed = -1

type abortCase struct {
	id           string
	source       string
	calls        []abortCall
	shows        []string
	budget       uint64
	injectInits  bool
	session      *abortSessionCase
	knownFailure string
	abandons     string
}

type abortSessionCase struct {
	setup []string
	run   string
	show  string
}

type abortHarness struct {
	t        *testing.T
	testCase abortCase
	service  *app.Service
	compiled *program.CompiledFileSet
	session  *app.Session
	recorder *abortRecorder
	clobber  *program.CompiledFileSet
}

type abortOutcome struct {
	result    any
	err       error
	hostPanic any
	hung      bool
}

type abortAuditWindow struct {
	before engine.AuditCounters
}

const abortSpawnsAbandons = "sync.Mutex locked in main.record"

const abortGlobalsSource = `package main

import (
	"retainhost"
	"strconv"
	"strings"
)

type T struct {
	Name string
	Vals []int
	Tags map[string]string
}

var gs string
var gl []string
var gm = map[string]string{}
var gp *T
var gb = []byte("gb-init")
var gsum int

func step(n int) int {
	gs = "gs-" + strconv.Itoa(n) + "-" + strings.Repeat("s", 24)
	local := make([]string, 0, 2)
	for i := 0; i < 6; i++ {
		local = append(local, "gl-"+strconv.Itoa(n)+"-"+strconv.Itoa(i))
	}
	gl = local
	gm["k"+strconv.Itoa(n%4)] = "gm-" + strconv.Itoa(n) + strings.Repeat("m", 16)
	gp = &T{Name: "gp-" + strconv.Itoa(n), Vals: []int{n, n + 1, n + 2}, Tags: map[string]string{"t": "tag-" + strconv.Itoa(n)}}
	gb = append(gb[:0], "gb-"+strconv.Itoa(n)...)
	retainhost.Retain(gl)
	retainhost.Retain(gp)
	gsum += len(gs) + len(gl)
	return gsum
}

func retainGlobals() {
	retainhost.Retain(&gs)
	retainhost.Retain(&gl)
	retainhost.Retain(&gm)
	retainhost.Retain(&gp)
	retainhost.Retain(&gb)
}

func RunHeavy(n int) int {
	retainGlobals()
	for i := 0; i < 3; i++ {
		step(n*10 + i)
		junk := make([]string, 0)
		for j := 0; j < 20000+n%7*3000; j++ {
			junk = append(junk, "junk-"+strconv.Itoa(j))
		}
		gsum += len(junk)
	}
	return gsum
}

func Show() string {
	out := gs + "|" + strings.Join(gl, ",") + "|"
	for i := 0; i < 4; i++ {
		out += gm["k"+strconv.Itoa(i)] + ","
	}
	if gp != nil {
		out += gp.Name + strconv.Itoa(len(gp.Vals)) + gp.Tags["t"]
		for _, v := range gp.Vals {
			out += "," + strconv.Itoa(v)
		}
	}
	return out + "|" + string(gb)
}
`

const abortAppendsSource = `package main

import (
	"retainhost"
	"strconv"
	"strings"
)

type pair struct {
	A int
	B string
}

var names = make([]string, 4)
var pairs = []pair{}
var ints = []int{}
var held *[]string

func step(n int) {
	local := []string{"c-" + strconv.Itoa(n), "d-" + strconv.Itoa(n), "e-" + strconv.Itoa(n), "f-" + strconv.Itoa(n)}
	copy(names, local)
	pairs = append(pairs[:0], pair{A: n, B: "pb-" + strconv.Itoa(n)}, pair{A: n + 1, B: "pc-" + strconv.Itoa(n)})
	ints = append(ints[:0], n, n+1, n+2)
	s := make([]string, 0, 1)
	held = &s
	*held = append(*held, "h-"+strconv.Itoa(n), "i-"+strconv.Itoa(n))
	more := []string{"j-" + strconv.Itoa(n), "k-" + strconv.Itoa(n)}
	*held = append(*held, more...)
	dst := make([]string, 4)
	retainhost.Retain(&dst)
	copy(dst, local)
}

func Run(n int) []string {
	retainhost.Retain(&names)
	retainhost.Retain(&pairs)
	retainhost.Retain(&ints)
	retainhost.Retain(&held)
	for i := 0; i < 3; i++ {
		func() {
			defer func() { recover() }()
			step(n*10 + i)
		}()
	}
	return names
}

func Show() string {
	out := strings.Join(names, ",") + "|"
	for _, p := range pairs {
		out += strconv.Itoa(p.A) + p.B + ","
	}
	for _, v := range ints {
		out += strconv.Itoa(v) + ","
	}
	if held != nil {
		out += strings.Join(*held, ",")
	}
	return out
}
`

const abortMapsSource = `package main

import (
	"retainhost"
	"strconv"
	"strings"
)

var byID = map[int]*[]int{}
var byName = map[string][]string{}
var anyMap = map[string]any{}
var bad int

func step(n int) {
	for i := 0; i < 32; i++ {
		s := []int{n, i, n + i}
		byID[i] = &s
		byName["n"+strconv.Itoa(i%4)] = []string{"v-" + strconv.Itoa(n), "w-" + strconv.Itoa(i)}
		anyMap["a"+strconv.Itoa(i%3)] = "any-" + strconv.Itoa(n+i)
		t := []string{"churn", "churn"}
		p := &t
		_ = len(*p)
	}
	for i := 0; i < 32; i++ {
		if p := byID[i]; p == nil || len(*p) != 3 || (*p)[0] != n || (*p)[2] != n+i {
			bad++
		}
	}
}

func Run(n int) int {
	bad = 0
	retainhost.Retain(&byID)
	retainhost.Retain(&byName)
	retainhost.Retain(&anyMap)
	for i := 0; i < 3; i++ {
		func() {
			defer func() { recover() }()
			step(n*10 + i)
		}()
	}
	return bad
}

func Show() string {
	out := ""
	for i := 0; i < 32; i++ {
		if p := byID[i]; p != nil {
			out += strconv.Itoa((*p)[0]) + ":" + strconv.Itoa((*p)[2]) + ","
		}
	}
	for i := 0; i < 4; i++ {
		out += strings.Join(byName["n"+strconv.Itoa(i)], "+") + ","
	}
	for i := 0; i < 3; i++ {
		if s, ok := anyMap["a"+strconv.Itoa(i)].(string); ok {
			out += s + ","
		}
	}
	return out
}
`

const abortClosuresSource = `package main

import (
	"strconv"
	"strings"
)

var describe func() string
var handlers = map[string]func() string{}

func step(n int) {
	parts := []string{"x-" + strconv.Itoa(n), "y-" + strconv.Itoa(n)}
	label := "label-" + strconv.Itoa(n) + strings.Repeat("l", 12)
	count := n
	describe = func() string {
		return label + "|" + strings.Join(parts, ",") + "|" + strconv.Itoa(count-n)
	}
	for i := 0; i < 3; i++ {
		key := "h" + strconv.Itoa(i)
		value := key + "-" + strconv.Itoa(n)
		handlers[key] = func() string { return value + "/" + parts[0] }
	}
}

func Run(n int) int {
	for i := 0; i < 3; i++ {
		func() {
			defer func() { recover() }()
			step(n*10 + i)
		}()
	}
	return len(handlers)
}

func Show() string {
	out := ""
	if describe != nil {
		out = describe()
	}
	for i := 0; i < 3; i++ {
		if h := handlers["h"+strconv.Itoa(i)]; h != nil {
			out += "|" + h()
		}
	}
	return out
}
`

const abortSpawnsSource = `package main

import (
	"retainhost"
	"strconv"
	"strings"
	"sync"
)

var mu sync.Mutex
var results = map[int]string{}
var collected = []string{}

func record(id int, s string) {
	mu.Lock()
	defer mu.Unlock()
	results[id] = s
}

func worker(id int, prefix string, parts []string, out chan string, wg *sync.WaitGroup) {
	defer wg.Done()
	defer func() { recover() }()
	s := prefix + "-" + strconv.Itoa(id) + "-" + strings.Join(parts, "+")
	record(id, s)
	out <- s
}

func step(n int) {
	var wg sync.WaitGroup
	out := make(chan string, 8)
	parts := []string{"p-" + strconv.Itoa(n), "q-" + strconv.Itoa(n)}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go worker(i, "w"+strconv.Itoa(n), parts, out, &wg)
	}
	wg.Wait()
	close(out)
	got := []string{}
	for s := range out {
		got = append(got, s)
	}
	collected = got
	retainhost.Retain(got)
}

func Run(n int) int {
	retainhost.Retain(&collected)
	for i := 0; i < 2; i++ {
		func() {
			defer func() { recover() }()
			step(n*10 + i)
		}()
	}
	return len(collected)
}

func Show() string {
	mu.Lock()
	defer mu.Unlock()
	out := ""
	for i := 0; i < 4; i++ {
		out += results[i] + ","
	}
	sorted := append([]string(nil), collected...)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	return out + "|" + strings.Join(sorted, ",")
}
`

const abortCallbacksSource = `package main

import (
	"retainhost"
	"sort"
	"strconv"
	"strings"
)

var fromCallback = []string{}
var lastLabel string

func step(n int) {
	fromCallback = fromCallback[:0]
	retainhost.Each(4, func(i int) string {
		s := "cb-" + strconv.Itoa(n) + "-" + strconv.Itoa(i) + strings.Repeat("c", 8)
		fromCallback = append(fromCallback, s)
		lastLabel = "last-" + s
		return s
	})
	rows := [][]int{{n, 3}, {n, 1}, {n, 2}}
	keys := []string{}
	sort.Slice(rows, func(a, b int) bool {
		keys = append(keys, "k"+strconv.Itoa(rows[a][1]))
		return rows[a][1] < rows[b][1]
	})
	retainhost.Retain(rows[0])
	retainhost.Retain(keys)
}

func Run(n int) int {
	retainhost.Retain(&fromCallback)
	retainhost.Retain(&lastLabel)
	for i := 0; i < 2; i++ {
		func() {
			defer func() { recover() }()
			step(n*10 + i)
		}()
	}
	return len(fromCallback)
}

func Show() string {
	return lastLabel + "|" + strings.Join(fromCallback, ",")
}
`

const abortVarInitSource = `package main

import (
	"strconv"
	"strings"
)

func build() map[string]string {
	m := map[string]string{}
	for i := 0; i < 8; i++ {
		m["k"+strconv.Itoa(i)] = "init-" + strconv.Itoa(i) + strings.Repeat("v", 12)
	}
	return m
}

var table = build()
var names = strings.Fields("alpha beta gamma " + strconv.Itoa(len(table)))
var label = "label-" + strings.Join(names, "-")

func Run(n int) int {
	return len(table) + n
}

func Show() string {
	out := label + "|" + strings.Join(names, ",") + "|"
	for i := 0; i < 8; i++ {
		out += table["k"+strconv.Itoa(i)] + ","
	}
	return out
}
`

func abortMaxN() int {
	maxN := 12
	if engine.ActiveDiagnosticBuilds().ArenaParanoid {
		maxN = 4
	}
	if testing.Short() {
		return min(maxN, abortShortMaxN)
	}
	return maxN
}

func (r *abortRecorder) retain(value any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.held = append(r.held, value)
	r.want = append(r.want, abortRender(value))
}

func (r *abortRecorder) symbols() map[string]reflect.Value {
	return map[string]reflect.Value{
		"Retain": reflect.ValueOf(r.retain),
		"Settle": reflect.ValueOf(r.settle),
		"Each": reflect.ValueOf(func(count int, each func(int) string) int {
			total := 0
			for index := range count {
				value := each(index)
				r.retain(value)
				total += len(value)
			}
			return total
		}),
	}
}

func (r *abortRecorder) settle() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index, value := range r.held {
		r.want[index] = abortRender(value)
	}
}

func (r *abortRecorder) compare(observations *hazardObservations, label string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	changed := 0
	for index, value := range r.held {
		got := abortRender(value)
		if strings.HasPrefix(got, "<corrupt") {
			changed++
			observations.fire("corrupt-header", "%s: host-held value %d (%T) holds a %s", label, index, value, got)
			continue
		}
		if got == r.want[index] {
			continue
		}
		changed++
		observations.compareString(fmt.Sprintf("%s: host-held value %d (%T)", label, index, value), r.want[index], got)
	}
	if changed == 0 {
		observations.note("%s: %d host-held values unchanged", label, len(r.held))
	}
}

func abortRender(value any) (rendering string) {
	defer debug.SetPanicOnFault(debug.SetPanicOnFault(true))
	defer func() {
		if recovered := recover(); recovered != nil {
			rendering = fmt.Sprintf("<corrupt value: reading it faulted: %v>", recovered)
		}
	}()
	if corrupt := abortCorruptString(reflect.ValueOf(value), 4); corrupt != "" {
		return corrupt
	}
	if err, ok := value.(error); ok && err != nil {
		return "error(" + err.Error() + ")"
	}
	reflected := reflect.ValueOf(value)
	for reflected.IsValid() && reflected.Kind() == reflect.Pointer && !reflected.IsNil() {
		reflected = reflected.Elem()
	}
	if !reflected.IsValid() {
		return fmt.Sprint(value)
	}
	if reflected.Kind() == reflect.Func {
		return "func"
	}
	if reflected.Kind() == reflect.Map {
		keys := reflected.MapKeys()
		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprint(key.Interface())+":"+abortRender(reflected.MapIndex(key).Interface()))
		}
		slices.Sort(parts)
		return "map[" + strings.Join(parts, " ") + "]"
	}
	return fmt.Sprint(reflected.Interface())
}

func abortCorruptString(value reflect.Value, depth int) string {
	if !value.IsValid() || depth == 0 {
		return ""
	}
	switch value.Kind() {
	case reflect.String:
		if value.Len() > abortCorruptLength {
			return fmt.Sprintf("<corrupt string header: length %d>", value.Len())
		}
	case reflect.Pointer, reflect.Interface:
		if !value.IsNil() {
			return abortCorruptString(value.Elem(), depth-1)
		}
	case reflect.Slice, reflect.Array:
		if value.Len() > abortCorruptLength {
			return fmt.Sprintf("<corrupt slice header: length %d>", value.Len())
		}
		for index := range value.Len() {
			if corrupt := abortCorruptString(value.Index(index), depth-1); corrupt != "" {
				return corrupt
			}
		}
	case reflect.Struct:
		for _, field := range value.Fields() {
			if corrupt := abortCorruptString(field, depth-1); corrupt != "" {
				return corrupt
			}
		}
	default:
	}
	return ""
}

func newAbortHarness(t *testing.T, testCase abortCase, withCost bool) *abortHarness {
	t.Helper()
	var options []app.Option
	if testCase.budget != 0 {
		options = append(options, app.WithMaxArenaSizeBytes(testCase.budget))
	}
	if withCost {
		options = append(options, app.WithCostBudget(1<<50))
	}
	harness := &abortHarness{t: t, testCase: testCase, recorder: &abortRecorder{}}
	harness.service = newHazardService(t, options...)
	harness.service.RegisterPackage("retainhost", harness.recorder.symbols())
	harness.clobber = hazardCompile(t, harness.service, clobberSource)
	if testCase.session != nil {
		harness.session = harness.service.NewSession()
		for _, code := range testCase.session.setup {
			_, err := harness.session.Submit(t.Context(), code)
			require.NoError(t, err, "abort harness: session setup %q", code)
		}
		return harness
	}
	harness.compiled = hazardCompile(t, harness.service, testCase.source)
	if !testCase.injectInits {
		require.NoError(t, harness.service.ExecuteInits(t.Context(), harness.compiled), "abort harness: running the package initialisers")
	}
	return harness
}

func (h *abortHarness) call(body func(ctx context.Context) (any, error)) abortOutcome {
	ctx, cancel := context.WithTimeout(h.t.Context(), abortCallTimeout)
	defer cancel()
	done := make(chan abortOutcome, 1)
	go func() {
		var outcome abortOutcome
		defer func() {
			if recovered := recover(); recovered != nil {
				outcome.hostPanic = recovered
			}
			done <- outcome
		}()
		outcome.result, outcome.err = body(ctx)
	}()
	select {
	case outcome := <-done:
		return outcome
	case <-time.After(abortCallDeadline):
		return abortOutcome{hung: true}
	}
}

func (h *abortHarness) run(seed int, observations *hazardObservations, injected bool) []abortOutcome {
	var outcomes []abortOutcome
	if h.testCase.session != nil {
		outcome := h.call(func(ctx context.Context) (any, error) {
			return h.session.Submit(ctx, strings.ReplaceAll(h.testCase.session.run, "SEED", strconv.Itoa(seed)))
		})
		return append(outcomes, outcome)
	}
	if h.testCase.injectInits && injected {
		outcomes = append(outcomes, h.call(func(ctx context.Context) (any, error) { return nil, h.service.ExecuteInits(ctx, h.compiled) }))
	}
	for _, call := range h.testCase.calls {
		arguments := make([]any, len(call.arguments))
		for index, argument := range call.arguments {
			if argument == abortSeed {
				argument = seed
			}
			arguments[index] = argument
		}
		outcome := h.call(func(ctx context.Context) (any, error) {
			return h.service.CallFunction(ctx, h.compiled, call.name, arguments...)
		})
		outcomes = append(outcomes, outcome)
		if outcome.err == nil && !outcome.hung && outcome.hostPanic == nil {
			if call.keep {
				h.recorder.retain(outcome.result)
			}
			if bad, ok := outcome.result.(int); call.mustBeZero && ok && bad != 0 && !injected {
				observations.fire("guest-value-changed", "%s(%d) read %d values wrong", call.name, seed, bad)
			}
		}
	}
	return outcomes
}

func (h *abortHarness) shows() []string {
	if h.testCase.session != nil {
		outcome := h.call(func(ctx context.Context) (any, error) { return h.session.Submit(ctx, h.testCase.session.show) })
		return []string{abortRenderOutcome(outcome)}
	}
	renderings := make([]string, 0, len(h.testCase.shows))
	for _, name := range h.testCase.shows {
		outcome := h.call(func(ctx context.Context) (any, error) { return h.service.CallFunction(ctx, h.compiled, name) })
		renderings = append(renderings, name+"="+abortRenderOutcome(outcome))
	}
	return renderings
}

func abortRenderOutcome(outcome abortOutcome) string {
	switch {
	case outcome.hung:
		return "<hung>"
	case outcome.hostPanic != nil:
		return fmt.Sprintf("<host panic %v>", outcome.hostPanic)
	case outcome.err != nil:
		return "<error " + abortErrorClass(outcome.err) + ": " + outcome.err.Error() + ">"
	default:
		return abortRender(outcome.result)
	}
}

func abortErrorClass(err error) string {
	switch {
	case err == nil:
		return "none"
	case errors.Is(err, fault.ErrArenaBudgetExceeded):
		return "arena-budget"
	case errors.Is(err, fault.ErrAllocationLimit):
		return "alloc-limit"
	case errors.Is(err, fault.ErrCostBudgetExceeded):
		return "cost"
	case errors.Is(err, fault.ErrExecutionCancelled):
		return "cancelled"
	default:
		return "other"
	}
}

func (h *abortHarness) readAndClobber(observations *hazardObservations, label string) {
	h.recorder.settle()
	before := h.shows()
	for round := range abortClobberRounds {
		outcome := h.call(func(ctx context.Context) (any, error) {
			return h.service.CallFunction(ctx, h.clobber, "Clobber", round*1000)
		})
		if outcome.hung || outcome.hostPanic != nil || outcome.err != nil {
			observations.fire("later-run-failed", "%s: clobbering run %d: %s", label, round, abortRenderOutcome(outcome))
		}
	}
	after := h.shows()
	for index := range before {
		switch {
		case strings.Contains(after[index], "<hung>") || strings.Contains(after[index], "<host panic"):
			observations.fire("later-read-failed", "%s: %s", label, after[index])
		default:
			observations.compareString(label+": guest state "+strconv.Itoa(index), before[index], after[index])
		}
	}
	h.recorder.compare(observations, label)
}

func abortSweepReaches(count uint64, maxN int) []uint64 {
	if count <= uint64(maxN) {
		reaches := make([]uint64, 0, count)
		for reach := uint64(1); reach <= count; reach++ {
			reaches = append(reaches, reach)
		}
		return reaches
	}
	head := maxN / 2
	reaches := make([]uint64, 0, maxN)
	for reach := 1; reach <= head; reach++ {
		reaches = append(reaches, uint64(reach))
	}
	tail := maxN - head
	span := count - uint64(head)
	for step := 1; step <= tail; step++ {
		reaches = append(reaches, uint64(head)+span*uint64(step)/uint64(tail))
	}
	return slices.Compact(reaches)
}

func abortSweepCase(t *testing.T, testCase abortCase) {
	t.Helper()
	if testCase.knownFailure != "" {
		t.Skip(testCase.knownFailure)
	}
	abortCheckControl(t, testCase)
	maxN := abortMaxN()
	for _, site := range engine.AbortInjectionSites() {
		t.Run(site, func(t *testing.T) {
			reaches := abortCountReaches(t, testCase, site)
			if reaches == 0 {
				t.Skipf("the case never reaches %s", site)
			}
			for _, reach := range abortSweepReaches(reaches, maxN) {
				t.Run(strconv.FormatUint(reach, 10), func(t *testing.T) {
					h := newAbortHarness(t, testCase, site == "cost")
					h.run(1, &hazardObservations{}, false)
					h.inject(site, reach, reaches)
				})
			}
		})
	}
}

func abortCountReaches(t *testing.T, testCase abortCase, site string) uint64 {
	t.Helper()
	counter := newAbortHarness(t, testCase, site == "cost")
	counter.run(1, &hazardObservations{}, false)
	require.NoError(t, engine.ArmAbortInjection(site, 0))
	counter.run(2, &hazardObservations{}, true)
	return engine.DisarmAbortInjection().Hits[site]
}

func abortCheckControl(t *testing.T, testCase abortCase) {
	t.Helper()
	observations := abortRunControl(t, testCase)
	if len(observations.fired) == 0 {
		return
	}
	seen := strings.Join(uniqueInOrder(observations.fired), ", ")
	require.Failf(t, "the uninjected control run shows a hazard", "seen by %s: %s", seen, observations.summary())
}

func abortRunControl(t *testing.T, testCase abortCase) *hazardObservations {
	t.Helper()
	harness := newAbortHarness(t, testCase, false)
	observations := &hazardObservations{}
	window := abortAuditOpen()
	harness.run(1, observations, false)
	outcomes := harness.run(2, observations, true)
	observations.note("control run: calls ended %s", abortOutcomeClasses(outcomes))
	harness.readAndClobber(observations, "control")
	harness.run(3, observations, false)
	harness.readAndClobber(observations, "control further run")
	window.close(observations)
	return observations
}

func (h *abortHarness) inject(site string, reach, reaches uint64) {
	h.t.Helper()
	observations := &hazardObservations{}
	window := abortAuditOpen()
	unwindsBefore := engine.LicenceDiagnostics().AbortUnwind
	require.NoError(h.t, engine.ArmAbortInjection(site, reach))
	outcomes := h.run(1000+int(reach), observations, true)
	report := engine.DisarmAbortInjection()
	observations.note("injected run: calls ended %s", abortOutcomeClasses(outcomes))
	if report.Fired {
		observations.note("the injection fired at %s", report.FiredAt)
	}
	for index, outcome := range outcomes {
		switch {
		case outcome.hung:
			observations.fire("injected-run-hung", "call %d outlived %s", index, abortCallDeadline)
		case outcome.hostPanic != nil:
			observations.fire("host-panic", "call %d panicked in the host: %v", index, outcome.hostPanic)
		}
	}
	if h.service.AbandonedLocks() > 0 {
		h.checkFailedClosed(observations, unwindsBefore)
		window.close(observations)
		assertNoHazard(h.t, observations, "")
		return
	}
	h.readAndClobber(observations, "after the injected run")
	further := h.run(2000+int(reach), observations, false)
	for index, outcome := range further {
		switch {
		case outcome.hung:
			observations.fire("later-run-hung", "further run call %d outlived %s", index, abortCallDeadline)
		case outcome.hostPanic != nil:
			observations.fire("host-panic", "further run call %d panicked in the host: %v", index, outcome.hostPanic)
		case outcome.err != nil:
			observations.fire("later-run-failed", "further run call %d failed: %v", index, outcome.err)
		}
	}
	h.readAndClobber(observations, "after the further run")
	window.close(observations)
	unreached := ""
	if !report.Fired {
		unreached = fmt.Sprintf("the injection at %s reach %d did not fire: the injected run reached the site fewer times than the counting run's %d or, at a site the guest can recover, had no later reach inside a guarded dispatch loop",
			site, reach, reaches)
	}
	assertNoHazard(h.t, observations, unreached)
}

func (h *abortHarness) checkFailedClosed(observations *hazardObservations, before engine.AbortUnwindCounts) {
	h.t.Helper()
	if h.testCase.abandons == "" {
		observations.fire("lock-abandoned", "the injected run left %d lock(s) held and the service failed closed", h.service.AbandonedLocks())
	}
	if blocked := engine.LicenceDiagnostics().AbortUnwind.NativeBlocked - before.NativeBlocked; blocked > 0 {
		observations.fire("lock-abandoned", "%d unwinding(s) were left blocked in native code, so a deferred release may not have run", blocked)
	}
	later := make([]abortCall, 0, len(h.testCase.shows)+len(h.testCase.calls))
	for _, name := range h.testCase.shows {
		later = append(later, abortCall{name: name, arguments: nil, mustBeZero: false, keep: false})
	}
	later = append(later, h.testCase.calls...)
	for _, call := range later {
		name := call.name
		arguments := make([]any, len(call.arguments))
		for index, argument := range call.arguments {
			if argument == abortSeed {
				argument = 3000
			}
			arguments[index] = argument
		}
		started := time.Now()
		outcome := h.call(func(ctx context.Context) (any, error) {
			return h.service.CallFunction(ctx, h.compiled, name, arguments...)
		})
		took := time.Since(started)
		switch {
		case outcome.hung || outcome.hostPanic != nil:
			observations.fire("later-run-hung", "%s after the service failed closed: %s", name, abortRenderOutcome(outcome))
		case !errors.Is(outcome.err, fault.ErrLockAbandoned):
			observations.fire("later-run-failed", "%s after the service failed closed did not fail with the abandoned lock: %s", name, abortRenderOutcome(outcome))
		case h.testCase.abandons != "" && !strings.Contains(outcome.err.Error(), h.testCase.abandons):
			observations.fire("lock-abandoned", "the service failed closed on a lock other than %q: %v", h.testCase.abandons, outcome.err)
		case took >= abortCallTimeout:
			observations.fire("later-run-hung", "%s took %v to fail closed", name, took)
		default:
			observations.note("%s failed at once: %v", name, outcome.err)
		}
	}
	h.recorder.settle()
	fresh := newHazardService(h.t)
	clobber := hazardCompile(h.t, fresh, clobberSource)
	for round := range abortClobberRounds {
		outcome := h.call(func(ctx context.Context) (any, error) {
			return fresh.CallFunction(ctx, clobber, "Clobber", round*1000)
		})
		if outcome.hung || outcome.hostPanic != nil || outcome.err != nil {
			observations.fire("later-run-failed", "clobbering run %d on a fresh service: %s", round, abortRenderOutcome(outcome))
		}
	}
	h.recorder.compare(observations, "after the service failed closed")
}

func abortOutcomeClasses(outcomes []abortOutcome) string {
	classes := make([]string, 0, len(outcomes))
	for _, outcome := range outcomes {
		switch {
		case outcome.hung:
			classes = append(classes, "hung")
		case outcome.hostPanic != nil:
			classes = append(classes, "host-panic")
		default:
			classes = append(classes, abortErrorClass(outcome.err))
		}
	}
	return strings.Join(classes, ",")
}

func abortAuditOpen() *abortAuditWindow {
	if !engine.ActiveDiagnosticBuilds().ReuseAudit {
		return &abortAuditWindow{}
	}
	engine.AuditReuse(engine.AuditOptions{MaxPasses: 0, KeepRecords: false, IncludeActiveRuns: true})
	return &abortAuditWindow{before: engine.AuditStats()}
}

func (w *abortAuditWindow) close(observations *hazardObservations) {
	settleReplayAudit(&auditWindow{before: w.before}, observations)
}

func abortCorpus() []abortCase {
	seed := abortSeed
	return []abortCase{
		{id: "globals-budget", source: abortGlobalsSource, budget: 4 << 20, calls: []abortCall{{name: "RunHeavy", arguments: []any{seed}}},
			shows: []string{"Show"}},
		{id: "appends-copy", source: abortAppendsSource, calls: []abortCall{{name: "Run", arguments: []any{seed}, keep: true}},
			shows: []string{"Show"}},
		{id: "maps", source: abortMapsSource, calls: []abortCall{{name: "Run", arguments: []any{seed}, mustBeZero: true}},
			shows: []string{"Show"}},
		{id: "closures", source: abortClosuresSource, calls: []abortCall{{name: "Run", arguments: []any{seed}}},
			shows: []string{"Show"}},
		{id: "spawns", source: abortSpawnsSource, calls: []abortCall{{name: "Run", arguments: []any{seed}}},
			shows: []string{"Show"}, abandons: abortSpawnsAbandons},
		{id: "spawn-captured-slice", source: capturedSliceSpawnSource, calls: []abortCall{{name: "Run", arguments: []any{8, 20000}, mustBeZero: true}}},
		{id: "callbacks", source: abortCallbacksSource, calls: []abortCall{{name: "Run", arguments: []any{seed}}},
			shows: []string{"Show"}},
		{id: "copy-rule", source: copyRuleSource, calls: []abortCall{
			{name: "AppendThroughGlobal"}, {name: "AppendPairsThroughGlobal"}, {name: "GivePairs", keep: true},
			{name: "GiveInts", keep: true}, {name: "GivePointer", keep: true}, {name: "GiveAnyMap", keep: true},
			{name: "GiveHolder", keep: true}, {name: "GoroutinePairs"}, {name: "ClosureCapture"}, {name: "StructSliceField"}},
			shows: []string{"ReadKeptPair", "ReadKeptInt", "ReadKeptPointer"}},
		{id: "varinit", source: abortVarInitSource, injectInits: true, calls: []abortCall{{name: "Run", arguments: []any{seed}}},
			shows: []string{"Show"}},
		{id: "session", session: &abortSessionCase{
			setup: []string{
				`import "strconv"`,
				`import "retainhost"`,
				`var keep *[]string`,
				`var names = []string{}`,
				`var label string`,
				`func publish(n int) { for i := 0; i < 4; i++ { func() { defer func() { recover() }(); s := []string{"s-" + strconv.Itoa(n+i), "t-" + strconv.Itoa(i)}; keep = &s; retainhost.Retain(keep); names = append(names[:0], s...); label = "label-" + strconv.Itoa(n) + "-" + (*keep)[0] }() } }`,
			},
			run:  `publish(SEED)`,
			show: `label + "|" + (*keep)[0] + "|" + (*keep)[1] + "|" + names[0] + "|" + names[1]`,
		}},
	}
}

func TestAbortInjectionSweep(t *testing.T) {
	for _, c := range abortCorpus() {
		t.Run(c.id, func(t *testing.T) {
			abortSweepCase(t, c)
		})
	}
}
