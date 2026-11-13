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

//go:build integration

package bytecode_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/compile/inline"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestInlinerPreservesByValueParamSemantics_GenericToGeneric(t *testing.T) {
	t.Parallel()
	const source = `package main

type Number interface {
	~int | ~float64
}

func tripled[T Number](v T) T {
	return v + v + v
}

func sevenTimes[T Number](v T) T {
	return tripled(v) + tripled(v) + v
}

func entrypoint() int {
	return sevenTimes(9)
}
`
	service := app.NewService()
	cfs, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(t.Context(), cfs, "entrypoint")
	require.NoError(t, err)
	require.Equal(t, "63", fmt.Sprint(result),
		"sevenTimes(9) is tripled(9)+tripled(9)+9; 189 means the inliner clobbers the caller's parameter through the callee's return-prep MOVE")
}

func TestInlinerPreservesByValueParamSemantics_StructArgument(t *testing.T) {
	t.Parallel()
	const source = `package main

type Box struct {
	N int
}

func mutate(b Box) {
	b.N = 999
}

func entrypoint() int {
	b := Box{N: 7}
	mutate(b)
	return b.N
}
`
	service := app.NewService()
	cfs, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(t.Context(), cfs, "entrypoint")
	require.NoError(t, err)
	require.Equal(t, "7", fmt.Sprint(result),
		"a struct parameter is passed by value; 999 means the callee's SET_FIELD leaks into the caller's struct")
}

func TestInlinerPreservesByValueParamSemantics_ValueReceiver(t *testing.T) {
	t.Parallel()
	const source = `package main

type Box struct {
	N int
}

func (b Box) MutateLocal() int {
	b.N = 99
	return b.N
}

func entrypoint() int {
	p := &Box{N: 7}
	r := p.MutateLocal()
	return p.N*100 + r
}
`
	service := app.NewService()
	cfs, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(t.Context(), cfs, "entrypoint")
	require.NoError(t, err)
	require.Equal(t, "799", fmt.Sprint(result),
		"p.N stays 7 and r is 99; 9999 means the value receiver copy was elided across the inline boundary")
}

func TestInlinerHandlesRecursiveStructFieldAccess(t *testing.T) {
	t.Parallel()
	const source = `package main

type Node[T any] struct {
	Value T
	Next  *Node[T]
}

func listLen[T any](head *Node[T]) int {
	n := 0
	for cur := head; cur != nil; cur = cur.Next {
		n++
	}
	return n
}

func entrypoint() int {
	c := &Node[string]{Value: "c"}
	b := &Node[string]{Value: "b", Next: c}
	a := &Node[string]{Value: "a", Next: b}
	count := listLen(a)
	return count + 0 // suppress tail call so the inliner gets a shot
}
`
	service := app.NewService()
	cfs, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(t.Context(), cfs, "entrypoint")
	require.NoError(t, err)
	require.Equal(t, "3", fmt.Sprint(result),
		"listLen(a→b→c) is 3; 1 means GET_FIELD's destination is not remapped, so each cur.Next read overwrites the caller's a")
}

func TestInlinerReadOnlyParamElidesPreCopy(t *testing.T) {
	t.Parallel()
	callee := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpAddInt64, 2, 0, 1),
			isa.NewInstruction(isa.OpDrillTier1, byte(isa.SubOpMoveInt64), 0, 2),
			isa.NewInstruction(
				isa.OpDrillTier1,
				byte(isa.SubOpDrillTier2),
				byte(isa.SubOpTier2Return),
				1,
			),
		},
		ParameterKinds: []isa.RegisterKind{isa.RegisterInt64, isa.RegisterInt64},
		ResultKinds:    []isa.RegisterKind{isa.RegisterInt64},
		NumRegisters:   [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 3},
	}
	caller := &program.CompiledFunction{
		Body: []isa.Instruction{
			makeOpCallSlot(0),
			isa.NewInstruction(isa.OpNoOperation, 0, 0, 0),
		},
		CallSites: []program.CallSite{
			{
				CachedCallee: callee,
				Arguments: []program.VarLocation{
					{Register: 5, Kind: isa.RegisterInt64},
					{Register: 6, Kind: isa.RegisterInt64},
				},
				Returns: []program.VarLocation{{Register: 7, Kind: isa.RegisterInt64}},
			},
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt64: 8},
	}
	result := inline.TrySpliceCall(caller, 0)
	require.True(t, result.Spliced, "the splice succeeds: %v", result.Reason)

	require.Equal(t, uint32(10), caller.NumRegisters[isa.RegisterInt64])

	var addInt isa.Instruction
	for _, instr := range caller.Body {
		if instr.Op == isa.OpAddInt64 {
			addInt = instr
			break
		}
	}
	require.Equal(t, isa.OpAddInt64, addInt.Op, "the inlined body holds an ADD_INT64: %+v", caller.Body)
	require.Equal(t, uint8(9), addInt.A)
	require.Equal(t, uint8(7), addInt.B, "callee slot 0 is both the first parameter and the result, so its copy lives in the result's register")
	require.Equal(t, uint8(6), addInt.C, "ADD_INT64 reads argument register 6 directly for callee slot 1, with no pre-copy")

	preCopyCount := 0
	for _, instr := range caller.Body {
		if instr.Op == isa.OpDrillTier1 && isa.SubOpcode(instr.A) == isa.SubOpMoveInt64 &&
			instr.B == 7 && instr.C == 5 {
			preCopyCount++
		}
	}
	require.Equal(t, 1, preCopyCount, "one pre-copy MOVE_INT64 7 <- 5, and none for the read-only slot 1: %+v", caller.Body)

	for _, instr := range caller.Body {
		if instr.Op == isa.OpDrillTier1 && isa.SubOpcode(instr.A) == isa.SubOpMoveInt64 {
			require.NotEqual(t, uint8(6), instr.C, "the read-only parameter 1 needs no pre-copy from argument register 6")
		}
	}
}

