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

package engine

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"sync/atomic"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
)

// debugPendingEventCap bounds the unconsumed event queue. Pause events are never dropped;
// when the queue is full the oldest thread lifecycle event is dropped instead, so a host
// that attaches a debugger but never reads events cannot grow the queue without bound.
const debugPendingEventCap = 4096

// debugBreakpointKey addresses a source breakpoint.
type debugBreakpointKey struct {
	// file is the source file as the compiler recorded it.
	file string

	// line is the 1-based source line.
	line int
}

// DebugBreakpoint is one source or function breakpoint with its optional condition and
// hit-count condition.
type DebugBreakpoint struct {
	// Condition is a boolean expression evaluated in the paused frame before the breakpoint
	// pauses; empty means unconditional.
	Condition string

	// HitCondition restricts which hits pause, counted over hits whose Condition held.
	HitCondition program.HitCondition

	// ID identifies the breakpoint to clients; assigned by the session when zero.
	ID int

	// HitCount is the number of times Condition has held; written on the VM goroutine under
	// the session mutex.
	HitCount int
}

// debugSessionEventKind classifies session events.
type debugSessionEventKind uint8

const (
	// DebugSessionEventPaused means a thread stopped; Pause carries where and why.
	DebugSessionEventPaused debugSessionEventKind = iota + 1

	// DebugSessionEventThreadStarted means a VM registered with the session.
	DebugSessionEventThreadStarted

	// DebugSessionEventThreadExited means a VM left the session.
	DebugSessionEventThreadExited
)

// DebugSessionEvent is one queued session event.
type DebugSessionEvent struct {
	// Pause is set for DebugSessionEventPaused.
	Pause *DebugPause

	// Thread summarises the thread the event concerns.
	Thread DebugThreadInfo

	// Kind says what happened.
	Kind debugSessionEventKind
}

// debugConditionEvaluator evaluates a breakpoint condition in the paused thread's
// innermost frame. It runs on the VM's goroutine with the session mutex released.
type debugConditionEvaluator func(thread *DebugThread, breakpoint *DebugBreakpoint) (bool, error)

// DebugSession is the engine side of a debugger, holding the breakpoint table, the
// registry of live debugged VMs, and the stop-the-world barrier they park on. One session
// is shared by every VM of every execution and is safe for concurrent use.
//
//exhaustruct:ignore
type DebugSession struct {
	// threads maps each registered VM to its debug thread.
	threads map[*VM]*DebugThread

	// trackers maps resource trackers to their registered thread count.
	trackers map[*ResourceTracker]int

	// breakpoints holds source breakpoints keyed by file and line.
	breakpoints map[debugBreakpointKey]*DebugBreakpoint

	// functionBreakpoints holds function-entry breakpoints keyed by name.
	functionBreakpoints map[string]*DebugBreakpoint

	// pending is the queue of unconsumed session events.
	pending []DebugSessionEvent

	// notify is poked when pending grows; NextEvent waits on it.
	notify chan struct{}

	// terminate is closed by Stop while threads are live and replaced once they have gone.
	terminate chan struct{}

	// quiescent is closed once every thread has parked or blocked for the current stop.
	quiescent chan struct{}

	// focus is the thread whose pause stopped the world, nil while running.
	focus *DebugThread

	// conditionEvaluator evaluates breakpoint conditions in the paused frame.
	conditionEvaluator debugConditionEvaluator

	// step holds the pending single-step state for the focus thread.
	step debugStepState

	// mu guards all mutable session state.
	mu sync.Mutex

	// nextThreadID is the monotonic counter for thread IDs.
	nextThreadID uint64

	// nextBreakpointID is the monotonic counter for breakpoint IDs.
	nextBreakpointID int

	// stopGen counts stops; a thread parks once per generation.
	stopGen uint64

	// running counts threads in debugThreadRunning.
	running int

	// active is 1 while any pause condition exists, so the hot path can skip the mutex.
	active atomic.Uint32

	// stopped is true while the world is stopped.
	stopped bool

	// terminated is true after Stop has been called.
	terminated bool

	// pauseRequested is true when a manual pause has been requested.
	pauseRequested bool

	// stopOnEntry arms the entry stop for the next entrypoint VM.
	stopOnEntry bool

	// pauseOnPanic makes every VM pause at the start of an interpreted panic.
	pauseOnPanic bool
}

