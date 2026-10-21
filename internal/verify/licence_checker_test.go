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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func generalRegister(r uint8) program.VarLocation {
	return program.VarLocation{Register: r, Kind: isa.RegisterGeneral}
}

func callInstruction(index uint16) isa.Instruction {
	lo, hi := isa.SplitWide(index)
	return isa.NewTier1Instruction(isa.SubOpCall, lo, hi)
}

func requireSpec(t *testing.T, instruction isa.Instruction) isa.OpSpec {
	t.Helper()
	row, known := isa.SpecForInstruction(instruction)
	require.True(t, known, "%v has a row", instruction)
	return row
}

func TestConvertForLicenceCheckerMapsAGrantedAllocationAndStore(t *testing.T) {
	function := &program.CompiledFunction{
		Name: "fill",
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpLoadGeneralConstant, 0, 0, 0),
			isa.NewInstruction(isa.OpSetElement, 0, 0, 1),
			isa.NewInstruction(isa.OpMoveGeneral, 2, 0, 0),
			isa.NewInstruction(isa.OpSetMap, 2, 0, 1),
			isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
		},
		ArenaSafeAllocPCs:      map[int]bool{0: true},
		FieldStoreArenaSafePCs: map[int]bool{1: true},
	}
	function.NumRegisters[isa.RegisterGeneral] = 3

	converted := ConvertForLicenceChecker(function)

	require.Equal(t, "fill", converted.Name)
	require.Equal(t, 3, converted.Registers)
	require.Equal(t, 1, converted.Granted, "one granted store")
	require.Equal(t, CheckerInstr{Kind: CheckerAlloc, Destination: 0}, converted.Code[0], "a granted allocation with one write and no reads")
	require.Equal(t, CheckerInstr{Kind: CheckerStore, Receiver: 0, Source: 1, Granted: true}, converted.Code[1], "a granted store of the value into its receiver")
	require.Equal(t, CheckerInstr{Kind: CheckerMove, Destination: 2, Source: 0}, converted.Code[2], "a general move")
	require.Equal(t, CheckerInstr{Kind: CheckerOp, Sources: []int{2, 0, 1}, Destinations: nil}, converted.Code[3], "a container store of two values is an operation")
	require.Equal(t, checkerNop, converted.Code[4].Kind, "a return reads no general register")
	require.Equal(t, [][]int{{1}, {2}, {3}, {4}, nil}, converted.Successors)
	require.True(t, checkerAccepts(t, converted), "the grant into the fresh allocation holds")
}

func TestConvertForLicenceCheckerSkipsExtensionWords(t *testing.T) {
	function := &program.CompiledFunction{
		Name: "slice",
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpMakeSlice, 0, 0, 0),
			isa.NewInstruction(isa.OpExtension, 0, 0, 0),
			isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
		},
	}
	function.NumRegisters[isa.RegisterGeneral] = 1

	converted := ConvertForLicenceChecker(function)

	require.Equal(t, []int{2}, converted.Successors[0], "the extension word is not a successor")
	require.Equal(t, CheckerInstr{Kind: CheckerOp, Destinations: []int{0}}, converted.Code[0])
	require.Equal(t, CheckerInstr{}, converted.Code[1], "the extension word is never converted")
	require.Equal(t, checkerNop, convertOne(function, checkerGrants{}, 1, function.Body[1], isa.ShapeForInstruction(function.Body[1]), []int{0}).Kind,
		"an extension word reached on its own is a no-op")
}

