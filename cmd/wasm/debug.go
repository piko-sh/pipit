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

//go:build js && wasm

package main

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"syscall/js"
	"time"
	"unicode/utf8"

	"pipit.sh/pipit"
	"pipit.sh/pipit/internal/debug"
	"pipit.sh/pipit/internal/debug/debugview"
	"pipit.sh/pipit/sdk/stdlib"
)

const (
	// debugFile is the file name the playground source compiles under; breakpoints are keyed
	// by it.
	debugFile = "main.go"

	// debugStopWait bounds how long debugStop waits for the execution goroutine.
	debugStopWait = 2 * time.Second

	// debugEvalTimeout bounds one watch expression evaluation.
	debugEvalTimeout = time.Second

	// debugMaxBreakpoints caps the breakpoints one request may set.
	debugMaxBreakpoints = 10000

	// debugMaxLine is the largest line number a breakpoint may name.
	debugMaxLine = 1 << 20

	// debugMaxFrames caps the stack frames returned in a snapshot.
	debugMaxFrames = 64

	// debugMaxThreads caps the threads returned in a snapshot.
	debugMaxThreads = 64

	// debugMaxVariables caps the variables returned by one listing.
	debugMaxVariables = 500

	// debugMaxText caps each rendered name, value, type or message in bytes.
	debugMaxText = 1024

	// debugMaxExpression caps a watch expression in bytes.
	debugMaxExpression = 4096

	// debugMaxCount is the largest thread ID, frame index or reference accepted from
	// JavaScript.
	debugMaxCount = 1 << 31

	// microsecondsPerMillisecond converts accumulated running time to milliseconds.
	microsecondsPerMillisecond = 1000.0

	// scopeLocals names the frame-locals scope in the JavaScript API.
	scopeLocals = "locals"

	// scopeClosure names the captured-variables scope in the JavaScript API.
	scopeClosure = "closure"

	// scopeGlobals names the package-variables scope in the JavaScript API.
	scopeGlobals = "globals"

	// stateRunning means the program is executing between pauses.
	stateRunning = "running"

	// statePaused means every thread is parked and the program can be inspected.
	statePaused = "paused"

	// stateExited means the execution ended.
	stateExited = "exited"
)

var (
	// debugSessionCeiling ends a debug session that has lasted this long, paused or not,
	// releasing the browser interpreter.
	debugSessionCeiling = 15 * time.Minute

	// errNoSession is returned by debug calls made with no session in the right state.
	errNoSession = errors.New("no debug session is paused")

	// sessionMu guards currentSession.
	sessionMu sync.Mutex

	// currentSession is the one debug session a browser worker may hold.
	currentSession *debugSession

	// noDebugEvent is the event a session holds before its first pause.
	noDebugEvent pipit.DebugEvent
)

// debugSession is a program running under the debugger across several JavaScript calls.
// It holds browserBusy from start until finish so run and format are refused meanwhile.
type debugSession struct {
	// runStarted is when the current running stretch began.
	runStarted time.Time

	// exitErr is the execution error once exited.
	exitErr error

	// result is the entrypoint's return value once exited.
	result any

	// waitErr is why the last wait for a pause ended without one.
	waitErr error

	// ctx bounds the execution; cancelled by stop or the session ceiling.
	ctx context.Context

	// debugger drives the execution.
	debugger *pipit.Debugger

	// capture collects output for the whole session.
	capture *outputCapture

	// cancel cancels ctx.
	cancel context.CancelFunc

	// stopCeiling deregisters the stop the session ceiling schedules.
	stopCeiling func() bool

	// done closes when the execution goroutine returns.
	done chan struct{}

	// refs maps variable references handed to the page to the composite values they expand;
	// reset on every resume.
	refs map[int]expandable

	// state is stateRunning, statePaused or stateExited.
	state string

	// panicText is the panic the program paused at, empty otherwise.
	panicText string

	// breakpoints holds the latest breakpoint results, in request order.
	breakpoints []pipit.BreakpointResult

	// event is the latest pause.
	event pipit.DebugEvent

	// ran is the running time accumulated over completed stretches.
	ran time.Duration

	// nextRef is the last variable reference allocated.
	nextRef int

	// mu guards every field below it and serialises inspection with state changes.
	mu sync.Mutex

	// finished guards the one-time release of the session's resources.
	finished sync.Once
}

