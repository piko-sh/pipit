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

//go:build integration

package bytecode_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/debug"
)

func waitPause(t *testing.T, dbg *debug.Debugger) debug.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	event, err := dbg.WaitForPause(ctx)
	require.NoError(t, err, "waiting for a pause")
	return event
}

func stackOf(t *testing.T, dbg *debug.Debugger, event debug.Event) []debug.StackFrame {
	t.Helper()
	frames, err := dbg.StackTrace(event.ThreadID)
	require.NoError(t, err)
	return frames
}

func localsOf(t *testing.T, dbg *debug.Debugger, event debug.Event, frameIndex int) []debug.VariableInfo {
	t.Helper()
	variables, err := dbg.Variables(event.ThreadID, frameIndex, debug.ScopeLocals)
	require.NoError(t, err)
	return variables
}
