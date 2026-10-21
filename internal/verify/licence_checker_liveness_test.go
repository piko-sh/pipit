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
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const readsCoverageGenerals = 32

var readsCoverageOperands = [isa.NumInstructionOperands]uint8{3, 4, 5}

var readsCoverageExtension = isa.Instruction{Op: isa.OpExtension, A: 6, B: 7, C: 8}

func readsCoverageInstruction(row isa.OpSpec) isa.Instruction {
	var instr isa.Instruction
	switch row.Tier {
	case isa.TierMain:
		instr = isa.Instruction{Op: isa.Opcode(row.Code), A: readsCoverageOperands[0], B: readsCoverageOperands[1], C: readsCoverageOperands[2]}
	case isa.TierSub1:
		instr = isa.Instruction{Op: isa.OpDrillTier1, A: row.Code, B: readsCoverageOperands[1], C: readsCoverageOperands[2]}
	case isa.TierSub2:
		instr = isa.Instruction{Op: isa.OpDrillTier1, A: uint8(isa.SubOpDrillTier2), B: row.Code, C: readsCoverageOperands[2]}
	default:
		instr = isa.NewTier3Instruction(isa.SubOpcodeTier3(row.Code))
	}
	shape := isa.ShapeForInstruction(instr)
	roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
	operands := [isa.NumInstructionOperands]*uint8{&instr.A, &instr.B, &instr.C}
	for position, role := range roles {
		if role == isa.RoleKindMarker {
			*operands[position] = uint8(isa.RegisterGeneral)
		}
	}
	return instr
}

func readsEveryRegister(instr CheckerInstr) bool {
	return instr.Kind == checkerHavoc || instr.Kind == checkerUnclassified
}

func TestCheckerReadsCoverEveryGeneralRegisterTheSpecReads(t *testing.T) {
	t.Parallel()

	all := make([]int, readsCoverageGenerals)
	for register := range all {
		all[register] = register
	}
	for _, row := range isa.AllSpecs() {
		instr := readsCoverageInstruction(row)
		if instr.Op == isa.OpExtension {
			continue
		}
		shape := isa.ShapeForInstruction(instr)
		body := []isa.Instruction{instr, readsCoverageExtension}
		function := &program.CompiledFunction{Body: body, NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterGeneral: readsCoverageGenerals}}
		converted := convertOne(function, checkerGrants{}, 0, instr, shape, all)
		if readsEveryRegister(converted) {
			continue
		}
		reads := checkerReads(converted)
		roles := [isa.NumInstructionOperands]isa.OperandRole{shape.A, shape.B, shape.C}
		operands := [isa.NumInstructionOperands]uint8{instr.A, instr.B, instr.C}
		for position, role := range roles {
			if (role == isa.RoleRegGeneral || role == isa.RoleRegDynamic) && shape.Reads[position] {
				require.Contains(t, reads, int(operands[position]), "%s reads operand %d", row.Name, position)
			}
		}
		if shape.Flags&isa.ShapeFlagFollowsExtension == 0 {
			continue
		}
		extensionReads, _, known := isa.ExtensionRegisterUse(instr, body[1:], isa.RegisterGeneral)
		require.True(t, known, "%s consumes extension words whose general registers the ISA does not describe, so the checker must read every register", row.Name)
		extensionOperands := [isa.NumInstructionOperands]uint8{readsCoverageExtension.A, readsCoverageExtension.B, readsCoverageExtension.C}
		for position, read := range extensionReads {
			if read {
				require.Contains(t, reads, int(extensionOperands[position]), "%s reads extension byte %d", row.Name, position)
			}
		}
	}
}

func checkerLoopProgram(length, registers int) *CheckerProgram {
	loop := &CheckerProgram{Name: "loop", Registers: registers}
	for programCounter := range length - 1 {
		loop.Code = append(loop.Code, CheckerInstr{Kind: CheckerMove, Destination: programCounter % registers, Source: (programCounter + 3) % registers})
		loop.Successors = append(loop.Successors, []int{programCounter + 1})
	}
	loop.Code = append(loop.Code, CheckerInstr{Kind: checkerNop})
	loop.Successors = append(loop.Successors, []int{0})
	return loop
}

func TestLiveRegistersAllocatesPerFunctionNotPerStep(t *testing.T) {
	short := checkerLoopProgram(64, 8)
	long := checkerLoopProgram(512, 8)

	ctx := t.Context()
	shortAllocations := testing.AllocsPerRun(10, func() { _, _ = short.liveRegisters(ctx) })
	longAllocations := testing.AllocsPerRun(10, func() { _, _ = long.liveRegisters(ctx) })

	require.Equal(t, shortAllocations, longAllocations, "the sets share one backing array, and a worklist step allocates nothing")
	require.Less(t, longAllocations, 16.0)
}

func liveAt(live checkerLiveness, programCounter int) []bool {
	registers := make([]bool, live.registers)
	for register := range registers {
		registers[register] = live.live(programCounter, register)
	}
	return registers
}

