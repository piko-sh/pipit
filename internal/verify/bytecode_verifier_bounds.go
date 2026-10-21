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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// extensionOperandA is the position of byte A in an extension word.
	extensionOperandA = 0

	// extensionOperandC is the position of byte C in an extension word.
	extensionOperandC = 2
)

var (
	// ErrRegisterOperandOutOfRange signals a described register operand whose slot lies
	// beyond the function's declared register count for that bank.
	ErrRegisterOperandOutOfRange = errors.New("register operand beyond declared register count")

	// ErrCallSiteOutOfRange signals a call instruction whose call-site index lies past its
	// function's call-site table.
	ErrCallSiteOutOfRange = errors.New("call-site index beyond the function's call-site table")

	// ErrRunMalformed signals an invalid run count or missing extension word.
	ErrRunMalformed = errors.New("run instruction without one extension word for each of its parts")

	// ErrConstantOutOfRange signals an instruction whose constant index lies past the
	// matching constant pool of its function.
	ErrConstantOutOfRange = errors.New("constant index beyond the function's constant pool")

	// ErrExtensionWordMissing signals an instruction missing a required extension word.
	ErrExtensionWordMissing = errors.New("instruction without the extension word it reads")

	// extensionOperandNames labels the operand positions of an extension word in errors.
	extensionOperandNames = [isa.NumInstructionOperands]string{"A", "B", "C"}
)

// VerifyOperandBounds checks that every operand in root and its nested functions that
// indexes a table stays inside it.
//
// Takes root (*CompiledFunction) which is the top-level function to check.
//
// Returns error which wraps ErrRegisterOperandOutOfRange, ErrCallSiteOutOfRange,
// ErrConstantOutOfRange, ErrExtensionWordMissing or ErrRunMalformed with the offending
// location, or the cancellation that stopped the walk. Returns nil when every operand is
// in range.
func VerifyOperandBounds(ctx context.Context, root *program.CompiledFunction) error {
	visited := make(map[*program.CompiledFunction]bool)
	return verifyFunctionOperandBounds(ctx, root, visited)
}

