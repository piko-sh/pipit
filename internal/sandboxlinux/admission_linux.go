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
	"sync"
)

// maximumAdmissionKeys bounds the tenants an admission table tracks at once.
const maximumAdmissionKeys = 256

// admissionTable keeps one admission gate per tenant, so one tenant's long session never
// starves another's launches; gates are dropped again once idle.
type admissionTable struct {
	// gates maps tenant keys to their admission gates.
	gates map[string]*admission

	// mutex guards gates.
	mutex sync.Mutex
}

var (
	// errAdmissionFull reports an occupied worker slot and its single waiting slot, or an
	// admission table that already tracks the maximum number of tenants.
	errAdmissionFull = errors.New("isolated worker admission queue is full")

	// workerAdmission is the process-wide table that bounds concurrent worker launches per
	// tenant.
	workerAdmission admissionTable
)

// acquire reserves the tenant's worker slot.
//
// Takes key (string) which identifies the tenant.
//
// Returns func() which releases the lease exactly once after resource cleanup.
// Returns error when cancelled, when both of the tenant's slots are occupied or when the
// table is full.
//
// Safe for concurrent use by multiple goroutines.
func (table *admissionTable) acquire(ctx context.Context, key string) (func(), error) {
	table.mutex.Lock()
	gate, ok := table.gates[key]
	if !ok {
		if len(table.gates) >= maximumAdmissionKeys {
			table.mutex.Unlock()
			return nil, errAdmissionFull
		}
		if table.gates == nil {
			table.gates = make(map[string]*admission)
		}
		gate = &admission{pending: nil, mutex: sync.Mutex{}, active: false}
		table.gates[key] = gate
	}
	table.mutex.Unlock()
	release, err := gate.acquire(ctx)
	if err != nil {
		table.dropIfIdle(key, gate)
		return nil, err
	}
	return sync.OnceFunc(func() {
		release()
		table.dropIfIdle(key, gate)
	}), nil
}

// dropIfIdle forgets a tenant's gate once nothing holds or awaits it.
//
// Takes key (string) which identifies the tenant.
// Takes gate (*admission) which must be the gate the table holds under key.
//
// Safe for concurrent use by multiple goroutines.
func (table *admissionTable) dropIfIdle(key string, gate *admission) {
	table.mutex.Lock()
	defer table.mutex.Unlock()
	if table.gates[key] != gate {
		return
	}
	gate.mutex.Lock()
	idle := !gate.active && gate.pending == nil
	gate.mutex.Unlock()
	if idle {
		delete(table.gates, key)
	}
}

// gate returns the tenant's live gate, or nil when none is tracked.
//
// Takes key (string) which identifies the tenant.
//
// Returns *admission which the table holds for the key at this moment.
//
// Safe for concurrent use by multiple goroutines.
func (table *admissionTable) gate(key string) *admission {
	table.mutex.Lock()
	defer table.mutex.Unlock()
	return table.gates[key]
}

// tracked reports how many tenants the table currently holds gates for.
//
// Returns int which is the live gate count.
//
// Safe for concurrent use by multiple goroutines.
func (table *admissionTable) tracked() int {
	table.mutex.Lock()
	defer table.mutex.Unlock()
	return len(table.gates)
}

// admission bounds process-wide worker creation to one active and one queued launch. A
// lease includes image preparation, execution and complete resource cleanup.
type admission struct {
	// pending holds the channel of a queued caller waiting for the active lease to be
	// released. Nil when no caller is waiting.
	pending chan struct{}

	// mutex guards pending and active.
	mutex sync.Mutex

	// active is true while a lease is held.
	active bool
}

// acquire reserves a worker slot without creating processes, images or cgroups.
//
// Returns func() which releases the lease exactly once after resource cleanup.
// Returns error when cancelled or when both admission slots are occupied.
//
// Safe for concurrent use by multiple goroutines.
func (gate *admission) acquire(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	gate.mutex.Lock()
	if !gate.active {
		gate.active = true
		gate.mutex.Unlock()
		return sync.OnceFunc(gate.release), nil
	}
	if gate.pending != nil {
		gate.mutex.Unlock()
		return nil, errAdmissionFull
	}
	waiting := make(chan struct{})
	gate.pending = waiting
	gate.mutex.Unlock()
	select {
	case <-waiting:
		if err := ctx.Err(); err != nil {
			gate.release()
			return nil, err
		}
		return sync.OnceFunc(gate.release), nil
	case <-ctx.Done():
		gate.mutex.Lock()
		transferred := gate.pending != waiting
		if !transferred {
			gate.pending = nil
		}
		gate.mutex.Unlock()
		if transferred {
			gate.release()
		}
		return nil, ctx.Err()
	}
}

// release transfers the active lease to the queued caller or makes it available.
//
// Safe for concurrent use by multiple goroutines.
func (gate *admission) release() {
	gate.mutex.Lock()
	defer gate.mutex.Unlock()
	if gate.pending != nil {
		close(gate.pending)
		gate.pending = nil
		return
	}
	gate.active = false
}
