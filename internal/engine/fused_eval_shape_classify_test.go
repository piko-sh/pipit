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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	fusedFetchedChild    = 2
	fusedEnvironment     = 1
	fusedTemporaryResult = 2
)

func fusedChildFetch(layoutIndex uint8, siteIndex uint8) []isa.Instruction {
	return []isa.Instruction{
		{Op: isa.OpGetStructFieldGeneral, A: fusedFetchedChild, B: 0, C: layoutIndex},
		isa.NewTier1Instruction(isa.SubOpCallMethodInlineable, siteIndex, 0),
		isa.NewInstruction(isa.OpExtension, 0, 0, 0),
	}
}

func fusedCallSite(result uint8) program.CallSite {
	return program.CallSite{
		Arguments: []program.VarLocation{
			{Kind: isa.RegisterGeneral, Register: fusedFetchedChild},
			{Kind: isa.RegisterGeneral, Register: fusedEnvironment},
		},
		Returns: []program.VarLocation{{Kind: isa.RegisterUint64, Register: result}},
	}
}

func fusedCandidate(body []isa.Instruction) *program.CompiledFunction {
	interfaceField := func(offset uint32) program.StructFieldLayout {
		return program.StructFieldLayout{
			Offset: offset, PathLength: 1, Kind: uint8(reflect.Interface), RegisterKind: uint8(isa.RegisterGeneral),
		}
	}
	compiledFunction := program.NewNamedFunction("fused")
	compiledFunction.Body = body
	compiledFunction.ParameterKinds = []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterGeneral}
	compiledFunction.ResultKinds = []isa.RegisterKind{isa.RegisterUint64}
	compiledFunction.StructLayoutTable = []program.StructFieldLayout{interfaceField(8), interfaceField(16), interfaceField(24)}
	compiledFunction.CallSites = []program.CallSite{fusedCallSite(0), fusedCallSite(1), fusedCallSite(0)}
	return compiledFunction
}

func truncateUint32(register uint8) isa.Instruction {
	return isa.Instruction{Op: isa.OpTruncateToWidth, A: register, B: 32, C: uint8(isa.RegisterUint64)}
}

func maskedBinaryBody(arithmetic isa.Opcode) []isa.Instruction {
	body := append(fusedChildFetch(0, 0), fusedChildFetch(1, 1)...)
	return append(body,
		isa.Instruction{Op: arithmetic, A: fusedTemporaryResult, B: 0, C: 1},
		truncateUint32(fusedTemporaryResult),
		isa.NewTier1Instruction(isa.SubOpMoveUint64, 0, fusedTemporaryResult),
		tier2Return(1),
	)
}

func maskedBinaryInPlaceBody(arithmetic isa.Opcode) []isa.Instruction {
	body := append(fusedChildFetch(0, 0), fusedChildFetch(1, 1)...)
	return append(body,
		isa.Instruction{Op: arithmetic, A: 0, B: 0, C: 1},
		truncateUint32(0),
		tier2Return(1),
	)
}

func minMaxBody(comparison isa.Opcode) []isa.Instruction {
	body := append(fusedChildFetch(0, 0), fusedChildFetch(1, 1)...)
	return append(body,
		isa.Instruction{Op: comparison, A: 0, B: 0, C: 1},
		isa.Instruction{Op: isa.OpJumpIfFalse, A: 0, B: 1, C: 0},
		tier2Return(1),
		isa.NewTier1Instruction(isa.SubOpMoveUint64, 0, 1),
		tier2Return(1),
	)
}

func modGuardedInPlaceBody() []isa.Instruction {
	body := fusedChildFetch(0, 0)
	body = append(body,
		isa.NewTier1Instruction(isa.SubOpEqualUint64ImmediateThenJumpIfFalse, 0, 0),
		isa.NewInstruction(isa.OpExtension, 2, 0, 0),
		nop(),
		isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, 0, 0),
		tier2Return(1),
	)
	body = append(body, fusedChildFetch(1, 1)...)
	return append(body,
		isa.Instruction{Op: isa.OpRemainderUint64, A: 0, B: 1, C: 0},
		truncateUint32(0),
		tier2Return(1),
	)
}

func modGuardedWholeBody() []isa.Instruction {
	body := fusedChildFetch(0, 0)
	body = append(body,
		isa.NewTier1Instruction(isa.SubOpEqualUint64ImmediateThenJumpIfFalse, 0, 0),
		isa.NewInstruction(isa.OpExtension, 2, 0, 0),
		nop(),
		isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, 0, 0),
		tier2Return(1),
	)
	body = append(body, fusedChildFetch(1, 1)...)
	return append(body,
		isa.Instruction{Op: isa.OpRemainderUint64, A: 0, B: 1, C: 0},
		tier2Return(1),
	)
}

