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
	"maps"
	"math"
	"math/bits"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// rangeWideningVisits is the number of times a block's entry state may change before the
	// ranges that keep changing are forgotten, which bounds the analysis.
	rangeWideningVisits = 8

	// maxRangeAnalysisWords bounds the functions the range analysis visits.
	maxRangeAnalysisWords = 8192

	// byteValueMax is the largest value a byte load produces.
	byteValueMax = math.MaxUint8

	// registerBits is the width of an int or uint register.
	registerBits = 64

	// rangeCorners is the number of operand-range corner pairs an arithmetic result is
	// computed from.
	rangeCorners = 4
)

// elideRangedTruncatesPass deletes narrowing truncations whose input already fits the
// target width, using value ranges propagated through the control-flow graph.
type elideRangedTruncatesPass struct{}

// Name returns the pass identifier.
//
// Returns string which is the pass identifier.
func (elideRangedTruncatesPass) Name() string { return "elide-ranged-truncates" }

// Run applies elideRangedTruncates().
//
// Takes state (*PassContext) which carries the shared analysis, invalidated on change.
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
//
// Returns error (always nil).
func (elideRangedTruncatesPass) Run(_ context.Context, state *PassContext, compiledFunction *program.CompiledFunction) error {
	if elideRangedTruncates(compiledFunction) {
		state.Analysis.invalidate()
	}
	return nil
}

// intRange is an inclusive range of int-bank values.
type intRange struct {
	// lo is the smallest possible value.
	lo int64

	// hi is the largest possible value.
	hi int64
}

// uintRange is an inclusive range of uint-bank values.
type uintRange struct {
	// lo is the smallest possible value.
	lo uint64

	// hi is the largest possible value.
	hi uint64
}

// rangeState holds the known ranges of int and uint registers; an absent register may
// hold any value.
type rangeState struct {
	// ints maps an int register to its range.
	ints map[uint8]intRange

	// uints maps a uint register to its range.
	uints map[uint8]uintRange
}

// newRangeState returns a state that knows nothing.
//
// Returns the empty state.
func newRangeState() rangeState {
	return rangeState{ints: make(map[uint8]intRange), uints: make(map[uint8]uintRange)}
}

// clone copies the state.
//
// Returns the copy.
func (state rangeState) clone() rangeState {
	return rangeState{ints: maps.Clone(state.ints), uints: maps.Clone(state.uints)}
}

// forget drops everything the state knows.
func (state rangeState) forget() {
	clear(state.ints)
	clear(state.uints)
}

// join keeps the registers both states know, with the hull of their ranges.
//
// Takes other (rangeState) which is the incoming state.
//
// Returns the joined state.
func (state rangeState) join(other rangeState) rangeState {
	joined := newRangeState()
	for reg, known := range state.ints {
		if incoming, ok := other.ints[reg]; ok {
			joined.ints[reg] = intRange{lo: min(known.lo, incoming.lo), hi: max(known.hi, incoming.hi)}
		}
	}
	for reg, known := range state.uints {
		if incoming, ok := other.uints[reg]; ok {
			joined.uints[reg] = uintRange{lo: min(known.lo, incoming.lo), hi: max(known.hi, incoming.hi)}
		}
	}
	return joined
}

// widen keeps only the ranges unchanged between the previous and next states.
//
// Takes next (rangeState) which is the new entry state.
//
// Returns the stable part.
func (state rangeState) widen(next rangeState) rangeState {
	stable := newRangeState()
	for reg, known := range next.ints {
		if state.ints[reg] == known {
			stable.ints[reg] = known
		}
	}
	for reg, known := range next.uints {
		if previous, ok := state.uints[reg]; ok && previous == known {
			stable.uints[reg] = known
		}
	}
	return stable
}

// equal reports whether two states know the same ranges.
//
// Takes other (rangeState) which is the state to compare.
//
// Returns true when they match.
func (state rangeState) equal(other rangeState) bool {
	return maps.Equal(state.ints, other.ints) && maps.Equal(state.uints, other.uints)
}

