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
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type fuserFunc func(*program.CompiledFunction, []isa.Instruction, int, int, map[int]bool) bool

type droppedWrite struct {
	kind     isa.RegisterKind
	register uint8
}

type fusionWindow struct {
	name    string
	fuser   fuserFunc
	window  []isa.Instruction
	branch  bool
	dropped []droppedWrite
}

const unrelatedRegister = 9

func fusionWindowsThatDropWrites() []fusionWindow {
	return []fusionWindow{
		{
			name:    "concat rune",
			fuser:   FuseConcatRune,
			window:  []isa.Instruction{isa.NewTier1Instruction(isa.SubOpConvertRuneToString, 4, 5), mk(isa.OpConcatenateString, 1, 2, 4)},
			dropped: []droppedWrite{{kind: isa.RegisterString, register: 4}},
		},
		{
			name:    "string index to int",
			fuser:   FuseGetStringByteAsInt64,
			window:  []isa.Instruction{mk(isa.OpGetStringByte, 4, 2, 3), isa.NewTier1Instruction(isa.SubOpConvertUint64ToInt64, 7, 4)},
			dropped: []droppedWrite{{kind: isa.RegisterUint64, register: 4}},
		},
		{
			name:    "unsigned element to int",
			fuser:   FuseGetElementUint64AsInt64,
			window:  []isa.Instruction{mk(isa.OpGetElementUint64, 4, 2, 3), isa.NewTier1Instruction(isa.SubOpConvertUint64ToInt64, 7, 4)},
			dropped: []droppedWrite{{kind: isa.RegisterUint64, register: 4}},
		},
		{
			name:    "boxed append and move",
			fuser:   FuseAppendMove,
			window:  []isa.Instruction{mk(isa.OpAppend, 6, 2, 3), mk(isa.OpMoveGeneral, 2, 6, 0)},
			dropped: []droppedWrite{{kind: isa.RegisterGeneral, register: 6}},
		},
		{
			name:    "typed append and move",
			fuser:   FuseAppendMove,
			window:  []isa.Instruction{isa.NewTier1Instruction(isa.SubOpAppendInt64Wide, 6, 2), mk(isa.OpExtension, 3, 0, 0), mk(isa.OpMoveGeneral, 2, 6, 0)},
			dropped: []droppedWrite{{kind: isa.RegisterGeneral, register: 6}},
		},
		{
			name:   "string constant compare and branch",
			fuser:  FuseThreeInstrPatterns,
			branch: true,
			window: []isa.Instruction{
				mk(isa.OpLoadStringConstant, 4, 0, 0),
				mk(isa.OpEqualString, 3, 1, 4),
				mk(isa.OpJumpIfFalse, 3, 0, 0),
			},
			dropped: []droppedWrite{{kind: isa.RegisterString, register: 4}, {kind: isa.RegisterInt64, register: 3}},
		},
		{
			name:   "nil test and branch",
			fuser:  FuseThreeInstrPatterns,
			branch: true,
			window: []isa.Instruction{
				isa.NewTier2Instruction(isa.SubOpTier2LoadNil, 4),
				mk(isa.OpEqualGeneral, 3, 1, 4),
				mk(isa.OpJumpIfTrue, 3, 0, 0),
			},
			dropped: []droppedWrite{{kind: isa.RegisterGeneral, register: 4}, {kind: isa.RegisterInt64, register: 3}},
		},
		{
			name:   "increment and loop-back compare",
			fuser:  FuseThreeInstrPatterns,
			branch: true,
			window: []isa.Instruction{
				isa.NewTier2Instruction(isa.SubOpTier2IncrementInt64, 1),
				mk(isa.OpLessThanInt64, 3, 1, 2),
				mk(isa.OpJumpIfTrue, 3, 0, 0),
			},
			dropped: []droppedWrite{{kind: isa.RegisterInt64, register: 3}},
		},
		{
			name:   "string length compare and branch",
			fuser:  FuseThreeInstrPatterns,
			branch: true,
			window: []isa.Instruction{
				isa.NewTier1Instruction(isa.SubOpLengthString, 4, 1),
				mk(isa.OpLessThanInt64, 3, 2, 4),
				mk(isa.OpJumpIfFalse, 3, 0, 0),
			},
			dropped: []droppedWrite{{kind: isa.RegisterInt64, register: 4}, {kind: isa.RegisterInt64, register: 3}},
		},
		{
			name:   "uint constant compare and branch",
			fuser:  FuseThreeInstrPatterns,
			branch: true,
			window: []isa.Instruction{
				isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, 4, 7),
				mk(isa.OpEqualUint64, 3, 1, 4),
				mk(isa.OpJumpIfFalse, 3, 0, 0),
			},
			dropped: []droppedWrite{{kind: isa.RegisterUint64, register: 4}, {kind: isa.RegisterInt64, register: 3}},
		},
		{
			name:   "int constant compare and branch",
			fuser:  FuseThreeInstrPatterns,
			branch: true,
			window: []isa.Instruction{
				mk(isa.OpLoadInt64Constant, 4, 0, 0),
				mk(isa.OpLessThanInt64, 3, 1, 4),
				mk(isa.OpJumpIfFalse, 3, 0, 0),
			},
			dropped: []droppedWrite{{kind: isa.RegisterInt64, register: 4}, {kind: isa.RegisterInt64, register: 3}},
		},
		{
			name:    "uint range check",
			fuser:   fuseLongPatterns,
			branch:  true,
			window:  rangeCheckWindow(1, 4, 6),
			dropped: []droppedWrite{{kind: isa.RegisterUint64, register: 4}, {kind: isa.RegisterUint64, register: 6}, {kind: isa.RegisterInt64, register: 5}, {kind: isa.RegisterInt64, register: 7}, {kind: isa.RegisterInt64, register: 3}},
		},
	}
}