func modGuardedBody() []isa.Instruction {
	body := fusedChildFetch(0, 0)
	body = append(body,
		isa.NewTier1Instruction(isa.SubOpEqualUint64ImmediateThenJumpIfFalse, 0, 0),
		isa.NewInstruction(isa.OpExtension, 3, 0, 0),
		nop(),
		isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, 1, 0),
		isa.NewTier1Instruction(isa.SubOpMoveUint64, 0, 1),
		tier2Return(1),
	)
	body = append(body, fusedChildFetch(1, 1)...)
	return append(body,
		isa.Instruction{Op: isa.OpRemainderUint64, A: fusedTemporaryResult, B: 1, C: 0},
		truncateUint32(fusedTemporaryResult),
		isa.NewTier1Instruction(isa.SubOpMoveUint64, 0, fusedTemporaryResult),
		tier2Return(1),
	)
}

func ifPosBody() []isa.Instruction {
	body := fusedChildFetch(0, 0)
	body = append(body,
		isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, 1, 0),
		isa.Instruction{Op: isa.OpNotEqualUint64, A: 0, B: 0, C: 1},
		isa.Instruction{Op: isa.OpJumpIfFalse, A: 0, B: 5, C: 0},
	)
	body = append(body, fusedChildFetch(1, 1)...)
	body = append(body, isa.NewTier1Instruction(isa.SubOpMoveUint64, 0, 1), tier2Return(1))
	body = append(body, fusedChildFetch(2, 2)...)
	return append(body, tier2Return(1))
}

func ifPosFusedTestBody() []isa.Instruction {
	body := ifPosBody()
	body[4] = isa.NewTier1Instruction(isa.SubOpNotEqualUint64ThenJumpIfFalse, 0, 1)
	body[5] = isa.NewInstruction(isa.OpExtension, 5, 0, 0)
	return body
}

func TestClassifyFusedEvalShapeRecognisesEachFamily(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body []isa.Instruction
		want program.FusedEvalShape
	}{
		{name: "masked addition", body: maskedBinaryBody(isa.OpAddUint64), want: program.FusedEvalAddMasked},
		{name: "masked subtraction", body: maskedBinaryBody(isa.OpSubtractUint64), want: program.FusedEvalSubMasked},
		{name: "masked multiplication", body: maskedBinaryBody(isa.OpMultiplyUint64), want: program.FusedEvalMulMasked},
		{name: "a minimum selection", body: minMaxBody(isa.OpLessThanUint64), want: program.FusedEvalMin},
		{name: "a maximum selection", body: minMaxBody(isa.OpGreaterThanUint64), want: program.FusedEvalMax},
		{name: "a guarded modulo", body: modGuardedBody(), want: program.FusedEvalModGuarded},
		{name: "masked addition made in the result slot", body: maskedBinaryInPlaceBody(isa.OpAddUint64), want: program.FusedEvalAddMasked},
		{name: "masked subtraction made in the result slot", body: maskedBinaryInPlaceBody(isa.OpSubtractUint64), want: program.FusedEvalSubMasked},
		{name: "masked multiplication made in the result slot", body: maskedBinaryInPlaceBody(isa.OpMultiplyUint64), want: program.FusedEvalMulMasked},
		{name: "a guarded modulo whose zero is loaded into the result slot", body: modGuardedInPlaceBody(), want: program.FusedEvalModGuarded},
		{name: "a guarded modulo that returns the whole remainder", body: modGuardedWholeBody(), want: program.FusedEvalModGuardedWhole},
		{name: "a three-way conditional", body: ifPosBody(), want: program.FusedEvalIfPos},
		{name: "a three-way conditional whose test and jump are one instruction", body: ifPosFusedTestBody(), want: program.FusedEvalIfPos},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := fusedCandidate(tt.body)

			classifyFusedEvalShapeOnce(compiledFunction)

			require.Equal(t, tt.want, compiledFunction.EvalShape)
			require.Equal(t, uint32(8), compiledFunction.EvalChildALayout.Offset, "the first child layout must be published")
			require.Equal(t, uint32(16), compiledFunction.EvalChildBLayout.Offset, "the second child layout must be published")
			require.Equal(t, uint16(0), compiledFunction.EvalSiteA)
			require.Equal(t, uint16(1), compiledFunction.EvalSiteB)
			if tt.want == program.FusedEvalIfPos {
				require.Equal(t, uint32(24), compiledFunction.EvalChildCLayout.Offset, "the third child layout must be published")
				require.Equal(t, uint16(2), compiledFunction.EvalSiteC)
			}
		})
	}
}

