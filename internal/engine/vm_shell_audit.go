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

//go:build pipit_reuse_audit

package engine

import (
	"runtime"
	"sync"
	"weak"
)

// vmShellAuditEnabled makes recycle abandon each VM it would park and record a weak
// pointer to it, so AuditVMShells can tell whether anything outside the run still reaches
// the VM.
const vmShellAuditEnabled = true

// vmShellAuditMaxPasses caps the forced collections in one shell audit.
const vmShellAuditMaxPasses = 6

// vmShellAuditRecords holds the weak pointers of abandoned shells and of the callback
// caches they keep.
var vmShellAuditRecords struct {
	pointers []weak.Pointer[VM]
	caches   []weak.Pointer[callbackVMCache]
	mu       sync.Mutex
}

// auditRecordVMShell records vm, which recycle zeroed and abandons instead of parking.
//
// Takes vm (*VM) which is the zeroed VM.
//
// Concurrency: Appends to the shared weak-shell record list under vmShellAuditRecords.mu;
// the caller must have finished clearing the VM.
func auditRecordVMShell(vm *VM) {
	vmShellAuditRecords.mu.Lock()
	vmShellAuditRecords.pointers = append(vmShellAuditRecords.pointers, weak.Make(vm))
	if vm.callbackVMs != nil {
		vmShellAuditRecords.caches = append(vmShellAuditRecords.caches, weak.Make(vm.callbackVMs))
		vm.callbackVMs = nil
	}
	vmShellAuditRecords.mu.Unlock()
}

// AuditVMShells forces collections until the result is stable and reports how many of the
// VMs recycle would have parked, or of the callback caches they would have kept, are
// still reachable, then forgets them. A reachable one is a pinning site the VM shell pool
// missed.
//
// Returns int which is the number of shells recorded.
// Returns int which is the number of shells and caches still reachable.
//
// Concurrency: Snapshots and clears the shared shell record list under
// vmShellAuditRecords.mu; collection runs after releasing that mutex.
func AuditVMShells() (recorded, reachable int) {
	vmShellAuditRecords.mu.Lock()
	pointers, caches := vmShellAuditRecords.pointers, vmShellAuditRecords.caches
	vmShellAuditRecords.pointers, vmShellAuditRecords.caches = nil, nil
	vmShellAuditRecords.mu.Unlock()
	previous := -1
	for range vmShellAuditMaxPasses {
		runtime.GC()
		reachable = 0
		for _, pointer := range pointers {
			if pointer.Value() != nil {
				reachable++
			}
		}
		for _, cache := range caches {
			if cache.Value() != nil {
				reachable++
			}
		}
		if reachable == previous {
			break
		}
		previous = reachable
	}
	return len(pointers), reachable
}