// rangeBlock is a basic block of the range analysis.
type rangeBlock struct {
	// successors lists the blocks control can reach next.
	successors []int

	// start is the block's first PC.
	start int

	// end is the PC after the block's last word.
	end int
}

// elideRangedTruncates finds the truncations whose input already fits and turns them into
// NOPs.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten.
//
// Returns true when a truncation was removed.
func elideRangedTruncates(compiledFunction *program.CompiledFunction) bool {
	body := compiledFunction.Body
	if len(body) == 0 || len(body) > maxRangeAnalysisWords || bodyHasTypeSwitch(body) || program.MayCreateSharedCells(compiledFunction) {
		return false
	}
	blocks, blockAt := rangeBlocks(body)
	entries := solveRanges(compiledFunction, blocks, blockAt)
	removed := false
	for index, block := range blocks {
		entry, reached := entries[index]
		if !reached {
			continue
		}
		state := entry.clone()
		for pc := block.start; pc < block.end; pc += instructionFootprint(body, pc) {
			if applyRangeTransfer(compiledFunction, state, pc) {
				body[pc] = isa.NewInstruction(isa.OpNop, 0, 0, 0)
				removed = true
			}
		}
	}
	return removed
}

// rangeBlocks splits the body into basic blocks.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
//
// Returns the blocks in PC order and, for each PC that starts a block, its index.
func rangeBlocks(body []isa.Instruction) ([]rangeBlock, map[int]int) {
	leaders := map[int]bool{0: true}
	for pc := 0; pc < len(body); pc += instructionFootprint(body, pc) {
		next := pc + instructionFootprint(body, pc)
		if target, ok := program.JumpTargetAt(body, pc); ok {
			leaders[target] = true
			leaders[next] = true
		}
		if program.IsReturnInstruction(body[pc]) || !isLinearFallThroughFrom(body[pc]) {
			leaders[next] = true
		}
	}
	var blocks []rangeBlock
	blockAt := make(map[int]int)
	for pc := 0; pc < len(body); {
		block := rangeBlock{successors: nil, start: pc, end: pc}
		for block.end = pc; block.end < len(body); {
			last := block.end
			block.end += instructionFootprint(body, block.end)
			if leaders[block.end] || !isLinearFallThroughFrom(body[last]) || program.IsReturnInstruction(body[last]) {
				break
			}
		}
		blockAt[pc] = len(blocks)
		blocks = append(blocks, block)
		pc = block.end
	}
	for index := range blocks {
		blocks[index].successors = rangeSuccessors(body, blocks[index], blockAt)
	}
	return blocks, blockAt
}

// rangeSuccessors lists the blocks reached from the end of block.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes block (rangeBlock) which is the block.
// Takes blockAt (map[int]int) which maps block starts to indices.
//
// Returns the successor block indices.
func rangeSuccessors(body []isa.Instruction, block rangeBlock, blockAt map[int]int) []int {
	last := block.start
	for pc := block.start; pc < block.end; pc += instructionFootprint(body, pc) {
		last = pc
	}
	var successors []int
	if target, ok := program.JumpTargetAt(body, last); ok {
		if index, known := blockAt[target]; known {
			successors = append(successors, index)
		}
	}
	if isLinearFallThroughFrom(body[last]) && !program.IsReturnInstruction(body[last]) {
		if index, known := blockAt[block.end]; known {
			successors = append(successors, index)
		}
	}
	return successors
}

