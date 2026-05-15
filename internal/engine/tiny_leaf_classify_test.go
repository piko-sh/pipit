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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type tinyLeafCandidate struct {
	body       []isa.Instruction
	layouts    []program.StructFieldLayout
	paramKinds []isa.RegisterKind
	paramRegs  []uint8
}

func (c tinyLeafCandidate) classify() *program.CompiledFunction {
	compiledFunction := program.NewNamedFunction("candidate")
	compiledFunction.Body = c.body
	compiledFunction.StructLayoutTable = c.layouts
	compiledFunction.ParameterKinds = c.paramKinds
	compiledFunction.ParameterRegisters = c.paramRegs
	ClassifyTinyLeaf(compiledFunction)
	return compiledFunction
}

func nop() isa.Instruction {
	return isa.Instruction{Op: isa.OpDrillTier1, A: 0, B: 0, C: 0}
}

func tier2Return(count uint8) isa.Instruction {
	return isa.NewTier2Instruction(isa.SubOpTier2Return, count)
}

func receiverOnly() ([]isa.RegisterKind, []uint8) {
	return []isa.RegisterKind{isa.RegisterGeneral}, []uint8{0}
}

func intFieldLayouts() []program.StructFieldLayout {
	return []program.StructFieldLayout{{Offset: 8, PathLength: 1, Kind: uint8(reflect.Int64), RegisterKind: uint8(isa.RegisterInt)}}
}

func TestClassifyTinyLeafRecognisesTheReturnFieldShapes(t *testing.T) {
	t.Parallel()

	kinds, regs := receiverOnly()

	tests := []struct {
		name string
		body []isa.Instruction
		want program.TinyLeafShape
	}{
		{
			name: "an unsigned field read followed by a return",
			body: []isa.Instruction{{Op: isa.OpGetStructFieldUint, A: 0, B: 0, C: 0}, tier2Return(1)},
			want: program.TinyLeafReturnUintField,
		},
		{
			name: "a signed field read followed by a return",
			body: []isa.Instruction{{Op: isa.OpGetStructFieldIntT0, A: 0, B: 0, C: 0}, tier2Return(1)},
			want: program.TinyLeafReturnIntField,
		},
		{
			name: "leading padding before the field read is skipped",
			body: []isa.Instruction{nop(), {Op: isa.OpGetStructFieldUint, A: 0, B: 0, C: 0}, tier2Return(1)},
			want: program.TinyLeafReturnUintField,
		},
		{
			name: "padding between the read and the return is skipped",
			body: []isa.Instruction{{Op: isa.OpGetStructFieldUint, A: 0, B: 0, C: 0}, nop(), tier2Return(1)},
			want: program.TinyLeafReturnUintField,
		},
		{
			name: "trailing padding after the return is skipped",
			body: []isa.Instruction{{Op: isa.OpGetStructFieldIntT0, A: 0, B: 0, C: 0}, tier2Return(1), nop(), nop()},
			want: program.TinyLeafReturnIntField,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			candidate := tinyLeafCandidate{body: tt.body, layouts: intFieldLayouts(), paramKinds: kinds, paramRegs: regs}

			compiledFunction := candidate.classify()

			require.Equal(t, tt.want, compiledFunction.TinyLeafShape)
			require.Equal(t, uint32(8), compiledFunction.TinyLeafLayout.Offset, "a match must publish the layout it matched")
		})
	}
}