// startDebugSession compiles source, installs breakpoints and runs it until the first
// pause or exit.
//
// Takes source (string) which is the complete Go file.
// Takes lines ([]int) which are the 1-based breakpoint lines.
// Takes stopOnEntry (bool) which pauses at the first instruction of main.
//
// Returns map[string]any which is the first snapshot.
//
// Concurrency: claims browserBusy so one session runs at a time, and starts the goroutine
// that executes the program.
func startDebugSession(source string, lines []int, stopOnEntry bool) map[string]any {
	if len(source) > browserSourceLimit {
		return debugFailure("browser source exceeds 1 MiB")
	}
	if !browserBusy.CompareAndSwap(false, true) {
		return debugFailure("browser interpreter is busy")
	}

	caps := developerCaps()
	debugger := pipit.NewDebugger()
	interpreter := pipit.NewInterpreter(
		pipit.WithDebugger(debugger),
		pipit.WithMaxOutputSize(caps.maxOutput),
		pipit.WithMaxGoroutines(caps.maxGoroutines),
		pipit.WithMaxCallDepth(caps.maxCallDepth),
		stdlib.WithStandardLibrary(),
	)
	ctx, cancel := context.WithTimeout(context.Background(), debugSessionCeiling)
	session := newDebugSession(ctx, cancel, debugger, caps.maxOutput)
	session.stopCeiling = context.AfterFunc(ctx, session.expire)

	compiled, err := interpreter.CompileFileSet(ctx, map[string]string{debugFile: source})
	if err != nil {
		session.exitErr = err
		close(session.done)
		session.finish()
		return session.snapshot()
	}
	session.breakpoints = debugger.SetBreakpoints(debugFile, toBreakpoints(lines))
	debugger.SetExceptionBreakpoints([]pipit.ExceptionFilter{pipit.ExceptionFilterPanic})
	debugger.SetPauseOnEntry(stopOnEntry)

	sessionMu.Lock()
	currentSession = session
	sessionMu.Unlock()

	session.runStarted = time.Now()
	go func() {
		defer close(session.done)
		defer func() {
			if recovered := recover(); recovered != nil {
				session.exitErr = fmt.Errorf("panic in pipit debug run: %v", recovered)
			}
		}()
		session.result, session.exitErr = interpreter.ExecuteEntrypoint(ctx, compiled, "main")
	}()
	return session.waitForStop()
}

// newDebugSession builds a running session that captures output from now on.
//
// Takes cancel (context.CancelFunc) which cancels ctx.
// Takes debugger (*pipit.Debugger) which drives the execution.
// Takes maxOutput (int) which bounds the output retained over the whole session.
//
// Returns *debugSession which is in the running state.
func newDebugSession(ctx context.Context, cancel context.CancelFunc, debugger *pipit.Debugger, maxOutput int) *debugSession {
	return &debugSession{
		runStarted:  time.Time{},
		exitErr:     nil,
		result:      nil,
		waitErr:     nil,
		ctx:         ctx,
		debugger:    debugger,
		capture:     installCapture(maxOutput),
		cancel:      cancel,
		stopCeiling: nil,
		done:        make(chan struct{}),
		refs:        map[int]expandable{},
		state:       stateRunning,
		panicText:   "",
		breakpoints: nil,
		event:       noDebugEvent,
		ran:         0,
		nextRef:     0,
		mu:          sync.Mutex{},
		finished:    sync.Once{},
	}
}

