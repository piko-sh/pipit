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

package passes

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestFuseCompareRegJumpVariants(t *testing.T) {
	t.Parallel()
	const condition = 3
	cases := []struct {
		name       string
		comparison isa.Opcode
		jump       isa.Opcode
		fused      isa.SubOpcode
	}{
		{name: "less than", comparison: isa.OpLessThanInt64, jump: isa.OpJumpIfFalse, fused: isa.SubOpLessThanInt64ThenJumpIfFalse},
		{name: "less or equal", comparison: isa.OpLessOrEqualInt64, jump: isa.OpJumpIfFalse, fused: isa.SubOpLessOrEqualInt64ThenJumpIfFalse},
		{name: "greater than", comparison: isa.OpGreaterThanInt64, jump: isa.OpJumpIfFalse, fused: isa.SubOpGreaterThanInt64ThenJumpIfFalse},
		{name: "greater or equal", comparison: isa.OpGreaterOrEqualInt64, jump: isa.OpJumpIfFalse, fused: isa.SubOpGreaterOrEqualInt64ThenJumpIfFalse},
		{name: "equal", comparison: isa.OpEqualInt64, jump: isa.OpJumpIfFalse, fused: isa.SubOpEqualInt64ThenJumpIfFalse},
		{name: "not equal", comparison: isa.OpNotEqualInt64, jump: isa.OpJumpIfFalse, fused: isa.SubOpNotEqualInt64ThenJumpIfFalse},
		{name: "lt on true is ge on false", comparison: isa.OpLessThanInt64, jump: isa.OpJumpIfTrue, fused: isa.SubOpGreaterOrEqualInt64ThenJumpIfFalse},
		{name: "le on true is gt on false", comparison: isa.OpLessOrEqualInt64, jump: isa.OpJumpIfTrue, fused: isa.SubOpGreaterThanInt64ThenJumpIfFalse},
		{name: "gt on true is le on false", comparison: isa.OpGreaterThanInt64, jump: isa.OpJumpIfTrue, fused: isa.SubOpLessOrEqualInt64ThenJumpIfFalse},
		{name: "ge on true is lt on false", comparison: isa.OpGreaterOrEqualInt64, jump: isa.OpJumpIfTrue, fused: isa.SubOpLessThanInt64ThenJumpIfFalse},
		{name: "eq on true is ne on false", comparison: isa.OpEqualInt64, jump: isa.OpJumpIfTrue, fused: isa.SubOpNotEqualInt64ThenJumpIfFalse},
		{name: "ne on true is eq on false", comparison: isa.OpNotEqualInt64, jump: isa.OpJumpIfTrue, fused: isa.SubOpEqualInt64ThenJumpIfFalse},
		{name: "unsigned equality", comparison: isa.OpEqualUint64, jump: isa.OpJumpIfFalse, fused: isa.SubOpEqualUint64ThenJumpIfFalse},
		{name: "unsigned inequality", comparison: isa.OpNotEqualUint64, jump: isa.OpJumpIfFalse, fused: isa.SubOpNotEqualUint64ThenJumpIfFalse},
		{
			name: "unsigned equality on true is inequality on false", comparison: isa.OpEqualUint64, jump: isa.OpJumpIfTrue,
			fused: isa.SubOpNotEqualUint64ThenJumpIfFalse,
		},
		{
			name: "unsigned inequality on true is equality on false", comparison: isa.OpNotEqualUint64, jump: isa.OpJumpIfTrue,
			fused: isa.SubOpEqualUint64ThenJumpIfFalse,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			body := compareJumpBody(testCase.comparison, testCase.jump, condition,
				[]isa.Instruction{mk(isa.OpLoadInt64Constant, condition, 0, 0)},
				[]isa.Instruction{returnVoid()})
			before, ok := program.JumpTargetAt(body, 1)
			require.True(t, ok)
			compiledFunction := &program.CompiledFunction{Body: body}

			require.True(t, FuseCompareRegJump(compiledFunction, body, 0, len(body), program.JumpTargetsOf(body)))

			require.Equal(t, isa.NewTier1Instruction(testCase.fused, 1, 2), body[0])
			require.Equal(t, isa.OpExtension, body[1].Op)
			after, ok := program.JumpTargetAt(body, 0)
			require.True(t, ok)
			require.Equal(t, before, after, "the fused branch lands where the original did")
		})
	}
}

