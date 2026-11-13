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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func pointerSwapBody() []isa.Instruction {
	const pointer = 0
	return []isa.Instruction{
		mk(isa.OpDereference, 2, pointer, 0),
		mk(isa.OpMoveGeneral, 3, 2, engine.MoveGeneralModeDynamic),
		mk(isa.OpDereference, 4, pointer, 0),
		mk(isa.OpMoveGeneral, 5, 4, engine.MoveGeneralModeDynamic),
		mk(isa.OpGetElementInt64ViaPointer, 6, pointer, 0),
		mk(isa.OpGetElementInt64ViaPointer, 7, pointer, 3),
		mk(isa.OpSetElementInt64, 3, 3, 6),
		mk(isa.OpSetElementInt64, 5, 0, 7),
		returnVoid(),
	}
}

func rewritePointerStores(compiledFunction *program.CompiledFunction, targets map[int]bool) bool {
	changed := false
	for index := range compiledFunction.Body {
		changed = storeElementThroughItsPointer(compiledFunction, compiledFunction.Body, index, targets) || changed
	}
	return changed
}

func TestASwapThroughAPointerStoresThroughThePointer(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{Body: pointerSwapBody()}

	require.True(t, rewritePointerStores(compiledFunction, nil))

	require.Equal(t, []isa.Instruction{
		nop(), nop(), nop(), nop(),
		mk(isa.OpGetElementInt64ViaPointer, 6, 0, 0),
		mk(isa.OpGetElementInt64ViaPointer, 7, 0, 3),
		mk(isa.OpSetElementInt64ViaPointer, 0, 3, 6),
		mk(isa.OpSetElementInt64ViaPointer, 0, 0, 7),
		returnVoid(),
	}, compiledFunction.Body, "both dereferences and both moves are gone, and the stores name the pointer")
}

func TestAStoreNamingTheViewItselfGoesThroughThePointer(t *testing.T) {
	t.Parallel()
	compiledFunction := &program.CompiledFunction{Body: []isa.Instruction{
		mk(isa.OpDereference, 2, 0, 0),
		mk(isa.OpGetElementInt64ViaPointer, 6, 0, 1),
		mk(isa.OpSetElementInt64, 2, 3, 6),
		returnVoid(),
	}}

	require.True(t, rewritePointerStores(compiledFunction, nil))
	require.Equal(t, []isa.Instruction{
		nop(), mk(isa.OpGetElementInt64ViaPointer, 6, 0, 1), mk(isa.OpSetElementInt64ViaPointer, 0, 3, 6), returnVoid(),
	}, compiledFunction.Body)
}

func TestAStoreKeepsItsDereferenceWhereThePointerStoreCouldDiffer(t *testing.T) {
	t.Parallel()
	with := func(change func(body []isa.Instruction) []isa.Instruction) []isa.Instruction {
		return change(pointerSwapBody())
	}
	insert := func(at int, instruction isa.Instruction) func([]isa.Instruction) []isa.Instruction {
		return func(body []isa.Instruction) []isa.Instruction { return slices.Insert(body, at, instruction) }
	}
	replace := func(at int, instruction isa.Instruction) func([]isa.Instruction) []isa.Instruction {
		return func(body []isa.Instruction) []isa.Instruction { body[at] = instruction; return body }
	}
	for name, tt := range map[string]struct {
		body    []isa.Instruction
		targets map[int]bool
		want    int
	}{
		"nothing reads through the pointer as a slice of 8-byte ints": {
			body: with(func(body []isa.Instruction) []isa.Instruction {
				body[4], body[5] = mk(isa.OpLoadInt64Constant, 6, 0, 0), mk(isa.OpLoadInt64Constant, 7, 0, 0)
				return body
			}),
		},
		"a jump lands between the dereferences and the stores": {body: pointerSwapBody(), targets: map[int]bool{5: true}},
		"the pointer is written on the way":                    {body: with(insert(6, mk(isa.OpMoveGeneral, 0, 1, engine.MoveGeneralModeAlias)))},
		"a call lies on the way":                               {body: with(insert(6, isa.NewTier1Instruction(isa.SubOpCall, 0, 0)))},
		"a read of another collection comes before the pointer is dereferenced again": {
			body: []isa.Instruction{
				mk(isa.OpDereference, 2, 0, 0),
				mk(isa.OpGetElementInt64, 6, 1, 0),
				mk(isa.OpGetElementInt64ViaPointer, 7, 0, 3),
				mk(isa.OpSetElementInt64, 2, 3, 6),
				returnVoid(),
			},
		},
		"the view is a snapshot": {body: with(replace(0, mk(isa.OpDereference, 2, 0, engine.DerefSnapshot))), want: 1},
		"the move copies the header": {
			body: with(replace(1, mk(isa.OpMoveGeneral, 3, 2, engine.MoveGeneralModeSnapshot))), want: 1,
		},
		"the collection is read after the store": {
			body: with(insert(8, mk(isa.OpMoveGeneral, 1, 3, engine.MoveGeneralModeAlias))), want: 1,
		},
		"the view is read by something else": {
			body: with(insert(6, mk(isa.OpMoveGeneral, 1, 2, engine.MoveGeneralModeAlias))), want: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: tt.body}
			rewritePointerStores(compiledFunction, tt.targets)
			stores := 0
			for _, instruction := range compiledFunction.Body {
				if instruction.Op == isa.OpSetElementInt64ViaPointer {
					stores++
				}
			}
			require.Equal(t, tt.want, stores, "the stores that may still go through the pointer")
		})
	}
}