func TestClassifyFusedEvalShapeRecognisesABodyWhoseEnvironmentHasABankOfItsOwn(t *testing.T) {
	t.Parallel()

	compiledFunction := fusedCandidate(maskedBinaryBody(isa.OpAddUint64))
	compiledFunction.ParameterKinds[1] = isa.RegisterSliceUint64
	for site := range compiledFunction.CallSites {
		compiledFunction.CallSites[site].Arguments[1] = program.VarLocation{Kind: isa.RegisterSliceUint64, Register: 0}
	}

	classifyFusedEvalShapeOnce(compiledFunction)

	require.Equal(t, program.FusedEvalAddMasked, compiledFunction.EvalShape)

	compiledFunction = fusedCandidate(maskedBinaryBody(isa.OpAddUint64))
	compiledFunction.ParameterKinds[1] = isa.RegisterSliceUint64
	classifyFusedEvalShapeOnce(compiledFunction)

	require.Equal(t, program.FusedEvalNone, compiledFunction.EvalShape,
		"a site that passes a general register is not passing the environment the method was given")
}

func TestClassifyFusedEvalShapeAccountsForEveryWordOfTheBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body []isa.Instruction
	}{
		{name: "an empty body", body: nil},
		{name: "one instruction short of the masked family", body: maskedBinaryBody(isa.OpAddUint64)[:9]},
		{name: "one instruction past the masked family", body: append(maskedBinaryBody(isa.OpAddUint64), isa.Instruction{Op: isa.OpAddInt64})},
		{name: "a second return after the masked family", body: append(maskedBinaryInPlaceBody(isa.OpAddUint64), tier2Return(1))},
		{name: "one instruction short of the selection family", body: minMaxBody(isa.OpLessThanUint64)[:10]},
		{name: "one instruction past the selection family", body: append(minMaxBody(isa.OpLessThanUint64), tier2Return(1))},
		{name: "one instruction short of the guarded modulo", body: modGuardedBody()[:15]},
		{name: "one instruction past the guarded modulo", body: append(modGuardedInPlaceBody(), tier2Return(1))},
		{name: "one instruction short of the conditional family", body: ifPosBody()[:14]},
		{name: "one instruction past the conditional family", body: append(ifPosBody(), tier2Return(1))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := fusedCandidate(tt.body)

			classifyFusedEvalShapeOnce(compiledFunction)

			require.Equal(t, program.FusedEvalNone, compiledFunction.EvalShape,
				"a body with a word more or a word fewer than a family is not that family")
		})
	}
}

