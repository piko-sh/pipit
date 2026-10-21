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
	"sync/atomic"
	"unsafe"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// fusedEvalChildren is the number of child operands a fused evaluator shape carries: the
	// two operands of a binary combiner plus the third arm of a conditional.
	fusedEvalChildren = 3

	// maxFusedEvalDepth bounds the recursion depth so pathological trees fall back to framed
	// execution.
	maxFusedEvalDepth = 200

	// fusedEnvWideStrideBytes is the 64-bit element width the fused environment reader
	// supports.
	fusedEnvWideStrideBytes = 8

	// fusedEnvNarrowStrideBytes is the 32-bit element width the fused environment reader
	// supports.
	fusedEnvNarrowStrideBytes = 4
)

// fusedEnv captures the environment slice argument the shaped evaluators thread through
// every child call, pre-resolved to a raw base pointer so leaf reads are two loads.
type fusedEnv struct {
	// data is the element backing base pointer.
	data unsafe.Pointer

	// length is the element count for bounds checks.
	length int

	// wide is true when elements are 8-byte bank slots ([]uint64 widened) rather than the
	// 4-byte elements of a real []uint32.
	wide bool
}

// at reads the environment element at the given position.
//
// Takes index (int64) which is the element position.
//
// Returns uint64 which is the element value.
// Returns bool which is false when the index is out of range.
func (e *fusedEnv) at(index int64) (uint64, bool) {
	if index < 0 || index >= int64(e.length) {
		return 0, false
	}
	if e.wide {
		return *(*uint64)(unsafe.Add(e.data, uintptr(index)*8)), true
	}
	return uint64(*(*uint32)(unsafe.Add(e.data, uintptr(index)*4))), true
}

// fusedResolveChildCallee resolves a child receiver type word through the call site's
// inline cache.
//
// Takes site (*program.CallSite) which owns the inline cache entries.
// Takes typeWord (uintptr) which is the receiver's type word.
//
// Returns *program.CompiledFunction which is the resolved callee, or nil on a cache miss.
func fusedResolveChildCallee(site *program.CallSite, typeWord uintptr) *program.CompiledFunction {
	if typeWord != 0 && typeWord == site.LastReceiverTypeWord {
		return site.LastReceiverCallee
	}
	for slotIndex := range site.MethodICSlots {
		pointer := atomic.LoadPointer(&site.MethodICSlots[slotIndex])
		if pointer == nil {
			continue
		}
		entry := (*program.MonomorphicCacheEntry)(pointer)
		if entry.ReceiverTypeWord == typeWord && typeWord != 0 {
			site.LastReceiverTypeWord = typeWord
			site.LastReceiverCallee = entry.Callee
			return entry.Callee
		}
	}
	return nil
}

// fusedChildEface reads an interface child at the given layout offset from a receiver
// base pointer.
//
// Takes base (unsafe.Pointer) which is the receiver struct base.
// Takes layout (program.StructFieldLayout) which describes the child field offset and
// type.
//
// Only a pointer child is taken. A channel, a map and a function are held in the data
// word too, but the word read as the element type means something else in each of their
// types, so the callee would be looked up under a type the child does not have.
//
// Returns uintptr which is the held element type word.
// Returns unsafe.Pointer which is the held data pointer.
// Returns bool which is false when the child is nil or not a pointer.
func fusedChildEface(base unsafe.Pointer, layout program.StructFieldLayout) (uintptr, unsafe.Pointer, bool) {
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	heldType := *(*unsafe.Pointer)(fieldPointer)
	if heldType == nil {
		return 0, nil, false
	}
	heldKind := reflect.Kind(*(*uint8)(unsafe.Add(heldType, program.AbiTypeKindByteOffset)) & uint8(flagKindMask))
	if heldKind != reflect.Pointer {
		return 0, nil, false
	}
	pointerWord := *(*unsafe.Pointer)(unsafe.Add(fieldPointer, program.EfaceDataPointerOffset))
	elemWord := *(*uintptr)(unsafe.Add(heldType, program.AbiTypePointerElemOffset))
	return elemWord, pointerWord, true
}

// mask32 truncates a fused-eval operand to 32 bits.
//
// Takes value (uint64) which is the operand to truncate.
//
// Returns uint32 which is the low 32 bits of value.
func mask32(value uint64) uint32 {
	return uint32(value) //nolint:gosec // deliberate truncation
}