func referenceLiveRegisters(p *CheckerProgram) [][]bool {
	live := make([][]bool, len(p.Code))
	for programCounter := range live {
		live[programCounter] = make([]bool, p.Registers)
	}
	for changed := true; changed; {
		changed = false
		for programCounter, instr := range slices.Backward(p.Code) {
			needed := make([]bool, p.Registers)
			if readsEveryRegister(instr) {
				for register := range needed {
					needed[register] = true
				}
			} else {
				for _, successor := range p.Successors[programCounter] {
					if successor >= 0 && successor < len(p.Code) {
						for register, isLive := range live[successor] {
							needed[register] = needed[register] || isLive
						}
					}
				}
				eachCheckerWrite(instr, func(register int) {
					if register >= 0 && register < p.Registers {
						needed[register] = false
					}
				})
				eachCheckerRead(instr, func(register int) {
					if register >= 0 && register < p.Registers {
						needed[register] = true
					}
				})
			}
			for register, isNeeded := range needed {
				if isNeeded && !live[programCounter][register] {
					live[programCounter][register] = true
					changed = true
				}
			}
		}
	}
	return live
}

func randomCheckerProgram(random *rand.Rand, length, registers int) *CheckerProgram {
	pick := func() int { return random.IntN(registers+2) - 1 }
	picks := func() []int {
		out := make([]int, random.IntN(3))
		for index := range out {
			out[index] = pick()
		}
		return out
	}
	generated := &CheckerProgram{Name: "random", Registers: registers}
	for range length {
		var instr CheckerInstr
		switch random.IntN(8) {
		case 0:
			instr = CheckerInstr{Kind: CheckerAlloc, Destination: pick(), Container: random.IntN(2) == 0}
		case 1:
			instr = CheckerInstr{Kind: CheckerMove, Destination: pick(), Source: pick()}
		case 2:
			instr = CheckerInstr{Kind: CheckerStore, Receiver: pick(), Source: pick(), Granted: random.IntN(2) == 0}
		case 3:
			instr = CheckerInstr{Kind: CheckerOp, Sources: picks(), Destinations: picks()}
		case 4:
			instr = CheckerInstr{Kind: CheckerRead, Receiver: pick(), Sources: picks(), Destinations: picks()}
		case 5:
			instr = CheckerInstr{Kind: checkerHavoc, Sources: picks()}
		case 6:
			instr = CheckerInstr{Kind: checkerUnclassified}
		default:
			instr = CheckerInstr{Kind: checkerNop}
		}
		generated.Code = append(generated.Code, instr)
		successors := make([]int, random.IntN(3))
		for index := range successors {
			successors[index] = random.IntN(length+2) - 1
		}
		generated.Successors = append(generated.Successors, successors)
	}
	return generated
}

func TestLiveRegistersAgreesWithASweepToAFixpoint(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(11, 6))
	for trial := range 500 {
		registers := 1 + random.IntN(140)
		generated := randomCheckerProgram(random, 1+random.IntN(40), registers)

		live := liveOf(t, generated)

		want := referenceLiveRegisters(generated)
		for programCounter := range generated.Code {
			require.Equal(t, want[programCounter], liveAt(live, programCounter), "trial %d, pc %d", trial, programCounter)
		}
		require.True(t, live.closed(generated), "trial %d", trial)
	}
}

func TestLicenceCheckReusesOnlyClosedLiveness(t *testing.T) {
	t.Parallel()
	random := rand.New(rand.NewPCG(5, 8))
	for trial := range 300 {
		generated := randomCheckerProgram(random, 1+random.IntN(30), 1+random.IntN(70))
		want, err := generated.CheckLicenceGrantsVerdict(t.Context())
		require.NoError(t, err)

		empty := newCheckerLiveness(len(generated.Code), generated.Registers)
		got, err := generated.checkLicenceGrantsVerdict(t.Context(), empty)
		require.NoError(t, err)
		require.Equal(t, want, got, "trial %d: sets that are not closed are replaced, not trusted", trial)

		full := newCheckerLiveness(len(generated.Code), generated.Registers)
		for _, set := range full.sets {
			bitsetFill(set, generated.Registers)
		}
		_, err = generated.checkLicenceGrantsVerdict(t.Context(), full)
		require.NoError(t, err, "trial %d: sets that keep every register live are closed and sound", trial)
	}
}

func TestCheckCertificateRejectsLivenessThatIsNotClosed(t *testing.T) {
	programs := parseCheckerPrograms(t, `func loop 3 6
calloc 0 ; 1
nop ; 2
move 2 0 ; 3
cstore 2 1 ; 4
nop ; 1 5
nop ;
end
`)
	converted := programs[0]
	live := liveOf(t, converted)
	entry := checkerState{registers: []int{checkerTop, checkerTop, checkerTop}}
	certificate, err := converted.proposeCertificate(t.Context(), entry, live)
	require.NoError(t, err)
	require.Equal(t, CheckerAccepted, converted.checkCertificate(entry, certificate, live))

	bitsetClear(live.sets[3], live.registers, 2)

	require.Equal(t, checkerRejectedCertificate, converted.checkCertificate(entry, certificate, live),
		"the check does not trust the sets the proposal used")
}

