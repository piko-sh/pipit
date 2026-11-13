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
	"go/ast"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
)

const exportedCallbackBudget = 16 << 20

const exportedCallbackDeadline = 90 * time.Second

const exportedCallbackChurn = `
type P struct {
	A int
	B int
}

func churn(rounds int) int {
	sink := 0
	for j := 0; j < rounds; j++ {
		tmp := make([]P, 64)
		tmp[0].A = j
		tmp[63].B = j
		sink += tmp[0].A + tmp[63].B - 2*j
	}
	return sink
}
`

type exportHost struct {
	stop      chan struct{}
	signal    chan struct{}
	transport *http.Transport
	servers   []*httptest.Server
	kept      []any
	wg        sync.WaitGroup
	calls     atomic.Int64
	panics    atomic.Int64
	abandoned atomic.Bool
	mu        sync.Mutex
	stopped   bool
}

func newExportHost(t *testing.T) *exportHost {
	t.Helper()
	host := &exportHost{stop: make(chan struct{}), signal: make(chan struct{}, 1), transport: &http.Transport{}}
	t.Cleanup(func() {
		host.stopAll()
		assert.False(t, host.abandoned.Load(),
			"a host goroutine running a callback did not finish within %s", exportedCallbackDeadline)
	})
	return host
}

func (h *exportHost) call(f func(int), i int) (ok bool) {
	defer func() {
		if recover() != nil {
			h.panics.Add(1)
			ok = false
		}
	}()
	f(i)
	h.calls.Add(1)
	return true
}

func (h *exportHost) runInspectElsewhere(f func(ast.Node) bool) {
	h.wg.Go(func() {
		node := &ast.Ident{Name: "x"}
		for i := 0; ; i++ {
			select {
			case <-h.stop:
				return
			default:
			}
			if !h.call(func(i int) {
				if i%2 == 0 {
					f(node)
				} else {
					f(nil)
				}
			}, i) {
				return
			}
			if i%64 == 63 {
				runtime.Gosched()
			}
		}
	})
}

func (h *exportHost) stopAll() {
	h.mu.Lock()
	if !h.stopped {
		h.stopped = true
		close(h.stop)
	}
	servers := h.servers
	h.servers = nil
	h.mu.Unlock()
	waited := make(chan struct{})
	go func() {
		h.wg.Wait()
		close(waited)
	}()
	select {
	case <-waited:
	case <-time.After(exportedCallbackDeadline):
		h.abandoned.Store(true)
	}
	for _, server := range servers {
		server.Close()
	}
	h.transport.CloseIdleConnections()
}

func (h *exportHost) symbols() map[string]reflect.Value {
	return map[string]reflect.Value{
		"Keep": reflect.ValueOf(func(f func(int)) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.kept = append(h.kept, f)
		}),
		"RunElsewhere": reflect.ValueOf(func(f func(int)) {
			h.wg.Go(func() {
				for i := 0; ; i++ {
					select {
					case <-h.stop:
						return
					default:
					}
					if !h.call(f, i) {
						return
					}
					if i%64 == 63 {
						runtime.Gosched()
					}
				}
			})
		}),
		"RunElsewhereTimes": reflect.ValueOf(func(f func(int), times int) {
			h.wg.Go(func() {
				for i := range times {
					if !h.call(f, i) {
						return
					}
				}
			})
		}),
		"CallNow": reflect.ValueOf(func(f func(int), i int) {
			f(i)
			h.calls.Add(1)
		}),
		"CallKept": reflect.ValueOf(func(i int) {
			h.mu.Lock()
			kept := append([]any(nil), h.kept...)
			h.mu.Unlock()
			for _, f := range kept {
				if f, ok := f.(func(int)); ok {
					f(i)
					h.calls.Add(1)
				}
			}
		}),
		"Observe": reflect.ValueOf(func(value any) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.kept = append(h.kept, value)
		}),
		"WaitSignal": reflect.ValueOf(func() bool {
			select {
			case <-h.signal:
				return true
			case <-h.stop:
				return false
			case <-time.After(exportedCallbackDeadline):
				return false
			}
		}),
		"Signal": reflect.ValueOf(func() {
			select {
			case h.signal <- struct{}{}:
			default:
			}
		}),
		"Pause": reflect.ValueOf(func(microseconds int) {
			time.Sleep(time.Duration(microseconds) * time.Microsecond)
		}),
		"RunInspectElsewhere": reflect.ValueOf(func(f func(ast.Node) bool) {
			h.runInspectElsewhere(f)
		}),
		"RunNamedInspectElsewhere": reflect.ValueOf(func(f InspectFunc) {
			h.runInspectElsewhere(f)
		}),
		"KeepInspect": reflect.ValueOf(func(f func(ast.Node) bool) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.kept = append(h.kept, f)
		}),
		"KeepNamedInspect": reflect.ValueOf(func(f InspectFunc) {
			h.mu.Lock()
			defer h.mu.Unlock()
			h.kept = append(h.kept, f)
		}),
		"StopAll": reflect.ValueOf(h.stopAll),
		"Calls":   reflect.ValueOf(func() int { return int(h.calls.Load()) }),
		"Serve": reflect.ValueOf(func(handle func(string) string) string {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = io.WriteString(w, handle(r.URL.Path))
			}))
			h.mu.Lock()
			h.servers = append(h.servers, server)
			h.mu.Unlock()
			return server.URL
		}),
		"Get": reflect.ValueOf(func(url string) string {

			request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
			if err != nil {
				return err.Error()
			}
			response, err := (&http.Client{Transport: h.transport}).Do(request)
			if err != nil {
				return err.Error()
			}
			defer func() { _ = response.Body.Close() }()
			body, err := io.ReadAll(response.Body)
			if err != nil {
				return err.Error()
			}
			return string(body)
		}),
	}
}

