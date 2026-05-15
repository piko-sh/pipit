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

package escape

import (
	"context"
	"fmt"
	"slices"

	"pipit.sh/pipit/internal/compile/inline"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/isa"
)

const (
	// cancellationCheckMask is the bitmask applied to the loop index for periodic
	// context-cancellation checks.
	cancellationCheckMask = 0xFF

	// maxEscapeFixpointIter caps the fixpoint iteration count as a safety net for
	// pathological call graphs. Typical inputs converge in 2-3 iterations.
	maxEscapeFixpointIter = 6
)

var (
	// tier1SafeSubOpTable is the precomputed allowlist of tier-1 sub-ops whose effect on a
	// tainted general register is non-escaping.
	tier1SafeSubOpTable = func() [isa.GeneralRegisterBankSize]bool {
		var table [isa.GeneralRegisterBankSize]bool
		for _, sub := range tier1SafeSubOpAllowList {
			table[sub] = true
		}
		return table
	}()

	// tier1SafeSubOpAllowList is the flat union of every category below. Each category
	// groups sub-ops by their escape rationale so the soundness reasoning stays close to the
	// membership decision.
	tier1SafeSubOpAllowList = func() []isa.SubOpcode {
		categories := [][]isa.SubOpcode{
			tier1SafeMathIntrinsics,
			tier1SafeStringConversion,
			tier1SafeComplexOps,
			tier1SafeTypedSliceMakes,
			tier1SafeTypedSliceLengths,
			tier1SafeTypedSliceGetSet,
			tier1SafeByteSliceOps,
			tier1SafeTypedBankMoves,
			tier1SafeTypedBankUnary,
			tier1SafeStringMisc,
			tier1SafeStringCaseTrim,
			tier1SafeConstantLoadsAndJump,
			tier1SafeFusedJumpArith,
			tier1SafeUintArithConst,
			tier1SafeStructFieldGet,
			tier1SafeStructFieldSet,
			tier1SafeStructFieldIncDec,
			tier1SafeAppendTypedValue,
			tier1SafeStarAppendByte,
			tier1SafeReadOnlyInspection,
			tier1SafeCrossBankMoves,
			tier1SafeGeneralInspection,
		}
		var total int
		for _, group := range categories {
			total += len(group)
		}
		out := make([]isa.SubOpcode, 0, total)
		for _, group := range categories {
			out = append(out, group...)
		}
		return out
	}()

	// tier1SafeMathIntrinsics carries float-bank-only math intrinsics.
	tier1SafeMathIntrinsics = []isa.SubOpcode{
		isa.SubOpMathSin, isa.SubOpMathCos, isa.SubOpMathExp, isa.SubOpMathTan, isa.SubOpMathMod,
		isa.SubOpMathSqrt, isa.SubOpMathAbs, isa.SubOpMathFloor, isa.SubOpMathCeil,
		isa.SubOpMathTrunc, isa.SubOpMathRound,
	}

	// tier1SafeStringConversion carries int/bool/string-bank string conversion sub-ops.
	tier1SafeStringConversion = []isa.SubOpcode{
		isa.SubOpStrconvFormatBool, isa.SubOpStrconvFormatInt, isa.SubOpStrconvItoa,
	}

	// tier1SafeComplexOps carries complex/float-bank complex-number sub-ops.
	tier1SafeComplexOps = []isa.SubOpcode{
		isa.SubOpRealComplex, isa.SubOpImagComplex, isa.SubOpNegComplex, isa.SubOpMoveComplex,
	}

	// tier1SafeTypedSliceMakes carries typed-slice constructors that write to the
	// typed-slice bank from typed int args.
	tier1SafeTypedSliceMakes = []isa.SubOpcode{
		isa.SubOpMakeSliceInt, isa.SubOpMakeSliceFloat, isa.SubOpMakeSliceString,
		isa.SubOpMakeSliceBool, isa.SubOpMakeSliceUint, isa.SubOpMakeSliceByte, isa.SubOpMakeSliceHeap,
	}

	// tier1SafeTypedSliceLengths carries typed-slice length and capacity sub-ops.
	tier1SafeTypedSliceLengths = []isa.SubOpcode{
		isa.SubOpLenSliceIntDirect, isa.SubOpLenSliceFloatDirect, isa.SubOpLenSliceStringDirect,
		isa.SubOpLenSliceBoolDirect, isa.SubOpLenSliceUintDirect, isa.SubOpLenSliceByteDirect,
		isa.SubOpCapSliceIntDirect, isa.SubOpCapSliceFloatDirect, isa.SubOpCapSliceStringDirect,
		isa.SubOpCapSliceBoolDirect, isa.SubOpCapSliceUintDirect, isa.SubOpCapSliceByteDirect,
	}

	// tier1SafeTypedSliceGetSet carries typed-slice get/set sub-ops.
	tier1SafeTypedSliceGetSet = []isa.SubOpcode{
		isa.SubOpSliceGetFloatDirect, isa.SubOpSliceSetFloatDirect,
		isa.SubOpSliceGetStringDirect, isa.SubOpSliceSetStringDirect,
		isa.SubOpSliceGetBoolDirect, isa.SubOpSliceSetBoolDirect,
		isa.SubOpSliceGetUintDirect, isa.SubOpSliceSetUintDirect,
		isa.SubOpSliceGetByteDirect, isa.SubOpSliceSetByteDirect,
	}

	// tier1SafeByteSliceOps carries byte-slice helpers.
	tier1SafeByteSliceOps = []isa.SubOpcode{
		isa.SubOpSliceByteSlice, isa.SubOpSliceByteToString,
	}

	// tier1SafeTypedBankMoves carries typed-bank-only move sub-ops that never touch general
	// registers.
	tier1SafeTypedBankMoves = []isa.SubOpcode{
		isa.SubOpMoveInt, isa.SubOpMoveFloat, isa.SubOpMoveString, isa.SubOpMoveBool, isa.SubOpMoveUint,
	}

	// tier1SafeTypedBankUnary carries typed-bank unary / conversion sub-ops.
	tier1SafeTypedBankUnary = []isa.SubOpcode{
		isa.SubOpNegInt, isa.SubOpNegFloat, isa.SubOpBitNot, isa.SubOpBitNotUint,
		isa.SubOpIntToFloat, isa.SubOpFloatToInt, isa.SubOpIntToUint, isa.SubOpUintToInt,
		isa.SubOpUintToFloat, isa.SubOpFloatToUint,
		isa.SubOpNot, isa.SubOpBoolToInt, isa.SubOpIntToBool,
	}

	// tier1SafeStringMisc carries string length / construction sub-ops in typed banks.
	tier1SafeStringMisc = []isa.SubOpcode{
		isa.SubOpLenString, isa.SubOpRuneToString,
	}

	// tier1SafeStringCaseTrim carries string case / trim sub-ops in typed banks.
	tier1SafeStringCaseTrim = []isa.SubOpcode{
		isa.SubOpStrToUpper, isa.SubOpStrToLower, isa.SubOpStrTrimSpace,
	}

	// tier1SafeConstantLoadsAndJump carries the constant-load and unconditional jump sub-ops
	// (no general-bank effect).
	tier1SafeConstantLoadsAndJump = []isa.SubOpcode{
		isa.SubOpLoadIntConstSmall, isa.SubOpLoadBool, isa.SubOpLoadZero,
		isa.SubOpLoadUintConstSmall, isa.SubOpLoadBoolConst, isa.SubOpJump,
	}

	// tier1SafeGeneralInspection carries sub-ops that read a general register only to
	// inspect it or that fill a register from a global, neither of which moves a pointer
	// anywhere that outlives the frame.
	tier1SafeGeneralInspection = []isa.SubOpcode{
		isa.SubOpEqInterfaceNil, isa.SubOpNeInterfaceNil, isa.SubOpGetGlobalWide,
	}

	// tier1SafeFusedJumpArith carries the int/uint-bank fused jump-arith sub-ops. Listed so
	// the conservative operand taint test does not misread a constant-pool index as a
	// register number.
	tier1SafeFusedJumpArith = []isa.SubOpcode{
		isa.SubOpIncIntJumpLt, isa.SubOpLenStringLtJumpFalse,
		isa.SubOpEqUintConstJumpFalse, isa.SubOpEqStringConstJumpFalse,
		isa.SubOpLtIntJumpFalse, isa.SubOpLeIntJumpFalse,
		isa.SubOpGtIntJumpFalse, isa.SubOpGeIntJumpFalse,
		isa.SubOpEqIntJumpFalse, isa.SubOpNeIntJumpFalse,
		isa.SubOpLeIntConstJumpFalse, isa.SubOpLtIntConstJumpFalse,
		isa.SubOpEqIntConstJumpFalse, isa.SubOpEqIntConstJumpTrue,
		isa.SubOpGeIntConstJumpFalse, isa.SubOpGtIntConstJumpFalse,
	}

	// tier1SafeUintArithConst carries uint-bank const-fused arithmetic sub-ops.
	tier1SafeUintArithConst = []isa.SubOpcode{
		isa.SubOpAddUintConst, isa.SubOpSubUintConst, isa.SubOpBitAndUintConst,
	}

	// tier1SafeStructFieldGet carries the typed-output struct-field get sub-ops. The
	// receiver pointer is dereferenced for the field load (not propagated); the loaded value
	// lands in a typed bank that cannot carry pointer-ness.
	tier1SafeStructFieldGet = []isa.SubOpcode{
		isa.SubOpGetStructFieldInt, isa.SubOpGetStructFieldUint,
		isa.SubOpGetStructFieldFloat, isa.SubOpGetStructFieldBool,
		isa.SubOpGetStructFieldString,
	}

	// tier1SafeStructFieldSet carries the typed-input struct-field set sub-ops. Same logic
	// as the get variants: the pointer receiver is dereferenced, and the typed value cannot
	// carry pointer-ness.
	tier1SafeStructFieldSet = []isa.SubOpcode{
		isa.SubOpSetStructFieldInt, isa.SubOpSetStructFieldUint,
		isa.SubOpSetStructFieldFloat, isa.SubOpSetStructFieldBool,
		isa.SubOpSetStructFieldString,
	}

	// tier1SafeStructFieldIncDec carries in-place increment/decrement sub-ops through a
	// struct receiver (no value flow at all - the field is both read and written in-place).
	tier1SafeStructFieldIncDec = []isa.SubOpcode{
		isa.SubOpIncStructFieldInt, isa.SubOpDecStructFieldInt,
		isa.SubOpIncStructFieldUint, isa.SubOpDecStructFieldUint,
	}

	// tier1SafeAppendTypedValue carries append-typed-value sub-ops. The general slice
	// receiver is dereferenced; the typed value cannot carry pointer-ness.
	tier1SafeAppendTypedValue = []isa.SubOpcode{
		isa.SubOpAppendInt, isa.SubOpAppendString, isa.SubOpAppendFloat,
		isa.SubOpAppendBool, isa.SubOpAppendUint,
	}

	// tier1SafeStarAppendByte carries *general[B] = append(*general[B], uints[C]) sub-ops.
	// The pointer is dereferenced (not escaped) and the appended value comes from the uint
	// bank.
	tier1SafeStarAppendByte = []isa.SubOpcode{
		isa.SubOpStarAppendByteFast, isa.SubOpStarAppendByteSpread,
	}

	// tier1SafeReadOnlyInspection carries len/cap/bytes-to-string sub-ops that read general
	// registers without propagating their pointer value.
	tier1SafeReadOnlyInspection = []isa.SubOpcode{
		isa.SubOpLen, isa.SubOpCap, isa.SubOpBytesToString,
	}

	// tier1SafeCrossBankMoves carries the unbox/box cross-bank moves. Both are read/value
	// operations that cannot let a tainted general pointer reach a heap-anchored slot.
	tier1SafeCrossBankMoves = []isa.SubOpcode{
		isa.SubOpMoveGeneralToInt, isa.SubOpMoveIntToGeneral,
		isa.SubOpMoveGeneralToFloat, isa.SubOpMoveFloatToGeneral,
		isa.SubOpMoveGeneralToString, isa.SubOpMoveStringToGeneral,
	}
)