// waitForStop blocks until the program pauses again or exits.
//
// Returns map[string]any which is the resulting snapshot.
//
// Concurrency: blocks until the debugger pauses or the session ends; acquires s.mu
// briefly.
func (s *debugSession) waitForStop() map[string]any {
	for {
		event, err := s.debugger.WaitForPause(s.ctx)
		if err == nil && event.Reason == debug.StopReasonOtherThread {
			continue
		}
		s.mu.Lock()
		s.ran += time.Since(s.runStarted)
		if err != nil {
			s.waitErr = err
			s.mu.Unlock()
			select {
			case <-s.done:
			case <-time.After(debugStopWait):
			}
			s.finish()
			return s.snapshot()
		}
		s.state = statePaused
		s.event = event
		s.panicText = ""
		if event.Panic != nil {
			s.panicText = event.Panic.Text
		}
		s.mu.Unlock()
		return s.snapshot()
	}
}

// resume continues or steps the paused program and waits for the next stop.
//
// Takes action (string) which is continue, stepOver, stepIn or stepOut.
// Takes threadID (uint64) which selects the stepping thread; 0 means the paused one.
//
// Returns map[string]any which is the next snapshot.
//
// Concurrency: acquires s.mu to change state, then waits for the next stop without it.
func (s *debugSession) resume(action string, threadID uint64) map[string]any {
	s.mu.Lock()
	if s.state != statePaused {
		s.mu.Unlock()
		return debugFailure(errNoSession.Error())
	}
	if threadID == 0 {
		threadID = s.event.ThreadID
	}
	var err error
	switch action {
	case "continue":
		err = s.debugger.Continue()
	case "stepOver":
		err = s.debugger.StepOver(threadID)
	case "stepIn":
		err = s.debugger.StepIn(threadID)
	case "stepOut":
		err = s.debugger.StepOut(threadID)
	default:
		err = errors.New("unknown debug action")
	}
	if err != nil {
		s.mu.Unlock()
		return debugFailure(err.Error())
	}
	s.state = stateRunning
	s.refs = map[int]expandable{}
	s.runStarted = time.Now()
	s.mu.Unlock()
	return s.waitForStop()
}

// pause asks the running program to stop at its next safe point. The pending start or
// resume call resolves with the pause.
//
// Returns map[string]any which acknowledges the request.
func (s *debugSession) pause() map[string]any {
	if err := s.debugger.Pause(); err != nil {
		return map[string]any{resultKeyOK: false, resultKeyError: err.Error()}
	}
	return map[string]any{resultKeyOK: true, resultKeyError: ""}
}

// stop ends the execution and releases the session.
//
// Returns map[string]any which is the final snapshot.
//
// Concurrency: safe for concurrent use; waits for the execution goroutine and acquires
// s.mu.
func (s *debugSession) stop() map[string]any {
	s.debugger.Stop()
	s.cancel()
	select {
	case <-s.done:
	case <-time.After(debugStopWait):
	}
	s.mu.Lock()
	if s.state == statePaused {
		s.state = stateRunning
	}
	s.mu.Unlock()
	s.finish()
	return s.snapshot()
}

// expire stops the session once the session ceiling has passed, paused or not, so an
// abandoned paused session does not keep the browser interpreter busy.
//
// It runs on the goroutine context.AfterFunc starts when the ceiling passes.
func (s *debugSession) expire() {
	if !errors.Is(s.ctx.Err(), context.DeadlineExceeded) {
		return
	}
	s.stop()
}

// setBreakpoints replaces the program's breakpoints.
//
// Takes lines ([]int) which are the 1-based lines.
//
// Returns map[string]any carrying the verified breakpoints.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *debugSession) setBreakpoints(lines []int) map[string]any {
	results := s.debugger.SetBreakpoints(debugFile, toBreakpoints(lines))
	s.mu.Lock()
	s.breakpoints = results
	s.mu.Unlock()
	return map[string]any{resultKeyOK: true, resultKeyError: "", "breakpoints": breakpointList(results)}
}