func exportedCallbackService(t *testing.T, host *exportHost, source string, extra ...app.Option) (*app.Service, *program.CompiledFileSet) {
	t.Helper()
	options := append([]app.Option{app.WithMaxArenaSizeBytes(exportedCallbackBudget)}, extra...)
	service := newHazardService(t, options...)
	service.RegisterPackage("callbackhost", host.symbols())
	compiled := hazardCompile(t, service, source)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled), "test harness: running the package initialisers")
	return service, compiled
}

func callWithin(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, name string, arguments ...any) (any, error) {
	t.Helper()
	type outcome struct {
		result any

		err error
	}
	done := make(chan outcome, 1)
	go func() {
		result, err := service.CallFunction(t.Context(), compiled, name, arguments...)
		done <- outcome{result: result, err: err}
	}()
	select {
	case got := <-done:
		return got.result, got.err
	case <-time.After(exportedCallbackDeadline):
		stacks := make([]byte, 1<<22)
		stacks = stacks[:runtime.Stack(stacks, true)]
		require.FailNowf(t, "call did not return", "%s did not return within %s (deadlock?):\n%s",
			name, exportedCallbackDeadline, stacks)
		return nil, nil
	}
}

func successfulCompactions(before, after engine.LicenceCounts) int64 {
	failed := int64(0)
	for reason, count := range after.CompactionsFailedClosed {
		failed += count - before.CompactionsFailedClosed[reason]
	}
	return after.Compactions - before.Compactions - failed
}

func requireOwnerCompacted(t *testing.T, service *app.Service, compiled *program.CompiledFileSet, want any, arguments ...any) {
	t.Helper()
	before := engine.LicenceDiagnostics().Reuse
	result, err := callWithin(t, service, compiled, "Run", arguments...)
	require.NoError(t, err, "a VM that exported a callback must keep compacting, so its churn stays within the budget")
	require.Equal(t, want, result)
	after := engine.LicenceDiagnostics().Reuse
	require.Positive(t, successfulCompactions(before, after),
		"the owner's collections must compact while native code holds its callback (failed closed: %v)", after.CompactionsFailedClosed)
}

func TestAnOwnerCompactsWhileNativeCodeRunsItsCallbackElsewhere(t *testing.T) {
	host := newExportHost(t)
	service, compiled := exportedCallbackService(t, host, `package main

import (
	"callbackhost"
	"strconv"
	"sync"
)
`+exportedCallbackChurn+`
type shared struct {
	mu     sync.Mutex
	latest *int
	items  []string
	byKey  map[string]int
}

func Run() int {
	s := &shared{byKey: map[string]int{}}
	callbackhost.RunElsewhere(func(i int) {
		s.mu.Lock()
		v := i
		s.latest = &v
		s.items = append(s.items, strconv.Itoa(i))
		if len(s.items) > 64 {
			s.items = s.items[:0]
		}
		s.byKey[strconv.Itoa(i%16)] = i
		s.mu.Unlock()
	})
	total := 0
	for round := 0; round < 60; round++ {
		total += churn(20000)
		callbackhost.Observe(s)
	}
	callbackhost.StopAll()
	return total
}
`)
	requireOwnerCompacted(t, service, compiled, 0)
	require.Positive(t, host.calls.Load(), "the callback must have run while the owner compacted")
}