func TestConvertOneClassifiesInstructions(t *testing.T) {
	all := []int{0, 1, 2}
	function := &program.CompiledFunction{
		CallSites: []program.CallSite{{
			Arguments: []program.VarLocation{generalRegister(1)},
			Returns:   []program.VarLocation{generalRegister(2)},
			IsNative:  true, NativeRegister: 0,
		}},
	}
	cases := []struct {
		instruction isa.Instruction
		want        CheckerInstr
		name        string
	}{
		{name: "an opcode with no row is unclassified", instruction: isa.NewInstruction(isa.Opcode(250), 0, 0, 0), want: CheckerInstr{Kind: checkerUnclassified}},
		{name: "a call with a recorded site reads its arguments and callee", instruction: callInstruction(0), want: CheckerInstr{Kind: CheckerOp, Sources: []int{1, 0}, Destinations: []int{2}}},
		{name: "a call with no recorded site is havoc", instruction: callInstruction(7), want: CheckerInstr{Kind: checkerHavoc, Sources: all}},
		{name: "an opcode writing registers it does not describe is havoc", instruction: isa.NewInstruction(isa.OpDefer, 1, 0, 0), want: CheckerInstr{Kind: checkerHavoc, Sources: all}},
		{name: "a container store of one value stores it", instruction: isa.NewInstruction(isa.OpSetElement, 0, 0, 1), want: CheckerInstr{Kind: CheckerStore, Receiver: 0, Source: 1}},
		{name: "any other described opcode is an operation", instruction: isa.NewInstruction(isa.OpMakeClosure, 2, 0, 0), want: CheckerInstr{Kind: CheckerOp, Destinations: []int{2}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, convertOne(function, checkerGrants{}, 0, testCase.instruction, isa.ShapeForInstruction(testCase.instruction), all))
		})
	}
}

func TestConvertDescribedFollowsTheGrantTables(t *testing.T) {
	load := isa.NewInstruction(isa.OpLoadGeneralConstant, 1, 0, 0)
	store := isa.NewInstruction(isa.OpSetElement, 0, 0, 1)
	operandsOf := func(instruction isa.Instruction, generals int) describedOperands {
		shape := isa.ShapeForInstruction(instruction)
		reads, writes := generalOperands(instruction, shape, generals)
		return describedOperands{instr: instruction, shape: shape, reads: reads, writes: writes}
	}
	granted := &program.CompiledFunction{ArenaSafeAllocPCs: map[int]bool{0: true}, FieldStoreArenaSafePCs: map[int]bool{1: true}}

	require.Equal(t, CheckerInstr{Kind: CheckerOp, Sources: []int{0, 1}}, convertDescribed(granted, 0, requireSpec(t, store), operandsOf(store, 2)),
		"a granted allocation that reads registers is an operation")
	require.Equal(t, CheckerInstr{Kind: checkerUnclassified}, convertDescribed(granted, 1, requireSpec(t, load), operandsOf(load, 2)),
		"a granted store with no receiver is unclassified")
	require.Equal(t, CheckerInstr{Kind: checkerUnclassified}, convertGrantedStore(describedOperands{instr: store, shape: isa.ShapeForInstruction(store), reads: []int{0, 1, 2}}),
		"a granted store of two values is unclassified")

	call := callInstruction(0)
	callShape := isa.ShapeForInstruction(call)
	require.Equal(t, CheckerInstr{Kind: checkerHavoc, Sources: []int{1}}, convertDescribed(&program.CompiledFunction{}, 0, requireSpec(t, call),
		describedOperands{instr: call, shape: callShape, reads: []int{1}}), "a described call hands its reads off and may change anything")
}

func TestConvertContainerStoreOfSeveralValuesIsAnOperation(t *testing.T) {
	store := isa.NewInstruction(isa.OpSetElement, 0, 0, 1)
	operands := describedOperands{instr: store, shape: isa.ShapeForInstruction(store), reads: []int{0, 1}, writes: []int{2}}
	require.Equal(t, CheckerInstr{Kind: CheckerOp, Sources: []int{0, 1}, Destinations: []int{2}}, convertContainerStore(operands),
		"a container store that also writes is an operation")
}

