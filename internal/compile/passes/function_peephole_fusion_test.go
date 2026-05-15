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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func compareJumpBody(cmpOp, jumpOp isa.Opcode, cond uint8, fallThrough, taken []isa.Instruction) []isa.Instruction {
	lo, hi := jumpOffset(int16(len(fallThrough)))
	body := []isa.Instruction{
		mk(cmpOp, cond, 1, 2),
		mk(jumpOp, cond, lo, hi),
	}
	body = append(body, fallThrough...)
	return append(body, taken...)
}

func returnVoid() isa.Instruction {
	return isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid)
}

func TestFuseCompareRegJumpVariants(t *testing.T) {
	t.Parallel()
	const cond = 3
	cases := []struct {
		name  string
		cmpOp isa.Opcode
		fused isa.SubOpcode
	}{
		{name: "lt", cmpOp: isa.OpLtInt, fused: isa.SubOpLtIntJumpFalse},
		{name: "le", cmpOp: isa.OpLeInt, fused: isa.SubOpLeIntJumpFalse},
		{name: "gt", cmpOp: isa.OpGtInt, fused: isa.SubOpGtIntJumpFalse},
		{name: "ge", cmpOp: isa.OpGeInt, fused: isa.SubOpGeIntJumpFalse},
		{name: "eq", cmpOp: isa.OpEqInt, fused: isa.SubOpEqIntJumpFalse},
		{name: "ne", cmpOp: isa.OpNeInt, fused: isa.SubOpNeIntJumpFalse},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			body := compareJumpBody(testCase.cmpOp, isa.OpJumpIfFalse, cond,
				[]isa.Instruction{mk(isa.OpLoadIntConst, cond, 0, 0)},
				[]isa.Instruction{returnVoid()})
			before, ok := program.JumpTargetAt(body, 1)
			require.True(t, ok)
			compiledFunction := &program.CompiledFunction{Body: body}

			require.True(t, FuseCompareRegJump(compiledFunction, body, 0, len(body), program.JumpTargetsOf(body)))

			require.Equal(t, isa.NewTier1Instruction(testCase.fused, 1, 2), body[0])
			require.Equal(t, isa.OpExt, body[1].Op)
			after, ok := program.JumpTargetAt(body, 0)
			require.True(t, ok)
			require.Equal(t, before, after, "the fused branch lands where the original did")
		})
	}
}

