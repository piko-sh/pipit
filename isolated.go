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

package pipit

import (
	"context"

	"pipit.sh/pipit/internal/sandboxhost"
)

const (
	// FilesystemRead grants read access to a filesystem root.
	FilesystemRead = sandboxhost.FilesystemRead

	// FilesystemWrite grants write access to a filesystem root.
	FilesystemWrite = sandboxhost.FilesystemWrite

	// FilesystemList grants directory listing on a filesystem root.
	FilesystemList = sandboxhost.FilesystemList
)

var (
	// ErrIsolatedUnavailable reports an unsupported or unenforceable native boundary.
	ErrIsolatedUnavailable = sandboxhost.ErrIsolatedUnavailable

	// ErrInvalidIsolatedConfig reports invalid host-selected worker policy.
	ErrInvalidIsolatedConfig = sandboxhost.ErrInvalidIsolatedConfig

	// ErrIsolatedClosed reports a worker that has already accepted a submission or closed.
	ErrIsolatedClosed = sandboxhost.ErrIsolatedClosed

	// ErrIsolatedUsed reports a consumed worker. It is the former spelling of
	// ErrIsolatedClosed and matches the same value.
	ErrIsolatedUsed = sandboxhost.ErrIsolatedUsed

	// ErrIsolatedSessionClosed reports a permanently closed or unavailable session.
	ErrIsolatedSessionClosed = sandboxhost.ErrIsolatedSessionClosed

	// ErrIsolatedSessionBusy reports a concurrent submission to one session.
	ErrIsolatedSessionBusy = sandboxhost.ErrIsolatedSessionBusy

	// ErrIsolatedSessionLimit reports an exceeded session resource limit.
	ErrIsolatedSessionLimit = sandboxhost.ErrIsolatedSessionLimit
)

// IsolatedConfig selects the host policy one isolated launch runs under: the executables
// to verify, the tenant that owns the reservation, and the resource ceilings.
type IsolatedConfig = sandboxhost.IsolatedConfig

// IsolatedFilesystemConfig adds independently approved broker images and filesystem
// grants to an isolated worker configuration.
type IsolatedFilesystemConfig = sandboxhost.IsolatedFilesystemConfig

// FilesystemRoot is one named filesystem grant handed to a broker.
type FilesystemRoot = sandboxhost.FilesystemRoot

// FilesystemRights is the rights bitmask a single grant carries.
type FilesystemRights = sandboxhost.FilesystemRights

// FilesystemLimits is the cumulative broker budget one launch may spend.
type FilesystemLimits = sandboxhost.FilesystemLimits

// IsolatedWorker evaluates one submission inside a verified native boundary.
type IsolatedWorker = sandboxhost.IsolatedWorker

// NewIsolatedWorker launches a verified single-submission worker.
//
// Takes config (IsolatedConfig) which is validated and defensively copied.
//
// Returns *IsolatedWorker which accepts exactly one submission.
// Returns error when the configuration is invalid or the native launch fails.
func NewIsolatedWorker(ctx context.Context, config IsolatedConfig) (*IsolatedWorker, error) {
	return sandboxhost.NewIsolatedWorker(ctx, config)
}

// IsolatedFilesystemWorker evaluates one submission against an approved broker.
type IsolatedFilesystemWorker = sandboxhost.IsolatedFilesystemWorker

// NewIsolatedFilesystemWorker launches a worker paired with an approved broker.
//
// Takes config (IsolatedFilesystemConfig) which is validated and defensively copied.
//
// Returns *IsolatedFilesystemWorker which accepts exactly one submission.
// Returns error when the configuration is invalid or either launch fails.
func NewIsolatedFilesystemWorker(ctx context.Context, config IsolatedFilesystemConfig) (*IsolatedFilesystemWorker, error) {
	return sandboxhost.NewIsolatedFilesystemWorker(ctx, config)
}

// IsolatedSession keeps interpreter state across submissions inside one boundary.
type IsolatedSession = sandboxhost.IsolatedSession

// NewIsolatedSession launches a worker that keeps state across submissions.
//
// Takes config (IsolatedConfig) which is validated and defensively copied.
//
// Returns *IsolatedSession which accepts submissions until closed.
// Returns error when the configuration is invalid or the native launch fails.
func NewIsolatedSession(ctx context.Context, config IsolatedConfig) (*IsolatedSession, error) {
	return sandboxhost.NewIsolatedSession(ctx, config)
}

// IsolatedRecovery claims and releases orphaned worker launch records.
type IsolatedRecovery = sandboxhost.IsolatedRecovery

// NewIsolatedRecovery claims the orphaned worker launch records of one tenant.
//
// Takes config (IsolatedConfig) which names the tenant and state directory.
//
// Returns *IsolatedRecovery which releases the claims on Close.
// Returns error when the configuration is invalid or the claim fails.
func NewIsolatedRecovery(ctx context.Context, config IsolatedConfig) (*IsolatedRecovery, error) {
	return sandboxhost.NewIsolatedRecovery(ctx, config)
}

// IsolatedFilesystemRecovery claims and releases orphaned filesystem launch records.
type IsolatedFilesystemRecovery = sandboxhost.IsolatedFilesystemRecovery

// NewIsolatedFilesystemRecovery claims the orphaned filesystem launch records of one
// tenant.
//
// Takes config (IsolatedFilesystemConfig) which names the tenant and state directory.
//
// Returns *IsolatedFilesystemRecovery which releases the claims on Close.
// Returns error when the configuration is invalid or the claim fails.
func NewIsolatedFilesystemRecovery(ctx context.Context, config IsolatedFilesystemConfig) (*IsolatedFilesystemRecovery, error) {
	return sandboxhost.NewIsolatedFilesystemRecovery(ctx, config)
}

// IsolatedImages holds executables staged and hash-verified once, so repeated launches
// copy verified bytes instead of re-hashing them.
type IsolatedImages = sandboxhost.IsolatedImages

// IsolatedCleanupError reports a launch that failed and could not release its native
// resources. Retry releases them.
type IsolatedCleanupError = sandboxhost.IsolatedCleanupError

// PrepareIsolatedImages stages and verifies the launch executables once, so later
// launches copy verified bytes instead of re-hashing them.
//
// Takes config (IsolatedConfig) which names the executables to stage.
//
// Returns *IsolatedImages which the caller closes when the launches are done.
// Returns error when the configuration is invalid or staging fails.
func PrepareIsolatedImages(ctx context.Context, config IsolatedConfig) (*IsolatedImages, error) {
	return sandboxhost.PrepareIsolatedImages(ctx, config)
}
