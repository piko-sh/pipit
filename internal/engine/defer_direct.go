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

	"pipit.sh/pipit/internal/isa"
)

const (
	// directDeferSlotsPerKind is the number of arguments of one register kind a trivial
	// defer can capture bank to bank; a defer needing more of one kind uses the reflect
	// path.
	directDeferSlotsPerKind = 4
)

// TryCaptureDirect captures deferred arguments bank to bank into direct slots.
//
// Consumes the argument extension words. Arena-owned strings are cloned and general
// values detached exactly as the reflect path does, because the frame's arena region may
// be reclaimed before the deferred call runs. Returns false when any argument is of a
// typed-slice kind or when more than directDeferSlotsPerKind arguments share a kind, so
// the caller can take the reflect path.
//
// Takes vm (*VM) which provides the arena for string and value detachment.
// Takes frame (*CallFrame) which supplies the extension words.
// Takes registers (*Registers) which holds the argument values.
// Takes argumentCount (int) which is the number of arguments to capture.
//
// Returns bool which is true when every argument was captured directly.
func (r *SimpleDeferRecord) TryCaptureDirect(vm *VM, frame *CallFrame, registers *Registers, argumentCount int) bool {
	if argumentCount > isa.MaxTrivialDeferArgs {
		return false
	}
	var used [isa.NumRegisterKinds]uint8
	body := frame.Function.Body
	for i := range argumentCount {
		kind := isa.RegisterKind(body[frame.ProgramCounter+i].C)
		if !directDeferKind(kind) || used[kind] >= directDeferSlotsPerKind {
			return false
		}
		r.argumentKinds[i] = kind
		r.argumentSlots[i] = used[kind]
		used[kind]++
	}
	r.ensureDirectBanks()
	for i := range argumentCount {
		extensionWord := body[frame.ProgramCounter]
		frame.ProgramCounter++
		kind := r.argumentKinds[i]
		slot := int(r.argumentSlots[i])
		copySameKindArg(&r.banks, registers, kind, slot, extensionWord.B)
		switch kind {
		case isa.RegisterString:
			r.banks.Strings[slot] = materialiseString(vm.Arena, r.banks.Strings[slot])
		case isa.RegisterGeneral:
			if vm.Arena != nil {
				r.banks.General[slot] = materialiseArenaValueAliasing(vm.Arena, r.banks.General[slot])
			}
		default:
		}
	}
	r.direct = true
	r.ArgumentCount = uint8(argumentCount) //nolint:gosec // bounded by maxTrivialDeferArgs above
	return true
}

// ClearDirect releases the direct slots after placement so captured strings and values do
// not outlive the deferred call.
func (r *SimpleDeferRecord) ClearDirect() {
	r.direct = false
	r.ArgumentCount = 0
	clear(r.directStrings[:])
	clear(r.directGenerals[:])
}

// ensureDirectBanks points the record's bank view at its direct slots.
func (r *SimpleDeferRecord) ensureDirectBanks() {
	if r.banks.Ints != nil {
		return
	}
	r.banks = Registers{Ints: r.directInts[:],
		Floats:  r.directFloats[:],
		Strings: r.directStrings[:],
		General: r.directGenerals[:],
		Bools:   r.directBools[:],
		Uints:   r.directUints[:],
		Complex: r.directComplexes[:], SlicesInt: nil, slicesFloat: nil, slicesString: nil, slicesBool: nil, slicesUint: nil, slicesByte: nil, lastAllocMask: 0}
}

// trivialDeferClosure reports whether a trivial defer's target is a compiled closure, the
// only target kind whose arguments can be placed bank to bank.
//
// Takes target (reflect.Value) which is the deferred callable.
//
// Returns the closure and true, or false for native or invalid targets.
func trivialDeferClosure(target reflect.Value) (*RuntimeClosure, bool) {
	if !target.IsValid() {
		return nil, false
	}
	return reflect.TypeAssert[*RuntimeClosure](target)
}

// directDeferKind reports whether values of kind can be captured into the record's direct
// slots. Typed-slice banks keep the reflect path.
//
// Membership test for the banks a direct defer record can capture. Every other kind is
// correctly false and the defer falls back to the boxed record.
//
// Takes kind (isa.RegisterKind) which is the argument's register bank.
//
// Returns bool which is true for the scalar, string and general banks.
func directDeferKind(kind isa.RegisterKind) bool {
	switch kind {
	case isa.RegisterInt, isa.RegisterFloat, isa.RegisterString, isa.RegisterGeneral, isa.RegisterBool, isa.RegisterUint, isa.RegisterComplex:
		return true
	default:
		return false
	}
}

// placeDirectDeferArgs copies a direct record's captured arguments into the deferred
// callee's frame, one bank slot per positional parameter with per-kind counters exactly
// as placeReflectArgs does, converting between banks through CopyOneCallArgument where a
// captured kind differs from the parameter kind.
//
// Takes regs (*Registers) which is the callee frame's register set.
// Takes record (*SimpleDeferRecord) which holds the captured arguments.
// Takes parameterKinds ([]isa.RegisterKind) which lists the callee's parameter banks.
// Takes arena (*RegisterArena) which receives boxing allocations.
func placeDirectDeferArgs(regs *Registers, record *SimpleDeferRecord, parameterKinds []isa.RegisterKind, arena *RegisterArena) {
	var kindIndex [isa.NumRegisterKinds]int
	for i := range int(record.ArgumentCount) {
		if i >= len(parameterKinds) {
			break
		}
		parameterKind := parameterKinds[i]
		dest := kindIndex[parameterKind]
		kindIndex[parameterKind]++
		copyOneCallArgument(regs, &record.banks, parameterKind, record.argumentKinds[i], dest, record.argumentSlots[i], arena)
	}
}
