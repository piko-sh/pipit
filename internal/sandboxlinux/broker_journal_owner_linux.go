//go:build linux && (amd64 || arm64)

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

package sandboxlinux

import (
	"context"
	"errors"
	"slices"
	"sync"

	"pipit.sh/pipit/internal/sandboxbroker"
)

// journalledOwnerAdmission bounds concurrent journalled broker ownership to one active
// and one queued per host process.
var journalledOwnerAdmission admission

// filesystemBrokerRecovery tracks the journal, checkpoint and approval stores alongside
// the worker process for coordinated cleanup.
type filesystemBrokerRecovery struct {
	// base is the lifetime context with its cancellation and deadline stripped. Cleanup runs
	// after cancel() has fired, so it cannot derive a deadline from a live context, yet it
	// still needs the values the host attached, such as its logger.
	base context.Context

	// releaseAdmission frees the process-wide admission slot.
	releaseAdmission func()

	// cancel stops the lifetime context for this broker.
	cancel context.CancelFunc

	// authority holds the recovery verification and bootstrap state.
	authority *sandboxbroker.LinuxRecoveryAuthority

	// journal holds the durable write-ahead records for recovery.
	journal *sandboxbroker.LinuxRecoveryJournal

	// checkpoint holds persisted recovery checkpoints.
	checkpoint *sandboxbroker.LinuxRecoveryCheckpointStore

	// approval holds persisted recovery approvals.
	approval *sandboxbroker.LinuxRecoveryApprovalStore

	// parent is the optional aggregate group that owns this broker.
	parent *Group

	// process holds the recovery child process, if any.
	process *WorkerProcess

	// result caches the final joined error after complete cleanup.
	result error

	// config is the immutable worker configuration for this broker.
	config WorkerConfig

	// mutex guards closed and active registration.
	mutex sync.Mutex

	// cleanup serialises recovery cleanup attempts.
	cleanup sync.Mutex

	// active tracks in-progress filesystem operations.
	active sync.WaitGroup

	// closed is true once shutdown has begun.
	closed bool

	// complete is true once cleanup has finished successfully.
	complete bool
}

// beginOperation registers work before shutdown can wait for finished reservations.
//
// Returns false once closure has permanently stopped host admission.
//
// Safe for concurrent use by multiple goroutines.
func (state *filesystemBrokerRecovery) beginOperation() bool {
	state.mutex.Lock()
	defer state.mutex.Unlock()
	if state.closed {
		return false
	}
	state.active.Add(1)
	return true
}

// recover preserves every recovery process until its cleanup has succeeded.
//
// Takes original (*WorkerProcess) which is the matching original process owner.
//
// Returns success only after all intents have been checked by confined recovery.
func (state *filesystemBrokerRecovery) recover(ctx context.Context, original *WorkerProcess) error {
	if state.process != nil {
		if err := state.process.reapForRecovery(ctx); err != nil {
			return err
		}
		state.process = nil
	}
	if state.journal == nil || original == nil {
		return nil
	}
	records, err := state.journal.RecoveryRecords()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}
	bootstrap, err := state.authority.RecoveryBootstrap(state.journal)
	if err != nil {
		return err
	}
	state.process, err = launchFilesystemRecovery(ctx, state.config, original, bootstrap, state.parent)
	if failure := errors.Join(err, bootstrap.Close()); failure != nil {
		return failure
	}
	return sandboxbroker.ExchangeFilesystemRecovery(ctx, state.process)
}

// closeJournalled joins execution before serialising recoverable cleanup attempts.
//
// Returns original execution failures as well as recovery failures.
//
// Safe for concurrent use by multiple goroutines.
func (owner *FilesystemBroker) closeJournalled() error {
	state := owner.recovery
	state.mutex.Lock()
	state.closed = true
	state.mutex.Unlock()
	state.cleanup.Lock()
	defer state.cleanup.Unlock()
	if state.complete {
		return state.result
	}
	var execution error
	if owner.client != nil {
		execution = owner.client.Close()
	} else if owner.process != nil {
		execution = owner.process.Close()
	}
	if state.cancel != nil {
		state.cancel()
	}
	state.active.Wait()
	ctx, cancel := context.WithTimeout(state.base, workerCleanupTimeout)
	defer cancel()
	if owner.process != nil {
		if err := owner.process.reapForRecovery(ctx); err != nil {
			return errors.Join(execution, err)
		}
	}
	if err := state.recover(ctx, owner.process); err != nil {
		return errors.Join(execution, err)
	}
	state.complete = true
	state.result = errors.Join(execution, state.authority.Close(), state.journal.Close(), state.checkpoint.Close(), state.approval.Close())
	if state.releaseAdmission != nil {
		state.releaseAdmission()
	}
	return state.result
}

