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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestLicenceCheckerGrantsStoresIntoAMapTheFunctionHoldsAlone(t *testing.T) {
	programs := parseCheckerPrograms(t, `func filled_in_a_loop 2 5
calloc 0 ; 1
read 0 / 1 ; 2
cstore 0 1 ; 3
nop ; 1 4
nop ;
end
func stored_through_a_copy 3 4
calloc 0 ; 1
move 2 0 ; 2
cstore 2 1 ; 3
nop ;
end
func handed_off_first 2 4
calloc 0 ; 1
op 0 / ; 2
cstore 0 1 ; 3
nop ;
end
func handed_off_later_in_the_loop 2 5
calloc 0 ; 1
cstore 0 1 ; 2
op 0 / ; 3
nop ; 1 4
nop ;
end
func a_map_from_anywhere 2 2
cstore 0 1 ; 1
nop ;
end
func one_of_two_maps 2 6
nop ; 1 3
calloc 0 ; 2
nop ; 4
calloc 0 ; 4
cstore 0 1 ; 5
nop ;
end
`)
	require.True(t, checkerAccepts(t, programs[0]), "a lookup does not hand the map off, so the store stays granted around the loop")
	require.True(t, checkerAccepts(t, programs[1]), "a copy of the register names the same map")
	require.False(t, checkerAccepts(t, programs[2]), "a map handed off before the store is not held alone")
	require.False(t, checkerAccepts(t, programs[3]), "a hand-off later in the loop reaches the store on the next iteration")
	require.False(t, checkerAccepts(t, programs[4]), "a map the function did not make is not held alone")
	require.False(t, checkerAccepts(t, programs[5]), "a register that holds one of two maps is not a single allocation")
}

func TestLicenceCheckerKeepsContainerAndArenaGrantsApart(t *testing.T) {
	programs := parseCheckerPrograms(t, `func container_store_into_an_arena_object 2 3
alloc 0 ; 1
cstore 0 1 ; 2
nop ;
end
func field_store_into_a_map 2 3
calloc 0 ; 1
store 0 1 1 ; 2
nop ;
end
`)
	require.False(t, checkerAccepts(t, programs[0]), "a container store needs a map in its receiver")
	require.False(t, checkerAccepts(t, programs[1]), "a field store needs an arena allocation in its receiver")
}

func TestLicenceCheckerReadHandsOffItsOtherSources(t *testing.T) {
	programs := parseCheckerPrograms(t, `func key_is_a_fresh_object 3 4
alloc 1 ; 1
read 0 1 / 2 ; 2
store 1 2 1 ; 3
nop ;
end
func lookup_result_is_unknown 3 4
calloc 0 ; 1
read 0 / 2 ; 2
cstore 2 1 ; 3
nop ;
end
`)
	require.False(t, checkerAccepts(t, programs[0]), "a lookup hands off the key it reads")
	require.False(t, checkerAccepts(t, programs[1]), "what a lookup returns may be anything")
}

