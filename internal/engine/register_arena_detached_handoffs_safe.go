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

//go:build safe || (js && wasm)

package engine

import (
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
)

// undo puts back each detached register that still holds its clone (a native result
// written into the same register is left alone).
//
// This build cannot compare a string's storage, so a string register is put back when it
// still holds the clone's contents; the original holds the same contents, so a native
// result equal to the clone is replaced by an equal string.
//
// Takes registers (*Registers) which holds the registers.
func (r *nativeArgumentRestore) undo(registers *Registers) {
	for _, entry := range r.strings[:r.nStrings] {
		if registers.Strings[entry.register] == entry.clone {
			registers.Strings[entry.register] = entry.original
		}
	}
	for _, entry := range r.generals[:r.nGeneral] {
		if registers.General[entry.register] == entry.clone {
			registers.General[entry.register] = entry.original
		}
	}
	r.reset()
}

// detachNativeStringArguments detaches nothing in this build, where no arena is reused
// without a licence (reusedWithoutLicence).
//
// Returns *nativeArgumentRestore which is nil.
func detachNativeStringArguments(_ *RegisterArena, _ *Registers, _ *program.CallSite, _ reflect.Value, _ *VM) *nativeArgumentRestore {
	return nil
}

// detachNativeMethodStringArguments detaches nothing in this build, where no arena is
// reused without a licence (reusedWithoutLicence).
func detachNativeMethodStringArguments(_ *RegisterArena, _ reflect.Value, _ []reflect.Value) {}