// solveRanges propagates ranges to a fixed point over the blocks.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is analysed.
// Takes blocks ([]rangeBlock) which are the basic blocks.
// Takes blockAt (map[int]int) which maps block starts to indices.
//
// Returns the entry state of every reached block.
func solveRanges(compiledFunction *program.CompiledFunction, blocks []rangeBlock, blockAt map[int]int) map[int]rangeState {
	solver := rangeSolver{entries: map[int]rangeState{blockAt[0]: newRangeState()}, visits: make(map[int]int), pending: []int{blockAt[0]}}
	for len(solver.pending) > 0 {
		index := solver.pending[len(solver.pending)-1]
		solver.pending = solver.pending[:len(solver.pending)-1]
		state := solver.entries[index].clone()
		for pc := blocks[index].start; pc < blocks[index].end; pc += instructionFootprint(compiledFunction.Body, pc) {
			applyRangeTransfer(compiledFunction, state, pc)
		}
		for _, successor := range blocks[index].successors {
			solver.flow(successor, state)
		}
	}
	return solver.entries
}

// rangeSolver holds the worklist state of solveRanges().
type rangeSolver struct {
	// entries maps each reached block to its entry state.
	entries map[int]rangeState

	// visits counts the entry-state changes of each block.
	visits map[int]int

	// pending lists the blocks whose entry state changed.
	pending []int
}

// flow merges a predecessor's exit state into a successor's entry state, queueing the
// successor when it changed and widening once it has changed too often.
//
// Takes successor (int) which is the block index.
// Takes state (rangeState) which is the predecessor's exit state.
func (solver *rangeSolver) flow(successor int, state rangeState) {
	previous, seen := solver.entries[successor]
	next := state
	if seen {
		next = previous.join(state)
		if next.equal(previous) {
			return
		}
		solver.visits[successor]++
		if solver.visits[successor] > rangeWideningVisits {
			next = previous.widen(next)
		}
	}
	solver.entries[successor] = next.clone()
	solver.pending = append(solver.pending, successor)
}

// applyRangeTransfer updates state for the instruction at pc.
//
// Takes compiledFunction (*program.CompiledFunction) which supplies the body and pools.
// Takes state (rangeState) which is updated in place.
// Takes pc (int) which is the instruction.
//
// Returns true when the instruction is a truncation whose input already fits.
func applyRangeTransfer(compiledFunction *program.CompiledFunction, state rangeState, pc int) bool {
	inst := compiledFunction.Body[pc]
	switch {
	case isCanonicalNop(inst):
		return false
	case inst.Op == isa.OpTruncateNarrow:
		return truncateRange(state, inst)
	case inst.Op == isa.OpDrillTier1:
		if tier1RangeTransfer(compiledFunction, state, pc) {
			return false
		}
	default:
		if intRangeTransfer(compiledFunction, state, inst) || uintRangeTransfer(compiledFunction, state, inst) {
			return false
		}
	}
	forgetWrites(compiledFunction.Body, state, pc)
	return false
}

// truncateRange applies a narrowing truncation and reports whether it changed nothing.
//
// Takes state (rangeState) which is updated in place.
// Takes inst (isa.Instruction) which is the truncation.
//
// Returns true when the input already fits the width.
func truncateRange(state rangeState, inst isa.Instruction) bool {
	width := uint(inst.B)
	if width == 0 || width >= registerBits {
		return false
	}
	if isa.RegisterKind(inst.C) == isa.RegisterUint {
		limit := uint64(1)<<width - 1
		known, ok := state.uints[inst.A]
		state.uints[inst.A] = uintRange{lo: 0, hi: limit}
		if ok && known.hi <= limit {
			state.uints[inst.A] = known
			return true
		}
		return false
	}
	bounds := intRange{lo: -1 << (width - 1), hi: 1<<(width-1) - 1}
	known, ok := state.ints[inst.A]
	state.ints[inst.A] = bounds
	if ok && known.lo >= bounds.lo && known.hi <= bounds.hi {
		state.ints[inst.A] = known
		return true
	}
	return false
}

