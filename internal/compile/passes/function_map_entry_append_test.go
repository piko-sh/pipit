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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestFuseMapEntryAppendJoinsTheLookupTheAppendAndTheStore(t *testing.T) {
	t.Parallel()
	const receiver, held, grown, key, element = 0, 1, 2, 4, 5
	nop := mk(isa.OpNoOperation, 0, 0, 0)
	lookup := mk(isa.OpGetMapStringGeneral, held, receiver, key)
	grow := mk(isa.OpAppendString, grown, held, element)
	store := mk(isa.OpSetMapStringGeneral, receiver, key, grown)
	fused := mk(isa.OpAppendStringToMapString, receiver, key, element)
	function := func(words ...isa.Instruction) *program.CompiledFunction {
		return &program.CompiledFunction{Body: append(slices.Clone(words), returnVoid())}
	}

	compiledFunction := function(lookup, grow, store)
	require.True(t, fuseMapEntryAppend(compiledFunction, compiledFunction.Body, 0, nil))
	require.Equal(t, []isa.Instruction{fused, nop, nop, returnVoid()}, compiledFunction.Body)

	compiledFunction = function(lookup, nop, grow, nop, store)
	require.True(t, fuseMapEntryAppend(compiledFunction, compiledFunction.Body, 0, nil), "no-operation words between the three are passed over")
	require.Equal(t, []isa.Instruction{fused, nop, nop, nop, nop, returnVoid()}, compiledFunction.Body)

	refusals := []struct {
		name    string
		words   []isa.Instruction
		targets map[int]bool
	}{
		{name: "a jump lands on the append", words: []isa.Instruction{lookup, grow, store}, targets: map[int]bool{1: true}},
		{name: "a jump lands on the store", words: []isa.Instruction{lookup, grow, store}, targets: map[int]bool{2: true}},
		{name: "a jump lands on a no-operation between them", words: []isa.Instruction{lookup, nop, grow, store}, targets: map[int]bool{1: true}},
		{name: "the append grows another slice", words: []isa.Instruction{lookup, mk(isa.OpAppendString, grown, 3, element), store}},
		{name: "the append grows its operand in place", words: []isa.Instruction{lookup, mk(isa.OpAppendString, held, held, element),
			mk(isa.OpSetMapStringGeneral, receiver, key, held)}},
		{name: "the store names another map", words: []isa.Instruction{lookup, grow, mk(isa.OpSetMapStringGeneral, 3, key, grown)}},
		{name: "the store names another key", words: []isa.Instruction{lookup, grow, mk(isa.OpSetMapStringGeneral, receiver, 6, grown)}},
		{name: "the store stores another slice", words: []isa.Instruction{lookup, grow, mk(isa.OpSetMapStringGeneral, receiver, key, 3)}},
		{name: "the lookup overwrites the map", words: []isa.Instruction{mk(isa.OpGetMapStringGeneral, receiver, receiver, key),
			mk(isa.OpAppendString, grown, receiver, element), store}},
		{name: "an instruction stands between the lookup and the append", words: []isa.Instruction{lookup,
			isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, 0, 1), grow, store}},
		{name: "the slice the map held is read afterwards", words: []isa.Instruction{lookup, grow, store,
			mk(isa.OpMoveGeneral, 3, held, 0)}},
		{name: "the grown slice is read afterwards", words: []isa.Instruction{lookup, grow, store,
			mk(isa.OpMoveGeneral, 3, grown, 0)}},
		{name: "the body ends before the store", words: []isa.Instruction{lookup, grow}},
	}
	for _, refusal := range refusals {
		compiledFunction = function(refusal.words...)
		before := slices.Clone(compiledFunction.Body)
		require.False(t, fuseMapEntryAppend(compiledFunction, compiledFunction.Body, 0, refusal.targets), refusal.name)
		require.Equal(t, before, compiledFunction.Body, refusal.name)
	}
}
