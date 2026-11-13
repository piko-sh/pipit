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

package verify

import (
	"bufio"
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type countingContext struct {
	context.Context
	live   int
	checks int
}

func checkerAccepts(t *testing.T, checkerProgram *CheckerProgram) bool {
	t.Helper()
	accepted, err := checkerProgram.checkLicenceGrants(t.Context())
	require.NoError(t, err)
	return accepted
}

func parseCheckerPrograms(t *testing.T, text string) []*CheckerProgram {
	t.Helper()
	var out []*CheckerProgram
	var cur *CheckerProgram
	ints := func(s string) []int {
		fields := strings.Fields(s)
		xs := make([]int, 0, len(fields))
		for _, f := range fields {
			x, err := strconv.Atoi(f)
			require.NoError(t, err)
			xs = append(xs, x)
		}
		return xs
	}
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 1<<20), 1<<26)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
		case strings.HasPrefix(line, "func "):
			fields := strings.Fields(line)
			n, _ := strconv.Atoi(fields[2])
			cur = &CheckerProgram{Name: fields[1], Registers: n}
		case line == "end":
			out = append(out, cur)
			cur = nil
		default:
			lhs, rhs, _ := strings.Cut(line, ";")
			words := strings.Fields(lhs)
			var instr CheckerInstr
			switch words[0] {
			case "nop":
			case "alloc":
				instr = CheckerInstr{Kind: CheckerAlloc, Destination: ints(words[1])[0]}
			case "move":
				instr = CheckerInstr{Kind: CheckerMove, Destination: ints(words[1])[0], Source: ints(words[2])[0]}
			case "store":
				instr = CheckerInstr{Kind: CheckerStore, Receiver: ints(words[1])[0], Source: ints(words[2])[0], Granted: words[3] == "1"}
			case "calloc":
				instr = CheckerInstr{Kind: CheckerAlloc, Destination: ints(words[1])[0], Container: true}
			case "cstore":
				instr = CheckerInstr{Kind: CheckerStore, Receiver: ints(words[1])[0], Source: ints(words[2])[0], Granted: true, Container: true}
			case "vstore":
				instr = CheckerInstr{Kind: checkerStoreValue, Receiver: ints(words[1])[0], Granted: words[2] == "1", Container: words[3] == "1"}
			case "cvstore":
				instr = CheckerInstr{Kind: checkerStoreValue, Receiver: ints(words[1])[0], Granted: true, Container: true}
			case "read":
				srcs, dsts, _ := strings.Cut(strings.Join(words[2:], " "), "/")
				instr = CheckerInstr{Kind: CheckerRead, Receiver: ints(words[1])[0], Sources: ints(srcs), Destinations: ints(dsts)}
			case "op":
				srcs, dsts, _ := strings.Cut(strings.Join(words[1:], " "), "/")
				instr = CheckerInstr{Kind: CheckerOp, Sources: ints(srcs), Destinations: ints(dsts)}
			case "havoc":
				instr = CheckerInstr{Kind: checkerHavoc, Sources: ints(strings.Join(words[1:], " "))}
			case "unclassified":
				instr = CheckerInstr{Kind: checkerUnclassified}
			default:
				require.Failf(t, "bad instruction", "%q", line)
			}
			cur.Code = append(cur.Code, instr)
			cur.Successors = append(cur.Successors, ints(rhs))
		}
	}
	require.NoError(t, scanner.Err())
	return out
}

func TestLicenceCheckerControls(t *testing.T) {
	programs := parseCheckerPrograms(t, `func right_grant 2 4
alloc 0 ; 1
store 0 1 1 ; 2
op 0 / ; 3
nop ;
end
func wrong_grant 2 4
alloc 0 ; 1
op 0 / ; 2
store 0 1 1 ; 3
nop ;
end
func unclassified 1 2
unclassified ; 1
nop ;
end
`)
	require.True(t, checkerAccepts(t, programs[0]), "a grant into a fresh, unescaped object is accepted")
	require.False(t, checkerAccepts(t, programs[1]), "a grant into an escaped object is rejected")
	require.False(t, checkerAccepts(t, programs[2]), "an unclassified instruction is rejected")
}