// intRangeTransfer models the main-tier int operations whose result range follows from
// their inputs.
//
// Takes compiledFunction (*program.CompiledFunction) which supplies the int pool.
// Takes state (rangeState) which is updated in place.
// Takes inst (isa.Instruction) which is the instruction.
//
// Returns true when the instruction was modelled.
func intRangeTransfer(compiledFunction *program.CompiledFunction, state rangeState, inst isa.Instruction) bool {
	b, bKnown := state.ints[inst.B]
	c, cKnown := state.ints[inst.C]
	switch inst.Op {
	case isa.OpLoadIntConst:
		if inst.C != 0 || int(inst.B) >= len(compiledFunction.IntConstants) {
			return false
		}
		value := compiledFunction.IntConstants[inst.B]
		state.ints[inst.A] = intRange{lo: value, hi: value}
	case isa.OpAddInt, isa.OpSubInt, isa.OpMulInt:
		setIntResult(state, inst.A, combineInts(inst.Op, b, c), bKnown && cKnown)
	case isa.OpAddIntConst, isa.OpSubIntConst, isa.OpMulIntConst:
		constant, ok := intPoolRange(compiledFunction, inst.C)
		setIntResult(state, inst.A, combineInts(fusedIntBase(inst.Op), b, constant), bKnown && ok)
	case isa.OpBitAnd:
		result, ok := andIntRange(b, bKnown, c, cKnown)
		setIntResult(state, inst.A, result, ok)
	case isa.OpShiftRight:
		setIntResult(state, inst.A, intRange{lo: min(b.lo, 0), hi: max(b.hi, 0)}, bKnown)
	default:
		return false
	}
	return true
}

// uintRangeTransfer models the main-tier uint operations whose result range follows from
// their inputs.
//
// Takes compiledFunction (*program.CompiledFunction) which supplies the uint pool.
// Takes state (rangeState) which is updated in place.
// Takes inst (isa.Instruction) which is the instruction.
//
// Returns true when the instruction was modelled.
func uintRangeTransfer(compiledFunction *program.CompiledFunction, state rangeState, inst isa.Instruction) bool {
	b, bKnown := state.uints[inst.B]
	c, cKnown := state.uints[inst.C]
	switch inst.Op {
	case isa.OpLoadUintConst:
		if inst.C != 0 || int(inst.B) >= len(compiledFunction.UintConstants) {
			return false
		}
		value := compiledFunction.UintConstants[inst.B]
		state.uints[inst.A] = uintRange{lo: value, hi: value}
	case isa.OpAddUint:
		lo, _ := bits.Add64(b.lo, c.lo, 0)
		hi, carry := bits.Add64(b.hi, c.hi, 0)
		setUintResult(state, inst.A, uintRange{lo: lo, hi: hi}, bKnown && cKnown && carry == 0)
	case isa.OpBitAndUint:
		setUintResult(state, inst.A, uintRange{lo: 0, hi: min(boundOrMax(b, bKnown), boundOrMax(c, cKnown))}, bKnown || cKnown)
	case isa.OpBitOrUint, isa.OpBitXorUint:
		setUintResult(state, inst.A, uintRange{lo: 0, hi: allOnesCovering(max(b.hi, c.hi))}, bKnown && cKnown)
	case isa.OpShiftRightUint:
		if !cKnown {
			c = uintRange{lo: 0, hi: math.MaxUint64}
		}
		setUintResult(state, inst.A, uintRange{lo: shiftRightUint(b.lo, c.hi), hi: shiftRightUint(b.hi, c.lo)}, bKnown)
	default:
		return false
	}
	return true
}

// shiftRightUint shifts as the VM does, giving zero for counts of 64 or more.
//
// Takes value (uint64) which is the value shifted.
// Takes count (uint64) which is the shift count.
//
// Returns the shifted value.
func shiftRightUint(value, count uint64) uint64 {
	if count >= registerBits {
		return 0
	}
	return value >> count
}