// verifyFunctionOperandBounds checks one function's body and recurses into its children.
//
// Takes compiledFunction (*CompiledFunction) which is the function to check.
// Takes visited (map[*CompiledFunction]bool) which guards against cycles.
//
// Returns the first out-of-range operand or the cancellation as an error, or nil.
func verifyFunctionOperandBounds(ctx context.Context, compiledFunction *program.CompiledFunction, visited map[*program.CompiledFunction]bool) error {
	if compiledFunction == nil || visited[compiledFunction] {
		return nil
	}
	visited[compiledFunction] = true
	nextPoll := 0
	for pc := 0; pc < len(compiledFunction.Body); pc++ {
		if pc >= nextPoll {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("verifying operand bounds of %s: %w", compiledFunction.Name, err)
			}
			nextPoll = pc + program.OptimisationLoopCheckMask + 1
		}
		last, err := checkInstructionBounds(compiledFunction, pc)
		if err != nil {
			return err
		}
		pc = last
	}
	for _, child := range compiledFunction.Functions {
		if err := verifyFunctionOperandBounds(ctx, child, visited); err != nil {
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
// Returns int which is the program counter of the last word the instruction occupies.
// Returns error which names the first out-of-range operand or missing word, or nil.
func checkInstructionBounds(compiledFunction *program.CompiledFunction, pc int) (int, error) {
	instruction := compiledFunction.Body[pc]
	if site, isCall := isa.CallSiteIndex(instruction); isCall && int(site) >= len(compiledFunction.CallSites) {
		return pc, fmt.Errorf("verifier: %s pc=%d op=%s site=%d count=%d: %w",
			compiledFunction.Name, pc, instruction.Op, site, len(compiledFunction.CallSites), ErrCallSiteOutOfRange)
	}
	shape := isa.ShapeForInstruction(instruction)
	if shape.Flags&isa.ShapeFlagDescribed != 0 {
		if err := checkOperandBounds(compiledFunction, pc, instruction, shape); err != nil {
			return pc, err
		}
	}
	if err := checkConstantBound(compiledFunction, pc, instruction); err != nil {
		return pc, err
	}
	if shape.Flags&isa.ShapeFlagFollowsExtension == 0 {
		return pc, nil
	}
	if isa.ReadsExtensionWord(instruction) && (pc+1 >= len(compiledFunction.Body) || compiledFunction.Body[pc+1].Op != isa.OpExtension) {
		return pc, fmt.Errorf("verifier: %s pc=%d op=%s: %w", compiledFunction.Name, pc, instruction.Op, ErrExtensionWordMissing)
	}
	if err := checkFirstExtensionWord(compiledFunction, pc); err != nil {
		return pc, err
	}
	if err := checkRunWords(compiledFunction, pc); err != nil {
		return pc, err
	}
	return checkExtensionBounds(compiledFunction, pc)
}

// checkFirstExtensionWord checks constant and general-register extension operands.
//
// Takes compiledFunction (*CompiledFunction) which supplies the body, pools and register
// counts.
// Takes pc (int) which is the program counter of the instruction that owns the word.
//
// Returns error which names the constant or register that is out of range, or nil.
func checkFirstExtensionWord(compiledFunction *program.CompiledFunction, pc int) error {
	if err := checkExtensionConstantBound(compiledFunction, pc); err != nil {
		return err
	}
	if !isa.IsElementFieldStore(compiledFunction.Body[pc]) {
		return nil
	}
	return checkExtensionRegister(compiledFunction, pc, pc+1, extensionOperandC, isa.RegisterGeneral)
}

// checkConstantBound checks a scalar constant index against its pool size.
//
// Takes compiledFunction (*CompiledFunction) which supplies the constant pools.
// Takes pc (int) which is the instruction's program counter.
// Takes instruction (isa.Instruction) which is the instruction.
//
// Returns ErrConstantOutOfRange wrapped with its location, or nil.
func checkConstantBound(compiledFunction *program.CompiledFunction, pc int, instruction isa.Instruction) error {
	index, kind, ok := isa.ScalarConstantOperand(instruction)
	if !ok {
		return nil
	}
	size := scalarConstantPoolSize(compiledFunction, kind)
	if index < size {
		return nil
	}
	return fmt.Errorf("verifier: %s pc=%d op=%s bank=%d index=%d size=%d: %w",
		compiledFunction.Name, pc, instruction.Op, kind, index, size, ErrConstantOutOfRange)
}

// checkExtensionConstantBound checks a scalar constant index held in an instruction's
// extension word against its pool size.
//
// The assembly bodies of those instructions index the pool without a check of their own.
//
// Takes compiledFunction (*CompiledFunction) which supplies the body and constant pools.
// Takes pc (int) which is the program counter of the instruction that owns the word.
//
// Returns ErrConstantOutOfRange wrapped with its location, or nil.
func checkExtensionConstantBound(compiledFunction *program.CompiledFunction, pc int) error {
	body := compiledFunction.Body
	if pc+1 >= len(body) {
		return nil
	}
	index, kind, ok := isa.ExtensionConstantIndex(body[pc], body[pc+1])
	if !ok {
		return nil
	}
	size := scalarConstantPoolSize(compiledFunction, kind)
	if index < size {
		return nil
	}
	return fmt.Errorf("verifier: %s pc=%d op=%s extension pc=%d bank=%d index=%d size=%d: %w",
		compiledFunction.Name, pc, body[pc].Op, pc+1, kind, index, size, ErrConstantOutOfRange)
}

// scalarConstantPoolSize returns the length of a function's constant pool for a scalar
// register kind.
//
// Takes compiledFunction (*CompiledFunction) which holds the pools.
// Takes kind (isa.RegisterKind) which names the pool.
//
// Returns int which is the pool's length, zero for a kind without one.
func scalarConstantPoolSize(compiledFunction *program.CompiledFunction, kind isa.RegisterKind) int {
	switch kind {
	case isa.RegisterInt64:
		return len(compiledFunction.IntConstants)
	case isa.RegisterUint64:
		return len(compiledFunction.UintConstants)
	case isa.RegisterFloat64:
		return len(compiledFunction.FloatConstants)
	case isa.RegisterString:
		return len(compiledFunction.StringConstants)
	case isa.RegisterBool:
		return len(compiledFunction.BoolConstants)
	case isa.RegisterComplex128:
		return len(compiledFunction.ComplexConstants)
	default:
		return 0
	}
}

// checkRunWords checks the count and extension words of a run instruction.
//
// Takes compiledFunction (*CompiledFunction) which supplies the body and register counts.
// Takes pc (int) which is the program counter of the instruction.
//
// Returns error when the count, extension words or register operands are invalid.
func checkRunWords(compiledFunction *program.CompiledFunction, pc int) error {
	body := compiledFunction.Body
	first := isa.NewInstruction(isa.OpExtension, 0, 0, 0)
	if pc+1 < len(body) {
		first = body[pc+1]
	}
	parts, least, most, isRun := isa.RunParts(body[pc], first)
	if !isRun {
		return nil
	}
	if parts < least || parts > most || pc+parts >= len(body) {
		return fmt.Errorf("verifier: %s pc=%d parts=%d: %w", compiledFunction.Name, pc, parts, ErrRunMalformed)
	}
	if after := pc + parts + 1; after < len(body) && body[after].Op == isa.OpExtension {
		return fmt.Errorf("verifier: %s pc=%d parts=%d word pc=%d: %w", compiledFunction.Name, pc, parts, after, ErrRunMalformed)
	}
	for word := pc + 1; word <= pc+parts; word++ {
		if body[word].Op != isa.OpExtension {
			return fmt.Errorf("verifier: %s pc=%d parts=%d word pc=%d: %w", compiledFunction.Name, pc, parts, word, ErrRunMalformed)
		}
		if err := checkExtensionRegister(compiledFunction, pc, word, extensionOperandA, isa.RegisterUint64); err != nil {
			return err
		}
	}
	if !isa.InstrIsTier1SubOp(body[pc], isa.SubOpGetElementFieldUint64Run) {
		return nil
	}
	scratch := pc + 1 + isa.ElementFieldRunScratchWord
	return checkExtensionRegister(compiledFunction, pc, scratch, extensionOperandC, isa.RegisterGeneral)
}

// checkExtensionRegister checks one register an extension word names outside the int
// bank.
//
// Takes compiledFunction (*CompiledFunction) which supplies the body and register counts.
// Takes pc (int) which is the program counter of the instruction that owns the word.
// Takes word (int) which is the program counter of the extension word.
// Takes position (int) which is the byte of the word that holds the register.
// Takes bank (isa.RegisterKind) which is the bank the register belongs to.
//
// Returns error when the function declares no such register in the bank.
func checkExtensionRegister(compiledFunction *program.CompiledFunction, pc, word, position int, bank isa.RegisterKind) error {
	extension := compiledFunction.Body[word]
	slot := [isa.NumInstructionOperands]uint8{extension.A, extension.B, extension.C}[position]
	if uint32(slot) < compiledFunction.NumRegisters[bank] {
		return nil
	}
	return fmt.Errorf("verifier: %s pc=%d op=%s extension pc=%d operand=%s bank=%d slot=%d count=%d: %w",
		compiledFunction.Name, pc, compiledFunction.Body[pc].Op, word, extensionOperandNames[position], bank, slot,
		compiledFunction.NumRegisters[bank], ErrRegisterOperandOutOfRange)
}

// checkExtensionBounds checks the OpExtension words that follow the instruction at pc,
// however many there are (a defer or go with no arguments carries none).
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
	for last+1 < len(body) && body[last+1].Op == isa.OpExtension {
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
			if uint32(slot) < compiledFunction.NumRegisters[isa.RegisterInt64] {
				continue
			}
			return last, fmt.Errorf("verifier: %s pc=%d op=%s extension pc=%d operand=%s bank=%d slot=%d count=%d: %w",
				compiledFunction.Name, pc, owner.Op, last, extensionOperandNames[position], isa.RegisterInt64, slot,
				compiledFunction.NumRegisters[isa.RegisterInt64], ErrRegisterOperandOutOfRange)
		}
	}
	return last, nil
}