// NewDebugSession creates an idle session with no breakpoints.
//
// Returns *DebugSession ready to be installed on VMLimits.Debug.
func NewDebugSession() *DebugSession {
	session := new(DebugSession)
	session.threads = make(map[*VM]*DebugThread)
	session.trackers = make(map[*ResourceTracker]int)
	session.breakpoints = make(map[debugBreakpointKey]*DebugBreakpoint)
	session.functionBreakpoints = make(map[string]*DebugBreakpoint)
	session.notify = make(chan struct{}, 1)
	session.terminate = make(chan struct{})
	return session
}

// SetBreakpoint installs or replaces the breakpoint at file:line.
//
// Takes file (string) which is the source file as compiled.
// Takes line (int) which is the 1-based line.
// Takes breakpoint (DebugBreakpoint) whose Condition and HitCondition are kept; a zero ID
// is assigned.
//
// Returns int which is the breakpoint id.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) SetBreakpoint(file string, line int, breakpoint DebugBreakpoint) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.assignBreakpointIDLocked(breakpoint)
	s.breakpoints[debugBreakpointKey{file: file, line: line}] = stored
	s.recomputeActiveLocked()
	return stored.ID
}

// ClearBreakpoint removes the breakpoint at file:line, if any.
//
// Takes file (string) which is the source file as compiled.
// Takes line (int) which is the 1-based line.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) ClearBreakpoint(file string, line int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.breakpoints, debugBreakpointKey{file: file, line: line})
	s.recomputeActiveLocked()
}

// ClearBreakpoints removes every breakpoint in file.
//
// Takes file (string) which is the source file as compiled.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) ClearBreakpoints(file string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key := range s.breakpoints {
		if key.file == file {
			delete(s.breakpoints, key)
		}
	}
	s.recomputeActiveLocked()
}

// SetFunctionBreakpoint installs or replaces a breakpoint on entry to the named function.
//
// Takes name (string) which is the function's runtime name (main.(*T).M) or table name.
// Takes breakpoint (DebugBreakpoint) whose Condition and HitCondition are kept.
//
// Returns int which is the breakpoint id.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) SetFunctionBreakpoint(name string, breakpoint DebugBreakpoint) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.assignBreakpointIDLocked(breakpoint)
	s.functionBreakpoints[name] = stored
	s.recomputeActiveLocked()
	return stored.ID
}

// ClearFunctionBreakpoints removes every function breakpoint.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) ClearFunctionBreakpoints() {
	s.mu.Lock()
	defer s.mu.Unlock()
	clear(s.functionBreakpoints)
	s.recomputeActiveLocked()
}

// SetStopOnEntry makes the next entrypoint VM pause at its first instruction.
//
// Takes enabled (bool) which arms or disarms the entry stop.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) SetStopOnEntry(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopOnEntry = enabled
	s.recomputeActiveLocked()
}

// SetPauseOnPanic makes every VM pause where an interpreted panic starts, before deferred
// functions run.
//
// Takes enabled (bool) which arms or disarms the panic pause.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) SetPauseOnPanic(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pauseOnPanic = enabled
	s.recomputeActiveLocked()
}

// SetConditionEvaluator installs the evaluator used for breakpoint conditions. Without
// one, conditional breakpoints pause with a message saying so.
//
// Takes evaluator (debugConditionEvaluator) which may be nil.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) SetConditionEvaluator(evaluator debugConditionEvaluator) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conditionEvaluator = evaluator
}

// RequestPause asks the running execution to stop at its next safe point.
//
// Returns error which is fault.ErrDebugNotRunning when no VM is registered or
// fault.ErrDebugAlreadyPaused when the world is already stopped.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) RequestPause() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.threads) == 0 {
		return fault.ErrDebugNotRunning
	}
	if s.stopped {
		return fault.ErrDebugAlreadyPaused
	}
	s.pauseRequested = true
	s.recomputeActiveLocked()
	return nil
}

// Stop ends the running execution: every registered execution context is cancelled with
// fault.ErrDebuggerStop and every parked VM wakes to return that error. A no-op when no
// VM is registered.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.threads) == 0 || s.terminated {
		return
	}
	s.terminateLocked()
}

// Resume releases the stopped world. The named thread resumes with action (a step mode or
// Continue) and every other parked thread continues.
//
// Takes threadID (uint64) which selects the thread the action applies to.
// Takes action (program.DebugAction) which is Continue, a step, or Stop.
//
// Returns error which is fault.ErrDebugNotPaused when the world is running or the thread
// is not parked, or fault.ErrDebugUnknownThread.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) Resume(threadID uint64, action program.DebugAction) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stopped {
		return fault.ErrDebugNotPaused
	}
	thread := s.threadByIDLocked(threadID)
	if thread == nil {
		return fault.ErrDebugUnknownThread
	}
	if thread.state != DebugThreadParked {
		return fault.ErrDebugNotPaused
	}
	if action == program.DebugActionStop {
		s.terminateLocked()
		return nil
	}
	s.applyActionLocked(thread, action)
	s.releaseWorldLocked()
	return nil
}