func TestFuseIncrementThenCompareJumpFormsTheRegisterLatch(t *testing.T) {
	t.Parallel()
	const counter, bound = 4, 6
	extension := mk(isa.OpExtension, 0xF8, 0xFF, 0)
	latch := func(test isa.SubOpcode, tested, against uint8) []isa.Instruction {
		return []isa.Instruction{
			isa.NewTier2Instruction(isa.SubOpTier2IncrementInt64, counter), nop(),
			isa.NewTier1Instruction(test, tested, against), extension, returnVoid(),
		}
	}
	for name, tt := range map[string]struct {
		test  isa.SubOpcode
		fused isa.SubOpcode
	}{
		"a bound the counter stays below": {isa.SubOpGreaterOrEqualInt64ThenJumpIfFalse, isa.SubOpIncrementInt64ThenJumpIfLessThan},
		"a bound the counter may reach":   {isa.SubOpGreaterThanInt64ThenJumpIfFalse, isa.SubOpIncrementInt64ThenJumpIfLessOrEqual},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			body := latch(tt.test, counter, bound)
			require.True(t, fuseIncrementThenCompareJump(body, 0, len(body), nil))
			require.Equal(t, []isa.Instruction{nop(), nop(), isa.NewTier1Instruction(tt.fused, counter, bound), extension, returnVoid()}, body,
				"the latch takes the test's place and keeps its extension word, so the offset stands")
		})
	}
	for name, tt := range map[string]struct {
		body    []isa.Instruction
		targets map[int]bool
	}{
		"a jump lands on the test":                  {body: latch(isa.SubOpGreaterOrEqualInt64ThenJumpIfFalse, counter, bound), targets: map[int]bool{2: true}},
		"a jump lands between the two":              {body: latch(isa.SubOpGreaterOrEqualInt64ThenJumpIfFalse, counter, bound), targets: map[int]bool{1: true}},
		"the test is of another register":           {body: latch(isa.SubOpGreaterOrEqualInt64ThenJumpIfFalse, counter+1, bound)},
		"the counter is tested against itself":      {body: latch(isa.SubOpGreaterOrEqualInt64ThenJumpIfFalse, counter, counter)},
		"the test jumps while the counter is above": {body: latch(isa.SubOpLessOrEqualInt64ThenJumpIfFalse, counter, bound)},
		"the bound is a constant":                   {body: latch(isa.SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse, counter, 0)},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want := slices.Clone(tt.body)
			require.False(t, fuseIncrementThenCompareJump(tt.body, 0, len(tt.body), tt.targets))
			require.Equal(t, want, tt.body)
		})
	}
}

func TestFuseCompareConstJumpVariants(t *testing.T) {
	t.Parallel()
	const (
		condition = 3
		constant  = 4
	)
	poolLoad := mk(isa.OpLoadInt64Constant, constant, 1, 0)
	immediateLoad := isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, constant, 9)
	cases := []struct {
		name       string
		comparison isa.Opcode
		jump       isa.Opcode
		fused      isa.SubOpcode
	}{
		{name: "less than", comparison: isa.OpLessThanInt64, jump: isa.OpJumpIfFalse, fused: isa.SubOpLessThanInt64ConstantThenJumpIfFalse},
		{name: "eq on true", comparison: isa.OpEqualInt64, jump: isa.OpJumpIfTrue, fused: isa.SubOpEqualInt64ConstantThenJumpIfTrue},
		{name: "le on true is gt on false", comparison: isa.OpLessOrEqualInt64, jump: isa.OpJumpIfTrue, fused: isa.SubOpGreaterThanInt64ConstantThenJumpIfFalse},
		{name: "lt on true is ge on false", comparison: isa.OpLessThanInt64, jump: isa.OpJumpIfTrue, fused: isa.SubOpGreaterOrEqualInt64ConstantThenJumpIfFalse},
		{name: "ge on true is lt on false", comparison: isa.OpGreaterOrEqualInt64, jump: isa.OpJumpIfTrue, fused: isa.SubOpLessThanInt64ConstantThenJumpIfFalse},
		{name: "gt on true is le on false", comparison: isa.OpGreaterThanInt64, jump: isa.OpJumpIfTrue, fused: isa.SubOpLessOrEqualInt64ConstantThenJumpIfFalse},
		{name: "ne on true is eq on false", comparison: isa.OpNotEqualInt64, jump: isa.OpJumpIfTrue, fused: isa.SubOpEqualInt64ConstantThenJumpIfFalse},
		{name: "ne on false is eq on true", comparison: isa.OpNotEqualInt64, jump: isa.OpJumpIfFalse, fused: isa.SubOpEqualInt64ConstantThenJumpIfTrue},
	}
	for _, testCase := range cases {
		for _, load := range []struct {
			name        string
			instruction isa.Instruction
			pool        []int64
			index       uint8
			value       int64
		}{
			{name: "pool constant", instruction: poolLoad, pool: []int64{0, 700}, index: 1, value: 700},
			{name: "immediate already pooled", instruction: immediateLoad, pool: []int64{5, 9}, index: 1, value: 9},
			{name: "immediate added to the pool", instruction: immediateLoad, pool: []int64{5}, index: 1, value: 9},
		} {
			t.Run(testCase.name+"/"+load.name, func(t *testing.T) {
				t.Parallel()
				low, high := jumpOffset(1)
				body := []isa.Instruction{
					load.instruction,
					mk(testCase.comparison, condition, 1, constant),
					mk(testCase.jump, condition, low, high),
					mk(isa.OpLoadInt64Constant, condition, 0, 0),
					mk(isa.OpLoadInt64Constant, constant, 0, 0),
					returnVoid(),
				}
				before, ok := program.JumpTargetAt(body, 2)
				require.True(t, ok)
				compiledFunction := &program.CompiledFunction{Body: body, IntConstants: slices.Clone(load.pool)}

				require.True(t, FuseThreeInstrPatterns(compiledFunction, body, 0, len(body), program.JumpTargetsOf(body)))

				require.Equal(t, isa.NewTier1Instruction(testCase.fused, 1, load.index), body[0])
				require.Equal(t, load.value, compiledFunction.IntConstants[load.index], "the fused form compares against the loaded value")
				require.Equal(t, isa.OpExtension, body[1].Op)
				require.Equal(t, isa.OpNoOperation, body[2].Op)
				after, ok := program.JumpTargetAt(body, 0)
				require.True(t, ok)
				require.Equal(t, before, after, "the fused branch lands where the original did")
			})
		}
	}
}