// variables lists a page of a scope of a frame, or of the children of a previously
// returned value.
//
// Takes request (variablesRequest) which names the thread, frame, scope or reference, and
// the first row of the page.
//
// Returns map[string]any carrying the page's variables and the total the listing holds.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *debugSession) variables(request variablesRequest) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != statePaused {
		return debugFailure(errNoSession.Error())
	}
	if request.ref != 0 {
		parent, ok := s.refs[request.ref]
		if !ok {
			return debugFailure("unknown variable reference")
		}
		children := debugview.ChildrenRange(parent.value, request.start, debugMaxVariables)
		elementType := elementTypeName(parent.typeName)
		list := make([]any, 0, len(children))
		for _, child := range children {
			childType := child.Type
			if elementType != "" {
				childType = elementType
			}
			list = append(list, s.renderVariable(child.Name, childType, child.Value))
		}
		return variablesResult(list, debugview.ChildCount(parent.value))
	}
	list, total, err := s.scopeVariables(s.thread(request.threadID), request.frame, request.scope, request.start)
	if err != nil {
		return debugFailure(err.Error())
	}
	return variablesResult(list, total)
}

// variablesResult is the reply to a successful variables listing.
//
// Takes list ([]any) which holds the rendered page.
// Takes total (int) which is how many rows the whole listing holds.
//
// Returns map[string]any with ok, error, variables and total.
func variablesResult(list []any, total int) map[string]any {
	return map[string]any{resultKeyOK: true, resultKeyError: "", "variables": list, "total": total}
}

// evaluate runs a side-effect-free watch expression in a frame.
//
// Takes threadID (uint64) which selects the thread; 0 means the paused one.
// Takes frame (int) which is the 0-based frame index.
// Takes expression (string) which is the Go expression.
//
// Returns map[string]any carrying the rendered value.
//
// Concurrency: safe for concurrent use; holds s.mu for the evaluation.
func (s *debugSession) evaluate(threadID uint64, frame int, expression string) map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state != statePaused {
		return debugFailure(errNoSession.Error())
	}
	if len(expression) > debugMaxExpression {
		return debugFailure("expression exceeds 4 KiB")
	}
	ctx, cancel := context.WithTimeout(s.ctx, debugEvalTimeout)
	defer cancel()
	result, err := s.debugger.Evaluate(ctx, s.thread(threadID), frame, expression, pipit.EvalOptions{Timeout: debugEvalTimeout, AllowCalls: false})
	if err != nil {
		return debugFailure(err.Error())
	}
	variable := s.renderVariable(expression, result.Type, reflect.ValueOf(result.Value))
	return map[string]any{
		resultKeyOK:    true,
		resultKeyError: "",
		"value":        variable["value"],
		"type":         variable["type"],
		"ref":          variable["ref"],
	}
}

// finish releases the session once: it restores output capture, frees the busy flag and
// forgets the session.
//
// Concurrency: safe for concurrent use; runs once and acquires s.mu, then sessionMu.
func (s *debugSession) finish() {
	s.finished.Do(func() {
		if s.stopCeiling != nil {
			s.stopCeiling()
		}
		s.cancel()
		s.capture.restore()
		s.mu.Lock()
		s.state = stateExited
		s.mu.Unlock()
		sessionMu.Lock()
		if currentSession == s {
			currentSession = nil
		}
		sessionMu.Unlock()
		browserBusy.Store(false)
	})
}

// snapshot renders the session state for the page.
//
// Returns map[string]any which always carries the same fields.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *debugSession) snapshot() map[string]any {
	stdout, stderr, truncated := s.capture.take()
	s.mu.Lock()
	defer s.mu.Unlock()

	result := map[string]any{
		resultKeyOK:     true,
		resultKeyError:  "",
		"state":         s.state,
		"reason":        "",
		"line":          0,
		"function":      "",
		"threadID":      0,
		"panic":         "",
		"result":        "",
		"stdout":        stdout,
		"stderr":        stderr,
		"truncated":     truncated,
		"durationMs":    float64(s.ran.Microseconds()) / microsecondsPerMillisecond,
		"breakpoints":   breakpointList(s.breakpoints),
		"stack":         []any{},
		"locals":        []any{},
		"threads":       []any{},
		"hitBreakpoint": false,
	}

	if s.state == stateExited {
		select {
		case <-s.done:
		default:
			result[resultKeyOK] = false
			result[resultKeyError] = "debug session did not stop in time"
			if s.waitErr != nil {
				result[resultKeyError] = "debug session did not stop in time: " + clip(s.waitErr.Error())
			}
			return result
		}
		if s.exitErr != nil && !errors.Is(s.exitErr, pipit.ErrDebuggerStop) {
			result[resultKeyOK] = false
			result[resultKeyError] = clip(s.exitErr.Error())
		}
		if s.result != nil {
			result["result"] = clip(fmt.Sprintf("%v", s.result))
		}
		return result
	}
	if s.state != statePaused {
		return result
	}

	s.addPauseDetails(result)
	return result
}