// opcodePublishesGeneral lists the opcodes that do move a general-bank register somewhere
// outliving the frame, with the sink that makes each one an escape.
var opcodePublishesGeneral = map[int]string{
	isa.FlatIndexOfSub1(isa.SubOpWriteSharedCell): "stores into a heap-anchored cell shared with closures",
}

// opcodeCannotPublishGeneral lists the opcodes that cannot move a general-bank register
// anywhere that outlives the frame. Every other opcode reaching escapesAtUnclassifiedOp()
// without an operand shape is treated as an escape.
var opcodeCannotPublishGeneral = map[int]string{
	isa.FlatIndexOfSub1(isa.SubOpRangeInit):                "reads the collection in C to build the iterator in B; publishes nothing",
	isa.FlatIndexOfSub1(isa.SubOpRangeNext):                "advances the iterator in B; C is a done flag, not a general register",
	isa.FlatIndexOfSub1(isa.SubOpResetSharedCell):          "clears the cell, storing no value into it",
	isa.FlatIndexOfSub2(isa.SubOpTier2SyncClosureUpvalues): "copies upvalue cells into frame registers, which moves values inward",
	isa.FlatIndexOfSub3(isa.SubOpTier3SyncIIFEUpvalues):    "copies the returned callee's upvalue cells into frame registers, which moves values inward",
}

// RunEscapeAnalysisPass computes parameterEscapes for every reachable function and
// arenaSafeAllocPCs for every isa.OpAllocIndirect site whose output is non-escaping.
//
// Takes root (*CompiledFunction) which is the program's top-level compiled function whose
// nested functions are walked.
//
// Returns error when the context is cancelled before completion.
func RunEscapeAnalysisPass(ctx context.Context, root *program.CompiledFunction) error {
	return RunEscapeAnalysisPassWithOptions(ctx, root, PassOptions{ArenaPromotion: true})
}

// PassOptions selects the optional work of RunEscapeAnalysisPassWithOptions().
type PassOptions struct {
	// ArenaPromotion enables the arena annotations (ArenaSafeAllocPCs,
	// FieldStoreArenaSafePCs and the in-place header reuse marks). The parameter-escape
	// fixpoint always runs because later passes read parameterEscapes.
	ArenaPromotion bool
}