func TestConvertCallFallsBackWhenAnOperandIsNotAGeneralRegister(t *testing.T) {
	cases := []struct {
		site program.CallSite
		name string
	}{
		{name: "spilled argument", site: program.CallSite{Arguments: []program.VarLocation{{Kind: isa.RegisterGeneral, IsSpilled: true}}}},
		{name: "callee out of range", site: program.CallSite{IsClosure: true, ClosureRegister: 9}},
		{name: "result out of range", site: program.CallSite{Returns: []program.VarLocation{generalRegister(9)}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			function := &program.CompiledFunction{CallSites: []program.CallSite{testCase.site}}
			_, ok := convertCall(function, callInstruction(0), 3)
			require.False(t, ok)
		})
	}
}

func TestAppendGeneralLocationsKeepsOnlyGeneralRegisters(t *testing.T) {
	cases := []struct {
		name      string
		locations []program.VarLocation
		want      []int
		ok        bool
	}{
		{name: "general registers in range", locations: []program.VarLocation{generalRegister(0), generalRegister(2)}, want: []int{0, 2}, ok: true},
		{name: "another bank is skipped", locations: []program.VarLocation{{Register: 1, Kind: isa.RegisterInt64}, generalRegister(1)}, want: []int{1}, ok: true},
		{name: "an upvalue fails", locations: []program.VarLocation{{Kind: isa.RegisterGeneral, IsUpvalue: true}}, ok: false},
		{name: "a spilled location fails", locations: []program.VarLocation{{Kind: isa.RegisterGeneral, IsSpilled: true}}, ok: false},
		{name: "a register out of range fails", locations: []program.VarLocation{generalRegister(3)}, ok: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, ok := appendGeneralLocations(nil, testCase.locations, 3)
			require.Equal(t, testCase.ok, ok)
			if ok {
				require.Equal(t, testCase.want, got)
			}
		})
	}
}

func TestAppendCalleesListsTheRegistersTheSiteUses(t *testing.T) {
	site := &program.CallSite{IsNative: true, NativeRegister: 0, IsClosure: true, ClosureRegister: 1, IsMethod: true, MethodReceiverRegister: 2}
	got, ok := appendCallees([]int{7}, site, 3)
	require.True(t, ok)
	require.Equal(t, []int{7, 0, 1, 2}, got)

	got, ok = appendCallees(nil, &program.CallSite{}, 3)
	require.True(t, ok)
	require.Empty(t, got, "a site with no callee register adds none")

	_, ok = appendCallees(nil, &program.CallSite{IsMethod: true, MethodReceiverRegister: 3}, 3)
	require.False(t, ok, "a receiver out of range fails")
}

func TestGeneralOperandsCountsDynamicOperandsThatCanBeGeneral(t *testing.T) {
	shape := isa.OperandShape{
		A: isa.RoleRegDynamic, B: isa.RoleRegDynamic, C: isa.RoleRegGeneral,
		Reads:  [isa.NumInstructionOperands]bool{true, true, false},
		Writes: [isa.NumInstructionOperands]bool{false, false, true},
	}
	reads, writes := generalOperands(isa.Instruction{A: 1, B: 5, C: 2}, shape, 3)
	require.Equal(t, []int{1}, reads, "a dynamic operand below the general count may be general; one above cannot")
	require.Equal(t, []int{2}, writes)
}

