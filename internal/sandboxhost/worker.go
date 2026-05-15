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
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/policy"
	"pipit.sh/pipit/internal/sandboxworker"
	"pipit.sh/pipit/internal/stdlibindex"
)

const (
	// maximumTenant bounds a host-selected tenant identity in bytes.
	maximumTenant = 256

	// maximumIsolatedImports bounds the host allowlist a single launch may grant, so a
	// malformed configuration cannot request an unbounded package set.
	maximumIsolatedImports = 256
)

var (
	// ErrIsolatedUnavailable reports an unsupported or unenforceable native boundary. It
	// wraps policy.ErrUnavailable.
	ErrIsolatedUnavailable = fmt.Errorf("native isolated execution unavailable: %w", policy.ErrUnavailable)

	// ErrInvalidIsolatedConfig reports invalid host-selected worker policy. It wraps
	// policy.ErrInvalidConfig.
	ErrInvalidIsolatedConfig = fmt.Errorf("invalid isolated worker configuration: %w", policy.ErrInvalidConfig)

	// ErrIsolatedClosed reports a worker that has already accepted a submission or closed.
	// It wraps policy.ErrClosed.
	ErrIsolatedClosed = fmt.Errorf("isolated worker is already used or closed: %w", policy.ErrClosed)

	// ErrIsolatedUsed is a deprecated alias of ErrIsolatedClosed.
	ErrIsolatedUsed = ErrIsolatedClosed
)

// RestrictedResult is the bounded evaluation result every isolated tier returns. It is
// the same type the restricted tier returns, aliased here so the exported signatures name
// it without a package qualifier no caller outside the module can spell.
type RestrictedResult = app.RestrictedResult

// IsolatedConfig contains host-private settings for an experimental native worker. Zero
// resource limits select finite defaults.
type IsolatedConfig struct {
	// Logger receives audit events at info level and warnings above. Nil discards them.
	Logger *slog.Logger `json:"-"`

	// Images, when set, supplies executables pre-staged by PrepareIsolatedImages so a launch
	// copies their verified bytes instead of re-hashing them; nil stages per launch. The
	// handle must outlive every launch that uses it and be closed afterwards.
	Images *IsolatedImages `json:"-"`

	// Tenant is a stable host-selected identity, never a value supplied by a script. It keys
	// the service reservation and the admission slot on Linux, so one tenant's long session
	// never blocks another's launches under the same delegated parent.
	Tenant string

	// StateDirectory is an absolute, private directory the host owns, disjoint from every
	// granted root. Each launch records its checkpoint, approval and image stores under
	// StateDirectory/<tenant>/<launch>/ and removes them on a clean close;
	// NewIsolatedRecovery walks what a dead host left behind.
	StateDirectory string

	// WorkerPath names an absolute path to a host-approved static worker executable.
	WorkerPath string

	// WatchdogPath names an independently approved static lifetime supervisor.
	WatchdogPath string

	// LinuxCgroupParent names an explicitly delegated cgroup-v2 parent on Linux.
	LinuxCgroupParent string

	// Imports selects the packages the isolated script may import. The worker fails closed
	// on anything absent from the registry.
	Imports []string

	// MemoryBytes defaults to 256 MiB and must be page-aligned on Linux.
	MemoryBytes int64

	// CPUMilli defaults to 1000, corresponding to one logical CPU of bandwidth.
	CPUMilli int64

	// Tasks defaults to 64 and counts native threads as well as processes.
	Tasks int64

	// Lifetime includes admission, startup and idle time. The one-shot default is 17
	// seconds; NewIsolatedSession selects a 15-minute default and upper bound.
	Lifetime time.Duration

	// OutputBytes is shared by native diagnostics and script output. The one-shot default is
	// 64 KiB; NewIsolatedSession selects a 1 MiB default.
	OutputBytes int

	// WorkerSHA256 approves exact executable bytes independently of script input.
	WorkerSHA256 [sha256.Size]byte

	// WatchdogSHA256 approves exact supervisor bytes independently of script input.
	WatchdogSHA256 [sha256.Size]byte
}

