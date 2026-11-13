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

package verify

import "context"

// checkerLivenessWordBits is the number of registers one bitset word covers.
const checkerLivenessWordBits = 64

// checkerLiveness holds the registers live on entry to each instruction.
type checkerLiveness struct {
	// sets holds one bitset per pc.
	sets [][]uint64

	// registers is the general register count each set covers.
	registers int
}

// checkerLivenessWords returns the word count for a register bitset.
//
// Takes registers (int) which is the register count.
//
// Returns int which is the word count.
func checkerLivenessWords(registers int) int {
	return (max(registers, 0) + checkerLivenessWordBits - 1) / checkerLivenessWordBits
}

// newCheckerLiveness allocates empty liveness sets for a program.
//
// Takes instructions (int) which is the instruction count.
// Takes registers (int) which is the register count.
//
// Returns checkerLiveness which has every register dead everywhere.
func newCheckerLiveness(instructions, registers int) checkerLiveness {
	words := checkerLivenessWords(registers)
	backing := make([]uint64, instructions*words)
	sets := make([][]uint64, instructions)
	for programCounter := range sets {
		sets[programCounter] = backing[programCounter*words : (programCounter+1)*words : (programCounter+1)*words]
	}
	return checkerLiveness{sets: sets, registers: registers}
}

// live reports whether a register is live on entry to an instruction.
//
// Takes programCounter (int) which is the instruction's pc.
// Takes register (int) which is the register.
//
// Returns bool which is false for a pc or register out of range.
func (live checkerLiveness) live(programCounter, register int) bool {
	if programCounter < 0 || programCounter >= len(live.sets) || register < 0 || register >= live.registers {
		return false
	}
	return bitsetHas(live.sets[programCounter], register)
}

// bitsetHas reports whether a register's bit is set.
//
// Takes set ([]uint64) which is the bitset.
// Takes register (int) which is the register, at least zero.
//
// Returns bool which is false for a register beyond the set.
func bitsetHas(set []uint64, register int) bool {
	word := register / checkerLivenessWordBits
	return word < len(set) && set[word]&(1<<(register%checkerLivenessWordBits)) != 0
}

// bitsetSet sets a register's bit.
//
// Takes set ([]uint64) which is the bitset.
// Takes registers (int) which is the register count the set covers.
// Takes register (int) which is the register. Out-of-range registers are ignored.
func bitsetSet(set []uint64, registers, register int) {
	if register >= 0 && register < registers {
		set[register/checkerLivenessWordBits] |= 1 << (register % checkerLivenessWordBits)
	}
}

// bitsetClear clears a register's bit.
//
// Takes set ([]uint64) which is the bitset.
// Takes registers (int) which is the register count the set covers.
// Takes register (int) which is the register. Out-of-range registers are ignored.
func bitsetClear(set []uint64, registers, register int) {
	if register >= 0 && register < registers {
		set[register/checkerLivenessWordBits] &^= 1 << (register % checkerLivenessWordBits)
	}
}

// bitsetFill sets the bit of every register below registers and clears the rest.
//
// Takes set ([]uint64) which is the bitset, checkerLivenessWords(registers) long.
// Takes registers (int) which is the register count.
func bitsetFill(set []uint64, registers int) {
	for word := range set {
		remaining := registers - word*checkerLivenessWordBits
		switch {
		case remaining >= checkerLivenessWordBits:
			set[word] = ^uint64(0)
		case remaining > 0:
			set[word] = 1<<remaining - 1
		default:
			set[word] = 0
		}
	}
}