func TestGeneralOperandsFollowsTheKindMarkerOfAGlobalAccess(t *testing.T) {
	testCases := []struct {
		name        string
		instruction isa.Instruction
		wantReads   []int
		wantWrites  []int
	}{
		{name: "store of an integer", instruction: isa.Instruction{Op: isa.OpSetGlobal, A: 1, B: 0, C: uint8(isa.RegisterInt64)}},
		{name: "store of a general", instruction: isa.Instruction{Op: isa.OpSetGlobal, A: 1, B: 0, C: uint8(isa.RegisterGeneral)}, wantReads: []int{1}},
		{name: "load of a string", instruction: isa.Instruction{Op: isa.OpGetGlobal, A: 2, B: 0, C: uint8(isa.RegisterString)}},
		{name: "load of a general", instruction: isa.Instruction{Op: isa.OpGetGlobal, A: 2, B: 0, C: uint8(isa.RegisterGeneral)}, wantWrites: []int{2}},
		{
			name:        "wide store of a float",
			instruction: isa.Instruction{Op: isa.OpDrillTier1, A: uint8(isa.SubOpSetGlobalWide), B: 1, C: uint8(isa.RegisterFloat64)},
		},
		{
			name:        "wide store of a general",
			instruction: isa.Instruction{Op: isa.OpDrillTier1, A: uint8(isa.SubOpSetGlobalWide), B: 1, C: uint8(isa.RegisterGeneral)},
			wantReads:   []int{1},
		},
		{
			name:        "boxing an integer still counts the dynamic operand",
			instruction: isa.Instruction{Op: isa.OpBoxAsInterface, A: 0, B: 1, C: uint8(isa.RegisterInt64)},
			wantReads:   []int{1}, wantWrites: []int{0},
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			reads, writes := generalOperands(testCase.instruction, isa.ShapeForInstruction(testCase.instruction), 3)
			require.Equal(t, testCase.wantReads, reads)
			require.Equal(t, testCase.wantWrites, writes)
		})
	}
}

func TestReceiverOperandIsAGeneralReadOfOperandA(t *testing.T) {
	recv, ok := receiverOperand(isa.Instruction{A: 4}, isa.OperandShape{A: isa.RoleRegDynamic, Reads: [isa.NumInstructionOperands]bool{true, false, false}})
	require.True(t, ok)
	require.Equal(t, 4, recv)
	_, ok = receiverOperand(isa.Instruction{A: 4}, isa.OperandShape{A: isa.RoleRegGeneral})
	require.False(t, ok, "operand A that is written, not read, is no receiver")
}

func TestSourcesExceptDropsTheReceiver(t *testing.T) {
	require.Equal(t, []int{1, 2}, sourcesExcept([]int{0, 1, 0, 2}, 0))
	require.Nil(t, sourcesExcept([]int{0}, 0))
}

func TestStoresIntoContainerNeedsOnlyContainerRoutes(t *testing.T) {
	require.True(t, storesIntoContainer(requireSpec(t, isa.NewInstruction(isa.OpSetMap, 0, 0, 0))), "a map store")
	require.False(t, storesIntoContainer(requireSpec(t, isa.NewInstruction(isa.OpMoveGeneral, 0, 0, 0))), "an opcode with no declared hand-offs")
	require.False(t, storesIntoContainer(requireSpec(t, isa.NewInstruction(isa.OpDefer, 0, 0, 0))), "a deferred call's route is not a container store")
}

func TestExtensionGeneralOperandsFollowsTheISA(t *testing.T) {
	t.Parallel()

	const generals = 4
	makeSlice := isa.NewTier1Instruction(isa.SubOpMakeSliceInt64, 1, 2)
	deferred := isa.NewInstruction(isa.OpDefer, 0, 0, 0)
	fieldStore := isa.NewInstruction(isa.OpSetElementFieldInt64, 0, 1, 2)
	tests := []struct {
		name       string
		body       []isa.Instruction
		wantWrites []int
		wantKnown  bool
	}{
		{name: "a capacity register of the int bank", body: []isa.Instruction{makeSlice, readsCoverageExtension}, wantKnown: true},
		{name: "words the ISA does not describe may name a general register", body: []isa.Instruction{deferred, readsCoverageExtension}},
		{name: "an operation with no extension word names none", body: []isa.Instruction{makeSlice}, wantKnown: true},
		{
			name:       "the register an element field store may leave the element in",
			body:       []isa.Instruction{fieldStore, isa.NewInstruction(isa.OpExtension, 9, 9, 3)},
			wantWrites: []int{3},
			wantKnown:  true,
		},
		{
			name: "an element field store that names a register outside the general bank",
			body: []isa.Instruction{fieldStore, isa.NewInstruction(isa.OpExtension, 0, 0, generals)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			reads, writes, known := extensionGeneralOperands(tt.body, 0, tt.body[0], isa.ShapeForInstruction(tt.body[0]), generals)

			require.Equal(t, tt.wantKnown, known)
			require.Empty(t, reads)
			require.Equal(t, tt.wantWrites, writes)
		})
	}
}

