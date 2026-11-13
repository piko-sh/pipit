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

//go:build !safe && !(js && wasm)

package engine

import (
	"reflect"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// byteViewArgumentBits is the number of arguments a call site's byte-view mask can mark.
const byteViewArgumentBits = 64

// byteViewLookahead bounds the conversions a byte view may skip to reach its call.
const byteViewLookahead = 8

// byteViewFor decides a CONVERT_STRING_TO_BYTES conversion for a reads-only native.
//
// When the instructions after it are only further conversions and then the native call
// whose site marks this register as a []byte(text) argument, and the callee is
// reads-only, the conversion's value is the string's own bytes.
//
// Takes vm (*VM) which runs the conversion.
// Takes frame (*CallFrame) whose program counter is past the conversion.
// Takes registers (*Registers) which hold the callee.
// Takes destination (uint8) which is the conversion's general register.
// Takes text (string) which is the converted string.
//
// Returns reflect.Value which is the argument.
// Returns bool which is false when the conversion must copy as Go does.
func byteViewFor(vm *VM, frame *CallFrame, registers *Registers, destination uint8, text string) (reflect.Value, bool) {
	site, ok := byteViewSite(frame, destination)
	if !ok {
		return reflect.Value{}, false
	}
	function := registers.General[site.NativeRegister]
	if !vm.Arena.nativeReadsOnly(function) {
		if licenceCounting {
			hostDeclarationCounters.viewFallbacks.Add(1)
		}
		return reflect.Value{}, false
	}
	if arenaAuditEnabled {
		if copied, audited := auditReadsOnlyArgument(vm, function, text); audited {
			return copied, true
		}
	}
	if licenceCounting {
		hostDeclarationCounters.views.Add(1)
	}
	if len(text) == 0 {
		return reflect.ValueOf([]byte{}), true
	}
	return reflect.ValueOf(unsafe.Slice(unsafe.StringData(text), len(text))), true
}

// auditReadsOnlyArgument prepares the audit build's honesty check.
//
// The first call of a reads-only native is passed a copy, compared with the string
// afterwards; later calls are passed the string's bytes and checksummed around the call.
//
// Takes vm (*VM) which makes the call.
// Takes function (reflect.Value) which is the reads-only callee.
// Takes text (string) which is the converted string.
//
// Returns reflect.Value which is the copy to pass.
// Returns bool which is true on a first call.
func auditReadsOnlyArgument(vm *VM, function reflect.Value, text string) (reflect.Value, bool) {
	pointer := function.Pointer()
	check := readsOnlyCheck{function: function, original: text, view: nil, copied: nil, sum: 0}
	_, attested := readsOnlyAttested.load(pointer)
	if !attested {
		check.copied = []byte(text)
		appendReadsOnlyCheck(vm, check)
		return reflect.ValueOf(check.copied), true
	}
	if len(text) > 0 {
		check.view = unsafe.Slice(unsafe.StringData(text), len(text))
	}
	check.sum = checksumBytes(check.view)
	appendReadsOnlyCheck(vm, check)
	if licenceCounting {
		hostDeclarationCounters.checksums.Add(1)
	}
	return reflect.Value{}, false
}

// byteViewSite finds a SubOpCallNative within byteViewLookahead instructions, skipping
// only further conversions.
//
// Takes frame (*CallFrame) whose program counter is past the conversion.
// Takes destination (uint8) which is the conversion's general register.
//
// Returns *program.CallSite which is the call's site.
// Returns bool which is false otherwise.
func byteViewSite(frame *CallFrame, destination uint8) (*program.CallSite, bool) {
	if frame == nil || frame.Function == nil {
		return nil, false
	}
	body := frame.Function.Body
	programCounter := frame.ProgramCounter
	for range byteViewLookahead {
		if programCounter < 0 || programCounter >= len(body) {
			return nil, false
		}
		instruction := body[programCounter]
		if instruction.Op != isa.OpDrillTier1 {
			return nil, false
		}
		switch isa.SubOpcode(instruction.A) {
		case isa.SubOpConvertStringToBytes:
			if instruction.B == destination {
				return nil, false
			}
			programCounter++
			continue
		case isa.SubOpCallNative:
			return byteViewCallSite(frame.Function, instruction, destination)
		default:
			return nil, false
		}
	}
	return nil, false
}

// byteViewCallSite returns a native call's site when it takes destination as a byte-view
// argument.
//
// Takes function (*program.CompiledFunction) which holds the call sites.
// Takes instruction (isa.Instruction) which is the SubOpCallNative.
// Takes destination (uint8) which is the conversion's general register.
//
// Returns *program.CallSite which is the call's site, or nil.
// Returns bool which is false when the site is out of range or does not take it.
func byteViewCallSite(function *program.CompiledFunction, instruction isa.Instruction, destination uint8) (*program.CallSite, bool) {
	index := int(isa.JoinWide(instruction.B, instruction.C))
	if index >= len(function.CallSites) {
		return nil, false
	}
	site := &function.CallSites[index]
	if !byteViewSiteTakes(site, destination) {
		return nil, false
	}
	return site, true
}

// byteViewSiteTakes reports whether a native call site marks destination, held in a
// general register of its own, as a byte-view argument.
//
// Takes site (*program.CallSite) which is the call's site.
// Takes destination (uint8) which is the conversion's general register.
//
// Returns bool which is true when the site takes the conversion as a byte view.
func byteViewSiteTakes(site *program.CallSite, destination uint8) bool {
	if !site.IsNative || site.IsEllipsisSpread || len(site.LinkedTypeArgs) > 0 {
		return false
	}
	for argument, location := range site.Arguments {
		if argument < byteViewArgumentBits && site.ByteViewArguments&(1<<argument) != 0 &&
			location.Kind == isa.RegisterGeneral && location.Register == destination && !location.IsSpilled && !location.IsIndirect && !location.IsUpvalue {
			return true
		}
	}
	return false
}

// appendReadsOnlyCheck queues check for vm's next native call.
//
// Takes vm (*VM) which makes the call.
// Takes check (readsOnlyCheck) which is the check.
func appendReadsOnlyCheck(vm *VM, check readsOnlyCheck) {
	checks, _ := readsOnlyPending.load(vm)
	readsOnlyPending.store(vm, append(checks, check))
}