// tier1RangeTransfer models the tier-1 loads, moves and conversions the analysis tracks.
//
// Takes compiledFunction (*program.CompiledFunction) which supplies the body and pools.
// Takes state (rangeState) which is updated in place.
// Takes pc (int) which is the instruction.
//
// Returns true when the instruction was modelled.
func tier1RangeTransfer(compiledFunction *program.CompiledFunction, state rangeState, pc int) bool {
	body := compiledFunction.Body
	inst := body[pc]
	switch {
	case isa.InstrIsTier1SubOp(inst, isa.SubOpLoadIntConstSmall):
		state.ints[inst.B] = intRange{lo: int64(inst.C), hi: int64(inst.C)}
	case isa.InstrIsTier1SubOp(inst, isa.SubOpLoadUintConstSmall):
		state.uints[inst.B] = uintRange{lo: uint64(inst.C), hi: uint64(inst.C)}
	case isa.InstrIsTier1SubOp(inst, isa.SubOpMoveInt):
		known, ok := state.ints[inst.C]
		setIntResult(state, inst.B, known, ok)
	case isa.InstrIsTier1SubOp(inst, isa.SubOpMoveUint):
		known, ok := state.uints[inst.C]
		setUintResult(state, inst.B, known, ok)
	case isa.InstrIsTier1SubOp(inst, isa.SubOpUintToInt):
		known, ok := state.uints[inst.C]
		setIntResult(state, inst.B, intRange{lo: int64(min(known.lo, math.MaxInt64)), hi: int64(min(known.hi, math.MaxInt64))}, ok && known.hi <= math.MaxInt64)
	case isa.InstrIsTier1SubOp(inst, isa.SubOpIntToUint):
		known, ok := state.ints[inst.C]
		setUintResult(state, inst.B, uintRange{lo: uint64(max(known.lo, 0)), hi: uint64(max(known.hi, 0))}, ok && known.lo >= 0)
	case isa.InstrIsTier1SubOp(inst, isa.SubOpSliceGetByteDirect):
		state.uints[inst.B] = uintRange{lo: 0, hi: byteValueMax}
	case isa.InstrIsTier1SubOp(inst, isa.SubOpBitAndUintConst) && pc+1 < len(body):
		mask, ok := uintPoolBound(compiledFunction, isa.JoinWide(body[pc+1].A, body[pc+1].B))
		known, sourceKnown := state.uints[inst.C]
		setUintResult(state, inst.B, uintRange{lo: 0, hi: min(boundOrMax(uintRange{lo: 0, hi: mask}, ok), boundOrMax(known, sourceKnown))}, ok || sourceKnown)
	case isa.InstrIsTier1SubOp(inst, isa.SubOpGetGlobalWide):
		forgetRegister(state, isa.RegisterKind(inst.C), inst.B)
	case isa.InstrIsTier1SubOp(inst, isa.SubOpSetGlobalWide), isa.InstrIsTier1SubOp(inst, isa.SubOpSetStructFieldSliceByte),
		isa.InstrIsTier1SubOp(inst, isa.SubOpGetStructFieldSliceByte):
	default:
		return false
	}
	return true
}

// forgetWrites drops what the state knows about every int or uint register the
// instruction at pc may write, including through its extension words, or everything when
// those writes are not described.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes state (rangeState) which is updated in place.
// Takes pc (int) which is the instruction.
func forgetWrites(body []isa.Instruction, state rangeState, pc int) {
	inst := body[pc]
	shape := isa.ShapeForInstruction(inst)
	if IsCallInstruction(inst) || shape.Flags&isa.ShapeFlagDescribed == 0 || shape.Flags&isa.ShapeFlagOpaqueWrites != 0 {
		state.forget()
		return
	}
	intWrites := registersWrittenInBank(inst, isa.RoleForKind(isa.RegisterInt))
	uintWrites := registersWrittenInBank(inst, isa.RoleForKind(isa.RegisterUint))
	if intWrites.any || uintWrites.any || !forgetExtensionWrites(body, state, pc) {
		state.forget()
		return
	}
	for _, reg := range intWrites.registers() {
		delete(state.ints, reg)
	}
	for _, reg := range uintWrites.registers() {
		delete(state.uints, reg)
	}
}