func TestFuseCompareRegJumpLiveness(t *testing.T) {
	t.Parallel()
	const (
		cond      = 3
		other     = 5
		siteIndex = 0
	)
	intLocation := func(register uint8) program.VarLocation {
		return program.VarLocation{Register: register, Kind: isa.RegisterInt}
	}
	call := isa.NewTier1Instruction(isa.SubOpCall, siteIndex, 0)
	builtin := isa.NewTier1Instruction(isa.SubOpCallBuiltin, 0, 1)
	writeCond := mk(isa.OpLoadIntConst, cond, 0, 0)
	readCond := mk(isa.OpAddInt, other, cond, 1)

	cases := []struct {
		name        string
		jumpOp      isa.Opcode
		fallThrough []isa.Instruction
		taken       []isa.Instruction
		site        program.CallSite
		resultKinds []isa.RegisterKind
		jumpTargets map[int]bool
		fused       bool
	}{
		{name: "written on the fall-through and returned on the taken edge", fallThrough: []isa.Instruction{writeCond}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "overwritten on both edges", fallThrough: []isa.Instruction{mk(isa.OpAddInt, cond, 1, 2)}, taken: []isa.Instruction{writeCond}, fused: true},
		{name: "both edges fall off the end of the body", fallThrough: []isa.Instruction{mk(isa.OpNop, 0, 0, 0)}, taken: []isa.Instruction{mk(isa.OpNop, 0, 0, 0)}, fused: true},
		{name: "read on the fall-through", fallThrough: []isa.Instruction{readCond}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "read on the taken edge", fallThrough: []isa.Instruction{writeCond}, taken: []isa.Instruction{readCond}, fused: false},
		{name: "read through a jump on the taken edge", fallThrough: []isa.Instruction{writeCond}, taken: []isa.Instruction{jumpForward(1), returnVoid(), readCond}, fused: false},
		{name: "the branch word is a jump target", fallThrough: []isa.Instruction{writeCond}, taken: []isa.Instruction{returnVoid()}, jumpTargets: map[int]bool{1: true}, fused: false},
		{name: "jump-if-true is not a fusable branch", jumpOp: isa.OpJumpIfTrue, fallThrough: []isa.Instruction{writeCond}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "the condition is a result slot", fallThrough: []isa.Instruction{writeCond}, taken: []isa.Instruction{returnVoid()}, resultKinds: []isa.RegisterKind{isa.RegisterInt, isa.RegisterInt, isa.RegisterInt, isa.RegisterInt}, fused: false},
		{name: "a call passes the condition as an int argument", fallThrough: []isa.Instruction{call, writeCond}, taken: []isa.Instruction{returnVoid()}, site: program.CallSite{Arguments: []program.VarLocation{intLocation(cond)}}, fused: false},
		{name: "a call writes the condition as its int result", fallThrough: []isa.Instruction{call}, taken: []isa.Instruction{returnVoid()}, site: program.CallSite{Returns: []program.VarLocation{intLocation(cond)}}, fused: true},
		{name: "a call passing an unrelated int neither reads nor writes it", fallThrough: []isa.Instruction{call, readCond}, taken: []isa.Instruction{returnVoid()}, site: program.CallSite{Arguments: []program.VarLocation{intLocation(other)}}, fused: false},
		{name: "a builtin call reads the condition through its argument word", fallThrough: []isa.Instruction{builtin, mk(isa.OpExt, cond, uint8(isa.RegisterInt), 0), writeCond}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "a builtin call reading a general register with the same number is not a read", fallThrough: []isa.Instruction{builtin, mk(isa.OpExt, cond, uint8(isa.RegisterGeneral), 0), writeCond}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "a bare extension word naming the condition is a read", fallThrough: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpMakeSliceInt, other, 1), mk(isa.OpExt, cond, 0, 0), writeCond}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "a deferred call passing the condition as an int argument", fallThrough: []isa.Instruction{mk(isa.OpDefer, other, 1, 0), mk(isa.OpExt, 0, cond, uint8(isa.RegisterInt)), writeCond}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "a deferred call passing a string register with the same number", fallThrough: []isa.Instruction{mk(isa.OpDefer, other, 1, 0), mk(isa.OpExt, 0, cond, uint8(isa.RegisterString)), writeCond}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "an extension immediate equal to the condition is not a read", fallThrough: []isa.Instruction{mk(isa.OpPackTyped, other, 1, 0), mk(isa.OpExt, cond, 0, 0), writeCond}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "slice bounds flags equal to the condition are not a read", fallThrough: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSliceOp, other, 1), mk(isa.OpExt, isa.SliceLowBoundFlag|isa.SliceHighBoundFlag, 4, 5), writeCond}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "a slice low bound naming the condition is a read", fallThrough: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSliceOp, other, 1), mk(isa.OpExt, isa.SliceLowBoundFlag, cond, 0), writeCond}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "a channel receive into the condition ends the search as a write", fallThrough: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpChannelReceive, 1, other), mk(isa.OpExt, cond, uint8(isa.RegisterInt), 0), readCond}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "a channel receive into a string register with the same number is neither", fallThrough: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpChannelReceive, 1, other), mk(isa.OpExt, cond, uint8(isa.RegisterString), 0), readCond}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "a range step writing the condition as its int key ends the search", fallThrough: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpRangeNext, 1, other), mk(isa.OpExt, 1, cond, uint8(isa.RegisterInt)), mk(isa.OpExt, 0, 0, 0), readCond}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "an unrecorded extension layout is read conservatively", fallThrough: []isa.Instruction{mk(isa.OpAppend, other, 1, 2), mk(isa.OpExt, 0, cond, 0), writeCond}, taken: []isa.Instruction{returnVoid()}, fused: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			jumpOp := testCase.jumpOp
			if jumpOp == 0 {
				jumpOp = isa.OpJumpIfFalse
			}
			body := compareJumpBody(isa.OpLtInt, jumpOp, cond, testCase.fallThrough, testCase.taken)
			compiledFunction := &program.CompiledFunction{
				Body:        body,
				CallSites:   []program.CallSite{testCase.site},
				ResultKinds: testCase.resultKinds,
			}
			jumpTargets := testCase.jumpTargets
			if jumpTargets == nil {
				jumpTargets = program.JumpTargetsOf(body)
			}
			original := append([]isa.Instruction(nil), body...)

			fused := FuseCompareRegJump(compiledFunction, body, 0, len(body), jumpTargets)

			require.Equal(t, testCase.fused, fused)
			if !fused {
				require.Equal(t, original, body, "a refused fusion leaves the body untouched")
			}
		})
	}
}

