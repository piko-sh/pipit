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

package facade_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit"
	"pipit.sh/pipit/sdk/stdlib"
)

func TestDeadlockOnProgramChannel(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithDeadlockGrace(50*time.Millisecond), pipit.WithMaxExecutionTime(10*time.Second))
	start := time.Now()
	_, err := interpreter.Eval(t.Context(), `
ch := make(chan int)
<-ch
`)
	require.ErrorIs(t, err, pipit.ErrDeadlock)
	require.NotErrorIs(t, err, pipit.ErrExecutionCancelled, "a deadlock must not read as a timeout")
	require.LessOrEqual(t, time.Since(start), 5*time.Second, "the deadlock is reported within 5 seconds")
}

func TestDeadlockAcrossGoroutines(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithDeadlockGrace(50*time.Millisecond), pipit.WithMaxExecutionTime(10*time.Second))
	_, err := interpreter.Eval(t.Context(), `
a := make(chan int)
b := make(chan int)
done := make(chan bool)
go func() { <-a; b <- 1; done <- true }()
go func() { <-b; a <- 1; done <- true }()
<-done
<-done
`)
	require.ErrorIs(t, err, pipit.ErrDeadlock)
}

func TestNoDeadlockWhenAGoroutineCanWake(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithDeadlockGrace(20*time.Millisecond), pipit.WithMaxExecutionTime(10*time.Second))
	result, err := interpreter.Eval(t.Context(), `
import "time"
ch := make(chan int)
go func() {
	time.Sleep(150 * time.Millisecond)
	ch <- 42
}()
first := <-ch
<-time.After(120 * time.Millisecond)
first
`)
	require.NoError(t, err)
	require.Equal(t, 42, result)
}

func TestSleepIsInterruptedByDeadline(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithMaxExecutionTime(100*time.Millisecond))
	start := time.Now()
	_, err := interpreter.Eval(t.Context(), `
import "time"
time.Sleep(30 * time.Second)
`)
	require.ErrorIs(t, err, pipit.ErrExecutionCancelled)
	require.ErrorIs(t, err, context.DeadlineExceeded, "the context's own error must stay reachable")
	require.LessOrEqual(t, time.Since(start), 5*time.Second, "the 100ms deadline stops the sleep")
}

func TestUncaughtPanicIsClassified(t *testing.T) {
	t.Parallel()
	interpreter := pipit.NewInterpreter(stdlib.WithStandardLibrary(), pipit.WithMaxExecutionTime(5*time.Second))
	_, err := interpreter.Eval(t.Context(), "panic(\"boom\")\n1")
	require.ErrorIs(t, err, pipit.ErrUncaughtPanic)
	value, ok := pipit.UncaughtPanicValue(err)
	require.True(t, ok, "the error carries the panic value")
	require.Equal(t, "boom", value)
	require.True(t, strings.HasSuffix(err.Error(), "panic: boom"), "the error text ends with the panic message")
}