// liveRegisters computes the registers live on entry to each instruction.
//
// An instruction that may change any register (checkerHavoc) or that the checker cannot
// classify is taken to read every register and write none, so both only keep registers
// live.
//
// Returns checkerLiveness which is indexed by pc, then by register.
// Returns error which is the context's cause when it was cancelled first.
func (p *CheckerProgram) liveRegisters(ctx context.Context) (checkerLiveness, error) {
	live := newCheckerLiveness(len(p.Code), p.Registers)
	predecessors := p.predecessors()
	work := make([]int, len(p.Code))
	queued := make([]bool, len(p.Code))
	for programCounter := range work {
		work[programCounter] = len(p.Code) - 1 - programCounter
		queued[programCounter] = true
	}
	needed := make([]uint64, checkerLivenessWords(p.Registers))
	for steps := 0; len(work) > 0; steps++ {
		if steps%checkerCancelCheckSteps == 0 {
			if cause := context.Cause(ctx); cause != nil {
				return checkerLiveness{}, cause
			}
		}
		programCounter := work[len(work)-1]
		work = work[:len(work)-1]
		queued[programCounter] = false
		if !p.updateLiveIn(live, programCounter, needed) {
			continue
		}
		for _, predecessor := range predecessors.of(programCounter) {
			if !queued[predecessor] {
				queued[predecessor] = true
				work = append(work, predecessor)
			}
		}
	}
	return live, nil
}

// checkerPredecessors lists the instructions that can run before each instruction.
type checkerPredecessors struct {
	// starts holds, at pc and pc+1, the bounds of pc's run in list.
	starts []int

	// list holds every instruction's predecessors, one run per pc.
	list []int
}

// of returns the predecessors of one instruction.
//
// Takes programCounter (int) which is the instruction's pc.
//
// Returns []int which aliases the shared list.
func (predecessors checkerPredecessors) of(programCounter int) []int {
	return predecessors.list[predecessors.starts[programCounter]:predecessors.starts[programCounter+1]]
}

// predecessors lists, for each instruction, the instructions that can run just before it.
//
// Returns checkerPredecessors which is indexed by pc.
func (p *CheckerProgram) predecessors() checkerPredecessors {
	starts := make([]int, len(p.Code)+1)
	for _, successors := range p.Successors {
		for _, successor := range successors {
			if successor >= 0 && successor < len(p.Code) {
				starts[successor+1]++
			}
		}
	}
	for programCounter := range len(p.Code) {
		starts[programCounter+1] += starts[programCounter]
	}
	list := make([]int, starts[len(p.Code)])
	filled := make([]int, len(p.Code))
	for programCounter, successors := range p.Successors {
		for _, successor := range successors {
			if successor >= 0 && successor < len(p.Code) {
				list[starts[successor]+filled[successor]] = programCounter
				filled[successor]++
			}
		}
	}
	return checkerPredecessors{starts: starts, list: list}
}

// updateLiveIn grows the set of registers live on entry to one instruction to hold what
// liveInNeeded asks for.
//
// Takes live (checkerLiveness) which holds the current sets and receives the new one.
// Takes programCounter (int) which is the instruction's pc.
// Takes needed ([]uint64) which is scratch space for one set.
//
// Returns bool which is true when the set grew.
func (p *CheckerProgram) updateLiveIn(live checkerLiveness, programCounter int, needed []uint64) bool {
	p.liveInNeeded(live, programCounter, needed)
	liveIn := live.sets[programCounter]
	changed := false
	for word, bitsNeeded := range needed {
		if grown := liveIn[word] | bitsNeeded; grown != liveIn[word] {
			liveIn[word] = grown
			changed = true
		}
	}
	return changed
}

// liveInNeeded computes the registers that must be live on entry to one instruction,
// given the sets on entry to its successors.
//
// Takes live (checkerLiveness) which holds the sets. A successor outside it is ignored.
// Takes programCounter (int) which is the instruction's pc.
// Takes needed ([]uint64) which receives the set, checkerLivenessWords(p.Registers) long.
func (p *CheckerProgram) liveInNeeded(live checkerLiveness, programCounter int, needed []uint64) {
	instr := p.Code[programCounter]
	if instr.Kind == checkerUnclassified || instr.Kind == checkerHavoc {
		bitsetFill(needed, p.Registers)
		return
	}
	clear(needed)
	for _, successor := range p.Successors[programCounter] {
		if successor >= 0 && successor < len(live.sets) {
			orLive(needed, live.sets[successor])
		}
	}
	eachCheckerWrite(instr, func(register int) { bitsetClear(needed, p.Registers, register) })
	eachCheckerRead(instr, func(register int) { bitsetSet(needed, p.Registers, register) })
}

