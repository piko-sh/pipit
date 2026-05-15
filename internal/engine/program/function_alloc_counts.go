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

import "pipit.sh/pipit/internal/isa"

const (
	// ConstMaskInt flags that the int64 constant pool is non-empty.
	ConstMaskInt uint16 = 1 << 0

	// ConstMaskFloat flags that the float64 constant pool is non-empty.
	ConstMaskFloat uint16 = 1 << 1

	// ConstMaskString flags that the string constant pool is non-empty.
	ConstMaskString uint16 = 1 << 2

	// ConstMaskBool flags that the bool constant pool is non-empty.
	ConstMaskBool uint16 = 1 << 3

	// ConstMaskStructLayoutTable flags that the struct-layout table is non-empty.
	ConstMaskStructLayoutTable uint16 = 1 << 4

	// ConstMaskTypeTable flags that the type table is non-empty.
	ConstMaskTypeTable uint16 = 1 << 5

	// constMaskComplex flags that the complex128 constant pool is non-empty.
	constMaskComplex uint16 = 1 << 6

	// ConstMaskUint flags that the uint64 constant pool is non-empty.
	ConstMaskUint uint16 = 1 << 7
)

// TypedSlabCounts bundles the minimum capacity required for every arena slab kind. Used
// by ensureCapacity so adding a new typed register bank only changes the struct layout,
// not the function signature.
type TypedSlabCounts struct {
	// Ints is the int64 register count for one frame.
	Ints int

	// Floats is the float64 register count for one frame.
	Floats int

	// Strings is the string register count for one frame.
	Strings int

	// Generals is the reflect.Value register count for one frame.
	Generals int

	// Bools is the bool register count for one frame.
	Bools int

	// Uints is the uint64 register count for one frame.
	Uints int

	// Complexes is the complex128 register count for one frame.
	Complexes int

	// SlicesInts is the []int64 slice-header register count.
	SlicesInts int

	// SlicesFloats is the []float64 slice-header register count.
	SlicesFloats int

	// SlicesStrings is the []string slice-header register count.
	SlicesStrings int

	// SlicesBools is the []bool slice-header register count.
	SlicesBools int

	// SlicesUints is the []uint64 slice-header register count.
	SlicesUints int

	// SlicesBytes is the []byte slice-header register count.
	SlicesBytes int

	// Bytes is the byte-slab budget for arena-routed string production. Populated from a
	// bytecode walk over byteSlab-writing opcodes.
	Bytes int

	// IntBacking is the budget for arena-routed []int64 storage (make([]int64, ...) +
	// append-grow). Populated from isa.SubOpMakeSliceInt counts.
	IntBacking int

	// FloatBacking is the budget for arena-routed []float64 storage.
	FloatBacking int

	// StringBacking is the budget for arena-routed []string storage.
	StringBacking int

	// BoolBacking is the budget for arena-routed []bool storage.
	BoolBacking int

	// UintBacking is the budget for arena-routed []uint64 storage.
	UintBacking int

	// GenericBytes is the byte budget for the snapshot slab. Fed from a bytecode walk of
	// composite-literal, array-snapshot, and make-slice opcodes.
	GenericBytes int

	// SliceHeaders is the slot budget for the slice-header slab. Each arena-routed
	// reflect.Value of kind Slice consumes one slot; growing the slab inside dispatch is
	// identical-cost to growing the byte slab.
	SliceHeaders int

	// FrameStack is the minimum frame-stack depth the arena must support without growing.
	// Computed from the static call graph via estimateMaxCallDepth; clamped at
	// maxFrameStackPreSize.
	FrameStack int

	// IntBoxes is the int64 box-slab slot budget. Charged by opPackInterface (registerInt
	// source) and opPackTyped (Int* destinations).
	IntBoxes int

	// UintBoxes is the uint64 box-slab slot budget.
	UintBoxes int

	// FloatBoxes is the float64 box-slab slot budget.
	FloatBoxes int

	// StringBoxes is the string box-slab slot budget.
	StringBoxes int

	// ComplexBoxes is the complex128 box-slab slot budget.
	ComplexBoxes int

	// UpvalueCells is the UpvalueCell slab slot budget. Charged by opMakeClosure occurrences
	// using the callee's upvalueDescriptor count.
	UpvalueCells int

	// UpvalueReferences is the upvalue reference slab slot budget. Tracks alongside
	// UpvalueCells; current ABI has them coincide but the field exists separately so future
	// shape changes do not require a hint-struct churn.
	UpvalueReferences int
}

// Precomputed per-frame allocation counts, split out of register_arena.go so the arena
// file declares methods on the arena only.

