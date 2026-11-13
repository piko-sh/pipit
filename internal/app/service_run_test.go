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

package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"reflect"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab"
)

func TestEvalCancelledContext(t *testing.T) {
	t.Parallel()
	service := NewService()

	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errors.New("test: pre-cancelled eval context"))

	_, err := service.Eval(ctx, `1 + 2`)
	require.ErrorIs(t, err, fault.ErrExecutionCancelled, "a cancelled context stops execution")
	require.ErrorIs(t, err, context.Canceled)
}

func TestEvalTimeout(t *testing.T) {
	t.Parallel()
	service := NewService()
	service.UseSymbols(newOSSymbols())

	ctx, cancel := context.WithTimeoutCause(t.Context(), 100*time.Millisecond, errors.New("test: eval timeout"))
	defer cancel()

	_, err := service.Eval(ctx, `
import (
	"os"
)
x := 0
for {
	x++
	os.Getenv("PATH")
}
`)
	require.ErrorIs(t, err, context.DeadlineExceeded, "the deadline stops execution")
}

func TestCompileCancelledContext(t *testing.T) {
	t.Parallel()
	service := NewService()

	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errors.New("test: pre-cancelled compile context"))

	_, err := service.Compile(ctx, `1 + 2`)
	require.ErrorIs(t, err, fault.ErrExecutionCancelled, "a cancelled context stops execution")
}

func TestExecuteEntrypointCancelledContext(t *testing.T) {
	t.Parallel()
	service := NewService()

	source := `package main
func entrypoint() int { return 42 }
`
	cfs, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoError(t, err)

	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(errors.New("test: pre-cancelled entrypoint context"))

	_, err = service.ExecuteEntrypoint(ctx, cfs, "entrypoint")
	require.ErrorIs(t, err, fault.ErrExecutionCancelled, "a cancelled context stops execution")
}

func TestMaxExecutionTime(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxExecutionTime(100 * time.Millisecond))
	service.UseSymbols(newOSSymbols())

	_, err := service.Eval(t.Context(), `
import (
	"os"
)
x := 0
for {
	x++
	os.Getenv("PATH")
}
`)
	require.ErrorIs(t, err, context.DeadlineExceeded, "the deadline stops execution")
}

func TestEvalCancelDuringExecution(t *testing.T) {
	t.Parallel()
	service := NewService()
	service.UseSymbols(newOSSymbols())

	ctx, cancel := context.WithCancelCause(t.Context())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel(errors.New("test: cancel during execution"))
	}()

	_, err := service.Eval(ctx, `
import (
	"os"
)
x := 0
for {
	x++
	os.Getenv("PATH")
}
`)
	require.ErrorIs(t, err, context.Canceled, "cancellation stops execution")
}

func TestCompileDefersGlobalInitialisation(t *testing.T) {
	t.Parallel()
	var output bytes.Buffer
	service := NewService(WithSafeMode(), WithStderr(&output))
	compiled, err := service.Compile(t.Context(), `
func unused() {}
var value = func() int {
	println("initialised")
	return 7
}()
value
`)
	require.NoError(t, err)
	require.Empty(t, output.String(), "compilation runs no script code")
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = service.Execute(cancelled, compiled)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 0, output.Len(), "a cancelled execution runs no initialiser")
	for iteration := 1; iteration <= 2; iteration++ {
		result, err := service.Execute(t.Context(), compiled)
		require.NoError(t, err)
		require.Equal(t, "7", fmt.Sprint(result))
		require.Equal(t, iteration, bytes.Count(output.Bytes(), []byte("initialised")), "the initialiser runs once per execution")
	}
}

func TestEntrypointKeepsStringsVariableInitialisersBuilt(t *testing.T) {
	t.Parallel()
	const entrypoint = `
func run() string {
	other := string(host.Bytes("XXXX"))
	return value + other[:0]
}`
	tests := []struct {
		name   string
		source string
	}{
		{name: "a conversion in the initialiser", source: `var value = string(host.Bytes("kept"))`},
		{name: "a conversion in a function the initialiser calls", source: `var value = load()
func load() string { data := host.Bytes("kept"); return string(data) }`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			service := NewService()
			service.UseSymbols(symtab.NewSymbolRegistry(symtab.SymbolExports{"host": {
				"Bytes": reflect.ValueOf(func(s string) []byte { return []byte(s) }),
			}}))
			cfs, err := service.CompileFileSet(t.Context(), map[string]string{
				"main.go": "package main\nimport \"host\"\n" + tt.source + "\n" + entrypoint,
			})
			require.NoError(t, err)

			value, err := service.ExecuteEntrypoint(t.Context(), cfs, "run")

			require.NoError(t, err)
			require.Equal(t, "kept", value, "the global must not point into the initialiser's released arena")
		})
	}
}