func TestAnOwnerCompactsWhileNativeCodeRunsItsTypedInspectCallbacksElsewhere(t *testing.T) {
	host := newExportHost(t)
	service, compiled := exportedCallbackService(t, host, `package main

import (
	"callbackhost"
	"go/ast"
	"strconv"
	"sync"
)
`+exportedCallbackChurn+`
type shared struct {
	mu     sync.Mutex
	latest *int
	items  []string
	byKey  map[string]int
	nodes  int
}

func (s *shared) note(n ast.Node, by string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nodes++
	v := s.nodes
	s.latest = &v
	s.items = append(s.items, by+strconv.Itoa(v))
	if len(s.items) > 64 {
		s.items = s.items[:0]
	}
	if n != nil {
		s.byKey[strconv.Itoa(v%16)] = v
	}
	return n != nil
}

func (s *shared) visit(n ast.Node) bool {
	return s.note(n, "m")
}

func Run() int {
	s := &shared{byKey: map[string]int{}}
	callbackhost.RunInspectElsewhere(func(n ast.Node) bool { return s.note(n, "c") })
	callbackhost.RunInspectElsewhere(s.visit)
	callbackhost.RunNamedInspectElsewhere(func(n ast.Node) bool { return s.note(n, "n") })
	total := 0
	for round := 0; round < 60; round++ {
		total += churn(20000)
		callbackhost.Observe(s)
	}
	callbackhost.StopAll()
	return total
}
`)
	requireOwnerCompacted(t, service, compiled, 0)
	require.Positive(t, host.calls.Load(), "the callbacks must have run while the owner compacted")
}

func TestADeclaredTopLevelCallOfATypedInspectCallbackRunsAsARootExecution(t *testing.T) {
	host := newExportHost(t)
	service, compiled := exportedCallbackService(t, host, `package main

import (
	"callbackhost"
	"go/ast"
)

var visited int

func init() {
	callbackhost.KeepInspect(func(n ast.Node) bool {
		visited++
		return n != nil
	})
	callbackhost.KeepNamedInspect(func(n ast.Node) bool {
		visited++
		return n != nil
	})
}
`)
	service.DeclareTopLevelCalls(compiled)
	host.mu.Lock()
	kept := append([]any(nil), host.kept...)
	host.mu.Unlock()
	require.Len(t, kept, 2)
	for _, f := range kept {
		inspect, ok := reflect.TypeAssert[func(ast.Node) bool](reflect.ValueOf(f).Convert(reflect.TypeFor[func(ast.Node) bool]()))
		require.True(t, ok)
		before := engine.LicenceDiagnostics().CallbackRelease
		require.True(t, inspect(&ast.Ident{Name: "x"}))
		require.False(t, inspect(nil))
		after := engine.LicenceDiagnostics().CallbackRelease
		require.Equal(t, int64(2), after.TopLevelRuns-before.TopLevelRuns,
			"%T: a call from host code with no VM below it runs as a root execution", f)
		require.Equal(t, before.TopLevelFallbacks, after.TopLevelFallbacks)
	}
}

func TestAGoroutinesExportedCallbackStillExcludesItsSpawnersWalks(t *testing.T) {
	host := newExportHost(t)
	service, compiled := exportedCallbackService(t, host, `package main

import (
	"callbackhost"
	"strconv"
	"sync"
)
`+exportedCallbackChurn+`
type shared struct {
	mu     sync.Mutex
	latest *int
	items  []string
}

func Run() int {
	s := &shared{}
	done := make(chan bool)
	go func() {
		callbackhost.RunElsewhere(func(i int) {
			s.mu.Lock()
			v := i
			s.latest = &v
			s.items = append(s.items, strconv.Itoa(i))
			if len(s.items) > 64 {
				s.items = s.items[:0]
			}
			s.mu.Unlock()
		})
		done <- true
	}()
	<-done
	total := 0
	for round := 0; round < 60; round++ {
		total += churn(20000)
		callbackhost.Observe(s)
	}
	callbackhost.StopAll()
	return total
}
`)
	result, err := callWithin(t, service, compiled, "Run")
	require.NoError(t, err)
	require.Equal(t, 0, result)
	require.Positive(t, host.calls.Load())
}

