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
	"math"
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestFusedCombineBinaryTruncatesTheMaskedFamiliesToThirtyTwoBits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		shape program.FusedEvalShape
		left  uint64
		right uint64
		want  uint64
	}{
		{name: "addition of two small values", shape: program.FusedEvalAddMasked, left: 10, right: 32, want: 42},
		{name: "addition discards bits above thirty-two", shape: program.FusedEvalAddMasked, left: 1 << 40, right: 42, want: 42},
		{name: "addition wraps at the thirty-two-bit ceiling", shape: program.FusedEvalAddMasked, left: math.MaxUint32, right: 1, want: 0},
		{name: "subtraction of two small values", shape: program.FusedEvalSubMasked, left: 50, right: 8, want: 42},
		{name: "subtraction wraps below zero into the thirty-two-bit range", shape: program.FusedEvalSubMasked, left: 0, right: 1, want: math.MaxUint32},
		{name: "multiplication of two small values", shape: program.FusedEvalMulMasked, left: 6, right: 7, want: 42},
		{name: "multiplication wraps at the thirty-two-bit ceiling", shape: program.FusedEvalMulMasked, left: 1 << 16, right: 1 << 16, want: 0},
		{name: "minimum selects the smaller operand at full width", shape: program.FusedEvalMin, left: 1 << 40, right: 42, want: 42},
		{name: "minimum of equal operands is that operand", shape: program.FusedEvalMin, left: 7, right: 7, want: 7},
		{name: "maximum selects the larger operand at full width", shape: program.FusedEvalMax, left: 1 << 40, right: 42, want: 1 << 40},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, fusedCombineBinary(tt.shape, tt.left, tt.right))
		})
	}
}

func TestFusedGuardedRemainderDividesWholeValuesAndTruncatesOnlyTheResult(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		shape    program.FusedEvalShape
		dividend uint64
		divisor  uint64
		want     uint64
	}{
		{name: "small values", shape: program.FusedEvalModGuarded, dividend: 47, divisor: 5, want: 2},
		{name: "a divisor above 32 bits is not cut down first", shape: program.FusedEvalModGuarded, dividend: 7, divisor: 1<<32 | 2, want: 7},
		{name: "a divisor of exactly 2 to the 32 is not zero", shape: program.FusedEvalModGuarded, dividend: 1<<33 | 5, divisor: 1 << 32, want: 5},
		{name: "a wide remainder keeps its low 32 bits", shape: program.FusedEvalModGuarded, dividend: 1<<32 | 7, divisor: 3 << 32, want: 7},
		{name: "the whole form keeps a wide remainder", shape: program.FusedEvalModGuardedWhole, dividend: 1<<32 | 7, divisor: 3 << 32, want: 1<<32 | 7},
		{name: "the whole form with a wide divisor", shape: program.FusedEvalModGuardedWhole, dividend: 1 << 40, divisor: 1<<32 | 1, want: (1 << 40) % (1<<32 | 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, fusedGuardedRemainder(tt.shape, tt.dividend, tt.divisor))
		})
	}
}

func TestFusedChildEfaceTakesOnlyAPointerChild(t *testing.T) {
	t.Parallel()

	type leaf struct{ value uint64 }
	type holder struct{ child any }
	layout := program.StructFieldLayout{Offset: uint32(unsafe.Offsetof(holder{}.child))}
	pointed := &leaf{value: 9}

	tests := []struct {
		child any
		name  string
		want  bool
	}{
		{name: "a pointer", child: pointed, want: true},
		{name: "nothing", child: nil},
		{name: "a channel, whose element type is not its own type", child: make(chan leaf)},
		{name: "a map, whose key type is not its own type", child: map[leaf]bool{}},
		{name: "a function", child: func() {}},
		{name: "an unsafe pointer", child: unsafe.Pointer(pointed)},
		{name: "a struct held by value", child: leaf{value: 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			held := holder{child: tt.child}

			typeWord, data, ok := fusedChildEface(unsafe.Pointer(&held), layout)

			require.Equal(t, tt.want, ok)
			if tt.want {
				require.Equal(t, unsafe.Pointer(pointed), data)
				require.NotZero(t, typeWord)
			}
		})
	}
}