func TestCompileDefersFailingInitialiser(t *testing.T) {
	t.Parallel()
	service := NewService(WithSafeMode())
	compiled, err := service.Compile(t.Context(), `
func unused() {}
var value = func() int {
	panic("initialiser failure")
	return 7
}()
value
`)
	require.NoError(t, err, "a runtime initialiser failure does not surface during compilation")
	_, err = service.Execute(t.Context(), compiled)
	require.Error(t, err, "execution reports the initialiser failure")
}

func TestGoroutineLimitIsWiredThroughService(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxGoroutines(4))
	require.Equal(t, int32(4), service.limits.MaxGoroutines)
}

func TestErrGoroutineLimitIsExported(t *testing.T) {
	t.Parallel()
	require.Error(t, fault.ErrGoroutineLimit)
	wrapped := errors.Join(errors.New("wrapper"), fault.ErrGoroutineLimit)
	require.ErrorIs(t, wrapped, fault.ErrGoroutineLimit)
	require.Contains(t, fault.ErrGoroutineLimit.Error(), "goroutine")
}

func TestWithCapabilityHookAndForceGoDispatchCompose(t *testing.T) {
	t.Parallel()
	hook := &recordingHook{}
	service := NewService(
		WithCapabilityHook(hook),
		WithForceGoDispatch(),
		WithMaxGoroutines(8),
		WithMaxCallDepth(1024),
	)
	require.Same(t, hook, service.CapabilityHook(), "the hook survives the rest of the option list")
	require.Equal(t, int32(8), service.limits.MaxGoroutines)
	require.Equal(t, 1024, service.limits.MaxCallDepth)
}

func TestEvalAndSessionsReportSeveralTypeErrors(t *testing.T) {
	t.Parallel()

	const snippet = "x := missingOne\ny := missingTwo\n_, _ = x, y\n"
	_, err := NewService().Eval(t.Context(), snippet)
	require.ErrorIs(t, err, fault.ErrTypeCheck)
	require.Contains(t, err.Error(), "missingOne")
	require.Contains(t, err.Error(), "missingTwo")

	_, err = NewService().NewSession().Submit(t.Context(), snippet)
	require.ErrorIs(t, err, fault.ErrTypeCheck)
	require.Contains(t, err.Error(), "missingTwo")
}

func initsRunCount(service *Service) int {
	service.initsMu.Lock()
	defer service.initsMu.Unlock()
	return len(service.initsRun)
}

func TestExecuteInitsDoesNotKeepTheFileSetAlive(t *testing.T) {
	service := newTestService(t)
	collected := make(chan struct{})
	func() {
		compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": varInitReuseSource})
		require.NoError(t, err)
		require.NoError(t, service.ExecuteInits(t.Context(), compiled))
		require.Equal(t, 1, initsRunCount(service))
		runtime.AddCleanup(compiled, func(done chan struct{}) { close(done) }, collected)
	}()

	require.Eventually(t, func() bool {
		runtime.GC()
		select {
		case <-collected:
			return true
		default:
			return false
		}
	}, 5*time.Second, 10*time.Millisecond, "a file set ExecuteInits ran and nothing else holds is collected")
	require.Eventually(t, func() bool {
		runtime.GC()
		return initsRunCount(service) == 0
	}, 5*time.Second, 10*time.Millisecond, "its record goes with it")
}

func TestExecuteInitsRecordIsTakenOnce(t *testing.T) {
	service := newTestService(t)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": varInitReuseSource})
	require.NoError(t, err)

	require.False(t, service.takeInitsRun(compiled), "nothing ran yet")
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	require.True(t, service.takeInitsRun(compiled), "the next entrypoint skips the initialisers")
	require.False(t, service.takeInitsRun(compiled), "only once")
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	require.True(t, service.takeInitsRun(compiled), "a second ExecuteInits records again")
}

func TestServiceCloseForgetsExecuteInitsRecords(t *testing.T) {
	service := newTestService(t)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": varInitReuseSource})
	require.NoError(t, err)
	require.NoError(t, service.ExecuteInits(t.Context(), compiled))
	require.Equal(t, 1, initsRunCount(service))

	require.NoError(t, service.Close())

	require.Zero(t, initsRunCount(service), "Close drops every record")
	require.False(t, service.takeInitsRun(compiled))
	runtime.KeepAlive(compiled)
}

