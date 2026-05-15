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
	"sync"
	"time"

	"pipit.sh/pipit/internal/symtab"
)

const (
	// defaultSessionSourceBytes is the default cumulative source byte quota.
	defaultSessionSourceBytes = 4 << 20

	// defaultSessionOutputBytes is the default cumulative output byte quota.
	defaultSessionOutputBytes = 1 << 20

	// defaultSessionSubmissions is the default cumulative submission count.
	defaultSessionSubmissions = 1024

	// defaultSessionCostBudget is the default cumulative metered cost budget.
	defaultSessionCostBudget = 100_000_000

	// defaultSessionLifetime is the default total session lifetime.
	defaultSessionLifetime = 15 * time.Minute
)

var (
	// errRestrictedSessionClosed reports explicit permanent closure.
	errRestrictedSessionClosed = errors.New("restricted session is closed")

	// errRestrictedSessionExpired reports idle or total lifetime exhaustion.
	errRestrictedSessionExpired = errors.New("restricted session expired")
)

// RestrictedSessionConfig defines immutable cooperative limits for one private session.
// Native workers must additionally enforce hard process deadlines and resource limits.
type RestrictedSessionConfig struct {
	// Interpreter holds the per-submission cooperative limits.
	Interpreter RestrictedConfig

	// IdleTimeout is the maximum idle gap between submissions.
	IdleTimeout time.Duration

	// Lifetime is the maximum total session duration.
	Lifetime time.Duration

	// MaxCumulativeSourceBytes bounds total source across all submissions.
	MaxCumulativeSourceBytes int

	// MaxCumulativeOutputBytes bounds total captured output.
	MaxCumulativeOutputBytes int

	// MaxSubmissions bounds the number of submissions.
	MaxSubmissions int

	// costBudget bounds the cumulative metered cost.
	costBudget int64
}

// RestrictedSession retains script state without exposing the backing service. Any
// admitted submission failure is terminal.
type RestrictedSession struct {
	// ctx is the session-scoped context, cancelled on close or expiry.
	ctx context.Context

	// cancel terminates the session with a cause.
	cancel context.CancelCauseFunc

	// stopLifetime cancels the outer lifetime context.
	stopLifetime context.CancelFunc

	// idle is the timer for the current idle interval.
	idle *time.Timer

	// session is the retained interpreter session.
	session *Session

	// output is the bounded capture writer for this submission.
	output *restrictedOutput

	// config holds the immutable cooperative limits.
	config RestrictedSessionConfig

	// generation counts idle intervals to prevent stale timer fires.
	generation uint64

	// sourceRemaining tracks the remaining cumulative source byte budget.
	sourceRemaining int

	// outputRemaining tracks the remaining cumulative output byte budget.
	outputRemaining int

	// costRemaining tracks the remaining cumulative cost budget.
	costRemaining int64

	// submissionsRemaining tracks the remaining submission count.
	submissionsRemaining int

	// mutex guards all mutable session state.
	mutex sync.Mutex

	// active is true while a submission is executing.
	active bool
}

// NewRestrictedSession constructs a private interpreter with copied reviewed grants.
// Defaults are 60 seconds idle, 15 minutes lifetime, 4 MiB cumulative source, 1 MiB
// cumulative output, 1,024 submissions and 100,000,000 metered cost units.
//
// Takes config (RestrictedSessionConfig) which holds limits and imports.
//
// Returns a session owned by one caller, or an error for invalid limits or cancellation.
func NewRestrictedSession(ctx context.Context, config RestrictedSessionConfig) (*RestrictedSession, error) {
	return newRestrictedSession(ctx, config, nil)
}

// NewRestrictedSessionWithProvider is NewRestrictedSession over the full registry the
// provider supplies, exposing only the allowlisted packages.
//
// Takes config (RestrictedSessionConfig) which holds limits and imports.
// Takes provider (symtab.SymbolProviderPort) which supplies the full symbol registry.
//
// Returns the session, or a configuration error.
func NewRestrictedSessionWithProvider(ctx context.Context, config RestrictedSessionConfig, provider symtab.SymbolProviderPort) (*RestrictedSession, error) {
	if provider == nil {
		return nil, ErrInvalidRestrictedConfig
	}
	return newRestrictedSession(ctx, config, provider)
}

