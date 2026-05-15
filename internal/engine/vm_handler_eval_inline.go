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
)

const (
	// inlineEvalMethodName is the hard-coded method name targeted by the binop inline shape.
	// The inline-eval node family is the sole consumer, so the name is a package constant
	// rather than a per-inlineDescriptor field.
	inlineEvalMethodName = "Eval"

	// inlineBinopUintMaxMaskWidth caps accepted mask widths at 64 so the (1<<width)-1 mask
	// never overflows.
	inlineBinopUintMaxMaskWidth = 64
)

// handleCallMethodInlineable dispatches isa.SubOpCallMethodInlineable.
//
// Behaves identically to handleCallMethod at the bytecode level (same operand shape, same
// call-site lookup) but the runtime path consults a per-CallSite per-receiver-type
// inlineDescriptor cache and, on a known fused shape (binary-op Eval), runs the body
// inline in the caller's frame. On a non-inlineable shape it falls back to the standard
// dispatch via dispatchMethodCallSite.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which holds the program counter and layout tables.
// Takes registers (*Registers) which holds the active register banks.
// Takes instr (instruction) which encodes the call-site index in isa.WideIndex().
//
// Returns OpResult indicating the next execution step.
func handleCallMethodInlineable(vm *VM, frame *CallFrame, registers *Registers, instr isa.Instruction) OpResult {
	siteIndex := instr.WideIndex()
	if int(siteIndex) >= len(frame.Function.CallSites) {
		vMBoundsError(vm, frame, boundsTableCallSite, int(siteIndex), len(frame.Function.CallSites))
		return opPanicError
	}
	site := &frame.Function.CallSites[siteIndex]
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++

	if len(site.Arguments) == 0 {
		vm.evalError = newInvariantError("method call site has no receiver argument")
		return opPanicError
	}
	receiver := registers.General[site.Arguments[0].Register]
	recvType := receiver.Type()
	if recvType.Kind() == reflect.Pointer {
		recvType = recvType.Elem()
	}

	descriptor := lookupInlineDescriptor(site, recvType)
	if descriptor == nil {
		result := dispatchMethodCallSite(vm, frame, registers, site, extensionWord)
		classifyAndCacheFromMethodIC(site, recvType)
		return result
	}

	if descriptor.Callee != nil {
		classifyFusedEvalShape(descriptor.Callee)
		if descriptor.Callee.EvalShape > program.FusedEvalNone {
			if result, fused := runFusedEvalShape(registers, site, descriptor.Callee); fused {
				return result
			}
		}
	}
	if descriptor.Shape == program.InlineShapeBinopUint {
		return runInlineBinopUint(vm, frame, registers, site, descriptor)
	}
	if descriptor.Callee.TinyLeafShape != program.TinyLeafNone {
		return runTinyLeafInline(vm, registers, site, descriptor.Callee)
	}
	return pushCompiledFrame(vm, registers, site, descriptor.Callee)
}

// lookupInlineDescriptor walks the call site's inlineDescriptorSlots looking for a cached
// classification of the given receiver type. Returns nil on miss.
//
// Takes site (*CallSite) which holds the cache slots.
// Takes recvType (reflect.Type) which is the receiver concrete type.
//
// Returns the cached descriptor on hit, nil on miss.
func lookupInlineDescriptor(site *program.CallSite, recvType reflect.Type) *program.InlineDescriptor {
	for slotIndex := range site.InlineDescriptorSlots {
		pointer := atomic.LoadPointer(&site.InlineDescriptorSlots[slotIndex])
		if pointer == nil {
			continue
		}
		descriptor := (*program.InlineDescriptor)(pointer)
		if descriptor.RecvType == recvType {
			return descriptor
		}
	}
	return nil
}

