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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func inlineableSite(returnRegister uint8) program.CallSite {
	return program.CallSite{
		Arguments: []program.VarLocation{{Kind: isa.RegisterGeneral}, {Kind: isa.RegisterSliceUint}},
		Returns:   []program.VarLocation{{Kind: isa.RegisterUint, Register: returnRegister}},
	}
}

func binopUintCandidate(arithmetic isa.Opcode) *program.CompiledFunction {
	callee := program.NewNamedFunction("binop")
	callee.StructLayoutTable = []program.StructFieldLayout{{Offset: 8, PathLength: 1}, {Offset: 16, PathLength: 1}}
	callee.CallSites = []program.CallSite{inlineableSite(4), inlineableSite(5)}
	callee.Body = []isa.Instruction{
		{Op: isa.OpGetStructFieldGeneral, A: 0, B: 0, C: 0},
		isa.NewTier1Instruction(isa.SubOpCallMethodInlineable, 0, 0),
		isa.NewInstruction(isa.OpExt, 0, 0, 0),
		{Op: isa.OpGetStructFieldGeneral, A: 1, B: 0, C: 1},
		isa.NewTier1Instruction(isa.SubOpCallMethodInlineable, 1, 0),
		isa.NewInstruction(isa.OpExt, 0, 0, 0),
		{Op: arithmetic, A: 0, B: 4, C: 5},
		tier2Return(1),
	}
	return callee
}

func TestMatchBinopUintShapeAcceptsEachArithmeticFamily(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		arithmetic isa.Opcode
	}{
		{name: "unsigned addition", arithmetic: isa.OpAddUint},
		{name: "unsigned subtraction", arithmetic: isa.OpSubUint},
		{name: "unsigned multiplication", arithmetic: isa.OpMulUint},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			callee := binopUintCandidate(tt.arithmetic)
			descriptor := &program.InlineDescriptor{}

			require.True(t, matchBinopUintShape(callee, descriptor))
			require.Equal(t, program.InlineShapeBinopUint, classifyInlineShape(callee))
			require.Equal(t, tt.arithmetic, descriptor.BinopOpcode)
			require.Equal(t, uint32(8), descriptor.LeftLayout.Offset)
			require.Equal(t, uint32(16), descriptor.RightLayout.Offset)
			require.False(t, descriptor.MaskApplies, "an unmasked body must not claim a mask")
		})
	}
}

func TestMatchBinopUintShapeRecordsAnOptionalTruncation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		width     uint8
		wantMask  uint64
		wantApply bool
	}{
		{name: "a byte-wide truncation", width: 8, wantMask: 0xFF, wantApply: true},
		{name: "a half-word truncation", width: 16, wantMask: 0xFFFF, wantApply: true},
		{name: "a word truncation", width: 32, wantMask: 0xFFFFFFFF, wantApply: true},
		{name: "a one-bit truncation", width: 1, wantMask: 0x1, wantApply: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			callee := binopUintCandidate(isa.OpAddUint)
			callee.Body = []isa.Instruction{
				callee.Body[0], callee.Body[1], callee.Body[2],
				callee.Body[3], callee.Body[4], callee.Body[5],
				callee.Body[6],
				{Op: isa.OpTruncateNarrow, A: 0, B: tt.width, C: uint8(isa.RegisterUint)},
				tier2Return(1),
			}
			descriptor := &program.InlineDescriptor{}

			require.True(t, matchBinopUintShape(callee, descriptor))
			require.Equal(t, tt.wantApply, descriptor.MaskApplies)
			if tt.wantApply {
				require.Equal(t, tt.wantMask, descriptor.MaskValue)
			}
		})
	}
}

func TestMatchBinopUintShapeRefusesATruncationItCannotEncode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		width uint8
	}{
		{name: "a full-width truncation leaves no mask to apply", width: 64},
		{name: "a zero-width truncation is meaningless", width: 0},
		{name: "a truncation past the register width is out of range", width: 65},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			callee := binopUintCandidate(isa.OpAddUint)
			callee.Body = []isa.Instruction{
				callee.Body[0], callee.Body[1], callee.Body[2],
				callee.Body[3], callee.Body[4], callee.Body[5],
				callee.Body[6],
				{Op: isa.OpTruncateNarrow, A: 0, B: tt.width, C: uint8(isa.RegisterUint)},
				tier2Return(1),
			}

			require.False(t, matchBinopUintShape(callee, nil),
				"a width the matcher cannot encode leaves the word unconsumed, so the body no longer ends in a return")
		})
	}
}

func TestMatchBinopUintShapeAcceptsAnOptionalMove(t *testing.T) {
	t.Parallel()

	callee := binopUintCandidate(isa.OpAddUint)
	callee.Body = []isa.Instruction{
		callee.Body[0], callee.Body[1], callee.Body[2],
		callee.Body[3], callee.Body[4], callee.Body[5],
		callee.Body[6],
		isa.NewTier1Instruction(isa.SubOpMoveUint, 1, 0),
		tier2Return(1),
	}

	require.True(t, matchBinopUintShape(callee, nil), "a trailing move of the result is part of the shape")
}

