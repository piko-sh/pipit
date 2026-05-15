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

func fusedChild(layoutIndex uint8, siteIndex uint8) []isa.Instruction {
	return []isa.Instruction{
		{Op: isa.OpGetStructFieldGeneral, A: 0, B: 0, C: layoutIndex},
		isa.NewTier1Instruction(isa.SubOpCallMethodInlineable, siteIndex, 0),
		isa.NewInstruction(isa.OpExt, 0, 0, 0),
	}
}

func fusedCandidate(body []isa.Instruction) *program.CompiledFunction {
	compiledFunction := program.NewNamedFunction("fused")
	compiledFunction.Body = body
	compiledFunction.StructLayoutTable = []program.StructFieldLayout{
		{Offset: 8, PathLength: 1},
		{Offset: 16, PathLength: 1},
		{Offset: 24, PathLength: 1},
	}
	compiledFunction.CallSites = []program.CallSite{{}, {}, {}}
	return compiledFunction
}

func maskedBinaryBody(arithmetic isa.Opcode) []isa.Instruction {
	body := append(fusedChild(0, 0), fusedChild(1, 1)...)
	return append(body,
		isa.Instruction{Op: arithmetic, A: 0, B: 1, C: 2},
		isa.Instruction{Op: isa.OpTruncateNarrow, A: 0, B: 32, C: uint8(isa.RegisterUint)},
		isa.NewTier1Instruction(isa.SubOpMoveUint, 0, 0),
		tier2Return(1),
	)
}

func minMaxBody(comparison isa.Opcode) []isa.Instruction {
	body := append(fusedChild(0, 0), fusedChild(1, 1)...)
	return append(body,
		isa.Instruction{Op: comparison, A: 0, B: 1, C: 2},
		isa.Instruction{Op: isa.OpJumpIfFalse, A: 0, B: 0, C: 0},
		tier2Return(1),
		isa.Instruction{Op: isa.OpAddInt, A: 0, B: 0, C: 0},
		isa.Instruction{Op: isa.OpAddInt, A: 0, B: 0, C: 0},
	)
}

func modGuardedBody() []isa.Instruction {
	body := fusedChild(0, 0)
	body = append(body,
		isa.NewTier1Instruction(isa.SubOpEqUintConstJumpFalse, 0, 0),
		isa.NewInstruction(isa.OpExt, 0, 0, 0),
		isa.Instruction{Op: isa.OpAddInt, A: 0, B: 0, C: 0},
		isa.Instruction{Op: isa.OpAddInt, A: 0, B: 0, C: 0},
		isa.Instruction{Op: isa.OpAddInt, A: 0, B: 0, C: 0},
	)
	body = append(body, fusedChild(1, 1)...)
	return append(body,
		isa.Instruction{Op: isa.OpRemUint, A: 0, B: 1, C: 2},
		isa.Instruction{Op: isa.OpAddInt, A: 0, B: 0, C: 0},
		tier2Return(1),
	)
}

func ifPosBody() []isa.Instruction {
	body := fusedChild(0, 0)
	body = append(body,
		isa.Instruction{Op: isa.OpAddInt, A: 0, B: 0, C: 0},
		isa.Instruction{Op: isa.OpNeUint, A: 0, B: 1, C: 2},
		isa.Instruction{Op: isa.OpJumpIfFalse, A: 0, B: 0, C: 0},
	)
	body = append(body, fusedChild(1, 1)...)
	body = append(body, isa.Instruction{Op: isa.OpAddInt, A: 0, B: 0, C: 0})
	return append(body, fusedChild(2, 2)...)
}

