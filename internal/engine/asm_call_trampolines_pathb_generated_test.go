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

//go:build !safe && !(js && wasm) && (amd64 || arm64)

package engine

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

type pathBTrampolineFunc func(*dispatchContext, uint32) *dispatchContext

func instructionWord(a, b, c uint8) uint32 {
	word := isa.Instruction{A: a, B: b, C: c}
	return *(*uint32)(unsafe.Pointer(&word))
}

func runPathB(t *testing.T, trampoline pathBTrampolineFunc, a, b, c uint8, load func(*Registers)) *Registers {
	t.Helper()

	vm, _, registers := newStandardVM(t)
	load(registers)
	context := &dispatchContext{}
	vm.buildDispatchContext(context, nil)

	require.Same(t, context, trampoline(context, instructionWord(a, b, c)),
		"the context is handed back so the assembly caller can reload it")
	return registers
}

func TestPathBComplexTrampolinesDriveTheComplexBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		trampoline pathBTrampolineFunc
		want       complex128
	}{
		{name: "addition", trampoline: asmCallHandleAddComplex128, want: 4 + 6i},
		{name: "subtraction", trampoline: asmCallHandleSubtractComplex128, want: -2 - 2i},
		{name: "multiplication", trampoline: asmCallHandleMultiplyComplex128, want: (1 + 2i) * (3 + 4i)},
		{name: "division", trampoline: asmCallHandleDivideComplex128, want: (1 + 2i) / (3 + 4i)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := runPathB(t, tt.trampoline, 0, 1, 2, func(r *Registers) {
				r.Complex[1] = 1 + 2i
				r.Complex[2] = 3 + 4i
			})
			require.Equal(t, tt.want, registers.Complex[0])
		})
	}
}

func TestPathBComplexComparisonsWriteTheIntBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		trampoline pathBTrampolineFunc
		right      complex128
		want       int64
	}{
		{name: "equality holds", trampoline: asmCallHandleEqualComplex128, right: 1 + 2i, want: 1},
		{name: "equality fails", trampoline: asmCallHandleEqualComplex128, right: 9i, want: 0},
		{name: "inequality holds", trampoline: asmCallHandleNotEqualComplex128, right: 9i, want: 1},
		{name: "inequality fails", trampoline: asmCallHandleNotEqualComplex128, right: 1 + 2i, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := runPathB(t, tt.trampoline, 0, 1, 2, func(r *Registers) {
				r.Complex[1] = 1 + 2i
				r.Complex[2] = tt.right
			})
			require.Equal(t, tt.want, registers.Ints[0])
		})
	}
}

func TestPathBMakeComplex128PairsTheFloat64Bank(t *testing.T) {
	t.Parallel()

	registers := runPathB(t, asmCallHandleMakeComplex128, 0, 1, 2, func(r *Registers) {
		r.Floats[1] = 1.5
		r.Floats[2] = 2.5
	})
	require.Equal(t, complex(1.5, 2.5), registers.Complex[0])
}

func TestPathBGeneralMoveCopiesTheRegister(t *testing.T) {
	t.Parallel()

	registers := runPathB(t, asmCallHandleMoveGeneral, 0, 1, 0, func(r *Registers) {
		r.General[1] = reflect.ValueOf("moved")
	})
	require.Equal(t, "moved", registers.General[0].Interface())
}