// classifyAndCacheFromMethodIC publishes an inline descriptor.
//
// Walks methodICSlots looking for an entry that matches recvType (just populated by the
// dispatch we returned from), classifies the resolved callee's body for known fused
// inline shapes, and publishes the result into the inlineDescriptorSlots cache.
// Subsequent calls with the same (CallSite, recvType) hit the cache and take the inline
// path.
//
// Takes site (*CallSite) which holds both the method IC and the inline-descriptor cache.
// Takes recvType (reflect.Type) which is the receiver type we just dispatched for.
func classifyAndCacheFromMethodIC(site *program.CallSite, recvType reflect.Type) {
	var callee *program.CompiledFunction
	for slotIndex := range site.MethodICSlots {
		pointer := atomic.LoadPointer(&site.MethodICSlots[slotIndex])
		if pointer == nil {
			continue
		}
		entry := (*program.MonomorphicCacheEntry)(pointer)
		if entry.ReceiverType == recvType {
			callee = entry.Callee
			break
		}
	}
	if callee == nil {
		return
	}
	descriptor := &program.InlineDescriptor{RecvType: recvType,
		Callee: callee,
		Shape:  classifyInlineShape(callee), InnerCalleeSlots: [program.InnerCalleeSlotCount]unsafe.Pointer{}, MaskValue: 0, LeftLayout: program.StructFieldLayout{Offset: 0,
			TypeIndex:      0,
			Path:           [isa.StructFieldLayoutMaxPathDepth]uint8{},
			PathLength:     0,
			Kind:           0,
			RegisterKind:   0,
			Flags:          0,
			FieldTypeIndex: 0}, RightLayout: program.StructFieldLayout{Offset: 0,
			TypeIndex:      0,
			Path:           [isa.StructFieldLayoutMaxPathDepth]uint8{},
			PathLength:     0,
			Kind:           0,
			RegisterKind:   0,
			Flags:          0,
			FieldTypeIndex: 0}, InnerCalleeVictim: 0, BinopOpcode: 0, MaskApplies: false,
	}
	if descriptor.Shape == program.InlineShapeBinopUint {
		fillBinopUintDescriptor(callee, descriptor)
	}
	victim := site.InlineDescriptorVictim & program.InlineDescriptorVictimMask
	site.InlineDescriptorVictim++
	atomic.StorePointer(&site.InlineDescriptorSlots[victim], unsafe.Pointer(descriptor))
}

// classifyInlineShape walks callee.body for known fused shapes.
//
// The binop matcher accepts:
//
//	GET_STRUCT_FIELD_GENERAL leftDest recv=0 leftLayoutIdx
//	CALL_METHOD[_INLINEABLE] + ext     (left.Eval(env))
//	GET_STRUCT_FIELD_GENERAL rightDest recv=0 rightLayoutIdx
//	CALL_METHOD[_INLINEABLE] + ext     (right.Eval(env))
//	{ADD|SUB|MUL}_UINT result leftReg rightReg
//	(optional) LOAD_UINT_CONST or {isa.OpDrillTier1,isa.SubOpLoadUintConstSmall}
//	(optional) BIT_AND_UINT result result maskReg
//	TIER2_RETURN
//
// NOPs between ops are skipped. Any unrecognised op aborts the match.
//
// Takes callee (*CompiledFunction) which is the function whose body is scanned.
//
// Returns the recognised shape, or InlineShapeNone when no pattern matches.
func classifyInlineShape(callee *program.CompiledFunction) program.InlineShape {
	if callee == nil || len(callee.Body) == 0 {
		return program.InlineShapeNone
	}
	if matchBinopUintShape(callee, nil) {
		return program.InlineShapeBinopUint
	}
	return program.InlineShapeNone
}

// fillBinopUintDescriptor populates descriptor's binop-shape metadata (layouts, binop
// opcode, mask). Called only after classifyInlineShape already returned
// inlineShapeBinopUint, so re-runs the matcher with descriptor != nil to extract the
// metadata into the descriptor.
//
// Takes callee (*CompiledFunction) which is the matched function.
// Takes descriptor (*inlineDescriptor) which receives the metadata.
func fillBinopUintDescriptor(callee *program.CompiledFunction, descriptor *program.InlineDescriptor) {
	matchBinopUintShape(callee, descriptor)
}

// matchBinopUintShape walks the body once.
//
// When descriptor is non-nil and the body matches, also writes layout, binop, and mask
// metadata into descriptor.
//
// Takes callee (*CompiledFunction) which is the function to scan.
// Takes descriptor (*inlineDescriptor) which is optional metadata sink.
//
// Returns bool which is true when the body matches.
func matchBinopUintShape(callee *program.CompiledFunction, descriptor *program.InlineDescriptor) bool {
	body := callee.Body
	leftLayoutIdx, leftReturnReg, pos, ok := matchFieldEvalPair(callee, 0)
	if !ok {
		return false
	}
	rightLayoutIdx, rightReturnReg, pos, ok := matchFieldEvalPair(callee, pos)
	if !ok {
		return false
	}
	binopInstr, pos, ok := matchUintBinop(body, pos, leftReturnReg, rightReturnReg)
	if !ok {
		return false
	}
	maskApplies, maskValue, pos := matchOptionalNarrowMask(body, pos, binopInstr.A)
	pos = skipOptionalResultMove(body, pos, binopInstr.A)
	if !matchTrailingReturn(body, pos) {
		return false
	}

	if descriptor != nil {
		descriptor.LeftLayout = callee.StructLayoutTable[leftLayoutIdx]
		descriptor.RightLayout = callee.StructLayoutTable[rightLayoutIdx]
		descriptor.BinopOpcode = binopInstr.Op
		descriptor.MaskApplies = maskApplies
		descriptor.MaskValue = maskValue
	}
	return true
}