func TestALongRunningOwnerWithAnExportedHandlerStaysWithinItsBudget(t *testing.T) {
	host := newExportHost(t)
	service, compiled := exportedCallbackService(t, host, `package main

import (
	"callbackhost"
	"strconv"
	"strings"
	"sync"
)
`+exportedCallbackChurn+`
func Run(requests int) int {
	var mu sync.Mutex
	hits := map[string]int{}
	url := callbackhost.Serve(func(path string) string {
		mu.Lock()
		hits[path]++
		mu.Unlock()
		return strings.Repeat("x", 32) + path
	})
	total := 0
	for i := 0; i < requests; i++ {
		body := callbackhost.Get(url + "/" + strconv.Itoa(i%8))
		total += len(body) - 34
		total += churn(4000)
	}
	mu.Lock()
	served := 0
	for _, count := range hits {
		served += count
	}
	mu.Unlock()
	return total + served - requests
}
`)
	requireOwnerCompacted(t, service, compiled, 0, 400)
}

func TestExportedCallbacksNeverDeadlockTheirOwner(t *testing.T) {
	cases := []struct {
		name string

		source string

		options []app.Option

		start bool

		wantErr bool
	}{
		{name: "the owner waits for callbacks with WaitGroup.Wait", source: `
func Run() int {
	var mu sync.Mutex
	var wg sync.WaitGroup
	items := []string{}
	for worker := range 4 {
		wg.Go(func() {
			for step := range 200 {
				mu.Lock()
				items = append(items, strconv.Itoa(worker*1000+step))
				mu.Unlock()
			}
		})
	}
	total := churn(20000)
	callbackhost.Observe(&items)
	wg.Wait()
	return total + len(items) - 800
}
`},
		{name: "the owner receives from a callback that sends", source: `
func Run() int {
	box := &struct{ items []int }{}
	results := make(chan int, 200)
	callbackhost.RunElsewhereTimes(func(i int) {
		box.items = append(box.items[:0], i)
		results <- i
	}, 200)
	total := 0
	for range 200 {
		got := <-results
		callbackhost.Observe(box)
		total += got - got
		total += churn(200)
	}
	return total
}
`},
		{name: "the owner locks a mutex a callback holds across a native call", source: `
func Run() int {
	var mu sync.Mutex
	box := &struct{ items []int }{}
	callbackhost.RunElsewhereTimes(func(i int) {
		mu.Lock()
		box.items = append(box.items, i)
		callbackhost.Pause(50)
		mu.Unlock()
	}, 200)
	total := 0
	for range 100 {
		callbackhost.Observe(box)
		mu.Lock()
		total += len(box.items) - len(box.items)
		mu.Unlock()
		total += churn(200)
	}
	callbackhost.StopAll()
	return total
}
`},
		{name: "a callback waits in a native for the owner", source: `
func Run() int {
	total := churn(20000)
	box := &struct{ items []int }{}
	waited := make(chan bool, 1)
	callbackhost.RunElsewhereTimes(func(i int) {
		box.items = append(box.items, i)
		waited <- callbackhost.WaitSignal()
	}, 1)
	for range 4 {
		callbackhost.Observe(box)
		callbackhost.Pause(500)
	}
	callbackhost.Signal()
	if !<-waited {
		return -1
	}
	callbackhost.StopAll()
	return total + len(box.items) - 1
}
`},
		{name: "a callback calls native code that calls further callbacks", source: `
func Run() int {
	var mu sync.Mutex
	box := &struct{ items []int }{}
	inner := func(i int) {
		mu.Lock()
		box.items = append(box.items, i)
		mu.Unlock()
	}
	middle := func(i int) {
		callbackhost.CallNow(inner, i)
		callbackhost.CallNow(inner, -i)
	}
	callbackhost.RunElsewhereTimes(func(i int) {
		callbackhost.CallNow(middle, i)
	}, 300)
	total := 0
	for range 50 {
		total += churn(400)
		callbackhost.Observe(box)
	}
	callbackhost.StopAll()
	mu.Lock()
	defer mu.Unlock()
	return total + len(box.items) - 600
}
`},
		{name: "a kept callback runs on the owner's own goroutine", source: `
func Run() int {
	box := &struct{ items []int }{}
	callbackhost.Keep(func(i int) {
		box.items = append(box.items, i)
		callbackhost.Observe(box)
		churn(100)
	})
	total := 0
	for i := range 200 {
		callbackhost.CallKept(i)
		total += churn(200)
	}
	return total + len(box.items) - 200
}
`},
		{name: "callbacks keep running after their owner's execution ends", start: true, source: `
var box = &struct{ items []int }{}

func Start() int {
	callbackhost.RunElsewhere(func(i int) {
		box.items = append(box.items[:0], i)
	})
	return 0
}

func Run() int {
	total := 0
	for range 20 {
		total += churn(20000)
	}
	callbackhost.StopAll()
	return total
}
`},
		{name: "a callback panics and recovers", source: `
func Run() int {
	var mu sync.Mutex
	box := &struct{ items []int }{}
	callbackhost.RunElsewhereTimes(func(i int) {
		defer func() {
			if recover() != nil {
				mu.Lock()
				box.items = append(box.items, i)
				mu.Unlock()
			}
		}()
		if i%2 == 0 {
			panic("even")
		}
	}, 100)
	total := 0
	for range 40 {
		total += churn(2000)
		callbackhost.Observe(box)
	}
	callbackhost.StopAll()
	return total
}
`},
		{name: "a callback is stopped by its deadline", options: []app.Option{app.WithCallbackTimeout(200 * time.Millisecond)}, wantErr: true, source: `
func Run() int {
	box := &struct{ items []int }{}
	callbackhost.RunElsewhereTimes(func(i int) {
		for {
			box.items = append(box.items[:0], i)
		}
	}, 1)
	total := 0
	for range 400 {
		total += churn(2000)
		callbackhost.Observe(box)
		callbackhost.Pause(1000)
	}
	return total
}
`},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			host := newExportHost(t)
			source := `package main

import (
	"callbackhost"
	"strconv"
	"sync"
)

var _ = strconv.Itoa
var _ sync.Mutex
` + exportedCallbackChurn + testCase.source
			service, compiled := exportedCallbackService(t, host, source, testCase.options...)
			if testCase.start {
				_, err := callWithin(t, service, compiled, "Start")
				require.NoError(t, err)
			}
			before := engine.LicenceDiagnostics()
			result, err := callWithin(t, service, compiled, "Run")
			host.stopAll()
			after := engine.LicenceDiagnostics()
			t.Logf("export gates %+v -> %+v; failed closed %v; shared walks %d", before.ExportGates, after.ExportGates,
				after.Reuse.CompactionsFailedClosed, after.Reuse.SharedWalks-before.Reuse.SharedWalks)
			if testCase.wantErr {
				require.Error(t, err, "the callback's deadline must fail the execution it belongs to")
				require.NotContains(t, err.Error(), "budget", "the owner must keep compacting until the callback's deadline fails it")
				return
			}
			require.NoError(t, err)
			require.Equal(t, 0, result)
		})
	}
}

func TestAnOwnerWhoseCallbackRunsWhileItsExecutionIsCancelledStops(t *testing.T) {
	host := newExportHost(t)
	service, compiled := exportedCallbackService(t, host, `package main

import "callbackhost"
`+exportedCallbackChurn+`
func Run() int {
	box := &struct{ items []int }{}
	callbackhost.RunElsewhere(func(i int) {
		box.items = append(box.items[:0], i)
	})
	total := 0
	for {
		total += churn(2000)
		callbackhost.Observe(box)
	}
}
`, app.WithMaxExecutionTime(500*time.Millisecond))
	_, err := callWithin(t, service, compiled, "Run")
	require.Error(t, err, "the execution deadline must stop the owner while its callback runs elsewhere")
	require.NotContains(t, err.Error(), "budget", "the owner must keep compacting until its deadline stops it")
	host.stopAll()
}