func TestFuseCompareConstJumpRefusesAnImmediateWhenThePoolIsFull(t *testing.T) {
	t.Parallel()
	low, high := jumpOffset(1)
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, 4, 9),
		mk(isa.OpLessThanInt64, 3, 1, 4),
		mk(isa.OpJumpIfFalse, 3, low, high),
		mk(isa.OpLoadInt64Constant, 3, 0, 0),
		mk(isa.OpLoadInt64Constant, 4, 0, 0),
		returnVoid(),
	}
	pool := make([]int64, 256)
	for index := range pool {
		pool[index] = int64(1000 + index)
	}
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: pool}

	require.False(t, FuseThreeInstrPatterns(compiledFunction, body, 0, len(body), program.JumpTargetsOf(body)),
		"a constant index must fit in one byte")
	require.Len(t, compiledFunction.IntConstants, 256)
}

func TestFuseCompareRegJumpLiveness(t *testing.T) {
	t.Parallel()
	const (
		condition = 3
		other     = 5
		siteIndex = 0
	)
	intLocation := func(register uint8) program.VarLocation {
		return program.VarLocation{Register: register, Kind: isa.RegisterInt64}
	}
	call := isa.NewTier1Instruction(isa.SubOpCall, siteIndex, 0)
	builtin := isa.NewTier1Instruction(isa.SubOpCallBuiltin, 0, 1)
	writesCondition := mk(isa.OpLoadInt64Constant, condition, 0, 0)
	readsCondition := mk(isa.OpAddInt64, other, condition, 1)

	cases := []struct {
		name        string
		jump        isa.Opcode
		fallThrough []isa.Instruction
		taken       []isa.Instruction
		site        program.CallSite
		resultKinds []isa.RegisterKind
		jumpTargets map[int]bool
		fused       bool
	}{
		{name: "written on the fall-through and returned on the taken edge", fallThrough: []isa.Instruction{writesCondition}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "overwritten on both edges", fallThrough: []isa.Instruction{mk(isa.OpAddInt64, condition, 1, 2)}, taken: []isa.Instruction{writesCondition}, fused: true},
		{name: "both edges fall off the end of the body", fallThrough: []isa.Instruction{mk(isa.OpNoOperation, 0, 0, 0)}, taken: []isa.Instruction{mk(isa.OpNoOperation, 0, 0, 0)}, fused: true},
		{name: "read on the fall-through", fallThrough: []isa.Instruction{readsCondition}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "read on the taken edge", fallThrough: []isa.Instruction{writesCondition}, taken: []isa.Instruction{readsCondition}, fused: false},
		{name: "read through a jump on the taken edge", fallThrough: []isa.Instruction{writesCondition}, taken: []isa.Instruction{jumpForward(1), returnVoid(), readsCondition}, fused: false},
		{name: "the branch word is a jump target", fallThrough: []isa.Instruction{writesCondition}, taken: []isa.Instruction{returnVoid()}, jumpTargets: map[int]bool{1: true}, fused: false},
		{name: "jump-if-true fuses into the opposite compare", jump: isa.OpJumpIfTrue, fallThrough: []isa.Instruction{writesCondition}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "jump-if-true with the condition read on the taken edge", jump: isa.OpJumpIfTrue, fallThrough: []isa.Instruction{writesCondition}, taken: []isa.Instruction{readsCondition}, fused: false},
		{name: "the condition is a result slot", fallThrough: []isa.Instruction{writesCondition}, taken: []isa.Instruction{returnVoid()}, resultKinds: []isa.RegisterKind{isa.RegisterInt64, isa.RegisterInt64, isa.RegisterInt64, isa.RegisterInt64}, fused: false},
		{name: "a result-slot condition overwritten on both edges", fallThrough: []isa.Instruction{writesCondition}, taken: []isa.Instruction{writesCondition}, resultKinds: []isa.RegisterKind{isa.RegisterInt64, isa.RegisterInt64, isa.RegisterInt64, isa.RegisterInt64}, fused: true},
		{name: "a call passes the condition as an int argument", fallThrough: []isa.Instruction{call, writesCondition}, taken: []isa.Instruction{returnVoid()}, site: program.CallSite{Arguments: []program.VarLocation{intLocation(condition)}}, fused: false},
		{name: "a call writes the condition as its int result", fallThrough: []isa.Instruction{call}, taken: []isa.Instruction{returnVoid()}, site: program.CallSite{Returns: []program.VarLocation{intLocation(condition)}}, fused: true},
		{name: "a call passing an unrelated int neither reads nor writes it", fallThrough: []isa.Instruction{call, readsCondition}, taken: []isa.Instruction{returnVoid()}, site: program.CallSite{Arguments: []program.VarLocation{intLocation(other)}}, fused: false},
		{name: "a builtin call reads the condition through its argument word", fallThrough: []isa.Instruction{builtin, mk(isa.OpExtension, condition, uint8(isa.RegisterInt64), 0), writesCondition}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "a builtin call reading a general register with the same number is not a read", fallThrough: []isa.Instruction{builtin, mk(isa.OpExtension, condition, uint8(isa.RegisterGeneral), 0), writesCondition}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "a bare extension word naming the condition is a read", fallThrough: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpMakeSliceInt64, other, 1), mk(isa.OpExtension, condition, 0, 0), writesCondition}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "a deferred call passing the condition as an int argument", fallThrough: []isa.Instruction{mk(isa.OpDefer, other, 1, 0), mk(isa.OpExtension, 0, condition, uint8(isa.RegisterInt64)), writesCondition}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "a deferred call passing a string register with the same number", fallThrough: []isa.Instruction{mk(isa.OpDefer, other, 1, 0), mk(isa.OpExtension, 0, condition, uint8(isa.RegisterString)), writesCondition}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "an extension immediate equal to the condition is not a read", fallThrough: []isa.Instruction{mk(isa.OpBoxAsType, other, 1, 0), mk(isa.OpExtension, condition, 0, 0), writesCondition}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "slice bounds flags equal to the condition are not a read", fallThrough: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSliceGeneral, other, 1), mk(isa.OpExtension, isa.SliceLowBoundFlag|isa.SliceHighBoundFlag, 4, 5), writesCondition}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "a slice low bound naming the condition is a read", fallThrough: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSliceGeneral, other, 1), mk(isa.OpExtension, isa.SliceLowBoundFlag, condition, 0), writesCondition}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "a channel receive into the condition ends the search as a write", fallThrough: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpReceiveFromChannel, 1, other), mk(isa.OpExtension, condition, uint8(isa.RegisterInt64), 0), readsCondition}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "a channel receive into a string register with the same number is neither", fallThrough: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpReceiveFromChannel, 1, other), mk(isa.OpExtension, condition, uint8(isa.RegisterString), 0), readsCondition}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "a range step writing the condition as its int key ends the search", fallThrough: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpRangeNext, 1, other), mk(isa.OpExtension, 1, condition, uint8(isa.RegisterInt64)), mk(isa.OpExtension, 0, 0, 0), readsCondition}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "an unrecorded extension layout is read conservatively", fallThrough: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpAppendInt64Wide, other, 1), mk(isa.OpExtension, 0, condition, 0), writesCondition}, taken: []isa.Instruction{returnVoid()}, fused: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			jump := testCase.jump
			if jump == 0 {
				jump = isa.OpJumpIfFalse
			}
			body := compareJumpBody(isa.OpLessThanInt64, jump, condition, testCase.fallThrough, testCase.taken)
			compiledFunction := &program.CompiledFunction{
				Body:        body,
				CallSites:   []program.CallSite{testCase.site},
				ResultKinds: testCase.resultKinds,
			}
			jumpTargets := testCase.jumpTargets
			if jumpTargets == nil {
				jumpTargets = program.JumpTargetsOf(body)
			}
			original := slices.Clone(body)

			fused := FuseCompareRegJump(compiledFunction, body, 0, len(body), jumpTargets)

			require.Equal(t, testCase.fused, fused)
			if !fused {
				require.Equal(t, original, body, "a refused fusion leaves the body untouched")
			}
		})
	}
}