func TestClassifyFusedEvalShapeRefusesABodyThatDiffersFromItsFamily(t *testing.T) {
	t.Parallel()

	filler := isa.Instruction{Op: isa.OpAddInt64}
	tests := []struct {
		body   func() []isa.Instruction
		mutate func(function *program.CompiledFunction)
		name   string
	}{
		{
			name:   "masked: an arithmetic opcode outside the family",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[6].Op = isa.OpDivideUint64 },
		},
		{
			name:   "masked: operands in the other order",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpSubtractUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[6].B, function.Body[6].C = 1, 0 },
		},
		{
			name:   "masked: an operand that is no child's value",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[6].C = 3 },
		},
		{
			name:   "masked: a truncation to a width other than thirty-two",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[7].B = 16 },
		},
		{
			name:   "masked: a truncation of another register",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[7].A = 3 },
		},
		{
			name:   "masked: a truncation that extends the sign",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[7].C = uint8(isa.RegisterInt64) },
		},
		{
			name:   "masked: a missing truncation",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[7] = filler },
		},
		{
			name: "masked: a move from a bank other than the unsigned one",
			body: func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) {
				function.Body[8] = isa.NewTier1Instruction(isa.SubOpNegateInt64, 0, 0)
			},
		},
		{
			name:   "masked: a move of another register into the result slot",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[8].C = 1 },
		},
		{
			name:   "masked: a result left outside the result slot",
			body:   func() []isa.Instruction { return maskedBinaryInPlaceBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[6].A, function.Body[7].A = 3, 3 },
		},
		{
			name:   "masked: a body that does not end in a return",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[9] = filler },
		},
		{
			name:   "masked: a return of two values",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[9] = tier2Return(2) },
		},
		{
			name:   "child: a field read from a register other than the receiver's",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[0].B = 1 },
		},
		{
			name:   "child: a field read that overwrites the receiver",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[0].A = 0 },
		},
		{
			name:   "child: a field read that overwrites the environment",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[3].A = fusedEnvironment },
		},
		{
			name: "child: a call that is not inlineable",
			body: func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) {
				function.Body[1] = isa.NewTier1Instruction(isa.SubOpCallMethod, 0, 0)
			},
		},
		{
			name:   "child: a missing extension word",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[2] = filler },
		},
		{
			name:   "child: a layout index past the table",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[0].C = 9 },
		},
		{
			name:   "child: a field that is not an interface",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.StructLayoutTable[1].Kind = uint8(reflect.Pointer) },
		},
		{
			name: "child: a call-site index past the table",
			body: func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) {
				function.Body[1] = isa.NewTier1Instruction(isa.SubOpCallMethodInlineable, 9, 0)
			},
		},
		{
			name:   "child: a call on a value other than the one fetched",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.CallSites[0].Arguments[0].Register = 3 },
		},
		{
			name:   "child: a call given another environment",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.CallSites[1].Arguments[1].Register = 3 },
		},
		{
			name: "child: a call given a third argument",
			body: func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) {
				function.CallSites[1].Arguments = append(function.CallSites[1].Arguments, program.VarLocation{})
			},
		},
		{
			name:   "child: a call that returns into another bank",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.CallSites[0].Returns[0].Kind = isa.RegisterInt64 },
		},
		{
			name:   "child: a call whose result is spilled",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.CallSites[0].Returns[0].IsSpilled = true },
		},
		{
			name:   "child: two calls that return into one register",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.CallSites[1].Returns[0].Register = 0 },
		},
		{
			name:   "child: a site that calls a closure",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.CallSites[0].IsClosure = true },
		},
		{
			name:   "child: a site that calls a native function",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.CallSites[1].IsNative = true },
		},
		{
			name:   "child: a result kept in an upvalue",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.CallSites[0].Returns[0].IsUpvalue = true },
		},
		{
			name:   "child: a result held indirectly",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.CallSites[0].Returns[0].IsIndirect = true },
		},
		{
			name:   "child: a result that a closure captured",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.CallSites[1].Returns[0].IsCaptured = true },
		},
		{
			name:   "child: a receiver argument held indirectly",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.CallSites[0].Arguments[0].IsIndirect = true },
		},
		{
			name:   "child: an environment argument that a closure captured",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.CallSites[1].Arguments[1].IsCaptured = true },
		},
		{
			name:   "modulo: the dividend returned into the divisor's register",
			body:   modGuardedBody,
			mutate: func(function *program.CompiledFunction) { function.CallSites[1].Returns[0].Register = 0 },
		},
		{
			name:   "method: a receiver outside the general bank",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.ParameterKinds[0] = isa.RegisterInt64 },
		},
		{
			name: "method: a third parameter",
			body: func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) {
				function.ParameterKinds = append(function.ParameterKinds, isa.RegisterInt64)
			},
		},
		{
			name:   "method: a result that is not unsigned",
			body:   func() []isa.Instruction { return maskedBinaryBody(isa.OpAddUint64) },
			mutate: func(function *program.CompiledFunction) { function.ResultKinds[0] = isa.RegisterInt64 },
		},
		{
			name:   "selection: a comparison in the other order",
			body:   func() []isa.Instruction { return minMaxBody(isa.OpLessThanUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[6].B, function.Body[6].C = 1, 0 },
		},
		{
			name:   "selection: a jump on another flag",
			body:   func() []isa.Instruction { return minMaxBody(isa.OpLessThanUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[7].A = 1 },
		},
		{
			name:   "selection: a jump that lands elsewhere",
			body:   func() []isa.Instruction { return minMaxBody(isa.OpLessThanUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[7].B = 2 },
		},
		{
			name:   "selection: a word in place of the first return",
			body:   func() []isa.Instruction { return minMaxBody(isa.OpLessThanUint64) },
			mutate: func(function *program.CompiledFunction) { function.Body[8] = filler },
		},
		{
			name: "selection: a second arm that returns the first child again",
			body: func() []isa.Instruction { return minMaxBody(isa.OpLessThanUint64) },
			mutate: func(function *program.CompiledFunction) {
				function.Body[9] = isa.NewTier1Instruction(isa.SubOpMoveUint64, 0, 0)
			},
		},
		{
			name:   "modulo: a guard against a value other than zero",
			body:   modGuardedInPlaceBody,
			mutate: func(function *program.CompiledFunction) { function.Body[3].C = 1 },
		},
		{
			name:   "modulo: a guard on another register",
			body:   modGuardedInPlaceBody,
			mutate: func(function *program.CompiledFunction) { function.Body[3].B = 1 },
		},
		{
			name:   "modulo: a jump that lands elsewhere",
			body:   modGuardedInPlaceBody,
			mutate: func(function *program.CompiledFunction) { function.Body[4].A = 3 },
		},
		{
			name:   "modulo: a guarded result other than zero",
			body:   modGuardedInPlaceBody,
			mutate: func(function *program.CompiledFunction) { function.Body[6].C = 1 },
		},
		{
			name:   "modulo: the divisor divided by the dividend",
			body:   modGuardedInPlaceBody,
			mutate: func(function *program.CompiledFunction) { function.Body[11].B, function.Body[11].C = 0, 1 },
		},
		{
			name:   "modulo: a division in place of the remainder",
			body:   modGuardedInPlaceBody,
			mutate: func(function *program.CompiledFunction) { function.Body[11].Op = isa.OpDivideUint64 },
		},
		{
			name:   "modulo: a missing truncation",
			body:   modGuardedInPlaceBody,
			mutate: func(function *program.CompiledFunction) { function.Body[12] = filler },
		},
		{
			name:   "conditional: a comparison with a value other than zero",
			body:   ifPosBody,
			mutate: func(function *program.CompiledFunction) { function.Body[3].C = 1 },
		},
		{
			name:   "conditional: a zero loaded over the condition",
			body:   ifPosBody,
			mutate: func(function *program.CompiledFunction) { function.Body[3].B, function.Body[4].C = 0, 0 },
		},
		{
			name:   "conditional: a test of another register",
			body:   ifPosBody,
			mutate: func(function *program.CompiledFunction) { function.Body[4].B = 3 },
		},
		{
			name:   "conditional: a test for equality",
			body:   ifPosBody,
			mutate: func(function *program.CompiledFunction) { function.Body[4].Op = isa.OpEqualUint64 },
		},
		{
			name:   "conditional: a jump on another flag",
			body:   ifPosBody,
			mutate: func(function *program.CompiledFunction) { function.Body[5].A = 1 },
		},
		{
			name:   "conditional: a jump that lands elsewhere",
			body:   ifPosBody,
			mutate: func(function *program.CompiledFunction) { function.Body[5].B = 6 },
		},
		{
			name:   "conditional: a fused test whose jump lands elsewhere",
			body:   ifPosFusedTestBody,
			mutate: func(function *program.CompiledFunction) { function.Body[5].A = 4 },
		},
		{
			name:   "conditional: a first arm that returns the zero it was tested against",
			body:   ifPosBody,
			mutate: func(function *program.CompiledFunction) { function.Body[9] = filler },
		},
		{
			name:   "conditional: a second arm with no return",
			body:   ifPosBody,
			mutate: func(function *program.CompiledFunction) { function.Body[14] = filler },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := fusedCandidate(tt.body())
			classifyFusedEvalShapeOnce(compiledFunction)
			require.NotEqual(t, program.FusedEvalNone, compiledFunction.EvalShape, "the body is one of a family before it is changed")

			tt.mutate(compiledFunction)
			classifyFusedEvalShapeOnce(compiledFunction)

			require.Equal(t, program.FusedEvalNone, compiledFunction.EvalShape,
				"a body that differs from its family in one word or one register is run as bytecode")
		})
	}
}