// IsolatedWorker owns one experimental, fresh native worker for one submission. Call
// Close even after evaluation fails.
type IsolatedWorker struct {
	// process is the native boundary backing this worker.
	process isolatedProcess

	// imports holds the approved package import paths.
	imports []string

	// used tracks whether the single submission has been consumed.
	used atomic.Bool
}

// NewIsolatedWorker launches a verified worker with independent native limits. The
// context governs its entire lifetime.
//
// Takes config (IsolatedConfig) which selects the approved executable and policy.
//
// Returns *IsolatedWorker on success; nil with the error otherwise. On a partial launch
// the constructor cleans up itself and returns nil, wrapping *IsolatedCleanupError when
// that cleanup also fails.
func NewIsolatedWorker(ctx context.Context, config IsolatedConfig) (*IsolatedWorker, error) {
	if err := validateIsolatedConfig(ctx, &config); err != nil {
		return nil, err
	}
	ctx = isolatedLoggerContext(ctx, config.Logger)
	process, err := newIsolatedProcess(ctx, config)
	if process == nil {
		return nil, err
	}
	if err != nil {
		return nil, cleanupOrFail(err, process.Close)
	}
	return &IsolatedWorker{process: process, imports: config.Imports, used: atomic.Bool{}}, nil
}

// Eval compiles and evaluates source exactly once in this worker.
//
// Takes source (string) which contains an expression or statements.
//
// Returns RestrictedResult containing only bounded output and a JSON scalar.
// Returns error for script failure, cancellation, protocol failure or cleanup failure.
func (worker *IsolatedWorker) Eval(source string) (RestrictedResult, error) {
	return worker.evaluate(sandboxworker.Request{Kind: "expression", Source: source, Entrypoint: ""})
}

// EvalFile compiles a complete source file and executes its named entrypoint once.
//
// Takes source (string) which contains a complete Go file, not a host pathname.
// Takes entrypoint (string) which names the function to invoke.
//
// Returns RestrictedResult containing only bounded output and a JSON scalar.
// Returns error for script failure, cancellation, protocol failure or cleanup failure.
func (worker *IsolatedWorker) EvalFile(source, entrypoint string) (RestrictedResult, error) {
	return worker.evaluate(sandboxworker.Request{Kind: "file", Source: source, Entrypoint: entrypoint})
}

// Close permanently closes admission and terminates any active submission. Resource
// cleanup may be retried; a worker can never be reused after Close.
//
// Returns error for worker failure or incomplete native cleanup.
func (worker *IsolatedWorker) Close() error {
	if worker == nil || worker.process == nil {
		return nil
	}
	worker.used.Store(true)
	return worker.process.Close()
}

// Diagnostics returns bounded, untrusted native stdout and stderr for host inspection.
//
// Returns string containing diagnostic bytes, never interpreted host objects.
func (worker *IsolatedWorker) Diagnostics() string {
	if worker == nil || worker.process == nil {
		return ""
	}
	return worker.process.Output()
}

// evaluate validates the protocol and reaps the worker before publishing any result.
//
// Takes request (sandboxworker.Request) which selects this single source submission.
//
// Returns RestrictedResult only after successful native lifecycle completion.
// Returns error for any failed execution or boundary invariant.
func (worker *IsolatedWorker) evaluate(request sandboxworker.Request) (RestrictedResult, error) {
	if worker == nil || worker.process == nil || !worker.used.CompareAndSwap(false, true) {
		return RestrictedResult{}, ErrIsolatedUsed
	}
	response, err := sandboxworker.Exchange(worker.process,
		sandboxworker.Configuration{Profile: sandboxworker.Profile, Imports: worker.imports}, request)
	if err != nil {
		return RestrictedResult{}, errors.Join(err, worker.process.Close())
	}
	if err := worker.process.Wait(); err != nil {
		return RestrictedResult{}, errors.Join(err, worker.process.Close())
	}
	if err := worker.process.Close(); err != nil {
		return RestrictedResult{}, err
	}
	result := RestrictedResult{
		Value: response.Value, Output: response.Output,
		CostUsed: response.CostUsed, OutputTruncated: response.OutputTruncated,
	}
	if response.Code == "evaluation_failed" {
		return result, &fault.EvaluationError{Tier: "isolated", Message: response.Error}
	}
	return result, nil
}