func (c *countingContext) Err() error {
	c.checks++
	if c.checks > c.live {
		return context.Canceled
	}
	return nil
}

func straightLineProgram(length int) *CheckerProgram {
	checkerProgram := &CheckerProgram{Name: "straight", Registers: 1}
	for pc := range length {
		checkerProgram.Code = append(checkerProgram.Code, CheckerInstr{Kind: CheckerOp})
		if pc+1 < length {
			checkerProgram.Successors = append(checkerProgram.Successors, []int{pc + 1})
		} else {
			checkerProgram.Successors = append(checkerProgram.Successors, nil)
		}
	}
	return checkerProgram
}

func TestLicenceCheckerStopsWhenCancelled(t *testing.T) {
	stop := errors.New("compile abandoned")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(stop)

	verdict, err := straightLineProgram(4).CheckLicenceGrantsVerdict(ctx)

	require.ErrorIs(t, err, stop, "the check returns the context's cause")
	require.Equal(t, checkerRejectedCertificate, verdict, "a cut check never accepts")
}

func TestLicenceCheckerChecksCancellationDuringTheFixpoint(t *testing.T) {
	ctx := &countingContext{Context: t.Context(), live: 1}

	verdict, err := straightLineProgram(3 * checkerCancelCheckSteps).CheckLicenceGrantsVerdict(ctx)

	require.ErrorIs(t, err, context.Canceled, "a cancellation during the fixpoint stops it")
	require.Equal(t, checkerRejectedCertificate, verdict)
	require.Equal(t, 2, ctx.checks, "checked at the start and again after checkerCancelCheckSteps steps")
}

func TestLicenceCheckerFollowsMovesJoinsAndHavoc(t *testing.T) {
	programs := parseCheckerPrograms(t, `func moved 2 3
alloc 0 ; 1
move 1 0 ; 2
store 1 0 1 ;
end
func same_site_on_both_paths 2 5
alloc 0 ; 1 2
nop ; 3
nop ; 3
store 0 1 1 ; 4
nop ;
end
func different_sites_on_two_paths 2 5
alloc 0 ; 1 2
alloc 0 ; 3
nop ; 3
store 0 1 1 ; 4
nop ;
end
func havoc 2 3
alloc 0 ; 1
havoc 1 ; 2
store 0 1 1 ;
end
func loop 1 2
alloc 0 ; 1
nop ; 0
end
`)
	require.True(t, checkerAccepts(t, programs[0]), "a move carries the fresh allocation to its destination")
	require.True(t, checkerAccepts(t, programs[1]), "a join keeps a site both paths agree on")
	require.False(t, checkerAccepts(t, programs[2]), "a join of two sites is no longer fresh")
	require.False(t, checkerAccepts(t, programs[3]), "havoc forgets every register")
	require.True(t, checkerAccepts(t, programs[4]), "a loop reaches a fixpoint")
}

func TestLicenceCheckerRejectsMalformedPrograms(t *testing.T) {
	cases := []struct {
		program *CheckerProgram
		name    string
		want    CheckerVerdict
	}{
		{name: "no code", program: &CheckerProgram{Name: "empty", Registers: 1}, want: checkerRejectedEmpty},
		{name: "a successor past the end", program: &CheckerProgram{Registers: 1, Code: []CheckerInstr{{Kind: checkerNop}}, Successors: [][]int{{5}}}, want: checkerRejectedCertificate},
		{name: "a register out of range", program: &CheckerProgram{Registers: 1, Code: []CheckerInstr{{Kind: CheckerAlloc, Destination: 3}}, Successors: [][]int{nil}}, want: CheckerRejectedInstruction},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			verdict, err := testCase.program.CheckLicenceGrantsVerdict(t.Context())
			require.NoError(t, err)
			require.Equal(t, testCase.want, verdict)
		})
	}
}

