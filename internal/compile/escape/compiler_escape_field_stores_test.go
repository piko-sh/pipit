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

package escape

import (
	"context"
	"reflect"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func fieldStoreFunction(between ...isa.Instruction) (*program.CompiledFunction, int) {
	body := make([]isa.Instruction, 0, 5+len(between))
	body = append(body,
		isa.NewInstruction(isa.OpAllocIndirect, 0, 0, uint8(isa.RegisterInt)),
		isa.NewInstruction(isa.OpExt, 0, 0, 0),
		isa.NewInstruction(isa.OpLoadGeneralConst, 1, 0, 0),
	)
	body = append(body, between...)
	storePC := len(body)
	body = append(body,
		isa.NewInstruction(isa.OpSetStructFieldGeneral, 0, 1, 0),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	)
	return &program.CompiledFunction{Name: "store", Body: body, GeneralConstants: make([]reflect.Value, 1)}, storePC
}

func TestAnnotateArenaSafeFieldStores(t *testing.T) {
	t.Parallel()
	jumpLo, jumpHi := isa.SplitOffset(0)
	receiverArgument := program.CallSite{Arguments: []program.VarLocation{{Kind: isa.RegisterGeneral, Register: 0}}}
	receiverResult := program.CallSite{Returns: []program.VarLocation{{Kind: isa.RegisterGeneral, Register: 0}}}
	otherResult := program.CallSite{Returns: []program.VarLocation{{Kind: isa.RegisterGeneral, Register: 1}}}
	cases := []struct {
		name      string
		between   []isa.Instruction
		site      program.CallSite
		escapeAll bool
		annotated bool
	}{
		{name: "receiver straight from the allocation", annotated: true},
		{name: "unrelated work in between", between: []isa.Instruction{isa.NewInstruction(isa.OpLoadIntConst, 0, 0, 0)}, annotated: true},
		{name: "receiver copied to another register", between: []isa.Instruction{isa.NewInstruction(isa.OpMoveGeneral, 2, 0, 1)}, annotated: false},
		{name: "a call that touches neither the receiver nor its register", between: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpCall, 0, 0)}, site: otherResult, annotated: true},
		{name: "a call that returns into the receiver register", between: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpCall, 0, 0)}, site: receiverResult, annotated: false},
		{name: "a call that receives the receiver", between: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpCall, 0, 0)}, site: receiverArgument, annotated: false},
		{name: "the store is a jump target", between: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpJump, jumpLo, jumpHi)}, annotated: false},
		{name: "receiver reassigned by another arena-safe allocation", between: []isa.Instruction{isa.NewInstruction(isa.OpLoadGeneralConst, 0, 0, 0)}, annotated: true},
		{name: "receiver reassigned from another register", between: []isa.Instruction{isa.NewInstruction(isa.OpMoveGeneral, 0, 2, 1)}, annotated: false},
		{name: "allocation escapes", escapeAll: true, annotated: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction, storePC := fieldStoreFunction(testCase.between...)
			if testCase.escapeAll {

				compiledFunction.Body = append(compiledFunction.Body[:storePC+1],
					isa.NewInstruction(isa.OpSetGlobal, 0, 0, uint8(isa.RegisterGeneral)),
					isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid))
			}
			compiledFunction.CallSites = []program.CallSite{testCase.site}

			annotateArenaSafeAllocs(compiledFunction)
			annotateArenaSafeFieldStores(compiledFunction)

			require.Equal(t, testCase.annotated, compiledFunction.FieldStoreArenaSafePCs[storePC],
				"arena-safe allocations %v, annotated stores %v", compiledFunction.ArenaSafeAllocPCs, compiledFunction.FieldStoreArenaSafePCs)
		})
	}
}