// matchFieldEvalPair matches one operand of the binop shape: a field read from the
// receiver followed by an inlineable Eval call on that field.
//
// Takes callee (*CompiledFunction) which is the function being scanned.
// Takes start (int) which is the body index to resume scanning from.
//
// Returns layoutIdx (uint8) which indexes the field's entry in StructLayoutTable.
// Returns returnReg (uint8) which is the register the Eval call writes its result to.
// Returns next (int) which is the body index just after the call and its extension word.
// Returns ok (bool) which is true when both ops match.
func matchFieldEvalPair(callee *program.CompiledFunction, start int) (layoutIdx, returnReg uint8, next int, ok bool) {
	layoutIdx, next, ok = matchReceiverFieldLoad(callee, start)
	if !ok {
		return 0, 0, 0, false
	}
	returnReg, next, ok = matchInlineableEvalCall(callee, next)
	if !ok {
		return 0, 0, 0, false
	}
	return layoutIdx, returnReg, next, true
}

// matchReceiverFieldLoad matches a GET_STRUCT_FIELD_GENERAL that reads from the receiver
// in register 0 through a valid layout index.
//
// Takes callee (*CompiledFunction) which is the function being scanned.
// Takes start (int) which is the body index to resume scanning from.
//
// Returns uint8 which is the layout index of the field read.
// Returns int which is the body index just after the field read.
// Returns bool which is true when the op matches.
func matchReceiverFieldLoad(callee *program.CompiledFunction, start int) (uint8, int, bool) {
	body := callee.Body
	pos := findNextNonNop(body, start)
	if pos < 0 || body[pos].Op != isa.OpGetStructFieldGeneral || body[pos].B != 0 {
		return 0, 0, false
	}
	layoutIdx := body[pos].C
	if int(layoutIdx) >= len(callee.StructLayoutTable) {
		return 0, 0, false
	}
	return layoutIdx, pos + 1, true
}

// matchInlineableEvalCall matches a CALL_METHOD or CALL_METHOD_INLINEABLE plus its
// extension word whose call site has the inlineable Eval shape.
//
// Takes callee (*CompiledFunction) which is the function being scanned.
// Takes start (int) which is the body index to resume scanning from.
//
// Returns uint8 which is the register the call writes its result to.
// Returns int which is the body index just after the extension word.
// Returns bool which is true when the call matches.
func matchInlineableEvalCall(callee *program.CompiledFunction, start int) (uint8, int, bool) {
	body := callee.Body
	pos := findNextNonNop(body, start)
	if pos < 0 || pos+1 >= len(body) {
		return 0, 0, false
	}
	if !isa.InstrIsTier1SubOp(body[pos], isa.SubOpCallMethod) &&
		!isa.InstrIsTier1SubOp(body[pos], isa.SubOpCallMethodInlineable) {
		return 0, 0, false
	}
	siteIndex := body[pos].WideIndex()
	if int(siteIndex) >= len(callee.CallSites) {
		return 0, 0, false
	}
	site := &callee.CallSites[siteIndex]
	if !program.IsInlineableShape(site) {
		return 0, 0, false
	}
	return site.Returns[0].Register, pos + 2, true
}

// matchUintBinop matches the ADD_UINT, SUB_UINT or MUL_UINT that combines the two Eval
// results in left-then-right order.
//
// Takes body ([]isa.Instruction) which is the function body slice.
// Takes start (int) which is the body index to resume scanning from.
// Takes leftReg (uint8) which is the register holding the left Eval result.
// Takes rightReg (uint8) which is the register holding the right Eval result.
//
// Returns isa.Instruction which is the matched binop instruction.
// Returns int which is the body index just after the binop.
// Returns bool which is true when the binop matches.
func matchUintBinop(body []isa.Instruction, start int, leftReg, rightReg uint8) (isa.Instruction, int, bool) {
	pos := findNextNonNop(body, start)
	if pos < 0 {
		return isa.Instruction{}, 0, false
	}
	instr := body[pos]
	switch instr.Op {
	case isa.OpAddUint, isa.OpSubUint, isa.OpMulUint:
	default:
		return isa.Instruction{}, 0, false
	}
	if instr.B != leftReg || instr.C != rightReg {
		return isa.Instruction{}, 0, false
	}
	return instr, pos + 1, true
}

