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
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/fault"
)

func newRestrictedSessionForTest(t *testing.T, config RestrictedSessionConfig) *RestrictedSession {
	t.Helper()
	session, err := NewRestrictedSession(t.Context(), config)
	require.NoError(t, err)
	t.Cleanup(session.Close)
	return session
}

func TestRestrictedSessionRetainsPrivateStateAndCopiesGrants(t *testing.T) {
	t.Parallel()
	config := RestrictedSessionConfig{}
	config.Interpreter.Imports = []string{"math"}
	session := newRestrictedSessionForTest(t, config)
	config.Interpreter.Imports[0] = "os"
	for _, source := range []string{"import \"math\"", "var value = math.Sqrt(144)"} {
		_, err := session.submit(t.Context(), source)
		require.NoError(t, err)
	}
	result, err := session.submit(t.Context(), "value + 30")
	require.NoError(t, err)
	require.Equal(t, "42", string(result.Value))
	other := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
	_, err = other.submit(t.Context(), "value")
	require.Error(t, err, "sessions do not share state")
}

func TestRestrictedSessionFailureIsTerminal(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		source string
	}{
		{name: "os import", source: "import \"os\""},
		{name: "goroutine", source: "go func() {}()"},
		{name: "channel", source: "make(chan int)"},
		{name: "panic", source: "panic(\"failed\")"},
		{name: "slice result", source: "[]int{1, 2}"},
		{name: "undefined name", source: "not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			session := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
			_, err := session.submit(t.Context(), tt.source)
			require.Error(t, err, "an invalid submission is rejected")
			result, err := session.submit(t.Context(), "42")
			require.Error(t, err, "a failed session cannot be reused")
			require.Nil(t, result.Value)
		})
	}
}

func TestRestrictedSessionCumulativeSourceAndSubmissionLimits(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		config RestrictedSessionConfig
	}{
		{name: "source", config: RestrictedSessionConfig{MaxCumulativeSourceBytes: 2}},
		{name: "submissions", config: RestrictedSessionConfig{MaxSubmissions: 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			session := newRestrictedSessionForTest(t, tt.config)
			_, err := session.submit(t.Context(), "42")
			require.NoError(t, err)
			_, err = session.submit(t.Context(), "1")
			require.ErrorIs(t, err, ErrRestrictedLimit, "the cumulative budget carries across submissions")
		})
	}
}

func TestRestrictedSessionCumulativeOutputLimit(t *testing.T) {
	t.Parallel()
	config := RestrictedSessionConfig{}
	config.MaxCumulativeOutputBytes = 6
	session := newRestrictedSessionForTest(t, config)
	first, err := session.submit(t.Context(), "print(\"abcd\")")
	require.NoError(t, err)
	require.Equal(t, "abcd", first.Output)
	second, err := session.submit(t.Context(), "print(\"abcd\")")
	require.Error(t, err, "the output budget carries across submissions")
	require.Equal(t, "ab", second.Output)
	require.True(t, second.OutputTruncated)
	require.Nil(t, second.Value)
	_, err = session.submit(t.Context(), "42")
	require.Error(t, err, "an output failure ends the session")
}

func TestRestrictedSessionCompilationBoundary(t *testing.T) {
	t.Parallel()
	session := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
	refused := errors.New("host refused execution")
	result, err := SubmitRestrictedSessionPhased(t.Context(), session,
		"var value = func() int { println(42); return 42 }()\nvalue",
		func() error {
			assert.Zero(t, session.output.buffer.Len(), "no initialiser runs during compilation")
			return refused
		})
	require.ErrorIs(t, err, refused, "a phase refusal stops the submission")
	require.Empty(t, result.Output)
	require.Nil(t, result.Value)
	_, err = session.submit(t.Context(), "42")
	require.ErrorIs(t, err, refused, "a refused session cannot be reused")
}

