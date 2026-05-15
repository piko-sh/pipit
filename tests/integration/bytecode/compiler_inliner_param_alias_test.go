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
	"context"
	"fmt"
	"testing"

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
	cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ExecuteEntrypoint(context.Background(), cfs, "entrypoint")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(result) != "63" {
		t.Fatalf("sevenTimes(9) = %v, want 63 (tripled(9)+tripled(9)+9 = 27+27+9); "+
			"189 indicates the inliner is clobbering the caller's parameter via "+
			"the callee's return-prep MOVE", result)
	}
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
	cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ExecuteEntrypoint(context.Background(), cfs, "entrypoint")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(result) != "7" {
		t.Fatalf("b.N after mutate(b) = %v, want 7 (Go's by-value parameter "+
			"semantics); 999 indicates the inliner is letting the callee's "+
			"SET_FIELD on its struct param slot leak into the caller's struct", result)
	}
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
	cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ExecuteEntrypoint(context.Background(), cfs, "entrypoint")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(result) != "799" {
		t.Fatalf("p.N*100+r = %v, want 799 (p.N stays 7, r=99); 9999 indicates "+
			"the value receiver copy was elided across the inline boundary",
			result)
	}
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
	cfs, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.ExecuteEntrypoint(context.Background(), cfs, "entrypoint")
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(result) != "3" {
		t.Fatalf("listLen(a→b→c) = %v, want 3; 1 indicates GET_FIELD's "+
			"destination operand isn't being remapped through the inliner "+
			"and so each `cur.Next` read overwrites the caller's `a` and "+
			"reads the never-written remapped slot for cur", result)
	}
}

func TestInlinerReadOnlyParamElidesPreCopy(t *testing.T) {
	t.Parallel()
	callee := &program.CompiledFunction{
		Body: []isa.Instruction{
			isa.NewInstruction(isa.OpAddInt, 2, 0, 1),
			isa.NewInstruction(isa.OpDrillTier1, byte(isa.SubOpMoveInt), 0, 2),
			isa.NewInstruction(
				isa.OpDrillTier1,
				byte(isa.SubOpDrillTier2),
				byte(isa.SubOpTier2Return),
				1,
			),
		},
		ParameterKinds: []isa.RegisterKind{isa.RegisterInt, isa.RegisterInt},
		ResultKinds:    []isa.RegisterKind{isa.RegisterInt},
		NumRegisters:   [isa.NumRegisterKinds]uint32{isa.RegisterInt: 3},
	}
	caller := &program.CompiledFunction{
		Body: []isa.Instruction{
			makeOpCallSlot(0),
			isa.NewInstruction(isa.OpNop, 0, 0, 0),
		},
		CallSites: []program.CallSite{
			{
				CachedCallee: callee,
				Arguments: []program.VarLocation{
					{Register: 5, Kind: isa.RegisterInt},
					{Register: 6, Kind: isa.RegisterInt},
				},
				Returns: []program.VarLocation{{Register: 7, Kind: isa.RegisterInt}},
			},
		},
		NumRegisters: [isa.NumRegisterKinds]uint32{isa.RegisterInt: 8},
	}
	result := inline.TrySpliceCall(caller, 0)
	if !result.Spliced {
		t.Fatalf("splice failed: %v", result.Reason)
	}

	if caller.NumRegisters[isa.RegisterInt] != 10 {
		t.Fatalf("numRegisters[int] %d want 10 (8 caller + 1 fresh param + 1 local)",
			caller.NumRegisters[isa.RegisterInt])
	}

	var addInt isa.Instruction
	for _, instr := range caller.Body {
		if instr.Op == isa.OpAddInt {
			addInt = instr
			break
		}
	}
	if addInt.Op != isa.OpAddInt {
		t.Fatalf("expected opAddInt in inlined body, body=%+v", caller.Body)
	}
	if addInt.A != 9 {
		t.Fatalf("opAddInt destination %d want 9 (fresh local for callee slot 2)", addInt.A)
	}
	if addInt.B != 8 {
		t.Fatalf("opAddInt srcA %d want 8 (fresh param slot for callee slot 0)", addInt.B)
	}
	if addInt.C != 6 {
		t.Fatalf("opAddInt srcB %d want 6 (alias to caller argument.register for callee slot 1, no pre-copy)", addInt.C)
	}

	preCopyCount := 0
	for _, instr := range caller.Body {
		if instr.Op == isa.OpDrillTier1 && isa.SubOpcode(instr.A) == isa.SubOpMoveInt &&
			instr.B == 8 && instr.C == 5 {
			preCopyCount++
		}
	}
	if preCopyCount != 1 {
		t.Fatalf("pre-copy MOVE_INT destination=8 source=5 count %d want 1 (read-only param "+
			"must NOT emit a pre-copy for slot 1); body=%+v", preCopyCount, caller.Body)
	}

	for _, instr := range caller.Body {
		if instr.Op == isa.OpDrillTier1 && isa.SubOpcode(instr.A) == isa.SubOpMoveInt &&
			instr.C == 6 {
			t.Fatalf("found pre-copy from caller argument.register 6 - read-only " +
				"param 1 should have skipped pre-copy")
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
			isa.NewInstruction(isa.OpNop, 0, 0, 0),
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
	if !result.Spliced {
		t.Fatalf("splice failed: %v", result.Reason)
	}

	if caller.NumRegisters[isa.RegisterGeneral] != 5 {
		t.Fatalf("numRegisters[general] %d want 5 (caller's 4 + 1 fresh param slot)",
			caller.NumRegisters[isa.RegisterGeneral])
	}

	foundPreCopy := false
	for _, instr := range caller.Body {
		if instr.Op == isa.OpMoveGeneral && instr.A == 4 && instr.B == 3 {
			foundPreCopy = true
			break
		}
	}
	if !foundPreCopy {
		t.Fatalf("expected opMoveGeneral destination=4 source=3 (pre-copy for struct param), "+
			"body=%+v", caller.Body)
	}

	var setField isa.Instruction
	for _, instr := range caller.Body {
		if instr.Op == isa.OpSetField {
			setField = instr
			break
		}
	}
	if setField.Op != isa.OpSetField {
		t.Fatalf("expected opSetField in inlined body, body=%+v", caller.Body)
	}
	if setField.A != 4 {
		t.Fatalf("inlined SET_FIELD struct operand %d want 4 (fresh slot); "+
			"3 would mean caller's struct mutation", setField.A)
	}

	if setField.C != 2 {
		t.Fatalf("inlined SET_FIELD value operand %d want 2 (aliased to caller argument, no pre-copy)",
			setField.C)
	}
}
