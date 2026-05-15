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
	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// maxParamSlotsScanned bounds the per-bank bitmap. Functions with more params per bank
	// fall through to markAllParamSlotsWritten.
	maxParamSlotsScanned = 32

	// instructionOperandCount is the number of explicit operand bytes (A, B, C) carried by
	// each instruction.
	instructionOperandCount = 3
)

var (
	// phase2OpcodeAllowList enumerates every opcode the splice driver may emit into the
	// caller, built from the per-category slices below.
	phase2OpcodeAllowList = func() [256]bool {
		var table [256]bool
		for _, op := range phase2AllowedTier0Categories {
			table[op] = true
		}
		return table
	}()

	// phase2Tier1SubOpAllowList enumerates the tier-1 sub-ops the splice driver may emit
	// into the caller. Absent sub-ops are refused at scan time.
	phase2Tier1SubOpAllowList = func() [256]bool {
		var table [256]bool
		for _, sub := range phase2AllowedTier1SubOps {
			table[sub] = true
		}
		return table
	}()

	// phase2AllowedTier1SubOps lists the tier-1 sub-ops the splice driver supports.
	// isa.SubOpCallScalar is deliberately absent because inlining a callee that itself calls
	// changes what runtime.Caller() observes, and the runtime's frame walk lacks inline
	// provenance to compensate.
	phase2AllowedTier1SubOps = []isa.SubOpcode{
		isa.SubOpJump,
		isa.SubOpMoveInt, isa.SubOpMoveFloat, isa.SubOpMoveString,
		isa.SubOpMoveBool, isa.SubOpMoveUint,
		isa.SubOpLoadIntConstSmall, isa.SubOpLoadBool, isa.SubOpLoadBoolConst,
		isa.SubOpCall, isa.SubOpTailCall,
		isa.SubOpLtIntJumpFalse, isa.SubOpLeIntJumpFalse, isa.SubOpGtIntJumpFalse,
		isa.SubOpGeIntJumpFalse, isa.SubOpEqIntJumpFalse, isa.SubOpNeIntJumpFalse,
		isa.SubOpLeIntConstJumpFalse, isa.SubOpLtIntConstJumpFalse, isa.SubOpEqIntConstJumpFalse,
		isa.SubOpEqIntConstJumpTrue, isa.SubOpGeIntConstJumpFalse, isa.SubOpGtIntConstJumpFalse,
		isa.SubOpEqStringConstJumpFalse,
		isa.SubOpDrillTier2,
	}

	// phase2AllowedTier0Categories is the flat union of every opcode category the splice
	// driver allows.
	phase2AllowedTier0Categories = func() []isa.Opcode {
		categories := [][]isa.Opcode{
			{isa.OpExt, isa.OpCopyStructFieldGeneralT0},
			phase2AllowedConstantLoads,
			phase2AllowedConstFusedArith,
			phase2AllowedTier0StructField,
			phase2AllowedPureArithmetic,
			phase2AllowedComparisons,
			phase2AllowedJumpsAndFusions,
			phase2AllowedGeneralEquality,
			phase2AllowedFieldAccessors,
			phase2AllowedMoveAndString,
			phase2AllowedMapOps,
			phase2AllowedExtensionOps,
			phase2AllowedMapIndexOkVariants,
			phase2AllowedTypedMapOps,
		}
		var total int
		for _, group := range categories {
			total += len(group)
		}
		out := make([]isa.Opcode, 0, total)
		for _, group := range categories {
			out = append(out, group...)
		}
		return out
	}()

	// phase2AllowedConstantLoads carries the constant-load opcodes the splice driver allows.
	phase2AllowedConstantLoads = []isa.Opcode{
		isa.OpLoadIntConst, isa.OpLoadFloatConst, isa.OpLoadStringConst,
		isa.OpLoadUintConst, isa.OpLoadComplexConst,
		isa.OpLoadGeneralConst,
	}

	// phase2AllowedConstFusedArith carries the const-fused arithmetic opcodes the splice
	// driver allows.
	phase2AllowedConstFusedArith = []isa.Opcode{
		isa.OpAddIntConst, isa.OpSubIntConst, isa.OpMulIntConst,
	}

	// phase2AllowedTier0StructField carries the tier-0 struct-field opcodes whose operand C
	// is a StructLayoutTable index merged via inlinePoolShapes.
	phase2AllowedTier0StructField = []isa.Opcode{
		isa.OpGetStructFieldIntT0, isa.OpSetStructFieldIntT0,
		isa.OpGetStructFieldUint, isa.OpSetStructFieldUint,
		isa.OpGetStructFieldFloat, isa.OpSetStructFieldFloat,
		isa.OpGetStructFieldBool, isa.OpSetStructFieldBool,
		isa.OpGetStructFieldGeneral, isa.OpSetStructFieldGeneral,
	}

	// phase2AllowedPureArithmetic carries the register-only arithmetic opcodes the splice
	// driver allows.
	phase2AllowedPureArithmetic = []isa.Opcode{
		isa.OpAddInt, isa.OpSubInt, isa.OpMulInt, isa.OpDivInt, isa.OpRemInt,
		isa.OpBitAnd, isa.OpBitOr, isa.OpBitXor, isa.OpBitAndNot,
		isa.OpShiftLeft, isa.OpShiftRight,
		isa.OpAddFloat, isa.OpSubFloat, isa.OpMulFloat, isa.OpDivFloat,
		isa.OpAddUint, isa.OpSubUint, isa.OpMulUint, isa.OpDivUint, isa.OpRemUint,
		isa.OpBitAndUint, isa.OpBitOrUint, isa.OpBitXorUint, isa.OpBitAndNotUint,
		isa.OpShiftLeftUint, isa.OpShiftRightUint,
	}

	// phase2AllowedComparisons carries the typed comparison opcodes the splice driver
	// allows.
	phase2AllowedComparisons = []isa.Opcode{
		isa.OpEqInt, isa.OpNeInt, isa.OpLtInt, isa.OpLeInt, isa.OpGtInt, isa.OpGeInt,
		isa.OpEqFloat, isa.OpNeFloat, isa.OpLtFloat, isa.OpLeFloat, isa.OpGtFloat, isa.OpGeFloat,
		isa.OpEqUint, isa.OpNeUint, isa.OpLtUint, isa.OpLeUint, isa.OpGtUint, isa.OpGeUint,
		isa.OpEqString, isa.OpNeString, isa.OpLtString, isa.OpLeString, isa.OpGtString, isa.OpGeString,
	}

	// phase2AllowedJumpsAndFusions carries the jump and compare+jump fusion opcodes the
	// splice driver allows. opTestNilJump* are included because LRU's linked-list traversal
	// depends on them.
	phase2AllowedJumpsAndFusions = []isa.Opcode{
		isa.OpJumpIfTrue, isa.OpJumpIfFalse,
		isa.OpTestNilJumpTrue, isa.OpTestNilJumpFalse,
	}

	// phase2AllowedGeneralEquality carries general-bank equality opcodes. Needed for LRU's
	// `node == cache.head` style comparisons.
	phase2AllowedGeneralEquality = []isa.Opcode{
		isa.OpEqGeneral, isa.OpNeGeneral,
	}

	// phase2AllowedFieldAccessors carries the slow opGet/Set field opcodes where operand B/C
	// is a uint8 field index and no pool merge is needed.
	phase2AllowedFieldAccessors = []isa.Opcode{
		isa.OpGetField, isa.OpSetField, isa.OpGetFieldInt, isa.OpSetFieldInt,
	}

	// phase2AllowedMoveAndString carries the move and string-concat opcodes the splice
	// driver allows.
	phase2AllowedMoveAndString = []isa.Opcode{
		isa.OpMoveGeneral, isa.OpConcatString,
	}

	// phase2AllowedMapOps carries the pure-register map opcodes (no extension) the splice
	// driver allows.
	phase2AllowedMapOps = []isa.Opcode{
		isa.OpMapIndex, isa.OpMapSet,
	}

	// phase2AllowedExtensionOps carries extension-word ops whose extension
	// A|B<<InstructionByteShift is a typeTable pool index.
	phase2AllowedExtensionOps = []isa.Opcode{
		isa.OpMakeSlice, isa.OpConvert, isa.OpTypeAssert, isa.OpAllocIndirect, isa.OpPackTyped,
	}

	// phase2AllowedMapIndexOkVariants carries isa.OpMapIndexOk variants whose extension
	// carries an okRegister to remap.
	phase2AllowedMapIndexOkVariants = []isa.Opcode{
		isa.OpMapIndexOk,
		isa.OpMapIndexOkIntInt, isa.OpMapIndexOkIntString, isa.OpMapIndexOkIntGeneral,
		isa.OpMapIndexOkStringInt, isa.OpMapIndexOkStringString,
	}

	// phase2AllowedTypedMapOps carries the typed map operations with primitive key+value
	// banks. The comma-ok variants are listed in phase2AllowedMapIndexOkVariants; these
	// direct variants are pure-register without extension.
	phase2AllowedTypedMapOps = []isa.Opcode{
		isa.OpMapGetIntInt, isa.OpMapSetIntInt,
		isa.OpMapGetStringInt, isa.OpMapSetStringInt,
		isa.OpMapGetIntString, isa.OpMapSetIntString,
		isa.OpMapGetStringString, isa.OpMapSetStringString,
		isa.OpMapGetIntGeneral,
		isa.OpMapAddIntInt, isa.OpMapAddStringInt,
	}
)