// RunEscapeAnalysisPassWithOptions is RunEscapeAnalysisPass() with the arena annotations
// optional.
//
// Takes root (*CompiledFunction) which is the program's top-level compiled function whose
// nested functions are walked.
// Takes options (PassOptions) which selects the annotations; with ArenaPromotion off
// every allocation takes the heap path at run time.
//
// Returns error when the context is cancelled before completion.
func RunEscapeAnalysisPassWithOptions(ctx context.Context, root *program.CompiledFunction, options PassOptions) error {
	if root == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("runEscapeAnalysisPass cancelled: %w", err)
	}
	all := inline.CollectReachableFunctions(root)
	if len(all) == 0 {
		return nil
	}
	initialiseParameterEscapes(all)
	runEscapeFixpoint(all)
	if !options.ArenaPromotion {
		return nil
	}
	for index, compiledFunction := range all {
		if index&cancellationCheckMask == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("runEscapeAnalysisPass cancelled: %w", err)
			}
		}
		annotateArenaSafeAllocs(compiledFunction)
		annotateArenaSafeFieldStores(compiledFunction)
		annotateInPlaceHeaderReuse(compiledFunction)
	}
	return nil
}

// IsSameSlotAppendOp reports whether op is an append opcode whose handler may reuse the
// source header when the source and destination registers coincide.
//
// Takes op (opcode) which is the opcode to classify.
//
// Returns true for the in-place append family and the plain single-element append.
func IsSameSlotAppendOp(op isa.Opcode) bool {
	return op == isa.OpAppendInPlace || op == isa.OpAppendByteFastInPlace || op == isa.OpAppendSpreadInPlace || op == isa.OpAppend || op == isa.OpAppendStructFast
}

// AnalyseGeneralRegisterAliasFree reports whether the general register slot holds the
// only reference to its slice header throughout compiledFunction. Parameter and result
// slots are never alias-free.
//
// Takes compiledFunction (*CompiledFunction) whose body is walked.
// Takes slot (uint8) which is the general register under analysis.
//
// Returns true only when every read of the slot is classifiably non-retaining.
func AnalyseGeneralRegisterAliasFree(compiledFunction *program.CompiledFunction, slot uint8) bool {
	for paramIdx := range compiledFunction.ParameterKinds {
		if compiledFunction.ParameterKinds[paramIdx] == isa.RegisterGeneral && paramRegisterSlot(compiledFunction, paramIdx) == slot {
			return false
		}
	}
	if slices.Contains(generalResultSlots(compiledFunction), slot) {
		return false
	}
	pc := 0
	for pc < len(compiledFunction.Body) {
		if !aliasFreeAtInstruction(compiledFunction, compiledFunction.Body[pc], slot) {
			return false
		}
		pc = advancePastExtensions(compiledFunction, pc)
	}
	return true
}

// initialiseParameterEscapes seeds each function's parameterEscapes slice with the
// optimistic "no parameter escapes" baseline so the fixpoint can monotonically demote
// individual parameters to escaping.
//
// Takes functions ([]*CompiledFunction) which are the reachable compiled functions to
// initialise.
func initialiseParameterEscapes(functions []*program.CompiledFunction) {
	for _, compiledFunction := range functions {
		if compiledFunction.ParameterEscapes == nil {
			compiledFunction.ParameterEscapes = make([]bool, len(compiledFunction.ParameterKinds))
		}
	}
}

// runEscapeFixpoint runs the bounded monotonic fixpoint that demotes general-bank
// parameters to "escapes" when analyseParameterEscape reports any unmodelled use. Loops
// up to maxEscapeFixpointIter passes.
//
// If the fixpoint does not converge within the cap, forceAllParametersEscaping marks all
// parameters as escaping to prevent unsound arena allocations.
//
// Takes functions ([]*CompiledFunction) which are the reachable compiled functions whose
// parameterEscapes are computed.
func runEscapeFixpoint(functions []*program.CompiledFunction) {
	for range maxEscapeFixpointIter {
		if !classifyEscapingParameters(functions) {
			return
		}
	}
	forceAllParametersEscaping(functions)
}

// forceAllParametersEscaping marks every general-bank parameter of every function as
// escaping.
//
// Takes functions ([]*CompiledFunction) whose general-bank parameters are forced to
// escaping.
func forceAllParametersEscaping(functions []*program.CompiledFunction) {
	for _, compiledFunction := range functions {
		for paramIdx := range compiledFunction.ParameterKinds {
			if compiledFunction.ParameterKinds[paramIdx] != isa.RegisterGeneral {
				continue
			}
			compiledFunction.ParameterEscapes[paramIdx] = true
		}
	}
}

// classifyEscapingParameters performs a single fixpoint pass over every general-bank
// parameter of every function.
//
// Takes functions ([]*CompiledFunction) whose parameters are re-classified.
//
// Returns true when at least one parameter flipped from non-escaping to escaping in this
// pass.
func classifyEscapingParameters(functions []*program.CompiledFunction) bool {
	changed := false
	for _, compiledFunction := range functions {
		for paramIdx := range compiledFunction.ParameterKinds {
			if compiledFunction.ParameterEscapes[paramIdx] {
				continue
			}
			if compiledFunction.ParameterKinds[paramIdx] != isa.RegisterGeneral {
				continue
			}
			if analyseParameterEscape(compiledFunction, paramIdx) {
				compiledFunction.ParameterEscapes[paramIdx] = true
				changed = true
			}
		}
	}
	return changed
}

// paramRegisterSlot returns the register-bank slot index for the paramIdx-th parameter of
// compiledFunction.
//
// Takes compiledFunction (*CompiledFunction) whose parameter layout is queried.
// Takes paramIdx (int) which is the parameter position in
// compiledFunction.parameterKinds.
//
// Returns the register slot within compiledFunction.parameterKinds[paramIdx]'s bank.
func paramRegisterSlot(compiledFunction *program.CompiledFunction, paramIdx int) uint8 {
	kind := compiledFunction.ParameterKinds[paramIdx]
	slot := uint8(0)
	for j := range paramIdx {
		if compiledFunction.ParameterKinds[j] == kind {
			slot++
		}
	}
	return slot
}

// generalResultSlots returns the register slots in the general bank that
// isa.SubOpTier2Return reads on return.
//
// Takes compiledFunction (*CompiledFunction) whose result kinds drive the slot count.
//
// Returns a slice (length 0 when no general-bank returns) of the register slot indices
// that constitute the return-value reads.
func generalResultSlots(compiledFunction *program.CompiledFunction) []uint8 {
	count := uint8(0)
	for _, kind := range compiledFunction.ResultKinds {
		if kind == isa.RegisterGeneral {
			count++
		}
	}
	if count == 0 {
		return nil
	}
	slots := make([]uint8, count)
	for i := range slots {
		slots[i] = uint8(i)
	}
	return slots
}

// analyseParameterEscape returns true when the paramIdx-th parameter (which must be in
// the isa.RegisterGeneral bank) escapes compiledFunction's frame.
//
// Takes compiledFunction (*CompiledFunction) whose body is walked.
// Takes paramIdx (int) which is the index in compiledFunction.parameterKinds.
//
// Returns true when paramIdx's pointer escapes; false when every use is safe.
func analyseParameterEscape(compiledFunction *program.CompiledFunction, paramIdx int) bool {
	rootSlot := paramRegisterSlot(compiledFunction, paramIdx)
	return analyseGeneralRegisterEscape(compiledFunction, rootSlot, 0)
}