func TestEvalOfACallWithNoResultRunsItAsAStatement(t *testing.T) {
	t.Parallel()
	var stderr bytes.Buffer
	service := NewService(WithStderr(&stderr))

	result, err := service.Eval(t.Context(), `println("x")`)
	require.NoError(t, err)
	require.Nil(t, result, "a call with no result evaluates to nil")
	require.Equal(t, "x\n", stderr.String())

	_, err = service.Eval(t.Context(), `println(undefinedName)`)
	require.ErrorIs(t, err, fault.ErrTypeCheck, "a call that is wrong as a statement too still fails")
}

type programTestSymbols struct{}

func (programTestSymbols) Exports() symtab.SymbolExports {
	return symtab.SymbolExports{
		"fmt": {"Sprint": reflect.ValueOf(fmt.Sprint)},
		"runtime": {
			"GC":           reflect.ValueOf(runtime.GC),
			"SetFinalizer": reflect.ValueOf(runtime.SetFinalizer),
		},
		"time": {
			"After":       reflect.ValueOf(time.After),
			"Duration":    reflect.ValueOf((*time.Duration)(nil)),
			"Millisecond": reflect.ValueOf(time.Millisecond),
			"Second":      reflect.ValueOf(time.Second),
		},
	}
}

func compileTestProgram(t *testing.T, source string, options ...Option) (*Service, *program.CompiledFileSet) {
	t.Helper()
	service := NewService(options...)
	require.NoError(t, service.UseSymbolProviders(programTestSymbols{}))
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoError(t, err)
	return service, compiled
}

const finaliserProgramSource = `package main

import (
	"fmt"
	"runtime"
	"time"
)

type node struct {
	id   int64
	next *node
}

type tail struct {
	id   int64
	name string
}

func refused(call func()) (message string) {
	defer func() { message = fmt.Sprint(recover()) }()
	call()
	return "accepted"
}

func Misuse() string {
	x := &node{id: 1}
	runtime.SetFinalizer(x, func(*node) {})
	t := &tail{id: 2}
	var missing *node
	results := []string{
		refused(func() { runtime.SetFinalizer(nil, nil) }),
		refused(func() { runtime.SetFinalizer(5, nil) }),
		refused(func() { runtime.SetFinalizer(missing, func(*node) {}) }),
		refused(func() { runtime.SetFinalizer(&node{}, 42) }),
		refused(func() { runtime.SetFinalizer(&node{}, func(*int) {}) }),
		refused(func() { runtime.SetFinalizer(&t.name, func(*string) {}) }),
		refused(func() { runtime.SetFinalizer(x, func(*node) {}) }),
	}
	runtime.SetFinalizer(x, nil)
	return fmt.Sprint(results)
}

func await(done chan int64, want int) int {
	got := 0
	limit := time.After(20 * time.Second)
	for got < want {
		runtime.GC()
		select {
		case <-done:
			got++
		case <-limit:
			return got
		case <-time.After(10 * time.Millisecond):
		}
	}
	return got
}

func panicking(done chan int64) {
	x := &node{id: 1}
	runtime.SetFinalizer(x, func(*node) { panic("finaliser panic") })
	z := &node{id: 3}
	runtime.SetFinalizer(z, func(p *node) { done <- p.id })
}

func Panicking() int {
	done := make(chan int64, 1)
	panicking(done)
	return await(done, 1)
}

type pair struct {
	id   int64
	name string
}

func registerEach(done chan int64) {
	a := new([4]int64)
	runtime.SetFinalizer(a, func(*[4]int64) { done <- 1 })
	b := new(pair)
	runtime.SetFinalizer(b, func(*pair) { done <- 2 })
	c := &pair{id: 3, name: "c"}
	runtime.SetFinalizer(c, func(*pair) { done <- 3 })
	d := make([]*pair, 4)
	runtime.SetFinalizer(&d[0], func(**pair) { done <- 4 })
	e := make([]int64, 4)
	runtime.SetFinalizer(&e[2], func(*int64) { done <- 5 })
	f := &pair{id: 6}
	runtime.SetFinalizer(&f.id, func(*int64) { done <- 6 })
}

func ValidUses() int {
	done := make(chan int64, 6)
	registerEach(done)
	return await(done, 6)
}

func looping(done chan int64) {
	x := &node{id: 1}
	runtime.SetFinalizer(x, func(*node) {
		for {
		}
	})
	y := &node{id: 2}
	runtime.SetFinalizer(y, func(p *node) { done <- p.id })
}

func Looping() int {
	done := make(chan int64, 1)
	looping(done)
	return await(done, 1)
}
`