func TestClassifyFusedEvalShapeRecognisesEachFamily(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body []isa.Instruction
		want program.FusedEvalShape
	}{
		{name: "masked addition", body: maskedBinaryBody(isa.OpAddUint), want: program.FusedEvalAddMasked},
		{name: "masked subtraction", body: maskedBinaryBody(isa.OpSubUint), want: program.FusedEvalSubMasked},
		{name: "masked multiplication", body: maskedBinaryBody(isa.OpMulUint), want: program.FusedEvalMulMasked},
		{name: "a minimum selection", body: minMaxBody(isa.OpLtUint), want: program.FusedEvalMin},
		{name: "a maximum selection", body: minMaxBody(isa.OpGtUint), want: program.FusedEvalMax},
		{name: "a guarded modulo", body: modGuardedBody(), want: program.FusedEvalModGuarded},
		{name: "a three-way conditional", body: ifPosBody(), want: program.FusedEvalIfPos},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := fusedCandidate(tt.body)

			classifyFusedEvalShapeOnce(compiledFunction)

			require.Equal(t, tt.want, compiledFunction.EvalShape)
			require.Equal(t, uint32(8), compiledFunction.EvalChildALayout.Offset, "the first child layout must be published")
			require.Equal(t, uint32(16), compiledFunction.EvalChildBLayout.Offset, "the second child layout must be published")
		})
	}
}

func TestClassifyFusedEvalShapeIsGatedOnTheExactBodyLength(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body []isa.Instruction
	}{
		{name: "an empty body", body: nil},
		{name: "one instruction short of the masked family", body: maskedBinaryBody(isa.OpAddUint)[:9]},
		{name: "one instruction past the masked family", body: append(maskedBinaryBody(isa.OpAddUint), isa.Instruction{Op: isa.OpAddInt})},
		{name: "one instruction short of the conditional family", body: ifPosBody()[:12]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := fusedCandidate(tt.body)

			classifyFusedEvalShapeOnce(compiledFunction)

			require.Equal(t, program.FusedEvalNone, compiledFunction.EvalShape,
				"classification is keyed on the exact effective body length")
		})
	}
}

func TestClassifyFusedEvalShapeRefusesAMalformedMaskedBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mutate func(body []isa.Instruction)
		name   string
	}{
		{
			name:   "an arithmetic opcode outside the masked family",
			mutate: func(body []isa.Instruction) { body[6] = isa.Instruction{Op: isa.OpDivUint, A: 0, B: 1, C: 2} },
		},
		{
			name:   "a truncation to a width other than thirty-two",
			mutate: func(body []isa.Instruction) { body[7].B = 16 },
		},
		{
			name:   "a missing truncation",
			mutate: func(body []isa.Instruction) { body[7] = isa.Instruction{Op: isa.OpAddInt} },
		},
		{
			name:   "a move from a bank other than the unsigned one",
			mutate: func(body []isa.Instruction) { body[8] = isa.NewTier1Instruction(isa.SubOpNegInt, 0, 0) },
		},
		{
			name:   "a body that does not end in a return",
			mutate: func(body []isa.Instruction) { body[9] = isa.Instruction{Op: isa.OpAddInt} },
		},
		{
			name:   "a child fetch whose field read targets a non-zero receiver",
			mutate: func(body []isa.Instruction) { body[0].B = 1 },
		},
		{
			name:   "a child fetch whose call is not inlineable",
			mutate: func(body []isa.Instruction) { body[1] = isa.NewTier1Instruction(isa.SubOpCallMethod, 0, 0) },
		},
		{
			name:   "a child fetch missing its extension word",
			mutate: func(body []isa.Instruction) { body[2] = isa.Instruction{Op: isa.OpAddInt} },
		},
		{
			name:   "a layout index past the table",
			mutate: func(body []isa.Instruction) { body[0].C = 9 },
		},
		{
			name:   "a call-site index past the table",
			mutate: func(body []isa.Instruction) { body[1] = isa.NewTier1Instruction(isa.SubOpCallMethodInlineable, 9, 0) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := maskedBinaryBody(isa.OpAddUint)
			tt.mutate(body)
			compiledFunction := fusedCandidate(body)

			classifyFusedEvalShapeOnce(compiledFunction)

			require.Equal(t, program.FusedEvalNone, compiledFunction.EvalShape,
				"a single mismatched word must abandon the fused shape")
		})
	}
}

func TestClassifyFusedEvalShapeIgnoresPaddingWhenMeasuringTheBody(t *testing.T) {
	t.Parallel()

	body := maskedBinaryBody(isa.OpAddUint)
	padded := make([]isa.Instruction, 0, len(body)+3)
	padded = append(padded, nop())
	padded = append(padded, body...)
	padded = append(padded, nop(), nop())
	compiledFunction := fusedCandidate(padded)

	classifyFusedEvalShapeOnce(compiledFunction)

	require.Equal(t, program.FusedEvalAddMasked, compiledFunction.EvalShape,
		"padding must not change the effective body length")
}