func TestClassifyFusedEvalShapeReadsPastPadding(t *testing.T) {
	t.Parallel()

	padAround := func(body []isa.Instruction) []isa.Instruction {
		padded := append([]isa.Instruction{nop()}, body...)
		return append(padded, nop(), nop())
	}
	landOnPadding := func() []isa.Instruction {
		body := minMaxBody(isa.OpLessThanUint64)
		return slices.Insert(body, 9, nop())
	}
	tests := []struct {
		name string
		body []isa.Instruction
		want program.FusedEvalShape
	}{
		{name: "padding before and after a body", body: padAround(maskedBinaryBody(isa.OpAddUint64)), want: program.FusedEvalAddMasked},
		{name: "padding around a body with a jump in it", body: padAround(ifPosBody()), want: program.FusedEvalIfPos},
		{name: "a jump that lands on padding runs on to the word after it", body: landOnPadding(), want: program.FusedEvalMin},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := fusedCandidate(tt.body)

			classifyFusedEvalShapeOnce(compiledFunction)

			require.Equal(t, tt.want, compiledFunction.EvalShape)
		})
	}
}

func TestClassifyFusedEvalShapeRunsOnlyOncePerFunction(t *testing.T) {
	t.Parallel()

	compiledFunction := fusedCandidate(maskedBinaryBody(isa.OpAddUint64))

	classifyFusedEvalShape(compiledFunction)
	require.Equal(t, program.FusedEvalAddMasked, compiledFunction.EvalShape)

	compiledFunction.Body = minMaxBody(isa.OpLessThanUint64)
	classifyFusedEvalShape(compiledFunction)

	require.Equal(t, program.FusedEvalAddMasked, compiledFunction.EvalShape,
		"the once-guard must keep the first classification")
}

