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

import (
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
)

// nativeArgumentRestoreSlots bounds the registers one native call restores; a call that
// detaches more leaves the rest holding their clones, which is equally correct.
const nativeArgumentRestoreSlots = 6

// nativeStringRestore is one string register a native call detached.
type nativeStringRestore struct {
	// original and clone hold the argument before and after materialisation.
	original, clone string

	// register identifies the argument register to restore after the native call.
	register uint8
}

// nativeGeneralRestore is one general register a native call detached.
type nativeGeneralRestore struct {
	// original and clone hold the argument before and after materialisation.
	original, clone reflect.Value

	// register identifies the argument register to restore after the native call.
	register uint8
}

// nativeArgumentRestore records the registers detachNativeStringArguments rewrote, so the
// guest's registers hold what they held before once the call returns.
type nativeArgumentRestore struct {
	// strings holds string argument registers and their restoration values.
	strings [nativeArgumentRestoreSlots]nativeStringRestore

	// generals holds general argument registers and their restoration values.
	generals [nativeArgumentRestoreSlots]nativeGeneralRestore

	// nStrings is the number of occupied string restoration slots.
	nStrings int

	// nGeneral is the number of occupied general restoration slots.
	nGeneral int
}

// vmArgumentRestores holds two scratch records for a VM.
type vmArgumentRestores struct {
	// nativeCopy and detach hold restoration records for copying and detaching native
	// arguments.
	nativeCopy, detach nativeArgumentRestore
}

// reusedWithoutLicence reports whether an unsafe-slab arena can return storage to a pool
// without a licence.
//
// Takes arena (*RegisterArena) which may be nil.
//
// Returns bool which is true when strings and arguments leaving arena are detached.
func reusedWithoutLicence(arena *RegisterArena) bool {
	return arenaUsesUnsafeSlabs && arena != nil && !arena.licensed && !arena.unpooled
}

// restoreRecord returns one of the VM's scratch records, empty, making the pair on first
// use.
//
// Takes nativeCopy (bool) which selects the native-copy record rather than the detach
// record.
//
// Returns *nativeArgumentRestore which is the record.
func (vm *VM) restoreRecord(nativeCopy bool) *nativeArgumentRestore {
	if vm.argumentRestores == nil {
		vm.argumentRestores = &vmArgumentRestores{}
	}
	record := &vm.argumentRestores.detach
	if nativeCopy {
		record = &vm.argumentRestores.nativeCopy
	}
	record.reset()
	return record
}

// reset empties the record, dropping its references to the strings and values it held, so
// a VM's scratch record pins nothing between calls.
func (r *nativeArgumentRestore) reset() {
	clear(r.strings[:r.nStrings])
	clear(r.generals[:r.nGeneral])
	r.nStrings, r.nGeneral = 0, 0
}

// detachArgumentsReusedWithoutLicence is detachNativeStringArguments for an arena reused
// without a licence, and nil for any other.
//
// Takes vm (*VM) which makes the call and owns the arena.
// Takes registers (*Registers) which holds the arguments.
// Takes site (*program.CallSite) which locates them.
//
// Returns *nativeArgumentRestore which holds the rewrites to undo, or nil.
func detachArgumentsReusedWithoutLicence(vm *VM, registers *Registers, site *program.CallSite) *nativeArgumentRestore {
	if !reusedWithoutLicence(vm.Arena) {
		return nil
	}
	return detachNativeStringArguments(vm.Arena, registers, site, registers.General[site.NativeRegister], vm)
}
