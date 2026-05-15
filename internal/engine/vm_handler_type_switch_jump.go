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
	"reflect"

	"pipit.sh/pipit/internal/isa"
)

// handleSubOpTypeSwitchJump dispatches a jump-table type switch.
//
// The OpTypeSwitchCase rows that follow the instruction form the table and the tier-1
// jump after them is the default edge. The switched value in general[C] is unwrapped from
// any interface box and compared against each row's type. On a hit the narrowed copy is
// written to general[B] and control transfers to that row's target. Otherwise control
// transfers to the default target.
//
// Takes vm (*VM) which owns the arena receiving the narrowed copy.
// Takes frame (*CallFrame) which provides the body and program counter.
// Takes registers (*Registers) which holds the source and destination.
// Takes instruction (instruction) which encodes the destination and source registers.
//
// Returns OpResult which is opContinue after the jump, or opPanicError when the table is
// not terminated by a default jump inside the body.
func handleSubOpTypeSwitchJump(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	body := frame.Function.Body
	tablePC := frame.ProgramCounter
	defaultPC := tablePC
	for defaultPC < len(body) && body[defaultPC].Op == isa.OpTypeSwitchCase {
		defaultPC++
	}
	if defaultPC >= len(body) || !isa.InstrIsTier1SubOp(body[defaultPC], isa.SubOpJump) {
		vMBoundsError(vm, frame, boundsTableBody, defaultPC, len(body))
		return opPanicError
	}
	source := registers.General[instruction.C]
	if source.IsValid() && source.Kind() == reflect.Interface && !source.IsNil() {
		source = source.Elem()
	}

	source = unwrapAdapterUnderlying(source)
	if source.IsValid() {
		if row, matched := typeSwitchCaseIndex(frame.Function, body[tablePC:defaultPC], source); matched {
			registers.General[instruction.B] = valueCopyForBoundaryArenaWithVM(vm.Arena, vm, source)
			rowPC := tablePC + row
			frame.ProgramCounter = rowPC + 1 + int(body[rowPC].SignedOffset())
			return opContinue
		}
	}
	frame.ProgramCounter = defaultPC + 1 + int(body[defaultPC].SignedOffset())
	return opContinue
}