func TestClassifyTinyLeafRefusesBodiesThatOnlyLookLikeALeaf(t *testing.T) {
	t.Parallel()

	kinds, regs := receiverOnly()
	read := isa.Instruction{Op: isa.OpGetStructFieldUint, A: 0, B: 0, C: 0}

	tests := []struct {
		name    string
		body    []isa.Instruction
		layouts []program.StructFieldLayout
	}{
		{
			name:    "an empty body has nothing to classify",
			body:    nil,
			layouts: intFieldLayouts(),
		},
		{
			name:    "a body of only padding has no opening op",
			body:    []isa.Instruction{nop(), nop()},
			layouts: intFieldLayouts(),
		},
		{
			name:    "a body longer than the gate is refused",
			body:    []isa.Instruction{read, nop(), nop(), nop(), nop(), nop(), tier2Return(1)},
			layouts: intFieldLayouts(),
		},
		{
			name:    "a destination register other than zero is refused",
			body:    []isa.Instruction{{Op: isa.OpGetStructFieldUint, A: 1, B: 0, C: 0}, tier2Return(1)},
			layouts: intFieldLayouts(),
		},
		{
			name:    "a receiver register other than zero is refused",
			body:    []isa.Instruction{{Op: isa.OpGetStructFieldUint, A: 0, B: 1, C: 0}, tier2Return(1)},
			layouts: intFieldLayouts(),
		},
		{
			name:    "a layout index past the table is refused",
			body:    []isa.Instruction{{Op: isa.OpGetStructFieldUint, A: 0, B: 0, C: 3}, tier2Return(1)},
			layouts: intFieldLayouts(),
		},
		{
			name:    "an empty layout table refuses every index",
			body:    []isa.Instruction{read, tier2Return(1)},
			layouts: nil,
		},
		{
			name:    "a read with no return is refused",
			body:    []isa.Instruction{read},
			layouts: intFieldLayouts(),
		},
		{
			name:    "real work after the return is refused",
			body:    []isa.Instruction{read, tier2Return(1), {Op: isa.OpAddInt, A: 0, B: 1, C: 2}},
			layouts: intFieldLayouts(),
		},
		{
			name:    "an unrecognised opening opcode is refused",
			body:    []isa.Instruction{{Op: isa.OpAddInt, A: 0, B: 1, C: 2}, tier2Return(1)},
			layouts: intFieldLayouts(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			candidate := tinyLeafCandidate{body: tt.body, layouts: tt.layouts, paramKinds: kinds, paramRegs: regs}

			compiledFunction := candidate.classify()

			require.Equal(t, program.TinyLeafNone, compiledFunction.TinyLeafShape,
				"an unproven body must stay unclassified rather than take the fast path")
		})
	}
}

func TestClassifyTinyLeafRecognisesTheFieldIncrementShapes(t *testing.T) {
	t.Parallel()

	kinds, regs := receiverOnly()

	tests := []struct {
		name  string
		subOp isa.SubOpcode
		want  program.TinyLeafShape
	}{
		{name: "a signed field increment", subOp: isa.SubOpIncStructFieldInt, want: program.TinyLeafIncIntField},
		{name: "an unsigned field increment", subOp: isa.SubOpIncStructFieldUint, want: program.TinyLeafIncUintField},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			candidate := tinyLeafCandidate{
				body:       []isa.Instruction{isa.NewTier1Instruction(tt.subOp, 0, 0)},
				layouts:    intFieldLayouts(),
				paramKinds: kinds,
				paramRegs:  regs,
			}

			compiledFunction := candidate.classify()

			require.Equal(t, tt.want, compiledFunction.TinyLeafShape)
		})
	}
}

func TestClassifyTinyLeafRefusesIncrementsThatAreNotLoneLeaves(t *testing.T) {
	t.Parallel()

	kinds, regs := receiverOnly()
	increment := isa.NewTier1Instruction(isa.SubOpIncStructFieldInt, 0, 0)

	tests := []struct {
		name       string
		body       []isa.Instruction
		paramKinds []isa.RegisterKind
		paramRegs  []uint8
	}{
		{
			name:       "a non-zero receiver operand is refused",
			body:       []isa.Instruction{isa.NewTier1Instruction(isa.SubOpIncStructFieldInt, 1, 0)},
			paramKinds: kinds, paramRegs: regs,
		},
		{
			name:       "a second parameter is refused",
			body:       []isa.Instruction{increment},
			paramKinds: []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterInt}, paramRegs: []uint8{0, 1},
		},
		{
			name:       "a receiver outside the general bank is refused",
			body:       []isa.Instruction{increment},
			paramKinds: []isa.RegisterKind{isa.RegisterInt}, paramRegs: []uint8{0},
		},
		{
			name:       "a receiver in a register other than zero is refused",
			body:       []isa.Instruction{increment},
			paramKinds: kinds, paramRegs: []uint8{1},
		},
		{
			name:       "real work after the increment is refused",
			body:       []isa.Instruction{increment, {Op: isa.OpAddInt, A: 0, B: 1, C: 2}},
			paramKinds: kinds, paramRegs: regs,
		},
		{
			name:       "an unrelated tier-one sub-op is refused",
			body:       []isa.Instruction{isa.NewTier1Instruction(isa.SubOpNegInt, 0, 0)},
			paramKinds: kinds, paramRegs: regs,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			candidate := tinyLeafCandidate{body: tt.body, layouts: intFieldLayouts(), paramKinds: tt.paramKinds, paramRegs: tt.paramRegs}

			compiledFunction := candidate.classify()

			require.Equal(t, program.TinyLeafNone, compiledFunction.TinyLeafShape)
		})
	}
}