func compileFinaliserProgram(t *testing.T, options ...Option) (*Service, *program.CompiledFileSet) {
	t.Helper()
	return compileTestProgram(t, finaliserProgramSource, options...)
}

func TestAProgramSeesGosMessageForEachRefusedSetFinalizer(t *testing.T) {
	service, compiled := compileFinaliserProgram(t)
	result, err := service.CallFunction(t.Context(), compiled, "Misuse")
	require.NoError(t, err)
	require.Equal(t, "["+
		"runtime.SetFinalizer: first argument is nil "+
		"runtime.SetFinalizer: first argument is int, not pointer "+
		"runtime.SetFinalizer: pointer not in allocated block "+
		"runtime.SetFinalizer: second argument is int, not a function "+
		"runtime.SetFinalizer: cannot pass *main.node to finalizer func(*int) "+
		"runtime.SetFinalizer: pointer not at beginning of allocated block "+
		"runtime.SetFinalizer: finalizer already set]", fmt.Sprint(result))
}

func TestEveryFinaliserGoAcceptsRuns(t *testing.T) {
	service, compiled := compileFinaliserProgram(t)
	result, err := service.CallFunction(t.Context(), compiled, "ValidUses")
	require.NoError(t, err, "new([N]T), new(T), &T{}, a made slice's first element and small pointer-free elements or fields all take finalisers in Go")
	require.Equal(t, "6", fmt.Sprint(result))
}

func TestAPanickingFinaliserDoesNotEndTheProcess(t *testing.T) {
	service, compiled := compileFinaliserProgram(t)
	result, err := service.CallFunction(t.Context(), compiled, "Panicking")
	require.NoError(t, err)
	require.Equal(t, "1", fmt.Sprint(result), "the finaliser after the panicking one still runs")
}

func TestALoopingFinaliserStopsAtTheDeadline(t *testing.T) {
	service, compiled := compileFinaliserProgram(t, WithCallbackTimeout(200*time.Millisecond))
	result, err := service.CallFunction(t.Context(), compiled, "Looping")
	require.NoError(t, err)
	require.Equal(t, "1", fmt.Sprint(result), "the finaliser queued behind a looping one runs once the loop is stopped")
}

const promotedMethodSetSource = `package main

import (
	"fmt"
	"reflect"
	"strings"
)

type Inner struct{ V int }

func (i Inner) Hello() string { return fmt.Sprint("hello ", i.V) }

func (*Inner) PtrOnly() string { return "ptr" }

type Outer struct {
	Inner
	N int
}

func (Outer) Own() string { return "own" }

type ViaPointer struct {
	*Inner
}

type Left struct{}

func (Left) Clash() string { return "left" }

func (Left) Deep() string { return "deep" }

type Right struct{}

func (Right) Clash() string { return "right" }

type Middle struct{ Left }

type Ambiguous struct {
	Middle
	Right
	Deep int
}

type Clashing struct {
	Left
	Right
}

type Builder struct {
	*strings.Builder
}

func names(t reflect.Type) string {
	var out []string
	for i := 0; i < t.NumMethod(); i++ {
		out = append(out, t.Method(i).Name)
	}
	return fmt.Sprint(t.NumMethod(), out)
}

func EntrypointRun() string {
	outer := Outer{Inner: Inner{V: 7}}
	var lines []string
	lines = append(lines, "T "+names(reflect.TypeOf(outer)))
	lines = append(lines, "*T "+names(reflect.TypeOf(&outer)))
	lines = append(lines, "VP "+names(reflect.TypeOf(ViaPointer{})))
	lines = append(lines, "AM "+names(reflect.TypeOf(Ambiguous{})))
	lines = append(lines, "CL "+names(reflect.TypeOf(Clashing{})))
	value := reflect.ValueOf(outer)
	hello := value.MethodByName("Hello")
	lines = append(lines, fmt.Sprint("valid ", hello.IsValid(), " ", value.NumMethod()))
	if hello.IsValid() {
		lines = append(lines, fmt.Sprint("call ", hello.Call(nil)[0].Interface()))
	}
	lines = append(lines, fmt.Sprint("index ", value.Method(0).Call(nil)[0].Interface()))
	method, ok := reflect.TypeOf(&outer).MethodByName("PtrOnly")
	lines = append(lines, fmt.Sprint("ptr ", ok, " ", method.Func.Call([]reflect.Value{reflect.ValueOf(&outer)})[0].Interface()))
	builder := Builder{Builder: &strings.Builder{}}
	write := reflect.ValueOf(builder).MethodByName("WriteString")
	write.Call([]reflect.Value{reflect.ValueOf("built")})
	lines = append(lines, fmt.Sprint("native ", builder.String(), " ", reflect.TypeOf(builder).NumMethod() > 0))
	lines = append(lines, fmt.Sprint("nil ", callPanics(reflect.ValueOf(ViaPointer{}).MethodByName("Hello"))))
	return strings.Join(lines, "\n")
}

func callPanics(method reflect.Value) (panicked bool) {
	defer func() { panicked = recover() != nil }()
	method.Call(nil)
	return false
}
`

