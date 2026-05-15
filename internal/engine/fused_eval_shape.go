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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// fusedEvalChildren is the number of child operands a fused evaluator shape carries: the
	// two operands of a binary combiner plus the third arm of a conditional.
	fusedEvalChildren = 3

	// maxFusedEvalDepth bounds the recursion depth so pathological trees fall back to framed
	// execution.
	maxFusedEvalDepth = 200

	// fusedChildFetchInstrs is the instruction span of one interface-child fetch sequence.
	fusedChildFetchInstrs = 3

	// fusedTruncateWidth is the operand bit width the masked-arithmetic families truncate
	// to.
	fusedTruncateWidth = 32

	// fusedMaskedBinaryBodyLen is the recognised body length for add/sub/mul masked shapes.
	fusedMaskedBinaryBodyLen = 10

	// fusedMinMaxBodyLen is the recognised body length for the min/max comparison-select
	// shape.
	fusedMinMaxBodyLen = 11

	// fusedIfPosBodyLen is the recognised body length for the compact three-way conditional
	// shape.
	fusedIfPosBodyLen = 13

	// fusedModGuardedBodyLenMin is the minimum body length for the guarded-modulo shape.
	fusedModGuardedBodyLenMin = 14

	// fusedModGuardedBodyLenMax is the maximum body length for the guarded-modulo shape.
	fusedModGuardedBodyLenMax = 15

	// fusedEnvWideStrideBytes is the 64-bit element width the fused environment reader
	// supports.
	fusedEnvWideStrideBytes = 8

	// fusedEnvNarrowStrideBytes is the 32-bit element width the fused environment reader
	// supports.
	fusedEnvNarrowStrideBytes = 4

	// fusedModGuardChildBOffset is the dividend child's fetch offset in the guarded-modulo
	// body.
	fusedModGuardChildBOffset = 8

	// fusedIfPosChildCOffset is the else-child's fetch offset in the compact if-pos body.
	fusedIfPosChildCOffset = 10
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

// classifyFusedEvalShape classifies a compiled function's body against the recognised
// evaluator families.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function whose
// EvalShape fields are populated on a match.
func classifyFusedEvalShape(compiledFunction *program.CompiledFunction) {
	compiledFunction.EvalShapeOnce.Do(func() { classifyFusedEvalShapeOnce(compiledFunction) })
}

// classifyFusedEvalShapeOnce runs the classification body exactly once per function under
// evalShapeOnce.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function to classify.
func classifyFusedEvalShapeOnce(compiledFunction *program.CompiledFunction) {
	compiledFunction.EvalShape = program.FusedEvalNone
	body := effectiveOps(compiledFunction.Body)
	switch len(body) {
	case fusedMaskedBinaryBodyLen:
		tryClassifyMaskedBinary(compiledFunction, body)
		if compiledFunction.EvalShape == program.FusedEvalNone {
			tryClassifyMinMax(compiledFunction, body)
		}

	case fusedMinMaxBodyLen:
		tryClassifyMinMax(compiledFunction, body)
	case fusedModGuardedBodyLenMin, fusedModGuardedBodyLenMax:
		tryClassifyModGuarded(compiledFunction, body)
		if compiledFunction.EvalShape == program.FusedEvalNone {
			tryClassifyIfPos(compiledFunction, body)
		}
	case fusedIfPosBodyLen:
		tryClassifyIfPos(compiledFunction, body)
	}
}

// effectiveOps strips tier-3 NOP words from a body so matchers see a consistent
// instruction stream.
//
// Takes body ([]isa.Instruction) which is the raw instruction sequence to filter.
//
// Returns []isa.Instruction which is the filtered stream with NOP words removed.
func effectiveOps(body []isa.Instruction) []isa.Instruction {
	out := make([]isa.Instruction, 0, len(body))
	for _, inst := range body {
		if inst.Op == isa.OpDrillTier1 && inst.A == 0 && inst.B == 0 && inst.C == 0 {
			continue
		}
		out = append(out, inst)
	}
	return out
}

// fusedChildFetch matches the instruction triple that loads an interface child and
// evaluates it.
//
// Takes compiledFunction (*program.CompiledFunction) which owns the struct layout table.
// Takes body ([]isa.Instruction) which is the instruction stream to match.
// Takes at (int) which is the offset into body where the triple starts.
//
// Returns program.StructFieldLayout which is the matched child field layout.
// Returns uint16 which is the call-site index.
// Returns bool which is true when the triple matched.
func fusedChildFetch(compiledFunction *program.CompiledFunction, body []isa.Instruction, at int) (program.StructFieldLayout, uint16, bool) {
	if at+2 >= len(body) {
		return program.StructFieldLayout{}, 0, false
	}
	get, call, ext := body[at], body[at+1], body[at+2]
	if get.Op != isa.OpGetStructFieldGeneral || get.B != 0 {
		return program.StructFieldLayout{}, 0, false
	}
	if !isa.InstrIsTier1SubOp(call, isa.SubOpCallMethodInlineable) || ext.Op != isa.OpExt {
		return program.StructFieldLayout{}, 0, false
	}
	layoutIndex := int(get.C)
	if layoutIndex >= len(compiledFunction.StructLayoutTable) {
		return program.StructFieldLayout{}, 0, false
	}
	siteIndex := uint16(call.B)
	if int(siteIndex) >= len(compiledFunction.CallSites) {
		return program.StructFieldLayout{}, 0, false
	}
	return compiledFunction.StructLayoutTable[layoutIndex], siteIndex, true
}

