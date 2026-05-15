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

package engine

import (
	"fmt"
	"math"
	"reflect"
	"strings"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// maxUTF8RuneBytes is the maximum number of bytes a single UTF-8 rune can occupy.
	maxUTF8RuneBytes = 4
)

var (
	// stringSliceType holds the reflect.Type for []string, used to construct string slices
	// via reflection in string intrinsics.
	stringSliceType = reflect.TypeFor[[]string]()
)

// handleStrContainsRune handles the isa.OpStrContainsRune instruction by checking if a
// string contains the specified rune.
//
// Takes registers (*Registers) which holds the string and rune operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrContainsRune(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Bools[instruction.A] = strings.ContainsRune(registers.Strings[instruction.B], safeconv.Int64ToInt32(registers.Ints[instruction.C]))
	return opContinue
}

// handleStrContains handles the isa.OpStrContains instruction by checking if a string
// contains the specified substring.
//
// Takes registers (*Registers) which holds the string operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrContains(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Bools[instruction.A] = strings.Contains(registers.Strings[instruction.B], registers.Strings[instruction.C])
	return opContinue
}

// handleStrHasPrefix handles the isa.OpStrHasPrefix instruction by checking if a string
// starts with the specified prefix.
//
// Takes registers (*Registers) which holds the string and prefix operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrHasPrefix(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Bools[instruction.A] = strings.HasPrefix(registers.Strings[instruction.B], registers.Strings[instruction.C])
	return opContinue
}

// handleStrHasSuffix handles the isa.OpStrHasSuffix instruction by checking if a string
// ends with the specified suffix.
//
// Takes registers (*Registers) which holds the string and suffix operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrHasSuffix(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Bools[instruction.A] = strings.HasSuffix(registers.Strings[instruction.B], registers.Strings[instruction.C])
	return opContinue
}

// handleStrEqualFold handles the isa.OpStrEqualFold instruction by performing a
// case-insensitive string comparison.
//
// Takes registers (*Registers) which holds the two string operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrEqualFold(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Bools[instruction.A] = strings.EqualFold(registers.Strings[instruction.B], registers.Strings[instruction.C])
	return opContinue
}

// handleStrIndex handles the isa.OpStrIndex instruction by finding the index of the first
// occurrence of a substring within a string.
//
// Takes registers (*Registers) which holds the string and substring operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrIndex(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(strings.Index(registers.Strings[instruction.B], registers.Strings[instruction.C]))
	return opContinue
}

// handleStrCount handles the isa.OpStrCount instruction by counting the non-overlapping
// occurrences of a substring within a string.
//
// Takes registers (*Registers) which holds the string and substring operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrCount(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(strings.Count(registers.Strings[instruction.B], registers.Strings[instruction.C]))
	return opContinue
}

// handleStrToUpper handles the isa.SubOpStrToUpper instruction by converting all
// characters in a string to uppercase.
//
// Takes registers (*Registers) which holds the source string and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrToUpper(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.A] = strings.ToUpper(registers.Strings[instruction.B])
	return opContinue
}

// handleStrToLower handles the isa.SubOpStrToLower instruction by converting all
// characters in a string to lowercase.
//
// Takes registers (*Registers) which holds the source string and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrToLower(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.A] = strings.ToLower(registers.Strings[instruction.B])
	return opContinue
}

// handleStrTrimSpace handles the isa.SubOpStrTrimSpace instruction by removing leading
// and trailing whitespace from a string.
//
// Takes registers (*Registers) which holds the source string and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrTrimSpace(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.A] = strings.TrimSpace(registers.Strings[instruction.B])
	return opContinue
}

// handleStrTrimPrefix handles the isa.OpStrTrimPrefix instruction by removing the
// specified prefix from a string if present.
//
// Takes registers (*Registers) which holds the string and prefix operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrTrimPrefix(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.A] = strings.TrimPrefix(registers.Strings[instruction.B], registers.Strings[instruction.C])
	return opContinue
}

// handleStrTrimSuffix handles the isa.OpStrTrimSuffix instruction by removing the
// specified suffix from a string if present.
//
// Takes registers (*Registers) which holds the string and suffix operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrTrimSuffix(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.A] = strings.TrimSuffix(registers.Strings[instruction.B], registers.Strings[instruction.C])
	return opContinue
}

// handleStrTrim handles the isa.OpStrTrim instruction by removing leading and trailing
// characters in a cutset from a string.
//
// Takes registers (*Registers) which holds the string and cutset operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrTrim(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.A] = strings.Trim(registers.Strings[instruction.B], registers.Strings[instruction.C])
	return opContinue
}

// handleStrIndexRune handles the isa.OpStrIndexRune instruction by finding the index of
// the first occurrence of a rune within a string.
//
// Takes registers (*Registers) which holds the string and rune operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrIndexRune(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(strings.IndexRune(registers.Strings[instruction.B], safeconv.Int64ToInt32(registers.Ints[instruction.C])))
	return opContinue
}

// handleMathAbs handles the isa.SubOpMathAbs instruction by computing the absolute value
// of a float register.
//
// Takes registers (*Registers) which holds the source float and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMathAbs(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = math.Abs(registers.Floats[instruction.B])
	return opContinue
}

// handleMathSqrt handles the isa.SubOpMathSqrt instruction by computing the square root
// of a float register.
//
// Takes registers (*Registers) which holds the source float and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMathSqrt(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = math.Sqrt(registers.Floats[instruction.B])
	return opContinue
}