func TestMatchBinopUintShapeToleratesPaddingExceptBesideAnExtensionWord(t *testing.T) {
	t.Parallel()

	t.Run("padding around the field reads and the arithmetic is skipped", func(t *testing.T) {
		t.Parallel()
		callee := binopUintCandidate(isa.OpAddUint)
		body := callee.Body
		callee.Body = []isa.Instruction{
			nop(), body[0], body[1], body[2],
			nop(), body[3], body[4], body[5],
			nop(), body[6], nop(), body[7], nop(),
		}

		require.True(t, matchBinopUintShape(callee, nil),
			"padding at every position the matcher scans past must be skipped")
	})

	t.Run("padding between a call and its extension word breaks the shape", func(t *testing.T) {
		t.Parallel()
		callee := binopUintCandidate(isa.OpAddUint)
		body := callee.Body
		callee.Body = []isa.Instruction{
			body[0], body[1], nop(), body[2],
			body[3], body[4], body[5],
			body[6], body[7],
		}

		require.False(t, matchBinopUintShape(callee, nil),
			"the matcher reaches an extension word by fixed offset, so it must stay adjacent to its call")
	})
}

func TestMatchBinopUintShapeRefusesEveryGuardViolation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		mutate func(callee *program.CompiledFunction)
		name   string
	}{
		{
			name:   "an empty body",
			mutate: func(callee *program.CompiledFunction) { callee.Body = nil },
		},
		{
			name:   "a body of only padding",
			mutate: func(callee *program.CompiledFunction) { callee.Body = []isa.Instruction{nop(), nop()} },
		},
		{
			name:   "an opening instruction that is not a general field read",
			mutate: func(callee *program.CompiledFunction) { callee.Body[0] = isa.Instruction{Op: isa.OpAddInt} },
		},
		{
			name:   "a field read against a non-zero receiver",
			mutate: func(callee *program.CompiledFunction) { callee.Body[0].B = 1 },
		},
		{
			name:   "a left layout index past the table",
			mutate: func(callee *program.CompiledFunction) { callee.Body[0].C = 9 },
		},
		{
			name:   "a right layout index past the table",
			mutate: func(callee *program.CompiledFunction) { callee.Body[3].C = 9 },
		},
		{
			name: "a first call that is not a method call",
			mutate: func(callee *program.CompiledFunction) {
				callee.Body[1] = isa.NewTier1Instruction(isa.SubOpNegInt, 0, 0)
			},
		},
		{
			name: "a call-site index past the table",
			mutate: func(callee *program.CompiledFunction) {
				callee.Body[1] = isa.NewTier1Instruction(isa.SubOpCallMethodInlineable, 9, 0)
			},
		},
		{
			name: "a call site whose return slot is not unsigned",
			mutate: func(callee *program.CompiledFunction) {
				callee.CallSites[0].Returns[0].Kind = isa.RegisterInt
			},
		},
		{
			name: "a call site whose receiver is not in the general bank",
			mutate: func(callee *program.CompiledFunction) {
				callee.CallSites[0].Arguments[0].Kind = isa.RegisterInt
			},
		},
		{
			name: "a call site with the wrong argument count",
			mutate: func(callee *program.CompiledFunction) {
				callee.CallSites[0].Arguments = callee.CallSites[0].Arguments[:1]
			},
		},
		{
			name:   "an arithmetic opcode outside the unsigned family",
			mutate: func(callee *program.CompiledFunction) { callee.Body[6].Op = isa.OpDivUint },
		},
		{
			name:   "an arithmetic left operand that is not the first call's return slot",
			mutate: func(callee *program.CompiledFunction) { callee.Body[6].B = 7 },
		},
		{
			name:   "an arithmetic right operand that is not the second call's return slot",
			mutate: func(callee *program.CompiledFunction) { callee.Body[6].C = 7 },
		},
		{
			name:   "a body that does not end in a return",
			mutate: func(callee *program.CompiledFunction) { callee.Body[7] = isa.Instruction{Op: isa.OpAddInt} },
		},
		{
			name: "real work after the return",
			mutate: func(callee *program.CompiledFunction) {
				callee.Body = append(callee.Body, isa.Instruction{Op: isa.OpAddInt})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			callee := binopUintCandidate(isa.OpAddUint)
			tt.mutate(callee)

			require.False(t, matchBinopUintShape(callee, nil),
				"an unproven body must not be inlined as a binop shape")
			require.Equal(t, program.InlineShapeNone, classifyInlineShape(callee))
		})
	}
}

func TestClassifyInlineShapeHandlesTheEmptyCases(t *testing.T) {
	t.Parallel()

	t.Run("a nil callee has no shape", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, program.InlineShapeNone, classifyInlineShape(nil))
	})

	t.Run("a callee with no body has no shape", func(t *testing.T) {
		t.Parallel()
		require.Equal(t, program.InlineShapeNone, classifyInlineShape(program.NewNamedFunction("empty")))
	})
}