// forgetExtensionWrites drops the int and uint registers the extension words after pc
// write. A jump's extension words hold its offset and only read registers, except for the
// map-index-ok branches, which write their ok register there.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes state (rangeState) which is updated in place.
// Takes pc (int) which is the owning instruction.
//
// Returns false when an extension word's layout is unknown.
func forgetExtensionWrites(body []isa.Instruction, state rangeState, pc int) bool {
	switch layout, _ := isa.JumpLayoutOf(body[pc]); layout {
	case isa.JumpLayoutNone:
	case isa.JumpLayoutExtensionBC, isa.JumpLayoutMultiWay:
		return false
	default:
		return true
	}
	end := pc + instructionFootprint(body, pc)
	for word := pc + 1; word < end; word++ {
		if !forgetExtensionWordWrites(body, state, pc, word, isa.RegisterInt) ||
			!forgetExtensionWordWrites(body, state, pc, word, isa.RegisterUint) {
			return false
		}
	}
	return true
}

// forgetExtensionWordWrites drops the registers of bank kind that the extension word at
// word writes.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes state (rangeState) which is updated in place.
// Takes owner (int) which is the owning instruction.
// Takes word (int) which is the extension word.
// Takes kind (isa.RegisterKind) which is the bank.
//
// Returns false when the word's layout is unknown.
func forgetExtensionWordWrites(body []isa.Instruction, state rangeState, owner, word int, kind isa.RegisterKind) bool {
	_, writes, ok := isa.ExtensionRegisterUse(body[owner], body[owner+1:word+1], kind)
	if !ok {
		return false
	}
	extension := body[word]
	operands := [isa.NumInstructionOperands]uint8{extension.A, extension.B, extension.C}
	for position, written := range writes {
		if written {
			forgetRegister(state, kind, operands[position])
		}
	}
	return true
}

// setIntResult records an int register's range, or forgets it when unknown.
//
// Takes state (rangeState) which is updated in place.
// Takes reg (uint8) which is the register.
// Takes result (intRange) which is its range.
// Takes known (bool) which is false when the range is not known.
func setIntResult(state rangeState, reg uint8, result intRange, known bool) {
	if !known {
		delete(state.ints, reg)
		return
	}
	state.ints[reg] = result
}

// setUintResult records a uint register's range, or forgets it when unknown.
//
// Takes state (rangeState) which is updated in place.
// Takes reg (uint8) which is the register.
// Takes result (uintRange) which is its range.
// Takes known (bool) which is false when the range is not known.
func setUintResult(state rangeState, reg uint8, result uintRange, known bool) {
	if !known {
		delete(state.uints, reg)
		return
	}
	state.uints[reg] = result
}

// forgetRegister drops what the state knows about reg when it is in the int or uint bank.
//
// Takes state (rangeState) which is updated in place.
// Takes kind (isa.RegisterKind) which is the register's bank.
// Takes reg (uint8) which is the register.
func forgetRegister(state rangeState, kind isa.RegisterKind, reg uint8) {
	switch kind {
	case isa.RegisterInt:
		delete(state.ints, reg)
	case isa.RegisterUint:
		delete(state.uints, reg)
	default:
	}
}

// combineInts computes the range of an int addition, subtraction or multiplication,
// returning the full range when any corner overflows.
//
// Takes op (isa.Opcode) which is OpAddInt, OpSubInt or OpMulInt.
// Takes b (intRange) which is the left operand.
// Takes c (intRange) which is the right operand.
//
// Returns the result range.
func combineInts(op isa.Opcode, b, c intRange) intRange {
	full := intRange{lo: math.MinInt64, hi: math.MaxInt64}
	corners := [rangeCorners][2]int64{{b.lo, c.lo}, {b.lo, c.hi}, {b.hi, c.lo}, {b.hi, c.hi}}
	result := intRange{lo: math.MaxInt64, hi: math.MinInt64}
	for _, corner := range corners {
		value, ok := intCorner(op, corner[0], corner[1])
		if !ok {
			return full
		}
		result = intRange{lo: min(result.lo, value), hi: max(result.hi, value)}
	}
	return result
}

