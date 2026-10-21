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

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// maxVerifierIterations is the hard upper bound on the verifier worklist iteration count
	// for a single function. The actual cap used is min(maxVerifierIterations,
	// 50*len(body)), bounding work per function while still tolerating dense merge graphs in
	// legitimate bytecode.
	maxVerifierIterations = 100000

	// verifierIterationsPerByteFactor is the multiplier applied to a function's body length
	// when sizing the iteration cap. Each PC may be re-enqueued multiple times due to CFG
	// joins, so the factor is generous.
	verifierIterationsPerByteFactor = 50

	// verifierContextPollInterval is the number of worklist iterations between context
	// cancellation checks. A power of two so the remainder check compiles to a bitmask.
	verifierContextPollInterval = 1024

	// slotWordBits is the number of slots one word of a bank's definedness set covers.
	slotWordBits = 64

	// slotWords is the number of words that cover the 256 slots a uint8 can address.
	slotWords = (math.MaxUint8 + 1) / slotWordBits
)

// verificationError reports a single contract violation found by the verifier. The fields
// name the offending function, instruction, and operand.
type verificationError struct {
	// functionName names the compiled function containing the violation.
	functionName string

	// operand labels the offending operand position (e.g. "A", "B", "C").
	operand string

	// reason describes why the verifier rejected the read.
	reason string

	// pc is the program counter of the offending instruction.
	pc int

	// instruction is the offending instruction word.
	instruction isa.Instruction

	// bank is the register bank of the read operand.
	bank isa.RegisterKind

	// slot is the register slot index within Bank.
	slot uint8
}

// Error returns the human-readable formatted error.
//
// Returns the verification error as a string.
func (e verificationError) Error() string {
	return fmt.Sprintf("verifier: %s pc=%d op=%s operand=%s bank=%d slot=%d: %s",
		e.functionName, e.pc, e.instruction.Op, e.operand, e.bank, e.slot, e.reason)
}

// VerificationReport collects all verification errors found in a compiled function (and
// recursively in its child functions). An empty report is the success case.
type VerificationReport struct {
	// errors lists every contract violation observed by the verifier.
	errors []verificationError
}

// HasErrors reports whether the verifier found any contract violations.
//
// Returns true when at least one verificationError was recorded.
func (r *VerificationReport) HasErrors() bool {
	return len(r.errors) > 0
}

// Format returns the report as a multiline string suitable for logging or test failure
// messages.
//
// Returns the joined error list, one per line.
func (r *VerificationReport) Format() string {
	if !r.HasErrors() {
		return ""
	}
	parts := make([]string, len(r.errors))
	for i, e := range r.errors {
		parts[i] = e.Error()
	}
	return strings.Join(parts, "\n")
}

// Err returns every violation joined into one error, or nil for a clean report. Wrap it
// with fault.ErrBytecodeVerification so callers can test the outcome with errors.Is; its
// text is the same line-per-violation listing Format produces.
//
// Returns error which joins the report's errors, or nil when there are none.
func (r *VerificationReport) Err() error {
	if !r.HasErrors() {
		return nil
	}
	joined := make([]error, len(r.errors))
	for i, e := range r.errors {
		joined[i] = e
	}
	return errors.Join(joined...)
}

// slotSet marks which of a bank's 256 slots are defined, one bit for each.
type slotSet [slotWords]uint64

// verifierState is the per-bank, per-slot definedness map at a particular program point.
//
// Each bank tracks its slots separately. A slot is defined once an instruction on the
// path has written it.
type verifierState struct {
	// banks holds the set of defined slots for each register bank.
	banks [isa.NumRegisterKinds]slotSet
}

// joinFrom merges source into the receiver using path-intersection semantics, where a
// slot is defined only if both predecessors agree it is defined.
//
// Takes source (*verifierState) which is the predecessor state to merge.
//
// Returns bool which is true when the receiver changed, so that the worklist revisits its
// successors.
func (s *verifierState) joinFrom(source *verifierState) bool {
	changed := false
	for bank := range s.banks {
		for word, defined := range s.banks[bank] {
			merged := defined & source.banks[bank][word]
			if merged != defined {
				s.banks[bank][word] = merged
				changed = true
			}
		}
	}
	return changed
}