func TestLivenessStepsAllocateNothing(t *testing.T) {
	converted := checkerLoopProgram(64, 130)
	live := liveOf(t, converted)
	needed := make([]uint64, checkerLivenessWords(converted.Registers))

	allocations := testing.AllocsPerRun(20, func() {
		for programCounter := range converted.Code {
			converted.updateLiveIn(live, programCounter, needed)
		}
	})

	require.Zero(t, allocations)
}

func TestLiveRegistersFollowsReadsBackToTheirWrites(t *testing.T) {
	programs := parseCheckerPrograms(t, `func loop 3 6
calloc 0 ; 1
nop ; 2
move 2 0 ; 3
cstore 2 1 ; 4
nop ; 1 5
nop ;
end
`)
	live := liveOf(t, programs[0])

	require.Equal(t, []bool{false, true, false}, liveAt(live, 0), "the allocation overwrites its register; the stored value comes from outside")
	require.Equal(t, []bool{true, true, false}, liveAt(live, 1), "the copy is written before it is read on every path from the loop head")
	require.Equal(t, []bool{true, true, true}, liveAt(live, 3), "the store reads its receiver and its source")
	require.Equal(t, []bool{false, false, false}, liveAt(live, 5), "nothing is read after the exit")
}

func TestLiveRegistersKeepsEveryRegisterLiveAcrossWhatMayReadAny(t *testing.T) {
	programs := parseCheckerPrograms(t, `func through_a_havoc 3 3
calloc 0 ; 1
havoc ; 2
nop ;
end
func through_an_unclassified 3 3
calloc 0 ; 1
unclassified ; 2
nop ;
end
`)
	for _, converted := range programs {
		live := liveOf(t, converted)

		require.Equal(t, []bool{true, true, true}, liveAt(live, 1), "%s: it may read registers its operands do not name", converted.Name)
		require.Equal(t, []bool{false, true, true}, liveAt(live, 0), "%s: the allocation still overwrites its own register", converted.Name)
	}
}

func TestLivenessClosedRejectsSetsThatDropARegisterStillRead(t *testing.T) {
	programs := parseCheckerPrograms(t, `func loop 3 6
calloc 0 ; 1
nop ; 2
move 2 0 ; 3
cstore 2 1 ; 4
havoc ; 1 5
nop ;
end
`)
	converted := programs[0]
	require.True(t, liveOf(t, converted).closed(converted), "the computed sets are closed")

	for name, damage := range map[string]func(live checkerLiveness) checkerLiveness{
		"a register the instruction reads":            func(live checkerLiveness) checkerLiveness { bitsetClear(live.sets[3], live.registers, 2); return live },
		"a register a successor reads":                func(live checkerLiveness) checkerLiveness { bitsetClear(live.sets[1], live.registers, 1); return live },
		"a register before what may read any":         func(live checkerLiveness) checkerLiveness { bitsetClear(live.sets[4], live.registers, 0); return live },
		"a set for too few registers":                 func(live checkerLiveness) checkerLiveness { live.registers = 2; return live },
		"sets for too few instructions":               func(live checkerLiveness) checkerLiveness { live.sets = live.sets[:5]; return live },
		"a register the allocation's successor reads": func(live checkerLiveness) checkerLiveness { bitsetClear(live.sets[0], live.registers, 1); return live },
	} {
		t.Run(name, func(t *testing.T) {
			require.False(t, damage(liveOf(t, converted)).closed(converted))
		})
	}
}

func TestLivenessForgetsOnlyRegistersThatAreNotLive(t *testing.T) {
	live := newCheckerLiveness(1, 3)
	bitsetSet(live.sets[0], live.registers, 0)
	state := checkerState{registers: []int{4, 4, checkerTop}, escaped: []int{9}}

	forgotten := live.forget(0, state)

	require.Equal(t, []int{4, checkerTop, checkerTop}, forgotten.registers)
	require.Equal(t, []int{9}, forgotten.escaped, "forgetting a register marks nothing")
	require.Equal(t, []int{4, 4, checkerTop}, state.registers, "the state flowing in is left as it was")
	require.Equal(t, state, live.forget(7, state), "a pc out of range forgets nothing")
}

func liveOf(t *testing.T, checkerProgram *CheckerProgram) checkerLiveness {
	t.Helper()
	live, err := checkerProgram.liveRegisters(t.Context())
	require.NoError(t, err)
	return live
}

func TestLiveRegistersStopsWhenCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancelCause(t.Context())
	stop := errors.New("stopped")
	cancel(stop)
	_, err := checkerLoopProgram(64, 8).liveRegisters(ctx)
	require.ErrorIs(t, err, stop)
}