func rangeCheckWindow(value, lowBound, highBound uint8) []isa.Instruction {
	chainLo, chainHi := jumpOffset(program.RangeCheckFirstJumpDelta)
	return []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, lowBound, 10),
		mk(isa.OpGreaterOrEqualUint64, 5, value, lowBound),
		isa.NewTier1Instruction(isa.SubOpMoveInt64, 3, 5),
		mk(isa.OpJumpIfFalse, 3, chainLo, chainHi),
		isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, highBound, 20),
		mk(isa.OpLessOrEqualUint64, 7, value, highBound),
		isa.NewTier1Instruction(isa.SubOpMoveInt64, 3, 7),
		mk(isa.OpJumpIfFalse, 3, 0, 0),
	}
}

func buildFusionBody(w fusionWindow, fallThrough, taken []isa.Instruction) []isa.Instruction {
	body := append([]isa.Instruction(nil), w.window...)
	if w.branch {
		last := len(body) - 1
		lo, hi := jumpOffset(int16(len(fallThrough)))
		body[last] = mk(body[last].Op, body[last].A, lo, hi)
	}
	body = append(body, fallThrough...)
	if w.branch {
		body = append(body, taken...)
	}
	return body
}

func readOf(kind isa.RegisterKind, register uint8) isa.Instruction {
	switch kind {
	case isa.RegisterInt64:
		return mk(isa.OpAddInt64, unrelatedRegister, register, register)
	case isa.RegisterUint64:
		return mk(isa.OpAddUint64, unrelatedRegister, register, register)
	case isa.RegisterString:
		return mk(isa.OpConcatenateString, unrelatedRegister, register, register)
	case isa.RegisterGeneral:
		return mk(isa.OpMoveGeneral, unrelatedRegister, register, 0)
	default:
		panic(fmt.Sprintf("no reader for register kind %d", kind))
	}
}

func runFusion(t *testing.T, w fusionWindow, compiledFunction *program.CompiledFunction) bool {
	t.Helper()
	body := compiledFunction.Body
	original := append([]isa.Instruction(nil), body...)
	fused := w.fuser(compiledFunction, body, 0, len(body), BuildAllJumpTargets(body))
	if !fused {
		require.Equal(t, original, body, "a refused fusion leaves the body untouched")
	}
	return fused
}