func TestPathBGeneralComparisonsOrderTheirOperands(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		trampoline pathBTrampolineFunc
		left       any
		right      any
		want       int64
	}{
		{name: "equality on equal values", trampoline: asmCallHandleEqualGeneral, left: 3, right: 3, want: 1},
		{name: "equality on differing values", trampoline: asmCallHandleEqualGeneral, left: 3, right: 4, want: 0},
		{name: "inequality on differing values", trampoline: asmCallHandleNotEqualGeneral, left: 3, right: 4, want: 1},
		{name: "less than holds", trampoline: asmCallHandleLessThanGeneral, left: 3, right: 4, want: 1},
		{name: "less than fails", trampoline: asmCallHandleLessThanGeneral, left: 4, right: 3, want: 0},
		{name: "greater than holds", trampoline: asmCallHandleGreaterThanGeneral, left: 4, right: 3, want: 1},
		{name: "less or equal holds on equality", trampoline: asmCallHandleLessOrEqualGeneral, left: 3, right: 3, want: 1},
		{name: "greater or equal holds on equality", trampoline: asmCallHandleGreaterOrEqualGeneral, left: 3, right: 3, want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := runPathB(t, tt.trampoline, 0, 1, 2, func(r *Registers) {
				r.General[1] = reflect.ValueOf(tt.left)
				r.General[2] = reflect.ValueOf(tt.right)
			})
			require.Equal(t, tt.want, registers.Ints[0])
		})
	}
}

func TestPathBInterfaceNilChecksReadTheGeneralBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		trampoline pathBTrampolineFunc
		value      reflect.Value
		want       int64
	}{
		{name: "an unset register is nil", trampoline: asmCallHandleEqualInterfaceNil, value: reflect.Value{}, want: 1},
		{name: "a live value is not nil", trampoline: asmCallHandleEqualInterfaceNil, value: reflect.ValueOf(3), want: 0},
		{name: "the inverse on an unset register", trampoline: asmCallHandleNotEqualInterfaceNil, value: reflect.Value{}, want: 0},
		{name: "the inverse on a live value", trampoline: asmCallHandleNotEqualInterfaceNil, value: reflect.ValueOf(3), want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := runPathB(t, tt.trampoline, 0, 1, 2, func(r *Registers) {
				r.General[2] = tt.value
			})
			require.Equal(t, tt.want, registers.Ints[1])
		})
	}
}