// spliceResult reports the outcome of a single splice attempt.
//
//exhaustruct:ignore
type spliceResult struct {
	// Spliced is true when the splice succeeded and the caller's body was mutated.
	Spliced bool

	// Reason records the refusal kind when spliced is false.
	Reason program.InlineRefusal
}

// spliceSnapshot captures every caller slice length / numRegisters value that the splice
// may mutate. RestoreSpliceSnapshot uses it on failure to roll the caller back to its
// pre-splice state.
type spliceSnapshot struct {
	// bodyLen records the caller body length before the splice.
	bodyLen int

	// numRegisters records the per-bank register counts before the splice.
	numRegisters [isa.NumRegisterKinds]uint32

	// callSites records the caller's callSites slice length.
	callSites int

	// intConsts records the caller's intConstants slice length.
	intConsts int

	// floatConsts records the caller's floatConstants slice length.
	floatConsts int

	// stringConsts records the caller's stringConstants slice length.
	stringConsts int

	// boolConsts records the caller's boolConstants slice length.
	boolConsts int

	// uintConsts records the caller's uintConstants slice length.
	uintConsts int

	// complexConsts records the caller's complexConstants slice length.
	complexConsts int

	// generalConsts records the caller's generalConstants slice length.
	generalConsts int

	// typeTable records the caller's typeTable slice length.
	typeTable int

	// structLayoutTable records the caller's structLayoutTable slice length.
	structLayoutTable int

	// positions records the caller's debug source-map positions length, or zero when the
	// caller carries no source map.
	positions int
}

// TrySpliceCall attempts to inline the call at site siteIndex in caller.
//
// Takes caller (*CompiledFunction) whose body is mutated on success.
// Takes siteIndex (uint16) which selects the call site within caller.callSites.
//
// Returns spliceResult which is the splice outcome.
//
//revive:disable-next-line:unexported-return // package-internal type stays unexported.
func TrySpliceCall(caller *program.CompiledFunction, siteIndex uint16) spliceResult {
	return trySpliceCallAt(caller, siteIndex, -1)
}

// CrossBankAdoptOrBoxSubOp returns the tier-1 sub-op bridging two register banks. Only
// general-to-typed-slice (adopt) and typed-slice-to-general (box) pairs are supported.
//
// Takes sourceKind (isa.RegisterKind) which holds the caller's argument.
// Takes destinationKind (isa.RegisterKind) which is the callee's parameter slot bank.
//
// Returns isa.SubOpcode which is the matching adopt/box sub-op when supported.
// Returns bool which is true on a supported pair.
func CrossBankAdoptOrBoxSubOp(sourceKind, destinationKind isa.RegisterKind) (isa.SubOpcode, bool) {
	if sourceKind == isa.RegisterGeneral {
		switch destinationKind {
		case isa.RegisterSliceInt:
			return isa.SubOpAdoptGeneralToSlicesInt, true
		case isa.RegisterSliceFloat:
			return isa.SubOpAdoptGeneralToSlicesFloat, true
		case isa.RegisterSliceString:
			return isa.SubOpAdoptGeneralToSlicesString, true
		case isa.RegisterSliceBool:
			return isa.SubOpAdoptGeneralToSlicesBool, true
		case isa.RegisterSliceUint:
			return isa.SubOpAdoptGeneralToSlicesUint, true
		case isa.RegisterSliceByte:
			return isa.SubOpAdoptGeneralToSlicesByte, true
		default:
		}
		return 0, false
	}
	if destinationKind == isa.RegisterGeneral {
		switch sourceKind {
		case isa.RegisterSliceInt:
			return isa.SubOpBoxSliceInt, true
		case isa.RegisterSliceFloat:
			return isa.SubOpBoxSliceFloat, true
		case isa.RegisterSliceString:
			return isa.SubOpBoxSliceString, true
		case isa.RegisterSliceBool:
			return isa.SubOpBoxSliceBool, true
		case isa.RegisterSliceUint:
			return isa.SubOpBoxSliceUint, true
		case isa.RegisterSliceByte:
			return isa.SubOpBoxSliceByte, true
		default:
		}
		return 0, false
	}
	return 0, false
}

// CaptureSpliceSnapshot records the caller's mutable state prior to the splice so any
// failure can roll back cleanly.
//
// Takes caller (*CompiledFunction) whose state is captured.
//
// Returns the snapshot describing the pre-splice state.
//
//revive:disable-next-line:unexported-return // package-internal type stays unexported.
func CaptureSpliceSnapshot(caller *program.CompiledFunction) spliceSnapshot {
	return spliceSnapshot{
		bodyLen:           len(caller.Body),
		numRegisters:      caller.NumRegisters,
		callSites:         len(caller.CallSites),
		intConsts:         len(caller.IntConstants),
		floatConsts:       len(caller.FloatConstants),
		stringConsts:      len(caller.StringConstants),
		boolConsts:        len(caller.BoolConstants),
		uintConsts:        len(caller.UintConstants),
		complexConsts:     len(caller.ComplexConstants),
		generalConsts:     len(caller.GeneralConstants),
		typeTable:         len(caller.TypeTable),
		structLayoutTable: len(caller.StructLayoutTable),
		positions:         sourceMapPositionsLength(caller),
	}
}

// RestoreSpliceSnapshot rewinds every caller slice and numRegisters counter to the
// lengths recorded in the snapshot. The constant-pool dedup indexes are dropped because
// stale entries would point past the truncated pool and addDedupedConstant lazily
// re-seeds them on next use.
//
// Takes caller (*CompiledFunction) whose state is restored.
// Takes snapshot (SpliceSnapshot) carrying the pre-splice lengths.
func RestoreSpliceSnapshot(caller *program.CompiledFunction, snapshot spliceSnapshot) {
	caller.Body = caller.Body[:snapshot.bodyLen]
	caller.NumRegisters = snapshot.numRegisters
	caller.CallSites = caller.CallSites[:snapshot.callSites]
	caller.IntConstants = caller.IntConstants[:snapshot.intConsts]
	caller.FloatConstants = caller.FloatConstants[:snapshot.floatConsts]
	caller.StringConstants = caller.StringConstants[:snapshot.stringConsts]
	caller.BoolConstants = caller.BoolConstants[:snapshot.boolConsts]
	caller.UintConstants = caller.UintConstants[:snapshot.uintConsts]
	caller.ComplexConstants = caller.ComplexConstants[:snapshot.complexConsts]
	caller.GeneralConstants = caller.GeneralConstants[:snapshot.generalConsts]
	caller.TypeTable = caller.TypeTable[:snapshot.typeTable]
	caller.StructLayoutTable = caller.StructLayoutTable[:snapshot.structLayoutTable]
	caller.IntConstIndex = nil
	caller.FloatConstIndex = nil
	caller.StringConstIndex = nil
	caller.UintConstIndex = nil
	caller.ComplexConstIndex = nil
	caller.TypeRefIndex = nil
	truncateSourceMapPositions(caller, snapshot.positions)
}