// WaitQuiescent blocks until every registered thread has parked or blocked for the
// current stop, so shared state can be inspected without racing a still-running VM.
//
// Returns error which is fault.ErrDebugNotPaused when the world is running, or ctx.Err().
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) WaitQuiescent(ctx context.Context) error {
	s.mu.Lock()
	if !s.stopped {
		s.mu.Unlock()
		return fault.ErrDebugNotPaused
	}
	done := s.quiescent
	s.mu.Unlock()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TryNextEvent returns the oldest unconsumed session event without waiting.
//
// Returns DebugSessionEvent which is the event.
// Returns bool which is false when the queue is empty.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) TryNextEvent() (DebugSessionEvent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.pending) == 0 {
		return emptyDebugSessionEvent(), false
	}
	event := s.pending[0]
	s.pending[0] = emptyDebugSessionEvent()
	s.pending = s.pending[1:]
	return event, true
}

// EventSignal returns the channel poked whenever an event is queued, so a consumer can
// wait for it alongside its own signals.
//
// Returns <-chan struct{} which carries at most one pending notification.
func (s *DebugSession) EventSignal() <-chan struct{} {
	return s.notify
}

// FocusThread returns the thread whose pause stopped the world.
//
// Returns uint64 which is the focus thread's id.
// Returns bool which is false while the world is running.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) FocusThread() (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.stopped || s.focus == nil {
		return 0, false
	}
	return s.focus.id, true
}

// Threads lists the registered threads ordered by id.
//
// Returns []DebugThreadInfo which is empty when no VM is registered.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) Threads() []DebugThreadInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	infos := make([]DebugThreadInfo, 0, len(s.threads))
	for _, thread := range s.threads {
		infos = append(infos, thread.infoLocked())
	}
	slices.SortFunc(infos, func(a, b DebugThreadInfo) int { return cmp.Compare(a.ID, b.ID) })
	return infos
}

// Thread looks up a registered thread by id.
//
// Takes threadID (uint64) which is the session-assigned id.
//
// Returns *DebugThread which is the thread.
// Returns bool which is false when no such thread is registered.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) Thread(threadID uint64) (*DebugThread, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	thread := s.threadByIDLocked(threadID)
	return thread, thread != nil
}

// Stopped reports whether the world is stopped.
//
// Returns bool which is true between a pause and the following Resume.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) Stopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

// Live reports whether any VM is registered.
//
// Returns bool which is true while an execution runs under this session.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) Live() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.threads) > 0
}

// nextEvent returns the oldest unconsumed session event, waiting for one when the queue
// is empty.
//
// Returns DebugSessionEvent which is the event.
// Returns error which is ctx.Err() when the wait was cancelled.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) nextEvent(ctx context.Context) (DebugSessionEvent, error) {
	for {
		s.mu.Lock()
		if len(s.pending) > 0 {
			event := s.pending[0]
			s.pending[0] = emptyDebugSessionEvent()
			s.pending = s.pending[1:]
			s.mu.Unlock()
			return event, nil
		}
		s.mu.Unlock()
		select {
		case <-s.notify:
		case <-ctx.Done():
			return emptyDebugSessionEvent(), ctx.Err()
		}
	}
}

// register adds vm to the session on its goroutine when it enters its first dispatch
// loop.
//
// Takes vm (*VM) which is entering run().
//
// Returns *DebugThread which is the registration.
func (s *DebugSession) register(vm *VM) *DebugThread {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextThreadID++
	thread := new(DebugThread)
	thread.vm = vm
	thread.session = s
	thread.wake = make(chan struct{}, 1)
	thread.id = s.nextThreadID
	thread.goroutineID = vm.goroutineID
	thread.kind = vm.debugKind
	thread.role = vm.debugRole
	thread.state = debugThreadRunning
	s.threads[vm] = thread
	s.running++
	if tracker := vm.Limits.Tracker; tracker != nil {
		s.trackers[tracker]++
	}
	s.appendEventLocked(DebugSessionEvent{Pause: nil, Thread: thread.infoLocked(), Kind: DebugSessionEventThreadStarted})
	return thread
}

