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

package debug

import (
	"context"
	"errors"
	"sync"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
)

// Debugger drives breakpoints, stepping and inspection for every VM of the executions run
// under it, with breakpoint mutators, Stop, Pause and the waits safe from any goroutine.
// Inspection and resume operations are valid only while paused and return
// fault.ErrDebugNotPaused otherwise.
type Debugger struct {
	// session is the engine side: breakpoints, thread registry and the stop barrier.
	session *engine.DebugSession

	// binding is the program information the evaluator needs, nil until a service binds a
	// compiled program.
	binding *ProgramBinding

	// exitSignal is poked when exits grows.
	exitSignal chan struct{}

	// exits queues the errors of executions that ended, oldest first.
	exits []error

	// mu guards binding, exits and executions.
	mu sync.Mutex

	// executions counts open BeginExecution handles.
	executions int
}

// NewDebugger constructs a debugger. Attach it at construction with WithDebugger.
//
// Returns *Debugger ready to attach.
func NewDebugger() *Debugger {
	debugger := new(Debugger)
	debugger.session = engine.NewDebugSession()
	debugger.exitSignal = make(chan struct{}, 1)
	return debugger
}

// Session returns the engine session, which services install on the limits of every VM
// they create.
//
// Returns *engine.DebugSession which is the session.
func (d *Debugger) Session() *engine.DebugSession {
	return d.session
}

// BeginExecution marks the start of a top-level execution. Nested executions started from
// within it must not call this again; the returned function ends the execution and, once
// every open execution has ended, queues an EventExited carrying err.
//
// Takes nothing.
//
// Returns func(error) which the caller invokes with the execution's error when it ends.
//
// Concurrency: safe for concurrent use; acquires d.mu.
func (d *Debugger) BeginExecution() func(err error) {
	d.mu.Lock()
	d.executions++
	d.mu.Unlock()
	return func(err error) {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.executions--
		if d.executions > 0 {
			return
		}
		d.exits = append(d.exits, err)
		select {
		case d.exitSignal <- struct{}{}:
		default:
		}
	}
}

// WaitForEvent returns the next debugger event, waiting for one when none is queued.
//
// Returns Event which is the event.
// Returns error which is ctx.Err() when the wait was cancelled.
func (d *Debugger) WaitForEvent(ctx context.Context) (Event, error) {
	for {
		if sessionEvent, ok := d.session.TryNextEvent(); ok {
			return eventFromSession(sessionEvent), nil
		}
		if event, ok := d.takeExit(); ok {
			return event, nil
		}
		select {
		case <-d.session.EventSignal():
		case <-d.exitSignal:
		case <-ctx.Done():
			return Event{}, ctx.Err()
		}
	}
}

// WaitForPause returns the next pause event, skipping thread lifecycle events.
//
// Returns Event which is an EventPaused event.
// Returns error which is ctx.Err() when cancelled, or fault.ErrDebugExited wrapping the
// execution's error when the execution ended before pausing again.
func (d *Debugger) WaitForPause(ctx context.Context) (Event, error) {
	for {
		event, err := d.WaitForEvent(ctx)
		if err != nil {
			return Event{}, err
		}
		switch event.Kind {
		case EventPaused:
			return event, nil
		case EventExited:
			if event.Err != nil {
				return event, errors.Join(fault.ErrDebugExited, event.Err)
			}
			return event, fault.ErrDebugExited
		default:

			continue
		}
	}
}

// Continue resumes every paused thread.
//
// Returns error which is fault.ErrDebugNotPaused when nothing is paused.
func (d *Debugger) Continue() error {
	focus, ok := d.session.FocusThread()
	if !ok {
		return fault.ErrDebugNotPaused
	}
	return d.session.Resume(focus, program.DebugActionContinue)
}

// StepIn resumes with the named thread stepping to its next source line, entering calls
// and host callbacks; the other threads run free until it pauses again.
//
// Takes threadID (uint64) which selects the thread.
//
// Returns error which is fault.ErrDebugNotPaused or fault.ErrDebugUnknownThread.
func (d *Debugger) StepIn(threadID uint64) error {
	return d.session.Resume(threadID, program.DebugActionStepIn)
}

// StepOver resumes with the named thread stepping to its next source line at the same or
// a shallower call depth.
//
// Takes threadID (uint64) which selects the thread.
//
// Returns error as for StepIn.
func (d *Debugger) StepOver(threadID uint64) error {
	return d.session.Resume(threadID, program.DebugActionStepOver)
}

// StepOut resumes with the named thread running until its current function returns.
//
// Takes threadID (uint64) which selects the thread.
//
// Returns error as for StepIn.
func (d *Debugger) StepOut(threadID uint64) error {
	return d.session.Resume(threadID, program.DebugActionStepOut)
}

// Pause asks the running execution to stop at its next safe point.
//
// Returns error which is fault.ErrDebugNotRunning or fault.ErrDebugAlreadyPaused.
func (d *Debugger) Pause() error {
	return d.session.RequestPause()
}

// Stop ends the running execution with fault.ErrDebuggerStop, waking any paused thread. A
// no-op when nothing is running.
func (d *Debugger) Stop() {
	d.session.Stop()
}

// takeExit pops the oldest queued execution exit.
//
// Returns Event which is an EventExited event.
// Returns bool which is false when none is queued.
//
// Concurrency: safe for concurrent use; acquires d.mu.
func (d *Debugger) takeExit() (Event, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.exits) == 0 {
		return Event{}, false
	}
	err := d.exits[0]
	d.exits = d.exits[1:]
	return Event{
		Err:              err,
		Panic:            nil,
		HitBreakpointIDs: nil,
		ThreadName:       "",
		Message:          "",
		Location:         Location{Function: "", File: "", Line: 0, Column: 0},
		ThreadID:         0,
		GoroutineID:      0,
		Kind:             EventExited,
		Reason:           stopReasonNone,
	}, true
}

// currentBinding returns the bound program information, or nil.
//
// Returns *ProgramBinding which is the binding.
//
// Concurrency: safe for concurrent use; acquires d.mu.
func (d *Debugger) currentBinding() *ProgramBinding {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.binding
}