// tryClassifyMaskedBinary matches the add/sub/mul family.
//
// Takes compiledFunction (*program.CompiledFunction) which receives the classification
// result.
// Takes body ([]isa.Instruction) which is the effective instruction stream.
func tryClassifyMaskedBinary(compiledFunction *program.CompiledFunction, body []isa.Instruction) {
	layoutA, siteA, ok := fusedChildFetch(compiledFunction, body, 0)
	if !ok {
		return
	}
	layoutB, siteB, ok := fusedChildFetch(compiledFunction, body, fusedChildFetchInstrs)
	if !ok {
		return
	}
	arith, trunc, move := body[2*fusedChildFetchInstrs], body[2*fusedChildFetchInstrs+1], body[2*fusedChildFetchInstrs+2]
	if trunc.Op != isa.OpTruncateNarrow || trunc.B != fusedTruncateWidth {
		return
	}
	if move.Op != isa.OpDrillTier1 || isa.SubOpcode(move.A) != isa.SubOpMoveUint {
		return
	}
	if !isTier2Return(body[9]) {
		return
	}
	var shape program.FusedEvalShape
	switch arith.Op {
	case isa.OpAddUint:
		shape = program.FusedEvalAddMasked
	case isa.OpSubUint:
		shape = program.FusedEvalSubMasked
	case isa.OpMulUint:
		shape = program.FusedEvalMulMasked
	default:
		return
	}
	setEvalShape(compiledFunction, shape,
		[fusedEvalChildren]program.StructFieldLayout{layoutA, layoutB, {
			Offset:         0,
			TypeIndex:      0,
			Path:           [isa.StructFieldLayoutMaxPathDepth]uint8{},
			PathLength:     0,
			Kind:           0,
			RegisterKind:   0,
			Flags:          0,
			FieldTypeIndex: 0,
		}},
		[fusedEvalChildren]uint16{siteA, siteB, 0})
}

// tryClassifyMinMax matches the comparison-select family.
//
// Takes compiledFunction (*program.CompiledFunction) which receives the classification
// result.
// Takes body ([]isa.Instruction) which is the effective instruction stream.
func tryClassifyMinMax(compiledFunction *program.CompiledFunction, body []isa.Instruction) {
	layoutA, siteA, ok := fusedChildFetch(compiledFunction, body, 0)
	if !ok {
		return
	}
	layoutB, siteB, ok := fusedChildFetch(compiledFunction, body, fusedChildFetchInstrs)
	if !ok {
		return
	}
	if len(body) < fusedMaskedBinaryBodyLen {
		return
	}
	cmp, jump := body[2*fusedChildFetchInstrs], body[2*fusedChildFetchInstrs+1]
	if jump.Op != isa.OpJumpIfFalse {
		return
	}
	if !isTier2Return(body[8]) {
		return
	}
	var shape program.FusedEvalShape
	switch cmp.Op {
	case isa.OpLtUint:
		shape = program.FusedEvalMin
	case isa.OpGtUint:
		shape = program.FusedEvalMax
	default:
		return
	}
	setEvalShape(compiledFunction, shape,
		[fusedEvalChildren]program.StructFieldLayout{layoutA, layoutB, {
			Offset:         0,
			TypeIndex:      0,
			Path:           [isa.StructFieldLayoutMaxPathDepth]uint8{},
			PathLength:     0,
			Kind:           0,
			RegisterKind:   0,
			Flags:          0,
			FieldTypeIndex: 0,
		}},
		[fusedEvalChildren]uint16{siteA, siteB, 0})
}

