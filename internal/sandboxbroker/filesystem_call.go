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
	"slices"

	"pipit.sh/pipit/internal/sandboxwire"
)

// FilesystemCall is an immutable authority decision and quota reservation. A backend must
// honour its exact operation, root, path and limit, then Finish once.
type FilesystemCall struct {
	// budget is the parent reservation ledger.
	budget *FilesystemBudget

	// finished is true after Finish releases the outstanding slot.
	finished *bool

	// request holds the authorised operation, root, path and limit.
	request filesystemRequest

	// identity is the transport call identifier for this reservation.
	identity uint64
}

// Operation returns the authorised operation.
//
// Returns Operation identifying the admitted action.
func (call *FilesystemCall) Operation() Operation { return call.request.operation }

// Finish releases an outstanding slot without refunding reserved resources. Finishing is
// still required after backend failure, using the actual consumed count.
//
// Takes used (int) counting transferred bytes or returned directory entries.
//
// Returns error for invalid accounting or an invalid reservation.
//
// Safe for concurrent use by multiple goroutines.
func (call *FilesystemCall) Finish(used int) error {
	if call == nil || call.budget == nil || call.finished == nil {
		return ErrClosed
	}
	budget := call.budget
	budget.mutex.Lock()
	defer budget.mutex.Unlock()
	if *call.finished {
		budget.closed = true
		return sandboxwire.ErrProtocol
	}
	*call.finished = true
	budget.outstanding--
	if used < 0 || used > call.request.limit {
		budget.closed = true
		return errLimit
	}
	return nil
}

// root returns the opaque root identifier, never a host pathname.
//
// Returns string with the named root selected by host policy.
func (call *FilesystemCall) root() string { return call.request.root }

// path returns the validated relative path without normalisation.
//
// Returns string with the approved path beneath the named root.
func (call *FilesystemCall) path() string { return call.request.path }

// limit returns the reserved byte count or directory entry count.
//
// Returns int with the worst-case reservation size.
func (call *FilesystemCall) limit() int { return call.request.limit }

// data returns an independent copy of the bounded write payload.
//
// Returns []byte which the caller may mutate freely.
func (call *FilesystemCall) data() []byte { return slices.Clone(call.request.data) }