// matchOptionalNarrowMask matches an optional TRUNCATE_NARROW of the binop result and
// turns its width into a mask.
//
// Widths of zero or of inlineBinopUintMaxMaskWidth and above leave the op in place, so
// the trailing-return check then rejects the body.
//
// Takes body ([]isa.Instruction) which is the function body slice.
// Takes start (int) which is the body index to resume scanning from.
// Takes resultReg (uint8) which is the register holding the binop result.
//
// Returns applies (bool) which is true when a mask was matched.
// Returns value (uint64) which is the mask, or zero when none applies.
// Returns next (int) which is the index to resume from, or -1 when only NOPs remain.
func matchOptionalNarrowMask(body []isa.Instruction, start int, resultReg uint8) (applies bool, value uint64, next int) {
	pos := findNextNonNop(body, start)
	if pos < 0 || body[pos].Op != isa.OpTruncateNarrow || body[pos].A != resultReg {
		return false, 0, pos
	}
	width := int(body[pos].B)
	if width <= 0 || width >= inlineBinopUintMaxMaskWidth {
		return false, 0, pos
	}
	return true, (uint64(1) << width) - 1, pos + 1
}

// skipOptionalResultMove skips an optional MOVE_UINT that copies the binop result into
// the return register.
//
// Takes body ([]isa.Instruction) which is the function body slice.
// Takes start (int) which is the body index to resume from, or -1 when only NOPs remain.
// Takes resultReg (uint8) which is the register holding the binop result.
//
// Returns int which is the index to resume from, or -1 when only NOPs remain.
func skipOptionalResultMove(body []isa.Instruction, start int, resultReg uint8) int {
	if start < 0 {
		return start
	}
	pos := findNextNonNop(body, start)
	if pos < 0 {
		return pos
	}
	instr := body[pos]
	if instr.Op == isa.OpDrillTier1 && isa.SubOpcode(instr.A) == isa.SubOpMoveUint && instr.C == resultReg {
		return pos + 1
	}
	return pos
}

// matchTrailingReturn matches the TIER2_RETURN that ends the binop shape, with only NOPs
// after it.
//
// Takes body ([]isa.Instruction) which is the function body slice.
// Takes start (int) which is the body index to resume from, or -1 when only NOPs remain.
//
// Returns bool which is true when the body ends with the return.
func matchTrailingReturn(body []isa.Instruction, start int) bool {
	if start < 0 {
		return false
	}
	pos := findNextNonNop(body, start)
	if pos < 0 || !isTier2Return(body[pos]) {
		return false
	}
	return !hasNonNopAfter(body, pos+1)
}

// runInlineBinopUint executes a classified binop-shape inline.
//
// Skips the outer pushCompiledFrame plus copyCallArgs for the binop method itself by
// reading left and right field pointers from the receiver via tinyLeafReflectField,
// dispatching the two inner Eval calls via invokeInlineEvalInner (each resolves the
// callee per-receiver-type via the outer site's methodICSlots, then pushes a frame
// manually with direct register placement to avoid the site-mismatch panic copyCallArgs
// would raise), applying the binop and optional mask inline, then writing the final
// result into site.Returns[0].Register.
//
// The manual placement bypassing copyCallArgs is required because the outer site's
// argCopyProgram is nil (method-call sites are runtime-resolved) and the fallback path's
// per-bank kindIndex reads source registers from positions encoded for the outer caller,
// not the inner callee's parameter-bank layout.
//
// Takes vm (*VM) which drives the synchronous sub-dispatch loop.
// Takes registers (*Registers) which holds the caller's banks.
// Takes site (*CallSite) which describes the outer call.
// Takes descriptor (*inlineDescriptor) which carries the binop metadata.
//
// Returns OpResult indicating the next execution step.
func runInlineBinopUint(vm *VM, _ *CallFrame, registers *Registers, site *program.CallSite, descriptor *program.InlineDescriptor) OpResult {
	if len(site.Arguments) < 2 || len(site.Returns) < 1 {
		return pushCompiledFrame(vm, registers, site, descriptor.Callee)
	}
	if vm.FramePointer >= vm.callDepthLimit() {
		return opStackOverflow
	}

	recvReg := site.Arguments[0].Register
	returnReg := site.Returns[0].Register
	recv := registers.General[recvReg]
	envValue, envOk := readInlineEnvAsReflect(registers, site.Arguments[1])
	if !envOk {
		return pushCompiledFrame(vm, registers, site, descriptor.Callee)
	}

	leftField, leftFieldOk := resolveInlineBinopOperand(recv, descriptor.LeftLayout)
	rightField, rightFieldOk := resolveInlineBinopOperand(recv, descriptor.RightLayout)
	if !leftFieldOk || !rightFieldOk {
		return pushCompiledFrame(vm, registers, site, descriptor.Callee)
	}

	leftResult, leftErr := invokeInlineEvalInner(vm, registers, site, descriptor, leftField, envValue, returnReg)
	if leftErr != opContinue {
		return leftErr
	}
	rightResult, rightErr := invokeInlineEvalInner(vm, registers, site, descriptor, rightField, envValue, returnReg)
	if rightErr != opContinue {
		return rightErr
	}

	var result uint64
	switch descriptor.BinopOpcode {
	case isa.OpAddUint:
		result = leftResult + rightResult
	case isa.OpSubUint:
		result = leftResult - rightResult
	case isa.OpMulUint:
		result = leftResult * rightResult
	default:
		return pushCompiledFrame(vm, registers, site, descriptor.Callee)
	}
	if descriptor.MaskApplies {
		result &= descriptor.MaskValue
	}
	registers.Uints[returnReg] = result
	return opContinue
}