// analyseGeneralRegisterEscape reports whether the value in rootSlot can escape the frame
// through any instruction from startPC to the end of the body.
//
// Takes compiledFunction (*CompiledFunction) whose body is walked.
// Takes rootSlot (uint8) which is the general register holding the value.
// Takes startPC (int) which is the first instruction inspected.
//
// Returns true when any use may escape.
func analyseGeneralRegisterEscape(compiledFunction *program.CompiledFunction, rootSlot uint8, startPC int) bool {
	return analyseGeneralRegisterEscapeRange(compiledFunction, rootSlot, startPC, len(compiledFunction.Body))
}

// analyseGeneralRegisterEscapeRange reports whether the value in rootSlot can escape the
// frame through any instruction in [startPC, endPC).
//
// Takes compiledFunction (*CompiledFunction) whose body is walked.
// Takes rootSlot (uint8) which is the general register holding the value.
// Takes startPC (int) which is the first instruction inspected.
// Takes endPC (int) which is the instruction after the last one inspected.
//
// Returns true when any use may escape.
func analyseGeneralRegisterEscapeRange(compiledFunction *program.CompiledFunction, rootSlot uint8, startPC, endPC int) bool {
	seed := [isa.GeneralRegisterBankSize]bool{}
	seed[rootSlot] = true
	escapes, _ := analyseGeneralRegisterEscapeSeeded(compiledFunction, &seed, startPC, endPC)
	return escapes
}

// analyseGeneralRegisterEscapeSeeded walks [startPC, endPC) in program order with the
// registers in seed holding the value and reports whether it can escape.
//
// Takes compiledFunction (*CompiledFunction) whose body is walked.
// Takes seed (*[isa.GeneralRegisterBankSize]bool) which marks the registers holding the
// value on entry; it is read, never written.
// Takes startPC (int) which is the first instruction inspected.
// Takes endPC (int) which is the instruction after the last one inspected.
//
// Returns escapes (bool) which is true when any use may escape.
// Returns ever ([isa.GeneralRegisterBankSize]bool) which marks every register that held
// the value at some point of the walk, seed included.
func analyseGeneralRegisterEscapeSeeded(
	compiledFunction *program.CompiledFunction, seed *[isa.GeneralRegisterBankSize]bool, startPC, endPC int,
) (escapes bool, ever [isa.GeneralRegisterBankSize]bool) {
	walk := escapeWalk{
		compiledFunction: compiledFunction,
		jumpTargets:      program.JumpTargetsOf(compiledFunction.Body),
		tainted:          *seed,
		ever:             *seed,
	}
	for pc := startPC; pc < endPC && pc < len(compiledFunction.Body); pc = advancePastExtensions(compiledFunction, pc) {
		if walk.jumpTargets[pc] {
			walk.tainted = walk.ever
		}
		if walk.step(pc) {
			return true, walk.ever
		}
	}
	return false, walk.ever
}

// escapeWalk is the taint state of one linear escape scan: tainted is the set of
// registers holding the value at the current instruction, ever the set that has held it
// anywhere in the scan so far, which a jump target restores.
type escapeWalk struct {
	// compiledFunction is the function being scanned.
	compiledFunction *program.CompiledFunction

	// jumpTargets marks every PC that is the target of a branch.
	jumpTargets map[int]bool

	// tainted tracks the registers holding the value at the current instruction.
	tainted [isa.GeneralRegisterBankSize]bool

	// ever tracks every register that has held the value at any point during the scan.
	ever [isa.GeneralRegisterBankSize]bool
}

// step classifies the instruction at pc: copies move the taint, every other instruction
// is checked for an escape with the current taint and then kills the registers it
// definitely overwrites.
//
// Takes pc (int) which is the instruction's program counter.
//
// Returns true when the instruction may escape the value.
func (w *escapeWalk) step(pc int) bool {
	inst := w.compiledFunction.Body[pc]
	if isa.InstrIsTier1SubOp(inst, isa.SubOpRangeInit) {
		if w.tainted[inst.C] {
			w.hold(inst.B)
		}
		return false
	}
	if inst.Op == isa.OpMoveGeneral {
		w.tainted[inst.A] = w.tainted[inst.B]
		if w.tainted[inst.B] {
			w.hold(inst.A)
		}
		return false
	}
	before := w.tainted
	if escapesAtInstruction(w.compiledFunction, pc, &w.tainted) {
		return true
	}
	for _, register := range definiteGeneralWrites(w.compiledFunction, inst) {
		if w.tainted[register] && !before[register] {
			continue
		}
		w.tainted[register] = false
	}
	for register, holds := range w.tainted {
		if holds {
			w.ever[register] = true
		}
	}
	return false
}

// hold marks register as holding the value now and as having held it.
//
// Takes register (uint8) which is the general register.
func (w *escapeWalk) hold(register uint8) {
	w.tainted[register] = true
	w.ever[register] = true
}

// definiteGeneralWrites lists the general registers inst overwrites unconditionally: a
// call's plain general results, read through its call site, or the general-role operands
// the shape table marks as written for a described operation without opaque writes.
// Anything the table cannot pin down writes nothing here, which only keeps registers
// tainted longer.
//
// Takes compiledFunction (*CompiledFunction) which owns the call-site table.
// Takes inst (isa.Instruction) which is the instruction.
//
// Returns the overwritten general registers; nil when there are none.
func definiteGeneralWrites(compiledFunction *program.CompiledFunction, inst isa.Instruction) []uint8 {
	if isCallSiteInstruction(inst) {
		siteIndex := int(inst.WideIndex())
		if siteIndex >= len(compiledFunction.CallSites) {
			return nil
		}
		var written []uint8
		for _, result := range compiledFunction.CallSites[siteIndex].Returns {
			if result.Kind != isa.RegisterGeneral || result.IsSpilled || result.IsIndirect || result.IsUpvalue {
				continue
			}
			written = append(written, result.Register)
		}
		return written
	}
	shape := isa.ShapeForInstruction(inst)
	if shape.Flags&isa.ShapeFlagDescribed == 0 || shape.Flags&isa.ShapeFlagOpaqueWrites != 0 {
		return nil
	}
	roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
	operands := [isa.NumInstructionOperands]uint8{inst.A, inst.B, inst.C}
	var written []uint8
	for position, role := range roles {
		if shape.Writes[position] && role == isa.RoleRegGeneral {
			written = append(written, operands[position])
		}
	}
	return written
}

// advancePastExtensions advances pc past the current instruction and any following
// isa.OpExt extension words.
//
// Takes compiledFunction (*CompiledFunction) whose body holds the instruction sequence.
// Takes pc (int) which is the current instruction position.
//
// Returns pc+1 at minimum, advanced further past trailing isa.OpExt words.
func advancePastExtensions(compiledFunction *program.CompiledFunction, pc int) int {
	pc++
	for pc < len(compiledFunction.Body) && compiledFunction.Body[pc].Op == isa.OpExt {
		pc++
	}
	return pc
}

