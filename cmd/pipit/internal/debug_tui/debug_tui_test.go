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

package debug_tui

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
	"pipit.sh/pipit/cmd/pipit/internal/styles"
	"pipit.sh/pipit/internal/debug"
)

func newTestModel(t *testing.T) *model {
	t.Helper()
	debugger := pipit.NewDebugger()
	interpreter := pipit.NewInterpreter(pipit.WithDebugger(debugger))
	return newModel(t.Context(), interpreter, debugger, "main.go", "package main\n\nfunc main() {\n\tx := 1\n\t_ = x\n}\n", styles.For(false))
}

func TestModelAppliesDebuggerEvents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		message    eventMsg
		wantStatus string
		wantPaused bool
		wantLine   int
	}{
		{
			name: "a pause records the location and reason",
			message: eventMsg{err: nil, event: pipit.DebugEvent{
				Kind: debug.EventPaused, Reason: debug.StopReasonBreakpoint, ThreadID: 1, GoroutineID: 1,
				Location: pipit.Location{Function: "main.main", File: "main.go", Line: 4, Column: 2},
			}},
			wantStatus: "paused (breakpoint)",
			wantPaused: true,
			wantLine:   4,
		},
		{
			name: "a panic pause carries the panic text",
			message: eventMsg{err: nil, event: pipit.DebugEvent{
				Kind: debug.EventPaused, Reason: debug.StopReasonPanic, ThreadID: 1,
				Panic:    &pipit.PanicInfo{Value: "boom", Text: "boom", Type: "string"},
				Location: pipit.Location{Function: "main.main", File: "main.go", Line: 5, Column: 2},
			}},
			wantStatus: "paused (exception)",
			wantPaused: true,
			wantLine:   5,
		},
		{
			name:       "a pause of another thread is not shown",
			message:    eventMsg{err: nil, event: pipit.DebugEvent{Kind: debug.EventPaused, Reason: debug.StopReasonOtherThread, ThreadID: 2}},
			wantStatus: "not started - press [r] to run, [b NN] for breakpoint",
			wantPaused: false,
		},
		{
			name:       "the execution ending finishes the run",
			message:    eventMsg{err: nil, event: pipit.DebugEvent{Kind: debug.EventExited}},
			wantStatus: "finished",
			wantPaused: false,
		},
		{
			name:       "a failed execution reports its error",
			message:    eventMsg{err: nil, event: pipit.DebugEvent{Kind: debug.EventExited, Err: errors.New("division by zero")}},
			wantStatus: "failed: division by zero",
			wantPaused: false,
		},
		{
			name:       "a wait error stops listening",
			message:    eventMsg{err: context.Canceled, event: pipit.DebugEvent{}},
			wantStatus: "stopped: context canceled",
			wantPaused: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newTestModel(t)
			updated, _ := m.applyEvent(tt.message)
			got, ok := updated.(*model)
			require.True(t, ok, "applyEvent returned %T", updated)
			require.Equal(t, tt.wantStatus, got.runStatus, "status")
			require.Equal(t, tt.wantPaused, got.paused, "paused")
			if tt.wantPaused {
				require.Equal(t, tt.wantLine, got.location.Line, "line")
			}
			view := got.renderSource()
			if tt.wantPaused {
				require.Contains(t, view, "▶", "the paused line is marked in the source view")
			}
		})
	}
}

func TestStepCommandsNeedAPausedProgram(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	require.Nil(t, m.stepCommand("c"), "continue needs a paused program")
	m.paused = true
	m.threadID = 1
	m.stepCommand("n")
	require.Contains(t, m.runStatus, "debugger:", "a step without a paused debugger must report the debugger's error")
}

func TestBreakpointCommandFiresForAnyScriptPath(t *testing.T) {
	t.Parallel()
	debugger := pipit.NewDebugger()
	interpreter := pipit.NewInterpreter(pipit.WithDebugger(debugger))
	const source = "package main\n\nfunc main() {\n\tx := 1\n\t_ = x\n}\n"
	m := newModel(t.Context(), interpreter, debugger, "/scripts/tool.go", source, styles.For(false))
	m.commandInput = "b 4"
	m.applyCommandInput()

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = interpreter.EvalFile(ctx, source, "main")
	}()
	defer func() { debugger.Stop(); <-done }()

	event, err := debugger.WaitForPause(ctx)
	require.NoError(t, err, "a breakpoint set from /scripts/tool.go fires")
	require.Equal(t, 4, event.Location.Line)
}