func structLiteralStoreFunction(callee *program.CompiledFunction, literalTypeIndex uint8, tail ...isa.Instruction) (*program.CompiledFunction, int) {
	body := slices.Concat([]isa.Instruction{
		isa.NewTier2Instruction(isa.SubOpTier2MakeMap, 0),
		isa.NewInstruction(isa.OpExt, literalTypeIndex, 0, 0),
		isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
		isa.NewInstruction(isa.OpLoadIntConst, 3, 0, 0),
		isa.NewInstruction(isa.OpSetStructFieldIntT0, 0, 3, 1),
		isa.NewInstruction(isa.OpSetStructFieldGeneral, 0, 1, 0),
		isa.NewInstruction(isa.OpAllocIndirect, 1, 0, uint8(isa.RegisterGeneral)),
		isa.NewInstruction(isa.OpExt, 0, 0, 0),
		isa.NewTier1Instruction(isa.SubOpCall, 1, 0),
	}, tail, []isa.Instruction{isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid)})
	compiledFunction := &program.CompiledFunction{
		Name:      "evaluate",
		Body:      body,
		TypeTable: []reflect.Type{reflect.TypeFor[struct{ Tokens []int }](), reflect.TypeFor[map[string]int]()},
		CallSites: []program.CallSite{
			{Returns: []program.VarLocation{{Kind: isa.RegisterGeneral, Register: 1}}},
			{Arguments: []program.VarLocation{{Kind: isa.RegisterGeneral, Register: 1}}, CachedCallee: callee},
		},
	}
	return compiledFunction, 5
}

func TestAnnotateArenaSafeFieldStoresIntoStructLiterals(t *testing.T) {
	t.Parallel()
	confined := &program.CompiledFunction{ParameterKinds: []isa.RegisterKind{isa.RegisterGeneral}, ParameterEscapes: []bool{false}}
	escaping := &program.CompiledFunction{ParameterKinds: []isa.RegisterKind{isa.RegisterGeneral}, ParameterEscapes: []bool{true}}
	backEdgeLo, backEdgeHi := isa.SplitOffset(-8)
	cases := []struct {
		name        string
		callee      *program.CompiledFunction
		literalType uint8
		tail        []isa.Instruction
		resultKinds []isa.RegisterKind
		annotated   bool
		structSite  bool
		addressSite bool
	}{
		{name: "receiver confined in the callee", callee: confined, annotated: true, structSite: true, addressSite: true},
		{name: "callee lets the receiver escape", callee: escaping, annotated: false, structSite: false, addressSite: false},
		{name: "unresolved callee", callee: nil, annotated: false, structSite: false, addressSite: false},
		{
			name: "pointer stored to a global after the call", callee: confined,
			tail:      []isa.Instruction{isa.NewInstruction(isa.OpSetGlobal, 1, 0, uint8(isa.RegisterGeneral))},
			annotated: false, structSite: false, addressSite: false,
		},
		{
			name: "pointer returned", callee: confined,
			tail:        []isa.Instruction{isa.NewInstruction(isa.OpMoveGeneral, 0, 1, 1), isa.NewTier2Instruction(isa.SubOpTier2Return, 1)},
			resultKinds: []isa.RegisterKind{isa.RegisterGeneral},
			annotated:   false, structSite: false, addressSite: false,
		},
		{

			name: "struct register copied after the call", callee: confined,
			tail:      []isa.Instruction{isa.NewInstruction(isa.OpMoveGeneral, 2, 0, 1)},
			annotated: false, structSite: true, addressSite: true,
		},
		{

			name: "a real map is not a struct-literal site", callee: confined, literalType: 1,
			annotated: false, structSite: false, addressSite: true,
		},
		{

			name: "loop back edge between literal and store", callee: confined,
			tail:      []isa.Instruction{isa.NewTier1Instruction(isa.SubOpJump, backEdgeLo, backEdgeHi)},
			annotated: false, structSite: true, addressSite: true,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction, storePC := structLiteralStoreFunction(testCase.callee, testCase.literalType, testCase.tail...)
			compiledFunction.ResultKinds = testCase.resultKinds

			annotateArenaSafeAllocs(compiledFunction)
			annotateArenaSafeFieldStores(compiledFunction)

			require.Equal(t, testCase.structSite, compiledFunction.ArenaSafeAllocPCs[0], "struct literal site: %v", compiledFunction.ArenaSafeAllocPCs)
			require.Equal(t, testCase.addressSite, compiledFunction.ArenaSafeAllocPCs[6], "address-of site: %v", compiledFunction.ArenaSafeAllocPCs)
			require.Equal(t, testCase.annotated, compiledFunction.FieldStoreArenaSafePCs[storePC],
				"arena-safe allocations %v, annotated stores %v", compiledFunction.ArenaSafeAllocPCs, compiledFunction.FieldStoreArenaSafePCs)
		})
	}
}

