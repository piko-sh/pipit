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

	"piko.sh/safeconv"

	"pipit.sh/pipit/internal/isa"
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

// handleStringsContainsRune handles the isa.OpStringsContainsRune instruction by checking
// if a string contains the specified rune.
//
// Takes registers (*Registers) which holds the string and rune operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsContainsRune(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Bools[instruction.A] = strings.ContainsRune(registers.Strings[instruction.B], safeconv.Int64ToInt32(registers.Ints[instruction.C]))
	return opContinue
}

// handleStringsContains handles the isa.OpStringsContains instruction by checking if a
// string contains the specified substring.
//
// Takes registers (*Registers) which holds the string operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsContains(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Bools[instruction.A] = strings.Contains(registers.Strings[instruction.B], registers.Strings[instruction.C])
	return opContinue
}

// handleStringsHasPrefix handles the isa.OpStringsHasPrefix instruction by checking if a
// string starts with the specified prefix.
//
// Takes registers (*Registers) which holds the string and prefix operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsHasPrefix(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Bools[instruction.A] = strings.HasPrefix(registers.Strings[instruction.B], registers.Strings[instruction.C])
	return opContinue
}

// handleStringsHasSuffix handles the isa.OpStringsHasSuffix instruction by checking if a
// string ends with the specified suffix.
//
// Takes registers (*Registers) which holds the string and suffix operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsHasSuffix(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Bools[instruction.A] = strings.HasSuffix(registers.Strings[instruction.B], registers.Strings[instruction.C])
	return opContinue
}

// handleStringsEqualFold handles the isa.OpStringsEqualFold instruction by performing a
// case-insensitive string comparison.
//
// Takes registers (*Registers) which holds the two string operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsEqualFold(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Bools[instruction.A] = strings.EqualFold(registers.Strings[instruction.B], registers.Strings[instruction.C])
	return opContinue
}

// handleStringsIndex handles the isa.OpStringsIndex instruction by finding the index of
// the first occurrence of a substring within a string.
//
// Takes registers (*Registers) which holds the string and substring operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsIndex(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(strings.Index(registers.Strings[instruction.B], registers.Strings[instruction.C]))
	return opContinue
}

// handleStringsCount handles the isa.OpStringsCount instruction by counting the
// non-overlapping occurrences of a substring within a string.
//
// Takes registers (*Registers) which holds the string and substring operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsCount(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(strings.Count(registers.Strings[instruction.B], registers.Strings[instruction.C]))
	return opContinue
}

// handleStringsToUpperBody handles the isa.SubOpStringsToUpper instruction by converting
// all characters in a string to uppercase.
//
// Takes vm (*VM) which is the VM executing the instruction.
// Takes registers (*Registers) which holds the source string and destination.
// Takes instruction (isa.Instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsToUpperBody(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.A] = arenaToUpper(vm.Arena, registers.Strings[instruction.B])
	return opContinue
}

// handleStringsToLowerBody handles the isa.SubOpStringsToLower instruction by converting
// all characters in a string to lowercase.
//
// Takes vm (*VM) which is the VM executing the instruction.
// Takes registers (*Registers) which holds the source string and destination.
// Takes instruction (isa.Instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsToLowerBody(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.A] = arenaToLower(vm.Arena, registers.Strings[instruction.B])
	return opContinue
}

// handleStringsTrimSpaceBody handles the isa.SubOpStringsTrimSpace instruction by
// removing leading and trailing whitespace from a string.
//
// Takes registers (*Registers) which holds the source string and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsTrimSpaceBody(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.A] = strings.TrimSpace(registers.Strings[instruction.B])
	return opContinue
}

// handleStringsTrimPrefix handles the isa.OpStringsTrimPrefix instruction by removing the
// specified prefix from a string if present.
//
// Takes registers (*Registers) which holds the string and prefix operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsTrimPrefix(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.A] = strings.TrimPrefix(registers.Strings[instruction.B], registers.Strings[instruction.C])
	return opContinue
}

// handleStringsTrimSuffix handles the isa.OpStringsTrimSuffix instruction by removing the
// specified suffix from a string if present.
//
// Takes registers (*Registers) which holds the string and suffix operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsTrimSuffix(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.A] = strings.TrimSuffix(registers.Strings[instruction.B], registers.Strings[instruction.C])
	return opContinue
}

// handleStringsTrim handles the isa.OpStringsTrim instruction by removing leading and
// trailing characters in a cutset from a string.
//
// Takes registers (*Registers) which holds the string and cutset operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsTrim(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.A] = strings.Trim(registers.Strings[instruction.B], registers.Strings[instruction.C])
	return opContinue
}

// handleStringsIndexRune handles the isa.OpStringsIndexRune instruction by finding the
// index of the first occurrence of a rune within a string.
//
// Takes registers (*Registers) which holds the string and rune operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsIndexRune(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(strings.IndexRune(registers.Strings[instruction.B], safeconv.Int64ToInt32(registers.Ints[instruction.C])))
	return opContinue
}

// handleMathAbsBody handles the isa.SubOpMathAbs instruction by computing the absolute
// value of a float register.
//
// Takes registers (*Registers) which holds the source float and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMathAbsBody(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = math.Abs(registers.Floats[instruction.B])
	return opContinue
}

// handleMathSqrtBody handles the isa.SubOpMathSqrt instruction by computing the square
// root of a float register.
//
// Takes registers (*Registers) which holds the source float and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMathSqrtBody(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = math.Sqrt(registers.Floats[instruction.B])
	return opContinue
}