// EnsurePrecomputedAllocCounts lazily populates the per-function alloc cache. Idempotent
// and safe to call concurrently - the cache fields are only ever transitioned from zero
// to populated, and any torn read just falls through to the (correct) lazy path on the
// next call.
func (compiledFunction *CompiledFunction) EnsurePrecomputedAllocCounts() {
	if compiledFunction.PrecomputedAllocCountsValid {
		return
	}
	compiledFunction.PrecomputedAllocCounts = ArenaCountsFromNumRegs(compiledFunction.NumRegisters)
	compiledFunction.NonZeroBankMask = computeNonZeroBankMask(compiledFunction.NumRegisters)
	compiledFunction.NonEmptyConstantMask = computeNonEmptyConstantMask(compiledFunction)
	compiledFunction.PrecomputedAllocCountsValid = true
}

// ArenaCountsFromNumRegs unpacks a per-kind register-count array into the TypedSlabCounts
// struct used by the arena's allocation machinery.
//
// Takes numRegs ([NumRegisterKinds]uint32) which is the per-kind register count for the
// frame being allocated.
//
// Returns the equivalent TypedSlabCounts.
func ArenaCountsFromNumRegs(numRegs [isa.NumRegisterKinds]uint32) TypedSlabCounts {
	return TypedSlabCounts{Ints: int(numRegs[isa.RegisterInt]),
		Floats:            int(numRegs[isa.RegisterFloat]),
		Strings:           int(numRegs[isa.RegisterString]),
		Generals:          int(numRegs[isa.RegisterGeneral]),
		Bools:             int(numRegs[isa.RegisterBool]),
		Uints:             int(numRegs[isa.RegisterUint]),
		Complexes:         int(numRegs[isa.RegisterComplex]),
		SlicesInts:        int(numRegs[isa.RegisterSliceInt]),
		SlicesFloats:      int(numRegs[isa.RegisterSliceFloat]),
		SlicesStrings:     int(numRegs[isa.RegisterSliceString]),
		SlicesBools:       int(numRegs[isa.RegisterSliceBool]),
		SlicesUints:       int(numRegs[isa.RegisterSliceUint]),
		SlicesBytes:       int(numRegs[isa.RegisterSliceByte]),
		Bytes:             0,
		IntBacking:        0,
		FloatBacking:      0,
		StringBacking:     0,
		BoolBacking:       0,
		UintBacking:       0,
		GenericBytes:      0,
		SliceHeaders:      0,
		FrameStack:        0,
		IntBoxes:          0,
		UintBoxes:         0,
		FloatBoxes:        0,
		StringBoxes:       0,
		ComplexBoxes:      0,
		UpvalueCells:      0,
		UpvalueReferences: 0}
}

// computeNonEmptyConstantMask returns the bitmask of non-empty constant pools, used by
// rebuildDispatchPointers to skip empty-pool base-pointer writes.
//
// Takes compiledFunction (*CompiledFunction) which carries the constant pools and
// type/layout tables being inspected.
//
// Returns the OR of every constMask* bit whose backing pool is non-empty.
func computeNonEmptyConstantMask(compiledFunction *CompiledFunction) uint16 {
	var mask uint16
	if len(compiledFunction.IntConstants) > 0 {
		mask |= ConstMaskInt
	}
	if len(compiledFunction.FloatConstants) > 0 {
		mask |= ConstMaskFloat
	}
	if len(compiledFunction.StringConstants) > 0 {
		mask |= ConstMaskString
	}
	if len(compiledFunction.BoolConstants) > 0 {
		mask |= ConstMaskBool
	}
	if len(compiledFunction.StructLayoutTable) > 0 {
		mask |= ConstMaskStructLayoutTable
	}
	if len(compiledFunction.TypeTable) > 0 {
		mask |= ConstMaskTypeTable
	}
	if len(compiledFunction.ComplexConstants) > 0 {
		mask |= constMaskComplex
	}
	if len(compiledFunction.UintConstants) > 0 {
		mask |= ConstMaskUint
	}
	return mask
}

// computeNonZeroBankMask returns the bitmask of in-use register banks, where bit i is set
// when numRegisters[i] > 0.
//
// Takes numRegs ([NumRegisterKinds]uint32) which is the per-kind register count for the
// callee.
//
// Returns the OR of allocMask* bits for every bank with a non-zero count.
func computeNonZeroBankMask(numRegs [isa.NumRegisterKinds]uint32) uint16 {
	var mask uint16
	for k := range isa.RegisterKind(isa.NumRegisterKinds) {
		if numRegs[k] > 0 {
			mask |= uint16(1) << uint(k)
		}
	}
	return mask
}