// SplitJumpOffsetBytes splits a signed offset into the B and C bytes.
//
// Mirrors the internal isa.SplitOffset helper but kept local to the inliner.
//
// Takes offset (int) which is the signed PC-relative offset.
//
// Returns lowByte (the low operand-B byte of the encoded offset).
// Returns highByte (the high operand-C byte of the encoded offset).
func SplitJumpOffsetBytes(offset int) (lowByte, highByte byte) {
	raw := safeconv.Int16ToUint16(safeconv.MustIntToInt16(offset))
	return byte(raw & 0xFF), byte((raw >> program.InstructionByteShift) & 0xFF)
}

// trySpliceCallAt is the inner splice driver.
//
// When opCallPCHint >= 0 it is trusted as the call's PC; otherwise the body is scanned
// via findOpCallPC. Callers in inlineCallsIn pass a precomputed hint so the per-site body
// scan is avoided.
//
// Takes caller (*CompiledFunction) whose body is mutated on success.
// Takes siteIndex (uint16) which selects the call site within caller.callSites.
// Takes opCallPCHint (int) which is the isa.SubOpCall PC, or -1 to force a body scan.
//
// Returns spliceResult which is the splice outcome.
func trySpliceCallAt(caller *program.CompiledFunction, siteIndex uint16, opCallPCHint int) spliceResult {
	if caller == nil {
		return spliceResult{Reason: program.InlineRefusalUnknown}
	}
	if int(siteIndex) >= len(caller.CallSites) {
		return spliceResult{Reason: program.InlineRefusalUnknown}
	}
	site := &caller.CallSites[siteIndex]
	callee := site.CachedCallee
	if callee == nil {
		return spliceResult{Reason: program.InlineRefusalNoBody}
	}
	opCallPC := opCallPCHint
	if opCallPC < 0 {
		opCallPC = findOpCallPC(caller, siteIndex)
	}
	if opCallPC < 0 {
		return spliceResult{Reason: program.InlineRefusalUnknown}
	}
	if reason := phase2OpcodeScan(callee); reason != program.InlineEligible {
		return spliceResult{Reason: reason}
	}
	ctx := inlineContext{caller: caller,
		callee:    callee,
		site:      site,
		siteIndex: siteIndex,
		opCallPC:  opCallPC, paramPreCopies: nil, remap: [isa.NumRegisterKinds][isa.GeneralRegisterBankSize]int16{}}
	if reason := buildRegisterRemap(&ctx); reason != program.InlineEligible {
		return spliceResult{Reason: reason}
	}
	if reason := performSplice(&ctx); reason != program.InlineEligible {
		return spliceResult{Reason: reason}
	}
	return spliceResult{Spliced: true}
}

// phase2OpcodeScan rejects callees containing any unhandled opcode.
//
// The check is allowlist-based: every opcode the splice driver must support has to be on
// this list. Adding a new opcode involves verifying both (a) its operand shape is
// described in isa.OperandShapes, and (b) any pool indices it references are wired into
// inlinePoolShapes.
//
// Takes callee (*CompiledFunction) whose body is scanned.
//
// Returns InlineRefusal which is InlineEligible when every opcode is on the allowlist, or
// InlineRefusalOversize when an unsupported opcode is encountered.
func phase2OpcodeScan(callee *program.CompiledFunction) program.InlineRefusal {
	for i := range callee.Body {
		instr := callee.Body[i]
		if !phase2OperationAllowed(instr) {
			return program.InlineRefusalOversize
		}
	}
	return program.InlineEligible
}

// phase2OperationAllowed reports whether the splice driver supports the operation inst
// executes. Tier-1 instructions are checked against their sub-opcode, not the op byte, so
// new sub-ops are not silently admitted.
//
// Takes inst (isa.Instruction) which is the instruction under inspection.
//
// Returns bool which is true when the operation is on the splice driver's allowlist.
func phase2OperationAllowed(inst isa.Instruction) bool {
	if inst.Op == isa.OpDrillTier1 {
		return phase2Tier1SubOpAllowList[inst.A]
	}
	return phase2OpcodeAllowList[inst.Op]
}

// buildRegisterRemap fills ctx.remap for every callee-side register, giving parameter
// slots that the callee writes to fresh caller-side slots and a pre-copy MOVE so the
// inlined body cannot clobber the caller's argument variable while read-only params alias
// directly.
//
// Takes ctx (*inlineContext) which holds caller, callee, site, and the remap arrays being
// populated.
//
// Returns InlineEligible on success, InlineRefusalCapWatermark on register-watermark
// overflow, or InlineRefusalVariadic when argument and parameter counts disagree.
func buildRegisterRemap(ctx *inlineContext) program.InlineRefusal {
	ctx.resetRegisterRemap()
	ctx.paramPreCopies = ctx.paramPreCopies[:0]
	if len(ctx.site.Arguments) != len(ctx.callee.ParameterKinds) {
		return program.InlineRefusalVariadic
	}
	calleeSlots, isParamSlot := resolveCalleeParameterSlots(ctx.callee)
	if refusal := remapInlineParameters(ctx, calleeSlots); refusal != program.InlineEligible {
		return refusal
	}
	return remapInlineLocals(ctx, &isParamSlot)
}

// resolveCalleeParameterSlots returns per-parameter slot indices.
//
// Computes the per-parameter callee slot index and the per-bank bitmap of slots occupied
// by parameters. parameterRegisters, when populated, gives the slot promoteToIndirect
// ended up at after any heap-promote prologue allocations pushed later same-bank
// parameters off the naive per-bank counter; older synthetic functions and tests fall
// back to the per-bank counter.
//
// Takes callee (*CompiledFunction) whose parameter layout is resolved.
//
// Returns []uint8 which holds the callee slot index per parameter.
// Returns [NumRegisterKinds][maxParamSlotsScanned]bool which marks slots occupied by
// parameters.
func resolveCalleeParameterSlots(callee *program.CompiledFunction) ([]uint8, [isa.NumRegisterKinds][maxParamSlotsScanned]bool) {
	var bankParamCounter [isa.NumRegisterKinds]uint8
	calleeSlots := make([]uint8, len(callee.ParameterKinds))
	hasRecordedRegisters := len(callee.ParameterRegisters) == len(callee.ParameterKinds)
	var isParamSlot [isa.NumRegisterKinds][maxParamSlotsScanned]bool
	for i, paramKind := range callee.ParameterKinds {
		var slot uint8
		if hasRecordedRegisters {
			slot = callee.ParameterRegisters[i]
		} else {
			slot = bankParamCounter[paramKind]
		}
		bankParamCounter[paramKind]++
		calleeSlots[i] = slot
		if int(slot) < maxParamSlotsScanned {
			isParamSlot[paramKind][slot] = true
		}
	}
	return calleeSlots, isParamSlot
}

// remapInlineParameters wires each callee parameter slot to its source.
//
// Arguments of a different kind to the parameter are routed through a fresh same-kind
// temporary plus a param-pre-copy; arguments to a slot the callee writes to also get a
// fresh temporary so the caller's slot is not clobbered by the inlined body.
//
// Takes ctx (*inlineContext) which holds caller, callee, and the remap table being
// populated.
// Takes calleeSlots ([]uint8) which is the per-parameter slot index.
//
// Returns InlineRefusal which is InlineEligible on success or the refusal kind on
// failure.
func remapInlineParameters(ctx *inlineContext, calleeSlots []uint8) program.InlineRefusal {
	writtenParamSlots := scanWrittenParamSlots(ctx.callee)
	for i, paramKind := range ctx.callee.ParameterKinds {
		argument := ctx.site.Arguments[i]
		calleeSlot := calleeSlots[i]
		if argument.Kind != paramKind {
			if _, supported := CrossBankAdoptOrBoxSubOp(argument.Kind, paramKind); !supported {
				return program.InlineRefusalCapWatermark
			}
			if refusal := allocateInlineParamCopy(ctx, paramKind, calleeSlot, argument.Kind, argument.Register); refusal != program.InlineEligible {
				return refusal
			}
			continue
		}
		if !writtenParamSlots[paramKind][calleeSlot] {
			ctx.setRegister(paramKind, calleeSlot, argument.Register)
			continue
		}
		if refusal := allocateInlineParamCopy(ctx, paramKind, calleeSlot, paramKind, argument.Register); refusal != program.InlineEligible {
			return refusal
		}
	}
	return program.InlineEligible
}