func TestFusersFuseWhenEveryDroppedWriteIsDead(t *testing.T) {
	t.Parallel()
	for _, w := range fusionWindowsThatDropWrites() {
		t.Run(w.name, func(t *testing.T) {
			t.Parallel()
			body := buildFusionBody(w, []isa.Instruction{returnVoid()}, []isa.Instruction{returnVoid()})

			require.True(t, runFusion(t, w, &program.CompiledFunction{Body: body}),
				"nothing reads the dropped registers, so the fusion is sound")
		})
	}
}

func TestFusersRefuseWhenDroppedRegisterIsRead(t *testing.T) {
	t.Parallel()
	for _, w := range fusionWindowsThatDropWrites() {
		for _, write := range w.dropped {
			t.Run(fmt.Sprintf("%s/read after the window/kind %d register %d", w.name, write.kind, write.register), func(t *testing.T) {
				t.Parallel()
				body := buildFusionBody(w, []isa.Instruction{readOf(write.kind, write.register), returnVoid()}, []isa.Instruction{returnVoid()})

				require.False(t, runFusion(t, w, &program.CompiledFunction{Body: body}))
			})
			if !w.branch {
				continue
			}
			t.Run(fmt.Sprintf("%s/read on the taken edge/kind %d register %d", w.name, write.kind, write.register), func(t *testing.T) {
				t.Parallel()
				body := buildFusionBody(w, []isa.Instruction{returnVoid()}, []isa.Instruction{readOf(write.kind, write.register), returnVoid()})

				require.False(t, runFusion(t, w, &program.CompiledFunction{Body: body}))
			})
		}
	}
}

func TestFusersRefuseWhenDroppedRegisterIsAResultSlot(t *testing.T) {
	t.Parallel()
	for _, w := range fusionWindowsThatDropWrites() {
		for _, write := range w.dropped {
			t.Run(fmt.Sprintf("%s/kind %d register %d", w.name, write.kind, write.register), func(t *testing.T) {
				t.Parallel()
				resultKinds := make([]isa.RegisterKind, int(write.register)+1)
				for index := range resultKinds {
					resultKinds[index] = write.kind
				}
				body := buildFusionBody(w, []isa.Instruction{returnVoid()}, []isa.Instruction{returnVoid()})

				require.False(t, runFusion(t, w, &program.CompiledFunction{Body: body, ResultKinds: resultKinds}),
					"every return reads a result slot, so its write is never dead")
			})
		}
	}
}

func TestFusersRefuseWhenDroppedRegisterIsPassedToACall(t *testing.T) {
	t.Parallel()
	for _, w := range fusionWindowsThatDropWrites() {
		for _, write := range w.dropped {
			t.Run(fmt.Sprintf("%s/kind %d register %d", w.name, write.kind, write.register), func(t *testing.T) {
				t.Parallel()
				call := isa.NewTier1Instruction(isa.SubOpCall, 0, 0)
				site := program.CallSite{Arguments: []program.VarLocation{{Register: write.register, Kind: write.kind}}}
				body := buildFusionBody(w, []isa.Instruction{call, returnVoid()}, []isa.Instruction{returnVoid()})

				require.False(t, runFusion(t, w, &program.CompiledFunction{Body: body, CallSites: []program.CallSite{site}}))
			})
		}
	}
}

