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

package program

import (
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// kindByteDestShift is the bit shift placing the destination kind in the upper nibble of
	// the packed KindByte field. The source kind occupies the lower nibble; both kinds fit
	// in 4 bits because there are fewer than 16 register banks.
	kindByteDestShift = 4
)

// CallArgCopy is one entry of a per-callsite copy program. Sized to 4 bytes.
type CallArgCopy struct {
	// Op is the encoded register-bank operation for this argument.
	Op callArgCopyOp

	// SourceRegister is the index of the value in the source bank.
	SourceRegister uint8

	// DestinationRegister is the index of the value in the destination bank.
	DestinationRegister uint8

	// KindByte packs sourceKind (low nibble) and destinationKind (high nibble) for the
	// CopyBoxOrUnbox slow-path dispatch. Unused for same-kind ops (op = copy*To* with
	// matching banks).
	KindByte uint8
}

// callArgCopyOp encodes the exact register-bank operation needed for one argument copy.
// Same-kind copies (the overwhelmingly common case) are seven distinct ops;
// box/unbox/cross-kind cases fall through to a generic runtime handler.
type callArgCopyOp uint8

const (
	// CopyIntToInt copies ints[srcReg] -> ints[dstReg].
	CopyIntToInt callArgCopyOp = iota

	// CopyFloatToFloat copies floats[srcReg] -> floats[dstReg].
	CopyFloatToFloat

	// CopyStringToString copies strings[srcReg] -> strings[dstReg].
	CopyStringToString

	// CopyGeneralToGeneral copies general[srcReg] -> general[dstReg] via
	// ValueCopyForBoundary (preserves boundary semantics).
	CopyGeneralToGeneral

	// CopyBoolToBool copies bools[srcReg] -> bools[dstReg].
	CopyBoolToBool

	// CopyUintToUint copies uints[srcReg] -> uints[dstReg].
	CopyUintToUint

	// CopyComplexToComplex copies complex[srcReg] -> complex[dstReg].
	CopyComplexToComplex

	// CopySliceIntToSliceInt copies slicesInt[srcReg] -> slicesInt[dstReg].
	CopySliceIntToSliceInt

	// CopySliceFloatToSliceFloat copies slicesFloat[srcReg] -> slicesFloat[dstReg].
	CopySliceFloatToSliceFloat

	// CopySliceStringToSliceString copies slicesString[srcReg] -> slicesString[dstReg].
	CopySliceStringToSliceString

	// CopySliceBoolToSliceBool copies slicesBool[srcReg] -> slicesBool[dstReg].
	CopySliceBoolToSliceBool

	// CopySliceUintToSliceUint copies slicesUint[srcReg] -> slicesUint[dstReg].
	CopySliceUintToSliceUint

	// CopySliceByteToSliceByte copies slicesByte[srcReg] -> slicesByte[dstReg].
	CopySliceByteToSliceByte

	// CopyBoxOrUnbox handles every cross-kind copy, including boxing scalar into general and
	// unboxing general into scalar.
	CopyBoxOrUnbox
)

// BuildCallArgCopyProgram lays out a per-argument copy plan at compile time. Each entry
// resolves to a single bank-to-bank move at runtime with no kind-switching, kind-index
// tracking, or function calls in the same-kind common case.
//
// Takes arguments ([]VarLocation) which are the caller-side argument locations from the
// call site's site.Arguments.
// Takes parameterKinds ([]isa.RegisterKind) which is the callee's expected per-parameter
// destination banks.
// Takes parameterRegisters ([]uint8) which are the callee's pre-allocated per-parameter
// destination slots; a short or empty slice falls back to sequential per-bank allocation.
//
// Returns a []CallArgCopy of length min(len(arguments), len(parameterKinds)). Variadic
// and mismatched-arity sites trim to the callee's arity.
func BuildCallArgCopyProgram(arguments []VarLocation, parameterKinds []isa.RegisterKind, parameterRegisters []uint8) []CallArgCopy {
	n := min(len(arguments), len(parameterKinds))
	if n == 0 {
		return nil
	}

	if len(parameterRegisters) != len(parameterKinds) {
		return nil
	}
	program := make([]CallArgCopy, n)
	var kindIndex [isa.NumRegisterKinds]int
	for i := range n {
		argumentLocation := arguments[i]     //nolint:gosec // bounded by n above
		destinationKind := parameterKinds[i] //nolint:gosec // bounded by n above

		var destinationRegister int
		if i < len(parameterRegisters) {
			destinationRegister = int(parameterRegisters[i])
			kindIndex[destinationKind] = destinationRegister + 1
		} else {
			destinationRegister = kindIndex[destinationKind]
			kindIndex[destinationKind]++
		}
		program[i] = buildOneArgCopyEntry(argumentLocation, destinationKind, safeconv.MustIntToUint8(destinationRegister))
	}
	return program
}

