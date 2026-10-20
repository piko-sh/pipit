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

//go:build safe || (js && wasm)

package engine

import (
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// swapStructFieldsGeneralUnsafe takes the reflect path; the safe build has no
// typedmemmove fast path.
//
// Takes vm (*VM) which provides the reflect fallback path.
// Takes frame (*CallFrame) which provides the layout and type tables.
// Takes registers (*Registers) which provides the struct register.
// Takes instruction (instruction) which encodes the operand indices.
// Takes layoutA (program.StructFieldLayout) which is the first field.
// Takes layoutB (program.StructFieldLayout) which is the second field.
//
// Returns the fallback's result.
func swapStructFieldsGeneralUnsafe(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction, layoutA, layoutB program.StructFieldLayout, _ reflect.Type) OpResult {
	return swapStructFieldsGeneralReflectFallback(vm, frame, registers, instruction, layoutA, layoutB)
}