func TestArenaSafeAddressAliasesInsteadOfEscaping(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		isa.NewInstruction(isa.OpAllocIndirect, 1, 0, uint8(isa.RegisterGeneral)),
		isa.NewInstruction(isa.OpExt, 0, 0, 0),
		isa.NewInstruction(isa.OpSetGlobal, 0, 1, uint8(isa.RegisterGeneral)),
		isa.NewTier3Instruction(isa.SubOpTier3ReturnVoid),
	}
	tests := []struct {
		name         string
		arenaSafe    map[int]bool
		endPC        int
		wantEscapes  bool
		wantAliasing bool
	}{
		{name: "address at an arena-safe site aliases", arenaSafe: map[int]bool{0: true}, endPC: 2, wantEscapes: false, wantAliasing: true},
		{name: "address at an unsafe site escapes", arenaSafe: nil, endPC: 2, wantEscapes: true},
		{name: "the alias still escapes through its own uses", arenaSafe: map[int]bool{0: true}, endPC: 4, wantEscapes: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: body, ArenaSafeAllocPCs: tt.arenaSafe}
			require.Equal(t, tt.wantEscapes, analyseGeneralRegisterEscapeRange(compiledFunction, 0, 0, tt.endPC))
			if tt.wantAliasing {
				tainted := [isa.GeneralRegisterBankSize]bool{0: true}
				require.False(t, escapesAtInstruction(compiledFunction, 0, &tainted))
				require.True(t, tainted[1], "the pointer register is tainted as an alias")
			}
		})
	}
}

func TestRunEscapeAnalysisPassWithOptionsArenaPromotion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		promotion bool
	}{
		{name: "promotion on annotates", promotion: true},
		{name: "promotion off annotates nothing", promotion: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction, storePC := fieldStoreFunction()
			compiledFunction.CallSites = []program.CallSite{{}}

			err := RunEscapeAnalysisPassWithOptions(context.Background(), compiledFunction, PassOptions{ArenaPromotion: testCase.promotion})
			require.NoError(t, err)

			require.Equal(t, testCase.promotion, len(compiledFunction.ArenaSafeAllocPCs) > 0, "ArenaSafeAllocPCs %v", compiledFunction.ArenaSafeAllocPCs)
			require.Equal(t, testCase.promotion, compiledFunction.FieldStoreArenaSafePCs[storePC], "FieldStoreArenaSafePCs %v", compiledFunction.FieldStoreArenaSafePCs)
		})
	}
}

func TestEscapeWalkKillsOnDefiniteWriteAndTypedOperands(t *testing.T) {
	t.Parallel()
	site := program.CallSite{Returns: []program.VarLocation{{Kind: isa.RegisterGeneral, Register: 0}}}
	tests := []struct {
		name string
		body []isa.Instruction
		want bool
	}{
		{
			name: "call result overwrites the tainted register before a global store",
			body: []isa.Instruction{
				isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
				isa.NewInstruction(isa.OpSetGlobal, 0, 0, uint8(isa.RegisterGeneral)),
			},
			want: false,
		},
		{
			name: "a jump target between the write and the store restores the taint",
			body: []isa.Instruction{
				isa.NewTier1Instruction(isa.SubOpCall, 0, 0),
				isa.NewInstruction(isa.OpLoadIntConst, 0, 0, 0),
				isa.NewInstruction(isa.OpSetGlobal, 0, 0, uint8(isa.RegisterGeneral)),
				isa.NewInstruction(isa.OpJumpIfFalse, 0, 0xFE, 0xFF),
			},
			want: true,
		},
		{
			name: "a global store of the tainted register escapes",
			body: []isa.Instruction{isa.NewInstruction(isa.OpSetGlobal, 0, 0, uint8(isa.RegisterGeneral))},
			want: true,
		},
		{
			name: "tier-2 int increment on register 0 is not a general read",
			body: []isa.Instruction{isa.NewTier2Instruction(isa.SubOpTier2IncInt, 0)},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: tt.body, CallSites: []program.CallSite{site}}
			require.Equal(t, tt.want, analyseGeneralRegisterEscapeRange(compiledFunction, 0, 0, len(tt.body)))
		})
	}
}