// intCorner applies op to one pair of range corners.
//
// Takes op (isa.Opcode) which is OpAddInt, OpSubInt or OpMulInt.
// Takes x (int64) which is the left value.
// Takes y (int64) which is the right value.
//
// Returns the result and false on overflow.
func intCorner(op isa.Opcode, x, y int64) (int64, bool) {
	switch op {
	case isa.OpAddInt:
		sum := x + y
		return sum, (sum > x) == (y > 0) || y == 0
	case isa.OpSubInt:
		difference := x - y
		return difference, (difference < x) == (y > 0) || y == 0
	default:
		if x == 0 || y == 0 {
			return 0, true
		}
		product := x * y
		return product, product/y == x && (x != -1 || y != math.MinInt64) && (y != -1 || x != math.MinInt64)
	}
}

// fusedIntBase maps a constant-operand int op to its register form.
//
// Takes op (isa.Opcode) which is the fused op.
//
// Returns the plain op.
func fusedIntBase(op isa.Opcode) isa.Opcode {
	switch op {
	case isa.OpSubIntConst:
		return isa.OpSubInt
	case isa.OpMulIntConst:
		return isa.OpMulInt
	default:
		return isa.OpAddInt
	}
}

// andIntRange bounds a bitwise AND: a non-negative operand bounds the result between zero
// and that operand's maximum.
//
// Takes b (intRange) which is the left range.
// Takes bKnown (bool) which reports whether it is known.
// Takes c (intRange) which is the right range.
// Takes cKnown (bool) which reports whether it is known.
//
// Returns the result range and whether it is known.
func andIntRange(b intRange, bKnown bool, c intRange, cKnown bool) (intRange, bool) {
	bNonNegative := bKnown && b.lo >= 0
	cNonNegative := cKnown && c.lo >= 0
	switch {
	case bNonNegative && cNonNegative:
		return intRange{lo: 0, hi: min(b.hi, c.hi)}, true
	case bNonNegative:
		return intRange{lo: 0, hi: b.hi}, true
	case cNonNegative:
		return intRange{lo: 0, hi: c.hi}, true
	default:
		return intRange{lo: 0, hi: 0}, false
	}
}

// intPoolRange reads an int pool constant as a single-value range.
//
// Takes compiledFunction (*program.CompiledFunction) which owns the pool.
// Takes index (uint8) which is the pool index.
//
// Returns the range and false when the index is out of range.
func intPoolRange(compiledFunction *program.CompiledFunction, index uint8) (intRange, bool) {
	if int(index) >= len(compiledFunction.IntConstants) {
		return intRange{lo: 0, hi: 0}, false
	}
	value := compiledFunction.IntConstants[index]
	return intRange{lo: value, hi: value}, true
}

// uintPoolBound reads a uint pool constant.
//
// Takes compiledFunction (*program.CompiledFunction) which owns the pool.
// Takes index (uint16) which is the pool index.
//
// Returns the value and false when the index is out of range.
func uintPoolBound(compiledFunction *program.CompiledFunction, index uint16) (uint64, bool) {
	if int(index) >= len(compiledFunction.UintConstants) {
		return 0, false
	}
	return compiledFunction.UintConstants[index], true
}

// boundOrMax returns the range's upper bound when known and the largest uint otherwise.
//
// Takes known (uintRange) which is the range.
// Takes isKnown (bool) which reports whether it is known.
//
// Returns the effective upper bound.
func boundOrMax(known uintRange, isKnown bool) uint64 {
	if isKnown {
		return known.hi
	}
	return math.MaxUint64
}

// allOnesCovering returns the smallest all-ones value at least value, which bounds an OR
// or XOR of operands no larger than value.
//
// Takes value (uint64) which is the larger operand bound.
//
// Returns the bound.
func allOnesCovering(value uint64) uint64 {
	if value == 0 {
		return 0
	}
	return math.MaxUint64 >> bits.LeadingZeros64(value)
}