// resolveInlineBinopOperand walks the receiver through the layout to the field, unwraps
// any interface, and validates the result. Returns (field, true) on success; (zero,
// false) when the field can't be resolved or is invalid.
//
// Takes recv (reflect.Value) which is the receiver value.
// Takes layout (StructFieldLayout) which selects the field.
//
// Returns reflect.Value which is the resolved field, valid on success.
// Returns bool which is true when resolution succeeded.
func resolveInlineBinopOperand(recv reflect.Value, layout program.StructFieldLayout) (reflect.Value, bool) {
	field, ok := tinyLeafReflectField(recv, layout)
	if !ok || !field.IsValid() {
		return reflect.Value{}, false
	}
	if field.Kind() == reflect.Interface {
		field = field.Elem()
	}
	if !field.IsValid() {
		return reflect.Value{}, false
	}
	return field, true
}

// invokeInlineEvalInner dispatches a single inner Eval call from the inlined binop body.
// Resolves the callee for receiver's concrete type via the outer site's methodICSlots (or
// methodTable on miss), then routes to either the tinyLeaf inline path or the manual
// frame-push path based on the callee's classification.
//
// Takes vm (*VM) which drives the re-entrant dispatch loop.
// Takes registers (*Registers) which holds the outer caller's banks.
// Takes site (*CallSite) which holds methodICSlots for type resolution and site.
// Takes descriptor (*InlineDescriptor) which carries the binop metadata and inner-callee
// cache.
// Takes receiver (reflect.Value) which is the leaf or binop pointer passed as recv to the
// inner Eval.
// Takes envValue (reflect.Value) which is the env []uint32 carried from the outer caller.
// Takes returnReg (uint8) which is the caller's uint slot where the inner Eval's result
// lands and from which we read it.
//
// Returns the inner Eval's uint64 result and opContinue on success.
// Returns 0 and a failing OpResult on panic, stack overflow, or callee resolution
// failure.
func invokeInlineEvalInner(vm *VM, registers *Registers, site *program.CallSite, descriptor *program.InlineDescriptor, receiver, envValue reflect.Value, returnReg uint8) (uint64, OpResult) {
	if !receiver.IsValid() {
		return 0, opPanicError
	}
	recvType := receiver.Type()
	if recvType.Kind() == reflect.Pointer {
		recvType = recvType.Elem()
	}

	callee := lookupInnerCallee(descriptor, recvType)
	if callee == nil {
		callee = lookupCalleeForType(site, recvType)
		if callee == nil {
			var ok bool
			callee, ok = resolveInlineEvalCallee(vm, recvType)
			if !ok || callee == nil {
				return 0, opPanicError
			}
		}
		publishInnerCallee(descriptor, recvType, callee)
	}

	if callee.TinyLeafShape != program.TinyLeafNone {
		return invokeInlineEvalTinyLeaf(vm, registers, site, receiver, returnReg, callee)
	}
	return invokeInlineEvalFullFrame(vm, registers, site, receiver, envValue, returnReg, callee)
}