// handleMathFloorBody handles the isa.SubOpMathFloor instruction by computing the floor
// of a float register value.
//
// Takes registers (*Registers) which holds the source float and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMathFloorBody(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = math.Floor(registers.Floats[instruction.B])
	return opContinue
}

// handleMathCeilBody handles the isa.SubOpMathCeil instruction by computing the ceiling
// of a float register value.
//
// Takes registers (*Registers) which holds the source float and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMathCeilBody(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = math.Ceil(registers.Floats[instruction.B])
	return opContinue
}

// handleMathRoundBody handles the isa.SubOpMathRound instruction by rounding a float
// register value to the nearest integer.
//
// Takes registers (*Registers) which holds the source float and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMathRoundBody(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
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

// handleMathTruncBody handles the isa.SubOpMathTrunc instruction by truncating a float
// register value toward zero.
//
// Takes registers (*Registers) which holds the source float and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleMathTruncBody(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Floats[instruction.A] = math.Trunc(registers.Floats[instruction.B])
	return opContinue
}

// handleStringsRepeat handles the isa.OpStringsRepeat instruction by repeating a string
// the specified number of times. A negative count or a result that would exceed the
// configured string-size limit raises an interpreted panic instead of crashing the host.
//
// Takes vm (*VM) which provides the string-size limit.
// Takes registers (*Registers) which holds the string, count and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsRepeat(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
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
	registers.Strings[instruction.A] = arenaRepeat(vm.Arena, source, count)
	return opContinue
}

// handleStringsLastIndex handles the isa.OpStringsLastIndex instruction by finding the
// index of the last occurrence of a substring within a string.
//
// Takes registers (*Registers) which holds the string and substring operands.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsLastIndex(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Ints[instruction.A] = int64(strings.LastIndex(registers.Strings[instruction.B], registers.Strings[instruction.C]))
	return opContinue
}

// handleConcatenateStringRune handles the isa.OpConcatenateStringRune instruction by
// concatenating a string with a rune using arena-based allocation.
//
// Takes vm (*VM) which provides the arena for string allocation.
// Takes registers (*Registers) which holds the string, rune and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleConcatenateStringRune(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if vm.Limits.MaxStringSize > 0 && len(registers.Strings[instruction.B])+maxUTF8RuneBytes > vm.Limits.MaxStringSize {
		vm.evalError = fmt.Errorf("%w: concat result exceeds limit %d",
			errStringLimit, vm.Limits.MaxStringSize)
		return opPanicError
	}
	registers.Strings[instruction.A] = arenaConcatenateStringRune(
		vm.Arena, registers.Strings[instruction.B], safeconv.Int64ToInt32(registers.Ints[instruction.C]))
	return opContinue
}

// handleStringsJoin handles the isa.OpStringsJoin instruction by joining a slice of
// strings with the specified separator.
//
// Takes vm (*VM) which provides the arena for string materialisation.
// Takes registers (*Registers) which holds the slice and separator.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsJoin(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	slice := registers.General[instruction.B]
	if slice.IsValid() && slice.Kind() == reflect.Interface && !slice.IsNil() {
		slice = slice.Elem()
	}
	if !slice.IsValid() || (slice.Kind() != reflect.Slice && slice.Kind() != reflect.Array) {
		vm.evalError = newRuntimePanicError("strings: Join called on non-slice value")
		return opPanicError
	}

	registers.Strings[instruction.A] = arenaJoin(vm.Arena, slice, registers.Strings[instruction.C])
	return opContinue
}

// handleStringsSplit handles the isa.OpStringsSplit instruction by splitting a string
// around each occurrence of the specified separator.
//
// Takes vm (*VM) which is the VM executing the instruction.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the string and separator.
// Takes instruction (isa.Instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsSplit(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if sep := registers.Strings[instruction.C]; sep != "" && placesBytes(vm, frame) {
		registers.General[instruction.A] = placeSplit(vm.Arena, frame, registers.Strings[instruction.B], sep)
		return opContinue
	}
	parts := strings.Split(registers.Strings[instruction.B], registers.Strings[instruction.C])
	result := reflect.MakeSlice(stringSliceType, len(parts), len(parts))
	for i, p := range parts {
		result.Index(i).SetString(p)
	}
	registers.General[instruction.A] = result
	return opContinue
}

// handleStringsReplaceAll handles the isa.OpStringsReplaceAll instruction by replacing
// all occurrences of a substring with a replacement string.
//
// Takes vm (*VM) which is the VM executing the instruction.
// Takes frame (*CallFrame) which provides the replacement extension word.
// Takes registers (*Registers) which holds the string and replacement values.
// Takes instruction (isa.Instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringsReplaceAll(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	registers.Strings[instruction.A] = arenaReplaceAll(vm.Arena,
		registers.Strings[instruction.B], registers.Strings[instruction.C],
		registers.Strings[extensionWord.A])
	return opContinue
}

// heapJoin is strings.Join over the strings of parts, on the Go heap, as STRINGS_JOIN
// always did. Element i is read by parts.Index(i).String().
//
// Takes parts (reflect.Value) which is a slice or array.
// Takes sep (string) which separates the parts.
//
// Returns string which is the joined string.
func heapJoin(parts reflect.Value, sep string) string {
	elements := make([]string, parts.Len())
	for i := range elements {
		elements[i] = parts.Index(i).String()
	}
	return strings.Join(elements, sep)
}
