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

package sandboxhost

import (
	"context"
	"errors"
	"sync/atomic"
	"time"

	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/sandboxworker"
)

const (
	// isolatedSessionLifetime is the default and upper bound for session lifetime.
	isolatedSessionLifetime = 15 * time.Minute

	// isolatedSessionOutputBytes is the default cumulative output byte quota.
	isolatedSessionOutputBytes = 1 << 20
)

var (
	// ErrIsolatedSessionClosed reports a permanently closed or unavailable session. It
	// matches both sandboxworker.ErrSessionClosed and the base policy.ErrClosed under
	// errors.Is.
	ErrIsolatedSessionClosed = sandboxworker.ErrSessionClosed

	// ErrIsolatedSessionBusy rejects overlapping submissions without queuing source.
	ErrIsolatedSessionBusy = sandboxworker.ErrSessionBusy

	// ErrIsolatedSessionLimit reports host-counted cumulative resource exhaustion.
	ErrIsolatedSessionLimit = sandboxworker.ErrSessionLimit
)

// IsolatedSession owns one experimental dedicated native worker with retained script
// state and immutable grants.
type IsolatedSession struct {
	// process is the native boundary backing this session.
	process isolatedProcess

	// client drives the session protocol over the worker transport.
	client *sandboxworker.SessionClient

	// active prevents concurrent submissions.
	active atomic.Bool
}

// NewIsolatedSession launches and configures an approved dedicated native worker. A zero
// Lifetime selects 15 minutes; longer lifetimes are rejected.
//
// Takes config (IsolatedConfig) which sets the worker's resource limits and grants.
//
// Returns a session owner, possibly alongside a launch or handshake error. Always Close a
// non-nil session, including after errors; failed cleanup is retryable.
func NewIsolatedSession(ctx context.Context, config IsolatedConfig) (*IsolatedSession, error) {
	if err := validateIsolatedConfig(ctx, &config); err != nil {
		return nil, err
	}
	ctx = isolatedLoggerContext(ctx, config.Logger)
	if config.Lifetime > isolatedSessionLifetime {
		return nil, ErrInvalidIsolatedConfig
	}
	if config.Lifetime == 0 {
		config.Lifetime = isolatedSessionLifetime
	}
	if config.OutputBytes == 0 {
		config.OutputBytes = isolatedSessionOutputBytes
	}
	process, err := newIsolatedProcess(ctx, config)
	if process == nil {
		return nil, err
	}
	if err != nil {
		return nil, cleanupOrFail(err, process.Close)
	}
	client, err := sandboxworker.OpenSession(process,
		sandboxworker.Configuration{Profile: sandboxworker.SessionProfile, Imports: config.Imports})
	if err != nil {
		return nil, cleanupOrFail(err, process.Close)
	}
	return &IsolatedSession{process: process, client: client, active: atomic.Bool{}}, nil
}

// Submit evaluates one source submission against retained state. Concurrent calls return
// ErrIsolatedSessionBusy.
//
// Takes source (string) which is the Go source to evaluate.
//
// Returns bounded output, a JSON scalar and untrusted worker-reported execution cost.
// Returns error after attempting native cleanup on any admitted failure.
func (session *IsolatedSession) Submit(ctx context.Context, source string) (result RestrictedResult, err error) {
	if session == nil || session.process == nil || session.client == nil {
		return RestrictedResult{}, ErrIsolatedSessionClosed
	}
	if ctx == nil {
		return RestrictedResult{}, ErrInvalidIsolatedConfig
	}
	if !session.active.CompareAndSwap(false, true) {
		return RestrictedResult{}, ErrIsolatedSessionBusy
	}
	defer session.active.Store(false)
	closed := make(chan error, 1)
	stopCancellation := context.AfterFunc(ctx, func() { closed <- session.Close() })
	defer func() {
		if !stopCancellation() {
			err = errors.Join(err, ctx.Err(), <-closed)
		} else if ctx.Err() != nil {
			err = errors.Join(err, ctx.Err(), session.Close())
		}
		if err != nil {
			result = RestrictedResult{Value: nil, Output: "", CostUsed: 0, OutputTruncated: false}
		}
	}()
	if err := ctx.Err(); err != nil {
		return RestrictedResult{}, err
	}
	response, err := session.client.Submit(source)
	if err != nil {
		if errors.Is(err, sandboxworker.ErrSessionEvaluation) {
			return RestrictedResult{}, &fault.EvaluationError{Tier: "isolated-session", Message: err.Error()}
		}
		return RestrictedResult{}, err
	}
	return RestrictedResult{Value: response.Value, Output: response.Output,
		CostUsed: response.CostUsed, OutputTruncated: response.OutputTruncated}, nil
}

// Close permanently closes admission and reaps the dedicated worker. Failed cleanup may
// be retried.
//
// Returns error for execution failure or incomplete native cleanup.
func (session *IsolatedSession) Close() error {
	if session == nil || session.process == nil {
		return nil
	}
	if session.client == nil {
		return session.process.Close()
	}
	return session.client.Close()
}

// Done signals worker exit and the first cleanup attempt, including idle expiry. Call
// Close to inspect errors and retry incomplete cleanup after notification.
//
// Returns a receive-only channel, already closed for a nil or absent session.
func (session *IsolatedSession) Done() <-chan struct{} {
	if session == nil || session.process == nil {
		done := make(chan struct{})
		close(done)
		return done
	}
	return session.process.Done()
}

// Diagnostics returns bounded, untrusted native stdout and stderr.
//
// Returns diagnostic text for host inspection, never interpreted objects.
func (session *IsolatedSession) Diagnostics() string {
	if session == nil || session.process == nil {
		return ""
	}
	return session.process.Output()
}