func TestCheckCertificateRejectsAStateOfTheWrongWidth(t *testing.T) {
	checkerProgram := &CheckerProgram{Registers: 2, Code: []CheckerInstr{{Kind: checkerNop}}, Successors: [][]int{nil}}
	entry := checkerState{registers: []int{checkerTop}}
	live := liveOf(t, checkerProgram)
	require.Equal(t, checkerRejectedCertificate, checkerProgram.checkCertificate(entry, []*checkerState{{registers: []int{checkerTop}}}, live))
	require.Equal(t, checkerRejectedCertificate, checkerProgram.checkCertificate(entry, []*checkerState{nil}, live), "an unreached entry")
}

func TestCheckerInstrOKChecksEveryRegister(t *testing.T) {
	cases := []struct {
		name  string
		instr CheckerInstr
		want  bool
	}{
		{name: "alloc in range", instr: CheckerInstr{Kind: CheckerAlloc, Destination: 1}, want: true},
		{name: "move source out of range", instr: CheckerInstr{Kind: CheckerMove, Destination: 0, Source: 2}, want: false},
		{name: "store receiver negative", instr: CheckerInstr{Kind: CheckerStore, Receiver: -1, Source: 0}, want: false},
		{name: "op destination out of range", instr: CheckerInstr{Kind: CheckerOp, Sources: []int{0}, Destinations: []int{2}}, want: false},
		{name: "havoc source out of range", instr: CheckerInstr{Kind: checkerHavoc, Sources: []int{2}}, want: false},
		{name: "havoc in range", instr: CheckerInstr{Kind: checkerHavoc, Sources: []int{0, 1}}, want: true},
		{name: "nop", instr: CheckerInstr{Kind: checkerNop}, want: true},
		{name: "unclassified", instr: CheckerInstr{Kind: checkerUnclassified}, want: false},
		{name: "an unknown kind", instr: CheckerInstr{Kind: CheckerInstrKind(99)}, want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, checkerInstrOK(2, testCase.instr))
		})
	}
}

func TestCheckerStateOrder(t *testing.T) {
	fresh := checkerState{registers: []int{0, checkerTop}}
	escaped := checkerState{registers: []int{checkerTop, checkerTop}, escaped: []int{0}}
	require.True(t, fresh.le(escaped), "a register holding a site is below checkerTop once the site is marked")
	require.False(t, escaped.le(fresh), "checkerTop is not below a site")
	require.False(t, fresh.le(checkerState{registers: []int{checkerTop, checkerTop}}), "an unmarked site is not below checkerTop")
	require.False(t, fresh.le(checkerState{registers: []int{0}}), "states of different widths are unordered")
	require.False(t, escaped.le(checkerState{registers: []int{checkerTop, checkerTop}}), "a mark the upper state lacks")
	require.Equal(t, checkerTop, fresh.get(5), "a register out of range reads checkerTop")

	joined := checkerJoin(checkerState{registers: []int{0, 1}, escaped: []int{3}}, checkerState{registers: []int{0, 2}, escaped: []int{3}})
	require.Equal(t, []int{0, checkerTop}, joined.registers, "agreeing sites stay, disagreeing ones are checkerTop")
	require.ElementsMatch(t, []int{1, 2, 3}, joined.escaped, "the disagreeing sites are marked, each mark once")
}

func TestWidenCertificateIgnoresASuccessorOutOfRange(t *testing.T) {
	certificate := []*checkerState{nil}
	require.False(t, widenCertificate(certificate, 1, checkerState{}))
	require.False(t, widenCertificate(certificate, -1, checkerState{}))
	require.Nil(t, certificate[0])
}

func TestCheckerVerdictNames(t *testing.T) {
	names := map[CheckerVerdict]string{
		CheckerAccepted:            "accepted",
		CheckerRejectedGrant:       "rejected-grant",
		CheckerRejectedInstruction: "rejected-instruction",
		checkerRejectedCertificate: "rejected-certificate",
		checkerRejectedEmpty:       "rejected-empty",
		CheckerVerdict(99):         "unknown",
	}
	for verdict, name := range names {
		require.Equal(t, name, verdict.String())
	}
}

