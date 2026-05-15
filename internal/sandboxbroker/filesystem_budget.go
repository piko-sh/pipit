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

package sandboxbroker

import (
	"sync"

	"pipit.sh/pipit/internal/sandboxwire"
)

// Rights contains the host-approved operations for exactly one named root.
type Rights uint8

const (
	// Read permits file reads, independently of writes and directory enumeration.
	Read Rights = 1 << iota

	// Write permits staged file writes, independently of reads and enumeration.
	Write

	// List permits bounded directory enumeration without granting file reads.
	List

	// maximumRoots is the upper bound on named roots per budget.
	maximumRoots = 64

	// defaultCalls is the default cumulative call quota.
	defaultCalls = 100

	// defaultOutstanding is the default concurrent reservation limit.
	defaultOutstanding = 4

	// defaultReadBytes is the default cumulative read byte quota.
	defaultReadBytes = 4 << 20

	// defaultWriteBytes is the default cumulative write byte quota.
	defaultWriteBytes = 4 << 20

	// defaultEntries is the default cumulative directory entry quota.
	defaultEntries = 4096
)

// RootGrant names authority already selected by the host. Native backends must separately
// bind each name to a pinned root descriptor.
type RootGrant struct {
	// Name is the opaque root name selected by the host.
	Name string

	// Rights holds the approved operations for this root.
	Rights Rights
}

// FilesystemLimits selects finite cumulative quotas for one broker budget. Zero selects
// defaults; positive values may reduce but not exceed those defaults.
type FilesystemLimits struct {
	// Calls is the total number of admitted requests.
	Calls int

	// Outstanding is the maximum concurrent reservations.
	Outstanding int

	// ReadBytes is the cumulative read byte allowance.
	ReadBytes int

	// WriteBytes is the cumulative write byte allowance.
	WriteBytes int

	// ListEntries is the cumulative directory entry allowance.
	ListEntries int
}

// FilesystemBudget authorises requests and reserves resources before any privileged I/O.
// It performs no filesystem access and does not establish a filesystem security boundary.
type FilesystemBudget struct {
	// roots maps each named root to its approved rights.
	roots map[string]Rights

	// limits holds the immutable cumulative quotas.
	limits FilesystemLimits

	// lastID is the most recently admitted call identifier.
	lastID uint64

	// calls counts how many requests have been admitted so far.
	calls int

	// outstanding counts currently active reservations.
	outstanding int

	// readRemaining tracks the remaining read byte allowance.
	readRemaining int

	// writeRemaining tracks the remaining write byte allowance.
	writeRemaining int

	// entriesRemaining tracks the remaining directory entry allowance.
	entriesRemaining int

	// mutex guards all mutable budget state.
	mutex sync.Mutex

	// closed is true after Close permanently prevents new reservations.
	closed bool
}

// NewFilesystemBudget copies and validates host grants and finite reservation quotas.
//
// Takes grants ([]RootGrant) which are the host-approved root authority entries.
// Takes limits (FilesystemLimits) which are the finite cumulative quotas selected only by
// the host.
//
// Returns a private budget, or an error without accepting any worker input.
func NewFilesystemBudget(grants []RootGrant, limits FilesystemLimits) (*FilesystemBudget, error) {
	if len(grants) > maximumRoots {
		return nil, ErrInvalidPolicy
	}
	for _, setting := range []struct {
		value    *int
		fallback int
	}{
		{value: &limits.Calls, fallback: defaultCalls},
		{value: &limits.Outstanding, fallback: defaultOutstanding},
		{value: &limits.ReadBytes, fallback: defaultReadBytes},
		{value: &limits.WriteBytes, fallback: defaultWriteBytes},
		{value: &limits.ListEntries, fallback: defaultEntries},
	} {
		if *setting.value < 0 || *setting.value > setting.fallback {
			return nil, ErrInvalidPolicy
		}
		if *setting.value == 0 {
			*setting.value = setting.fallback
		}
	}
	if limits.Outstanding > limits.Calls {
		return nil, ErrInvalidPolicy
	}
	roots := make(map[string]Rights, len(grants))
	for _, grant := range grants {
		if !validRootName(grant.Name) || grant.Rights == 0 || grant.Rights & ^(Read|Write|List) != 0 {
			return nil, ErrInvalidPolicy
		}
		if _, exists := roots[grant.Name]; exists {
			return nil, ErrInvalidPolicy
		}
		roots[grant.Name] = grant.Rights
	}
	return &FilesystemBudget{roots: roots, limits: limits, lastID: 0, calls: 0, outstanding: 0,
		readRemaining: limits.ReadBytes, writeRemaining: limits.WriteBytes,
		entriesRemaining: limits.ListEntries, mutex: sync.Mutex{}, closed: false}, nil
}

// Admit validates and reserves a request before the caller performs any I/O. All
// attempts, including denied and busy requests, consume the call quota.
//
// Takes message (sandboxwire.Message) which must be a validated broker Call frame.
//
// Returns a private reservation or an error without granting an operation.
//
// Safe for concurrent use by multiple goroutines.
func (budget *FilesystemBudget) Admit(message sandboxwire.Message) (*FilesystemCall, error) {
	if budget == nil {
		return nil, ErrClosed
	}
	budget.mutex.Lock()
	defer budget.mutex.Unlock()
	if budget.closed {
		return nil, ErrClosed
	}
	if message.Kind != sandboxwire.Call || message.ID <= budget.lastID {
		budget.closed = true
		return nil, sandboxwire.ErrProtocol
	}
	budget.lastID = message.ID
	if budget.calls == budget.limits.Calls {
		budget.closed = true
		return nil, errLimit
	}
	budget.calls++
	request, err := decodeFilesystemRequest(message)
	if err != nil {
		budget.closed = true
		return nil, err
	}
	if !budget.authorised(request) {
		return nil, ErrDenied
	}
	if budget.outstanding == budget.limits.Outstanding {
		return nil, errBusy
	}
	if err := budget.reserve(request); err != nil {
		return nil, err
	}
	budget.outstanding++
	return &FilesystemCall{budget: budget, request: request, finished: new(bool), identity: message.ID}, nil
}

// Close permanently prevents new reservations. Existing callers must still finish their
// native operations and reservations.
//
// Safe for concurrent use by multiple goroutines.
func (budget *FilesystemBudget) Close() {
	if budget == nil {
		return
	}
	budget.mutex.Lock()
	defer budget.mutex.Unlock()
	budget.closed = true
}

// authorised checks exactly the requested right without implicit rights inheritance.
//
// Takes request (filesystemRequest) which passed domain validation.
//
// Returns true only for the named root and the requested operation.
func (budget *FilesystemBudget) authorised(request filesystemRequest) bool {
	right := Read
	switch request.operation {
	case WriteFile:
		right = Write
	case ListDirectory:
		right = List
	case ReadFile:
	}
	return budget.roots[request.root]&right != 0
}

// reserve deducts the worst-case operation size without refundable credit.
//
// Takes request (filesystemRequest) containing a bounded byte or entry limit.
//
// Returns error before any quota can become negative.
func (budget *FilesystemBudget) reserve(request filesystemRequest) error {
	remaining := &budget.readRemaining
	switch request.operation {
	case WriteFile:
		remaining = &budget.writeRemaining
	case ListDirectory:
		remaining = &budget.entriesRemaining
	case ReadFile:
	}
	if request.limit > *remaining {
		return errLimit
	}
	*remaining -= request.limit
	return nil
}