// markDefined records that bank[slot] has been written.
//
// Takes bank (isa.RegisterKind) which selects the register bank.
// Takes slot (uint8) which is the index within the bank.
func (s *verifierState) markDefined(bank isa.RegisterKind, slot uint8) {
	if int(bank) >= len(s.banks) {
		return
	}
	s.banks[bank][slot/slotWordBits] |= 1 << (slot % slotWordBits)
}

// isDefined reports whether bank[slot] has been written at this program point.
//
// Takes bank (isa.RegisterKind) which selects the register bank.
// Takes slot (uint8) which is the index within the bank.
//
// Returns true when the slot has been written.
func (s *verifierState) isDefined(bank isa.RegisterKind, slot uint8) bool {
	if int(bank) >= len(s.banks) {
		return false
	}
	return s.banks[bank][slot/slotWordBits]&(1<<(slot%slotWordBits)) != 0
}

// markAllDefined records every slot of every bank as written.
func (s *verifierState) markAllDefined() {
	for bank := range s.banks {
		for word := range s.banks[bank] {
			s.banks[bank][word] = math.MaxUint64
		}
	}
}

// verifierEntries holds the entry state of every program counter a block starts at.
type verifierEntries struct {
	// index maps a program counter to one more than its place in states, zero for a program
	// counter no block has reached.
	index []int

	// states holds the entry states in the order they were first reached.
	states []verifierState
}

// join merges state into the entry state of pc, which it becomes when pc is reached for
// the first time.
//
// Takes pc (int) which is the program counter a block starts at.
// Takes state (*verifierState) which is the state at the end of a block that leads there.
//
// Returns bool which is true when pc is newly reached or its entry state changed.
func (e *verifierEntries) join(pc int, state *verifierState) bool {
	if place := e.index[pc]; place != 0 {
		return e.states[place-1].joinFrom(state)
	}
	e.states = append(e.states, *state)
	e.index[pc] = len(e.states)
	return true
}

// at returns the entry state of a program counter that join() has reached.
//
// Takes pc (int) which is the program counter.
//
// Returns *verifierState which is the entry state.
func (e *verifierEntries) at(pc int) *verifierState {
	return &e.states[e.index[pc]-1]
}

// readCheckContext bundles the values shared across all three operand-position read
// checks for a single instruction so that checkOneRead stays under the linter's
// argument-count limit.
type readCheckContext struct {
	// compiledFunction is the function whose body is being verified.
	compiledFunction *program.CompiledFunction

	// current is the entry state immediately before the instruction.
	current *verifierState

	// report receives any new verificationError findings.
	report *VerificationReport

	// pc is the program counter of the instruction being checked.
	pc int

	// instr is the instruction word being checked.
	instr isa.Instruction
}

// VerifyBytecode walks every program.CompiledFunction reachable from root (root itself
// plus root.functions, transitively) and asserts that every typed-bank read targets a
// slot demonstrably written by a preceding instruction on every dataflow path.
//
// Takes root (*program.CompiledFunction) which is the top-level compiled function to
// verify.
//
// Returns a VerificationReport listing all contract violations; an empty report's
// HasErrors returns false.
// Returns a non-nil error when the iteration cap fires or the context is cancelled.
func VerifyBytecode(ctx context.Context, root *program.CompiledFunction) (*VerificationReport, error) {
	report := &VerificationReport{errors: nil}
	visited := make(map[*program.CompiledFunction]bool)
	if err := verifyOneFunction(ctx, root, report, visited); err != nil {
		return report, err
	}
	return report, nil
}