// IsolatedCleanupError reports that a constructor's launch failed and the cleanup it then
// attempted did not complete, so native resources may still be held. Retry runs the
// cleanup again on a fresh context; errors.Is matches both the launch cause and the
// cleanup cause.
type IsolatedCleanupError struct {
	// launch holds the original constructor failure.
	launch error

	// cleanup holds the subsequent cleanup failure.
	cleanup error

	// retry runs the retained cleanup again on a fresh context.
	retry func(context.Context) error
}

// Error renders the launch and cleanup causes.
//
// Returns string describing both failures.
func (e *IsolatedCleanupError) Error() string {
	return fmt.Sprintf("isolated launch failed and cleanup did not complete: %v (cleanup: %v)", e.launch, e.cleanup)
}

// Unwrap exposes both the launch and cleanup causes to errors.Is and errors.As.
//
// Returns []error which is the launch cause and the cleanup cause.
func (e *IsolatedCleanupError) Unwrap() []error {
	return []error{e.launch, e.cleanup}
}

// Retry runs the retained cleanup again.
//
// Returns error which is nil once the resources are released.
func (e *IsolatedCleanupError) Retry(ctx context.Context) error {
	if e == nil || e.retry == nil {
		return nil
	}
	return e.retry(ctx)
}

// isolatedProcess is the host-owned native boundary, never exposed to scripts.
type isolatedProcess interface {
	sandboxworker.HostTransport

	// Done returns a channel closed when the worker exits.
	//
	// Returns a receive-only channel.
	Done() <-chan struct{}

	// Wait blocks until the worker exits and returns its error.
	//
	// Returns error from the worker's execution.
	Wait() error

	// Close terminates the worker and releases native resources.
	//
	// Returns error when cleanup fails.
	Close() error

	// Output returns bounded, untrusted native diagnostic text.
	//
	// Returns string containing diagnostic output.
	Output() string
}

// cleanupOrFail runs closer for a partially-constructed owner after a launch error. It
// returns the launch error when cleanup succeeds, or an *IsolatedCleanupError carrying a
// Retry when it does not, so a constructor never hands back a live owner alongside an
// error.
//
// Takes launch (error) which is the non-nil launch failure.
// Takes closer (func() error) which releases the partial resources.
//
// Returns error which is launch, or an *IsolatedCleanupError.
func cleanupOrFail(launch error, closer func() error) error {
	if closeErr := closer(); closeErr != nil {
		return &IsolatedCleanupError{launch: launch, cleanup: closeErr, retry: func(context.Context) error { return closer() }}
	}
	return launch
}

// validTenant reports whether a tenant identity is non-empty, bounded, UTF-8 and free of
// NUL bytes.
//
// Takes tenant (string) which the host selected.
//
// Returns bool which is true for a usable identity.
func validTenant(tenant string) bool {
	return len(tenant) > 0 && len(tenant) <= maximumTenant && utf8.ValidString(tenant) && strings.IndexByte(tenant, 0) < 0
}

// validateIsolatedConfig validates native approval and copies reviewed grants.
//
// Takes config (*IsolatedConfig) which holds the policy to validate.
//
// Returns error before launch when host approval or resource policy is invalid.
func validateIsolatedConfig(ctx context.Context, config *IsolatedConfig) error {
	if ctx == nil || !filepath.IsAbs(config.WorkerPath) || config.WorkerSHA256 == ([sha256.Size]byte{}) ||
		!filepath.IsAbs(config.WatchdogPath) || config.WatchdogSHA256 == ([sha256.Size]byte{}) ||
		config.Lifetime < 0 || config.OutputBytes < 0 || config.MemoryBytes < 0 || config.CPUMilli < 0 || config.Tasks < 0 ||
		len(config.Imports) > maximumIsolatedImports || !validTenant(config.Tenant) || !filepath.IsAbs(config.StateDirectory) {
		return ErrInvalidIsolatedConfig
	}
	config.Imports = append([]string(nil), config.Imports...)
	for _, name := range config.Imports {
		if !stdlibindex.Has(name) || name == "pipit/fs" {
			return ErrInvalidIsolatedConfig
		}
	}
	return ctx.Err()
}
