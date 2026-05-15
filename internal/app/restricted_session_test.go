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

	"pipit.sh/pipit/internal/fault"
)

func newRestrictedSessionForTest(t *testing.T, config RestrictedSessionConfig) *RestrictedSession {
	t.Helper()
	session, err := NewRestrictedSession(context.Background(), config)
	if err != nil {
		t.Fatal(err)
	}
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
		if _, err := session.submit(context.Background(), source); err != nil {
			t.Fatal(err)
		}
	}
	result, err := session.submit(context.Background(), "value + 30")
	if err != nil || string(result.Value) != "42" {
		t.Fatalf("result=%s error=%v", result.Value, err)
	}
	other := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
	if _, err := other.submit(context.Background(), "value"); err == nil {
		t.Fatal("state leaked between sessions")
	}
}

func TestRestrictedSessionFailureIsTerminal(t *testing.T) {
	t.Parallel()
	for _, source := range []string{
		"import \"os\"",
		"go func() {}()",
		"make(chan int)",
		"panic(\"failed\")",
		"[]int{1, 2}",
		"not defined",
	} {
		t.Run(source, func(t *testing.T) {
			session := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
			if _, err := session.submit(context.Background(), source); err == nil {
				t.Fatal("invalid submission accepted")
			}
			if result, err := session.submit(context.Background(), "42"); err == nil || result.Value != nil {
				t.Fatalf("failed session reused: result=%s error=%v", result.Value, err)
			}
		})
	}
}

func TestRestrictedSessionCumulativeSourceAndSubmissionLimits(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"source", "submissions"} {
		t.Run(kind, func(t *testing.T) {
			config := RestrictedSessionConfig{}
			if kind == "source" {
				config.MaxCumulativeSourceBytes = 2
			} else {
				config.MaxSubmissions = 1
			}
			session := newRestrictedSessionForTest(t, config)
			if _, err := session.submit(context.Background(), "42"); err != nil {
				t.Fatal(err)
			}
			if _, err := session.submit(context.Background(), "1"); !errors.Is(err, ErrRestrictedLimit) {
				t.Fatalf("cumulative %s budget reset: %v", kind, err)
			}
		})
	}
}

func TestRestrictedSessionCumulativeOutputLimit(t *testing.T) {
	t.Parallel()
	config := RestrictedSessionConfig{}
	config.MaxCumulativeOutputBytes = 6
	session := newRestrictedSessionForTest(t, config)
	first, err := session.submit(context.Background(), "print(\"abcd\")")
	if err != nil || first.Output != "abcd" {
		t.Fatalf("first=%+v error=%v", first, err)
	}
	second, err := session.submit(context.Background(), "print(\"abcd\")")
	if err == nil || second.Output != "ab" || !second.OutputTruncated || second.Value != nil {
		t.Fatalf("output budget reset: result=%+v error=%v", second, err)
	}
	if _, err := session.submit(context.Background(), "42"); err == nil {
		t.Fatal("output failure was not terminal")
	}
}

func TestRestrictedSessionCompilationBoundary(t *testing.T) {
	t.Parallel()
	session := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
	refused := errors.New("host refused execution")
	result, err := SubmitRestrictedSessionPhased(context.Background(), session,
		"var value = func() int { println(42); return 42 }()\nvalue",
		func() error {
			if session.output.buffer.Len() != 0 {
				t.Fatal("initialiser ran during compilation")
			}
			return refused
		})
	if !errors.Is(err, refused) || result.Output != "" || result.Value != nil {
		t.Fatalf("phase refusal ignored: result=%+v error=%v", result, err)
	}
	if _, err := session.submit(context.Background(), "42"); !errors.Is(err, refused) {
		t.Fatalf("refused session reused: %v", err)
	}
}

func TestRestrictedSessionCostIncludesInitialisers(t *testing.T) {
	t.Parallel()
	source := "var value = func() int { println(1); return 40 }()\nfunc init() { value++ }\nvalue+1"
	probe := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
	first, err := probe.submit(context.Background(), source)
	if err != nil || first.CostUsed <= 1 {
		t.Fatalf("cost not reported: %+v %v", first, err)
	}
	second, err := probe.submit(context.Background(), "value+1")
	if err != nil || second.CostUsed <= 0 || first.CostUsed <= second.CostUsed {
		t.Fatalf("initialisers not charged: first=%+v second=%+v error=%v", first, second, err)
	}
	config := RestrictedSessionConfig{}
	config.costBudget = first.CostUsed + second.CostUsed - 1
	session := newRestrictedSessionForTest(t, config)
	if _, err := session.submit(context.Background(), source); err != nil {
		t.Fatal(err)
	}
	if _, err := session.submit(context.Background(), "value+1"); !errors.Is(err, fault.ErrCostBudgetExceeded) {
		t.Fatalf("cumulative cost budget reset: %v", err)
	}
	config.costBudget = first.CostUsed - 1
	session = newRestrictedSessionForTest(t, config)
	if _, err := session.submit(context.Background(), source); !errors.Is(err, fault.ErrCostBudgetExceeded) {
		t.Fatalf("per-stage cost budget reset: %v", err)
	}
}

func TestRestrictedSessionSingleFlightAndTerminalClose(t *testing.T) {
	t.Parallel()
	session := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := SubmitRestrictedSessionPhased(context.Background(), session, "42", func() error {
			close(entered)
			<-release
			return nil
		})
		done <- err
	}()
	<-entered
	if _, err := session.submit(context.Background(), "1"); !errors.Is(err, ErrRestrictedBusy) {
		t.Errorf("concurrent submission accepted: %v", err)
	}
	session.Close()
	close(release)
	if err := <-done; !errors.Is(err, errRestrictedSessionClosed) {
		t.Fatalf("close ignored: %v", err)
	}
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if session.session != nil || session.output != nil {
		t.Fatal("closed session retained interpreter state")
	}
}