// tryClassifyModGuarded matches the guarded-modulo family.
//
// Takes compiledFunction (*program.CompiledFunction) which receives the classification
// result.
// Takes body ([]isa.Instruction) which is the effective instruction stream.
func tryClassifyModGuarded(compiledFunction *program.CompiledFunction, body []isa.Instruction) {
	layoutA, siteA, ok := fusedChildFetch(compiledFunction, body, 0)
	if !ok {
		return
	}
	guard := body[fusedChildFetchInstrs]
	if guard.Op != isa.OpDrillTier1 || isa.SubOpcode(guard.A) != isa.SubOpEqUintConstJumpFalse {
		return
	}
	if body[fusedChildFetchInstrs+1].Op != isa.OpExt {
		return
	}
	layoutB, siteB, ok := fusedChildFetch(compiledFunction, body, fusedModGuardChildBOffset)
	if !ok {
		return
	}
	if body[fusedModGuardChildBOffset+fusedChildFetchInstrs].Op != isa.OpRemUint || !isTier2Return(body[len(body)-1]) {
		return
	}
	setEvalShape(compiledFunction, program.FusedEvalModGuarded,
		[fusedEvalChildren]program.StructFieldLayout{layoutA, layoutB, {
			Offset:         0,
			TypeIndex:      0,
			Path:           [isa.StructFieldLayoutMaxPathDepth]uint8{},
			PathLength:     0,
			Kind:           0,
			RegisterKind:   0,
			Flags:          0,
			FieldTypeIndex: 0,
		}},
		[fusedEvalChildren]uint16{siteA, siteB, 0})
}

// tryClassifyIfPos matches the three-way conditional family.
//
// Takes compiledFunction (*program.CompiledFunction) which receives the classification
// result.
// Takes body ([]isa.Instruction) which is the effective instruction stream.
func tryClassifyIfPos(compiledFunction *program.CompiledFunction, body []isa.Instruction) {
	layoutA, siteA, ok := fusedChildFetch(compiledFunction, body, 0)
	if !ok {
		return
	}
	if body[fusedChildFetchInstrs+1].Op != isa.OpNeUint || body[fusedChildFetchInstrs+2].Op != isa.OpJumpIfFalse {
		return
	}
	layoutB, siteB, ok := fusedChildFetch(compiledFunction, body, 2*fusedChildFetchInstrs)
	if !ok {
		return
	}
	elseAt := fusedIfPosChildCOffset
	if _, _, probe := fusedChildFetch(compiledFunction, body, fusedIfPosChildCOffset+1); probe {
		elseAt = fusedIfPosChildCOffset + 1
	}
	layoutC, siteC, ok := fusedChildFetch(compiledFunction, body, elseAt)
	if !ok {
		return
	}
	setEvalShape(compiledFunction, program.FusedEvalIfPos,
		[fusedEvalChildren]program.StructFieldLayout{layoutA, layoutB, layoutC},
		[fusedEvalChildren]uint16{siteA, siteB, siteC})
}

// setEvalShape records a successful classification.
//
// Takes compiledFunction (*program.CompiledFunction) which receives the shape and child
// metadata.
// Takes shape (program.FusedEvalShape) which is the classified family.
// Takes layouts ([fusedEvalChildren]program.StructFieldLayout) which are the child field
// layouts.
// Takes sites ([fusedEvalChildren]uint16) which are the child call-site indices.
func setEvalShape(compiledFunction *program.CompiledFunction, shape program.FusedEvalShape, layouts [fusedEvalChildren]program.StructFieldLayout, sites [fusedEvalChildren]uint16) {
	compiledFunction.EvalChildALayout = layouts[0]
	compiledFunction.EvalChildBLayout = layouts[1]
	compiledFunction.EvalChildCLayout = layouts[2]
	compiledFunction.EvalSiteA = sites[0]
	compiledFunction.EvalSiteB = sites[1]
	compiledFunction.EvalSiteC = sites[2]
	compiledFunction.EvalShape = shape
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
// Returns uintptr which is the held element type word.
// Returns unsafe.Pointer which is the held data pointer.
// Returns bool which is false when the child is nil or not a direct pointer.
func fusedChildEface(base unsafe.Pointer, layout program.StructFieldLayout) (uintptr, unsafe.Pointer, bool) {
	fieldPointer := unsafe.Add(base, uintptr(layout.Offset))
	heldType := *(*unsafe.Pointer)(fieldPointer)
	if heldType == nil {
		return 0, nil, false
	}
	heldKind := reflect.Kind(*(*uint8)(unsafe.Add(heldType, program.AbiTypeKindByteOffset)) & uint8(flagKindMask))
	if !program.HeldKindIsDirectPointer(heldKind) {
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
	case program.FusedEvalModGuarded:
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
	if mask32(divisor) == 0 {
		return 0, true
	}
	dividend, ok := fusedEvalChild(callee, callee.EvalChildBLayout, callee.EvalSiteB, data, env, depth)
	if !ok {
		return 0, false
	}
	return uint64(mask32(dividend) % mask32(divisor)), true
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
	if mask32(condition) != 0 {
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
	case isa.RegisterSliceUint:
		envSlice := registers.slicesUint[envArg.Register]
		if len(envSlice) > 0 {
			env = fusedEnv{data: unsafe.Pointer(&envSlice[0]), length: len(envSlice), wide: true}
		}
	default:
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