// allocateInlineParamCopy reserves a fresh caller slot for a defensive copy.
//
// Reserves the slot for a parameter that needs a defensive copy (cross- kind argument or
// a slot the callee writes to) and records the matching pre-copy entry.
//
// Takes ctx (*inlineContext) which holds caller and paramPreCopies.
// Takes paramKind (isa.RegisterKind) which is the callee parameter bank.
// Takes calleeSlot (uint8) which is the callee-side slot being remapped.
// Takes sourceKind (isa.RegisterKind) which is the caller argument bank.
// Takes source (uint8) which is the caller argument slot.
//
// Returns InlineRefusal which is InlineEligible on success or InlineRefusalCapWatermark
// when the bank overflows.
func allocateInlineParamCopy(ctx *inlineContext, paramKind isa.RegisterKind, calleeSlot uint8, sourceKind isa.RegisterKind, source uint8) program.InlineRefusal {
	fresh := ctx.caller.NumRegisters[paramKind]
	if fresh > registerBankWatermark {
		return program.InlineRefusalCapWatermark
	}
	ctx.setRegister(paramKind, calleeSlot, uint8(fresh))
	ctx.caller.NumRegisters[paramKind] = fresh + 1
	ctx.paramPreCopies = append(ctx.paramPreCopies, paramPreCopy{
		sourceKind:  sourceKind,
		kind:        paramKind,
		destination: uint8(fresh),
		Source:      source,
	})
	return program.InlineEligible
}

// remapInlineLocals maps every non-parameter callee slot to a fresh local.
//
// Covers heap-promote pointer slots, temporaries, and named results. With recorded
// parameterRegisters the parameter slots may be scattered (e.g. general[0] is a
// heap-promote local but general[1] holds the parameter), so isParamSlot is honoured
// explicitly to keep scattered locals from reusing a parameter remap.
//
// Takes ctx (*inlineContext) which holds caller, callee, and the remap table.
// Takes isParamSlot (*[NumRegisterKinds][maxParamSlotsScanned]bool) which marks slots
// already mapped as parameters.
//
// Returns InlineRefusal which is InlineEligible on success or InlineRefusalCapWatermark
// when any bank overflows.
func remapInlineLocals(ctx *inlineContext, isParamSlot *[isa.NumRegisterKinds][maxParamSlotsScanned]bool) program.InlineRefusal {
	for k := range isa.RegisterKind(isa.NumRegisterKinds) {
		calleeTotal := ctx.callee.NumRegisters[k]
		if calleeTotal == 0 {
			continue
		}
		callerBase := ctx.caller.NumRegisters[k]
		var allocated uint32
		for slot := range calleeTotal {
			if int(slot) < maxParamSlotsScanned && isParamSlot[k][slot] {
				continue
			}
			if callerBase+allocated > registerBankWatermark {
				return program.InlineRefusalCapWatermark
			}
			ctx.setRegister(k, safeconv.MustUintToUint8(uint(slot)), safeconv.MustUintToUint8(uint(callerBase+allocated)))
			allocated++
		}
		ctx.caller.NumRegisters[k] = callerBase + allocated
	}
	return program.InlineEligible
}

// scanWrittenParamSlots returns a per-bank bitmap of callee parameter slots that are
// written to, treating dynamic-role operands conservatively as writes. Unwritten params
// can alias the caller's argument register directly, avoiding a pre-copy MOVE.
//
// Takes callee (*CompiledFunction) whose body is scanned.
//
// Returns the per-bank write bitmap.
func scanWrittenParamSlots(callee *program.CompiledFunction) [isa.NumRegisterKinds][maxParamSlotsScanned]bool {
	var written [isa.NumRegisterKinds][maxParamSlotsScanned]bool
	for i := range callee.Body {
		if !recordInstructionParamWrites(&written, callee, callee.Body[i]) {
			return written
		}
	}
	return written
}

// recordInstructionParamWrites records the parameter slots one instruction writes.
//
// A return reads the result slots and writes nothing. A call writes the registers its
// call site names as results. An undescribed operation, an unresolvable call site, or an
// operation whose writes reach registers its operand bytes do not name marks every slot
// written and ends the scan.
//
// Takes written (*[...]bool) which is the per-bank table being populated.
// Takes callee (*program.CompiledFunction) which owns the call-site table.
// Takes instr (instruction) which is the instruction under inspection.
//
// Returns false when every slot has been marked and the scan can stop.
func recordInstructionParamWrites(written *[isa.NumRegisterKinds][maxParamSlotsScanned]bool, callee *program.CompiledFunction, instr isa.Instruction) bool {
	if program.IsReturnInstruction(instr) {
		return true
	}
	shape := isa.ShapeForInstruction(instr)
	if shape.Flags&isa.ShapeFlagDescribed == 0 {
		markAllParamSlotsWritten(written, callee)
		return false
	}
	if isCallSiteInstruction(instr) {
		if recordCallSiteParamWrites(written, callee, instr) {
			return true
		}
		markAllParamSlotsWritten(written, callee)
		return false
	}
	if shape.Flags&isa.ShapeFlagOpaqueWrites != 0 {
		markAllParamSlotsWritten(written, callee)
		return false
	}
	if isa.InstrIsTier1SubOp(instr, isa.SubOpMapDelete) && int(instr.B) < maxParamSlotsScanned {
		written[isa.RegisterGeneral][instr.B] = true
	}
	return recordDescribedParamWrites(written, callee, instr, shape)
}

// markAllParamSlotsWritten marks every callee parameter slot as written.
//
// Used as the conservative fallback when the inliner cannot classify an opcode's writes
// (unknown shape, isa.RoleRegDynamic operand). Forces every param through the pre-copy
// path so no aliasing can occur.
//
// Takes written (*[NumRegisterKinds][maxParamSlotsScanned]bool) which is the per-bank
// bitmap being populated.
// Takes callee (*CompiledFunction) whose parameterKinds drive which slots get marked.
func markAllParamSlotsWritten(written *[isa.NumRegisterKinds][maxParamSlotsScanned]bool, callee *program.CompiledFunction) {
	hasRecordedRegisters := len(callee.ParameterRegisters) == len(callee.ParameterKinds)
	var bankCounter [isa.NumRegisterKinds]uint8
	for i, paramKind := range callee.ParameterKinds {
		var slot uint8
		if hasRecordedRegisters {
			slot = callee.ParameterRegisters[i]
		} else {
			slot = bankCounter[paramKind]
		}
		bankCounter[paramKind]++
		if int(slot) < maxParamSlotsScanned {
			written[paramKind][slot] = true
		}
	}
}

// recordDescribedParamWrites marks param slots that an instruction writes, including
// indirect writes through a register handle (e.g. isa.OpSetField, isa.OpMapSet) that
// mutate the storage the register points at.
//
// Takes written (*[NumRegisterKinds][maxParamSlotsScanned]bool) which accumulates the
// per-bank write bitmap.
// Takes callee (*CompiledFunction) used for the conservative fallback on dynamic-role
// operands.
// Takes instr (isa.Instruction) whose operand bytes are read.
// Takes shape (isa.OperandShape) which carries the per-operand role and writes-bit
// annotations.
//
// Returns bool which is true when the scan should continue.
func recordDescribedParamWrites(written *[isa.NumRegisterKinds][maxParamSlotsScanned]bool, callee *program.CompiledFunction, instr isa.Instruction, shape isa.OperandShape) bool {
	bytes := [instructionOperandCount]uint8{instr.A, instr.B, instr.C}
	roles := [instructionOperandCount]isa.OperandRole{shape.A, shape.B, shape.C}
	indirectPos, indirectBank, hasIndirect := indirectWriteThroughOperand(instr.Op)
	for pos, role := range roles {
		isWrite := shape.Writes[pos]
		if hasIndirect && pos == indirectPos {
			slot := bytes[pos]
			if int(slot) < maxParamSlotsScanned {
				written[indirectBank][slot] = true
			}
			continue
		}
		if !isWrite {
			continue
		}
		bank, isReg := writtenOperandBank(instr, shape, role)
		if !isReg {
			markAllParamSlotsWritten(written, callee)
			return false
		}
		slot := bytes[pos]
		if int(slot) < maxParamSlotsScanned {
			written[bank][slot] = true
		}
	}
	return true
}