// fusedEvalNode evaluates one node of the expression tree.
//
// Takes callee (*program.CompiledFunction) which is the node's compiled method.
// Takes data (unsafe.Pointer) which is the receiver struct base.
// Takes env (*fusedEnv) which is the shared environment slice.
// Takes depth (int) which is the current recursion depth.
//
// Returns uint64 which is the evaluated result.
// Returns bool which is false when any gate fails and the caller should use the framed
// path.
func fusedEvalNode(callee *program.CompiledFunction, data unsafe.Pointer, env *fusedEnv, depth int) (uint64, bool) {
	if depth > maxFusedEvalDepth || data == nil {
		return 0, false
	}
	if callee.TinyLeafShape != program.TinyLeafNone {
		return fusedEvalLeaf(callee, data, env)
	}
	classifyFusedEvalShape(callee)
	switch callee.EvalShape {
	case program.FusedEvalAddMasked, program.FusedEvalSubMasked, program.FusedEvalMulMasked, program.FusedEvalMin, program.FusedEvalMax:
		left, ok := fusedEvalChild(callee, callee.EvalChildALayout, callee.EvalSiteA, data, env, depth)
		if !ok {
			return 0, false
		}
		right, ok := fusedEvalChild(callee, callee.EvalChildBLayout, callee.EvalSiteB, data, env, depth)
		if !ok {
			return 0, false
		}
		return fusedCombineBinary(callee.EvalShape, left, right), true
	case program.FusedEvalModGuarded, program.FusedEvalModGuardedWhole:
		return fusedEvalModGuardedNode(callee, data, env, depth)
	case program.FusedEvalIfPos:
		return fusedEvalIfPosNode(callee, data, env, depth)
	default:
	}
	return 0, false
}

// fusedEvalModGuardedNode evaluates the guarded-modulo family.
//
// Takes callee (*program.CompiledFunction) which is the node's compiled method.
// Takes data (unsafe.Pointer) which is the receiver struct base.
// Takes env (*fusedEnv) which is the shared environment slice.
// Takes depth (int) which is the current recursion depth.
//
// Returns uint64 which is the modulo result, or zero when the divisor is zero.
// Returns bool which is false on a gate failure.
func fusedEvalModGuardedNode(callee *program.CompiledFunction, data unsafe.Pointer, env *fusedEnv, depth int) (uint64, bool) {
	divisor, ok := fusedEvalChild(callee, callee.EvalChildALayout, callee.EvalSiteA, data, env, depth)
	if !ok {
		return 0, false
	}
	if divisor == 0 {
		return 0, true
	}
	dividend, ok := fusedEvalChild(callee, callee.EvalChildBLayout, callee.EvalSiteB, data, env, depth)
	if !ok {
		return 0, false
	}
	return fusedGuardedRemainder(callee.EvalShape, dividend, divisor), true
}

// fusedGuardedRemainder takes the remainder the guarded-modulo family returns once its
// divisor is known not to be zero.
//
// Takes shape (program.FusedEvalShape) which says whether the body truncates.
// Takes dividend (uint64) which is the whole dividend.
// Takes divisor (uint64) which is the whole divisor, not zero.
//
// Returns uint64 which is the remainder, cut to 32 bits for FusedEvalModGuarded.
func fusedGuardedRemainder(shape program.FusedEvalShape, dividend, divisor uint64) uint64 {
	remainder := dividend % divisor
	if shape == program.FusedEvalModGuardedWhole {
		return remainder
	}
	return uint64(mask32(remainder))
}

// fusedEvalIfPosNode evaluates the three-way conditional family.
//
// Takes callee (*program.CompiledFunction) which is the node's compiled method.
// Takes data (unsafe.Pointer) which is the receiver struct base.
// Takes env (*fusedEnv) which is the shared environment slice.
// Takes depth (int) which is the current recursion depth.
//
// Returns uint64 which is the selected branch result.
// Returns bool which is false on a gate failure.
func fusedEvalIfPosNode(callee *program.CompiledFunction, data unsafe.Pointer, env *fusedEnv, depth int) (uint64, bool) {
	condition, ok := fusedEvalChild(callee, callee.EvalChildALayout, callee.EvalSiteA, data, env, depth)
	if !ok {
		return 0, false
	}
	if condition != 0 {
		return fusedEvalChild(callee, callee.EvalChildBLayout, callee.EvalSiteB, data, env, depth)
	}
	return fusedEvalChild(callee, callee.EvalChildCLayout, callee.EvalSiteC, data, env, depth)
}

// fusedCombineBinary applies the binary family's combining operation to two evaluated
// children.
//
// Takes shape (program.FusedEvalShape) which selects the arithmetic operation.
// Takes left (uint64) which is the first operand.
// Takes right (uint64) which is the second operand.
//
// Returns uint64 which is the combined result.
func fusedCombineBinary(shape program.FusedEvalShape, left, right uint64) uint64 {
	switch shape {
	case program.FusedEvalAddMasked:
		return uint64(mask32(left) + mask32(right))
	case program.FusedEvalSubMasked:
		return uint64(mask32(left) - mask32(right))
	case program.FusedEvalMulMasked:
		return uint64(mask32(left) * mask32(right))
	case program.FusedEvalMin:
		return min(left, right)
	default:
		return max(left, right)
	}
}

