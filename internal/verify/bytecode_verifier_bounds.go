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
	"errors"
	"fmt"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

var (
	// ErrRegisterOperandOutOfRange signals a described register operand whose slot lies
	// beyond the function's declared register count for that bank.
	ErrRegisterOperandOutOfRange = errors.New("register operand beyond declared register count")

	// ErrCallSiteOutOfRange signals a call instruction whose call-site index lies past its
	// function's call-site table.
	ErrCallSiteOutOfRange = errors.New("call-site index beyond the function's call-site table")

	// extensionOperandNames labels the operand positions of an extension word in errors.
	extensionOperandNames = [isa.NumInstructionOperands]string{"A", "B", "C"}
)

// VerifyOperandBounds checks that every operand in root and its nested functions that
// indexes a table stays inside it.
//
// Takes root (*CompiledFunction) which is the top-level function to check.
//
// Returns ErrRegisterOperandOutOfRange or ErrCallSiteOutOfRange wrapped with the
// offending location, or nil when every operand is in range.
func VerifyOperandBounds(root *program.CompiledFunction) error {
	visited := make(map[*program.CompiledFunction]bool)
	return verifyFunctionOperandBounds(root, visited)
}

// verifyFunctionOperandBounds checks one function's body and recurses into its children.
//
// Takes compiledFunction (*CompiledFunction) which is the function to check.
// Takes visited (map[*CompiledFunction]bool) which guards against cycles.
//
// Returns the first out-of-range operand as an error, or nil.
func verifyFunctionOperandBounds(compiledFunction *program.CompiledFunction, visited map[*program.CompiledFunction]bool) error {
	if compiledFunction == nil || visited[compiledFunction] {
		return nil
	}
	visited[compiledFunction] = true
	for pc := 0; pc < len(compiledFunction.Body); pc++ {
		last, err := checkInstructionBounds(compiledFunction, pc)
		if err != nil {
			return err
		}
		pc = last
	}
	for _, child := range compiledFunction.Functions {
		if err := verifyFunctionOperandBounds(child, visited); err != nil {
			return err
		}
	}
	return nil
}

// checkInstructionBounds checks the call-site index and register operands of the
// instruction at pc and the register operands of the extension words it owns.
//
// Takes compiledFunction (*CompiledFunction) which supplies the body, register counts and
// call sites.
// Takes pc (int) which is the instruction's program counter.
//
// Returns the program counter of the last word the instruction occupies, and an error
// naming the first out-of-range operand, or nil.
func checkInstructionBounds(compiledFunction *program.CompiledFunction, pc int) (int, error) {
	instr := compiledFunction.Body[pc]
	if site, isCall := isa.CallSiteIndex(instr); isCall && int(site) >= len(compiledFunction.CallSites) {
		return pc, fmt.Errorf("verifier: %s pc=%d op=%s site=%d count=%d: %w",
			compiledFunction.Name, pc, instr.Op, site, len(compiledFunction.CallSites), ErrCallSiteOutOfRange)
	}
	shape := isa.ShapeForInstruction(instr)
	if shape.Flags&isa.ShapeFlagDescribed != 0 {
		if err := checkOperandBounds(compiledFunction, pc, instr, shape); err != nil {
			return pc, err
		}
	}
	if shape.Flags&isa.ShapeFlagFollowsExtension == 0 {
		return pc, nil
	}
	return checkExtensionBounds(compiledFunction, pc)
}

// checkExtensionBounds checks the OpExt words that follow the instruction at pc, however
// many there are (a defer or go with no arguments carries none).
//
// Takes compiledFunction (*CompiledFunction) which supplies the body and register counts.
// Takes pc (int) which is the program counter of the instruction that owns the words.
//
// Returns the program counter of the last word consumed (pc itself when none follow), and
// an error naming the first out-of-range int register, or nil.
func checkExtensionBounds(compiledFunction *program.CompiledFunction, pc int) (int, error) {
	body := compiledFunction.Body
	owner := body[pc]
	last := pc
	for last+1 < len(body) && body[last+1].Op == isa.OpExt {
		last++
		reads, writes, ok := isa.ExtensionIntUse(owner, body[pc+1:last+1])
		if !ok {
			continue
		}
		word := body[last]
		operands := [isa.NumInstructionOperands]uint8{word.A, word.B, word.C}
		for position, slot := range operands {
			if !reads[position] && !writes[position] {
				continue
			}
			if uint32(slot) < compiledFunction.NumRegisters[isa.RegisterInt] {
				continue
			}
			return last, fmt.Errorf("verifier: %s pc=%d op=%s extension pc=%d operand=%s bank=%d slot=%d count=%d: %w",
				compiledFunction.Name, pc, owner.Op, last, extensionOperandNames[position], isa.RegisterInt, slot,
				compiledFunction.NumRegisters[isa.RegisterInt], ErrRegisterOperandOutOfRange)
		}
	}
	return last, nil
}

// checkOperandBounds checks the three operand bytes of one described instruction.
//
// Takes compiledFunction (*CompiledFunction) which supplies the register counts and name.
// Takes pc (int) which is the instruction's program counter.
// Takes instr (instruction) which is the instruction being checked.
// Takes shape (isa.OperandShape) which describes which operands name registers.
//
// Returns an error naming the first out-of-range operand, or nil.
func checkOperandBounds(compiledFunction *program.CompiledFunction, pc int, instr isa.Instruction, shape isa.OperandShape) error {
	if err := checkOneOperandBound(compiledFunction, pc, instr, "A", shape.Reads[0] || shape.Writes[0], shape.A, instr.A); err != nil {
		return err
	}
	if err := checkOneOperandBound(compiledFunction, pc, instr, "B", shape.Reads[1] || shape.Writes[1], shape.B, instr.B); err != nil {
		return err
	}
	return checkOneOperandBound(compiledFunction, pc, instr, "C", shape.Reads[2] || shape.Writes[2], shape.C, instr.C)
}

// checkOneOperandBound checks a single operand byte against the declared count of the
// bank its role names.
//
// Takes compiledFunction (*CompiledFunction) which supplies the register counts and name.
// Takes pc (int) which is the instruction's program counter.
// Takes instr (instruction) which is the instruction being checked.
// Takes name (string) which labels the operand position in errors.
// Takes names (bool) which is true when the operand reads or writes a register.
// Takes role (isa.OperandRole) which gives the operand's bank.
// Takes slot (uint8) which is the operand's register index.
//
// Returns an error when the slot lies at or beyond the bank's declared count, or nil.
func checkOneOperandBound(compiledFunction *program.CompiledFunction, pc int, instr isa.Instruction, name string, names bool, role isa.OperandRole, slot uint8) error {
	if !names {
		return nil
	}
	bank, ok := isa.KindForRole(role)
	if !ok {
		return nil
	}
	if uint32(slot) < compiledFunction.NumRegisters[bank] {
		return nil
	}
	return fmt.Errorf("verifier: %s pc=%d op=%s operand=%s bank=%d slot=%d count=%d: %w",
		compiledFunction.Name, pc, instr.Op, name, bank, slot, compiledFunction.NumRegisters[bank], ErrRegisterOperandOutOfRange)
}