// handleMathFloor handles the isa.SubOpMathFloor instruction by computing the floor of a
// float register value.
//
// Takes registers (*Registers) which holds the source float and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMathFloor(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = math.Floor(registers.Floats[instruction.B])
	return opContinue
}

// handleMathCeil handles the isa.SubOpMathCeil instruction by computing the ceiling of a
// float register value.
//
// Takes registers (*Registers) which holds the source float and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMathCeil(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = math.Ceil(registers.Floats[instruction.B])
	return opContinue
}

// handleMathRound handles the isa.SubOpMathRound instruction by rounding a float register
// value to the nearest integer.
//
// Takes registers (*Registers) which holds the source float and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMathRound(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = math.Round(registers.Floats[instruction.B])
	return opContinue
}

// handleMathPow handles the isa.OpMathPow instruction by computing base raised to the
// exponent power from two float registers.
//
// Takes registers (*Registers) which holds the base, exponent and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMathPow(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = math.Pow(registers.Floats[instruction.B], registers.Floats[instruction.C])
	return opContinue
}

// handleMathTrunc handles the isa.SubOpMathTrunc instruction by truncating a float
// register value toward zero.
//
// Takes registers (*Registers) which holds the source float and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMathTrunc(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = math.Trunc(registers.Floats[instruction.B])
	return opContinue
}

// handleStrRepeat handles the isa.OpStrRepeat instruction by repeating a string the
// specified number of times. A negative count or a result that would exceed the
// configured string-size limit raises an interpreted panic instead of crashing the host.
//
// Takes vm (*VM) which provides the string-size limit.
// Takes registers (*Registers) which holds the string, count and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrRepeat(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	source := registers.Strings[instruction.B]
	count := int(registers.Ints[instruction.C])
	if count < 0 {
		vm.evalError = newRuntimePanicError("strings: negative Repeat count")
		return opPanicError
	}
	if vm.Limits.MaxStringSize > 0 && len(source) > 0 && count > vm.Limits.MaxStringSize/len(source) {
		vm.evalError = fmt.Errorf("%w: repeat result exceeds limit %d",
			errStringLimit, vm.Limits.MaxStringSize)
		return opPanicError
	}
	registers.Strings[instruction.A] = strings.Repeat(source, count)
	return opContinue
}

// handleStrLastIndex handles the isa.OpStrLastIndex instruction by finding the index of
// the last occurrence of a substring within a string.
//
// Takes registers (*Registers) which holds the string and substring operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrLastIndex(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(strings.LastIndex(registers.Strings[instruction.B], registers.Strings[instruction.C]))
	return opContinue
}

// handleConcatRuneString handles the isa.OpConcatRuneString instruction by concatenating
// a string with a rune using arena-based allocation.
//
// Takes vm (*VM) which provides the arena for string allocation.
// Takes registers (*Registers) which holds the string, rune and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleConcatRuneString(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if vm.Limits.MaxStringSize > 0 && len(registers.Strings[instruction.B])+maxUTF8RuneBytes > vm.Limits.MaxStringSize {
		vm.evalError = fmt.Errorf("%w: concat result exceeds limit %d",
			errStringLimit, vm.Limits.MaxStringSize)
		return opPanicError
	}
	registers.Strings[instruction.A] = arenaConcatRuneString(
		vm.Arena, registers.Strings[instruction.B], safeconv.Int64ToInt32(registers.Ints[instruction.C]))
	return opContinue
}

// handleStrJoin handles the isa.OpStrJoin instruction by joining a slice of strings with
// the specified separator.
//
// Takes vm (*VM) which provides the arena for string materialisation.
// Takes registers (*Registers) which holds the slice and separator.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrJoin(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	slice := registers.General[instruction.B]
	if slice.IsValid() && slice.Kind() == reflect.Interface && !slice.IsNil() {
		slice = slice.Elem()
	}
	if !slice.IsValid() || (slice.Kind() != reflect.Slice && slice.Kind() != reflect.Array) {
		vm.evalError = newRuntimePanicError("strings: Join called on non-slice value")
		return opPanicError
	}
	sep := materialiseString(vm.Arena, registers.Strings[instruction.C])
	n := slice.Len()
	parts := make([]string, n)
	for i := range parts {
		parts[i] = slice.Index(i).String()
	}
	registers.Strings[instruction.A] = strings.Join(parts, sep)
	return opContinue
}

// handleStrSplit handles the isa.OpStrSplit instruction by splitting a string around each
// occurrence of the specified separator.
//
// Takes registers (*Registers) which holds the string and separator.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrSplit(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	parts := strings.Split(registers.Strings[instruction.B], registers.Strings[instruction.C])
	result := reflect.MakeSlice(stringSliceType, len(parts), len(parts))
	for i, p := range parts {
		result.Index(i).SetString(p)
	}
	registers.General[instruction.A] = result
	return opContinue
}

// handleStrReplaceAll handles the isa.OpStrReplaceAll instruction by replacing all
// occurrences of a substring with a replacement string.
//
// Takes frame (*CallFrame) which provides the replacement extension word.
// Takes registers (*Registers) which holds the string and replacement values.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStrReplaceAll(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	registers.Strings[instruction.A] = strings.ReplaceAll(
		registers.Strings[instruction.B], registers.Strings[instruction.C],
		registers.Strings[extensionWord.A])
	return opContinue
}