// addPauseDetails fills the pause-specific snapshot fields. The caller holds s.mu.
//
// Takes result (map[string]any) which receives the location, stack, locals and threads.
func (s *debugSession) addPauseDetails(result map[string]any) {
	event := s.event
	result["reason"] = event.Reason.String()
	result["line"] = event.Location.Line
	result["function"] = clip(event.Location.Function)
	result["threadID"] = float64(event.ThreadID)
	result["panic"] = clip(s.panicText)
	result["hitBreakpoint"] = len(event.HitBreakpointIDs) > 0

	if frames, err := s.debugger.StackTrace(event.ThreadID); err == nil {
		stack := make([]any, 0, min(len(frames), debugMaxFrames))
		for _, frame := range frames[:min(len(frames), debugMaxFrames)] {
			line := 0
			if frame.File == debugFile {
				line = frame.Line
			}
			stack = append(stack, map[string]any{"function": clip(frame.Function), "line": line})
		}
		result["stack"] = stack
	}
	if locals, _, err := s.scopeVariables(event.ThreadID, 0, scopeLocals, 0); err == nil {
		result["locals"] = locals
	}
	threads := s.debugger.Threads()
	list := make([]any, 0, min(len(threads), debugMaxThreads))
	for _, thread := range threads[:min(len(threads), debugMaxThreads)] {
		list = append(list, map[string]any{
			"id":     float64(thread.ID),
			"name":   clip(thread.Name),
			"paused": thread.State == debug.ThreadPaused,
		})
	}
	result["threads"] = list
}

// scopeVariables renders a page of one scope of a frame. The caller holds s.mu.
//
// Takes threadID (uint64) which selects the thread.
// Takes frame (int) which is the 0-based frame index.
// Takes scope (string) which is locals, closure or globals.
// Takes start (int) which is the index of the first variable in the page.
//
// Returns []any which lists the rendered variables of the page.
// Returns int which is how many variables the scope holds.
// Returns error when the scope name is unknown or the debugger query fails.
func (s *debugSession) scopeVariables(threadID uint64, frame int, scope string, start int) ([]any, int, error) {
	kind, ok := map[string]pipit.ScopeKind{
		scopeLocals:  pipit.ScopeLocals,
		scopeClosure: pipit.ScopeClosure,
		scopeGlobals: pipit.ScopeGlobals,
	}[scope]
	if !ok {
		return nil, 0, errors.New("unknown variable scope")
	}
	infos, err := s.debugger.Variables(threadID, frame, kind)
	if err != nil {
		return nil, 0, err
	}
	low := min(start, len(infos))
	high := min(len(infos), low+debugMaxVariables)
	list := make([]any, 0, high-low)
	for _, info := range infos[low:high] {
		typeName := info.Type
		if typeName == "" {
			typeName = info.Kind
		}
		list = append(list, s.renderVariable(info.Name, typeName, reflect.ValueOf(info.Value)))
	}
	return list, len(infos), nil
}

// renderVariable turns a value into a bounded, JavaScript-safe row, allocating a
// reference when it can be expanded. The caller holds s.mu.
//
// Takes name (string) which labels the value.
// Takes typeName (string) which is the declared type, or empty to use the dynamic type.
// Takes value (reflect.Value) which holds the value.
//
// Returns map[string]any with name, value, type, ref (0 for leaves) and children.
func (s *debugSession) renderVariable(name, typeName string, value reflect.Value) map[string]any {
	if typeName == "" && value.IsValid() {
		typeName = debugview.TypeName(value.Type())
	}
	ref := 0
	children := debugview.ChildCount(value)
	if children > 0 {
		s.nextRef++
		ref = s.nextRef
		s.refs[ref] = expandable{value: debugview.Deref(value), typeName: typeName}
	}
	return map[string]any{
		"name":     clip(name),
		"value":    clip(debugview.LeafNamed(value, typeName)),
		"type":     clip(typeName),
		"ref":      ref,
		"children": children,
	}
}