func TestFusersRefuseOperandsAliasingADroppedRegister(t *testing.T) {
	t.Parallel()
	branchTail := func(window []isa.Instruction) []isa.Instruction {
		body := append([]isa.Instruction(nil), window...)
		lo, hi := jumpOffset(1)
		last := len(body) - 1
		body[last] = mk(body[last].Op, body[last].A, lo, hi)
		return append(body, returnVoid(), returnVoid())
	}
	aliasedRangeCheck := rangeCheckWindow(4, 4, 6)
	cases := []struct {
		name  string
		fuser fuserFunc
		body  []isa.Instruction
	}{
		{
			name:  "a rune concatenated onto itself",
			fuser: FuseConcatRune,
			body:  []isa.Instruction{isa.NewTier1Instruction(isa.SubOpConvertRuneToString, 4, 5), mk(isa.OpConcatenateString, 1, 4, 4), returnVoid()},
		},
		{
			name:  "a string constant compared with itself",
			fuser: FuseThreeInstrPatterns,
			body:  branchTail([]isa.Instruction{mk(isa.OpLoadStringConstant, 4, 0, 0), mk(isa.OpEqualString, 3, 4, 4), mk(isa.OpJumpIfFalse, 3, 0, 0)}),
		},
		{
			name:  "nil compared with itself",
			fuser: FuseThreeInstrPatterns,
			body:  branchTail([]isa.Instruction{isa.NewTier2Instruction(isa.SubOpTier2LoadNil, 4), mk(isa.OpEqualGeneral, 3, 4, 4), mk(isa.OpJumpIfTrue, 3, 0, 0)}),
		},
		{
			name:  "a uint constant compared with itself",
			fuser: FuseThreeInstrPatterns,
			body:  branchTail([]isa.Instruction{isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, 4, 7), mk(isa.OpEqualUint64, 3, 4, 4), mk(isa.OpJumpIfFalse, 3, 0, 0)}),
		},
		{
			name:  "an int constant compared with itself",
			fuser: FuseThreeInstrPatterns,
			body:  branchTail([]isa.Instruction{mk(isa.OpLoadInt64Constant, 4, 0, 0), mk(isa.OpLessThanInt64, 3, 4, 4), mk(isa.OpJumpIfFalse, 3, 0, 0)}),
		},
		{
			name:  "a string length compared with itself",
			fuser: FuseThreeInstrPatterns,
			body:  branchTail([]isa.Instruction{isa.NewTier1Instruction(isa.SubOpLengthString, 4, 1), mk(isa.OpLessThanInt64, 3, 4, 4), mk(isa.OpJumpIfFalse, 3, 0, 0)}),
		},
		{
			name:  "a compare that overwrites the incremented counter",
			fuser: FuseThreeInstrPatterns,
			body:  branchTail([]isa.Instruction{isa.NewTier2Instruction(isa.SubOpTier2IncrementInt64, 1), mk(isa.OpLessThanInt64, 1, 1, 2), mk(isa.OpJumpIfTrue, 1, 0, 0)}),
		},
		{
			name:  "a range check whose value is its own lower bound",
			fuser: fuseLongPatterns,
			body:  branchTail(aliasedRangeCheck),
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			w := fusionWindow{fuser: testCase.fuser}

			require.False(t, runFusion(t, w, &program.CompiledFunction{Body: testCase.body}),
				"the fused instruction would read a register the rewrite no longer writes")
		})
	}
}

func TestFuseConcatRuneRegressions(t *testing.T) {
	t.Parallel()
	const (
		temporary    = 4
		runeRegister = 5
		target       = 1
	)
	convert := isa.NewTier1Instruction(isa.SubOpConvertRuneToString, temporary, runeRegister)
	backLo, backHi := jumpOffset(-3)
	cases := []struct {
		name  string
		body  []isa.Instruction
		fused bool
	}{
		{

			name:  "the temporary is read again after the concatenation",
			body:  []isa.Instruction{convert, mk(isa.OpConcatenateString, target, target, temporary), mk(isa.OpConcatenateString, 2, target, temporary), returnVoid()},
			fused: false,
		},
		{
			name:  "the concatenation overwrites the temporary itself",
			body:  []isa.Instruction{convert, mk(isa.OpConcatenateString, temporary, target, temporary), readOf(isa.RegisterString, temporary), returnVoid()},
			fused: true,
		},
		{
			name:  "a loop reconverts the rune before the temporary is read again",
			body:  []isa.Instruction{convert, mk(isa.OpConcatenateString, target, target, temporary), isa.NewTier1Instruction(isa.SubOpJump, backLo, backHi), returnVoid()},
			fused: true,
		},
		{
			name:  "the temporary is overwritten before it is read again",
			body:  []isa.Instruction{convert, mk(isa.OpConcatenateString, target, target, temporary), isa.NewTier1Instruction(isa.SubOpMoveString, temporary, 2), readOf(isa.RegisterString, temporary), returnVoid()},
			fused: true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			w := fusionWindow{fuser: FuseConcatRune}

			require.Equal(t, testCase.fused, runFusion(t, w, &program.CompiledFunction{Body: testCase.body}))
		})
	}
}