func TestMask32KeepsOnlyTheLowWord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value uint64
		want  uint32
	}{
		{name: "zero stays zero", value: 0, want: 0},
		{name: "a small value is unchanged", value: 42, want: 42},
		{name: "the thirty-two-bit ceiling is unchanged", value: math.MaxUint32, want: math.MaxUint32},
		{name: "a value one past the ceiling wraps to zero", value: 1 << 32, want: 0},
		{name: "the high word is discarded", value: 0xDEADBEEF_CAFEBABE, want: 0xCAFEBABE},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, mask32(tt.value))
		})
	}
}

func TestFusedEnvStrideAcceptsOnlyUnsignedWordElements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		elementType   reflect.Type
		name          string
		wantWide      bool
		wantSupported bool
	}{
		{name: "a uint64 element is a wide stride", elementType: reflect.TypeFor[uint64](), wantWide: true, wantSupported: true},
		{name: "a uintptr element is a wide stride", elementType: reflect.TypeFor[uintptr](), wantWide: true, wantSupported: true},
		{name: "a uint32 element is a narrow stride", elementType: reflect.TypeFor[uint32](), wantWide: false, wantSupported: true},
		{name: "a uint16 element is unsupported", elementType: reflect.TypeFor[uint16](), wantWide: false, wantSupported: false},
		{name: "a uint8 element is unsupported", elementType: reflect.TypeFor[uint8](), wantWide: false, wantSupported: false},
		{name: "a signed element is unsupported", elementType: reflect.TypeFor[int64](), wantWide: false, wantSupported: false},
		{name: "a float element is unsupported", elementType: reflect.TypeFor[float64](), wantWide: false, wantSupported: false},
		{name: "a string element is unsupported", elementType: reflect.TypeFor[string](), wantWide: false, wantSupported: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			wide, supported := fusedEnvStrideForElement(tt.elementType)
			require.Equal(t, tt.wantSupported, supported)
			require.Equal(t, tt.wantWide, wide)
		})
	}
}

func TestFusedEnvReadsWithinItsStride(t *testing.T) {
	t.Parallel()

	wide := []uint64{10, 20, 30}
	narrow := []uint32{10, 20, 30}

	tests := []struct {
		name  string
		env   fusedEnv
		index int64
		want  uint64
		found bool
	}{
		{
			name: "a wide element", env: fusedEnv{data: unsafe.Pointer(&wide[0]), length: len(wide), wide: true},
			index: 1, want: 20, found: true,
		},
		{
			name: "a narrow element", env: fusedEnv{data: unsafe.Pointer(&narrow[0]), length: len(narrow)},
			index: 2, want: 30, found: true,
		},
		{
			name: "an index past the end", env: fusedEnv{data: unsafe.Pointer(&wide[0]), length: len(wide), wide: true},
			index: 9,
		},
		{
			name: "a negative index", env: fusedEnv{data: unsafe.Pointer(&wide[0]), length: len(wide), wide: true},
			index: -1,
		},
		{name: "an empty environment", env: fusedEnv{}, index: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			value, ok := tt.env.at(tt.index)
			require.Equal(t, tt.found, ok)
			if tt.found {
				require.Equal(t, tt.want, value)
			}
		})
	}
}

func TestFusedResolveChildCalleeReadsTheSiteCache(t *testing.T) {
	t.Parallel()

	callee := program.NewNamedFunction("Eval")
	receiverType := reflect.TypeFor[int]()
	typeWord := uintptr(reflectValueABIType(receiverType))

	t.Run("an empty cache resolves to nothing", func(t *testing.T) {
		t.Parallel()

		require.Nil(t, fusedResolveChildCallee(&program.CallSite{}, typeWord))
	})

	t.Run("a zero type word never matches", func(t *testing.T) {
		t.Parallel()

		site := &program.CallSite{}
		updateMethodIC(site, receiverType, 0, callee, false)

		require.Nil(t, fusedResolveChildCallee(site, 0))
	})

	t.Run("a cached slot promotes itself to the short path", func(t *testing.T) {
		t.Parallel()

		site := &program.CallSite{}
		updateMethodIC(site, receiverType, 0, callee, false)

		require.Same(t, callee, fusedResolveChildCallee(site, typeWord))
		require.Equal(t, typeWord, site.LastReceiverTypeWord)
		require.Same(t, callee, fusedResolveChildCallee(site, typeWord), "the second resolve takes the short path")
	})

	t.Run("an unseen type word misses", func(t *testing.T) {
		t.Parallel()

		site := &program.CallSite{}
		updateMethodIC(site, receiverType, 0, callee, false)

		require.Nil(t, fusedResolveChildCallee(site, uintptr(reflectValueABIType(reflect.TypeFor[string]()))))
	})
}