// escapesAtInstruction reports whether any tainted register is used in an escape-relevant
// context at pc.
//
// Takes compiledFunction (*CompiledFunction) whose body is walked.
// Takes pc (int) which is the position of the current bytecode instruction.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which is the alias-closure derived
// from the parameter.
//
// Returns true on any escape; false when the instruction's effect is classifiably safe
// for the tainted set.
func escapesAtInstruction(compiledFunction *program.CompiledFunction, pc int, tainted *[isa.GeneralRegisterBankSize]bool) bool {
	inst := compiledFunction.Body[pc]
	switch inst.Op {
	case isa.OpAllocIndirect, isa.OpAddr:
		if tainted[inst.B] && isArenaCandidateSite(inst) && compiledFunction.ArenaSafeAllocPCs[pc] {
			tainted[inst.A] = true
			return false
		}
		return tainted[inst.B]
	case isa.OpGetField, isa.OpDeref, isa.OpMoveGeneral:
		return false
	case isa.OpSetField:
		return tainted[inst.C]
	case isa.OpMakeClosure:
		return makeClosureCaptures(compiledFunction, inst, tainted)
	case isa.OpDrillTier1:
		return tier1OrDrilledEscapes(compiledFunction, pc, inst, tainted)
	case isa.OpGo, isa.OpDefer:
		return spawnedArgumentsEscape(compiledFunction, pc, tainted)
	default:
	}
	if escapes, classified := structFieldOpEscapes(inst, tainted); classified {
		return escapes
	}
	return escapesAtUnclassifiedOp(inst, tainted)
}

// escapesAtUnclassifiedOp handles every opcode not covered by the explicit switch in
// escapesAtInstruction. When the operand-shape table describes which bytes are
// general-bank reads those bytes are checked directly.
//
// Takes inst (instruction) which is the current instruction.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which holds the alias closure.
//
// Returns true on any conservatively-detected escape; false when no operand byte reads a
// tainted slot.
func escapesAtUnclassifiedOp(inst isa.Instruction, tainted *[isa.GeneralRegisterBankSize]bool) bool {
	if _, cannotPublish := opcodeCannotPublishGeneral[isa.FlatIndexFor(inst)]; cannotPublish {
		return false
	}
	if _, publishes := opcodePublishesGeneral[isa.FlatIndexFor(inst)]; publishes {
		return anyGeneralTainted(inst, tainted)
	}
	shape := isa.ShapeForInstruction(inst)
	if shape.Flags&isa.ShapeFlagDescribed == 0 {
		return anyGeneralTainted(inst, tainted)
	}
	return readsTaintedGeneralOperand(inst, shape, tainted)
}

// readsTaintedGeneralOperand reports whether any operand the shape marks as read names a
// general-bank register that is tainted.
//
// Takes inst (instruction) which is the instruction under inspection.
// Takes shape (isa.OperandShape) which is the shape of the operation it executes.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which holds the alias closure.
//
// Returns true when a tainted general register is read.
func readsTaintedGeneralOperand(inst isa.Instruction, shape isa.OperandShape, tainted *[isa.GeneralRegisterBankSize]bool) bool {
	roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
	operands := [isa.NumInstructionOperands]uint8{inst.A, inst.B, inst.C}
	for position, role := range roles {
		if shape.Reads[position] && operandNamesGeneral(inst, shape, role) && tainted[operands[position]] {
			return true
		}
	}
	return false
}

// operandNamesGeneral reports whether an operand with the given role addresses the
// general bank: a general-bank role does, a dynamic role does when the kind-marker
// sibling names the general bank or is absent, and every other role does not.
//
// Takes inst (instruction) which carries the kind-marker byte when the shape has one.
// Takes shape (isa.OperandShape) which is the instruction's operand shape.
// Takes role (isa.OperandRole) which is the operand's role.
//
// Returns true when the operand may name a general-bank register.
func operandNamesGeneral(inst isa.Instruction, shape isa.OperandShape, role isa.OperandRole) bool {
	switch role {
	case isa.RoleRegGeneral:
		return true
	case isa.RoleRegDynamic:
		roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
		operands := [isa.NumInstructionOperands]uint8{inst.A, inst.B, inst.C}
		for position, sibling := range roles {
			if sibling == isa.RoleKindMarker {
				return isa.RegisterKind(operands[position]) == isa.RegisterGeneral
			}
		}
		return true
	default:
		return false
	}
}

// spawnedArgumentsEscape returns true when the isa.OpGo or isa.OpDefer at pc publishes a
// tainted general-bank register to the spawned goroutine or the deferred call.
//
// Takes compiledFunction (*CompiledFunction) whose body holds the instruction and its
// extension words.
// Takes pc (int) which is the position of the isa.OpGo or isa.OpDefer instruction.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which holds the alias closure.
//
// Returns true when the callee value or any general-bank argument is tainted.
func spawnedArgumentsEscape(compiledFunction *program.CompiledFunction, pc int, tainted *[isa.GeneralRegisterBankSize]bool) bool {
	if tainted[compiledFunction.Body[pc].A] {
		return true
	}
	for ext := pc + 1; ext < len(compiledFunction.Body) && compiledFunction.Body[ext].Op == isa.OpExt; ext++ {
		word := compiledFunction.Body[ext]
		if isa.RegisterKind(word.C) == isa.RegisterGeneral && tainted[word.B] {
			return true
		}
	}
	return false
}

// selectSendsEscape returns true when the isa.SubOpSelect at pc sends a tainted
// general-bank register to another goroutine.
//
// Takes compiledFunction (*CompiledFunction) whose body holds the instruction and its
// extension words.
// Takes pc (int) which is the position of the isa.SubOpSelect instruction.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which holds the alias closure.
//
// Returns true when any send case's value register is a tainted general-bank slot.
func selectSendsEscape(compiledFunction *program.CompiledFunction, pc int, tainted *[isa.GeneralRegisterBankSize]bool) bool {
	ext := pc + 1
	for caseIndex := 0; caseIndex < int(compiledFunction.Body[pc].B); caseIndex++ {
		if ext >= len(compiledFunction.Body) || compiledFunction.Body[ext].Op != isa.OpExt {
			return true
		}
		if compiledFunction.Body[ext].A == isa.SelectDirectionDefault {
			ext++
			continue
		}
		if ext+1 >= len(compiledFunction.Body) || compiledFunction.Body[ext+1].Op != isa.OpExt {
			return true
		}
		payload := compiledFunction.Body[ext+1]
		if compiledFunction.Body[ext].A == isa.SelectDirectionSend &&
			isa.RegisterKind(payload.B) == isa.RegisterGeneral && tainted[payload.A] {
			return true
		}
		ext += 2
	}
	return false
}

// makeClosureCaptures returns true when the isa.OpMakeClosure at inst captures any
// tainted general-bank register as a local upvalue.
//
// Takes compiledFunction (*CompiledFunction) whose nested functions list is consulted.
// Takes inst (instruction) which is the isa.OpMakeClosure instruction.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which holds the alias closure.
//
// Returns true when any local general-bank capture is tainted.
func makeClosureCaptures(compiledFunction *program.CompiledFunction, inst isa.Instruction, tainted *[isa.GeneralRegisterBankSize]bool) bool {
	functionIndex := int(inst.WideIndex())
	if functionIndex >= len(compiledFunction.Functions) {
		return true
	}
	nested := compiledFunction.Functions[functionIndex]
	if nested == nil {
		return true
	}
	for _, uv := range nested.UpvalueDescriptors {
		if !uv.IsLocal {
			continue
		}
		if uv.Kind != isa.RegisterGeneral {
			continue
		}
		if tainted[uv.Index] {
			return true
		}
	}
	return false
}