func TestFuseAppendMoveWhenTheTemporaryIsTheSlice(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{mk(isa.OpAppend, 2, 2, 3), mk(isa.OpMoveGeneral, 2, 2, 0), readOf(isa.RegisterGeneral, 2), returnVoid()}

	require.True(t, runFusion(t, fusionWindow{fuser: FuseAppendMove}, &program.CompiledFunction{Body: body}),
		"the fused append still writes the register, so a later read sees the same value")
}

func TestNonIntWalkSkipsExtensionsThatNameOnlyIntRegisters(t *testing.T) {
	t.Parallel()
	const register = 3
	cases := []struct {
		name string
		kind isa.RegisterKind
		tail []isa.Instruction
		dead bool
	}{
		{

			name: "an array field read whose index register shares the number",
			kind: isa.RegisterGeneral,
			tail: []isa.Instruction{mk(isa.OpGetStructFieldElementGeneral, 2, 1, 0), mk(isa.OpExtension, 16, 0, register), mk(isa.OpExtension, 16, 0, 20), returnVoid()},
			dead: true,
		},
		{
			name: "a map construction whose size hint shares the number",
			kind: isa.RegisterGeneral,
			tail: []isa.Instruction{isa.NewTier2Instruction(isa.SubOpTier2MakeMap, 5), mk(isa.OpExtension, 0, 0, register), returnVoid()},
			dead: true,
		},
		{
			name: "a struct literal whose type index shares the number",
			kind: isa.RegisterGeneral,
			tail: []isa.Instruction{isa.NewTier2Instruction(isa.SubOpTier2AllocateStructLiteral, 5), mk(isa.OpExtension, register, 0, 0), returnVoid()},
			dead: true,
		},
		{

			name: "a typed slice read whose int index shares the number",
			kind: isa.RegisterUint64,
			tail: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpGetSliceByteElement, 5, 1), mk(isa.OpExtension, register, 0, 0), returnVoid()},
			dead: true,
		},
		{
			name: "a slice expression whose bound flags share the number",
			kind: isa.RegisterUint64,
			tail: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSliceGeneral, 2, 1), mk(isa.OpExtension, register, 0, 0), returnVoid()},
			dead: true,
		},
		{
			name: "a struct fast append whose type index shares the number",
			kind: isa.RegisterUint64,
			tail: []isa.Instruction{mk(isa.OpAppendStruct, 5, 6, 7), mk(isa.OpExtension, register, 0, 0), returnVoid()},
			dead: true,
		},
		{
			name: "a struct slice field read whose layout index shares the number",
			kind: isa.RegisterUint64,
			tail: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpGetStructFieldSliceString, 5, 1), mk(isa.OpExtension, register, 0, 0), returnVoid()},
			dead: true,
		},
		{
			name: "a struct field write whose layout index shares the number",
			kind: isa.RegisterString,
			tail: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSetStructFieldStringWide, 1, 5), mk(isa.OpExtension, register, 0, 0), returnVoid()},
			dead: true,
		},
		{
			name: "a typed string slice store reads the string its extension names",
			kind: isa.RegisterString,
			tail: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSetSliceStringElement, 5, 1), mk(isa.OpExtension, register, 0, 0), returnVoid()},
			dead: false,
		},
		{
			name: "a typed float store cannot read a general register",
			kind: isa.RegisterGeneral,
			tail: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSetSliceFloat64Element, 5, 1), mk(isa.OpExtension, register, 0, 0), returnVoid()},
			dead: true,
		},
		{
			name: "a typed float store reads its float source",
			kind: isa.RegisterFloat64,
			tail: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpSetSliceFloat64Element, 5, 1), mk(isa.OpExtension, register, 0, 0), returnVoid()},
			dead: false,
		},
		{
			name: "a deferred call can pass the general register",
			kind: isa.RegisterGeneral,
			tail: []isa.Instruction{mk(isa.OpDefer, 5, 1, 0), mk(isa.OpExtension, 0, register, uint8(isa.RegisterGeneral)), returnVoid()},
			dead: false,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: testCase.tail}

			require.Equal(t, testCase.dead, registerDeadFrom(compiledFunction, testCase.tail, 0, testCase.kind, register))
		})
	}
}