func TestRestrictedSessionCostIncludesInitialisers(t *testing.T) {
	t.Parallel()
	source := "var value = func() int { println(1); return 40 }()\nfunc init() { value++ }\nvalue+1"
	probe := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
	first, err := probe.submit(t.Context(), source)
	require.NoError(t, err, "the cost is reported")
	require.Greater(t, first.CostUsed, int64(1))
	second, err := probe.submit(t.Context(), "value+1")
	require.NoError(t, err, "initialisers are charged")
	require.Positive(t, second.CostUsed)
	require.Greater(t, first.CostUsed, second.CostUsed)
	config := RestrictedSessionConfig{}
	config.costBudget = first.CostUsed + second.CostUsed - 1
	session := newRestrictedSessionForTest(t, config)
	_, err = session.submit(t.Context(), source)
	require.NoError(t, err)
	_, err = session.submit(t.Context(), "value+1")
	require.ErrorIs(t, err, fault.ErrCostBudgetExceeded, "the cumulative cost budget carries across submissions")
	config.costBudget = first.CostUsed - 1
	session = newRestrictedSessionForTest(t, config)
	_, err = session.submit(t.Context(), source)
	require.ErrorIs(t, err, fault.ErrCostBudgetExceeded, "the cost budget is shared across stages")
}

func TestRestrictedSessionSingleFlightAndTerminalClose(t *testing.T) {
	t.Parallel()
	session := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := SubmitRestrictedSessionPhased(t.Context(), session, "42", func() error {
			close(entered)
			<-release
			return nil
		})
		done <- err
	}()
	<-entered
	_, busyErr := session.submit(t.Context(), "1")
	session.Close()
	close(release)
	require.ErrorIs(t, busyErr, ErrRestrictedBusy, "a concurrent submission is refused")
	require.ErrorIs(t, <-done, errRestrictedSessionClosed, "close ends the session")
	session.mutex.Lock()
	defer session.mutex.Unlock()
	require.Nil(t, session.session, "a closed session releases its interpreter state")
	require.Nil(t, session.output)
}

func TestRestrictedSessionExpiryAndCallerCancellation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		config       RestrictedSessionConfig
		cancelCaller bool
	}{
		{name: "idle", config: RestrictedSessionConfig{IdleTimeout: 10 * time.Millisecond}},
		{name: "lifetime", config: RestrictedSessionConfig{Lifetime: 10 * time.Millisecond}},
		{name: "caller", cancelCaller: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			session := newRestrictedSessionForTest(t, tt.config)
			if tt.cancelCaller {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				_, err := session.submit(ctx, "42")
				require.ErrorIs(t, err, context.Canceled)
			} else {
				select {
				case <-session.closed:
				case <-time.After(2 * time.Second):
					require.FailNow(t, "the session expires within two seconds")
				}
			}
			_, err := session.submit(t.Context(), "42")
			require.Error(t, err, "an expired session cannot be reused")
		})
	}
}

func TestRestrictedSessionTimeoutTerminatesRunningCode(t *testing.T) {
	t.Parallel()
	config := RestrictedSessionConfig{}
	config.Interpreter.Timeout = 10 * time.Millisecond
	config.Interpreter.CostBudget = 1 << 60
	config.costBudget = 1 << 60
	session := newRestrictedSessionForTest(t, config)
	_, err := session.submit(t.Context(), "for {}")
	require.Error(t, err, "the submission timeout stops an infinite loop")
	_, err = session.submit(t.Context(), "42")
	require.Error(t, err, "a timed-out session cannot be reused")
}

func TestRestrictedSessionInvalidConfiguration(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		change func(*RestrictedSessionConfig)
	}{
		{name: "negative idle timeout", change: func(config *RestrictedSessionConfig) { config.IdleTimeout = -1 }},
		{name: "negative lifetime", change: func(config *RestrictedSessionConfig) { config.Lifetime = -1 }},
		{name: "negative cost budget", change: func(config *RestrictedSessionConfig) { config.costBudget = -1 }},
		{name: "negative source limit", change: func(config *RestrictedSessionConfig) { config.MaxCumulativeSourceBytes = -1 }},
		{name: "negative output limit", change: func(config *RestrictedSessionConfig) { config.MaxCumulativeOutputBytes = -1 }},
		{name: "negative submission limit", change: func(config *RestrictedSessionConfig) { config.MaxSubmissions = -1 }},
		{name: "os import", change: func(config *RestrictedSessionConfig) { config.Interpreter.Imports = []string{"os"} }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			config := RestrictedSessionConfig{}
			tt.change(&config)
			session, err := NewRestrictedSession(t.Context(), config)
			if err == nil {
				session.Close()
			}
			require.Error(t, err, "an invalid configuration is rejected")
		})
	}
}