func TestFuseCompareRegJumpFollowsLoopBackEdge(t *testing.T) {
	t.Parallel()
	const condition = 3
	exitLow, exitHigh := jumpOffset(2)
	backLow, backHigh := jumpOffset(-4)
	body := []isa.Instruction{
		mk(isa.OpLessThanInt64, condition, 1, 2),
		mk(isa.OpJumpIfFalse, condition, exitLow, exitHigh),
		isa.NewTier2Instruction(isa.SubOpTier2IncrementInt64, 1),
		isa.NewTier1Instruction(isa.SubOpJump, backLow, backHigh),
		returnVoid(),
	}
	compiledFunction := &program.CompiledFunction{Body: body}

	require.True(t, FuseCompareRegJump(compiledFunction, body, 0, len(body), program.JumpTargetsOf(body)))

	target, ok := program.JumpTargetAt(body, 0)
	require.True(t, ok)
	require.Equal(t, 4, target)
}

func TestFuseCompareConstJumpLiveness(t *testing.T) {
	t.Parallel()
	const (
		condition = 3
		constant  = 4
		other     = 5
	)
	cases := []struct {
		name        string
		fallThrough []isa.Instruction
		taken       []isa.Instruction
		fused       bool
	}{
		{name: "both registers dead", fallThrough: []isa.Instruction{mk(isa.OpLoadInt64Constant, condition, 0, 0), mk(isa.OpLoadInt64Constant, constant, 0, 0)}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "condition read after the branch", fallThrough: []isa.Instruction{mk(isa.OpAddInt64, other, condition, 1), mk(isa.OpLoadInt64Constant, constant, 0, 0)}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "loaded constant read after the branch", fallThrough: []isa.Instruction{mk(isa.OpLoadInt64Constant, condition, 0, 0), mk(isa.OpAddInt64, other, constant, 1)}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "loaded constant read on the taken edge", fallThrough: []isa.Instruction{mk(isa.OpLoadInt64Constant, condition, 0, 0), mk(isa.OpLoadInt64Constant, constant, 0, 0)}, taken: []isa.Instruction{mk(isa.OpAddInt64, other, constant, 1)}, fused: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			low, high := jumpOffset(int16(len(testCase.fallThrough)))
			body := []isa.Instruction{
				mk(isa.OpLoadInt64Constant, constant, 0, 0),
				mk(isa.OpLessThanInt64, condition, 1, constant),
				mk(isa.OpJumpIfFalse, condition, low, high),
			}
			body = append(body, testCase.fallThrough...)
			body = append(body, testCase.taken...)
			before, ok := program.JumpTargetAt(body, 2)
			require.True(t, ok)
			compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{0}}

			fused := FuseThreeInstrPatterns(compiledFunction, body, 0, len(body), program.JumpTargetsOf(body))

			require.Equal(t, testCase.fused, fused)
			if !fused {
				return
			}
			require.Equal(t, isa.NewTier1Instruction(isa.SubOpLessThanInt64ConstantThenJumpIfFalse, 1, 0), body[0])
			require.Equal(t, isa.OpExtension, body[1].Op)
			require.Equal(t, isa.OpNoOperation, body[2].Op)
			after, ok := program.JumpTargetAt(body, 0)
			require.True(t, ok)
			require.Equal(t, before, after, "the fused branch lands where the original did")
		})
	}
}