func TestClassifyFusedEvalShapeRunsOnlyOncePerFunction(t *testing.T) {
	t.Parallel()

	compiledFunction := fusedCandidate(maskedBinaryBody(isa.OpAddUint))

	classifyFusedEvalShape(compiledFunction)
	require.Equal(t, program.FusedEvalAddMasked, compiledFunction.EvalShape)

	compiledFunction.Body = minMaxBody(isa.OpLtUint)
	classifyFusedEvalShape(compiledFunction)

	require.Equal(t, program.FusedEvalAddMasked, compiledFunction.EvalShape,
		"the once-guard must keep the first classification")
}

func TestEffectiveOpsRemovesPaddingAndKeepsOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body []isa.Instruction
		want []isa.Instruction
	}{
		{name: "an empty body stays empty", body: nil, want: []isa.Instruction{}},
		{name: "a body of only padding empties", body: []isa.Instruction{nop(), nop()}, want: []isa.Instruction{}},
		{
			name: "padding is removed from between real instructions",
			body: []isa.Instruction{nop(), {Op: isa.OpAddInt}, nop(), {Op: isa.OpSubInt}, nop()},
			want: []isa.Instruction{{Op: isa.OpAddInt}, {Op: isa.OpSubInt}},
		},
		{
			name: "a tier-one word carrying operands is kept",
			body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpNegInt, 0, 1)},
			want: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpNegInt, 0, 1)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, effectiveOps(tt.body))
		})
	}
}

func TestFusedChildFetchBoundsChecksBothTables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		at   int
		body []isa.Instruction
		want bool
	}{
		{name: "a well-formed triple matches", at: 0, body: fusedChild(0, 0), want: true},
		{name: "a triple that runs past the body is refused", at: 1, body: fusedChild(0, 0), want: false},
		{name: "an offset past the body is refused", at: 9, body: fusedChild(0, 0), want: false},
		{name: "an empty body is refused", at: 0, body: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := fusedCandidate(tt.body)

			_, _, ok := fusedChildFetch(compiledFunction, tt.body, tt.at)

			require.Equal(t, tt.want, ok)
		})
	}

	t.Run("a matched triple returns the layout and site it named", func(t *testing.T) {
		t.Parallel()
		compiledFunction := fusedCandidate(fusedChild(2, 1))

		layout, site, ok := fusedChildFetch(compiledFunction, compiledFunction.Body, 0)

		require.True(t, ok)
		require.Equal(t, uint32(24), layout.Offset)
		require.Equal(t, uint16(1), site)
	})
}

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
			load: func(r *Registers) { r.General[0] = reflect.ValueOf(3) },
		},
		{
			name: "an environment that is not a slice",
			site: &program.CallSite{
				Arguments: []program.VarLocation{{}, {Kind: isa.RegisterGeneral, Register: 1}},
				Returns:   []program.VarLocation{{}},
			},
			load: func(r *Registers) {
				value := 3
				r.General[0] = reflect.ValueOf(&value)
				r.General[1] = reflect.ValueOf(4)
			},
		},
		{
			name: "an environment element the stride does not support",
			site: &program.CallSite{
				Arguments: []program.VarLocation{{}, {Kind: isa.RegisterGeneral, Register: 1}},
				Returns:   []program.VarLocation{{}},
			},
			load: func(r *Registers) {
				value := 3
				r.General[0] = reflect.ValueOf(&value)
				r.General[1] = reflect.ValueOf([]string{"a"})
			},
		},
		{
			name: "a callee with no recognised shape",
			site: &program.CallSite{
				Arguments: []program.VarLocation{{}, {Kind: isa.RegisterSliceUint, Register: 1}},
				Returns:   []program.VarLocation{{}},
			},
			load: func(r *Registers) {
				value := 3
				r.General[0] = reflect.ValueOf(&value)
				r.slicesUint[1] = []uint64{1, 2}
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
