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

package engine

// requestCheckpoint raises a checkpoint flag for the next dispatch safepoint. Inside the
// assembly dispatcher it also spends the poll budget, so the next back-edge poll returns
// to Go and the safepoint comes within one loop iteration, not one poll interval.
//
// Takes flag (uint8) which is the checkpoint bit to raise.
func (vm *VM) requestCheckpoint(flag uint8) {
	vm.checkpointFlags |= flag
	if budget := vm.activePollBudget; budget != nil {
		*budget = 0
	}
}

// runPendingCheckpoints services pending dispatch-loop checkpoint signals.
//
// It dispatches the arena GC trigger when the pending bit is set or the allocation
// threshold is exceeded. In a licensed arena a pending bit raised by a grow past the
// budget also checks the budget once the collection has run, panicking when the arena is
// still over.
func (vm *VM) runPendingCheckpoints() {
	arena := vm.Arena
	if arena == nil {
		return
	}
	if abortInjectBuilt && abortInjectFires(abortSiteSafepointBudget) {
		arena.raiseAbort(abortInjectedError(abortSiteSafepointBudget))
	}
	pending := vm.checkpointFlags&checkpointFlagGCPending != 0
	if !arena.disableMinorGC && (pending || arena.gcShouldRun()) {
		vm.checkpointFlags &^= checkpointFlagGCPending
		arena.MinorGC(vm)
		if arenaAuditEnabled {
			auditMidRunCheckpoint()
		}
	}
	if arena.licensed && pending {
		vm.checkpointFlags &^= checkpointFlagGCPending
		arena.enforceBudgetAtSafepoint()
	}
}