func TestRestrictedSessionExpiryAndCallerCancellation(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"idle", "lifetime", "caller"} {
		t.Run(kind, func(t *testing.T) {
			config := RestrictedSessionConfig{}
			if kind == "idle" {
				config.IdleTimeout = 10 * time.Millisecond
			}
			if kind == "lifetime" {
				config.Lifetime = 10 * time.Millisecond
			}
			session := newRestrictedSessionForTest(t, config)
			if kind == "caller" {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				if _, err := session.submit(ctx, "42"); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else {
				select {
				case <-session.ctx.Done():
				case <-time.After(2 * time.Second):
					t.Fatal("session did not expire")
				}
			}
			if _, err := session.submit(context.Background(), "42"); err == nil {
				t.Fatal("expired session reused")
			}
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
	if _, err := session.submit(context.Background(), "for {}"); err == nil {
		t.Fatal("infinite loop did not stop")
	}
	if _, err := session.submit(context.Background(), "42"); err == nil {
		t.Fatal("timed-out session reused")
	}
}

func TestRestrictedSessionInvalidConfiguration(t *testing.T) {
	t.Parallel()
	for _, change := range []func(*RestrictedSessionConfig){
		func(config *RestrictedSessionConfig) { config.IdleTimeout = -1 },
		func(config *RestrictedSessionConfig) { config.Lifetime = -1 },
		func(config *RestrictedSessionConfig) { config.costBudget = -1 },
		func(config *RestrictedSessionConfig) { config.MaxCumulativeSourceBytes = -1 },
		func(config *RestrictedSessionConfig) { config.MaxCumulativeOutputBytes = -1 },
		func(config *RestrictedSessionConfig) { config.MaxSubmissions = -1 },
		func(config *RestrictedSessionConfig) { config.Interpreter.Imports = []string{"os"} },
	} {
		config := RestrictedSessionConfig{}
		change(&config)
		if session, err := NewRestrictedSession(context.Background(), config); err == nil {
			session.Close()
			t.Fatal("invalid configuration accepted")
		}
	}
	session := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
	if _, err := session.submit(context.Background(), strings.Repeat("x", defaultRestrictedSourceBytes+1)); !errors.Is(err, ErrRestrictedLimit) {
		t.Fatalf("oversized source accepted: %v", err)
	}
}

func TestRestrictedSessionIdleTimerStopsDuringSubmission(t *testing.T) {
	t.Parallel()
	config := RestrictedSessionConfig{}
	config.IdleTimeout = 10 * time.Millisecond
	session := newRestrictedSessionForTest(t, config)
	_, err := SubmitRestrictedSessionPhased(context.Background(), session, "42", func() error {
		select {
		case <-session.ctx.Done():
			return errors.New("idle timer expired during active submission")
		case <-time.After(30 * time.Millisecond):
			return nil
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-session.ctx.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("idle timer was not restarted")
	}
}

func TestRestrictedSessionLifetimeCancelsActiveSubmission(t *testing.T) {
	t.Parallel()
	config := RestrictedSessionConfig{}
	config.Lifetime = 30 * time.Millisecond
	session := newRestrictedSessionForTest(t, config)
	result, err := SubmitRestrictedSessionPhased(context.Background(), session, "println(42)", func() error {
		<-session.ctx.Done()
		return nil
	})
	if !errors.Is(err, errRestrictedSessionExpired) || result.Output != "" || result.Value != nil {
		t.Fatalf("lifetime expiry allowed execution: result=%+v error=%v", result, err)
	}
}

func TestRestrictedSessionBoundaryPanicIsTerminal(t *testing.T) {
	t.Parallel()
	session := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
	func() {
		defer func() {
			if recover() == nil {
				t.Error("trusted boundary panic was swallowed")
			}
		}()
		_, _ = SubmitRestrictedSessionPhased(context.Background(), session, "42", func() error {
			panic("boundary failure")
		})
	}()
	if _, err := session.submit(context.Background(), "42"); !errors.Is(err, errRestrictedSessionClosed) {
		t.Fatalf("panicked session was reusable: %v", err)
	}
}

func TestRestrictedSessionCompilationFailureDoesNotRequestExecution(t *testing.T) {
	t.Parallel()
	session := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
	called := false
	result, err := SubmitRestrictedSessionPhased(context.Background(), session,
		"var value = func() int { println(42); return 42 }()\ngo func() {}()",
		func() error { called = true; return nil })
	if err == nil || called || result.Output != "" || result.Value != nil {
		t.Fatalf("compilation failure crossed execution boundary: called=%v result=%+v error=%v", called, result, err)
	}
}

func TestRestrictedSessionParentCancellationReleasesIdleState(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	session, err := NewRestrictedSession(ctx, RestrictedSessionConfig{})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(session.Close)
	cancel()
	session.releaseClosed()
	session.mutex.Lock()
	retained := session.session != nil || session.output != nil
	session.mutex.Unlock()
	if retained {
		t.Fatal("cancelled parent retained idle interpreter state")
	}
	if _, err := session.submit(context.Background(), "42"); !errors.Is(err, context.Canceled) {
		t.Fatalf("parent cancellation ignored: %v", err)
	}
}

func TestRestrictedSessionCallerCancellationAtBoundary(t *testing.T) {
	t.Parallel()
	session := newRestrictedSessionForTest(t, RestrictedSessionConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result, err := SubmitRestrictedSessionPhased(ctx, session, "println(42)", func() error {
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || result.Output != "" || result.Value != nil {
		t.Fatalf("caller cancellation allowed execution: result=%+v error=%v", result, err)
	}
}