// RunFunctionVerifier performs the abstract-interpretation walk over a single function
// body and accumulates errors into report.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being
// verified.
// Takes report (*VerificationReport) which collects errors.
//
// Returns errVerifierIterationLimitExceeded when the per-function iteration cap fires, a
// wrapped context error on cancellation, and nil on success.
func RunFunctionVerifier(ctx context.Context, compiledFunction *program.CompiledFunction, report *VerificationReport) error {
	body := compiledFunction.Body
	if len(body) == 0 {
		return nil
	}

	entries := verifierEntries{index: make([]int, len(body)), states: nil}
	entries.join(0, new(initialEntryState()))

	jumpTargets := compiledFunction.BuildJumpTargets(body)
	iterationCap := verifierIterationCapFor(len(body))

	worklist := []int{0}
	iterations := 0
	var current verifierState
	for len(worklist) > 0 {
		if iterations >= iterationCap {
			return fmt.Errorf("verifier: %s: %w", compiledFunction.Name, errVerifierIterationLimitExceeded)
		}
		if iterations&(verifierContextPollInterval-1) == 0 {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("verifier: %s: %w", compiledFunction.Name, err)
			}
		}
		iterations++

		pc := worklist[len(worklist)-1]
		worklist = worklist[:len(worklist)-1]

		current = *entries.at(pc)

		next := walkBlock(compiledFunction, body, pc, jumpTargets, &current, report)
		worklist = enqueueVerifierSuccessors(next, &current, &entries, worklist)
	}
	return nil
}

// verifyOneFunction verifies a single program.CompiledFunction and recurses into its
// child functions.
//
// Takes compiledFunction (*program.CompiledFunction) which is the function to verify.
// Takes report (*VerificationReport) which collects errors.
// Takes visited (map[*program.CompiledFunction]bool) which guards against cycles in the
// function-reference graph.
//
// Returns the first non-recoverable error from the worklist loop, or nil on success.
func verifyOneFunction(ctx context.Context, compiledFunction *program.CompiledFunction, report *VerificationReport, visited map[*program.CompiledFunction]bool) error {
	if compiledFunction == nil || visited[compiledFunction] {
		return nil
	}
	visited[compiledFunction] = true

	if len(compiledFunction.Body) > 0 {
		if err := RunFunctionVerifier(ctx, compiledFunction, report); err != nil {
			return err
		}
	}

	for _, child := range compiledFunction.Functions {
		if err := verifyOneFunction(ctx, child, report, visited); err != nil {
			return err
		}
	}
	return nil
}

// verifierIterationCapFor returns the iteration cap for a body of the given length,
// scaling with body size but staying within the absolute floor and ceiling.
//
// Takes bodyLength (int) which is the length of the bytecode body driving the cap.
//
// Returns the verifier iteration cap for the function.
func verifierIterationCapFor(bodyLength int) int {
	scaled := verifierIterationsPerByteFactor * bodyLength
	iterationCap := max(min(maxVerifierIterations, scaled), verifierIterationsPerByteFactor)
	return iterationCap
}

// enqueueVerifierSuccessors merges state into successor entries and queues those that
// change.
//
// Takes successors ([]int) which are the next PCs produced by walking the current block.
// Takes current (*verifierState) which is the joined state at the end of the current
// block.
// Takes entries (*verifierEntries) which records each reached PC's joined entry state.
// Takes worklist ([]int) which is the pending worklist receiving newly reachable
// successors.
//
// Returns the updated worklist slice.
func enqueueVerifierSuccessors(successors []int, current *verifierState, entries *verifierEntries, worklist []int) []int {
	for _, succ := range successors {
		if succ < 0 || succ >= len(entries.index) {
			continue
		}
		if entries.join(succ, current) {
			worklist = append(worklist, succ)
		}
	}
	return worklist
}

// initialEntryState marks every addressable register slot as defined.
//
// Returns a verifierState with all addressable slots marked defined.
func initialEntryState() verifierState {
	var state verifierState
	state.markAllDefined()
	return state
}

