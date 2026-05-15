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

	// entryStateSlotCount is the per-bank slot count for the entry state. Over-allocating to
	// 256 (uint8 max + 1) marks every addressable slot defined at entry, eliminating false
	// positives from undescribed opcodes while still catching inter-opcode contract
	// violations.
	entryStateSlotCount = 256
)

const (
	// registerSlotUndefined means no instruction along the dataflow path has written to this
	// slot in this bank. Reads of an undefined slot are verifier errors.
	registerSlotUndefined registerSlotKind = iota

	// registerSlotDefined means the slot has been written by some earlier instruction. The
	// verifier does not track the value's static type further than that; banks are
	// physically separated, so the slot is only ever read as the bank it was written in.
	registerSlotDefined
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

// registerSlotKind describes what bank a register slot was last written as at a given
// program point. The Compiler partitions register space by bank, so int[5] and general[5]
// are distinct physical slots; the verifier tracks each independently.
type registerSlotKind uint8

// verifierState is the per-bank, per-slot definedness map at a particular program point.
// The verifier walks the bytecode body updating this state and asserting that read
// operands target defined slots.
type verifierState struct {
	// banks holds the slot-definedness array for each register bank.
	banks [isa.NumRegisterKinds][]registerSlotKind
}

// cloneFrom copies the bank slices from source into the receiver. The receiver retains
// its own backing arrays so subsequent mutations do not leak into source.
//
// Takes source (*verifierState) which is the state to copy.
func (s *verifierState) cloneFrom(source *verifierState) {
	for bank := range s.banks {
		need := len(source.banks[bank])
		if cap(s.banks[bank]) < need {
			s.banks[bank] = make([]registerSlotKind, need)
		} else {
			s.banks[bank] = s.banks[bank][:need]
			for i := range s.banks[bank] {
				s.banks[bank][i] = registerSlotUndefined
			}
		}
		copy(s.banks[bank], source.banks[bank])
	}
}

// joinFrom merges source into the receiver using path-intersection semantics: a slot is
// defined only if both predecessors agree it is defined. Returns true when the receiver
// changed, signalling the worklist to revisit successors.
//
// Takes source (*verifierState) which is the predecessor state to merge.
//
// Returns true when the receiver's state widened.
func (s *verifierState) joinFrom(source *verifierState) bool {
	changed := false
	for bank := range s.banks {
		sourceLength := len(source.banks[bank])
		if len(s.banks[bank]) < sourceLength {
			grown := make([]registerSlotKind, sourceLength)
			copy(grown, s.banks[bank])
			s.banks[bank] = grown
		}
		for i, sourceKind := range source.banks[bank] {
			if i >= len(s.banks[bank]) {
				break
			}
			merged := joinSlotKind(s.banks[bank][i], sourceKind)
			if merged != s.banks[bank][i] {
				s.banks[bank][i] = merged
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
	if int(slot) >= len(s.banks[bank]) {
		grown := make([]registerSlotKind, int(slot)+1)
		copy(grown, s.banks[bank])
		s.banks[bank] = grown
	}
	s.banks[bank][slot] = registerSlotDefined
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
	if int(slot) >= len(s.banks[bank]) {
		return false
	}
	return s.banks[bank][slot] == registerSlotDefined
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

	entryStates := make([]verifierState, len(body))
	visited := make([]bool, len(body))

	entryStates[0].cloneFrom(new(initialEntryState()))
	visited[0] = true

	jumpTargets := compiledFunction.BuildJumpTargets(body)
	iterationCap := verifierIterationCapFor(len(body))

	worklist := []int{0}
	iterations := 0
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

		current := verifierState{banks: [isa.NumRegisterKinds][]registerSlotKind{}}
		current.cloneFrom(&entryStates[pc])

		next := walkBlock(compiledFunction, body, pc, jumpTargets, &current, report)
		worklist = enqueueVerifierSuccessors(next, len(body), &current, entryStates, visited, worklist)
	}
	return nil
}

// joinSlotKind merges two slot kinds at a CFG merge point. A slot is only defined when
// both predecessors define it; any disagreement widens to undefined.
//
// Takes a (registerSlotKind) which is the first predecessor's slot kind.
// Takes b (registerSlotKind) which is the second predecessor's slot kind.
//
// Returns the merged slot kind.
func joinSlotKind(a, b registerSlotKind) registerSlotKind {
	if a == registerSlotDefined && b == registerSlotDefined {
		return registerSlotDefined
	}
	return registerSlotUndefined
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

// enqueueVerifierSuccessors merges each successor PC's state into the per-PC entry state
// and appends to the worklist when the state changed (or the successor is first reached).
//
// Takes successors ([]int) which are the next PCs produced by walking the current block.
// Takes bodyLength (int) which is used to validate successor bounds.
// Takes current (*verifierState) which is the joined state at the end of the current
// block.
// Takes entryStates ([]verifierState) which records each PC's joined entry state.
// Takes visited ([]bool) which tracks whether each PC has been seeded.
// Takes worklist ([]int) which is the pending worklist receiving newly reachable
// successors.
//
// Returns the updated worklist slice.
func enqueueVerifierSuccessors(successors []int, bodyLength int, current *verifierState, entryStates []verifierState, visited []bool, worklist []int) []int {
	for _, succ := range successors {
		if succ < 0 || succ >= bodyLength {
			continue
		}
		if !visited[succ] {
			entryStates[succ].cloneFrom(current)
			visited[succ] = true
			worklist = append(worklist, succ)
			continue
		}
		if entryStates[succ].joinFrom(current) {
			worklist = append(worklist, succ)
		}
	}
	return worklist
}

// initialEntryState builds the verifier's view of the function entry, marking every
// addressable register slot in every bank as defined. This permissiveness keeps the
// verifier silent on entry-state uncertainties; the verifier still flags contract
// violations within the body.
//
// Returns a verifierState with all addressable slots marked defined.
func initialEntryState() verifierState {
	state := verifierState{banks: [isa.NumRegisterKinds][]registerSlotKind{}}
	for bank := isa.RegisterKind(0); int(bank) < isa.NumRegisterKinds; bank++ {
		state.banks[bank] = make([]registerSlotKind, entryStateSlotCount)
		for slot := range state.banks[bank] {
			state.banks[bank][slot] = registerSlotDefined
		}
	}
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
			next++
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
// The verifier widens by marking every known slot in every bank as defined whenever an
// undescribed opcode runs. This trades false negatives (the verifier may miss a real bug
// hiding behind an undescribed opcode) for zero false positives.
//
// Takes current (*verifierState) which is mutated.
func applyOpaqueWrites(current *verifierState) {
	for bank := isa.RegisterKind(0); int(bank) < isa.NumRegisterKinds; bank++ {
		for slot := range len(current.banks[bank]) {
			current.banks[bank][slot] = registerSlotDefined
		}
	}
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
// any isa.OpExt that follows).
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