// lookupCalleeForType walks methodICSlots and returns the cached callee for the given
// concrete receiver type, or nil on miss.
//
// Takes site (*CallSite) which holds the methodIC.
// Takes recvType (reflect.Type) which is the receiver's concrete dereferenced type.
//
// Returns the cached *CompiledFunction or nil.
func lookupCalleeForType(site *program.CallSite, recvType reflect.Type) *program.CompiledFunction {
	for slotIndex := range site.MethodICSlots {
		pointer := atomic.LoadPointer(&site.MethodICSlots[slotIndex])
		if pointer == nil {
			continue
		}
		entry := (*program.MonomorphicCacheEntry)(pointer)
		if entry.ReceiverType == recvType {
			return entry.Callee
		}
	}
	return nil
}

// lookupInnerCallee walks the descriptor's innerCalleeSlots looking for a cached
// (recvType -> callee) entry. Pattern mirrors lookupCalleeForType /
// lookupInlineDescriptor: each slot is read with atomic.LoadPointer so concurrent
// publishers cannot tear the entry's (receiverType, callee) pair.
//
// Takes descriptor (*inlineDescriptor) whose innerCalleeSlots carries the memoised
// inner-Eval callee cache.
// Takes recvType (reflect.Type) which is the dereferenced concrete receiver type.
//
// Returns the cached *CompiledFunction on hit, nil on miss.
func lookupInnerCallee(descriptor *program.InlineDescriptor, recvType reflect.Type) *program.CompiledFunction {
	for slotIndex := range descriptor.InnerCalleeSlots {
		pointer := atomic.LoadPointer(&descriptor.InnerCalleeSlots[slotIndex])
		if pointer == nil {
			continue
		}
		entry := (*program.InnerCalleeCacheEntry)(pointer)
		if entry.ReceiverType == recvType {
			return entry.Callee
		}
	}
	return nil
}

// publishInnerCallee installs an inner-callee cache entry via round-robin eviction. Each
// entry is immutable after publish, so concurrent reader races only cost a sub-optimal
// eviction.
//
// Takes descriptor (*inlineDescriptor) whose innerCalleeSlots receives the entry.
// Takes recvType (reflect.Type) which is the concrete receiver type.
// Takes callee (*CompiledFunction) which is the resolved inner-Eval target for recvType.
func publishInnerCallee(descriptor *program.InlineDescriptor, recvType reflect.Type, callee *program.CompiledFunction) {
	entry := &program.InnerCalleeCacheEntry{ReceiverType: recvType, Callee: callee}
	victim := descriptor.InnerCalleeVictim & program.InnerCalleeVictimMask
	descriptor.InnerCalleeVictim++
	atomic.StorePointer(&descriptor.InnerCalleeSlots[victim], unsafe.Pointer(entry))
}

// resolveInlineEvalCallee resolves the Eval method for recvType.
//
// Looks up the method through rootFunction's methodTable. Used when the outer site's
// methodICSlots misses on a concrete type (which happens regularly for the inner-Eval
// targets because the outer site only ever sees the tree root's type). Goes through
// resolveReceiverTypeName to handle pipit's synthetic _pipitID_*-named struct types.
//
// Takes vm (*VM) which carries the rootFunction and functions table.
// Takes recvType (reflect.Type) which is the receiver type.
//
// Returns the resolved callee and ok=true, or nil and false.
func resolveInlineEvalCallee(vm *VM, recvType reflect.Type) (*program.CompiledFunction, bool) {
	if vm == nil || vm.rootFunction == nil || vm.rootFunction.MethodTable() == nil {
		return nil, false
	}
	typeName := resolveReceiverTypeName(vm, nil, recvType)
	if typeName == "" {
		return nil, false
	}
	functionIndex, ok := vm.rootFunction.MethodTable()[typeName+"."+inlineEvalMethodName]
	if !ok || int(functionIndex) >= len(vm.functions) {
		return nil, false
	}
	return vm.functions[functionIndex], true
}

// invokeInlineEvalTinyLeaf dispatches an inner Eval to a tinyLeaf, bypassing
// pushCompiledFrame and running the inline body in the caller's frame.
//
// Takes vm (*VM) which drives the tiny-leaf dispatch.
// Takes registers (*Registers) which holds the caller's banks.
// Takes site (*CallSite) which describes the outer call.
// Takes receiver (reflect.Value) which is the inner receiver.
// Takes returnReg (uint8) which receives the result in uints.
// Takes callee (*CompiledFunction) which is the tinyLeaf target.
//
// Returns uint64 which is the inner uint result.
// Returns OpResult which signals continuation or a panic.
func invokeInlineEvalTinyLeaf(vm *VM, registers *Registers, site *program.CallSite, receiver reflect.Value, returnReg uint8, callee *program.CompiledFunction) (uint64, OpResult) {
	savedRecv := registers.General[site.Arguments[0].Register]
	registers.General[site.Arguments[0].Register] = receiver
	res := runTinyLeafInline(vm, registers, site, callee)
	registers.General[site.Arguments[0].Register] = savedRecv
	if res != opContinue {
		return 0, res
	}
	return registers.Uints[returnReg], opContinue
}