// writtenOperandBank resolves the bank a written operand addresses: a fixed register role
// names its bank directly and a dynamic role takes it from the instruction's kind-marker
// operand.
//
// Takes instr (instruction) which carries the kind-marker byte when the shape has one.
// Takes shape (isa.OperandShape) which is the instruction's operand shape.
// Takes role (isa.OperandRole) which is the written operand's role.
//
// Returns the bank and true, or false when the operand is not a register or its bank
// cannot be resolved.
func writtenOperandBank(instr isa.Instruction, shape isa.OperandShape, role isa.OperandRole) (isa.RegisterKind, bool) {
	if role != isa.RoleRegDynamic {
		return isa.KindForRole(role)
	}
	roles := [instructionOperandCount]isa.OperandRole{shape.A, shape.B, shape.C}
	operands := [instructionOperandCount]uint8{instr.A, instr.B, instr.C}
	for position, candidate := range roles {
		if candidate == isa.RoleKindMarker {
			kind := isa.RegisterKind(operands[position])
			return kind, int(kind) < int(isa.NumRegisterKinds)
		}
	}
	return 0, false
}

// isCallSiteInstruction reports whether instr names its registers through a call site
// rather than its operand bytes.
//
// Takes instr (instruction) which is the instruction under inspection.
//
// Returns true for the tier-1 call family.
func isCallSiteInstruction(instr isa.Instruction) bool {
	if instr.Op != isa.OpDrillTier1 {
		return false
	}
	switch isa.SubOpcode(instr.A) {
	case isa.SubOpCall, isa.SubOpTailCall, isa.SubOpCallScalar, isa.SubOpCallMethod,
		isa.SubOpCallMethodInlineable, isa.SubOpCallNative, isa.SubOpCallIIFE:
		return true
	default:
		return false
	}
}

// recordCallSiteParamWrites marks the registers a call writes its results into, read from
// the call site the instruction indexes.
//
// Takes written (*[...]bool) which is the per-bank table being populated.
// Takes callee (*program.CompiledFunction) which owns the call-site table.
// Takes instr (instruction) which is the call instruction.
//
// Returns false when the site cannot be resolved, so the caller marks every slot.
func recordCallSiteParamWrites(written *[isa.NumRegisterKinds][maxParamSlotsScanned]bool, callee *program.CompiledFunction, instr isa.Instruction) bool {
	siteIndex := int(instr.WideIndex())
	if siteIndex >= len(callee.CallSites) {
		return false
	}
	for _, result := range callee.CallSites[siteIndex].Returns {
		if int(result.Register) < maxParamSlotsScanned && int(result.Kind) < int(isa.NumRegisterKinds) {
			written[result.Kind][result.Register] = true
		}
	}
	return true
}

// indirectWriteThroughOperand reports an opcode's mutation-target operand position and
// bank for opcodes that mutate storage through a register handle.
//
// Takes op (isa.Opcode) which is the opcode under inspection.
//
// Returns int which is the operand position (0=A, 1=B, 2=C) of the mutation target.
// Returns isa.RegisterKind which is the bank of the mutation-target register.
// Returns bool which is true when op mutates through a register.
func indirectWriteThroughOperand(op isa.Opcode) (int, isa.RegisterKind, bool) {
	switch op {
	case isa.OpSetField, isa.OpSetFieldInt,
		isa.OpSetStructFieldIntT0, isa.OpSetStructFieldUint,
		isa.OpSetStructFieldFloat, isa.OpSetStructFieldBool,
		isa.OpSetStructFieldGeneral,
		isa.OpIndexSet,
		isa.OpMapSet,
		isa.OpMapSetIntInt, isa.OpMapSetIntString,
		isa.OpMapSetStringInt, isa.OpMapSetStringString, isa.OpMapSetStringGeneral,
		isa.OpMapSetIntGeneral,
		isa.OpMapAddIntInt, isa.OpMapAddStringInt:
		return 0, isa.RegisterGeneral, true
	default:
	}
	return 0, 0, false
}

// buildParamPreCopyInstruction emits the bytecode for one pre-copy MOVE.
//
// Copies the caller's argument source register into the fresh caller-side parameter slot
// the inliner allocated. Bank-typed pre-copies go through the corresponding tier-1 MOVE
// sub-op (same as the splice uses for return-prep moves); general-bank pre-copies go
// through the top-level isa.OpMoveGeneral with the dynamic mode marker that matches
// runtime copyCallArgs' ValueCopyForBoundary semantics, so arena-resident values are
// correctly snapshotted at the call boundary.
//
// Takes pc (paramPreCopy) which carries the bank and the destination/source slots.
//
// Returns the encoded instruction.
// Returns false when the bank has no corresponding tier-1 move sub-op.
func buildParamPreCopyInstruction(pc paramPreCopy) (isa.Instruction, bool) {
	if pc.sourceKind != pc.kind {
		subOp, ok := CrossBankAdoptOrBoxSubOp(pc.sourceKind, pc.kind)
		if !ok {
			return isa.Instruction{}, false
		}
		return isa.NewInstruction(isa.OpDrillTier1, byte(subOp), pc.destination, pc.Source), true
	}
	if pc.kind == isa.RegisterGeneral {
		return isa.NewInstruction(isa.OpMoveGeneral, pc.destination, pc.Source, engine.MoveGeneralModeDynamic), true
	}
	subOp, ok := typedMoveSubOp(pc.kind)
	if !ok {
		return isa.Instruction{}, false
	}
	return isa.NewInstruction(isa.OpDrillTier1, byte(subOp), pc.destination, pc.Source), true
}

// performSplice executes the actual bytecode mutation for one splice. On any failure the
// caller state is rolled back via CaptureSpliceSnapshot/RestoreSpliceSnapshot.
//
// Takes ctx (*inlineContext) holding caller, callee, site, opCallPC, remap, and
// paramPreCopies.
//
// Returns InlineEligible on success, or the refusal kind on rollback.
func performSplice(ctx *inlineContext) program.InlineRefusal {
	snapshot := CaptureSpliceSnapshot(ctx.caller)
	rollback := func() { RestoreSpliceSnapshot(ctx.caller, snapshot) }

	skipJumpPC := emitSpliceSkipJump(ctx)
	preCopyStart, ok := emitParamPreCopies(ctx)
	if !ok {
		rollback()
		return program.InlineRefusalCapWatermark
	}
	calleeBodyStart, refusal := appendCalleeBody(ctx)
	if refusal != program.InlineEligible {
		rollback()
		return refusal
	}
	retPrepStart := len(ctx.caller.Body)
	if reason := emitReturnPrep(ctx, retPrepStart); reason != program.InlineEligible {
		rollback()
		return reason
	}
	if !rewriteReturnsToRetPrep(ctx, calleeBodyStart, retPrepStart) {
		rollback()
		return program.InlineRefusalUnknown
	}
	if !patchOpCallForward(ctx, preCopyStart) {
		rollback()
		return program.InlineRefusalUnknown
	}
	if !patchSkipJump(ctx, skipJumpPC) {
		rollback()
		return program.InlineRefusalUnknown
	}
	recordSelfRecursionAnnotation(ctx, calleeBodyStart)
	recordInlineAnnotation(ctx, calleeBodyStart)
	return program.InlineEligible
}

// recordSelfRecursionAnnotation marks the first instruction of an inlined self-recursive
// callee body with peepholeRewriteUnroll.
//
// The origin is the original isa.SubOpCall PC that the unroll replaced; the annotation
// only applies to the self-recursive case (callee == caller); ordinary inlining is not
// annotated by this helper.
//
// Takes ctx (*inlineContext) which is the inline context carrying caller, callee, and
// opCallPC.
// Takes calleeBodyStart (int) which is the caller PC at which the appended body starts.
func recordSelfRecursionAnnotation(ctx *inlineContext, calleeBodyStart int) {
	if ctx == nil || ctx.caller == nil {
		return
	}
	if ctx.callee != ctx.caller {
		return
	}
	if calleeBodyStart < 0 || calleeBodyStart >= len(ctx.caller.Body) {
		return
	}
	passes.RecordPeepholeRewrite(ctx.caller, calleeBodyStart, passes.PeepholeRewriteUnroll, ctx.opCallPC)
}