func jumpForward(words int16) isa.Instruction {
	lo, hi := jumpOffset(words)
	return isa.NewTier1Instruction(isa.SubOpJump, lo, hi)
}

func TestFuseCompareRegJumpFollowsLoopBackEdge(t *testing.T) {
	t.Parallel()
	const cond = 3
	exitLo, exitHi := jumpOffset(2)
	backLo, backHi := jumpOffset(-4)
	body := []isa.Instruction{
		mk(isa.OpLtInt, cond, 1, 2),
		mk(isa.OpJumpIfFalse, cond, exitLo, exitHi),
		isa.NewTier2Instruction(isa.SubOpTier2IncInt, 1),
		isa.NewTier1Instruction(isa.SubOpJump, backLo, backHi),
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
		cond     = 3
		constant = 4
		other    = 5
	)
	cases := []struct {
		name        string
		fallThrough []isa.Instruction
		taken       []isa.Instruction
		fused       bool
	}{
		{name: "both registers dead", fallThrough: []isa.Instruction{mk(isa.OpLoadIntConst, cond, 0, 0), mk(isa.OpLoadIntConst, constant, 0, 0)}, taken: []isa.Instruction{returnVoid()}, fused: true},
		{name: "condition read after the branch", fallThrough: []isa.Instruction{mk(isa.OpAddInt, other, cond, 1), mk(isa.OpLoadIntConst, constant, 0, 0)}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "loaded constant read after the branch", fallThrough: []isa.Instruction{mk(isa.OpLoadIntConst, cond, 0, 0), mk(isa.OpAddInt, other, constant, 1)}, taken: []isa.Instruction{returnVoid()}, fused: false},
		{name: "loaded constant read on the taken edge", fallThrough: []isa.Instruction{mk(isa.OpLoadIntConst, cond, 0, 0), mk(isa.OpLoadIntConst, constant, 0, 0)}, taken: []isa.Instruction{mk(isa.OpAddInt, other, constant, 1)}, fused: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			lo, hi := jumpOffset(int16(len(testCase.fallThrough)))
			body := []isa.Instruction{
				mk(isa.OpLoadIntConst, constant, 0, 0),
				mk(isa.OpLtInt, cond, 1, constant),
				mk(isa.OpJumpIfFalse, cond, lo, hi),
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
			require.Equal(t, isa.NewTier1Instruction(isa.SubOpLtIntConstJumpFalse, 1, 0), body[0])
			require.Equal(t, isa.OpExt, body[1].Op)
			require.Equal(t, isa.OpNop, body[2].Op)
			after, ok := program.JumpTargetAt(body, 0)
			require.True(t, ok)
			require.Equal(t, before, after, "the fused branch lands where the original did")
		})
	}
}

func TestCountIncomingJumpsSeesFusedForms(t *testing.T) {
	t.Parallel()
	fusedLo, fusedHi := jumpOffset(1)
	wordLo, wordHi := jumpOffset(-2)
	body := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpLtIntJumpFalse, 1, 2),
		mk(isa.OpExt, fusedLo, fusedHi, 0),
		mk(isa.OpNop, 0, 0, 0),
		mk(isa.OpNop, 0, 0, 0),
		mk(isa.OpJumpIfFalse, 5, wordLo, wordHi),
	}
	for _, source := range []int{0, 4} {
		target, ok := program.JumpTargetAt(body, source)
		require.True(t, ok)
		require.Equal(t, 3, target, "both branches land on index 3")
	}

	require.Equal(t, 2, countIncomingJumps(nil, body, 3, -1))
	require.Equal(t, 1, countIncomingJumps(nil, body, 3, 0), "excluding the fused source leaves the word jump")
	require.Equal(t, 1, countIncomingJumps(nil, body, 3, 4), "excluding the word source leaves the fused jump")
}