func TestNewFusedBodyLeavesOutPaddingAndKeepsEachWordsPosition(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		body          []isa.Instruction
		wantWords     []isa.Instruction
		wantPositions []int
	}{
		{name: "an empty body stays empty", body: nil, wantWords: []isa.Instruction{}, wantPositions: []int{}},
		{name: "a body of only padding empties", body: []isa.Instruction{nop(), nop()}, wantWords: []isa.Instruction{}, wantPositions: []int{}},
		{
			name:          "padding is removed from between real instructions",
			body:          []isa.Instruction{nop(), {Op: isa.OpAddInt64}, nop(), {Op: isa.OpSubtractInt64}, nop()},
			wantWords:     []isa.Instruction{{Op: isa.OpAddInt64}, {Op: isa.OpSubtractInt64}},
			wantPositions: []int{1, 3},
		},
		{
			name:          "a tier-one word carrying operands is kept",
			body:          []isa.Instruction{isa.NewTier1Instruction(isa.SubOpNegateInt64, 0, 1)},
			wantWords:     []isa.Instruction{isa.NewTier1Instruction(isa.SubOpNegateInt64, 0, 1)},
			wantPositions: []int{0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			body, ok := newFusedBody(fusedCandidate(tt.body))

			require.True(t, ok)
			require.Equal(t, tt.wantWords, body.words)
			require.Equal(t, tt.wantPositions, body.positions)
		})
	}
}

func TestNewFusedBodyPlacesTheEnvironmentByItsBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		kind         isa.RegisterKind
		wantRegister uint8
	}{
		{name: "a general environment follows the receiver", kind: isa.RegisterGeneral, wantRegister: 1},
		{name: "an environment in a bank of its own is that bank's first register", kind: isa.RegisterSliceUint64, wantRegister: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := fusedCandidate(nil)
			compiledFunction.ParameterKinds[1] = tt.kind

			body, ok := newFusedBody(compiledFunction)

			require.True(t, ok)
			require.Equal(t, tt.kind, body.environmentKind)
			require.Equal(t, tt.wantRegister, body.environmentRegister)
		})
	}
}

func TestFusedBodyChildBoundsChecksBothTables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		at   int
		body []isa.Instruction
		want bool
	}{
		{name: "a well-formed triple matches", at: 0, body: fusedChildFetch(0, 0), want: true},
		{name: "a triple that runs past the body is refused", at: 1, body: fusedChildFetch(0, 0), want: false},
		{name: "an offset past the body is refused", at: 9, body: fusedChildFetch(0, 0), want: false},
		{name: "an offset before the body is refused", at: -1, body: fusedChildFetch(0, 0), want: false},
		{name: "an empty body is refused", at: 0, body: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, ok := newFusedBody(fusedCandidate(tt.body))
			require.True(t, ok)

			_, matched := body.child(tt.at)

			require.Equal(t, tt.want, matched)
		})
	}

	t.Run("a matched triple returns the layout, the site and the result register it named", func(t *testing.T) {
		t.Parallel()
		body, ok := newFusedBody(fusedCandidate(fusedChildFetch(2, 1)))
		require.True(t, ok)

		child, matched := body.child(0)

		require.True(t, matched)
		require.Equal(t, uint32(24), child.layout.Offset)
		require.Equal(t, uint16(1), child.site)
		require.Equal(t, uint8(1), child.result)
	})
}