func TestIsInlineableShapeRequiresTwoArgumentsAndAnUnsignedReturn(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		arguments []isa.RegisterKind
		returns   []isa.RegisterKind
		want      bool
	}{
		{name: "a general receiver with an unsigned return is inlineable",
			arguments: []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterSliceUint}, returns: []isa.RegisterKind{isa.RegisterUint}, want: true},
		{name: "a single argument is refused",
			arguments: []isa.RegisterKind{isa.RegisterGeneral}, returns: []isa.RegisterKind{isa.RegisterUint}, want: false},
		{name: "three arguments are refused",
			arguments: []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterSliceUint, isa.RegisterInt}, returns: []isa.RegisterKind{isa.RegisterUint}, want: false},
		{name: "no return slot is refused",
			arguments: []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterSliceUint}, returns: nil, want: false},
		{name: "two return slots are refused",
			arguments: []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterSliceUint}, returns: []isa.RegisterKind{isa.RegisterUint, isa.RegisterUint}, want: false},
		{name: "a receiver outside the general bank is refused",
			arguments: []isa.RegisterKind{isa.RegisterInt, isa.RegisterSliceUint}, returns: []isa.RegisterKind{isa.RegisterUint}, want: false},
		{name: "a return outside the unsigned bank is refused",
			arguments: []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterSliceUint}, returns: []isa.RegisterKind{isa.RegisterInt}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			site := &program.CallSite{}
			for _, kind := range tt.arguments {
				site.Arguments = append(site.Arguments, program.VarLocation{Kind: kind})
			}
			for _, kind := range tt.returns {
				site.Returns = append(site.Returns, program.VarLocation{Kind: kind})
			}

			require.Equal(t, tt.want, program.IsInlineableShape(site))
		})
	}
}

func TestInlineEvalFullFrameCalleeAcceptableChecksBanksAndCounts(t *testing.T) {
	t.Parallel()

	acceptable := func() *program.CompiledFunction {
		callee := program.NewNamedFunction("callee")
		callee.ParameterKinds = []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterSliceUint}
		callee.ResultKinds = []isa.RegisterKind{isa.RegisterUint}
		callee.NumRegisters[isa.RegisterGeneral] = 1
		callee.NumRegisters[isa.RegisterUint] = 1
		callee.NumRegisters[isa.RegisterSliceUint] = 1
		return callee
	}

	t.Run("a well-formed callee is acceptable", func(t *testing.T) {
		t.Parallel()
		require.True(t, inlineEvalFullFrameCalleeAcceptable(acceptable()))
	})

	tests := []struct {
		mutate func(callee *program.CompiledFunction)
		name   string
	}{
		{name: "a single parameter", mutate: func(c *program.CompiledFunction) { c.ParameterKinds = c.ParameterKinds[:1] }},
		{name: "a receiver outside the general bank", mutate: func(c *program.CompiledFunction) { c.ParameterKinds[0] = isa.RegisterInt }},
		{name: "an environment parameter in an unsupported bank", mutate: func(c *program.CompiledFunction) { c.ParameterKinds[1] = isa.RegisterInt }},
		{name: "no result", mutate: func(c *program.CompiledFunction) { c.ResultKinds = nil }},
		{name: "a result outside the unsigned bank", mutate: func(c *program.CompiledFunction) { c.ResultKinds[0] = isa.RegisterInt }},
		{name: "no general registers", mutate: func(c *program.CompiledFunction) { c.NumRegisters[isa.RegisterGeneral] = 0 }},
		{name: "no unsigned registers", mutate: func(c *program.CompiledFunction) { c.NumRegisters[isa.RegisterUint] = 0 }},
		{name: "no unsigned slice register for a slice environment", mutate: func(c *program.CompiledFunction) { c.NumRegisters[isa.RegisterSliceUint] = 0 }},
		{
			name: "only one general register for a general environment",
			mutate: func(c *program.CompiledFunction) {
				c.ParameterKinds[1] = isa.RegisterGeneral
				c.NumRegisters[isa.RegisterGeneral] = 1
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name+" is refused", func(t *testing.T) {
			t.Parallel()
			callee := acceptable()
			tt.mutate(callee)

			require.False(t, inlineEvalFullFrameCalleeAcceptable(callee))
		})
	}
}

func TestIsInlineEvalEnvKindAcceptsOnlyTwoBanks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind isa.RegisterKind
		want bool
	}{
		{name: "the general bank is an environment", kind: isa.RegisterGeneral, want: true},
		{name: "the unsigned slice bank is an environment", kind: isa.RegisterSliceUint, want: true},
		{name: "the integer bank is not", kind: isa.RegisterInt, want: false},
		{name: "the unsigned bank is not", kind: isa.RegisterUint, want: false},
		{name: "the integer slice bank is not", kind: isa.RegisterSliceInt, want: false},
		{name: "the string bank is not", kind: isa.RegisterString, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, isInlineEvalEnvKind(tt.kind))
		})
	}
}