// thread resolves a thread ID, defaulting to the paused thread. The caller holds s.mu.
//
// Takes threadID (uint64) which is the requested thread or 0.
//
// Returns uint64 which is the thread to query.
func (s *debugSession) thread(threadID uint64) uint64 {
	if threadID == 0 {
		return s.event.ThreadID
	}
	return threadID
}

// expandable is a composite value the page may expand, with its declared type.
type expandable struct {
	// value is the dereferenced composite.
	value reflect.Value

	// typeName is the declared type, used to name the elements.
	typeName string
}

// elementTypeName returns the element type named by a declared slice, array, map or
// pointer-to-one type, such as int for []int or string for map[int]string. The
// interpreter stores some elements in a wider runtime type, so the declared name reads
// better than the reflect one.
//
// Takes typeName (string) which is the declared type.
//
// Returns string which is the element type, or empty when typeName is none of those.
func elementTypeName(typeName string) string {
	typeName = strings.TrimPrefix(typeName, "*")
	switch {
	case strings.HasPrefix(typeName, "[]"):
		return typeName[2:]
	case strings.HasPrefix(typeName, "["):
		if closing := strings.IndexByte(typeName, ']'); closing > 0 {
			return typeName[closing+1:]
		}
	case strings.HasPrefix(typeName, "map["):
		return mapValueTypeName(typeName)
	}
	return ""
}

// mapValueTypeName returns V from a declared map[K]V, skipping any brackets inside K.
//
// Takes typeName (string) which starts with "map[".
//
// Returns string which is the value type, or empty when the brackets never close.
func mapValueTypeName(typeName string) string {
	depth := 0
	for index := len("map"); index < len(typeName); index++ {
		switch typeName[index] {
		case '[':
			depth++
		case ']':
			depth--
		}
		if depth == 0 {
			return typeName[index+1:]
		}
	}
	return ""
}

// toBreakpoints converts line numbers to debugger breakpoints.
//
// Takes lines ([]int) which are the 1-based lines.
//
// Returns []pipit.Breakpoint which has one entry per line.
func toBreakpoints(lines []int) []pipit.Breakpoint {
	breakpoints := make([]pipit.Breakpoint, 0, len(lines))
	for _, line := range lines {
		breakpoints = append(breakpoints, pipit.Breakpoint{Condition: "", HitCondition: "", Line: line})
	}
	return breakpoints
}

// breakpointList renders breakpoint results for the page.
//
// Takes results ([]pipit.BreakpointResult) which are the debugger's results.
//
// Returns []any which lists line, verified and message per breakpoint.
func breakpointList(results []pipit.BreakpointResult) []any {
	list := make([]any, 0, len(results))
	for _, result := range results {
		list = append(list, map[string]any{
			"line":     result.Line,
			"verified": result.Verified,
			"message":  clip(result.Message),
		})
	}
	return list
}

// debugFailure is the result of a debug call that could not run.
//
// Takes message (string) which explains why.
//
// Returns map[string]any with ok false and the message.
func debugFailure(message string) map[string]any {
	return map[string]any{resultKeyOK: false, resultKeyError: clip(message)}
}

// clip bounds text to debugMaxText bytes without splitting a UTF-8 sequence.
//
// Takes text (string) which is the text to bound.
//
// Returns string which is at most debugMaxText bytes.
func clip(text string) string {
	if len(text) <= debugMaxText {
		return text
	}
	cut := debugMaxText
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "…"
}

// activeSession returns the current debug session, if any.
//
// Returns *debugSession which is nil when no session is active.
//
// Concurrency: safe for concurrent use; acquires sessionMu.
func activeSession() *debugSession {
	sessionMu.Lock()
	defer sessionMu.Unlock()
	return currentSession
}