// invokeInlineEvalFullFrame dispatches an inner Eval via a full frame, manually placing
// the receiver and env into the callee's registers. Surprise signatures fall through to
// the standard-path fallback.
//
// Takes vm (*VM) which drives the synchronous sub-dispatch loop.
// Takes registers (*Registers) which holds the caller's banks.
// Takes site (*CallSite) which describes the outer call.
// Takes receiver (reflect.Value) which is the inner receiver.
// Takes envValue (reflect.Value) which is the env argument.
// Takes callee (*CompiledFunction) which is the inner Eval target.
//
// Returns uint64 which is the inner uint result.
// Returns OpResult which signals continuation or a panic.
func invokeInlineEvalFullFrame(vm *VM, registers *Registers, site *program.CallSite, receiver, envValue reflect.Value, _ uint8, callee *program.CompiledFunction) (uint64, OpResult) {
	if vm.FramePointer >= vm.callDepthLimit() {
		return 0, opStackOverflow
	}
	if !inlineEvalFullFrameCalleeAcceptable(callee) {
		return invokeInlineEvalViaStandardPath(vm, registers, site, callee)
	}

	vm.FramePointer++
	if vm.FramePointer >= len(vm.CallStack) {
		vm.growCallStack()
	}
	pushedFrameIndex := vm.FramePointer
	f := &vm.CallStack[pushedFrameIndex]
	if vm.Arena != nil {
		vm.Arena.SaveInto(&f.arenaSave)
		callee.EnsurePrecomputedAllocCounts()
		vm.Arena.allocRegistersIntoCached(&f.Registers, callee.PrecomputedAllocCounts, callee.NonZeroBankMask)
	} else {
		f.Registers = NewRegisters(callee.NumRegisters)
	}
	f.Function = callee
	f.ProgramCounter = 0
	f.returnDestination = site.Returns
	f.deferBase = len(vm.deferStack)
	if f.simpleDefer != nil {
		f.simpleDefer.active = false
	}
	f.upvalues = nil
	f.hasGeneralAlloc = callee.NumRegisters[isa.RegisterGeneral] > 0
	releaseSharedCellMap(f.sharedCells)
	f.sharedCells = nil
	vm.recordFrameSnapshot(pushedFrameIndex, nil)
	f.Registers.General[0] = receiver
	if !placeInlineEvalEnv(&f.Registers, callee.ParameterKinds[1], envValue, vm.Arena) {
		vm.FramePointer--
		return invokeInlineEvalViaStandardPath(vm, registers, site, callee)
	}

	previousFlag := vm.inlineDispatchExpectUintResult
	vm.inlineDispatchExpectUintResult = true
	_, err := vm.run(pushedFrameIndex)
	uintResult := vm.inlineDispatchUintResult
	vm.inlineDispatchExpectUintResult = previousFlag
	if err != nil {
		return 0, opPanicError
	}
	return uintResult, opContinue
}

// inlineEvalFullFrameCalleeAcceptable reports whether callee's signature and per-bank
// register budget match what invokeInlineEvalFullFrame can manually drive: 2-param
// (general receiver + general/slicesUint env), single uint return, with enough
// general/uint registers reserved and (for slicesUint env) a typed-slice slot available.
//
// Takes callee (*CompiledFunction) which is the candidate Eval.
//
// Returns bool which is true when the manual placement is safe.
func inlineEvalFullFrameCalleeAcceptable(callee *program.CompiledFunction) bool {
	if len(callee.ParameterKinds) < 2 ||
		callee.ParameterKinds[0] != isa.RegisterGeneral ||
		!isInlineEvalEnvKind(callee.ParameterKinds[1]) ||
		len(callee.ResultKinds) < 1 ||
		callee.ResultKinds[0] != isa.RegisterUint {
		return false
	}
	if callee.NumRegisters[isa.RegisterGeneral] < 1 || callee.NumRegisters[isa.RegisterUint] < 1 {
		return false
	}
	if callee.ParameterKinds[1] == isa.RegisterGeneral && callee.NumRegisters[isa.RegisterGeneral] < 2 {
		return false
	}
	if callee.ParameterKinds[1] == isa.RegisterSliceUint && callee.NumRegisters[isa.RegisterSliceUint] < 1 {
		return false
	}
	return true
}