func TestRunFusedEvalShapeRefusesShapesItCannotDrive(t *testing.T) {
	t.Parallel()

	callee := program.NewNamedFunction("Eval")

	tests := []struct {
		name string
		site *program.CallSite
		load func(*Registers)
	}{
		{name: "a site with too few arguments", site: &program.CallSite{Returns: []program.VarLocation{{}}}, load: func(*Registers) {}},
		{
			name: "a site with no return slot",
			site: &program.CallSite{Arguments: []program.VarLocation{{}, {}}}, load: func(*Registers) {},
		},
		{
			name: "an unset receiver register",
			site: &program.CallSite{Arguments: []program.VarLocation{{}, {}}, Returns: []program.VarLocation{{}}},
			load: func(*Registers) {},
		},
		{
			name: "a receiver that is not a pointer",
			site: &program.CallSite{Arguments: []program.VarLocation{{}, {}}, Returns: []program.VarLocation{{}}},
			load: func(registers *Registers) { registers.General[0] = reflect.ValueOf(3) },
		},
		{
			name: "an environment that is not a slice",
			site: &program.CallSite{
				Arguments: []program.VarLocation{{}, {Kind: isa.RegisterGeneral, Register: 1}},
				Returns:   []program.VarLocation{{}},
			},
			load: func(registers *Registers) {
				value := 3
				registers.General[0] = reflect.ValueOf(&value)
				registers.General[1] = reflect.ValueOf(4)
			},
		},
		{
			name: "an environment element the stride does not support",
			site: &program.CallSite{
				Arguments: []program.VarLocation{{}, {Kind: isa.RegisterGeneral, Register: 1}},
				Returns:   []program.VarLocation{{}},
			},
			load: func(registers *Registers) {
				value := 3
				registers.General[0] = reflect.ValueOf(&value)
				registers.General[1] = reflect.ValueOf([]string{"a"})
			},
		},
		{
			name: "an environment in a bank the evaluator does not read",
			site: &program.CallSite{
				Arguments: []program.VarLocation{{}, {Kind: isa.RegisterSliceByte, Register: 1}},
				Returns:   []program.VarLocation{{}},
			},
			load: func(registers *Registers) {
				value := 3
				registers.General[0] = reflect.ValueOf(&value)
				registers.General[1] = reflect.ValueOf([]uint32{1, 2})
			},
		},
		{
			name: "a callee with no recognised shape",
			site: &program.CallSite{
				Arguments: []program.VarLocation{{}, {Kind: isa.RegisterSliceUint64, Register: 1}},
				Returns:   []program.VarLocation{{}},
			},
			load: func(registers *Registers) {
				value := 3
				registers.General[0] = reflect.ValueOf(&value)
				registers.slicesUint[1] = []uint64{1, 2}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			registers := standardRegisters()
			tt.load(&registers)

			got, ran := runFusedEvalShape(&registers, tt.site, callee)
			require.Equal(t, opContinue, got)
			require.False(t, ran, "the framed path has to take over whenever the fused one cannot")
		})
	}
}

func TestFusedEvalNodeRefusesToRecurseWithoutBounds(t *testing.T) {
	t.Parallel()

	callee := program.NewNamedFunction("Eval")
	value := uint64(3)
	env := fusedEnv{data: unsafe.Pointer(&value), length: 1, wide: true}

	t.Run("a nil receiver is refused", func(t *testing.T) {
		t.Parallel()

		_, ok := fusedEvalNode(callee, nil, &env, 0)
		require.False(t, ok)
	})

	t.Run("a depth past the ceiling is refused", func(t *testing.T) {
		t.Parallel()

		_, ok := fusedEvalNode(callee, unsafe.Pointer(&value), &env, maxFusedEvalDepth+1)
		require.False(t, ok, "a cyclic expression tree must not recurse without bound")
	})

	t.Run("a callee with no recognised shape is refused", func(t *testing.T) {
		t.Parallel()

		_, ok := fusedEvalNode(callee, unsafe.Pointer(&value), &env, 0)
		require.False(t, ok)
	})
}