// jsDebugStart starts a debug session.
//
// Takes arguments ([]js.Value) where arguments[0] is the source and arguments[1] is {
// breakpoints: number[], stopOnEntry: boolean }.
//
// Returns any which is a Promise resolving to the first snapshot.
func jsDebugStart(_ js.Value, arguments []js.Value) any {
	return newPromise(func() any {
		if len(arguments) != 2 || arguments[0].Type() != js.TypeString || arguments[1].Type() != js.TypeObject || arguments[1].IsNull() {
			return debugFailure("debugStart requires a source string and an options object")
		}
		lines, err := parseLines(arguments[1].Get("breakpoints"))
		if err != nil {
			return debugFailure(err.Error())
		}
		stopOnEntry := arguments[1].Get("stopOnEntry")
		if stopOnEntry.Type() != js.TypeBoolean && stopOnEntry.Type() != js.TypeUndefined {
			return debugFailure("stopOnEntry must be a boolean")
		}
		return startDebugSession(arguments[0].String(), lines, stopOnEntry.Type() == js.TypeBoolean && stopOnEntry.Bool())
	})
}

// jsDebugResume continues or steps the paused session.
//
// Takes arguments ([]js.Value) which are the action string and the thread ID.
//
// Returns any which is a Promise resolving to the next snapshot.
func jsDebugResume(_ js.Value, arguments []js.Value) any {
	return newPromise(func() any {
		session := activeSession()
		if session == nil {
			return debugFailure(errNoSession.Error())
		}
		if len(arguments) != 2 || arguments[0].Type() != js.TypeString {
			return debugFailure("debugResume requires an action and a thread ID")
		}
		threadID, err := parseThreadID(arguments[1])
		if err != nil {
			return debugFailure(err.Error())
		}
		return session.resume(arguments[0].String(), threadID)
	})
}

// jsDebugPause asks the running session to pause.
//
// Returns any which is a Promise resolving to an acknowledgement.
func jsDebugPause(_ js.Value, _ []js.Value) any {
	return newPromise(func() any {
		session := activeSession()
		if session == nil {
			return debugFailure("no debug session is running")
		}
		return session.pause()
	})
}

// jsDebugStop ends the session.
//
// Returns any which is a Promise resolving to the final snapshot.
func jsDebugStop(_ js.Value, _ []js.Value) any {
	return newPromise(func() any {
		session := activeSession()
		if session == nil {
			return debugFailure("no debug session is running")
		}
		return session.stop()
	})
}

// jsDebugSetBreakpoints replaces the session's breakpoints.
//
// Takes arguments ([]js.Value) where arguments[0] is the array of lines.
//
// Returns any which is a Promise resolving to the verified breakpoints.
func jsDebugSetBreakpoints(_ js.Value, arguments []js.Value) any {
	return newPromise(func() any {
		session := activeSession()
		if session == nil {
			return debugFailure("no debug session is running")
		}
		if len(arguments) != 1 {
			return debugFailure("debugSetBreakpoints requires an array of lines")
		}
		lines, err := parseLines(arguments[0])
		if err != nil {
			return debugFailure(err.Error())
		}
		return session.setBreakpoints(lines)
	})
}

// jsDebugVariables lists a page of a scope or of a value's children.
//
// Takes arguments ([]js.Value) which are threadID, frame, scope, ref and an optional
// start row.
//
// Returns any which is a Promise resolving to the variable rows.
func jsDebugVariables(_ js.Value, arguments []js.Value) any {
	return newPromise(func() any {
		session := activeSession()
		if session == nil {
			return debugFailure(errNoSession.Error())
		}
		request, err := parseVariablesRequest(arguments)
		if err != nil {
			return debugFailure(err.Error())
		}
		return session.variables(request)
	})
}

// variablesRequest is a parsed debugVariables call.
type variablesRequest struct {
	// scope is locals, closure or globals; empty when ref names a value.
	scope string

	// threadID selects the thread; 0 means the paused one.
	threadID uint64

	// frame is the 0-based frame index.
	frame int

	// ref names a composite returned earlier to expand instead of a scope.
	ref int

	// start is the index of the first row of the page.
	start int
}