// OpenJournalledFilesystemBroker owns publication and supervised cleanup together. The
// journal directory must be durable, empty, private and on trusted local storage outside
// every script grant.
//
// Takes config (WorkerConfig) which is the approved configuration.
// Takes grants ([]sandboxbroker.LinuxRootGrant) which holds the host-approved root
// grants.
// Takes limits (sandboxbroker.FilesystemLimits) which selects the broker's cumulative
// quotas.
// Takes journalDirectory (string) which is the durable journal path on trusted local
// storage.
//
// Returns retained ownership on partial failure. Close every non-nil owner and retry
// failed cleanup; no success result should be exposed before Close succeeds.
func OpenJournalledFilesystemBroker(ctx context.Context, config WorkerConfig, grants []sandboxbroker.LinuxRootGrant,
	limits sandboxbroker.FilesystemLimits, journalDirectory string,
) (*FilesystemBroker, error) {
	return openJournalledFilesystemBroker(ctx, config, grants, limits, journalDirectory, nil, nil)
}

// openJournalledFilesystemBrokerInGroup retains one aggregate for work and recovery. Keep
// the parent alive until this owner has finished every cleanup attempt.
//
// Takes config (WorkerConfig) which is the approved policy.
// Takes grants ([]sandboxbroker.LinuxRootGrant) which holds the host-approved root
// grants.
// Takes limits (sandboxbroker.FilesystemLimits) which selects the broker's cumulative
// quotas.
// Takes journalDirectory (string) which is the private storage path on trusted local
// storage.
// Takes parent (*Group) which is the mandatory aggregate parent.
//
// Returns a retained owner without an independent-resource fallback.
func openJournalledFilesystemBrokerInGroup(ctx context.Context, config WorkerConfig, grants []sandboxbroker.LinuxRootGrant,
	limits sandboxbroker.FilesystemLimits, journalDirectory string, parent *Group,
) (*FilesystemBroker, error) {
	if parent == nil {
		return nil, ErrInvalidLimits
	}
	return openJournalledFilesystemBroker(ctx, config, grants, limits, journalDirectory, parent, nil)
}

// openJournalledFilesystemBroker derives execution and recovery from one bootstrap.
//
// Takes config (WorkerConfig) which is the immutable host policy.
// Takes grants ([]sandboxbroker.LinuxRootGrant) which holds the host-approved root
// grants.
// Takes limits (sandboxbroker.FilesystemLimits) which selects the broker's cumulative
// quotas.
// Takes journalDirectory (string) which is the durable journal path on trusted local
// storage.
// Takes parent (*Group) which is the optional aggregate parent.
// Takes storage (*FilesystemRecoveryStorage) which holds the optional recovery storage
// selection.
//
// Returns partial ownership without discarding the journal or pinned roots.
func openJournalledFilesystemBroker(ctx context.Context, config WorkerConfig, grants []sandboxbroker.LinuxRootGrant,
	limits sandboxbroker.FilesystemLimits, journalDirectory string, parent *Group, storage *FilesystemRecoveryStorage,
) (*FilesystemBroker, error) {
	grants = slices.Clone(grants)
	if err := checkBrokerImageStorage(ctx, config.ImageStore, grants, []string{journalDirectory}); err != nil {
		return nil, err
	}
	if config.Lifetime == 0 {
		config.Lifetime = defaultWorkerLifetime
	}
	if config.Lifetime < 0 {
		return nil, ErrInvalidLimits
	}
	ctx, cancel := context.WithTimeout(ctx, config.Lifetime)
	release, err := journalledOwnerAdmission.acquire(ctx)
	if err != nil {
		cancel()
		return nil, err
	}
	bootstrap, err := sandboxbroker.PrepareLinuxBootstrap(grants, limits)
	if err != nil {
		cancel()
		release()
		return nil, err
	}
	authority, err := bootstrap.RecoveryAuthority()
	if err != nil {
		err = errors.Join(err, bootstrap.Close())
		cancel()
		release()
		return nil, err
	}
	state := &filesystemBrokerRecovery{
		base: context.WithoutCancel(ctx), releaseAdmission: release, cancel: cancel,
		checkpoint: nil, approval: nil,
		authority: authority, journal: nil, parent: parent, process: nil, result: nil, config: config,
		mutex: sync.Mutex{}, cleanup: sync.Mutex{}, active: sync.WaitGroup{}, closed: false, complete: false,
	}
	owner := &FilesystemBroker{process: nil, client: nil, recovery: state}
	state.journal, err = sandboxbroker.OpenLinuxRecoveryJournal(journalDirectory, bootstrap.StagingNamespace())
	if err != nil {
		return owner, errors.Join(err, bootstrap.Close(), owner.Close())
	}
	if err := authority.ValidateJournal(state.journal); err != nil {
		return owner, errors.Join(err, bootstrap.Close(), owner.Close())
	}
	if storage != nil {
		if err := state.persistCheckpoint(*storage); err != nil {
			return owner, errors.Join(err, bootstrap.Close(), owner.Close())
		}
	}
	owner.process, err = launchFilesystemBroker(ctx, config, bootstrap, parent)
	if failure := errors.Join(err, bootstrap.Close()); failure != nil {
		return owner, errors.Join(failure, owner.Close())
	}
	owner.client, err = sandboxbroker.OpenJournalledFilesystemClient(ctx, owner.process, authority, state.journal)
	if err != nil {
		return owner, errors.Join(err, owner.Close())
	}
	return owner, nil
}