// isInlineEvalEnvKind reports whether kind is supported by the inline-eval env placement.
//
// Takes kind (isa.RegisterKind) which is the callee's env parameter kind.
//
// Returns true for RegisterGeneral and RegisterSliceUint.
func isInlineEvalEnvKind(kind isa.RegisterKind) bool {
	return kind == isa.RegisterGeneral || kind == isa.RegisterSliceUint
}

// readInlineEnvAsReflect extracts the outer caller's env argument as a reflect.Value
// regardless of the bank it currently lives on. runInlineBinopUint and the tinyLeaf path
// treat envValue as a reflect.Value throughout, so the inline driver can carry env across
// recursive Eval calls without re-checking the bank at each hop.
//
// Names the banks an inline eval environment can arrive in; any other kind declines the
// fast path through the default.
//
// Takes registers (*Registers) which is the outer caller's frame.
// Takes argLocation (VarLocation) which is site.Arguments[1] for the outer Eval call.
//
// Returns the env value boxed as reflect.Value and true on supported banks, or
// reflect.Value{} and false on unsupported banks (the inline driver then falls back to
// pushCompiledFrame()).
func readInlineEnvAsReflect(registers *Registers, argLocation program.VarLocation) (reflect.Value, bool) {
	switch argLocation.Kind {
	case isa.RegisterGeneral:
		return registers.General[argLocation.Register], true
	case isa.RegisterSliceUint:
		slice := registers.slicesUint[argLocation.Register]
		if slice == nil {
			return reflect.Value{}, true
		}
		return reflect.ValueOf(slice), true
	case isa.RegisterSliceInt:
		slice := registers.SlicesInt[argLocation.Register]
		if slice == nil {
			return reflect.Value{}, true
		}
		return reflect.ValueOf(slice), true
	default:
		return reflect.Value{}, false
	}
}

// placeInlineEvalEnv writes envValue into the callee's env slot, widening narrower
// element kinds when necessary. Returns false for unsupported banks.
//
// Takes registers (*Registers) which is the callee's frame.
// Takes envKind (isa.RegisterKind) which selects the env slot bank.
// Takes envValue (reflect.Value) which carries the env value.
// Takes arena (*RegisterArena) which provides the widen backing.
//
// Returns true on success, false when the value cannot be placed (the caller must then
// unwind framePointer and route through the standard path).
func placeInlineEvalEnv(registers *Registers, envKind isa.RegisterKind, envValue reflect.Value, arena *RegisterArena) bool {
	switch envKind {
	case isa.RegisterGeneral:
		registers.General[1] = envValue
		return true
	case isa.RegisterSliceUint:
		if !envValue.IsValid() {
			registers.slicesUint[0] = nil
			return true
		}
		typed := unboxToTypedUintSlice(envValue, arena)
		if typed == nil && envValue.Len() != 0 {
			return false
		}
		registers.slicesUint[0] = typed
		return true
	default:
		return false
	}
}

// invokeInlineEvalViaStandardPath is the safety fallback path.
//
// Used when the callee's signature does not match the expected (recv, env) -> uint shape,
// or the callee's pre-allocated bank cannot accommodate the manual placement. Pays the
// full pushCompiledFrame + copyCallArgs cost but stays correct in unusual edge cases
// (which the inline-eval classifier should never produce in practice).
//
// Takes vm (*VM) which drives the synchronous sub-dispatch loop.
// Takes registers (*Registers) which holds the caller's banks.
// Takes site (*CallSite) which describes the outer call.
// Takes callee (*CompiledFunction) which is the inner Eval target.
//
// Returns uint64 which is the inner uint result.
// Returns OpResult which signals continuation or a panic.
func invokeInlineEvalViaStandardPath(vm *VM, registers *Registers, site *program.CallSite, callee *program.CompiledFunction) (uint64, OpResult) {
	fpBefore := vm.FramePointer
	res := pushCompiledFrame(vm, registers, site, callee)
	if res == opPanicError || res == opStackOverflow {
		return 0, res
	}
	if vm.FramePointer > fpBefore {
		if _, err := vm.run(fpBefore); err != nil {
			return 0, opPanicError
		}
	}
	return registers.Uints[site.Returns[0].Register], opContinue
}