func TestClassifyTinyLeafRecognisesScalarFieldAssignment(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		opcode    isa.Opcode
		valueBank isa.RegisterKind
		fieldKind reflect.Kind
	}{
		{name: "a signed field assigned from the second parameter", opcode: isa.OpSetStructFieldIntT0, valueBank: isa.RegisterInt, fieldKind: reflect.Int64},
		{name: "an unsigned field assigned from the second parameter", opcode: isa.OpSetStructFieldUint, valueBank: isa.RegisterUint, fieldKind: reflect.Uint64},
		{name: "a float field assigned from the second parameter", opcode: isa.OpSetStructFieldFloat, valueBank: isa.RegisterFloat, fieldKind: reflect.Float64},
		{name: "a boolean field assigned from the second parameter", opcode: isa.OpSetStructFieldBool, valueBank: isa.RegisterBool, fieldKind: reflect.Bool},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			candidate := tinyLeafCandidate{
				body:       []isa.Instruction{{Op: tt.opcode, A: 0, B: 1, C: 0}},
				layouts:    []program.StructFieldLayout{{Offset: 16, PathLength: 1, Kind: uint8(tt.fieldKind), RegisterKind: uint8(tt.valueBank)}},
				paramKinds: []isa.RegisterKind{isa.RegisterGeneral, tt.valueBank},
				paramRegs:  []uint8{0, 1},
			}

			compiledFunction := candidate.classify()

			require.Equal(t, program.TinyLeafSetScalarFieldFromArg, compiledFunction.TinyLeafShape)
			require.Equal(t, uint32(16), compiledFunction.TinyLeafLayout.Offset)
		})
	}
}

func TestClassifyTinyLeafRefusesScalarAssignmentWhenTheBanksDisagree(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		body       []isa.Instruction
		layouts    []program.StructFieldLayout
		paramKinds []isa.RegisterKind
		paramRegs  []uint8
	}{
		{
			name:       "a value parameter in the wrong bank is refused",
			body:       []isa.Instruction{{Op: isa.OpSetStructFieldIntT0, A: 0, B: 1, C: 0}},
			layouts:    []program.StructFieldLayout{{Offset: 16, PathLength: 1, Kind: uint8(reflect.Int64), RegisterKind: uint8(isa.RegisterInt)}},
			paramKinds: []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterFloat}, paramRegs: []uint8{0, 1},
		},
		{
			name:       "a field kind that does not match the value bank is refused",
			body:       []isa.Instruction{{Op: isa.OpSetStructFieldIntT0, A: 0, B: 1, C: 0}},
			layouts:    []program.StructFieldLayout{{Offset: 16, PathLength: 1, Kind: uint8(reflect.String), RegisterKind: uint8(isa.RegisterInt)}},
			paramKinds: []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterInt}, paramRegs: []uint8{0, 1},
		},
		{
			name:       "a value register that is not the second parameter is refused",
			body:       []isa.Instruction{{Op: isa.OpSetStructFieldIntT0, A: 0, B: 2, C: 0}},
			layouts:    []program.StructFieldLayout{{Offset: 16, PathLength: 1, Kind: uint8(reflect.Int64), RegisterKind: uint8(isa.RegisterInt)}},
			paramKinds: []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterInt}, paramRegs: []uint8{0, 1},
		},
		{
			name:       "a receiver operand other than zero is refused",
			body:       []isa.Instruction{{Op: isa.OpSetStructFieldIntT0, A: 1, B: 1, C: 0}},
			layouts:    []program.StructFieldLayout{{Offset: 16, PathLength: 1, Kind: uint8(reflect.Int64), RegisterKind: uint8(isa.RegisterInt)}},
			paramKinds: []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterInt}, paramRegs: []uint8{0, 1},
		},
		{
			name:       "a single parameter is refused",
			body:       []isa.Instruction{{Op: isa.OpSetStructFieldIntT0, A: 0, B: 1, C: 0}},
			layouts:    []program.StructFieldLayout{{Offset: 16, PathLength: 1, Kind: uint8(reflect.Int64), RegisterKind: uint8(isa.RegisterInt)}},
			paramKinds: []isa.RegisterKind{isa.RegisterGeneral}, paramRegs: []uint8{0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			candidate := tinyLeafCandidate{body: tt.body, layouts: tt.layouts, paramKinds: tt.paramKinds, paramRegs: tt.paramRegs}

			compiledFunction := candidate.classify()

			require.Equal(t, program.TinyLeafNone, compiledFunction.TinyLeafShape)
		})
	}
}