func TestPathBStringTrampolinesCoverTheIntrinsics(t *testing.T) {
	t.Parallel()

	t.Run("predicates write the bool bank", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			trampoline pathBTrampolineFunc
			haystack   string
			needle     string
			want       bool
		}{
			{name: "contains a substring", trampoline: asmCallHandleStringsContains, haystack: "hello", needle: "ell", want: true},
			{name: "does not contain", trampoline: asmCallHandleStringsContains, haystack: "hello", needle: "zzz", want: false},
			{name: "has the prefix", trampoline: asmCallHandleStringsHasPrefix, haystack: "hello", needle: "he", want: true},
			{name: "has the suffix", trampoline: asmCallHandleStringsHasSuffix, haystack: "hello", needle: "lo", want: true},
			{name: "folds to equal", trampoline: asmCallHandleStringsEqualFold, haystack: "Hello", needle: "hELLO", want: true},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				registers := runPathB(t, tt.trampoline, 0, 1, 2, func(r *Registers) {
					r.Strings[1] = tt.haystack
					r.Strings[2] = tt.needle
				})
				require.Equal(t, tt.want, registers.Bools[0])
			})
		}
	})

	t.Run("searches write the int bank", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			trampoline pathBTrampolineFunc
			haystack   string
			needle     string
			want       int64
		}{
			{name: "the first occurrence", trampoline: asmCallHandleStringsIndex, haystack: "abcabc", needle: "b", want: 1},
			{name: "the last occurrence", trampoline: asmCallHandleStringsLastIndex, haystack: "abcabc", needle: "b", want: 4},
			{name: "an absent needle", trampoline: asmCallHandleStringsIndex, haystack: "abc", needle: "z", want: -1},
			{name: "the occurrence count", trampoline: asmCallHandleStringsCount, haystack: "abcabc", needle: "b", want: 2},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				registers := runPathB(t, tt.trampoline, 0, 1, 2, func(r *Registers) {
					r.Strings[1] = tt.haystack
					r.Strings[2] = tt.needle
				})
				require.Equal(t, tt.want, registers.Ints[0])
			})
		}
	})

	t.Run("trims and concatenations write the string bank", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			trampoline pathBTrampolineFunc
			source     string
			operand    string
			want       string
		}{
			{name: "trimming a cutset", trampoline: asmCallHandleStringsTrim, source: "xxhixx", operand: "x", want: "hi"},
			{name: "trimming a prefix", trampoline: asmCallHandleStringsTrimPrefix, source: "prefix-body", operand: "prefix-", want: "body"},
			{name: "trimming a suffix", trampoline: asmCallHandleStringsTrimSuffix, source: "body-suffix", operand: "-suffix", want: "body"},
			{name: "concatenation", trampoline: asmCallHandleConcatenateString, source: "ab", operand: "cd", want: "abcd"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				registers := runPathB(t, tt.trampoline, 0, 1, 2, func(r *Registers) {
					r.Strings[1] = tt.source
					r.Strings[2] = tt.operand
				})
				require.Equal(t, tt.want, registers.Strings[0])
			})
		}
	})

	t.Run("a rune operand is read from the int bank", func(t *testing.T) {
		t.Parallel()

		registers := runPathB(t, asmCallHandleStringsContainsRune, 0, 1, 2, func(r *Registers) {
			r.Strings[1] = "hello"
			r.Ints[2] = 'e'
		})
		require.True(t, registers.Bools[0])

		registers = runPathB(t, asmCallHandleStringsIndexRune, 0, 1, 2, func(r *Registers) {
			r.Strings[1] = "hello"
			r.Ints[2] = 'l'
		})
		require.Equal(t, int64(2), registers.Ints[0])

		registers = runPathB(t, asmCallHandleConcatenateStringRune, 0, 1, 2, func(r *Registers) {
			r.Strings[1] = "ab"
			r.Ints[2] = 'c'
		})
		require.Equal(t, "abc", registers.Strings[0])
	})

	t.Run("repeat reads its count from the int bank", func(t *testing.T) {
		t.Parallel()

		registers := runPathB(t, asmCallHandleStringsRepeat, 0, 1, 2, func(r *Registers) {
			r.Strings[1] = "ab"
			r.Ints[2] = 3
		})
		require.Equal(t, "ababab", registers.Strings[0])
	})

	t.Run("split builds a slice in the general bank", func(t *testing.T) {
		t.Parallel()

		registers := runPathB(t, asmCallHandleStringsSplit, 0, 1, 2, func(r *Registers) {
			r.Strings[1] = "a,b,c"
			r.Strings[2] = ","
		})
		require.Equal(t, []string{"a", "b", "c"}, registers.General[0].Interface())
	})

	t.Run("join reads a slice from the general bank", func(t *testing.T) {
		t.Parallel()

		registers := runPathB(t, asmCallHandleStringsJoin, 0, 1, 2, func(r *Registers) {
			r.General[1] = reflect.ValueOf([]string{"a", "b"})
			r.Strings[2] = "-"
		})
		require.Equal(t, "a-b", registers.Strings[0])
	})
}

func TestPathBMathPowReadsTheFloatBank(t *testing.T) {
	t.Parallel()

	registers := runPathB(t, asmCallHandleMathPow, 0, 1, 2, func(r *Registers) {
		r.Floats[1] = 2
		r.Floats[2] = 10
	})
	require.InDelta(t, 1024, registers.Floats[0], 0)
}