// DetectTailCallArgsAlias reports whether a tail call's argument copy would suffer from
// source-destination aliasing when executed sequentially against a shared register file.
//
// Takes arguments ([]VarLocation) which are the caller-side argument locations.
// Takes parameterKinds ([]isa.RegisterKind) which is the callee's expected per-parameter
// destination banks.
//
// Returns true when at least one alias exists.
// Returns false when the sequential copy is safe.
func DetectTailCallArgsAlias(arguments []VarLocation, parameterKinds []isa.RegisterKind) bool {
	n := min(len(arguments), len(parameterKinds))
	var kindIndex [isa.NumRegisterKinds]int
	destinationPositions := make([]struct {
		kind isa.RegisterKind
		reg  int
	}, n)
	for i := range n {
		destinationKind := parameterKinds[i]
		destinationPositions[i].kind = destinationKind
		destinationPositions[i].reg = kindIndex[destinationKind]
		kindIndex[destinationKind]++
	}
	for i := range n {
		destination := destinationPositions[i]
		for j := i + 1; j < n; j++ {
			sourceLocation := arguments[j]
			if sourceLocation.Kind == destination.kind && int(sourceLocation.Register) == destination.reg {
				return true
			}
		}
	}
	return false
}

// buildOneArgCopyEntry produces a single CallArgCopy entry. Same-kind copies route
// through a per-bank opcode; mismatched kinds fall through to the generic box-or-unbox
// path.
//
// Takes argumentLocation (VarLocation) which holds the source register and kind.
// Takes destinationKind (isa.RegisterKind) which is the callee parameter bank.
// Takes destinationRegister (uint8) which is the callee-side slot.
//
// Returns CallArgCopy which describes the copy plan.
func buildOneArgCopyEntry(argumentLocation VarLocation, destinationKind isa.RegisterKind, destinationRegister uint8) CallArgCopy {
	entry := CallArgCopy{SourceRegister: argumentLocation.Register,
		DestinationRegister: destinationRegister, Op: 0, KindByte: 0}
	if argumentLocation.Kind != destinationKind {
		entry.Op = CopyBoxOrUnbox
		entry.KindByte = uint8(argumentLocation.Kind) | (uint8(destinationKind) << kindByteDestShift)
		return entry
	}
	op, ok := sameKindCopyOp(destinationKind)
	if !ok {
		entry.Op = CopyBoxOrUnbox
		entry.KindByte = uint8(argumentLocation.Kind) | (uint8(destinationKind) << kindByteDestShift)
		return entry
	}
	entry.Op = op
	return entry
}

// sameKindCopyOp returns the same-kind copy opcode for kind.
//
// Takes kind (isa.RegisterKind) which selects the bank's copy opcode.
//
// Returns callArgCopyOp which is the matching per-bank opcode.
// Returns bool which is false when no per-bank opcode exists (e.g. UpvalueKindAsPointer).
func sameKindCopyOp(kind isa.RegisterKind) (callArgCopyOp, bool) {
	switch kind {
	case isa.RegisterInt:
		return CopyIntToInt, true
	case isa.RegisterFloat:
		return CopyFloatToFloat, true
	case isa.RegisterString:
		return CopyStringToString, true
	case isa.RegisterGeneral:
		return CopyGeneralToGeneral, true
	case isa.RegisterBool:
		return CopyBoolToBool, true
	case isa.RegisterUint:
		return CopyUintToUint, true
	case isa.RegisterComplex:
		return CopyComplexToComplex, true
	case isa.RegisterSliceInt:
		return CopySliceIntToSliceInt, true
	case isa.RegisterSliceFloat:
		return CopySliceFloatToSliceFloat, true
	case isa.RegisterSliceString:
		return CopySliceStringToSliceString, true
	case isa.RegisterSliceBool:
		return CopySliceBoolToSliceBool, true
	case isa.RegisterSliceUint:
		return CopySliceUintToSliceUint, true
	case isa.RegisterSliceByte:
		return CopySliceByteToSliceByte, true
	case UpvalueKindAsPointer:
		return 0, false
	}
	return 0, false
}