func TestReflectListsAndCallsPromotedMethodsAsGoDoes(t *testing.T) {
	t.Parallel()

	service := NewService()
	service.UseSymbols(symtab.NewSymbolRegistry(symtab.SymbolExports{
		"fmt":     {"Sprint": reflect.ValueOf(fmt.Sprint)},
		"reflect": {"TypeOf": reflect.ValueOf(reflect.TypeOf), "ValueOf": reflect.ValueOf(reflect.ValueOf), "Type": reflect.ValueOf((*reflect.Type)(nil)), "Value": reflect.ValueOf((*reflect.Value)(nil))},
		"strings": {"Join": reflect.ValueOf(strings.Join), "Builder": reflect.ValueOf((*strings.Builder)(nil))},
	}))
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": promotedMethodSetSource})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(t.Context(), compiled, "EntrypointRun")
	require.NoError(t, err)
	require.Equal(t, strings.Join([]string{
		"T 2 [Hello Own]",
		"*T 3 [Hello Own PtrOnly]",
		"VP 2 [Hello PtrOnly]",
		"AM 1 [Clash]",
		"CL 1 [Deep]",
		"valid true 2",
		"call hello 7",
		"index hello 7",
		"ptr true ptr",
		"native built true",
		"nil true",
	}, "\n"), result)
}

const nativeImplementsSource = `package main

import (
	"fmt"
	"reflect"
	"strings"
)

type Namer interface{ Name() string }

type Lener interface{ Len() int }

type T struct{}

func (T) Name() string { return "t" }

func EntrypointRun() string {
	namer := reflect.TypeOf((*Namer)(nil)).Elem()
	lener := reflect.TypeOf((*Lener)(nil)).Elem()
	builder := reflect.TypeOf(&strings.Builder{})
	return fmt.Sprint(namer.NumMethod(), reflect.TypeOf(T{}).Implements(namer), reflect.TypeOf(1).Implements(namer),
		reflect.TypeOf("").Implements(namer), builder.Implements(lener), builder.Implements(namer))
}
`

func TestANativeTypeImplementsAScriptInterfaceOnlyWithItsMethods(t *testing.T) {
	t.Parallel()

	service := NewService()
	service.UseSymbols(symtab.NewSymbolRegistry(symtab.SymbolExports{
		"fmt":     {"Sprint": reflect.ValueOf(fmt.Sprint)},
		"reflect": {"TypeOf": reflect.ValueOf(reflect.TypeOf), "Type": reflect.ValueOf((*reflect.Type)(nil))},
		"strings": {"Builder": reflect.ValueOf((*strings.Builder)(nil))},
	}))
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": nativeImplementsSource})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(t.Context(), compiled, "EntrypointRun")
	require.NoError(t, err)
	require.Equal(t, "1 true false false true false", result,
		"a native type implements a script interface only when it has the interface's methods")
}

const recursionChildEnvironment = "PIPIT_ENGINE_RECURSION_CHILD"

const recursionChildMemoryLimit = 1 << 30

const tailRecursionSource = "package main\n\nfunc f(n int) int { return f(n + 1) }\n\nfunc run() int { return f(0) }\n"

var recursionSources = map[string]string{
	"named":        "package main\n\nfunc f(n int) int { return f(n+1) + 1 }\n\nfunc run() int { return f(0) }\n",
	"closure":      "package main\n\nfunc run() int {\n\tvar f func(int) int\n\tf = func(n int) int { return f(n+1) + 1 }\n\treturn f(0)\n}\n",
	"closure tail": "package main\n\nfunc run() int {\n\tvar f func(int) int\n\tf = func(n int) int { return f(n + 1) }\n\treturn f(0)\n}\n",
	"method value": "package main\n\ntype t struct{}\n\nfunc (r t) f(n int) int { g := r.f; return g(n+1) + 1 }\n\nfunc run() int { return t{}.f(0) }\n",
}