func TestInlinerWritesThroughParamForceFreshSlot(t *testing.T) {
	t.Parallel()

	callee := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpSetField, 0, 0, 1),
			isa.NewInstruction(
				isa.OpDrillTier1,
				byte(isa.SubOpDrillTier2),
				byte(isa.SubOpTier2DrillTier3),
				byte(isa.SubOpTier3ReturnVoid),
			),
		},
		ParameterKinds: []isa.RegisterKind{isa.RegisterGeneral, isa.RegisterGeneral},
		NumRegisters: [isa.NumRegisterKinds]uint32{
			isa.RegisterGeneral: 2,
		},
	}
	caller := &program.CompiledFunction{
		Body: []isa.Instruction{
			makeOpCallSlot(0),
			isa.NewInstruction(isa.OpNoOperation, 0, 0, 0),
		},
		CallSites: []program.CallSite{
			{
				CachedCallee: callee,
				Arguments: []program.VarLocation{
					{Register: 3, Kind: isa.RegisterGeneral},
					{Register: 2, Kind: isa.RegisterGeneral},
				},
			},
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{
			isa.RegisterGeneral: 4,
		},
	}
	result := inline.TrySpliceCall(caller, 0)
	require.True(t, result.Spliced, "the splice succeeds: %v", result.Reason)

	require.Equal(t, uint32(5), caller.NumRegisters[isa.RegisterGeneral])

	foundPreCopy := false
	for _, instr := range caller.Body {
		if instr.Op == isa.OpMoveGeneral && instr.A == 4 && instr.B == 3 {
			foundPreCopy = true
			break
		}
	}
	require.True(t, foundPreCopy, "a struct parameter gets a pre-copy MOVE_GENERAL 4 <- 3: %+v", caller.Body)

	var setField isa.Instruction
	for _, instr := range caller.Body {
		if instr.Op == isa.OpSetField {
			setField = instr
			break
		}
	}
	require.Equal(t, isa.OpSetField, setField.Op, "the inlined body holds a SET_FIELD: %+v", caller.Body)
	require.Equal(t, uint8(4), setField.A, "the inlined SET_FIELD writes the fresh slot 4; 3 would mutate the caller's struct")

	require.Equal(t, uint8(2), setField.C, "the inlined SET_FIELD reads the caller's argument 2 directly, with no pre-copy")
}

func TestInlinerRemapsADynamicBankOperand(t *testing.T) {
	t.Parallel()
	const source = `package main

type Counter int

func (c *Counter) Set(v int) { *c = Counter(v) }

type Label string

func (l *Label) Set(v string) { *l = Label(v) }

func entrypoint() int {
	c := Counter(1000)
	c.Set(7)
	l := Label("before")
	l.Set("after")
	return int(c)*1000 + len(string(l))
}
`
	service := app.NewService()
	cfs, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(t.Context(), cfs, "entrypoint")
	require.NoError(t, err)
	require.Equal(t, "7005", fmt.Sprint(result),
		"the inlined Set boxes its argument with BOX_AS_TYPE, whose source bank is chosen by a kind marker; "+
			"anything else means the source register was left as the callee's slot instead of the caller's argument")
}
