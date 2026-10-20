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

// Package debugtest gives debugger tests a uniform way to run the debugged execution,
// wait for its pauses and read its stacks and locals. Only test code imports it.
package debugtest

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/debug"
)

// timeout bounds every wait on the debugged execution.
const timeout = 30 * time.Second

// stopInterval is the interval between cleanup attempts to stop the debugger.
const stopInterval = 10 * time.Millisecond

// Execution is a debugged run started by Run.
type Execution struct {
	// done is closed when the run returns.
	done chan struct{}

	// value is the run's result.
	value any

	// err is the run's error.
	err error
}

// Run starts run on a new goroutine. A cleanup stops the debugger until run returns, so a
// failed assertion cannot leave the execution parked at a breakpoint.
//
// Takes tb (testing.TB) which registers cleanup and reports failures.
// Takes dbg (*debug.Debugger) which the execution runs under.
// Takes run (func() (any, error)) which executes the program.
//
// Returns *Execution which waits for the run's result.
func Run(tb testing.TB, dbg *debug.Debugger, run func() (any, error)) *Execution {
	tb.Helper()
	execution := &Execution{done: make(chan struct{}), value: nil, err: nil}
	go func() {
		defer close(execution.done)
		execution.value, execution.err = run()
	}()
	tb.Cleanup(func() { execution.stop(tb, dbg) })
	return execution
}

// Wait waits for the run to return and fails the test when it takes longer than the
// timeout.
//
// Takes tb (testing.TB) which reports a timeout failure.
//
// Returns any which is the run's result.
// Returns error which is the run's error.
func (e *Execution) Wait(tb testing.TB) (any, error) {
	tb.Helper()
	select {
	case <-e.done:
		return e.value, e.err
	case <-time.After(timeout):
		require.FailNow(tb, "the debugged execution returns within 30 seconds")
		return nil, nil
	}
}

// stop repeats Stop until the run returns.
//
// Takes tb (testing.TB) which reports a timeout failure.
// Takes dbg (*debug.Debugger) which the execution runs under.
func (e *Execution) stop(tb testing.TB, dbg *debug.Debugger) {
	tb.Helper()
	ticker := time.NewTicker(stopInterval)
	defer ticker.Stop()
	deadline := time.After(timeout)
	for {
		select {
		case <-e.done:
			return
		case <-ticker.C:
			dbg.Stop()
		case <-deadline:
			assert.Fail(tb, "the debugged execution returns once the debugger stops")
			return
		}
	}
}

// WaitPause waits for the next pause and fails the test when none comes within the
// timeout.
//
// Takes tb (testing.TB) which supplies the test context and reports failures.
// Takes dbg (*debug.Debugger) which the execution runs under.
//
// Returns debug.Event which describes the pause.
func WaitPause(tb testing.TB, dbg *debug.Debugger) debug.Event {
	tb.Helper()
	ctx, cancel := context.WithTimeout(tb.Context(), timeout)
	defer cancel()
	event, err := dbg.WaitForPause(ctx)
	require.NoError(tb, err, "the execution pauses")
	return event
}

// StackOf returns the stack of the thread that paused.
//
// Takes tb (testing.TB) which reports a failed stack lookup.
// Takes dbg (*debug.Debugger) which the execution runs under.
// Takes event (debug.Event) which names the paused thread.
//
// Returns []debug.StackFrame which lists the frames, innermost first.
func StackOf(tb testing.TB, dbg *debug.Debugger, event debug.Event) []debug.StackFrame {
	tb.Helper()
	frames, err := dbg.StackTrace(event.ThreadID)
	require.NoError(tb, err)
	return frames
}

// LocalsOf returns the local variables of one frame of the thread that paused.
//
// Takes tb (testing.TB) which reports a failed variable lookup.
// Takes dbg (*debug.Debugger) which the execution runs under.
// Takes event (debug.Event) which names the paused thread.
// Takes frameIndex (int) which selects the frame, 0 being the innermost.
//
// Returns []debug.VariableInfo which lists the frame's locals.
func LocalsOf(tb testing.TB, dbg *debug.Debugger, event debug.Event, frameIndex int) []debug.VariableInfo {
	tb.Helper()
	variables, err := dbg.Variables(event.ThreadID, frameIndex, debug.ScopeLocals)
	require.NoError(tb, err)
	return variables
}