// drilledSubOpEscapes returns true when the tier-1/2/3 drilled sub-opcode at inst uses
// any tainted register in an escape sink.
//
// Resolves the sub-op tier by walking the drill chain and delegates to tier1SubOpEscapes,
// tier2SubOpEscapes or tier3SubOpEscapes.
//
// Takes compiledFunction (*CompiledFunction) whose result kinds drive return-slot
// detection.
// Takes inst (instruction) which is the isa.OpDrillTier1 wrapper.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which holds the alias closure.
//
// Returns true on any sub-op escape; false when the resolved sub-op is classifiably safe
// for the tainted set.
func drilledSubOpEscapes(compiledFunction *program.CompiledFunction, inst isa.Instruction, tainted *[isa.GeneralRegisterBankSize]bool) bool {
	if isa.SubOpcode(inst.A) != isa.SubOpDrillTier2 {
		return tier1SubOpEscapes(isa.SubOpcode(inst.A), inst, tainted)
	}
	tier2 := isa.SubOpcodeTier2(inst.B)
	if tier2 != isa.SubOpTier2DrillTier3 {
		return tier2SubOpEscapes(compiledFunction, tier2, inst, tainted)
	}
	tier3 := isa.SubOpcodeTier3(inst.C)
	return tier3SubOpEscapes(compiledFunction, tier3, inst, tainted)
}

// tier1SubOpEscapes classifies a tier-1 sub-op.
//
// Takes sub (isa.SubOpcode) which is the tier-1 sub-opcode from inst.a.
// Takes inst (instruction) which is the isa.OpDrillTier1 wrapper.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which holds the alias closure.
//
// Returns true on any escape; false for sub-ops in the safe allowlist and for unknown
// sub-ops where no operand byte matches a tainted slot.
func tier1SubOpEscapes(sub isa.SubOpcode, inst isa.Instruction, tainted *[isa.GeneralRegisterBankSize]bool) bool {
	if _, cannotPublish := opcodeCannotPublishGeneral[isa.FlatIndexOfSub1(sub)]; cannotPublish {
		return false
	}
	switch sub {
	case isa.SubOpWriteSharedCell, isa.SubOpSetGlobalWide:

		return isa.RegisterKind(inst.C) == isa.RegisterGeneral && tainted[inst.B]
	default:
	}
	if _, publishes := opcodePublishesGeneral[isa.FlatIndexOfSub1(sub)]; publishes {
		return anyGeneralTainted(inst, tainted)
	}
	if tier1SubOpSafe(sub) {
		return false
	}

	shape := isa.ShapeForInstruction(inst)
	if shape.Flags&isa.ShapeFlagDescribed == 0 {
		return tainted[inst.B] || tainted[inst.C]
	}
	return readsTaintedGeneralOperand(inst, shape, tainted)
}

// tier1SubOpSafe reports whether the sub-op's effect on a tainted general register is
// non-escaping.
//
// Takes sub (isa.SubOpcode) which is the tier-1 sub-opcode to classify.
//
// Returns true when the sub-op is in the verified-safe allowlist.
func tier1SubOpSafe(sub isa.SubOpcode) bool {
	return tier1SafeSubOpTable[sub]
}

// tier2SubOpEscapes classifies a tier-2 sub-op, returning true when the operation may let
// a tainted general-bank value escape.
//
// Takes compiledFunction (*CompiledFunction) whose result kinds drive return-slot reads.
// Takes sub (isa.SubOpcodeTier2) which is the tier-2 sub-opcode from inst.b.
// Takes inst (instruction) which is the isa.OpDrillTier1 wrapper.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which holds the alias closure.
//
// Returns true on any escape; false for safe tier-2 sub-ops.
func tier2SubOpEscapes(compiledFunction *program.CompiledFunction, sub isa.SubOpcodeTier2, inst isa.Instruction, tainted *[isa.GeneralRegisterBankSize]bool) bool {
	if sub == isa.SubOpTier2Return {
		for _, slot := range generalResultSlots(compiledFunction) {
			if tainted[slot] {
				return true
			}
		}
		return false
	}

	if _, cannotPublish := opcodeCannotPublishGeneral[isa.FlatIndexOfSub2(sub)]; cannotPublish {
		return false
	}
	if _, publishes := opcodePublishesGeneral[isa.FlatIndexOfSub2(sub)]; publishes {
		return tainted[inst.C]
	}
	if tier2SubOpSafe(sub) {
		return false
	}

	shape := isa.ShapeForInstruction(inst)
	if shape.Flags&isa.ShapeFlagDescribed == 0 {
		return tainted[inst.C]
	}
	return readsTaintedGeneralOperand(inst, shape, tainted)
}

// tier3SubOpEscapes classifies a tier-3 sub-op. Tier-3 holds zero-operand ops
// (isa.SubOpTier3Nop, isa.SubOpTier3ReturnVoid, isa.SubOpTier3SyncIIFEUpvalues) that read
// no registers, so no tainted value can escape through them; the upvalue sync only moves
// values inward, as its opcodeCannotPublishGeneral entry records.
//
// Returns false unconditionally; tier-3 sub-ops have no general-bank effect.
func tier3SubOpEscapes(_ *program.CompiledFunction, _ isa.SubOpcodeTier3, _ isa.Instruction, _ *[isa.GeneralRegisterBankSize]bool) bool {
	return false
}

// callArgsEscape reports whether the isa.SubOpCall at inst passes any tainted register to
// a callee whose corresponding parameter escapes.
//
// Resolves the call site via compiledFunction.callSites and uses the callee's
// parameterEscapes (set by a prior fixpoint pass) to decide each argument. When the
// callee is unknown (closure call, native call, dynamic dispatch) the analysis
// conservatively treats every tainted argument as escaping.
//
// Takes compiledFunction (*CompiledFunction) whose call sites are consulted.
// Takes inst (instruction) which is the isa.SubOpCall instruction; operands B/C encode
// the wide call-site index.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which is the alias-closure.
//
// Returns true on any escape; false when every tainted argument is bound to a
// non-escaping parameter.
func callArgsEscape(compiledFunction *program.CompiledFunction, inst isa.Instruction, tainted *[isa.GeneralRegisterBankSize]bool) bool {
	siteIdx := int(inst.WideIndex())
	if siteIdx >= len(compiledFunction.CallSites) {
		return true
	}
	site := &compiledFunction.CallSites[siteIdx]
	if callSiteTargetTainted(site, tainted) {
		return true
	}
	if site.CachedCallee == nil {
		return anyTaintedGeneralArg(site.Arguments, tainted)
	}
	return anyEscapingTaintedArg(site, tainted)
}

// callSiteTargetTainted reports whether the value being called is itself a tainted
// general register.
//
// Takes site (*CallSite) which describes the call.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which holds the alias closure.
//
// Returns true when the call target is a tainted general register.
func callSiteTargetTainted(site *program.CallSite, tainted *[isa.GeneralRegisterBankSize]bool) bool {
	if site.IsMethod && tainted[site.MethodReceiverRegister] {
		return true
	}
	if site.IsClosure && tainted[site.ClosureRegister] {
		return true
	}
	return site.IsNative && tainted[site.NativeRegister]
}