// walkBlock interprets instructions starting at pc until it reaches a terminator or a
// jump target. Updates current state in place and returns the list of successor PCs
// (fallthrough and any jump targets).
//
// Takes compiledFunction (*program.CompiledFunction) which is the function being verified
// (used for error reporting and for following extension words).
// Takes body ([]instruction) which is the instruction stream.
// Takes start (int) which is the first instruction in the block.
// Takes jumpTargets (map[int]bool) which marks block leaders.
// Takes current (*verifierState) which is mutated as instructions execute.
// Takes report (*VerificationReport) which collects any errors.
//
// Returns the list of successor PCs to enqueue.
func walkBlock(compiledFunction *program.CompiledFunction, body []isa.Instruction, start int, jumpTargets map[int]bool, current *verifierState, report *VerificationReport) []int {
	pc := start
	for pc < len(body) {
		instr := body[pc]
		shape := isa.ShapeForInstruction(instr)

		switch {
		case shape.Flags&isa.ShapeFlagOpaqueWrites != 0:

			checkInstructionReads(compiledFunction, pc, instr, shape, current, report)
			applyOpaqueWrites(current)
		case shape.Flags&isa.ShapeFlagDescribed != 0:
			checkInstructionReads(compiledFunction, pc, instr, shape, current, report)
			applyInstructionWrites(instr, shape, current)
		default:
			applyOpaqueWrites(current)
		}
		checkTypeSwitchCaseForward(compiledFunction, pc, instr, report)

		next := pc + 1
		if layout, _ := isa.JumpLayoutOf(instr); layout != isa.JumpLayoutNone {
			next = pc + program.JumpFootprint(instr)
		} else if shape.Flags&isa.ShapeFlagFollowsExtension != 0 {
			next = skipExtensionWords(body, next)
		}

		if shape.Flags&isa.ShapeFlagTerminator != 0 {
			return terminatorSuccessors(body, pc, instr)
		}
		if shape.Flags&isa.ShapeFlagControlFlow != 0 {
			return controlFlowSuccessors(body, pc, next)
		}
		if next < len(body) && jumpTargets[next] && next != start {
			return []int{next}
		}
		pc = next
	}
	return nil
}

// skipExtensionWords returns the first program counter at or after pc that is not
// OpExtension.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
// Takes pc (int) which is the first word after the owning operation.
//
// Returns the program counter of the next instruction.
func skipExtensionWords(body []isa.Instruction, pc int) int {
	for pc < len(body) && body[pc].Op == isa.OpExtension {
		pc++
	}
	return pc
}

// checkInstructionReads asserts that every register-read operand targets a slot that has
// been written previously.
//
// Takes compiledFunction (*program.CompiledFunction) which provides function-level
// metadata for error formatting.
// Takes pc (int) which is the program counter for error formatting.
// Takes instr (instruction) which is the instruction being checked.
// Takes shape (isa.OperandShape) which gives per-operand contract information.
// Takes current (*verifierState) which is the entry state for this instruction.
// Takes report (*VerificationReport) which collects errors.
func checkInstructionReads(compiledFunction *program.CompiledFunction, pc int, instr isa.Instruction, shape isa.OperandShape, current *verifierState, report *VerificationReport) {
	ctx := readCheckContext{compiledFunction: compiledFunction, pc: pc, instr: instr, current: current, report: report}
	checkOneRead(ctx, "A", shape.Reads[0], shape.A, instr.A)
	checkOneRead(ctx, "B", shape.Reads[1], shape.B, instr.B)
	checkOneRead(ctx, "C", shape.Reads[2], shape.C, instr.C)
}

