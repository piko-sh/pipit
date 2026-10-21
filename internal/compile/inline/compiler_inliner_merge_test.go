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

package inline

import (
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/compile/passes"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestMergeTypeTableIndexKeepsInterfaceMethods(t *testing.T) {
	t.Parallel()

	anyType := reflect.TypeFor[any]()
	tests := []struct {
		name          string
		callerMethods [][]string
		calleeMethods [][]string
		wantMethods   []string
	}{
		{
			name:          "an interface the program declares keeps its methods",
			callerMethods: nil,
			calleeMethods: [][]string{{"area"}},
			wantMethods:   []string{"area"},
		},
		{
			name:          "a caller entry for any without methods is not reused",
			callerMethods: [][]string{nil},
			calleeMethods: [][]string{{"area"}},
			wantMethods:   []string{"area"},
		},
		{
			name:          "a type without methods stays without",
			callerMethods: nil,
			calleeMethods: [][]string{nil},
			wantMethods:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			caller := &program.CompiledFunction{}
			for _, methods := range tt.callerMethods {
				_, err := program.AddTypeRefWithMethods(caller, anyType, methods)
				require.NoError(t, err)
			}
			callee := &program.CompiledFunction{}
			for _, methods := range tt.calleeMethods {
				_, err := program.AddTypeRefWithMethods(callee, anyType, methods)
				require.NoError(t, err)
			}

			newIndex, fits := mergeTypeTableIndex(caller, callee, 0, true)

			require.True(t, fits)
			require.Equal(t, anyType, caller.TypeTable[newIndex])
			require.Equal(t, tt.wantMethods, caller.TypeTableInterfaceMethods[newIndex],
				"an inlined assertion or type switch checks the methods recorded at its index")
		})
	}
}

func TestASplicedCallBackIntoTheCallerIsNotUnrolledAgain(t *testing.T) {
	t.Parallel()

	caller := callerCalling(nil, program.CallSite{
		Arguments: []program.VarLocation{intSlot(2)},
		Returns:   []program.VarLocation{intSlot(0)},
	})
	middle := &program.CompiledFunction{
		Name: "middle",
		Body: []isa.Instruction{
			isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
			isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
		},
		NumRegisters:       [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 2},
		ParameterKinds:     []isa.RegisterKind{isa.RegisterInt64},
		ParameterRegisters: []uint8{1},
		ResultKinds:        []isa.RegisterKind{isa.RegisterInt64},
		CallSites: []program.CallSite{{
			CachedCallee: caller,
			Arguments:    []program.VarLocation{intSlot(1)},
			Returns:      []program.VarLocation{intSlot(0)},
		}},
	}
	caller.CallSites[0].CachedCallee = middle
	splice := inlineContext{caller: caller, callee: middle, site: &caller.CallSites[0]}
	splice.resetRegisterRemap()
	splice.setRegister(isa.RegisterInt64, 0, 0)
	splice.setRegister(isa.RegisterInt64, 1, 2)

	merged, ok := mergeCallSiteForCtx(&splice, 0, false)

	require.True(t, ok)
	require.True(t, caller.CallSites[merged].RecursionUnrolled,
		"a site that calls the function it now sits in is marked as unrolled, so revisiting it cannot expand without end")
}

func TestGlobalsAreSplicedOnlyBetweenFunctionsWithTheSameBases(t *testing.T) {
	t.Parallel()

	globalReader := func() *program.CompiledFunction {
		return &program.CompiledFunction{
			Name: "read",
			Body: []isa.Instruction{
				isa.NewInstruction(isa.OpGetGlobal, 0, 5, uint8(isa.RegisterInt64)),
				isa.NewTier2Instruction(isa.SubOpTier2Return, 1),
			},
			NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 1},
			ResultKinds:  []isa.RegisterKind{isa.RegisterInt64},
		}
	}

	shared := callerCalling(globalReader(), program.CallSite{Returns: []program.VarLocation{intSlot(0)}})
	require.NoError(t, RunBytecodeInliner(t.Context(), shared, passes.DefaultOptions()))
	require.Zero(t, callsRemaining(shared), "two functions of one program read the same slot for the same index")
	read := slices.IndexFunc(shared.Body, func(instruction isa.Instruction) bool { return instruction.Op == isa.OpGetGlobal })
	require.GreaterOrEqual(t, read, 0)
	require.Equal(t, uint8(5), shared.Body[read].B, "the global's index is kept")

	elsewhere := globalReader()
	elsewhere.GlobalBases = &program.SlotAllocation{isa.RegisterInt64: 7}
	apart := callerCalling(elsewhere, program.CallSite{Returns: []program.VarLocation{intSlot(0)}})
	require.NoError(t, RunBytecodeInliner(t.Context(), apart, passes.DefaultOptions()))
	require.Equal(t, 1, callsRemaining(apart), "an index counted from another base would name a different global")
}