func TestGeneralWalkSeesTheAppendedElement(t *testing.T) {
	t.Parallel()
	const element = 3
	body := []isa.Instruction{mk(isa.OpAppend, 1, 2, element), returnVoid()}

	require.False(t, registerDeadFrom(&program.CompiledFunction{Body: body}, body, 0, isa.RegisterGeneral, element),
		"append reads its element from general[C]")
}

func TestFuseAppendMoveRefusesWhenTheTemporaryIsAppendedAsAnElement(t *testing.T) {
	t.Parallel()

	body := []isa.Instruction{
		mk(isa.OpAppend, 6, 2, 3),
		mk(isa.OpMoveGeneral, 2, 6, 0),
		mk(isa.OpAppend, 7, 8, 6),
		returnVoid(),
	}

	require.False(t, runFusion(t, fusionWindow{fuser: FuseAppendMove}, &program.CompiledFunction{Body: body}))
}

func TestWalkFollowsTheTakenEdgeOfAGetMapOkBranch(t *testing.T) {
	t.Parallel()
	const (
		register   = 5
		okRegister = 13
	)
	lo, hi := jumpOffset(2)
	body := []isa.Instruction{
		mk(isa.OpGetMapInt64Int64OkThenJumpIfFalse, 10, 11, 12),
		mk(isa.OpExtension, okRegister, lo, hi),
		mk(isa.OpNoOperation, 0, 0, 0),
		returnVoid(),
		readOf(isa.RegisterInt64, register),
		returnVoid(),
	}

	require.False(t, registerDeadFrom(&program.CompiledFunction{Body: body}, body, 0, isa.RegisterInt64, register),
		"a missing key jumps to the read")
}

func TestWalkTreatsANegativeBranchTargetAsLive(t *testing.T) {
	t.Parallel()
	const register = 5
	lo, hi := jumpOffset(-4)
	body := []isa.Instruction{
		mk(isa.OpNoOperation, 0, 0, 0),
		mk(isa.OpJumpIfTrue, 9, lo, hi),
		returnVoid(),
	}

	require.False(t, registerDeadFrom(&program.CompiledFunction{Body: body}, body, 0, isa.RegisterInt64, register),
		"a jump before the start of the body proves nothing, and must not index outside it")
}

func TestWalkSeesAnExtensionWordReadOfTheRegisterItsInstructionWrites(t *testing.T) {
	t.Parallel()
	const register = 5
	body := []isa.Instruction{
		mk(isa.OpGetStructFieldElementField, register, 2, 0),
		mk(isa.OpExtension, 0, 0, register),
		returnVoid(),
	}
	require.False(t, registerDeadFrom(&program.CompiledFunction{Body: body}, body, 0, isa.RegisterInt64, register),
		"the element read indexes with the register before it overwrites it")

	body[1] = mk(isa.OpExtension, 0, 0, register+1)
	require.True(t, registerDeadFrom(&program.CompiledFunction{Body: body}, body, 0, isa.RegisterInt64, register),
		"with another index the write is the first thing that happens to the register")
}