// fusedEvalChild fetches one interface child and evaluates it recursively.
//
// Takes parent (*program.CompiledFunction) which owns the call sites.
// Takes layout (program.StructFieldLayout) which describes the child field.
// Takes siteIndex (uint16) which is the call-site index.
// Takes base (unsafe.Pointer) which is the parent receiver base.
// Takes env (*fusedEnv) which is the shared environment.
// Takes depth (int) which is the current recursion depth.
//
// Returns uint64 which is the child's evaluated result.
// Returns bool which is false on any gate failure.
func fusedEvalChild(parent *program.CompiledFunction, layout program.StructFieldLayout, siteIndex uint16, base unsafe.Pointer, env *fusedEnv, depth int) (uint64, bool) {
	typeWord, data, ok := fusedChildEface(base, layout)
	if !ok {
		return 0, false
	}
	if int(siteIndex) >= len(parent.CallSites) {
		return 0, false
	}
	callee := fusedResolveChildCallee(&parent.CallSites[siteIndex], typeWord)
	if callee == nil {
		return 0, false
	}
	return fusedEvalNode(callee, data, env, depth+1)
}

// fusedEvalLeaf inlines a recognised tiny-leaf body inside the walker.
//
// Takes callee (*program.CompiledFunction) which is the leaf's compiled method.
// Takes data (unsafe.Pointer) which is the receiver struct base.
// Takes env (*fusedEnv) which is the shared environment.
//
// Returns uint64 which is the leaf's evaluated result.
// Returns bool which is false for unrecognised leaf shapes.
func fusedEvalLeaf(callee *program.CompiledFunction, data unsafe.Pointer, env *fusedEnv) (uint64, bool) {
	fieldPointer := unsafe.Add(data, uintptr(callee.TinyLeafLayout.Offset))
	switch callee.TinyLeafShape {
	case program.TinyLeafReturnUintField:
		return readUintAt(fieldPointer, reflect.Kind(callee.TinyLeafLayout.Kind)), true
	case program.TinyLeafReturnIntField:
		return safeconv.Int64ToUint64Reinterpret(readIntAt(fieldPointer, reflect.Kind(callee.TinyLeafLayout.Kind))), true
	case program.TinyLeafReturnEnvUintAtIntFieldSlot:
		slot := readIntAt(fieldPointer, reflect.Kind(callee.TinyLeafLayout.Kind))
		return env.at(slot)
	default:
	}
	return 0, false
}

// runFusedEvalShape attempts frameless recursive evaluation of a shaped method call.
//
// Takes registers (*Registers) which holds the caller's register file.
// Takes site (*program.CallSite) which describes arguments and return slots.
// Takes callee (*program.CompiledFunction) which is the method to evaluate.
//
// Returns OpResult which is opContinue on success.
// Returns bool which is false when the caller should use the framed path instead.
func runFusedEvalShape(registers *Registers, site *program.CallSite, callee *program.CompiledFunction) (OpResult, bool) {
	if len(site.Arguments) < 2 || len(site.Returns) < 1 {
		return opContinue, false
	}
	receiver := registers.General[site.Arguments[0].Register]
	receiverRaw := (*unsafeReflectValue)(unsafe.Pointer(&receiver))
	if receiverRaw.typ == nil || reflect.Kind(receiverRaw.flag&flagKindMask) != reflect.Pointer {
		return opContinue, false
	}
	base := receiverRaw.ptr
	if receiverRaw.flag&flagIndir != 0 {
		base = *(*unsafe.Pointer)(base)
	}
	var env fusedEnv
	switch envArg := site.Arguments[1]; envArg.Kind {
	case isa.RegisterSliceUint64:
		envSlice := registers.slicesUint[envArg.Register]
		if len(envSlice) > 0 {
			env = fusedEnv{data: unsafe.Pointer(&envSlice[0]), length: len(envSlice), wide: true}
		}
	case isa.RegisterGeneral:
		envValue := registers.General[envArg.Register]
		envRaw := (*unsafeReflectValue)(unsafe.Pointer(&envValue))
		if envRaw.typ == nil || reflect.Kind(envRaw.flag&flagKindMask) != reflect.Slice {
			return opContinue, false
		}
		wideEnv, supported := fusedEnvStrideForElement(envValue.Type().Elem())
		if !supported {
			return opContinue, false
		}
		header := (*snapshotSliceHeader)(envRaw.ptr)
		env = fusedEnv{data: header.Data, length: header.Len, wide: wideEnv}
	default:
		return opContinue, false
	}
	result, ok := fusedEvalNode(callee, base, &env, 0)
	if !ok {
		return opContinue, false
	}
	registers.Uints[site.Returns[0].Register] = result
	return opContinue, true
}

// fusedEnvStrideForElement reports the stride mode for an environment slice element type.
//
// Takes elementType (reflect.Type) which is the slice element type to check.
//
// Returns wide (bool) which is true for 8-byte elements.
// Returns supported (bool) which is false for unsupported element kinds or widths.
func fusedEnvStrideForElement(elementType reflect.Type) (wide bool, supported bool) {
	switch elementType.Kind() {
	case reflect.Uint, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		switch elementType.Size() {
		case fusedEnvWideStrideBytes:
			return true, true
		case fusedEnvNarrowStrideBytes:
			return false, true
		default:
			return false, false
		}
	default:
		return false, false
	}
}