func TestClassifyTinyLeafRecognisesTheEnvironmentLookupShapes(t *testing.T) {
	t.Parallel()

	kinds, regs := receiverOnly()
	read := isa.Instruction{Op: isa.OpGetStructFieldIntT0, A: 0, B: 0, C: 0}

	tests := []struct {
		name string
		body []isa.Instruction
		want program.TinyLeafShape
	}{
		{
			name: "a slice lookup at the field slot",
			body: []isa.Instruction{read, {Op: isa.OpSliceGetUint, A: 0, B: 1, C: 0}, tier2Return(1)},
			want: program.TinyLeafReturnEnvUintAtIntFieldSlot,
		},
		{
			name: "a typed-direct slice lookup carrying its extension word",
			body: []isa.Instruction{read, isa.NewTier1Instruction(isa.SubOpSliceGetUintDirect, 0, 1), isa.NewInstruction(isa.OpExt, 0, 0, 0), tier2Return(1)},
			want: program.TinyLeafReturnEnvUintAtIntFieldSlot,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			candidate := tinyLeafCandidate{body: tt.body, layouts: intFieldLayouts(), paramKinds: kinds, paramRegs: regs}

			compiledFunction := candidate.classify()

			require.Equal(t, tt.want, compiledFunction.TinyLeafShape)
		})
	}

	t.Run("a typed-direct lookup without its extension word is refused", func(t *testing.T) {
		t.Parallel()
		candidate := tinyLeafCandidate{
			body:       []isa.Instruction{read, isa.NewTier1Instruction(isa.SubOpSliceGetUintDirect, 0, 1), tier2Return(1)},
			layouts:    intFieldLayouts(),
			paramKinds: kinds, paramRegs: regs,
		}

		require.Equal(t, program.TinyLeafNone, candidate.classify().TinyLeafShape,
			"the typed-direct form must carry an extension word to be inlineable")
	})

	t.Run("a slice lookup writing a register other than zero is refused", func(t *testing.T) {
		t.Parallel()
		candidate := tinyLeafCandidate{
			body:       []isa.Instruction{read, {Op: isa.OpSliceGetUint, A: 1, B: 1, C: 0}, tier2Return(1)},
			layouts:    intFieldLayouts(),
			paramKinds: kinds, paramRegs: regs,
		}

		require.Equal(t, program.TinyLeafNone, candidate.classify().TinyLeafShape)
	})
}

