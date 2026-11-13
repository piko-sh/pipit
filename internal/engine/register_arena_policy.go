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

// abandonsArenas reports whether the limits require arenas to be collected on release
// instead of pooled.
//
// Returns bool which is true when the VM must not reuse arena storage.
func (l *VMLimits) abandonsArenas() bool {
	return l.AbandonArenasOnRelease
}

// acquireRegisterArena returns a pooled arena for a VM running under limits, or creates a
// fresh arena when the VM must not reuse storage.
//
// Takes abandon (bool) which is VMLimits.abandonsArenas for the acquiring VM.
//
// Returns *RegisterArena which is ready for use.
func acquireRegisterArena(abandon bool) *RegisterArena {
	if abandon {
		return newUnpooledRegisterArena()
	}
	return GetRegisterArena()
}

// newUnpooledRegisterArena constructs an arena exactly as the pool does when it is empty,
// but never draws from or joins the pool, and with dead-chunk recycling off so the minor
// collection drops dead chunks instead of handing them out again.
//
// Returns *RegisterArena which is a fresh arena owned only by the caller.
func newUnpooledRegisterArena() *RegisterArena {
	arena := new(RegisterArena)
	initRegisterArenaInPlace(arena)
	arena.recycleChunks = false
	arena.unpooled = true
	return arena
}

// releaseRegisterArena ends a VM's use of arena.
//
// A heap-data arena goes to its own pool (releaseHeapDataArena). An arena that must not
// be reused is dropped to the garbage collector after its budget charge is surrendered
// and its slabs leave the slab and licence registries, rather than when its cleanup runs.
// Otherwise the arena is licensed and returns to the pool, its epoch first revoked when
// abandoningEscapes is set and a value may have escaped
// (putRegisterArenaAbandoningEscapes). An arena the full pool refuses is torn down in the
// same way (PutRegisterArena).
//
// Takes abandon (bool) which is VMLimits.abandonsArenas for the releasing VM.
// Takes arena (*RegisterArena) which may be nil.
// Takes abandoningEscapes (bool) which is true for re-entrant (callback and bound-method)
// VMs whose values may outlive the release.
func releaseRegisterArena(abandon bool, arena *RegisterArena, abandoningEscapes bool) {
	if arena == nil {
		return
	}
	if arena.unwindLeaked {
		arena.detachBudgetTracker()
		return
	}
	if arena.heapData {
		releaseHeapDataArena(abandon, arena)
		return
	}
	if abandon || arena.unpooled || arena.abandonAtRelease {
		abandonUnpooledRegisterArena(arena)
		return
	}
	if abandoningEscapes {
		putRegisterArenaAbandoningEscapes(arena)
		return
	}
	PutRegisterArena(arena)
}

// abandonUnpooledRegisterArena drops arena to the garbage collector. It handles an arena
// of the abandon fallback, one marked unpooled or abandonAtRelease, an aborted arena
// after its Reset, or a reset arena the pool refused (PutRegisterArena).
//
// It surrenders the arena's budget charge and removes its slabs from the slab and licence
// registries, with any foreign revocation pending against them, while the arena is still
// reachable, and stops the cleanup that would otherwise do so after it is collected.
//
// Takes arena (*RegisterArena) which must not be nil and is never used again.
func abandonUnpooledRegisterArena(arena *RegisterArena) {
	arena.detachBudgetTracker()
	if arena.slabOwner != 0 {
		forgetArenaSlabOwner(arena.slabOwner)
		arena.slabCleanup.Stop()
	}
}