func mapFillBody(t *testing.T) []isa.Instruction {
	t.Helper()
	body := []isa.Instruction{
		isa.NewTier2Instruction(isa.SubOpTier2MakeMap, 0),
		isa.NewInstruction(isa.OpExtension, 0, 0, 0),
		isa.NewInstruction(isa.OpGetMapStringGeneral, 1, 0, 0),
		isa.NewInstruction(isa.OpSetMapStringGeneral, 0, 0, 1),
		isa.NewInstruction(isa.OpJumpIfFalse, 0, 0, 0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, program.SetJumpTarget(body, 4, 2))
	return body
}

func mapFillFunction(body []isa.Instruction) *program.CompiledFunction {
	function := &program.CompiledFunction{Name: "fill", Body: body, TypeTable: []reflect.Type{reflect.TypeFor[map[string][]string]()}}
	function.NumRegisters[isa.RegisterGeneral] = 3
	function.NumRegisters[isa.RegisterString] = 1
	function.NumRegisters[isa.RegisterInt64] = 1
	return function
}

func TestInferLocalContainerGrantsFindsAMapFilledInALoop(t *testing.T) {
	function := mapFillFunction(mapFillBody(t))

	inferGrants(t, function)

	require.Equal(t, program.PCSetOf(0), function.LocalContainerAllocPCs)
	require.Equal(t, program.PCSetOf(3), function.LocalContainerStorePCs)
	converted := ConvertForLicenceChecker(function)
	require.Equal(t, CheckerInstr{Kind: CheckerAlloc, Destination: 0, Container: true}, converted.Code[0])
	require.Equal(t, CheckerInstr{Kind: CheckerRead, Receiver: 0, Destinations: []int{1}}, converted.Code[2])
	require.Equal(t, CheckerInstr{Kind: CheckerStore, Receiver: 0, Source: 1, Granted: true, Container: true}, converted.Code[3])
	require.True(t, checkerAccepts(t, converted), "the checker accepts the grants it inferred")
}

func TestInferLocalContainerGrantsNeedsTheAllocationToBeAMap(t *testing.T) {
	for name, typeTable := range map[string][]reflect.Type{
		"a struct literal built by the same operation": {reflect.TypeFor[struct{ Names []string }]()},
		"a type index outside the table":               nil,
		"a missing type":                               {nil},
	} {
		t.Run(name, func(t *testing.T) {
			function := mapFillFunction(mapFillBody(t))
			function.TypeTable = typeTable

			inferGrants(t, function)
			require.Nil(t, function.LocalContainerStorePCs)

			function.LocalContainerAllocPCs = program.PCSetOf(0)
			function.LocalContainerStorePCs = program.PCSetOf(3)
			require.False(t, checkerAccepts(t, ConvertForLicenceChecker(function)), "a grant naming it is rejected")
		})
	}
}

func TestInferLocalContainerGrantsStopsAtAHandOff(t *testing.T) {
	body := []isa.Instruction{
		isa.NewTier2Instruction(isa.SubOpTier2MakeMap, 0),
		isa.NewInstruction(isa.OpExtension, 0, 0, 0),
		isa.NewTier2Instruction(isa.SubOpTier2MakeMap, 2),
		isa.NewInstruction(isa.OpExtension, 0, 0, 0),
		isa.NewInstruction(isa.OpSetMapStringGeneral, 0, 0, 1),
		isa.NewInstruction(isa.OpSetMapStringGeneral, 2, 0, 0),
		isa.NewInstruction(isa.OpSetMapStringGeneral, 0, 0, 1),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	function := mapFillFunction(body)

	inferGrants(t, function)

	require.Equal(t, program.PCSetOf(4, 5), function.LocalContainerStorePCs,
		"the store after the map was itself stored into another map is not granted")
	require.Equal(t, program.PCSetOf(0, 2), function.LocalContainerAllocPCs)
	require.True(t, checkerAccepts(t, ConvertForLicenceChecker(function)))
}

func TestInferLocalContainerGrantsReplacesStaleTables(t *testing.T) {
	function := mapFillFunction([]isa.Instruction{
		isa.NewInstruction(isa.OpSetMapStringGeneral, 0, 0, 1),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	})
	function.LocalContainerAllocPCs = program.PCSetOf(7)
	function.LocalContainerStorePCs = program.PCSetOf(0)

	inferGrants(t, function)

	require.Nil(t, function.LocalContainerAllocPCs)
	require.Nil(t, function.LocalContainerStorePCs, "a store into a map from a parameter is not granted")
}

func TestLicenceCheckerRejectsALocalContainerGrantOnTheWrongInstruction(t *testing.T) {
	for name, tables := range map[string]struct {
		allocs program.PCSet
		stores program.PCSet
	}{
		"an allocation grant on a lookup": {allocs: program.PCSetOf(2)},
		"a store grant on a lookup":       {stores: program.PCSetOf(2)},
		"a store grant with no site":      {stores: program.PCSetOf(3)},
	} {
		t.Run(name, func(t *testing.T) {
			function := mapFillFunction(mapFillBody(t))
			function.LocalContainerAllocPCs, function.LocalContainerStorePCs = tables.allocs, tables.stores

			require.False(t, checkerAccepts(t, ConvertForLicenceChecker(function)))
		})
	}
}

func TestLicenceCheckerForgetsARegisterNothingReadsAgain(t *testing.T) {
	programs := parseCheckerPrograms(t, `func a_temporary_rewritten_each_iteration 3 6
calloc 0 ; 1
nop ; 2
move 2 0 ; 3
cstore 2 1 ; 4
nop ; 1 5
nop ;
end
func a_copy_still_read_after_the_join 3 7
calloc 0 ; 1
nop ; 2 3
move 2 0 ; 4
op / 2 ; 4
op 2 / ; 5
cstore 0 1 ; 6
nop ;
end
`)
	require.True(t, checkerAccepts(t, programs[0]),
		"the temporary holds the map on the back edge and anything on entry, but is overwritten before it is read")
	require.False(t, checkerAccepts(t, programs[1]),
		"a register read after the join may be the map or anything else, so the map is no longer held alone")
}

func inferGrants(t *testing.T, function *program.CompiledFunction) {
	t.Helper()
	_, err := inferLocalContainerGrants(t.Context(), function)
	require.NoError(t, err)
}

func TestInferLocalContainerGrantsSurvivesACommaOkLookup(t *testing.T) {
	body := []isa.Instruction{
		isa.NewTier2Instruction(isa.SubOpTier2MakeMap, 0),
		isa.NewInstruction(isa.OpExtension, 0, 0, 0),
		isa.NewInstruction(isa.OpGetMapStringGeneralOk, 1, 0, 0),
		isa.NewInstruction(isa.OpExtension, 0, 0, 0),
		isa.NewInstruction(isa.OpSetMapStringGeneral, 0, 0, 1),
		isa.NewInstruction(isa.OpJumpIfFalse, 0, 0, 0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, program.SetJumpTarget(body, 5, 2))
	function := mapFillFunction(body)

	inferGrants(t, function)

	require.Equal(t, program.PCSetOf(0), function.LocalContainerAllocPCs)
	require.Equal(t, program.PCSetOf(4), function.LocalContainerStorePCs, "`v, ok := m[k]` reads the map and leaves its grant standing")
	converted := ConvertForLicenceChecker(function)
	require.Equal(t, CheckerInstr{Kind: CheckerRead, Receiver: 0, Destinations: []int{1}}, converted.Code[2])
	require.True(t, checkerAccepts(t, converted))
}

func mapEntryAppendBody(t *testing.T) []isa.Instruction {
	t.Helper()
	body := []isa.Instruction{
		isa.NewTier2Instruction(isa.SubOpTier2MakeMap, 0),
		isa.NewInstruction(isa.OpExtension, 0, 0, 0),
		isa.NewInstruction(isa.OpAppendStringToMapString, 0, 0, 1),
		isa.NewInstruction(isa.OpJumpIfFalse, 0, 0, 0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	require.True(t, program.SetJumpTarget(body, 3, 2))
	return body
}

func TestInferLocalContainerGrantsCoversAMapEntryGrownInPlace(t *testing.T) {
	function := mapFillFunction(mapEntryAppendBody(t))
	function.NumRegisters[isa.RegisterString] = 2

	inferGrants(t, function)

	require.Equal(t, program.PCSetOf(0), function.LocalContainerAllocPCs)
	require.Equal(t, program.PCSetOf(2), function.LocalContainerStorePCs)
	converted := ConvertForLicenceChecker(function)
	require.Equal(t, 3, converted.Registers, "the conversion has the function's own registers")
	require.Equal(t, CheckerInstr{Kind: checkerStoreValue, Receiver: 0, Granted: true, Container: true}, converted.Code[2],
		"the slice it stores is in no general register, so the store reads only the map")
	require.True(t, checkerAccepts(t, converted), "the checker accepts the grant it inferred")
}

func TestInferLocalContainerGrantsRefusesAMapEntryGrownInPlaceAfterAHandOff(t *testing.T) {
	body := []isa.Instruction{
		isa.NewTier2Instruction(isa.SubOpTier2MakeMap, 0),
		isa.NewInstruction(isa.OpExtension, 0, 0, 0),
		isa.NewTier2Instruction(isa.SubOpTier2MakeMap, 2),
		isa.NewInstruction(isa.OpExtension, 0, 0, 0),
		isa.NewInstruction(isa.OpAppendStringToMapString, 0, 0, 1),
		isa.NewInstruction(isa.OpSetMapStringGeneral, 2, 0, 0),
		isa.NewInstruction(isa.OpAppendStringToMapString, 0, 0, 1),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	function := mapFillFunction(body)
	function.NumRegisters[isa.RegisterString] = 2

	inferGrants(t, function)

	require.Equal(t, program.PCSetOf(4, 5), function.LocalContainerStorePCs,
		"the entry grown after the map was itself stored into another map is not granted")
	converted := ConvertForLicenceChecker(function)
	require.Equal(t, CheckerInstr{Kind: checkerStoreValue, Receiver: 0}, converted.Code[6])
	require.True(t, checkerAccepts(t, converted))

	function.LocalContainerStorePCs = program.PCSetOf(4, 5, 6)
	require.False(t, checkerAccepts(t, ConvertForLicenceChecker(function)), "a grant naming the later store is rejected")
}

func TestAMapEntryGrownInPlaceInAMapTheFunctionWasGivenHasNoGrant(t *testing.T) {
	function := mapFillFunction([]isa.Instruction{
		isa.NewInstruction(isa.OpAppendStringToMapString, 0, 0, 1),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	})
	function.NumRegisters[isa.RegisterString] = 2

	inferGrants(t, function)
	require.Nil(t, function.LocalContainerStorePCs)
	require.True(t, checkerAccepts(t, ConvertForLicenceChecker(function)), "the store runs with its hand-off and needs no grant")

	function.LocalContainerStorePCs = program.PCSetOf(0)
	require.False(t, checkerAccepts(t, ConvertForLicenceChecker(function)), "a grant for a map the function did not make is rejected")
}