func TestAnElementFieldStoreKeepsTheGrantsOfTheRegistersItDoesNotName(t *testing.T) {
	t.Parallel()

	const fresh, value, slice, scratch = 0, 1, 2, 3
	build := func(storeReceiver uint8) *program.CompiledFunction {
		function := &program.CompiledFunction{
			Name: "fill",
			Body: []isa.Instruction{
				isa.NewInstruction(isa.OpLoadGeneralConstant, fresh, 0, 0),
				isa.NewInstruction(isa.OpLoadGeneralConstant, scratch, 0, 0),
				isa.NewInstruction(isa.OpSetElementFieldInt64, 0, slice, 0),
				isa.NewInstruction(isa.OpExtension, 0, 0, scratch),
				isa.NewInstruction(isa.OpSetElement, storeReceiver, 0, value),
				isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
			},
			ArenaSafeAllocPCs:      map[int]bool{0: true, 1: true},
			FieldStoreArenaSafePCs: map[int]bool{4: true},
		}
		function.NumRegisters[isa.RegisterGeneral] = 4
		function.NumRegisters[isa.RegisterInt64] = 1
		return function
	}

	kept := ConvertForLicenceChecker(build(fresh))
	require.Equal(t, CheckerInstr{Kind: CheckerOp, Sources: []int{slice}, Destinations: []int{scratch}}, kept.Code[2],
		"the store reads the slice and may leave its element in the register the extension word names")
	require.True(t, checkerAccepts(t, kept), "a grant on a register the store does not name still holds after it")

	lost := ConvertForLicenceChecker(build(scratch))
	require.False(t, checkerAccepts(t, lost), "the register the store names no longer holds the allocation it was granted for")
}

func TestConvertOneReadsThroughACommaOkLookup(t *testing.T) {
	all := []int{0, 1, 2}
	lookups := []struct {
		op          isa.Opcode
		name        string
		sources     []int
		destination []int
	}{
		{name: "a boxed lookup", op: isa.OpGetMapOk, sources: []int{2}, destination: []int{0}},
		{name: "a string-keyed lookup of a boxed value", op: isa.OpGetMapStringGeneralOk, destination: []int{0}},
		{name: "an int-keyed lookup of a boxed value", op: isa.OpGetMapInt64GeneralOk, destination: []int{0}},
		{name: "a lookup of an int by a string", op: isa.OpGetMapStringInt64Ok},
		{name: "a lookup of a string by an int", op: isa.OpGetMapInt64StringOk},
		{name: "a fused lookup and branch of a boxed value", op: isa.OpGetMapStringGeneralOkThenJumpIfFalse, destination: []int{0}},
		{name: "a fused lookup and branch of an int", op: isa.OpGetMapInt64Int64OkThenJumpIfFalse},
	}
	for _, lookup := range lookups {
		t.Run(lookup.name, func(t *testing.T) {
			function := &program.CompiledFunction{Body: []isa.Instruction{
				isa.NewInstruction(lookup.op, 0, 1, 2),
				isa.NewInstruction(isa.OpExtension, 2, 0, 0),
			}}
			instruction := function.Body[0]
			want := CheckerInstr{Kind: CheckerRead, Receiver: 1, Sources: lookup.sources, Destinations: lookup.destination}
			require.Equal(t, want, convertOne(function, checkerGrants{}, 0, instruction, isa.ShapeForInstruction(instruction), all),
				"the ok flag is an int register, so the lookup is a read of the map and nothing more")
		})
	}
}
