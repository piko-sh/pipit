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

package compile

import (
	"cmp"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestNarrowArithmeticIsTruncatedOnlyWhereItCanLeaveItsType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		truncates bool
	}{
		{name: "an unsigned sum can carry", source: "func f(a, b uint32) uint32 { return a + b }", truncates: true},
		{name: "an unsigned difference can borrow", source: "func f(a, b uint8) uint8 { return a - b }", truncates: true},
		{name: "a product can overflow", source: "func f(a, b int16) int16 { return a * b }", truncates: true},
		{name: "a left shift can overflow", source: "func f(a uint16, n uint) uint16 { return a << n }", truncates: true},
		{name: "a signed quotient can overflow", source: "func f(a, b int8) int8 { return a / b }", truncates: true},
		{name: "a complement sets the upper bits", source: "func f(a uint8) uint8 { return ^a }", truncates: true},
		{name: "an unsigned and", source: "func f(a, b uint32) uint32 { return a & b }"},
		{name: "an unsigned or", source: "func f(a, b uint16) uint16 { return a | b }"},
		{name: "an unsigned exclusive or", source: "func f(a, b uint8) uint8 { return a ^ b }"},
		{name: "a signed exclusive or", source: "func f(a, b int32) int32 { return a ^ b }"},
		{name: "an and-not", source: "func f(a, b int16) int16 { return a &^ b }"},
		{name: "an unsigned remainder", source: "func f(a, b uint32) uint32 { return a % b }"},
		{name: "a signed remainder", source: "func f(a, b int8) int8 { return a % b }"},
		{name: "an unsigned quotient", source: "func f(a, b uint16) uint16 { return a / b }"},
		{name: "an unsigned right shift", source: "func f(a uint32, n uint) uint32 { return a >> n }"},
		{name: "a signed right shift", source: "func f(a int8, n uint) int8 { return a >> n }"},
		{name: "a bitwise operation on a named narrow type", source: "type flags uint8\nfunc f(a, b flags) flags { return a | b }"},
		{name: "a compound exclusive or into a local", source: "func f(a, b uint32) uint32 { a ^= b; return a }"},
		{name: "a compound sum into a local can carry", source: "func f(a, b uint32) uint32 { a += b; return a }", truncates: true},
		{name: "a full-width sum", source: "func f(a, b uint64) uint64 { return a + b }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.Equal(t, tt.truncates, bodyHoldsANarrowing(compiled))
		})
	}
}

func TestIntegerConversionIsTruncatedOnlyWhenItNarrows(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		function  string
		truncates bool
	}{
		{name: "uint8 to uint32 widens", source: "func f(a uint8) uint32 { return uint32(a) }"},
		{name: "uint16 to uint32 widens", source: "func f(a uint16) uint32 { return uint32(a) }"},
		{name: "int8 to int32 widens", source: "func f(a int8) int32 { return int32(a) }"},
		{name: "a byte read from a slice widened", source: "func f(s []byte, i int) uint32 { return uint32(s[i]) }"},
		{name: "uint32 to a named uint32", source: "type id uint32\nfunc f(a uint32) id { return id(a) }"},
		{name: "uint32 to uint8 narrows", source: "func f(a uint32) uint8 { return uint8(a) }", truncates: true},
		{name: "uint64 to uint32 narrows", source: "func f(a uint64) uint32 { return uint32(a) }", truncates: true},
		{name: "int to int16 narrows", source: "func f(a int) int16 { return int16(a) }", truncates: true},
		{name: "int32 to int8 narrows", source: "func f(a int32) int8 { return int8(a) }", truncates: true},
		{name: "int8 to uint32 changes sign", source: "func f(a int8) uint32 { return uint32(a) }", truncates: true},
		{name: "uint8 to int8 changes sign", source: "func f(a uint8) int8 { return int8(a) }", truncates: true},
		{name: "int32 to uint32 changes sign", source: "func f(a int32) uint32 { return uint32(a) }", truncates: true},
		{
			name:      "an int to a type parameter given a narrow type",
			source:    "func f[T ~uint8](x int) T { return T(x) }\nfunc g() uint8 { return f[uint8](300) }",
			function:  "f[spec:uint8]",
			truncates: true,
		},
		{
			name:      "one type parameter to a narrower one",
			source:    "func f[T ~uint8, U ~uint16](x U) T { return T(x) }\nfunc g() uint8 { return f[uint8, uint16](300) }",
			function:  "f[spec:uint8,uint16]",
			truncates: true,
		},
		{
			name:     "one type parameter to a wider one",
			source:   "func f[T ~uint32, U ~uint16](x U) T { return T(x) }\nfunc g() uint32 { return f[uint32, uint16](300) }",
			function: "f[spec:uint32,uint16]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, cmp.Or(tt.function, "f"))

			require.Equal(t, tt.truncates, bodyHoldsANarrowing(compiled))
		})
	}
}

func TestASpecialisedGenericBodyNarrowsAsItsTypeArgumentDoes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		source    string
		truncates bool
	}{
		{name: "an increment can carry", source: "func f[T ~uint8](a, _ T) T { a++; return a }", truncates: true},
		{name: "a sum can carry", source: "func f[T ~uint8](a, b T) T { return a + b }", truncates: true},
		{name: "an or cannot leave the type", source: "func f[T ~uint8](a, b T) T { return a | b }"},
		{name: "a right shift cannot leave the type", source: "func f[T ~uint8](a, b T) T { return a >> b }"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+tt.source+"\nfunc g() uint8 { return f[uint8](200, 100) }\n")

			compiled := findCompiledFunction(t, root, "f[spec:uint8]")

			require.Equal(t, tt.truncates, bodyHoldsANarrowing(compiled))
		})
	}
}

func bodyHoldsANarrowing(compiledFunction *program.CompiledFunction) bool {
	for _, instruction := range compiledFunction.Body {
		switch instruction.Op {
		case isa.OpTruncateToWidth,
			isa.OpAddInt32, isa.OpSubtractInt32, isa.OpMultiplyInt32, isa.OpAddInt32Constant, isa.OpSubtractInt32Constant,
			isa.OpAddUint32, isa.OpSubtractUint32, isa.OpMultiplyUint32:
			return true
		default:
		}
		if isa.InstrIsTier1SubOp(instruction, isa.SubOpAddUint32Constant) || isa.InstrIsTier1SubOp(instruction, isa.SubOpSubtractUint32Constant) ||
			isa.InstrIsTier2SubOp(instruction, isa.SubOpTier2IncrementInt32) || isa.InstrIsTier2SubOp(instruction, isa.SubOpTier2DecrementInt32) {
			return true
		}
	}
	return false
}