// newRestrictedSession shares the session lifecycle between the direct and provider
// constructors.
//
// Takes config (RestrictedSessionConfig) which holds limits and imports.
// Takes provider (symtab.SymbolProviderPort) which may be nil for the math-only surface.
//
// Returns the session, or a configuration error.
func newRestrictedSession(ctx context.Context, config RestrictedSessionConfig, provider symtab.SymbolProviderPort) (*RestrictedSession, error) {
	if ctx == nil {
		return nil, ErrInvalidRestrictedConfig
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	interpreter, err := validateRestrictedSessionConfig(&config, provider)
	if err != nil {
		return nil, err
	}
	lifetime, stopLifetime := context.WithTimeoutCause(ctx, config.Lifetime, errRestrictedSessionExpired)
	sessionContext, cancel := context.WithCancelCause(lifetime)
	output := &restrictedOutput{buffer: bytes.Buffer{}, remaining: 0, truncated: false}
	session := &RestrictedSession{
		mutex: sync.Mutex{}, config: config, ctx: sessionContext, cancel: cancel,
		stopLifetime: stopLifetime, idle: nil, generation: 0, active: false,
		session: interpreter.newService(output).NewSession(), output: output,
		sourceRemaining: config.MaxCumulativeSourceBytes, outputRemaining: config.MaxCumulativeOutputBytes,
		costRemaining: config.costBudget, submissionsRemaining: config.MaxSubmissions,
	}
	session.armIdle()
	context.AfterFunc(sessionContext, session.releaseClosed)
	return session, nil
}

// Close permanently cancels the session. Active work observes cooperative cancellation;
// its interpreter state is released when the active call returns.
func (session *RestrictedSession) Close() {
	if session == nil || session.cancel == nil {
		return
	}
	session.cancel(errRestrictedSessionClosed)
	session.releaseClosed()
}

// submit evaluates one bounded submission with retained script state.
//
// Takes source (string) which is the Go source to evaluate.
//
// Returns bounded output and a JSON scalar. Every admitted failure closes the session.
func (session *RestrictedSession) submit(ctx context.Context, source string) (RestrictedResult, error) {
	return SubmitRestrictedSessionPhased(ctx, session, source, func() error { return nil })
}

// releaseClosed drops retained state only after its exclusive owner has stopped.
//
// Safe for concurrent use; acquires the session mutex.
func (session *RestrictedSession) releaseClosed() {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if session.ctx.Err() == nil {
		return
	}
	if session.idle != nil {
		session.idle.Stop()
	}
	session.stopLifetime()
	if !session.active {
		session.session = nil
		session.output = nil
	}
}

// armIdle starts an idle timer while the caller owns the session lock.
//
// Not safe for concurrent use; the caller must hold the session mutex.
func (session *RestrictedSession) armIdle() {
	session.generation++
	generation := session.generation
	session.idle = time.AfterFunc(session.config.IdleTimeout, func() {
		session.mutex.Lock()
		defer session.mutex.Unlock()
		if session.active || session.generation != generation || session.ctx.Err() != nil {
			return
		}
		session.cancel(errRestrictedSessionExpired)
		session.stopLifetime()
		session.session = nil
		session.output = nil
	})
}

// admit accepts one submission without retaining a queue.
//
// Takes source (string) whose bytes are charged before compilation.
//
// Returns an admission, lifetime or cumulative resource error.
//
// Safe for concurrent use; acquires the session mutex.
func (session *RestrictedSession) admit(source string) error {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if session.ctx.Err() != nil {
		return context.Cause(session.ctx)
	}
	if session.active {
		return ErrRestrictedBusy
	}
	if len(source) > session.config.Interpreter.MaxSourceBytes || len(source) > session.sourceRemaining ||
		session.submissionsRemaining == 0 || session.costRemaining <= 0 {
		session.cancel(ErrRestrictedLimit)
		return ErrRestrictedLimit
	}
	session.active = true
	session.idle.Stop()
	session.generation++
	session.sourceRemaining -= len(source)
	session.submissionsRemaining--
	return nil
}

// finish returns ownership, closing on failure or starting a fresh idle interval.
//
// Takes err (error) which reports the completed submission's outcome.
//
// Returns the terminal cause when the session was cancelled.
//
// Safe for concurrent use; acquires the session mutex.
func (session *RestrictedSession) finish(err error) error {
	session.mutex.Lock()
	defer session.mutex.Unlock()
	if err != nil {
		session.cancel(err)
	}
	session.active = false
	if session.ctx.Err() != nil {
		session.stopLifetime()
		session.session = nil
		session.output = nil
		return errors.Join(err, context.Cause(session.ctx))
	}
	session.armIdle()
	return nil
}

// evaluatePhased runs an exclusively admitted submission and charges cumulative budgets.
//
// Takes source (string) which is the Go source to evaluate.
// Takes beginExecution (func() error) which the caller invokes after compilation to
// approve execution.
//
// Returns bounded output, a scalar and metered cost, or a terminal evaluation error.
func (session *RestrictedSession) evaluatePhased(
	ctx context.Context, source string, beginExecution func() error,
) (RestrictedResult, error) {
	config := session.config.Interpreter
	output := session.output
	output.buffer.Reset()
	output.remaining = min(config.MaxOutputBytes, session.outputRemaining)
	output.truncated = false
	service := session.session.service
	service.limits.CostBudget = min(config.CostBudget, session.costRemaining)
	service.lastCostUsed.Store(0)
	compileContext, cancelCompile := context.WithTimeout(ctx, restrictedCompilationTimeout)
	execute, err := session.session.compileSubmission(compileContext, source)
	err = errors.Join(err, compileContext.Err())
	cancelCompile()
	if err != nil {
		return RestrictedResult{}, err
	}
	if err := beginExecution(); err != nil {
		return RestrictedResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return RestrictedResult{}, err
	}
	executionContext, cancelExecution := context.WithTimeout(ctx, config.Timeout)
	defer cancelExecution()
	value, err := execute(executionContext)
	result := RestrictedResult{
		Value: nil, Output: output.String(), OutputTruncated: output.truncated, CostUsed: service.LastCostUsed(),
	}
	session.outputRemaining -= len(result.Output)
	session.costRemaining -= min(session.costRemaining, max(result.CostUsed, 0))
	if err := errors.Join(err, executionContext.Err()); err != nil {
		return result, err
	}
	if result.OutputTruncated {
		return result, ErrRestrictedLimit
	}
	result.Value, err = restrictedScalar(value, config.MaxReturnBytes)
	return result, err
}

// SubmitRestrictedSessionPhased compiles before asking the trusted worker transport for
// execution permission.
//
// Takes session (*RestrictedSession) which holds retained script state.
// Takes source (string) which is the Go source to evaluate.
// Takes beginExecution (func() error) which the caller invokes after compilation to
// approve execution.
//
// Returns only bounded output and a JSON scalar, or a terminal submission error.
func SubmitRestrictedSessionPhased(
	ctx context.Context, session *RestrictedSession, source string, beginExecution func() error,
) (result RestrictedResult, err error) {
	if ctx == nil || session == nil || session.ctx == nil || beginExecution == nil {
		return RestrictedResult{}, ErrInvalidRestrictedConfig
	}
	if err := session.admit(source); err != nil {
		return RestrictedResult{}, err
	}
	completed := false
	defer func() {
		if !completed {
			err = errRestrictedSessionClosed
		}
		err = session.finish(err)
		if err != nil {
			result.Value = nil
		}
	}()
	operation, cancel := context.WithCancelCause(session.ctx)
	stopCaller := context.AfterFunc(ctx, func() { cancel(context.Cause(ctx)) })
	defer stopCaller()
	defer cancel(context.Canceled)
	if err = ctx.Err(); err == nil {
		result, err = session.evaluatePhased(operation, source, func() error {
			if err := errors.Join(ctx.Err(), session.ctx.Err()); err != nil {
				return err
			}
			if err := beginExecution(); err != nil {
				return err
			}
			return errors.Join(ctx.Err(), session.ctx.Err())
		})
	}
	err = errors.Join(err, ctx.Err())
	completed = true
	return result, err
}

// validateRestrictedSessionConfig fills finite defaults and copies the interpreter
// policy.
//
// Takes config (*RestrictedSessionConfig) which receives the validated private policy.
// Takes provider (symtab.SymbolProviderPort) which may be nil.
//
// Returns the reviewed interpreter factory or a configuration error.
func validateRestrictedSessionConfig(config *RestrictedSessionConfig, provider symtab.SymbolProviderPort) (*RestrictedInterpreter, error) {
	if config.IdleTimeout < 0 || config.Lifetime < 0 || config.costBudget < 0 {
		return nil, ErrInvalidRestrictedConfig
	}
	for _, limit := range []struct {
		value    *int
		fallback int
	}{
		{value: &config.MaxCumulativeSourceBytes, fallback: defaultSessionSourceBytes},
		{value: &config.MaxCumulativeOutputBytes, fallback: defaultSessionOutputBytes},
		{value: &config.MaxSubmissions, fallback: defaultSessionSubmissions},
	} {
		if *limit.value < 0 {
			return nil, ErrInvalidRestrictedConfig
		}
		if *limit.value == 0 {
			*limit.value = limit.fallback
		}
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = time.Minute
	}
	if config.Lifetime == 0 {
		config.Lifetime = defaultSessionLifetime
	}
	if config.costBudget == 0 {
		config.costBudget = defaultSessionCostBudget
	}
	interpreter, err := newRestrictedInterpreter(config.Interpreter, provider)
	if err != nil {
		return nil, err
	}
	config.Interpreter = interpreter.config
	return interpreter, nil
}