// emitSpliceSkipJump appends a stub forward jump that vaults over the appended trampoline
// region. The offset is back-patched by patchSkipJump once the final body length is
// known.
//
// Takes ctx (*inlineContext) whose caller body receives the stub jump.
//
// Returns the PC of the stub jump.
func emitSpliceSkipJump(ctx *inlineContext) int {
	skipJumpPC := len(ctx.caller.Body)
	ctx.caller.Body = append(ctx.caller.Body, isa.NewInstruction(
		isa.OpDrillTier1,
		byte(isa.SubOpJump),
		0, 0,
	))
	ctx.appendSyntheticPosition()
	return skipJumpPC
}

// emitParamPreCopies appends one MOVE per parameter that the callee writes to, seeding
// fresh caller-side slots. Empty when all parameters are read-only.
//
// Takes ctx (*inlineContext) carrying caller and paramPreCopies.
//
// Returns int which is the PC at which the pre-copy block starts.
// Returns bool which is false when any pre-copy lacks a tier-1 move sub-op.
func emitParamPreCopies(ctx *inlineContext) (int, bool) {
	preCopyStart := len(ctx.caller.Body)
	for _, pc := range ctx.paramPreCopies {
		copyInstr, ok := buildParamPreCopyInstruction(pc)
		if !ok {
			return 0, false
		}
		ctx.caller.Body = append(ctx.caller.Body, copyInstr)
		ctx.appendSyntheticPosition()
	}
	return preCopyStart, true
}

// appendCalleeBody appends the callee body to the caller.
//
// Return instructions are temporarily emitted unchanged; rewriteReturnsToRetPrep patches
// them in a later step. Extension words (isa.OpExt) following ops marked hasExtensionWord
// get remapped and appended alongside their main instruction, with the loop counter
// advanced past them so they are not processed twice. isa.SubOpTailCall is rewritten to
// isa.SubOpCall after the remap: tail-call semantics in the original callee would replace
// the caller's frame with the tail-callee's frame, but once the callee has been inlined
// the surrounding continuation is the outer caller, so isa.SubOpCall preserves correct
// flow. Self-recursive tail calls are refused upstream at scanCalleeForRefusal to avoid
// unbounded frame growth.
//
// Takes ctx (*inlineContext) carrying caller, callee, and remap.
//
// Returns int which is the PC at which the callee body starts in the caller.
// Returns InlineRefusal which is InlineEligible on success or the refusal kind describing
// the failure.
func appendCalleeBody(ctx *inlineContext) (int, program.InlineRefusal) {
	calleeBodyStart := len(ctx.caller.Body)
	for i := 0; i < len(ctx.callee.Body); i++ {
		instr := ctx.callee.Body[i]
		if program.IsReturnInstruction(instr) {
			ctx.caller.Body = append(ctx.caller.Body, instr)
			ctx.appendCalleePosition(i)
			continue
		}
		remapped, ok := remapOperands(instr, ctx)
		if !ok {
			return 0, program.InlineRefusalUnknown
		}
		if isa.InstrIsTier1SubOp(remapped, isa.SubOpTailCall) {
			remapped.A = uint8(isa.SubOpCall)
		}
		ctx.caller.Body = append(ctx.caller.Body, remapped)
		ctx.appendCalleePosition(i)
		shape := inlinePoolShapeFor(instr)
		if !shape.hasExtensionWord {
			continue
		}
		if i+1 >= len(ctx.callee.Body) {
			return 0, program.InlineRefusalUnknown
		}
		ext := ctx.callee.Body[i+1]
		if ext.Op != isa.OpExt {
			return 0, program.InlineRefusalUnknown
		}
		remappedExt, ok := remapExtensionOperands(ext, shape, ctx)
		if !ok {
			return 0, program.InlineRefusalUnknown
		}
		ctx.caller.Body = append(ctx.caller.Body, remappedExt)
		ctx.appendCalleePosition(i + 1)
		i++
	}
	return calleeBodyStart, program.InlineEligible
}

// rewriteReturnsToRetPrep rewrites each return instruction inside the appended callee
// body into a forward jump that targets retPrepStart.
//
// Takes ctx (*inlineContext) carrying caller and callee.
// Takes calleeBodyStart (int) which is the caller PC at which the appended body starts.
// Takes retPrepStart (int) which is the caller PC of the return-prep block.
//
// Returns true on success; false when a return's offset cannot fit a signed 16-bit jump.
func rewriteReturnsToRetPrep(ctx *inlineContext, calleeBodyStart, retPrepStart int) bool {
	for i := range ctx.callee.Body {
		appendedPC := calleeBodyStart + i
		instr := ctx.caller.Body[appendedPC]
		if !program.IsReturnInstruction(instr) {
			continue
		}
		offset := retPrepStart - appendedPC - 1
		if !program.FitsJumpOffset(offset) {
			return false
		}
		lo, hi := SplitJumpOffsetBytes(offset)
		ctx.caller.Body[appendedPC] = isa.NewInstruction(
			isa.OpDrillTier1,
			byte(isa.SubOpJump),
			lo,
			hi,
		)
	}
	return true
}

// patchOpCallForward replaces the isa.SubOpCall at ctx.opCallPC with a forward jump to
// preCopyStart.
//
// Takes ctx (*inlineContext) carrying caller and opCallPC.
// Takes preCopyStart (int) which is the caller PC of the pre-copy block.
//
// Returns true on success, false when the jump offset overflows int16.
func patchOpCallForward(ctx *inlineContext, preCopyStart int) bool {
	offset := preCopyStart - ctx.opCallPC - 1
	if !program.FitsJumpOffset(offset) {
		return false
	}
	lo, hi := SplitJumpOffsetBytes(offset)
	ctx.caller.Body[ctx.opCallPC] = isa.NewInstruction(
		isa.OpDrillTier1,
		byte(isa.SubOpJump),
		lo,
		hi,
	)
	return true
}

// patchSkipJump back-patches the stub skip-jump emitted in emitSpliceSkipJump so it lands
// past the end of the appended trampoline (i.e., at the current end-of-body PC, where
// implicit return fires).
//
// Takes ctx (*inlineContext) carrying caller.
// Takes skipJumpPC (int) which is the PC of the stub jump.
//
// Returns true on success; false when the offset cannot fit a signed 16-bit immediate.
func patchSkipJump(ctx *inlineContext, skipJumpPC int) bool {
	pastTrampolinePC := len(ctx.caller.Body)
	offset := pastTrampolinePC - skipJumpPC - 1
	if !program.FitsJumpOffset(offset) {
		return false
	}
	lo, hi := SplitJumpOffsetBytes(offset)
	ctx.caller.Body[skipJumpPC] = isa.NewInstruction(
		isa.OpDrillTier1,
		byte(isa.SubOpJump),
		lo,
		hi,
	)
	return true
}