// parseVariablesRequest reads the arguments of a debugVariables call.
//
// Takes arguments ([]js.Value) which are threadID, frame, scope, ref and an optional
// start row (undefined meaning 0).
//
// Returns variablesRequest which holds the parsed call.
// Returns error when an argument is missing or out of range.
func parseVariablesRequest(arguments []js.Value) (variablesRequest, error) {
	var request variablesRequest
	if (len(arguments) != 4 && len(arguments) != 5) || arguments[2].Type() != js.TypeString {
		return request, errors.New("debugVariables requires threadID, frame, scope, ref and an optional start")
	}
	threadID, err := parseThreadID(arguments[0])
	if err != nil {
		return request, err
	}
	frame, err := parseCount(arguments[1], "frame")
	if err != nil {
		return request, err
	}
	ref, err := parseCount(arguments[3], "reference")
	if err != nil {
		return request, err
	}
	start := 0
	if len(arguments) == 5 && arguments[4].Type() != js.TypeUndefined {
		if start, err = parseCount(arguments[4], "start"); err != nil {
			return request, err
		}
	}
	return variablesRequest{scope: arguments[2].String(), threadID: threadID, frame: frame, ref: ref, start: start}, nil
}

// jsDebugEvaluate evaluates a watch expression without calls.
//
// Takes arguments ([]js.Value) which are threadID, frame and the expression.
//
// Returns any which is a Promise resolving to the rendered value.
func jsDebugEvaluate(_ js.Value, arguments []js.Value) any {
	return newPromise(func() any {
		session := activeSession()
		if session == nil {
			return debugFailure(errNoSession.Error())
		}
		if len(arguments) != 3 || arguments[2].Type() != js.TypeString {
			return debugFailure("debugEvaluate requires threadID, frame and an expression")
		}
		threadID, err := parseThreadID(arguments[0])
		if err != nil {
			return debugFailure(err.Error())
		}
		frame, err := parseCount(arguments[1], "frame")
		if err != nil {
			return debugFailure(err.Error())
		}
		return session.evaluate(threadID, frame, arguments[2].String())
	})
}

// parseLines reads a bounded array of breakpoint line numbers.
//
// Takes value (js.Value) which should be an array of positive integers.
//
// Returns []int which holds the lines.
// Returns error when the array is malformed or too large.
func parseLines(value js.Value) ([]int, error) {
	if value.Type() != js.TypeObject || !js.Global().Get("Array").Call("isArray", value).Bool() {
		return nil, errors.New("breakpoints must be an array of line numbers")
	}
	length := value.Length()
	if length > debugMaxBreakpoints {
		return nil, errors.New("too many breakpoints")
	}
	lines := make([]int, 0, length)
	for index := range length {
		element := value.Index(index)
		if element.Type() != js.TypeNumber {
			return nil, errors.New("breakpoint lines must be numbers")
		}
		line := element.Float()
		if line != float64(int(line)) || line < 1 || line > debugMaxLine {
			return nil, errors.New("breakpoint line out of range")
		}
		lines = append(lines, int(line))
	}
	return lines, nil
}

// parseCount reads a non-negative integer argument.
//
// Takes value (js.Value) which should be a whole number.
// Takes name (string) which names the argument in errors.
//
// Returns int which is the number.
// Returns error when the value is not a non-negative whole number.
func parseCount(value js.Value, name string) (int, error) {
	if value.Type() != js.TypeNumber {
		return 0, fmt.Errorf("%s must be a number", name)
	}
	number := value.Float()
	if number != float64(int(number)) || number < 0 || number > debugMaxCount {
		return 0, fmt.Errorf("%s out of range", name)
	}
	return int(number), nil
}

// parseThreadID reads a thread ID argument.
//
// Takes value (js.Value) which should be a non-negative whole number.
//
// Returns uint64 which is the thread ID, 0 meaning the paused thread.
// Returns error when the value is not a valid thread ID.
func parseThreadID(value js.Value) (uint64, error) {
	if _, err := parseCount(value, "thread ID"); err != nil {
		return 0, err
	}
	return uint64(value.Float()), nil
}