func runRecursionChild(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeoutCause(t.Context(), 2*time.Minute, errors.New("recursion child ran too long"))
	defer cancel()
	command := exec.CommandContext(ctx, os.Args[0], "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.v", "-test.count=1")
	command.Env = append(os.Environ(), recursionChildEnvironment+"="+t.Name())
	output, err := command.CombinedOutput()
	require.NoError(t, err, "the recursion child failed:\n%s", output)
	return string(output)
}

func inRecursionChild(t *testing.T) bool {
	t.Helper()
	if os.Getenv(recursionChildEnvironment) != t.Name() {
		return false
	}
	debug.SetMemoryLimit(recursionChildMemoryLimit)
	return true
}

func TestEveryCallRouteHonoursTheCallDepthLimit(t *testing.T) {
	if !inRecursionChild(t) {
		runRecursionChild(t)
		return
	}
	for name, source := range recursionSources {
		if name == "closure tail" {
			continue
		}
		t.Run(name, func(t *testing.T) {
			options := []Option{WithMaxCallDepth(64), WithMaxExecutionTime(2 * time.Second)}
			if os.Getenv("PIPIT_ENGINE_RECURSION_DEFAULT_DEPTH") != "" {
				options = options[1:]
			}
			if os.Getenv("PIPIT_ENGINE_RECURSION_GO") != "" {
				options = append(options, WithForceGoDispatch())
			}
			service, compiled := compileTestProgram(t, source, options...)
			start := time.Now()
			_, err := service.ExecuteEntrypoint(t.Context(), compiled, "run")
			require.ErrorIs(t, err, fault.ErrStackOverflow, "%s recursion stops at the call depth limit", name)
			require.Less(t, time.Since(start), 10*time.Second)
		})
	}
}

func TestTailRecursionHonoursTheExecutionDeadline(t *testing.T) {
	if !inRecursionChild(t) {
		runRecursionChild(t)
		return
	}
	dispatches := map[string][]Option{"default": nil, "go": {WithForceGoDispatch()}}
	for _, name := range []string{"named tail", "closure tail"} {
		for dispatch, options := range dispatches {
			t.Run(name+" "+dispatch, func(t *testing.T) {
				source := tailRecursionSource
				if name == "closure tail" {
					source = recursionSources[name]
				}
				service, compiled := compileTestProgram(t, source, append(options, WithMaxExecutionTime(500*time.Millisecond))...)
				start := time.Now()
				_, err := service.ExecuteEntrypoint(t.Context(), compiled, "run")
				if name == "closure tail" {
					stopped := errors.Is(err, fault.ErrStackOverflow) || errors.Is(err, fault.ErrExecutionCancelled)
					require.True(t, stopped, "a call through a function value nests a callback run, which the depth limit or the deadline stops first: %v", err)
				} else {
					require.ErrorIs(t, err, fault.ErrExecutionCancelled, "the deadline stops a tail recursion")
				}
				require.Less(t, time.Since(start), 10*time.Second)
			})
		}
	}
}

const rangeFunctionPanicSource = `package main

import "fmt"

func again(yield func(int) bool) {
	func() {
		defer func() { recover() }()
		yield(1)
	}()
	yield(2)
}

func swallow(yield func(int) bool) {
	defer func() { recover() }()
	yield(1)
}

func run(iterator func(func(int) bool)) (trace string) {
	defer func() { trace += fmt.Sprint(" recovered: ", recover()) }()
	for x := range iterator {
		trace += fmt.Sprint("body ", x)
		if x == 1 {
			panic("boom")
		}
	}
	return trace + " ended"
}

func Again() string   { return run(again) }
func Swallow() string { return run(swallow) }
`

func TestAnIteratorMayNotCallTheBodyAgainAfterItPanicked(t *testing.T) {
	service, compiled := compileTestProgram(t, rangeFunctionPanicSource)

	result, err := service.CallFunction(t.Context(), compiled, "Again")
	require.NoError(t, err)
	require.Equal(t, "body 1 recovered: runtime error: range function continued iteration after loop body panic", fmt.Sprint(result))

	result, err = service.CallFunction(t.Context(), compiled, "Swallow")
	require.NoError(t, err)
	require.Equal(t, "body 1 recovered: runtime error: range function recovered a loop body panic and did not resume panicking", fmt.Sprint(result))
}
