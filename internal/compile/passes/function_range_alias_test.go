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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestARangeValueIsAliasedOnlyWhenNothingStoresBeforeItIsTakenAgain(t *testing.T) {
	t.Parallel()

	const candidate = 1
	snapshot := mk(isa.OpMoveGeneral, 1, 1, isa.MoveGeneralModeSnapshotRangeCandidate)
	read := mk(isa.OpGetStructFieldFloat64, 0, 1, 0)
	store := mk(isa.OpSetStructFieldFloat64, 2, 0, 0)
	jump := isa.NewTier1Instruction(isa.SubOpJump, 0, 0)
	latch := mk(isa.OpJumpIfTrue, 0, 0, 0)

	tests := []struct {
		name      string
		body      []isa.Instruction
		targets   map[int]int
		callSites []program.CallSite
		wantAlias bool
	}{
		{
			name:      "a loop that only reads",
			body:      []isa.Instruction{nop(), snapshot, read, latch, returnVoid()},
			targets:   map[int]int{3: 0},
			wantAlias: true,
		},
		{
			name:    "a loop that stores",
			body:    []isa.Instruction{nop(), snapshot, read, store, latch, returnVoid()},
			targets: map[int]int{4: 0},
		},
		{
			name: "a loop that jumps out to a store appended after it and back",
			body: []isa.Instruction{
				nop(), snapshot, jump, read, latch, returnVoid(),
				store, jump,
			},
			targets: map[int]int{2: 6, 4: 0, 7: 3},
		},
		{
			name: "a loop that jumps out to an impure call appended after it and back",
			body: []isa.Instruction{
				nop(), snapshot, jump, read, latch, returnVoid(),
				isa.NewTier1Instruction(isa.SubOpCall, 0, 0), jump,
			},
			targets:   map[int]int{2: 6, 4: 0, 7: 3},
			callSites: []program.CallSite{{CachedCallee: &program.CompiledFunction{Name: "impure"}}},
		},
		{
			name:      "a store the loop reaches only on its way out",
			body:      []isa.Instruction{nop(), snapshot, read, latch, store, returnVoid()},
			targets:   map[int]int{3: 0},
			wantAlias: true,
		},
		{
			name: "a store appended after the loop that nothing in it jumps to",
			body: []isa.Instruction{
				nop(), snapshot, read, latch, returnVoid(),
				store, returnVoid(),
			},
			targets:   map[int]int{3: 0},
			wantAlias: true,
		},
		{
			name: "a move outside any loop",
			body: []isa.Instruction{nop(), snapshot, read, returnVoid()},
		},
		{
			name: "a store in an enclosing loop",
			body: []isa.Instruction{
				nop(), snapshot, read, latch, store, latch, returnVoid(),
			},
			targets: map[int]int{3: 1, 5: 0},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			function := &program.CompiledFunction{Body: withJumpTargets(t, tt.body, tt.targets), CallSites: tt.callSites}

			aliasReadOnlyRangeValues(function)

			if tt.wantAlias {
				require.True(t, isCanonicalNoOperation(function.Body[candidate]), "the copy is dropped: the value cannot change while it is in use")
				return
			}
			require.Equal(t, mk(isa.OpMoveGeneral, 1, 1, engine.MoveGeneralModeSnapshot), function.Body[candidate],
				"the element may be stored to while the range value is in use, so the value is copied")
		})
	}
}