// checkOperandBounds checks the three operand bytes of one described instruction.
//
// Takes compiledFunction (*CompiledFunction) which supplies the register counts and name.
// Takes pc (int) which is the instruction's program counter.
// Takes instruction (instruction) which is the instruction being checked.
// Takes shape (isa.OperandShape) which describes which operands name registers.
//
// Returns an error naming the first out-of-range operand, or nil.
func checkOperandBounds(compiledFunction *program.CompiledFunction, pc int, instruction isa.Instruction, shape isa.OperandShape) error {
	if err := checkOneOperandBound(compiledFunction, pc, instruction, "A", shape.Reads[0] || shape.Writes[0], shape.A, instruction.A); err != nil {
		return err
	}
	if err := checkOneOperandBound(compiledFunction, pc, instruction, "B", shape.Reads[1] || shape.Writes[1], shape.B, instruction.B); err != nil {
		return err
	}
	return checkOneOperandBound(compiledFunction, pc, instruction, "C", shape.Reads[2] || shape.Writes[2], shape.C, instruction.C)
}

// checkOneOperandBound checks a single operand byte against the declared count of the
// bank its role names.
//
// Takes compiledFunction (*CompiledFunction) which supplies the register counts and name.
// Takes pc (int) which is the instruction's program counter.
// Takes instruction (instruction) which is the instruction being checked.
// Takes name (string) which labels the operand position in errors.
// Takes names (bool) which is true when the operand reads or writes a register.
// Takes role (isa.OperandRole) which gives the operand's bank.
// Takes slot (uint8) which is the operand's register index.
//
// Returns an error when the slot lies at or beyond the bank's declared count, or nil.
func checkOneOperandBound(compiledFunction *program.CompiledFunction, pc int, instruction isa.Instruction, name string, names bool, role isa.OperandRole, slot uint8) error {
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
		compiledFunction.Name, pc, instruction.Op, name, bank, slot, compiledFunction.NumRegisters[bank], ErrRegisterOperandOutOfRange)
}