func TestRestrictedSessionRejectsOversizedSource(t *testing.T) {
	t.Parallel()
	session := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
	_, err := session.submit(t.Context(), strings.Repeat("x", defaultRestrictedSourceBytes+1))
	require.ErrorIs(t, err, ErrRestrictedLimit, "oversized source is rejected")
}

func TestRestrictedSessionIdleTimerStopsDuringSubmission(t *testing.T) {
	t.Parallel()
	config := RestrictedSessionConfig{}
	config.IdleTimeout = 10 * time.Millisecond
	session := newRestrictedSessionForTest(t, config)
	_, err := SubmitRestrictedSessionPhased(t.Context(), session, "42", func() error {
		select {
		case <-session.closed:
			return errors.New("idle timer expired during active submission")
		case <-time.After(30 * time.Millisecond):
			return nil
		}
	})
	require.NoError(t, err)
	select {
	case <-session.closed:
	case <-time.After(2 * time.Second):
		require.FailNow(t, "the idle timer restarts after a submission")
	}
}

func TestRestrictedSessionLifetimeCancelsActiveSubmission(t *testing.T) {
	t.Parallel()
	config := RestrictedSessionConfig{}
	config.Lifetime = 30 * time.Millisecond
	session := newRestrictedSessionForTest(t, config)
	result, err := SubmitRestrictedSessionPhased(t.Context(), session, "println(42)", func() error {
		<-session.closed
		return nil
	})
	require.ErrorIs(t, err, errRestrictedSessionExpired, "lifetime expiry stops execution")
	require.Empty(t, result.Output)
	require.Nil(t, result.Value)
}

func TestRestrictedSessionBoundaryPanicIsTerminal(t *testing.T) {
	t.Parallel()
	session := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
	assert.Panics(t, func() {
		_, _ = SubmitRestrictedSessionPhased(t.Context(), session, "42", func() error {
			panic("boundary failure")
		})
	}, "a trusted boundary panic is not swallowed")
	_, err := session.submit(t.Context(), "42")
	require.ErrorIs(t, err, errRestrictedSessionClosed, "a panicked session cannot be reused")
}

func TestRestrictedSessionCompilationFailureDoesNotRequestExecution(t *testing.T) {
	t.Parallel()
	session := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
	called := false
	result, err := SubmitRestrictedSessionPhased(t.Context(), session,
		"var value = func() int { println(42); return 42 }()\ngo func() {}()",
		func() error { called = true; return nil })
	require.Error(t, err, "a compilation failure stops before the execution boundary")
	require.False(t, called)
	require.Empty(t, result.Output)
	require.Nil(t, result.Value)
}

func TestRestrictedSessionParentCancellationReleasesIdleState(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	session, err := NewRestrictedSession(ctx, RestrictedSessionConfig{})
	if err != nil {
		cancel()
	}
	require.NoError(t, err)
	t.Cleanup(session.Close)
	cancel()
	<-session.closed
	session.mutex.Lock()
	retained := session.session != nil || session.output != nil
	session.mutex.Unlock()
	require.False(t, retained, "cancelling the parent releases idle interpreter state")
	_, err = session.submit(t.Context(), "42")
	require.ErrorIs(t, err, context.Canceled, "parent cancellation ends the session")
}

func TestRestrictedSessionCallerCancellationAtBoundary(t *testing.T) {
	t.Parallel()
	session := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result, err := SubmitRestrictedSessionPhased(ctx, session, "println(42)", func() error {
		cancel()
		return nil
	})
	require.ErrorIs(t, err, context.Canceled, "caller cancellation stops execution")
	require.Empty(t, result.Output)
	require.Nil(t, result.Value)
}