func TestFindNextNonNopSkipsOnlyTheAllZeroWord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		body  []isa.Instruction
		start int
		want  int
	}{
		{name: "an empty body has no instruction", body: nil, start: 0, want: -1},
		{name: "a body of only padding has no instruction", body: []isa.Instruction{nop(), nop()}, start: 0, want: -1},
		{name: "the first instruction is returned when it is real", body: []isa.Instruction{{Op: isa.OpAddInt}}, start: 0, want: 0},
		{name: "leading padding is skipped", body: []isa.Instruction{nop(), nop(), {Op: isa.OpAddInt}}, start: 0, want: 2},
		{name: "the search starts where it is told", body: []isa.Instruction{{Op: isa.OpAddInt}, {Op: isa.OpSubInt}}, start: 1, want: 1},
		{name: "a start past the end finds nothing", body: []isa.Instruction{{Op: isa.OpAddInt}}, start: 5, want: -1},
		{name: "a tier-one word with operands is not padding", body: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpNegInt, 0, 1)}, start: 0, want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, findNextNonNop(tt.body, tt.start))
			require.Equal(t, tt.want >= 0, hasNonNopAfter(tt.body, tt.start), "the two helpers must agree")
		})
	}
}

func TestIsTier2ReturnIgnoresTheValueCount(t *testing.T) {
	t.Parallel()

	tests := []struct {
		instruction isa.Instruction
		name        string
		want        bool
	}{
		{name: "a return of no values matches", instruction: tier2Return(0), want: true},
		{name: "a return of one value matches", instruction: tier2Return(1), want: true},
		{name: "a return of many values matches, because the count is not part of the shape", instruction: tier2Return(200), want: true},
		{name: "padding is not a return", instruction: nop(), want: false},
		{name: "a tier-zero opcode is not a return", instruction: isa.Instruction{Op: isa.OpAddInt}, want: false},
		{name: "another tier-two sub-op is not a return", instruction: isa.NewTier2Instruction(isa.SubOpTier2MakeMap, 0), want: false},
		{name: "another tier-one sub-op is not a return", instruction: isa.NewTier1Instruction(isa.SubOpNegInt, 0, 0), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isTier2Return(tt.instruction))
		})
	}
}

func TestTinyLeafSetOpBankMapsEachScalarSetter(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		opcode isa.Opcode
		want   isa.RegisterKind
	}{
		{name: "a signed field setter reads the integer bank", opcode: isa.OpSetStructFieldIntT0, want: isa.RegisterInt},
		{name: "an unsigned field setter reads the unsigned bank", opcode: isa.OpSetStructFieldUint, want: isa.RegisterUint},
		{name: "a float field setter reads the float bank", opcode: isa.OpSetStructFieldFloat, want: isa.RegisterFloat},
		{name: "a boolean field setter reads the boolean bank", opcode: isa.OpSetStructFieldBool, want: isa.RegisterBool},
		{name: "any other opcode falls back to the general bank", opcode: isa.OpAddInt, want: isa.RegisterGeneral},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tinyLeafSetOpBank(tt.opcode))
		})
	}
}

func TestTinyLeafScalarFieldBankCoversEveryScalarKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind reflect.Kind
		want isa.RegisterKind
	}{
		{name: "a plain int uses the integer bank", kind: reflect.Int, want: isa.RegisterInt},
		{name: "an int8 uses the integer bank", kind: reflect.Int8, want: isa.RegisterInt},
		{name: "an int16 uses the integer bank", kind: reflect.Int16, want: isa.RegisterInt},
		{name: "an int32 uses the integer bank", kind: reflect.Int32, want: isa.RegisterInt},
		{name: "an int64 uses the integer bank", kind: reflect.Int64, want: isa.RegisterInt},
		{name: "a plain uint uses the unsigned bank", kind: reflect.Uint, want: isa.RegisterUint},
		{name: "a uint8 uses the unsigned bank", kind: reflect.Uint8, want: isa.RegisterUint},
		{name: "a uint16 uses the unsigned bank", kind: reflect.Uint16, want: isa.RegisterUint},
		{name: "a uint32 uses the unsigned bank", kind: reflect.Uint32, want: isa.RegisterUint},
		{name: "a uint64 uses the unsigned bank", kind: reflect.Uint64, want: isa.RegisterUint},
		{name: "a uintptr uses the unsigned bank", kind: reflect.Uintptr, want: isa.RegisterUint},
		{name: "a float32 uses the float bank", kind: reflect.Float32, want: isa.RegisterFloat},
		{name: "a float64 uses the float bank", kind: reflect.Float64, want: isa.RegisterFloat},
		{name: "a bool uses the boolean bank", kind: reflect.Bool, want: isa.RegisterBool},
		{name: "a string falls back to the general bank", kind: reflect.String, want: isa.RegisterGeneral},
		{name: "a struct falls back to the general bank", kind: reflect.Struct, want: isa.RegisterGeneral},
		{name: "a complex value falls back to the general bank", kind: reflect.Complex128, want: isa.RegisterGeneral},
		{name: "a slice falls back to the general bank", kind: reflect.Slice, want: isa.RegisterGeneral},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, tinyLeafScalarFieldBank(tt.kind))
		})
	}
}