// checkOneRead is the per-position read check shared by the three operand slots.
// Splitting from the loop avoids gosec G602 false positives when the linter cannot prove
// fixed-bound indexing.
//
// Takes ctx (readCheckContext) which carries the function, pc, instruction, current
// state, and report destination.
// Takes name (string) which labels the operand position in errors.
// Takes reads (bool) which is true when this operand is a register read.
// Takes role (isa.OperandRole) which gives the operand's role.
// Takes slot (uint8) which is the operand's register-index byte.
func checkOneRead(ctx readCheckContext, name string, reads bool, role isa.OperandRole, slot uint8) {
	if !reads {
		return
	}
	bank, ok := isa.KindForRole(role)
	if !ok {
		return
	}
	if ctx.current.isDefined(bank, slot) {
		return
	}
	ctx.report.errors = append(ctx.report.errors, verificationError{
		functionName: ctx.compiledFunction.Name,
		pc:           ctx.pc,
		instruction:  ctx.instr,
		operand:      name,
		bank:         bank,
		slot:         slot,
		reason:       "read of undefined slot in this bank",
	})
}

// applyInstructionWrites updates current to reflect the writes declared by shape.
//
// Takes instr (instruction) which is the instruction whose writes to apply.
// Takes shape (isa.OperandShape) which declares per-operand write roles.
// Takes current (*verifierState) which is mutated.
func applyInstructionWrites(instr isa.Instruction, shape isa.OperandShape, current *verifierState) {
	applyOneWrite(shape.Writes[0], shape.A, instr.A, current)
	applyOneWrite(shape.Writes[1], shape.B, instr.B, current)
	applyOneWrite(shape.Writes[2], shape.C, instr.C, current)
}

// applyOneWrite is the per-position write update shared by the three operand slots. Like
// checkOneRead it sidesteps gosec G602 by avoiding fixed-array indexing inside a loop.
//
// Takes writes (bool) which is true when this operand is a register write.
// Takes role (isa.OperandRole) which gives the operand's role.
// Takes slot (uint8) which is the operand's register-index byte.
// Takes current (*verifierState) which is mutated.
func applyOneWrite(writes bool, role isa.OperandRole, slot uint8, current *verifierState) {
	if !writes {
		return
	}
	bank, ok := isa.KindForRole(role)
	if !ok {
		return
	}
	current.markDefined(bank, slot)
}

// applyOpaqueWrites widens state when an opcode lacks a described shape.
//
// Every slot is marked as defined, so the verifier may miss invalid reads after this
// point.
//
// Takes current (*verifierState) which is mutated.
func applyOpaqueWrites(current *verifierState) {
	current.markAllDefined()
}

// terminatorSuccessors returns the successor PCs for an instruction that terminates a
// basic block (return, panic, unconditional jump).
//
// Takes body ([]instruction) which is the full instruction stream.
// Takes pc (int) which is the terminator's PC.
// Takes instr (instruction) which is the terminator instruction.
//
// Returns the list of successor PCs (empty for return/panic, single target for
// unconditional jumps).
func terminatorSuccessors(body []isa.Instruction, pc int, instr isa.Instruction) []int {
	if isa.InstrIsTier1SubOp(instr, isa.SubOpTypeSwitchJump) {
		if pc+1 < len(body) {
			return []int{pc + 1}
		}
		return nil
	}
	if target, ok := program.JumpTargetAt(body, pc); ok && target >= 0 && target < len(body) {
		return []int{target}
	}
	return nil
}

// controlFlowSuccessors returns the successor PCs for a non-terminating control-flow
// instruction (conditional jumps, fused arith-and-jump, etc.). Both the fallthrough
// successor and the branch target are returned.
//
// Takes body ([]instruction) which is the full instruction stream.
// Takes pc (int) which is the branch's PC.
// Takes fallthroughPC (int) which is the fallthrough successor PC (already accounting for
// any isa.OpExtension that follows).
//
// Returns the list of successor PCs.
func controlFlowSuccessors(body []isa.Instruction, pc int, fallthroughPC int) []int {
	successors := make([]int, 0, 2)
	if fallthroughPC < len(body) {
		successors = append(successors, fallthroughPC)
	}

	target, ok := program.JumpTargetAt(body, pc)
	if !ok {
		return successors
	}
	if target >= 0 && target < len(body) && target != fallthroughPC {
		successors = append(successors, target)
	}
	return successors
}