// orLive marks in into every register that is live in from.
//
// Takes into ([]uint64) which receives the marks.
// Takes from ([]uint64) which is the set copied. Words beyond into are ignored.
func orLive(into, from []uint64) {
	for word := range min(len(into), len(from)) {
		into[word] |= from[word]
	}
}

// closed reports whether the liveness sets satisfy the checker's conditions.
//
// Every set is for the program's registers, and the set on entry to each instruction
// holds what liveInNeeded asks for. Sets that are closed but larger than the least
// solution are accepted, since keeping a register live only makes forget keep more.
//
// Takes p (*CheckerProgram) which is the program the sets describe.
//
// Returns bool which is true when the conditions hold at every instruction.
func (live checkerLiveness) closed(p *CheckerProgram) bool {
	if len(live.sets) != len(p.Code) || live.registers != p.Registers {
		return false
	}
	words := checkerLivenessWords(p.Registers)
	for programCounter := range live.sets {
		if len(live.sets[programCounter]) != words {
			return false
		}
	}
	needed := make([]uint64, words)
	for programCounter := range live.sets {
		if !live.closedAt(p, programCounter, needed) {
			return false
		}
	}
	return true
}

// closedAt reports whether the set on entry to one instruction holds everything
// liveInNeeded asks for.
//
// Takes p (*CheckerProgram) which is the program the sets describe.
// Takes programCounter (int) which is the instruction's pc.
// Takes needed ([]uint64) which is scratch space for one set.
//
// Returns bool which is true when it does.
func (live checkerLiveness) closedAt(p *CheckerProgram, programCounter int, needed []uint64) bool {
	p.liveInNeeded(live, programCounter, needed)
	liveIn := live.sets[programCounter]
	for word, bitsNeeded := range needed {
		if bitsNeeded&^liveIn[word] != 0 {
			return false
		}
	}
	return true
}

// eachCheckerRead calls visit with each register an instruction reads.
//
// Takes instr (CheckerInstr) which is the instruction.
// Takes visit (func(int)) which receives each register.
func eachCheckerRead(instr CheckerInstr, visit func(register int)) {
	switch instr.Kind {
	case CheckerMove:
		visit(instr.Source)
	case CheckerStore:
		visit(instr.Receiver)
		visit(instr.Source)
	case checkerStoreValue:
		visit(instr.Receiver)
	case CheckerRead:
		visit(instr.Receiver)
		for _, register := range instr.Sources {
			visit(register)
		}
	case CheckerOp:
		for _, register := range instr.Sources {
			visit(register)
		}
	case CheckerAlloc, checkerNop, checkerHavoc, checkerUnclassified:
	}
}

// eachCheckerWrite calls visit with each register an instruction is sure to overwrite.
//
// Takes instr (CheckerInstr) which is the instruction.
// Takes visit (func(int)) which receives each register.
func eachCheckerWrite(instr CheckerInstr, visit func(register int)) {
	switch instr.Kind {
	case CheckerAlloc, CheckerMove:
		visit(instr.Destination)
	case CheckerOp, CheckerRead:
		for _, register := range instr.Destinations {
			visit(register)
		}
	case CheckerStore, checkerStoreValue, checkerHavoc, checkerNop, checkerUnclassified:
	}
}

// checkerReads lists the registers an instruction reads.
//
// Takes instr (CheckerInstr) which is the instruction.
//
// Returns []int which holds the registers.
func checkerReads(instr CheckerInstr) []int {
	var reads []int
	eachCheckerRead(instr, func(register int) { reads = append(reads, register) })
	return reads
}

// forget returns state with every register that is not live at pc set to checkerTop.
//
// Takes programCounter (int) which is the instruction the state flows into. One out of
// range forgets nothing.
// Takes state (checkerState) which is the state flowing in.
//
// Returns checkerState which is state when nothing was forgotten, and otherwise a copy.
func (live checkerLiveness) forget(programCounter int, state checkerState) checkerState {
	if programCounter < 0 || programCounter >= len(live.sets) {
		return state
	}
	forgotten := state
	copied := false
	for register, value := range state.registers {
		if value == checkerTop || live.live(programCounter, register) {
			continue
		}
		if !copied {
			forgotten = state.clone()
			copied = true
		}
		forgotten.registers[register] = checkerTop
	}
	return forgotten
}
