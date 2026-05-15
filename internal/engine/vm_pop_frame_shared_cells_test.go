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

package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab"
)

func TestPopFrameReleasesTheSharedCellMap(t *testing.T) {
	tests := []struct {
		name      string
		withCells bool
	}{
		{name: "frame that created closures", withCells: true},
		{name: "frame without closures", withCells: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := NewVM(context.Background(), NewGlobalStore(), symtab.NewSymbolRegistry(nil))
			var counts [isa.NumRegisterKinds]uint32
			for kind := range counts {
				counts[kind] = 1
			}
			vm.CallStack = []CallFrame{
				{Function: program.NewNamedFunction("caller"), Registers: NewRegisters(counts)},
				{Function: program.NewNamedFunction("callee"), Registers: NewRegisters(counts)},
			}
			vm.FramePointer = 1
			var cells *sharedCellMap
			if tt.withCells {
				cells = acquireSharedCellMap()
				cells.set(isa.JoinWide(uint8(isa.RegisterInt), 0), &program.UpvalueCell{})
				vm.CallStack[1].sharedCells = cells
			}

			vm.popFrame()

			require.Equal(t, 0, vm.FramePointer)
			require.Nil(t, vm.CallStack[1].sharedCells, "the popped slot must not keep the map")
			if tt.withCells {
				require.Zero(t, cells.inlineLen, "the released map must hold no cells")
			}
		})
	}
}