// emitReturnPrep appends the shared return-prep block to caller.body.
//
// Emits one typed-move sub-op per return value, copying from the callee's return-position
// slot (bank-K, slot i_in_bank, REMAPPED to caller-side via ctx.remap) to the caller's
// site.Returns[i] destination register. i_in_bank is a per-bank counter that matches how
// vm_handler_calls.handleReturn reads the values at runtime. A final unconditional
// isa.SubOpJump back to afterCallPC closes the block. Supports same-bank returns
// (resultKinds[i] == site.Returns[i].Kind). Cross-bank returns (e.g., int callee ->
// general interface caller) require boxing/unboxing and are refused with
// InlineRefusalCapWatermark.
//
// Correctness invariant: by the time the callee's return instruction fires at runtime,
// bank-K-slot-(i_in_bank) holds return value i. The Compiler enforces this via
// moveLocsToReturnPositions when the result expression does not naturally land in that
// slot. The per-bank counter mirrors handleReturn's bankCounters.
//
// Takes ctx (*inlineContext) which carries callee, site, and ctx.remap.
// Takes retPrepStart (int) which is unused at runtime; it names the PC where the first
// emitted instruction lands.
//
// Returns InlineEligible on success, InlineRefusalUnknown when result counts disagree or
// the back-jump offset will not fit, and InlineRefusalCapWatermark for unsupported
// cross-bank returns or missing tier-1 move sub-ops.
func emitReturnPrep(ctx *inlineContext, retPrepStart int) program.InlineRefusal {
	_ = retPrepStart
	resultCount := len(ctx.callee.ResultKinds)
	if resultCount != len(ctx.site.Returns) {
		return program.InlineRefusalUnknown
	}
	var bankResultCounter [isa.NumRegisterKinds]uint8
	for i, kind := range ctx.callee.ResultKinds {
		calleeSlot := bankResultCounter[kind]
		bankResultCounter[kind]++
		sourceRegister, ok := ctx.lookupRegister(kind, calleeSlot)
		if !ok {
			return program.InlineRefusalCapWatermark
		}
		destination := ctx.site.Returns[i]
		var subOp isa.SubOpcode
		if destination.Kind != kind {
			adapter, supported := CrossBankAdoptOrBoxSubOp(kind, destination.Kind)
			if !supported {
				return program.InlineRefusalCapWatermark
			}
			subOp = adapter
		} else {
			move, ok := typedMoveSubOp(kind)
			if !ok {
				return program.InlineRefusalCapWatermark
			}
			subOp = move
		}
		ctx.caller.Body = append(ctx.caller.Body, isa.NewInstruction(
			isa.OpDrillTier1,
			byte(subOp),
			destination.Register,
			sourceRegister,
		))
		ctx.appendSyntheticPosition()
	}
	afterCallPC := ctx.opCallPC + 1
	jumpPC := len(ctx.caller.Body)
	offset := afterCallPC - jumpPC - 1
	if !program.FitsJumpOffset(offset) {
		return program.InlineRefusalUnknown
	}
	lo, hi := SplitJumpOffsetBytes(offset)
	ctx.caller.Body = append(ctx.caller.Body, isa.NewInstruction(
		isa.OpDrillTier1,
		byte(isa.SubOpJump),
		lo,
		hi,
	))
	ctx.appendSyntheticPosition()
	return program.InlineEligible
}

// typedMoveSubOp returns the tier-1 register-to-register move sub-op for bank.
//
// Takes bank (isa.RegisterKind) which selects the move's bank.
//
// Returns the tier-1 sub-opcode for that bank.
// Returns false when the bank has no tier-1 move.
func typedMoveSubOp(bank isa.RegisterKind) (isa.SubOpcode, bool) {
	switch bank {
	case isa.RegisterInt:
		return isa.SubOpMoveInt, true
	case isa.RegisterFloat:
		return isa.SubOpMoveFloat, true
	case isa.RegisterString:
		return isa.SubOpMoveString, true
	case isa.RegisterBool:
		return isa.SubOpMoveBool, true
	case isa.RegisterUint:
		return isa.SubOpMoveUint, true
	case isa.RegisterSliceInt:
		return isa.SubOpMoveSliceInt, true
	case isa.RegisterSliceFloat:
		return isa.SubOpMoveSliceFloat, true
	case isa.RegisterSliceString:
		return isa.SubOpMoveSliceString, true
	case isa.RegisterSliceBool:
		return isa.SubOpMoveSliceBool, true
	case isa.RegisterSliceUint:
		return isa.SubOpMoveSliceUint, true
	case isa.RegisterSliceByte:
		return isa.SubOpMoveSliceByte, true
	default:
	}
	return 0, false
}

// remapOperands rewrites register and pool operands for a callee instruction.
// isa.OpDrillTier1 is routed through remapTier1SubOp for per-sub-op bank resolution.
//
// Takes instr (isa.Instruction) which is the callee-side instruction.
// Takes ctx (*inlineContext) holding caller pools and the remap table.
//
// Returns the rewritten instruction.
// Returns false on any failure (unknown shape, unmapped register, pool overflow).
func remapOperands(instr isa.Instruction, ctx *inlineContext) (isa.Instruction, bool) {
	out := instr
	if !remapPoolOperands(&out, inlinePoolShapeFor(instr), ctx) {
		return isa.Instruction{}, false
	}
	if instr.Op == isa.OpDrillTier1 {
		return remapTier1SubOp(out, ctx)
	}
	if !remapRegisterOperands(&out, instr.Op, ctx) {
		return isa.Instruction{}, false
	}
	return out, true
}

// remapPoolOperands merges every pool-index operand on instruction into the caller's
// tables.
//
// Handles three encodings: a B-byte pool index (bKindByte != poolNone) that must fit in 8
// bits, a C-byte pool index (cKindByte != poolNone) that must fit in 8 bits, and a
// B|C<<InstructionByteShift wide pool index (bcWide16 != poolNone) using the full 16-bit
// range. The poolCallSites variant routes through mergeCallSiteForCtx so register
// operands inside the merged site are remapped via ctx.
//
// Takes out (*instruction) which is mutated in place.
// Takes poolShape (inlinePoolShape) describing the encoding.
// Takes ctx (*inlineContext) holding caller pools.
//
// Returns true on success; false on overflow or unresolved merge.
func remapPoolOperands(out *isa.Instruction, poolShape inlinePoolShape, ctx *inlineContext) bool {
	if poolShape.bKindByte != poolNone {
		newIdx, ok := mergePoolIndex(ctx.caller, ctx.callee, poolShape.bKindByte, uint16(out.B), true)
		if !ok || newIdx >= byteEncodingLimit {
			return false
		}
		out.B = byte(newIdx)
	}
	if poolShape.cKindByte != poolNone {
		newIdx, ok := mergePoolIndex(ctx.caller, ctx.callee, poolShape.cKindByte, uint16(out.C), true)
		if !ok || newIdx >= byteEncodingLimit {
			return false
		}
		out.C = byte(newIdx)
	}
	if poolShape.bcWide16 != poolNone {
		return remapWidePoolOperand(out, poolShape.bcWide16, ctx)
	}
	return true
}

// remapWidePoolOperand merges a wide (B|C<<InstructionByteShift) pool-index operand into
// the caller's tables.
//
// Takes out (*instruction) which is mutated in place.
// Takes pool (inlinePool) which selects the destination pool.
// Takes ctx (*inlineContext) holding caller pools and the remap table.
//
// Returns true on success; false on overflow or unresolved merge.
func remapWidePoolOperand(out *isa.Instruction, pool inlinePool, ctx *inlineContext) bool {
	oldIdx := uint16(out.B) | uint16(out.C)<<program.InstructionByteShift
	var newIdx uint16
	var ok bool
	if pool == poolCallSites {
		newIdx, ok = mergeCallSiteForCtx(ctx, oldIdx, false)
	} else {
		newIdx, ok = mergePoolIndex(ctx.caller, ctx.callee, pool, oldIdx, false)
	}
	if !ok {
		return false
	}
	out.B = byte(newIdx & 0xFF)
	out.C = byte((newIdx >> program.InstructionByteShift) & 0xFF)
	return true
}

// remapRegisterOperands rewrites every register byte described by the opcode's
// isa.OperandShapes entry, looking each callee slot up in the caller-side remap table.
//
// Takes out (*instruction) which is mutated in place.
// Takes op (opcode) used to fetch the isa.OperandShapes entry.
// Takes ctx (*inlineContext) holding the remap table.
//
// Returns true on success; false when the opcode lacks a `described` shape or a
// referenced register has no remap entry.
func remapRegisterOperands(out *isa.Instruction, op isa.Opcode, ctx *inlineContext) bool {
	shape := isa.OperandShapeFor(op)
	if shape.Flags&isa.ShapeFlagDescribed == 0 {
		return false
	}
	bytePtrs := [instructionOperandCount]*byte{&out.A, &out.B, &out.C}
	roles := [instructionOperandCount]isa.OperandRole{shape.A, shape.B, shape.C}
	for pos, role := range roles {
		bank, isReg := isa.KindForRole(role)
		if !isReg {
			continue
		}
		oldSlot := *bytePtrs[pos]
		newSlot, ok := ctx.lookupRegister(bank, oldSlot)
		if !ok {
			return false
		}
		*bytePtrs[pos] = newSlot
	}
	return true
}

