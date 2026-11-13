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

package inline

import (
	"context"
	"go/types"
	"slices"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// parameterWrites records what a callee's body does to its parameter slots.
type parameterWrites struct {
	// direct marks, per bank, the parameter slots whose register an instruction writes.
	direct [isa.NumRegisterKinds][maxParamSlotsScanned]bool

	// through marks the general-bank parameter slots an instruction stores through.
	through [maxParamSlotsScanned]bool
}

// needsCopy reports whether a parameter must be given its own caller slot, seeded by the
// copy the call would have made, instead of sharing the argument's register.
//
// A parameter whose register is written always does. One that is only stored through does
// when its value may be a struct or an array, which a call copies. A pointer, map,
// channel, function or slice is the same value on both sides of a call, so sharing the
// register changes nothing.
//
// Takes callee (*program.CompiledFunction) which declares the parameter.
// Takes index (int) which is the parameter's position.
// Takes slot (uint8) which is the parameter's slot in its bank.
//
// Returns bool which is true when the parameter needs its own slot.
func (writes *parameterWrites) needsCopy(callee *program.CompiledFunction, index int, slot uint8) bool {
	kind := callee.ParameterKinds[index]
	if int(slot) >= maxParamSlotsScanned || writes.direct[kind][slot] {
		return true
	}
	return kind == isa.RegisterGeneral && writes.through[slot] && !parameterHoldsOnlyAReference(callee, index)
}

// spliceableByDescription reports whether an operation can be spliced from its
// instruction description.
//
// Takes instruction (isa.Instruction) which is the callee instruction.
//
// Returns bool which is true when the operation can be spliced by description.
func spliceableByDescription(instruction isa.Instruction) bool {
	return inlineRefusalReason(instruction) == "" && descriptionAccountsForEveryOperand(instruction)
}

// descriptionAccountsForEveryOperand checks an operation's shape and pool mapping for
// splicing.
//
// Takes instruction (isa.Instruction) which is the callee instruction.
//
// Returns bool which is true when the splice knows what to do with every byte of the
// operation and of the words that follow it.
func descriptionAccountsForEveryOperand(instruction isa.Instruction) bool {
	if _, known := isa.SpecForInstruction(instruction); !known {
		return false
	}
	shape := isa.ShapeForInstruction(instruction)
	pools := inlinePoolShapeFor(instruction)
	return shapeFlagsAllowASplice(shape, pools) && everyOperandByteIsAccountedFor(shape, pools)
}

// shapeFlagsAllowASplice checks whether a shape's instruction flags permit splicing.
//
// Takes shape (isa.OperandShape) which is the operation's shape.
// Takes pools (inlinePoolShape) which is its entry in inlinePoolShapes.
//
// Returns bool which is true when no property rules the operation out.
func shapeFlagsAllowASplice(shape isa.OperandShape, pools inlinePoolShape) bool {
	flags := shape.Flags
	if flags&isa.ShapeFlagDescribed == 0 || flags&isa.ShapeFlagTerminator != 0 {
		return false
	}
	if flags&(isa.ShapeFlagOpaqueWrites|isa.ShapeFlagFollowsExtension) != 0 && !pools.describedExtension {
		return false
	}
	return flags&isa.ShapeFlagControlFlow == 0 || pools.relativeJump
}

// everyOperandByteIsAccountedFor checks that each operand byte has a known role.
//
// Takes shape (isa.OperandShape) which is the operation's shape.
// Takes pools (inlinePoolShape) which is its entry in inlinePoolShapes.
//
// Returns bool which is true when the splice knows what to do with all three bytes.
func everyOperandByteIsAccountedFor(shape isa.OperandShape, pools inlinePoolShape) bool {
	roles := [instructionOperandCount]isa.OperandRole{shape.A, shape.B, shape.C}
	carried := [instructionOperandCount]bool{
		false,
		pools.bKindByte != poolNone || pools.bcWide16 != poolNone,
		pools.cKindByte != poolNone || pools.bcWide16 != poolNone,
	}
	namesItsBank := slices.Contains(roles[:], isa.RoleKindMarker)
	for position, role := range roles {
		_, isRegister := isa.KindForRole(role)
		switch {
		case role == isa.RoleRegDynamic:
			if !namesItsBank {
				return false
			}
		case isa.RoleMeansTheSameInEveryFunction(role), isRegister, carried[position]:
		default:
			return false
		}
	}
	return true
}

// operationOnASpliceList reports whether an explicit splice list admits the operation.
//
// Takes instruction (isa.Instruction) which is the callee instruction.
//
// Returns bool which is true for a listed operation.
func operationOnASpliceList(instruction isa.Instruction) bool {
	if instruction.Op != isa.OpDrillTier1 {
		return phase2OpcodeAllowList[instruction.Op]
	}
	if isa.SubOpcode(instruction.A) != isa.SubOpDrillTier2 {
		return phase2Tier1SubOpAllowList[instruction.A]
	}
	return tier2OperationListed(isa.SubOpcodeTier2(instruction.B))
}

// remapDescribedRegisters rewrites every register byte the instruction's shape names,
// looking each callee slot up in the caller-side remap table. A dynamic-bank operand
// takes its bank from the instruction's kind marker.
//
// Takes out (*isa.Instruction) which is rewritten in place.
// Takes original (isa.Instruction) which is the callee instruction, read for its shape
// and its kind marker.
// Takes ctx (*inlineContext) which holds the remap table.
//
// Returns bool which is false when the shape is not described or a register has no remap
// entry.
func remapDescribedRegisters(out *isa.Instruction, original isa.Instruction, ctx *inlineContext) bool {
	shape := isa.ShapeForInstruction(original)
	if shape.Flags&isa.ShapeFlagDescribed == 0 {
		return false
	}
	operands := [instructionOperandCount]*byte{&out.A, &out.B, &out.C}
	roles := [instructionOperandCount]isa.OperandRole{shape.A, shape.B, shape.C}
	for position, role := range roles {
		bank, isRegister := isa.KindForRole(role)
		if role == isa.RoleRegDynamic {
			bank, isRegister = writtenOperandBank(original, shape, role)
			if !isRegister {
				return false
			}
		}
		if !isRegister {
			continue
		}
		callerSlot, mapped := ctx.lookupRegister(bank, *operands[position])
		if !mapped {
			return false
		}
		*operands[position] = callerSlot
	}
	return true
}

// appendDescribedExtensionWords copies and remaps an operation's described extension
// words.
//
// Takes ctx (*inlineContext) which carries caller, callee and the remap table.
// Takes ownerPC (int) which is the callee PC of the operation that owns the words.
// Takes pools (inlinePoolShape) which names the table index in the first word.
//
// Returns int which is the number of words copied.
// Returns program.InlineRefusal which is InlineRefusalUndescribedWord when the operation
// has no word, or a word names a register the instruction set does not describe or the
// caller has no slot for.
func appendDescribedExtensionWords(ctx *inlineContext, ownerPC int, pools inlinePoolShape) (int, program.InlineRefusal) {
	body := ctx.callee.Body
	owner := body[ownerPC]
	end := ownerPC + 1
	for end < len(body) && body[end].Op == isa.OpExtension {
		end++
	}
	words := body[ownerPC+1 : end]
	if len(words) == 0 {
		return 0, program.InlineRefusalUndescribedWord
	}
	for ordinal := range words {
		remapped, refusal := remapDescribedExtensionWord(ctx, owner, words[:ordinal+1], pools)
		if refusal != program.InlineEligible {
			return 0, refusal
		}
		ctx.caller.Body = append(ctx.caller.Body, remapped)
		ctx.appendCalleePosition(ownerPC + 1 + ordinal)
	}
	return len(words), program.InlineEligible
}

// remapDescribedExtensionWord remaps the registers and table index in an extension word.
//
// Takes ctx (*inlineContext) which carries caller, callee and the remap table.
// Takes owner (isa.Instruction) which is the operation the words belong to.
// Takes words ([]isa.Instruction) which ends with the word to rewrite.
// Takes pools (inlinePoolShape) which names the table index in the first word.
//
// Returns isa.Instruction which is the rewritten word.
// Returns program.InlineRefusal which is InlineEligible, InlineRefusalCapWatermark when
// the caller's table cannot take the entry the word names, or
// InlineRefusalUndescribedWord when a byte is claimed twice, the layout is not described
// for some bank, or the caller has no slot for a register.
func remapDescribedExtensionWord(
	ctx *inlineContext, owner isa.Instruction, words []isa.Instruction, pools inlinePoolShape,
) (isa.Instruction, program.InlineRefusal) {
	out := words[len(words)-1]
	var claimed [instructionOperandCount]bool
	if len(words) == 1 && pools.extAWide16 != poolNone {
		if !mergeExtensionWideIndex(ctx, pools.extAWide16, &out) {
			return isa.Instruction{}, program.InlineRefusalCapWatermark
		}
		claimed[0], claimed[1] = true, true
	}
	for kind := range isa.RegisterKind(isa.NumRegisterKinds) {
		reads, writes, described := isa.ExtensionRegisterUse(owner, words, kind)
		if !described || !remapExtensionRegistersOfBank(ctx, &out, &claimed, kind, reads, writes) {
			return isa.Instruction{}, program.InlineRefusalUndescribedWord
		}
	}
	return out, program.InlineEligible
}

// remapExtensionRegistersOfBank rewrites the bytes of one extension word that name a
// register of one bank.
//
// Takes ctx (*inlineContext) which holds the remap table.
// Takes out (*isa.Instruction) which is the word, rewritten in place.
// Takes claimed (*[instructionOperandCount]bool) which marks the bytes already spoken
// for, and gains the ones rewritten here.
// Takes kind (isa.RegisterKind) which is the bank.
// Takes reads (isa.ExtensionIntMask) which marks the bytes naming a register read.
// Takes writes (isa.ExtensionIntMask) which marks the bytes naming a register written.
//
// Returns bool which is false when a byte is claimed twice or names a register the caller
// has no slot for.
func remapExtensionRegistersOfBank(ctx *inlineContext, out *isa.Instruction, claimed *[instructionOperandCount]bool, kind isa.RegisterKind, reads, writes isa.ExtensionIntMask) bool {
	operands := [instructionOperandCount]*byte{&out.A, &out.B, &out.C}
	for position, operand := range operands {
		if !reads[position] && !writes[position] {
			continue
		}
		callerSlot, mapped := ctx.lookupRegister(kind, *operand)
		if claimed[position] || !mapped {
			return false
		}
		*operand = callerSlot
		claimed[position] = true
	}
	return true
}

// calleeNeedsEveryGeneralParameterCopied reports whether a callee both runs an operation
// spliced by description and stores to memory somewhere.
//
// For an operation spliced by description nothing says whether a value it produces shares
// storage with one of its operands, so a store anywhere in such a callee makes every
// general parameter take the copy the call would have made.
//
// Takes callee (*program.CompiledFunction) whose body is scanned.
//
// Returns bool which is true when every general parameter must be copied.
func calleeNeedsEveryGeneralParameterCopied(callee *program.CompiledFunction) bool {
	var byDescription, stores bool
	for _, instruction := range callee.Body {
		if instruction.Op == isa.OpExtension || program.IsReturnInstruction(instruction) {
			continue
		}
		if !operationOnASpliceList(instruction) {
			byDescription = true
		}
		if mutates, classified := isa.InstructionMutatesMemory(instruction); mutates || !classified {
			stores = true
		}
		if byDescription && stores {
			return true
		}
	}
	return false
}

// markGeneralParameterSlotsWrittenThrough marks every general-bank parameter as stored
// through.
//
// Takes writes (*parameterWrites) which is the record being populated.
// Takes callee (*program.CompiledFunction) whose parameters are walked.
func markGeneralParameterSlotsWrittenThrough(writes *parameterWrites, callee *program.CompiledFunction) {
	slots, _ := resolveCalleeParameterSlots(callee)
	for index, kind := range callee.ParameterKinds {
		if kind == isa.RegisterGeneral && int(slots[index]) < maxParamSlotsScanned {
			writes.through[slots[index]] = true
		}
	}
}

// parameterHoldsOnlyAReference reports whether a parameter is known to hold only a
// reference.
//
// Takes callee (*program.CompiledFunction) which declares the parameter.
// Takes index (int) which is the parameter's position.
//
// Returns bool which is true when the parameter can only hold a reference.
func parameterHoldsOnlyAReference(callee *program.CompiledFunction, index int) bool {
	if len(callee.ParameterTypeRefs) != len(callee.ParameterKinds) || callee.ParameterTypeRefs[index] == nil {
		return false
	}
	return typeHoldsOnlyAReference(callee.ParameterTypeRefs[index])
}

// recordExtensionParameterWrites marks the parameter slots an operation writes through
// the registers its extension words name.
//
// Takes writes (*parameterWrites) which is the record being populated.
// Takes owner (isa.Instruction) which is the operation the words belong to.
// Takes words ([]isa.Instruction) which ends with the word being read.
//
// Returns bool which is false when the word's layout is not described for some bank, so
// what it writes there is not known and the caller has to take every slot as written.
func recordExtensionParameterWrites(writes *parameterWrites, owner isa.Instruction, words []isa.Instruction) bool {
	word := words[len(words)-1]
	operands := [instructionOperandCount]uint8{word.A, word.B, word.C}
	for kind := range isa.RegisterKind(isa.NumRegisterKinds) {
		_, wordWrites, described := isa.ExtensionRegisterUse(owner, words, kind)
		if !described {
			return false
		}
		for position, slot := range operands {
			if wordWrites[position] && int(slot) < maxParamSlotsScanned {
				writes.direct[kind][slot] = true
			}
		}
	}
	return true
}

// scalarCallCannotObserveItsCallers reports whether the function a scalar call names, and
// everything that function goes on to call, is unable to inspect the call stack.
//
// Takes holder (*program.CompiledFunction) which owns the call site.
// Takes call (isa.Instruction) which is the scalar call.
//
// Returns bool which is true when splicing holder into another function cannot change
// what the called function observes. It is false for a callee that
// markFunctionsThatCannotObserveCallers() has not read.
func scalarCallCannotObserveItsCallers(holder *program.CompiledFunction, call isa.Instruction) bool {
	site := calleeNamedAt(holder, call)
	return site != nil && site.CachedCallee.CannotObserveCallers
}

// markFunctionsThatCannotObserveCallers records whether each reachable function is unable
// to inspect the call stack, itself or through the functions it calls.
//
// Takes ctx (context.Context) which cancels the reading of the program's calls.
// Takes functions ([]*program.CompiledFunction) which are the program's functions.
//
// Returns error when the context is cancelled, with no answer recorded.
func markFunctionsThatCannotObserveCallers(ctx context.Context, functions []*program.CompiledFunction) error {
	graph, err := newNamedCallGraph(ctx, functions, callerObservingWord)
	if err != nil {
		return err
	}
	for function, position := range graph.index {
		if function.InspectsCallStack {
			graph.marked[position] = true
		}
	}
	observes := graph.reachesMarked()
	for function, position := range graph.index {
		function.CannotObserveCallers = !observes[position]
	}
	return nil
}

// callerObservingWord classifies a word for markFunctionsThatCannotObserveCallers().
//
// Takes function (*program.CompiledFunction) which holds the word.
// Takes instruction (isa.Instruction) which is the word.
//
// Returns *program.CompiledFunction which is the callee of a call that names it.
// Returns callWord which is opaque for a call whose target is unknown or that leaves the
// interpreter.
func callerObservingWord(function *program.CompiledFunction, instruction isa.Instruction) (*program.CompiledFunction, callWord) {
	if !isCallSiteInstruction(instruction) && instruction.Op != isa.OpDefer && instruction.Op != isa.OpGo &&
		!isa.InstrIsTier1SubOp(instruction, isa.SubOpCallBuiltin) {
		return nil, callWordPlain
	}
	if !namesItsCallee(instruction) {
		return nil, callWordOpaque
	}
	site := calleeNamedAt(function, instruction)
	if site == nil {
		return nil, callWordOpaque
	}
	return site.CachedCallee, callWordFollowed
}

// placeResultsInCallerRegisters maps eligible callee result slots directly to caller
// result registers.
//
// The spliced body then writes the caller's register wherever the callee wrote its result
// slot, which may be long before it returns. That is safe only when nothing can see the
// register in between.
//
//   - The caller defers nothing, so no recovered panic resumes it with a result half
//     made.
//   - The caller is not a debug build, where a variable is watched between statements.
//   - The register is a plain one of the result's own bank, and no argument of the call
//     and no other result is in it, so the body reads nothing through it.
//   - A general result can only be a reference, since a struct or an array result must be
//     handed over as a copy (see emitReturnPrep).
//
// Takes ctx (*inlineContext) which holds the remap table built so far.
func placeResultsInCallerRegisters(ctx *inlineContext) {
	ctx.resultInPlace = make([]bool, len(ctx.callee.ResultKinds))
	if len(ctx.site.Returns) != len(ctx.callee.ResultKinds) || ctx.caller.DebugVarTable != nil || bodyDefers(ctx.caller.Body) {
		return
	}
	var ordinal [isa.NumRegisterKinds]uint8
	for index, kind := range ctx.callee.ResultKinds {
		slot := ordinal[kind]
		ordinal[kind]++
		if !resultMayLandInPlace(ctx, index, kind) {
			continue
		}
		current, mapped := ctx.lookupRegister(kind, slot)
		if !mapped || !retargetResultSlot(ctx, kind, slot, current, ctx.site.Returns[index].Register) {
			continue
		}
		ctx.resultInPlace[index] = true
	}
}

// resultMayLandInPlace checks whether a result may use its caller's destination register.
//
// Takes ctx (*inlineContext) which holds the call site and the results placed so far.
// Takes index (int) which is the result's position.
// Takes kind (isa.RegisterKind) which is the bank the callee returns it in.
//
// Returns bool which is true when the destination can stand in for the result slot.
func resultMayLandInPlace(ctx *inlineContext, index int, kind isa.RegisterKind) bool {
	destination := ctx.site.Returns[index]
	if destination.Kind != kind || destination.IsUpvalue || destination.IsIndirect || destination.IsCaptured || destination.IsSpilled {
		return false
	}
	if kind == isa.RegisterGeneral && !resultHoldsOnlyAReference(ctx.callee, index) {
		return false
	}
	for _, argument := range ctx.site.Arguments {
		if argument.Kind == kind && argument.Register == destination.Register {
			return false
		}
	}
	for earlier := range index {
		if ctx.resultInPlace[earlier] && ctx.site.Returns[earlier].Kind == kind && ctx.site.Returns[earlier].Register == destination.Register {
			return false
		}
	}
	return true
}

// retargetResultSlot points a callee result slot at the caller's register for the result.
//
// A slot the callee owns outright is remapped. A slot shared with a parameter is remapped
// only when the parameter has a pre-copy, which is then made to seed the caller's
// register instead of the fresh one.
//
// Takes ctx (*inlineContext) which holds the remap table and the pre-copies.
// Takes kind (isa.RegisterKind) which is the result's bank.
// Takes slot (uint8) which is the callee result slot.
// Takes current (uint8) which is the caller register the slot maps to now.
// Takes destination (uint8) which is the caller register the call site names.
//
// Returns bool which is true when the slot now maps to destination.
func retargetResultSlot(ctx *inlineContext, kind isa.RegisterKind, slot, current, destination uint8) bool {
	if !calleeSlotIsParameter(ctx.callee, kind, slot) {
		ctx.setRegister(kind, slot, destination)
		return true
	}
	for index := range ctx.paramPreCopies {
		preCopy := &ctx.paramPreCopies[index]
		if preCopy.kind == kind && preCopy.destination == current {
			preCopy.destination = destination
			ctx.setRegister(kind, slot, destination)
			return true
		}
	}
	return false
}

// calleeSlotIsParameter reports whether a callee register holds one of its parameters.
//
// Takes callee (*program.CompiledFunction) whose parameter layout is read.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes slot (uint8) which is the register.
//
// Returns bool which is true when a parameter lives in the register.
func calleeSlotIsParameter(callee *program.CompiledFunction, kind isa.RegisterKind, slot uint8) bool {
	slots, _ := resolveCalleeParameterSlots(callee)
	for index, parameterKind := range callee.ParameterKinds {
		if parameterKind == kind && slots[index] == slot {
			return true
		}
	}
	return false
}

// resultHoldsOnlyAReference is parameterHoldsOnlyAReference for a result.
//
// Takes callee (*program.CompiledFunction) which declares the result.
// Takes index (int) which is the result's position.
//
// Returns bool which is true when the result can only hold a reference.
func resultHoldsOnlyAReference(callee *program.CompiledFunction, index int) bool {
	if len(callee.ResultTypeRefs) != len(callee.ResultKinds) || callee.ResultTypeRefs[index] == nil {
		return false
	}
	return typeHoldsOnlyAReference(callee.ResultTypeRefs[index])
}

// typeHoldsOnlyAReference reports whether a type holds only reference values.
//
// Takes declared (types.Type) which is the declared type.
//
// Returns bool which is true for a reference type.
func typeHoldsOnlyAReference(declared types.Type) bool {
	switch declared.Underlying().(type) {
	case *types.Pointer, *types.Map, *types.Chan, *types.Signature, *types.Slice:
		return true
	default:
		return false
	}
}

// bodyDefers reports whether a body holds a defer.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
//
// Returns bool which is true when the body defers a call.
func bodyDefers(body []isa.Instruction) bool {
	return slices.ContainsFunc(body, func(instruction isa.Instruction) bool { return instruction.Op == isa.OpDefer })
}
