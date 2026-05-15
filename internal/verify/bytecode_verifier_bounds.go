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
)

// VerifyRegisterOperandBounds checks that every described register operand in root and
// its nested functions addresses a slot within the declared register count. Reserved for
// untrusted payloads; freshly compiled code satisfies this by construction.
//
// Takes root (*CompiledFunction) which is the top-level function to check.
//
// Returns ErrRegisterOperandOutOfRange wrapped with the offending location, or nil when
// every operand is in range.
func VerifyRegisterOperandBounds(root *program.CompiledFunction) error {
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
	body := compiledFunction.Body
	for pc := 0; pc < len(body); pc++ {
		instr := body[pc]
		shape := isa.ShapeForInstruction(instr)
		if shape.Flags&isa.ShapeFlagDescribed != 0 {
			if err := checkOperandBounds(compiledFunction, pc, instr, shape); err != nil {
				return err
			}
		}
		if shape.Flags&isa.ShapeFlagFollowsExtension != 0 {
			pc++
		}
	}
	for _, child := range compiledFunction.Functions {
		if err := verifyFunctionOperandBounds(child, visited); err != nil {
			return err
		}
	}
	return nil
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