// remapExtensionOperands rewrites a follow-on isa.OpExt extension word.
//
// The extension's content is per-opcode: most carry a uint16 pool index in operands A|B,
// some carry a register reference, and some are pure immediates. The shape parameter
// selects which interpretation applies.
//
// Takes ext (instruction) which is the extension word emitted after its parent opcode.
// Takes shape (inlinePoolShape) describing the extension's operand layout.
// Takes ctx (*inlineContext) holding caller pools and the remap table.
//
// Returns the rewritten extension instruction.
// Returns false on any failure (pool overflow, unmapped register).
func remapExtensionOperands(ext isa.Instruction, shape inlinePoolShape, ctx *inlineContext) (isa.Instruction, bool) {
	out := ext
	if shape.extAWide16 != poolNone {
		oldIdx := uint16(out.A) | uint16(out.B)<<program.InstructionByteShift
		newIdx, ok := mergePoolIndex(ctx.caller, ctx.callee, shape.extAWide16, oldIdx, false)
		if !ok {
			return isa.Instruction{}, false
		}
		out.A = byte(newIdx & 0xFF)
		out.B = byte((newIdx >> program.InstructionByteShift) & 0xFF)
	}
	if shape.extARegSet {
		newSlot, ok := ctx.lookupRegister(shape.extARegBank, out.A)
		if !ok {
			return isa.Instruction{}, false
		}
		out.A = newSlot
	}
	return out, true
}

// remapTier1SubOp rewrites the register operands of an allow-listed tier-1 instruction
// whose pool operands remapOperands() has already merged.
//
// The register-operand layout is per sub-op: the moves and the register-to-register
// compare-and-branch fusions name two registers of one bank in B and C; the constant
// loads and the compare-against-constant fusions name one register in B beside a constant
// index or immediate in C; the calls name their registers only through the merged call
// site, so their operand bytes are left as remapped; the unconditional jump carries an
// offset. Anything else refuses, which phase2Tier1SubOpAllowList keeps unreachable.
//
// Takes instr (instruction) which is the tier-1 instruction with its pool operands
// merged.
// Takes ctx (*inlineContext) which supplies the register remap.
//
// Returns the rewritten instruction and true, or the zero instruction and false.
func remapTier1SubOp(instr isa.Instruction, ctx *inlineContext) (isa.Instruction, bool) {
	out := instr
	switch isa.SubOpcode(instr.A) {
	case isa.SubOpJump, isa.SubOpCall, isa.SubOpTailCall, isa.SubOpCallScalar:
		return out, true
	case isa.SubOpMoveFloat:
		return remapRegBC(&out, ctx, isa.RegisterFloat, isa.RegisterFloat)
	case isa.SubOpMoveString:
		return remapRegBC(&out, ctx, isa.RegisterString, isa.RegisterString)
	case isa.SubOpMoveBool:
		return remapRegBC(&out, ctx, isa.RegisterBool, isa.RegisterBool)
	case isa.SubOpMoveUint:
		return remapRegBC(&out, ctx, isa.RegisterUint, isa.RegisterUint)
	case isa.SubOpLoadIntConstSmall, isa.SubOpLoadBool,
		isa.SubOpLeIntConstJumpFalse, isa.SubOpLtIntConstJumpFalse,
		isa.SubOpEqIntConstJumpFalse, isa.SubOpEqIntConstJumpTrue,
		isa.SubOpGeIntConstJumpFalse, isa.SubOpGtIntConstJumpFalse:
		return remapTier1B(&out, ctx, isa.RegisterInt)
	case isa.SubOpLoadBoolConst:
		return remapTier1B(&out, ctx, isa.RegisterBool)
	case isa.SubOpEqStringConstJumpFalse:
		return remapTier1B(&out, ctx, isa.RegisterString)
	case isa.SubOpMoveInt,
		isa.SubOpLtIntJumpFalse, isa.SubOpLeIntJumpFalse,
		isa.SubOpGtIntJumpFalse, isa.SubOpGeIntJumpFalse,
		isa.SubOpEqIntJumpFalse, isa.SubOpNeIntJumpFalse:
		return remapRegBC(&out, ctx, isa.RegisterInt, isa.RegisterInt)
	case isa.SubOpDrillTier2:
		return remapTier2SubOp(&out, ctx)
	default:
	}
	return isa.Instruction{}, false
}

// remapTier1B rewrites operand B of a tier-1 instruction as a register of the given bank,
// leaving C, which holds a constant index or an immediate, untouched.
//
// Takes out (*instruction) which is the instruction to rewrite in place.
// Takes ctx (*inlineContext) which supplies the register remap.
// Takes bank (isa.RegisterKind) which is the bank operand B addresses.
//
// Returns the rewritten instruction and true, or the zero instruction and false when B is
// not mapped.
func remapTier1B(out *isa.Instruction, ctx *inlineContext, bank isa.RegisterKind) (isa.Instruction, bool) {
	newB, ok := remapRegByte(out.B, ctx, bank)
	if !ok {
		return isa.Instruction{}, false
	}
	out.B = newB
	return *out, true
}

// remapTier2SubOp rewrites the register payloads of a isa.SubOpDrillTier2 instruction.
// Operand B carries the tier-2 sub-op; operand C is its register operand (banked per
// sub-op).
//
// Takes out (*instruction) which is mutated in place.
// Takes ctx (*inlineContext) holding the remap table.
//
// Returns the rewritten instruction.
// Returns false on unsupported sub-ops or unresolved register lookups.
func remapTier2SubOp(out *isa.Instruction, ctx *inlineContext) (isa.Instruction, bool) {
	tier2 := isa.SubOpcodeTier2(out.B)
	switch tier2 {
	case isa.SubOpTier2DrillTier3:
		return *out, true
	case isa.SubOpTier2IncInt, isa.SubOpTier2DecInt:
		return remapTier2OperandC(out, ctx, isa.RegisterInt)
	case isa.SubOpTier2IncUint, isa.SubOpTier2DecUint:
		return remapTier2OperandC(out, ctx, isa.RegisterUint)
	case isa.SubOpTier2LoadNil, isa.SubOpTier2SetZero:
		return remapTier2OperandC(out, ctx, isa.RegisterGeneral)
	default:
	}
	return isa.Instruction{}, false
}

// remapTier2OperandC remaps operand C of a tier-2 sub-op as a register in the named bank.
//
// Takes out (*instruction) which is mutated in place when the lookup succeeds.
// Takes ctx (*inlineContext) holding the remap table.
// Takes bank (isa.RegisterKind) which is the bank carrying operand C.
//
// Returns the updated instruction by value (also reflected in *out).
// Returns false when the register has no remap entry.
func remapTier2OperandC(out *isa.Instruction, ctx *inlineContext, bank isa.RegisterKind) (isa.Instruction, bool) {
	newC, ok := remapRegByte(out.C, ctx, bank)
	if !ok {
		return isa.Instruction{}, false
	}
	out.C = newC
	return *out, true
}

// remapRegBC remaps operand B and operand C as registers in the given banks.
//
// Takes out (*instruction) which is mutated in place when both lookups succeed.
// Takes ctx (*inlineContext) holding the remap table.
// Takes bankB (isa.RegisterKind) which is the bank for operand B.
// Takes bankC (isa.RegisterKind) which is the bank for operand C.
//
// Returns the updated instruction by value (also reflected in *out).
// Returns false when either register has no remap entry.
func remapRegBC(out *isa.Instruction, ctx *inlineContext, bankB, bankC isa.RegisterKind) (isa.Instruction, bool) {
	newB, okB := remapRegByte(out.B, ctx, bankB)
	if !okB {
		return isa.Instruction{}, false
	}
	newC, okC := remapRegByte(out.C, ctx, bankC)
	if !okC {
		return isa.Instruction{}, false
	}
	out.B = newB
	out.C = newC
	return *out, true
}

// remapRegByte looks up a callee register byte in the remap table.
//
// Takes calleeSlot (byte) which is the callee-side register index.
// Takes ctx (*inlineContext) holding the remap table.
// Takes bank (isa.RegisterKind) which selects the bank.
//
// Returns the caller-side register byte.
// Returns false when no mapping exists for the (bank, calleeSlot) pair.
func remapRegByte(calleeSlot byte, ctx *inlineContext, bank isa.RegisterKind) (byte, bool) {
	return ctx.lookupRegister(bank, calleeSlot)
}