func TestTinyLeafSiteCompatibleMatchesArityAndBanks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		shape     program.TinyLeafShape
		params    []isa.RegisterKind
		arguments []isa.RegisterKind
		returns   []isa.RegisterKind
		want      bool
	}{
		{
			name:   "a void increment with no return slot is compatible",
			shape:  program.TinyLeafIncIntField,
			params: []isa.RegisterKind{isa.RegisterGeneral}, arguments: []isa.RegisterKind{isa.RegisterGeneral},
			want: true,
		},
		{
			name:   "a void increment with a return slot is refused",
			shape:  program.TinyLeafIncIntField,
			params: []isa.RegisterKind{isa.RegisterGeneral}, arguments: []isa.RegisterKind{isa.RegisterGeneral},
			returns: []isa.RegisterKind{isa.RegisterInt},
			want:    false,
		},
		{
			name:   "a signed field return needs an integer return slot",
			shape:  program.TinyLeafReturnIntField,
			params: []isa.RegisterKind{isa.RegisterGeneral}, arguments: []isa.RegisterKind{isa.RegisterGeneral},
			returns: []isa.RegisterKind{isa.RegisterInt},
			want:    true,
		},
		{
			name:   "a signed field return refuses an unsigned return slot",
			shape:  program.TinyLeafReturnIntField,
			params: []isa.RegisterKind{isa.RegisterGeneral}, arguments: []isa.RegisterKind{isa.RegisterGeneral},
			returns: []isa.RegisterKind{isa.RegisterUint},
			want:    false,
		},
		{
			name:   "an unsigned field return needs an unsigned return slot",
			shape:  program.TinyLeafReturnUintField,
			params: []isa.RegisterKind{isa.RegisterGeneral}, arguments: []isa.RegisterKind{isa.RegisterGeneral},
			returns: []isa.RegisterKind{isa.RegisterUint},
			want:    true,
		},
		{
			name:   "a mismatched argument count is refused",
			shape:  program.TinyLeafReturnUintField,
			params: []isa.RegisterKind{isa.RegisterGeneral}, arguments: []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterInt},
			returns: []isa.RegisterKind{isa.RegisterUint},
			want:    false,
		},
		{
			name:      "a later argument boxed into the general bank is tolerated",
			shape:     program.TinyLeafSetScalarFieldFromArg,
			params:    []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterInt},
			arguments: []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterGeneral},
			want:      true,
		},
		{
			name:      "a receiver in the wrong bank is refused even when boxed",
			shape:     program.TinyLeafSetScalarFieldFromArg,
			params:    []isa.RegisterKind{isa.RegisterInt, isa.RegisterInt},
			arguments: []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterInt},
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			callee := program.NewNamedFunction("callee")
			callee.ParameterKinds = tt.params
			callee.TinyLeafShape = tt.shape

			site := &program.CallSite{}
			for _, kind := range tt.arguments {
				site.Arguments = append(site.Arguments, program.VarLocation{Kind: kind})
			}
			for _, kind := range tt.returns {
				site.Returns = append(site.Returns, program.VarLocation{Kind: kind})
			}

			require.Equal(t, tt.want, tinyLeafSiteCompatible(site, callee))
		})
	}
}