// builtinArgumentsEscape reports whether the builtin call at pc hands a tainted
// general-bank register to the builtin.
//
// Takes compiledFunction (*CompiledFunction) whose body holds the extension words.
// Takes pc (int) which is the position of the call instruction.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which holds the alias closure.
//
// Returns true when any general-bank argument is tainted.
func builtinArgumentsEscape(compiledFunction *program.CompiledFunction, pc int, tainted *[isa.GeneralRegisterBankSize]bool) bool {
	body := compiledFunction.Body
	for ext := pc + 1; ext < len(body) && body[ext].Op == isa.OpExt; ext++ {
		word := body[ext]
		if isa.RegisterKind(word.B) == isa.RegisterGeneral && tainted[word.A] {
			return true
		}
	}
	return false
}

// anyTaintedGeneralArg returns true when at least one general-bank argument register is
// in the tainted set. Used when the callee is unknown (closure / native / dynamic
// dispatch) and any passed tainted register is assumed to leak.
//
// Takes arguments ([]VarLocation) which are the call-site arguments.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which holds the alias closure.
//
// Returns true on the first tainted general-bank argument.
func anyTaintedGeneralArg(arguments []program.VarLocation, tainted *[isa.GeneralRegisterBankSize]bool) bool {
	for _, argument := range arguments {
		if argument.Kind == isa.RegisterGeneral && tainted[argument.Register] {
			return true
		}
	}
	return false
}

// anyEscapingTaintedArg returns true when at least one tainted argument is bound to a
// callee parameter classified as escaping. Variadic and shape mismatches conservatively
// count as escaping.
//
// Takes site (*CallSite) which carries the call-site arguments and the cached callee.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which holds the alias closure.
//
// Returns true on the first tainted argument bound to an escaping parameter, or to an
// unmatched parameter.
func anyEscapingTaintedArg(site *program.CallSite, tainted *[isa.GeneralRegisterBankSize]bool) bool {
	callee := site.CachedCallee
	for argumentIndex, argument := range site.Arguments {
		if argument.Kind != isa.RegisterGeneral || !tainted[argument.Register] {
			continue
		}
		if argumentIndex >= len(callee.ParameterEscapes) {
			return true
		}
		if callee.ParameterEscapes[argumentIndex] {
			return true
		}
	}
	return false
}

// anyGeneralTainted is a fallback used when no per-operand shape info is available and
// the bare a/b/c bytes are tested against the tainted set.
//
// Takes inst (instruction) whose operand bytes are checked.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which holds the alias closure.
//
// Returns true when any of a/b/c is tainted.
func anyGeneralTainted(inst isa.Instruction, tainted *[isa.GeneralRegisterBankSize]bool) bool {
	return tainted[inst.A] || tainted[inst.B] || tainted[inst.C]
}

// annotateArenaSafeAllocs scans compiledFunction.body for isa.OpAllocIndirect and
// address-taking isa.OpAddr sites whose output pointer cannot escape compiledFunction's
// frame and records each in compiledFunction.ArenaSafeAllocPCs, so the handlers may place
// the pointee in the arena.
//
// Takes compiledFunction (*CompiledFunction) whose body is scanned.
func annotateArenaSafeAllocs(compiledFunction *program.CompiledFunction) {
	annotateArenaSafeSites(compiledFunction, false)
	annotateArenaSafeSites(compiledFunction, true)
}

// annotateArenaSafeSites runs one phase of annotateArenaSafeAllocs() over the sites of
// one family, in program-counter order.
//
// Takes compiledFunction (*CompiledFunction) whose ArenaSafeAllocPCs receives the
// verdicts.
// Takes structLiterals (bool) which selects the tier-2 struct-literal sites; false
// selects the pointer-producing tier-0 sites.
func annotateArenaSafeSites(compiledFunction *program.CompiledFunction, structLiterals bool) {
	pc := 0
	for pc < len(compiledFunction.Body) {
		afterSite := advancePastExtensions(compiledFunction, pc)
		outputSlot, ok := arenaCandidateSiteAt(compiledFunction, pc)
		if !ok || isStructLiteralSiteAt(compiledFunction, pc) != structLiterals {
			pc = afterSite
			continue
		}
		if !outputEscapesFromSite(compiledFunction, outputSlot, pc, afterSite) {
			if compiledFunction.ArenaSafeAllocPCs == nil {
				compiledFunction.ArenaSafeAllocPCs = make(map[int]bool)
			}
			compiledFunction.ArenaSafeAllocPCs[pc] = true
		}
		pc = afterSite
	}
}

// annotateInPlaceHeaderReuse scans compiledFunction.body for append sites that write
// their result back into the source register and whose register is alias-free within the
// frame, recording each in compiledFunction.InPlaceHeaderReusePCs.
//
// Takes compiledFunction (*CompiledFunction) whose body is scanned.
func annotateInPlaceHeaderReuse(compiledFunction *program.CompiledFunction) {
	pc := 0
	for pc < len(compiledFunction.Body) {
		inst := compiledFunction.Body[pc]
		if IsSameSlotAppendOp(inst.Op) && inst.A == inst.B && AnalyseGeneralRegisterAliasFree(compiledFunction, inst.A) {
			if compiledFunction.InPlaceHeaderReusePCs == nil {
				compiledFunction.InPlaceHeaderReusePCs = make(map[int]bool)
			}
			compiledFunction.InPlaceHeaderReusePCs[pc] = true
		}
		pc = advancePastExtensions(compiledFunction, pc)
	}
}

// aliasFreeAtInstruction classifies a single instruction's effect on the slot's header.
//
// Takes compiledFunction (*CompiledFunction) which supplies the call sites a call
// instruction passes its arguments through.
// Takes inst (instruction) which is the instruction to classify.
// Takes slot (uint8) which is the general register under analysis.
//
// Returns false when the instruction may copy the slot's reflect.Value into another
// register or location, true when it leaves the header unshared.
func aliasFreeAtInstruction(compiledFunction *program.CompiledFunction, inst isa.Instruction, slot uint8) bool {
	if aliasFree, isCall := callAliasFreeForSlot(compiledFunction, inst, slot); isCall {
		return aliasFree
	}
	switch inst.Op {
	case isa.OpMoveGeneral:
		return inst.B != slot
	case isa.OpAppendInPlace, isa.OpAppendByteFastInPlace, isa.OpAppendSpreadInPlace, isa.OpAppend, isa.OpAppendByteFast, isa.OpAppendSpread,
		isa.OpAppendStructFast, isa.OpAppendIntFast, isa.OpAppendFloatFast, isa.OpAppendStringFast:
		return inst.B != slot || inst.A == slot
	case isa.OpIndex, isa.OpSliceIndexStructFieldInt, isa.OpSliceIndexStructFieldUint,
		isa.OpSliceIndexStructFieldFloat, isa.OpSliceIndexStructFieldBool, isa.OpSliceIndexStructFieldString:
		return true
	case isa.OpIndexSet:
		return inst.C != slot
	default:
	}
	if tier1InspectsWithoutCopying(inst) {
		return true
	}
	shape := isa.ShapeForInstruction(inst)
	if shape.Flags&isa.ShapeFlagDescribed == 0 {
		if inst.Op == isa.OpDrillTier1 {
			return inst.B != slot && inst.C != slot
		}
		return inst.A != slot && inst.B != slot && inst.C != slot
	}
	return !shapeReadsGeneralSlot(inst, shape, slot)
}

