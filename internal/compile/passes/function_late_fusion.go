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

package passes

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"slices"

	"piko.sh/safeconv"
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// byteStoreWords counts the words in one byte-store instruction and its extension.
const byteStoreWords = 2

// uint64Bits is the width of a uint-bank register.
const uint64Bits = 64

// slotFusion tries one fusion at one slot of a body.
type slotFusion func(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool

// lateFusionSweeps holds the late fusions, one list for each sweep over the body.
var lateFusionSweeps = [...][]slotFusion{
	{
		closeUnhoistedByteView, fuseDisplacedByteStore,
		func(_ *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
			return fuseConstantLatch(body, i, jumpTargets)
		},
		fuseMultiplyAdd,
	},
	slices.Concat(
		[]slotFusion{fuseByteStoreRun, fuseByteReadConversion, fuseFieldReadRun, fuseMapEntryAppend},
		branchConditionFusions,
		[]slotFusion{dropAllOnesMask, dropMaskAfterTruncation},
	),
	{fuseElementFieldRun, storeElementThroughItsPointer},
	branchConditionFusions,
	branchConditionFusions,
}

// branchConditionFusions are the fusions of what a conditional jump tests.
var branchConditionFusions = []slotFusion{dropBoolRoundTripBeforeJump, jumpOnBoolRegister, flipJumpOverNot}

// fuseLatePairsPass applies late fusions before compaction.
type fuseLatePairsPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (fuseLatePairsPass) Name() string { return "fuse-late-pairs" }

// Run applies the late fusions at every slot, one sweep after another.
//
// Takes ctx (context.Context) which cancels a sweep part way.
// Takes state (*PassContext) which carries the shared analysis, invalidated on change.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error when the context is cancelled.
func (fuseLatePairsPass) Run(ctx context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	changed, err := runLateFusions(ctx, compiledFunction, state.Analysis.JumpTargets())
	if changed {
		state.Analysis.invalidate()
	}
	if err != nil {
		return fmt.Errorf("late fusion cancelled: %w", err)
	}
	return nil
}

// runLateFusions makes the sweeps of lateFusionSweeps, then one more for the conversions
// that jumps land on, which shares one record of the body's jumps.
//
// Takes ctx (context.Context) which cancels a sweep part way.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns bool which is true when some fusion rewrote the body.
// Returns error which is the context's when it is cancelled.
func runLateFusions(ctx context.Context, compiledFunction *program.CompiledFunction, jumpTargets map[int]bool) (bool, error) {
	changed := false
	for _, fusions := range lateFusionSweeps {
		swept, err := sweepSlotFusions(ctx, compiledFunction, jumpTargets, fusions)
		changed = changed || swept
		if err != nil {
			return changed, err
		}
	}
	var joins joinEntries
	joined := func(function *program.CompiledFunction, body []isa.Instruction, i int, targets map[int]bool) bool {
		return fuseJoinedReadConversion(function, body, i, targets, &joins)
	}
	swept, err := sweepSlotFusions(ctx, compiledFunction, jumpTargets, []slotFusion{joined})
	return changed || swept, err
}

// sweepSlotFusions tries a list of fusions at every slot of a body, in order.
//
// Takes ctx (context.Context) which cancels the sweep.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
// Takes fusions ([]slotFusion) which are tried at each slot until one takes it.
//
// Returns bool which is true when some fusion rewrote the body.
// Returns error which is the context's when it is cancelled.
func sweepSlotFusions(
	ctx context.Context, compiledFunction *program.CompiledFunction, jumpTargets map[int]bool, fusions []slotFusion,
) (bool, error) {
	body := compiledFunction.Body
	changed := false
	for i := range body {
		if i&program.OptimisationLoopCheckMask == 0 {
			if err := ctx.Err(); err != nil {
				return changed, err
			}
		}
		for _, fuse := range fusions {
			if fuse(compiledFunction, body, i, jumpTargets) {
				changed = true
				break
			}
		}
	}
	return changed, nil
}

// fuseConstantLatch rewrites `INCREMENT_INT64 r` followed by the fused
// `GREATER_OR_EQUAL_INT64_CONSTANT_THEN_JUMP_IF_FALSE r, k`, which jumps while r < k,
// into one isa.SubOpIncrementInt64ThenJumpIfLessThanConstant.
//
// A jump that lands on the increment still runs the whole latch. A jump that lands on the
// test would skip the increment, so it is refused.
//
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the candidate increment.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns true when the pair was fused.
func fuseConstantLatch(body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	if i+2 >= len(body) || !isa.InstrIsTier2SubOp(body[i], isa.SubOpTier2IncrementInt64) ||
		!isa.InstrIsTier1SubOp(body[i+1], isa.SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse) ||
		body[i+2].Op != isa.OpExtension || body[i+1].B != body[i].C || jumpTargets[i+1] {
		return false
	}
	offset := int(isa.JoinOffset(body[i+2].A, body[i+2].B)) + 1
	if !program.FitsJumpOffset(offset) {
		return false
	}
	lo, hi := isa.SplitOffset(safeconv.MustIntToInt16(offset))
	body[i] = isa.NewTier1Instruction(isa.SubOpIncrementInt64ThenJumpIfLessThanConstant, body[i].C, body[i+1].C)
	body[i+1] = isa.NewInstruction(isa.OpExtension, lo, hi, 0)
	body[i+2] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// fuseMultiplyAdd rewrites `MULTIPLY_INT64_CONSTANT t, i, k` followed by an `ADD_INT64`
// of t and another register into one isa.OpMultiplyInt64ConstantThenAdd, whose extension
// word names the other register.
//
// The product register must be dead after the addition, unless the addition overwrites
// it.
//
// Takes compiledFunction (*program.CompiledFunction) whose call sites and result kinds
// the liveness guard reads.
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the candidate multiplication.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns true when the pair was fused.
func fuseMultiplyAdd(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	multiply := body[i]
	if multiply.Op != isa.OpMultiplyInt64Constant {
		return false
	}
	add := i + 1
	for add < len(body) && isCanonicalNoOperation(body[add]) && !jumpTargets[add] {
		add++
	}
	if add >= len(body) || body[add].Op != isa.OpAddInt64 || jumpTargets[add] {
		return false
	}
	product, addition := multiply.A, body[add]
	var other uint8
	switch {
	case addition.C == product && addition.B != product:
		other = addition.B
	case addition.B == product && addition.C != product:
		other = addition.C
	default:
		return false
	}
	if addition.A != product && !registerDeadFrom(compiledFunction, body, add+1, isa.RegisterInt64, product) {
		return false
	}
	body[i] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	body[add-1] = isa.NewInstruction(isa.OpMultiplyInt64ConstantThenAdd, addition.A, multiply.B, multiply.C)
	body[add] = isa.NewInstruction(isa.OpExtension, other, 0, 0)
	return true
}

// closeUnhoistedByteView rewrites `UNBOX_SLICE_BYTE view, s` directly followed by the one
// byte read or store through view back into the element operation on s itself.
//
// A store with a displacement keeps its view, since the element operation has nowhere to
// carry one.
//
// Takes compiledFunction (*program.CompiledFunction) whose call sites and result kinds
// the liveness guard reads.
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the candidate unboxing.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns true when the view was closed.
func closeUnhoistedByteView(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	unbox := body[i]
	if !isa.InstrIsTier1SubOp(unbox, isa.SubOpUnboxSliceByte) {
		return false
	}
	use := i + 1
	for use < len(body) && isCanonicalNoOperation(body[use]) && !jumpTargets[use] {
		use++
	}
	if use+1 >= len(body) || jumpTargets[use] || body[use+1].Op != isa.OpExtension {
		return false
	}
	view, collection := unbox.B, unbox.C
	access, extension := body[use], body[use+1]
	var element isa.Instruction
	switch {
	case isa.InstrIsTier1SubOp(access, isa.SubOpGetSliceByteElement) && access.C == view:
		element = isa.NewInstruction(isa.OpGetElementUint64, access.B, collection, extension.A)
	case isa.InstrIsTier1SubOp(access, isa.SubOpSetSliceByteElement) && access.B == view && extension.B == 0:
		element = isa.NewInstruction(isa.OpSetElementUint64, collection, access.C, extension.A)
	default:
		return false
	}
	if !registerDeadFrom(compiledFunction, body, use+2, isa.RegisterSliceByte, view) {
		return false
	}
	body[i] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	body[use] = element
	body[use+1] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// fuseByteStoreRun rewrites two to four byte stores into consecutive positions of one
// slice, through one index register, into a single isa.SubOpSetSliceByteElementRun.
//
// Only no-operation words may lie between the stores, no jump may land on any word after
// the first store, and each store must be displaced one past the store before it. The run
// stores in the same order with the same checks, so a panic part of the way through
// leaves what the separate stores would have left.
//
// Takes compiledFunction (*program.CompiledFunction) whose source map is kept in step.
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the candidate first store.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns true when a run was formed.
func fuseByteStoreRun(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	stores, count := byteStoreRun(body, i, jumpTargets)
	if count < isa.ByteRunMinStores {
		return false
	}
	run := isa.NewTier1Instruction(isa.SubOpSetSliceByteElementRun, body[i].B, body[i].C)
	var words [isa.ByteRunMaxStores]isa.Instruction
	end := i
	for ordinal := range count {
		words[ordinal] = isa.NewInstruction(isa.OpExtension, body[stores[ordinal]+1].A, 0, 0)
		end = stores[ordinal] + byteStoreWords
	}
	words[0].B, words[0].C = body[i+1].B, safeconv.MustIntToUint8(count)
	for word := i; word < end; word++ {
		body[word] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	}
	body[i] = run
	copy(body[i+1:], words[:count])
	moveRunSourcePositions(compiledFunction.DebugSourceMap, len(body), i, stores[:count])
	return true
}

// moveRunSourcePositions gives each extension word of a run the source position of the
// store or read it stands for.
//
// The word of the part numbered n sits n+1 words after the run, which is never past that
// part's own first word, so the positions are read before they are overwritten.
//
// Takes sourceMap (*program.SourceMap) which may be nil.
// Takes length (int) which is the body's length, checked against the map's.
// Takes run (int) which is the position of the run instruction.
// Takes parts ([]int) which holds the position each fused store or read had.
func moveRunSourcePositions(sourceMap *program.SourceMap, length, run int, parts []int) {
	if sourceMap == nil || len(sourceMap.Positions) != length {
		return
	}
	for ordinal, part := range parts {
		sourceMap.Positions[run+1+ordinal] = sourceMap.Positions[part]
	}
}

// fuseFieldReadRun rewrites `MOVE_GENERAL d, s` followed by two to four
// `GET_STRUCT_FIELD_UINT64 u, d, layout` reads of narrow fields into one
// isa.SubOpGetStructFieldUint64Run.
//
// Only no-operation words may lie between the move and the reads, and no jump may land on
// a read.
//
// Takes compiledFunction (*program.CompiledFunction) whose field layouts and source map
// are read.
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the candidate move.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns true when a run was formed.
func fuseFieldReadRun(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	move := body[i]
	if move.Op != isa.OpMoveGeneral || move.C > isa.FieldRunMaxMode {
		return false
	}
	reads, count := narrowFieldReadsAfter(compiledFunction, body, i, jumpTargets)
	if count < isa.FieldRunMinReads {
		return false
	}
	var words [isa.FieldRunMaxReads]isa.Instruction
	end := i
	for ordinal := range count {
		read := body[reads[ordinal]]
		words[ordinal] = isa.NewInstruction(isa.OpExtension, read.A, read.C, 0)
		end = reads[ordinal] + 1
	}
	words[0].C = safeconv.MustIntToUint8(count) | move.C<<isa.FieldRunModeShift
	for word := i; word < end; word++ {
		body[word] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	}
	body[i] = isa.NewTier1Instruction(isa.SubOpGetStructFieldUint64Run, move.A, move.B)
	copy(body[i+1:], words[:count])
	moveRunSourcePositions(compiledFunction.DebugSourceMap, len(body), i, reads[:count])
	return true
}

// fuseElementFieldRun fuses an element read with consecutive unsigned-field reads.
//
// The reads are either a field run that reads from e or two to four separate field reads
// of e. Each must name a plain field, one the element's storage holds at the layout's
// offset.
//
// Takes compiledFunction (*program.CompiledFunction) which supplies liveness, the field
// layouts and the source map.
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the candidate element read.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns true when the reads were fused.
func fuseElementFieldRun(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	if body[i].Op != isa.OpGetElement {
		return false
	}
	return fuseElementReadIntoItsFieldRun(compiledFunction, body, i, jumpTargets) ||
		fuseElementReadIntoItsFieldReads(compiledFunction, body, i, jumpTargets)
}

// fuseElementReadIntoItsFieldRun is fuseElementFieldRun for an element read followed by a
// field run.
//
// The run's own register and the element's must both be dead after the run, and the
// element's must not be a variable a debugger shows. The fused instruction writes the
// run's register only when a debugger is attached, and the element's never. Only
// no-operation words may lie between the two, and no jump may land on the run. The fused
// instruction uses the element read's source position, which is the one a failing index
// reports.
//
// Takes compiledFunction (*program.CompiledFunction) which supplies liveness, the field
// layouts and the source map.
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the element read.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns true when the pair was fused.
func fuseElementReadIntoItsFieldRun(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	element := body[i].A
	run := nextOperation(body, i+1, jumpTargets)
	if run < 0 || run+isa.FieldRunMinReads >= len(body) || !isa.InstrIsTier1SubOp(body[run], isa.SubOpGetStructFieldUint64Run) || body[run].C != element {
		return false
	}
	receiver := body[run].B
	reads := int(body[run+1].C & isa.FieldRunCountMask)
	after := run + 1 + reads
	if reads < isa.FieldRunMinReads || reads > isa.FieldRunMaxReads || after > len(body) ||
		!everyWordNamesAPlainField(compiledFunction, body[run+1:after]) ||
		namesDebugVariable(compiledFunction, isa.RegisterGeneral, element, i, after) ||
		!registerDeadFrom(compiledFunction, body, after, isa.RegisterGeneral, element) ||
		!registerDeadFrom(compiledFunction, body, after, isa.RegisterGeneral, receiver) {
		return false
	}
	body[run] = isa.NewTier1Instruction(isa.SubOpGetElementFieldUint64Run, body[i].B, body[i].C)
	body[run+1+isa.ElementFieldRunScratchWord].C = receiver
	body[i] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	if sourceMap := compiledFunction.DebugSourceMap; sourceMap != nil && len(sourceMap.Positions) == len(body) {
		sourceMap.Positions[run] = sourceMap.Positions[i]
	}
	return true
}

// fuseElementReadIntoItsFieldReads is fuseElementFieldRun for an element read followed by
// two to four `GET_STRUCT_FIELD_UINT64 u, e, layout` reads of the element.
//
// The element's register must be dead after the last read and must not be a variable a
// debugger shows. Only no-operation words may lie between the instructions, and no jump
// may land on a read. The fused instruction takes the element read's place, and each
// extension word the source position of the read it stands for.
//
// Takes compiledFunction (*program.CompiledFunction) which supplies liveness, the field
// layouts and the source map.
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the element read.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns true when the reads were fused.
func fuseElementReadIntoItsFieldReads(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	element := body[i].A
	reads, count, end := plainFieldReadsAfter(compiledFunction, body, i, element, jumpTargets)
	if count < isa.FieldRunMinReads {
		return false
	}
	if namesDebugVariable(compiledFunction, isa.RegisterGeneral, element, i, end) ||
		!registerDeadFrom(compiledFunction, body, end, isa.RegisterGeneral, element) {
		return false
	}
	var words [isa.FieldRunMaxReads]isa.Instruction
	for ordinal := range count {
		read := body[reads[ordinal]]
		words[ordinal] = isa.NewInstruction(isa.OpExtension, read.A, read.C, 0)
	}
	words[0].C = safeconv.MustIntToUint8(count) | engine.MoveGeneralModeAlias<<isa.FieldRunModeShift
	words[isa.ElementFieldRunScratchWord].C = element
	head := isa.NewTier1Instruction(isa.SubOpGetElementFieldUint64Run, body[i].B, body[i].C)
	for word := i; word < end; word++ {
		body[word] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	}
	body[i] = head
	copy(body[i+1:], words[:count])
	moveRunSourcePositions(compiledFunction.DebugSourceMap, len(body), i, reads[:count])
	return true
}

// plainFieldReadsAfter finds the reads of plain unsigned fields of one register that
// follow the instruction at i, up to the most a run holds.
//
// Takes compiledFunction (*program.CompiledFunction) whose field layouts are read.
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes i (int) which is the instruction the reads follow.
// Takes receiver (uint8) which is the general register the reads are of.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns reads ([isa.FieldRunMaxReads]int) which holds the position of each read.
// Returns count (int) which is how many there are.
// Returns end (int) which is the position after the last of them.
func plainFieldReadsAfter(
	compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, receiver uint8, jumpTargets map[int]bool,
) (reads [isa.FieldRunMaxReads]int, count, end int) {
	end = i + 1
	for pc := i + 1; count < isa.FieldRunMaxReads && pc < len(body) && !jumpTargets[pc]; pc++ {
		read := body[pc]
		if isCanonicalNoOperation(read) {
			continue
		}
		if read.Op != isa.OpGetStructFieldUint64 || read.B != receiver || !namesAPlainField(compiledFunction, read.C) {
			break
		}
		reads[count] = pc
		count++
		end = pc + 1
	}
	return reads, count, end
}

// everyWordNamesAPlainField reports whether each word of a field run is an extension word
// that names a plain field in its byte B.
//
// Takes compiledFunction (*program.CompiledFunction) whose layout table is indexed.
// Takes words ([]isa.Instruction) which are the run's extension words.
//
// Returns bool which is true when every one does.
func everyWordNamesAPlainField(compiledFunction *program.CompiledFunction, words []isa.Instruction) bool {
	return !slices.ContainsFunc(words, func(word isa.Instruction) bool {
		return word.Op != isa.OpExtension || !namesAPlainField(compiledFunction, word.B)
	})
}

// namesAPlainField reports whether a layout names a directly stored struct field.
//
// Embedded pointers and interface-backed fields do not qualify.
//
// Takes compiledFunction (*program.CompiledFunction) whose layout table is indexed.
// Takes layoutIndex (uint8) which is the layout operand.
//
// Returns bool which is true for such a field.
func namesAPlainField(compiledFunction *program.CompiledFunction, layoutIndex uint8) bool {
	return int(layoutIndex) < len(compiledFunction.StructLayoutTable) && compiledFunction.StructLayoutTable[layoutIndex].Flags == 0
}

// narrowFieldReadsAfter finds narrow unsigned-field reads following the move at i.
//
// Takes compiledFunction (*program.CompiledFunction) whose field layouts are read.
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes i (int) which is the move.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns reads ([isa.FieldRunMaxReads]int) which holds the position of each read.
// Returns count (int) which is how many there are.
func narrowFieldReadsAfter(
	compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool,
) (reads [isa.FieldRunMaxReads]int, count int) {
	receiver := body[i].A
	for pc := i + 1; count < isa.FieldRunMaxReads && pc < len(body) && !jumpTargets[pc]; pc++ {
		read := body[pc]
		if isCanonicalNoOperation(read) {
			continue
		}
		if read.Op != isa.OpGetStructFieldUint64 || read.B != receiver || !fieldReadLeavesTheDispatchLoop(compiledFunction, read.C) {
			break
		}
		reads[count] = pc
		count++
	}
	return reads, count
}

// fieldReadLeavesTheDispatchLoop reports whether a field read uses the Go handler.
//
// Takes compiledFunction (*program.CompiledFunction) whose layout table is indexed.
// Takes layoutIndex (uint8) which is the read's layout operand.
//
// Returns true for a layout the table holds that is embedded, cycle-broken or narrower
// than a register.
func fieldReadLeavesTheDispatchLoop(compiledFunction *program.CompiledFunction, layoutIndex uint8) bool {
	if int(layoutIndex) >= len(compiledFunction.StructLayoutTable) {
		return false
	}
	layout := compiledFunction.StructLayoutTable[layoutIndex]
	kind := reflect.Kind(layout.Kind)
	return layout.Flags != 0 || (kind != reflect.Uint && kind != reflect.Uint64)
}

// byteStoreRun finds consecutive byte stores through one slice and index register.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes i (int) which is the candidate first store.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns stores ([isa.ByteRunMaxStores]int) which holds the position of each store.
// Returns count (int) which is how many there are, zero when i is not a byte store.
func byteStoreRun(body []isa.Instruction, i int, jumpTargets map[int]bool) (stores [isa.ByteRunMaxStores]int, count int) {
	pc := i
	for count < isa.ByteRunMaxStores && isByteStore(body, pc) {
		if count > 0 && (jumpTargets[pc] || body[pc].B != body[i].B || body[pc].C != body[i].C ||
			int(body[pc+1].B) != int(body[i+1].B)+count) {
			break
		}
		stores[count] = pc
		count++
		pc += byteStoreWords
		for pc < len(body) && isCanonicalNoOperation(body[pc]) && !jumpTargets[pc] {
			pc++
		}
	}
	return stores, count
}

// isByteStore reports whether an isa.SubOpSetSliceByteElement and its extension word
// stand at pc.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the position to test.
//
// Returns true when a whole byte store stands there.
func isByteStore(body []isa.Instruction, pc int) bool {
	return pc+1 < len(body) && isa.InstrIsTier1SubOp(body[pc], isa.SubOpSetSliceByteElement) && body[pc+1].Op == isa.OpExtension
}

// fuseByteReadConversion rewrites the read of a byte into the uint bank, followed by the
// conversion of that byte to int, into the read alone with the int register as its
// destination.
//
// Only no-operation words may lie between the read and the conversion, no jump may land
// on them or on the conversion, and the uint register must be dead after it, since the
// fused read no longer writes it.
//
// Takes compiledFunction (*program.CompiledFunction) whose call sites and result kinds
// the liveness guard reads.
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the candidate read.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns true when the conversion was folded into the read.
func fuseByteReadConversion(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	read := body[i]
	throughView := isa.InstrIsTier1SubOp(read, isa.SubOpGetSliceByteElement) && i+1 < len(body) && body[i+1].Op == isa.OpExtension
	if !throughView && read.Op != isa.OpGetElementUint64 {
		return false
	}
	unsigned, convert := read.A, i+1
	if throughView {
		unsigned, convert = read.B, i+byteStoreWords
	}
	for convert < len(body) && isCanonicalNoOperation(body[convert]) && !jumpTargets[convert] {
		convert++
	}
	if convert >= len(body) || jumpTargets[convert] ||
		!isa.InstrIsTier1SubOp(body[convert], isa.SubOpConvertUint64ToInt64) || body[convert].C != unsigned ||
		!registerDeadFrom(compiledFunction, body, convert+1, isa.RegisterUint64, unsigned) {
		return false
	}
	if throughView {
		body[i] = isa.NewTier1Instruction(isa.SubOpGetSliceByteElementInt64, body[convert].B, read.C)
	} else {
		body[i] = isa.NewInstruction(isa.OpGetElementInt64, body[convert].B, read.B, read.C)
	}
	body[convert] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// dropBoolRoundTripBeforeJump removes int-to-bool-to-int conversions before a jump on the
// result, which is zero exactly when the int was.
//
// The bool and the converted int must be dead afterwards, and no jump may land between
// the first conversion and the jump.
//
// Takes compiledFunction (*program.CompiledFunction) which supplies liveness.
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the candidate conversion to bool.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns bool which is true when the conversions were removed.
func dropBoolRoundTripBeforeJump(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	if !isa.InstrIsTier1SubOp(body[i], isa.SubOpConvertInt64ToBool) {
		return false
	}
	flag, source := body[i].B, body[i].C
	back := nextOperation(body, i+1, jumpTargets)
	if back < 0 || !isa.InstrIsTier1SubOp(body[back], isa.SubOpConvertBoolToInt64) || body[back].C != flag {
		return false
	}
	converted := body[back].B
	jump := nextOperation(body, back+1, jumpTargets)
	if jump < 0 || !isRegisterBranch(body[jump]) || body[jump].A != converted {
		return false
	}
	target, ok := program.JumpTargetAt(body, jump)
	if !ok || !registerDeadFrom(compiledFunction, body, back+1, isa.RegisterBool, flag) ||
		!registerDeadFrom(compiledFunction, body, jump+program.JumpFootprint(body[jump]), isa.RegisterInt64, converted) ||
		!registerDeadFrom(compiledFunction, body, target, isa.RegisterInt64, converted) {
		return false
	}
	body[i] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	body[back] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	body[jump].A = source
	return true
}

// jumpOnBoolRegister fuses a bool-to-int conversion and conditional jump into a bool
// jump.
//
// The pair takes the two words that end at the jump, so the jump's offset stands as it
// is. The converted int must be dead on both ways out, and no jump may land between the
// conversion and the jump.
//
// Takes compiledFunction (*program.CompiledFunction) which supplies liveness.
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the candidate conversion.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns bool which is true when the pair was replaced.
func jumpOnBoolRegister(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	if !isa.InstrIsTier1SubOp(body[i], isa.SubOpConvertBoolToInt64) {
		return false
	}
	converted, flag := body[i].B, body[i].C
	jump := nextOperation(body, i+1, jumpTargets)
	if jump < 0 || body[jump].A != converted {
		return false
	}
	fused := isa.SubOpTier2JumpIfFalseBool
	switch body[jump].Op {
	case isa.OpJumpIfFalse:
	case isa.OpJumpIfTrue:
		fused = isa.SubOpTier2JumpIfTrueBool
	default:
		return false
	}
	target, ok := program.JumpTargetAt(body, jump)
	if !ok || !registerDeadFrom(compiledFunction, body, jump+program.JumpFootprint(body[jump]), isa.RegisterInt64, converted) ||
		!registerDeadFrom(compiledFunction, body, target, isa.RegisterInt64, converted) {
		return false
	}
	low, high := isa.SplitOffset(body[jump].SignedOffset())
	body[i] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	body[jump-1] = isa.NewTier2Instruction(fused, flag)
	body[jump] = isa.NewInstruction(isa.OpExtension, low, high, 0)
	return true
}

// flipJumpOverNot replaces a jump on a negation with a jump on the opposite condition.
//
// The negation must be dead on both ways out of the jump, and no jump may land on the
// jump itself.
//
// Takes compiledFunction (*program.CompiledFunction) which supplies liveness.
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the candidate negation.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns bool which is true when the negation was removed.
func flipJumpOverNot(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	if !isa.InstrIsTier1SubOp(body[i], isa.SubOpNotInt64) {
		return false
	}
	negated, operand := body[i].B, body[i].C
	jump := nextOperation(body, i+1, jumpTargets)
	if jump < 0 || body[jump].A != negated {
		return false
	}
	opposite := isa.OpJumpIfTrue
	switch body[jump].Op {
	case isa.OpJumpIfFalse:
	case isa.OpJumpIfTrue:
		opposite = isa.OpJumpIfFalse
	default:
		return false
	}
	target, ok := program.JumpTargetAt(body, jump)
	if !ok || !registerDeadFrom(compiledFunction, body, jump+program.JumpFootprint(body[jump]), isa.RegisterInt64, negated) ||
		!registerDeadFrom(compiledFunction, body, target, isa.RegisterInt64, negated) {
		return false
	}
	body[i] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	body[jump].Op = opposite
	body[jump].A = operand
	return true
}

// dropAllOnesMask removes an all-ones AND, using a move when the destination differs.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the constant pool.
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the candidate mask.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns bool which is true when the mask was removed.
func dropAllOnesMask(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	if !isa.InstrIsTier1SubOp(body[i], isa.SubOpBitAndUint64Constant) || i+1 >= len(body) ||
		body[i+1].Op != isa.OpExtension || jumpTargets[i+1] {
		return false
	}
	index := int(isa.JoinWide(body[i+1].A, body[i+1].B))
	if index >= len(compiledFunction.UintConstants) || compiledFunction.UintConstants[index] != math.MaxUint64 {
		return false
	}
	replacement := isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	if destination, source := body[i].B, body[i].C; destination != source {
		replacement = isa.NewTier1Instruction(isa.SubOpMoveUint64, destination, source)
	}
	body[i] = replacement
	body[i+1] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// dropMaskAfterTruncation removes an all-ones mask after a matching unsigned truncation,
// or after an operation that truncates its own result to the same width.
//
// Takes compiledFunction (*program.CompiledFunction) which holds the constant pool.
// Takes body ([]isa.Instruction) which is the instruction stream, rewritten in place.
// Takes i (int) which is the candidate truncation.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns bool which is true when the mask was removed.
func dropMaskAfterTruncation(compiledFunction *program.CompiledFunction, body []isa.Instruction, i int, jumpTargets map[int]bool) bool {
	register, width, words, ok := unsignedWidthLeftBy(body[i])
	if !ok {
		return false
	}
	mask := nextOperation(body, i+words, jumpTargets)
	if mask < 0 || mask+1 >= len(body) || !isa.InstrIsTier1SubOp(body[mask], isa.SubOpBitAndUint64Constant) ||
		body[mask].B != register || body[mask].C != register || body[mask+1].Op != isa.OpExtension || jumpTargets[mask+1] {
		return false
	}
	index := int(isa.JoinWide(body[mask+1].A, body[mask+1].B))
	if index >= len(compiledFunction.UintConstants) || compiledFunction.UintConstants[index] != 1<<width-1 {
		return false
	}
	body[mask] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	body[mask+1] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
	return true
}

// unsignedWidthLeftBy reports the unsigned width left in a uint register.
//
// Takes inst (isa.Instruction) which is the instruction.
//
// Returns register (uint8) which is the uint register written.
// Returns width (uint8) which is the number of low bits the register may have set.
// Returns words (int) which is how many words the instruction takes, its extension word
// included.
// Returns ok (bool) which is false for any other instruction.
func unsignedWidthLeftBy(inst isa.Instruction) (register, width uint8, words int, ok bool) {
	if inst.Op == isa.OpTruncateToWidth {
		return inst.A, inst.B, 1, isa.RegisterKind(inst.C) == isa.RegisterUint64 && inst.B != 0 && inst.B < uint64Bits
	}
	register, words, ok = narrow32UintResult(inst)
	return register, narrow32Width, words, ok
}

// nextOperation returns the program counter of the first word from pc on that is not a
// canonical no-operation, provided no jump lands on it or on a skipped word.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is where to start.
// Takes jumpTargets (map[int]bool) which marks the protected jump destinations.
//
// Returns int which is the program counter, or -1 when the body ends or a jump lands on
// the way.
func nextOperation(body []isa.Instruction, pc int, jumpTargets map[int]bool) int {
	for ; pc < len(body); pc++ {
		if jumpTargets[pc] {
			return -1
		}
		if !isCanonicalNoOperation(body[pc]) {
			return pc
		}
	}
	return -1
}