// deregister removes a thread when its VM leaves its outermost dispatch loop.
//
// Takes thread (*DebugThread) which is leaving.
//
// Concurrency: safe for concurrent use; acquires s.mu.
func (s *DebugSession) deregister(thread *DebugThread) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if thread.state == debugThreadRunning {
		s.running--
	}
	delete(s.threads, thread.vm)
	if tracker := thread.vm.Limits.Tracker; tracker != nil {
		s.trackers[tracker]--
		if s.trackers[tracker] == 0 {
			delete(s.trackers, tracker)
		}
	}
	s.onThreadExitLocked(thread)
	info := thread.infoLocked()
	info.State = debugThreadRunning
	s.appendEventLocked(DebugSessionEvent{Pause: nil, Thread: info, Kind: DebugSessionEventThreadExited})
	s.checkQuiescentLocked()
	if len(s.threads) == 0 {
		s.resetAfterLastThreadLocked()
	}
}

// appendEventLocked queues an event for NextEvent, dropping the oldest thread lifecycle
// event when the queue is full; the mutex must be held.
//
// Takes event (DebugSessionEvent) which is queued.
func (s *DebugSession) appendEventLocked(event DebugSessionEvent) {
	if len(s.pending) >= debugPendingEventCap {
		for index, pending := range s.pending {
			if pending.Kind != DebugSessionEventPaused {
				s.pending = append(s.pending[:index], s.pending[index+1:]...)
				break
			}
		}
	}
	s.pending = append(s.pending, event)
	select {
	case s.notify <- struct{}{}:
	default:
	}
}

// assignBreakpointIDLocked copies breakpoint into the table's own record, assigning an id
// when the caller gave none; the mutex must be held.
//
// Takes breakpoint (DebugBreakpoint) which is the caller's description.
//
// Returns *DebugBreakpoint which is the stored record.
func (s *DebugSession) assignBreakpointIDLocked(breakpoint DebugBreakpoint) *DebugBreakpoint {
	stored := new(DebugBreakpoint)
	*stored = breakpoint
	stored.HitCount = 0
	if stored.ID == 0 {
		s.nextBreakpointID++
		stored.ID = s.nextBreakpointID
	}
	return stored
}

// recomputeActiveLocked sets the hot-path flag from the pause conditions; the mutex must
// be held.
func (s *DebugSession) recomputeActiveLocked() {
	active := len(s.breakpoints) > 0 || len(s.functionBreakpoints) > 0 ||
		s.step.mode != debugStepNone || s.stopped || s.terminated ||
		s.pauseRequested || s.stopOnEntry || s.pauseOnPanic
	if active {
		s.active.Store(1)
		return
	}
	s.active.Store(0)
}

// threadByIDLocked finds a registered thread; the mutex must be held.
//
// Takes threadID (uint64) which is the id.
//
// Returns *DebugThread which is nil when unknown.
func (s *DebugSession) threadByIDLocked(threadID uint64) *DebugThread {
	for _, thread := range s.threads {
		if thread.id == threadID {
			return thread
		}
	}
	return nil
}

// terminateLocked ends the execution; the mutex must be held.
func (s *DebugSession) terminateLocked() {
	if s.terminated {
		return
	}
	s.terminated = true
	close(s.terminate)
	for tracker := range s.trackers {
		if cancel := tracker.executionCancel.Load(); cancel != nil {
			(*cancel)(fault.ErrDebuggerStop)
		}
	}
	if s.stopped {
		s.releaseWorldLocked()
	}
	s.recomputeActiveLocked()
}

// resetAfterLastThreadLocked clears the per-execution state once the last VM has left, so
// the same session serves the next execution; the mutex must be held.
func (s *DebugSession) resetAfterLastThreadLocked() {
	if s.terminated {
		s.terminated = false
		s.terminate = make(chan struct{})
	}
	s.stopped = false
	s.pauseRequested = false
	s.focus = nil
	s.step = debugStepState{vm: nil, file: "", goroutineID: 0, framePointer: 0, line: 0, mode: debugStepNone}
	s.recomputeActiveLocked()
}

// emptyDebugSessionEvent is the zero event, spelled out for the exhaustive-literal check.
//
// Returns DebugSessionEvent with every field zero.
func emptyDebugSessionEvent() DebugSessionEvent {
	return DebugSessionEvent{
		Pause:  nil,
		Thread: DebugThreadInfo{functionName: "", file: "", ID: 0, GoroutineID: 0, line: 0, Kind: 0, role: 0, State: 0},
		Kind:   0,
	}
}