func TestPathBTypedSliceAccessReadsAndWritesElements(t *testing.T) {
	t.Parallel()

	t.Run("reads land in their own bank", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name       string
			trampoline pathBTrampolineFunc
			collection any
			inspect    func(*testing.T, *Registers)
		}{
			{
				name: "an int element", trampoline: asmCallHandleGetElementInt64, collection: []int{4, 5},
				inspect: func(t *testing.T, r *Registers) {
					t.Helper()
					require.Equal(t, int64(5), r.Ints[0])
				},
			},
			{
				name: "a float element", trampoline: asmCallHandleGetElementFloat64, collection: []float64{1.5, 2.5},
				inspect: func(t *testing.T, r *Registers) {
					t.Helper()
					require.InDelta(t, 2.5, r.Floats[0], 0)
				},
			},
			{
				name: "a string element", trampoline: asmCallHandleGetElementString, collection: []string{"a", "b"},
				inspect: func(t *testing.T, r *Registers) {
					t.Helper()
					require.Equal(t, "b", r.Strings[0])
				},
			},
			{
				name: "a bool element", trampoline: asmCallHandleGetElementBool, collection: []bool{false, true},
				inspect: func(t *testing.T, r *Registers) {
					t.Helper()
					require.True(t, r.Bools[0])
				},
			},
			{
				name: "a uint element", trampoline: asmCallHandleGetElementUint64, collection: []uint64{4, 5},
				inspect: func(t *testing.T, r *Registers) {
					t.Helper()
					require.Equal(t, uint64(5), r.Uints[0])
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				t.Parallel()

				registers := runPathB(t, tt.trampoline, 0, 1, 2, func(r *Registers) {
					r.General[1] = reflect.ValueOf(tt.collection)
					r.Ints[2] = 1
				})
				tt.inspect(t, registers)
			})
		}
	})

	t.Run("writes reach the caller's slice", func(t *testing.T) {
		t.Parallel()

		collection := []int{4, 5}
		runPathB(t, asmCallHandleSetElementInt64, 0, 1, 2, func(r *Registers) {
			r.General[0] = reflect.ValueOf(collection)
			r.Ints[1] = 1
			r.Ints[2] = 9
		})
		require.Equal(t, []int{4, 9}, collection)
	})
}

func TestPathBIndexTrampolinesReachTheGeneralBank(t *testing.T) {
	t.Parallel()

	t.Run("a read boxes the element", func(t *testing.T) {
		t.Parallel()

		registers := runPathB(t, asmCallHandleGetElement, 0, 1, 2, func(r *Registers) {
			r.General[1] = reflect.ValueOf([]string{"a", "b"})
			r.Ints[2] = 1
		})
		require.Equal(t, "b", registers.General[0].Interface())
	})

	t.Run("a write reaches the caller's slice", func(t *testing.T) {
		t.Parallel()

		collection := []string{"a", "b"}
		runPathB(t, asmCallHandleSetElement, 0, 1, 2, func(r *Registers) {
			r.General[0] = reflect.ValueOf(collection)
			r.Ints[1] = 0
			r.General[2] = reflect.ValueOf("z")
		})
		require.Equal(t, []string{"z", "b"}, collection)
	})

	t.Run("a copy reports the element count", func(t *testing.T) {
		t.Parallel()

		destination := []int{0, 0, 0}
		registers := runPathB(t, asmCallHandleCopyGeneral, 0, 1, 2, func(r *Registers) {
			r.General[1] = reflect.ValueOf(destination)
			r.General[2] = reflect.ValueOf([]int{7, 8})
		})
		require.Equal(t, int64(2), registers.Ints[0])
		require.Equal(t, []int{7, 8, 0}, destination)
	})
}

func TestPathBBoxAsInterfaceBoxesTheNamedBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind isa.RegisterKind
		load func(*Registers)
		want any
	}{
		{name: "the int bank", kind: isa.RegisterInt64, load: func(r *Registers) { r.Ints[1] = 4 }, want: 4},
		{name: "the float bank", kind: isa.RegisterFloat64, load: func(r *Registers) { r.Floats[1] = 1.5 }, want: 1.5},
		{name: "the string bank", kind: isa.RegisterString, load: func(r *Registers) { r.Strings[1] = "s" }, want: "s"},
		{name: "the bool bank", kind: isa.RegisterBool, load: func(r *Registers) { r.Bools[1] = true }, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := runPathB(t, asmCallHandleBoxAsInterface, 0, 1, uint8(tt.kind), tt.load)
			require.Equal(t, tt.want, registers.General[0].Interface())
		})
	}
}