func TestInferenceSkipsAFunctionThatMakesNoMap(t *testing.T) {
	body := make([]isa.Instruction, 0, 65)
	for range 64 {
		body = append(body, isa.NewInstruction(isa.OpSetMapStringGeneral, 0, 0, 1))
	}
	body = append(body, isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid))
	function := mapFillFunction(body)
	conversion := testing.AllocsPerRun(10, func() { convertForLicenceChecker(function, checkerGrants{every: true}) })

	inference := testing.AllocsPerRun(10, func() { inferGrants(t, function) })

	require.Nil(t, function.LocalContainerStorePCs)
	require.LessOrEqual(t, inference, conversion, "a store into a map the function did not make is never granted, so no certificate is proposed")
}

func TestInferenceHandsTheCheckItsConversionOnlyWhenNoGrantCanChangeIt(t *testing.T) {
	noMapOperation := mapFillFunction([]isa.Instruction{
		isa.NewInstruction(isa.OpMoveGeneral, 1, 0, 0),
		isa.NewInstruction(isa.OpJumpIfFalse, 0, 0, 0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	})
	require.True(t, program.SetJumpTarget(noMapOperation.Body, 1, 0))
	inferred, err := inferLocalContainerGrants(t.Context(), noMapOperation)
	require.NoError(t, err)
	require.Equal(t, ConvertForLicenceChecker(noMapOperation), inferred.conversion,
		"with no map allocation and no map store the two conversions are the same program")
	require.Nil(t, inferred.live.sets)

	filled := mapFillFunction(mapFillBody(t))
	inferred, err = inferLocalContainerGrants(t.Context(), filled)
	require.NoError(t, err)
	require.Nil(t, inferred.conversion, "a granted function is converted again under its grants")
	require.NotNil(t, inferred.live.sets)

	storeOnly := mapFillFunction([]isa.Instruction{
		isa.NewInstruction(isa.OpSetMapStringGeneral, 0, 0, 1),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	})
	inferred, err = inferLocalContainerGrants(t.Context(), storeOnly)
	require.NoError(t, err)
	require.Nil(t, inferred.conversion, "the inference takes the store as granted, the check does not")
	require.NotEqual(t, ConvertForLicenceChecker(storeOnly),
		convertForLicenceChecker(storeOnly, checkerGrants{allocs: nil, stores: nil, every: true}))

	allocOnly := mapFillFunction([]isa.Instruction{
		isa.NewTier2Instruction(isa.SubOpTier2MakeMap, 0),
		isa.NewInstruction(isa.OpExtension, 0, 0, 0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	})
	inferred, err = inferLocalContainerGrants(t.Context(), allocOnly)
	require.NoError(t, err)
	require.Nil(t, inferred.conversion, "the inference takes the allocation as a map site, the check does not")

	for _, function := range []*program.CompiledFunction{noMapOperation, filled, storeOnly, allocOnly} {
		verdict, err := CheckLicence(t.Context(), function, true)
		require.NoError(t, err)
		require.Equal(t, CheckerAccepted, verdict)
	}
	require.Equal(t, program.PCSetOf(3), filled.LocalContainerStorePCs)
	require.Nil(t, storeOnly.LocalContainerStorePCs)
}

func TestLicenceCheckerGrantsAStoreOfAValueThatIsNoObject(t *testing.T) {
	programs := parseCheckerPrograms(t, `func entry_grown_in_place 1 4
calloc 0 ; 1
cvstore 0 ; 2
nop ; 1 3
nop ;
end
func entry_grown_after_a_hand_off 1 4
calloc 0 ; 1
op 0 / ; 2
cvstore 0 ; 3
nop ;
end
func entry_grown_in_a_map_from_anywhere 1 2
cvstore 0 ; 1
nop ;
end
`)
	require.True(t, checkerAccepts(t, programs[0]), "a value store marks nothing, so the map stays held alone around the loop")
	require.False(t, checkerAccepts(t, programs[1]), "a map handed off before the store is not held alone")
	require.False(t, checkerAccepts(t, programs[2]), "a map the function did not make is not held alone")
}
