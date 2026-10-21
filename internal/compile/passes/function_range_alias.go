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
	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// aliasReadOnlyRangeValues resolves every range-value snapshot candidate in the body.
//
// Takes compiledFunction (*program.CompiledFunction) whose body is rewritten in place.
func aliasReadOnlyRangeValues(compiledFunction *program.CompiledFunction) {
	body := compiledFunction.Body
	var flow *controlFlowLists
	for pc, inst := range body {
		if inst.Op != isa.OpMoveGeneral || inst.C != isa.MoveGeneralModeSnapshotRangeCandidate {
			continue
		}
		if inst.A != inst.B {
			body[pc].C = engine.MoveGeneralModeSnapshot
			continue
		}
		if flow == nil {
			flow = controlFlowListsOf(body)
		}
		if loopRemainderIsHeapPure(compiledFunction, flow, pc) {
			body[pc] = isa.NewInstruction(isa.OpNoOperation, 0, 0, 0)
			continue
		}
		body[pc].C = engine.MoveGeneralModeSnapshot
	}
}