// shapeReadsGeneralSlot reports whether a described instruction reads general[slot]
// through an operand that could copy it: every general-bank read counts except the
// receiver of a struct-field access, which is dereferenced in place.
//
// Takes inst (instruction) which is the instruction.
// Takes shape (isa.OperandShape) which is its operand shape.
// Takes slot (uint8) which is the general register.
//
// Returns true when a copying read of the slot may occur.
func shapeReadsGeneralSlot(inst isa.Instruction, shape isa.OperandShape, slot uint8) bool {
	roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
	operands := [isa.NumInstructionOperands]uint8{inst.A, inst.B, inst.C}
	receiverPosition, hasReceiver := structFieldReceiverOperand(inst.Op)
	for position, role := range roles {
		if hasReceiver && position == receiverPosition {
			continue
		}
		if shape.Reads[position] && operandNamesGeneral(inst, shape, role) && operands[position] == slot {
			return true
		}
	}
	return false
}

// structFieldReceiverOperand returns the operand position that names the receiver of a
// struct-field read or write.
//
// Takes op (isa.Opcode) which is the instruction's opcode.
//
// Returns the receiver's operand position and true, or false for any other opcode.
func structFieldReceiverOperand(op isa.Opcode) (int, bool) {
	switch op {
	case isa.OpSetStructFieldIntT0, isa.OpSetStructFieldUint, isa.OpSetStructFieldFloat,
		isa.OpSetStructFieldBool, isa.OpSetStructFieldGeneral, isa.OpSetField, isa.OpSetFieldInt:
		return 0, true
	case isa.OpGetStructFieldIntT0, isa.OpGetStructFieldUint, isa.OpGetStructFieldFloat,
		isa.OpGetStructFieldBool, isa.OpGetStructFieldGeneral, isa.OpGetField, isa.OpGetFieldInt:
		return 1, true
	default:
		return 0, false
	}
}

// tier1InspectsWithoutCopying reports whether inst is a tier-1 operation that reads a
// general register only to inspect it, producing a scalar and never another handle to the
// same storage.
//
// Takes inst (instruction) which is the instruction to classify.
//
// Returns true for the length, capacity and nil-test sub-ops.
func tier1InspectsWithoutCopying(inst isa.Instruction) bool {
	if inst.Op != isa.OpDrillTier1 {
		return false
	}
	switch isa.SubOpcode(inst.A) {
	case isa.SubOpLen, isa.SubOpCap, isa.SubOpEqInterfaceNil, isa.SubOpNeInterfaceNil:
		return true
	default:
		return false
	}
}

// callSitePassesGeneralSlot reports whether the call at inst hands the general register
// slot to its callee.
//
// Takes compiledFunction (*CompiledFunction) which owns the call-site table.
// Takes inst (instruction) which is the call instruction.
// Takes slot (uint8) which is the general register under analysis.
//
// Returns true when the slot is a general-bank argument of the call site, and true when
// the site cannot be resolved so an unreadable site never reads as safe.
func callSitePassesGeneralSlot(compiledFunction *program.CompiledFunction, inst isa.Instruction, slot uint8) bool {
	siteIndex := int(inst.WideIndex())
	if siteIndex >= len(compiledFunction.CallSites) {
		return true
	}
	for _, argument := range compiledFunction.CallSites[siteIndex].Arguments {
		if argument.Kind == isa.RegisterGeneral && argument.Register == slot {
			return true
		}
	}
	return false
}

// callAliasFreeForSlot classifies a call instruction's effect on the slot's header.
//
// Takes compiledFunction (*CompiledFunction) which owns the call-site table.
// Takes inst (instruction) which is the instruction to classify.
// Takes slot (uint8) which is the general register under analysis.
//
// Returns aliasFree (bool) which is true when the call leaves the header unshared.
// Returns isCall (bool) which is false when inst is not a call, leaving the caller to
// classify it.
func callAliasFreeForSlot(compiledFunction *program.CompiledFunction, inst isa.Instruction, slot uint8) (aliasFree, isCall bool) {
	if inst.Op == isa.OpDrillTier1 {
		switch isa.SubOpcode(inst.A) {
		case isa.SubOpCall, isa.SubOpCallScalar, isa.SubOpTailCall, isa.SubOpCallMethod,
			isa.SubOpCallMethodInlineable, isa.SubOpCallNative, isa.SubOpCallIIFE:
			return !callSitePassesGeneralSlot(compiledFunction, inst, slot), true
		case isa.SubOpCallBuiltin:
			return false, true
		case isa.SubOpSliceOp:

			return true, true
		default:
			return false, false
		}
	}
	switch inst.Op {
	case isa.OpDefer, isa.OpGo:

		return false, true
	default:
		return false, false
	}
}

// tier2SubOpSafe reports whether a tier-2 sub-op cannot publish a general-bank pointer.
//
// Takes sub (isa.SubOpcodeTier2) which is the tier-2 sub-opcode.
//
// Returns true when the sub-op cannot move a general-bank pointer out of the frame.
func tier2SubOpSafe(sub isa.SubOpcodeTier2) bool {
	return sub == isa.SubOpTier2RangeCheckUintJumpFalse
}

// tier1OrDrilledEscapes classifies a tier-1 instruction, routing the operations whose
// effect its operand bytes cannot express to a reader that looks where the operands
// actually live.
//
// Takes compiledFunction (*CompiledFunction) which owns the call sites and body.
// Takes pc (int) which is the instruction's program counter.
// Takes inst (instruction) which is the isa.OpDrillTier1 instruction.
// Takes tainted (*[isa.GeneralRegisterBankSize]bool) which holds the alias closure.
//
// Returns true on any escape.
func tier1OrDrilledEscapes(compiledFunction *program.CompiledFunction, pc int, inst isa.Instruction, tainted *[isa.GeneralRegisterBankSize]bool) bool {
	switch isa.SubOpcode(inst.A) {
	case isa.SubOpCall, isa.SubOpCallScalar, isa.SubOpTailCall, isa.SubOpCallMethod,
		isa.SubOpCallMethodInlineable, isa.SubOpCallNative, isa.SubOpCallIIFE:
		return callArgsEscape(compiledFunction, inst, tainted)
	case isa.SubOpCallBuiltin:
		return builtinArgumentsEscape(compiledFunction, pc, tainted)
	case isa.SubOpSelect:
		return selectSendsEscape(compiledFunction, pc, tainted)
	default:
	}
	return drilledSubOpEscapes(compiledFunction, inst, tainted)
}

// isCallSiteInstruction reports whether inst is a tier-1 call that names its registers
// through a call site rather than its operand bytes.
//
// Takes inst (isa.Instruction) which is the instruction.
//
// Returns true for the call, scalar call, tail call, method call, native call and IIFE
// call sub-ops.
func isCallSiteInstruction(inst isa.Instruction) bool {
	if inst.Op != isa.OpDrillTier1 {
		return false
	}
	switch isa.SubOpcode(inst.A) {
	case isa.SubOpCall, isa.SubOpCallScalar, isa.SubOpTailCall, isa.SubOpCallMethod,
		isa.SubOpCallMethodInlineable, isa.SubOpCallNative, isa.SubOpCallIIFE:
		return true
	default:
		return false
	}
}
