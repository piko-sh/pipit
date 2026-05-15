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

//go:build !safe && !(js && wasm)

package engine

import (
	"reflect"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// typeSwitchCaseIndex finds the table row whose type is the dynamic type of source by
// comparing runtime type words against the function's cached per-type-table words, so a
// hit costs one pointer compare per row and no reflect call.
//
// Takes compiledFunction (*CompiledFunction) which owns the type table and its type-word
// cache.
// Takes rows ([]instruction) which are the isa.OpTypeSwitchCase rows of the table.
// Takes source (reflect.Value) which is the unwrapped switched value.
//
// Returns the row index and true on a match, or zero and false when no row matches.
func typeSwitchCaseIndex(compiledFunction *program.CompiledFunction, rows []isa.Instruction, source reflect.Value) (int, bool) {
	words := layoutTypeWordTable(compiledFunction)
	sourceWord := (*unsafeReflectValue)(unsafe.Pointer(&source)).typ
	if source.Type() == runtimeClosureReflectType {
		signature := closureSignatureType(source)
		for row, entry := range rows {
			typeIndex := int(entry.A)
			if signature != nil && typeIndex < len(compiledFunction.TypeTable) && compiledFunction.TypeTable[typeIndex] == signature {
				return row, true
			}
		}
		return 0, false
	}
	for row, entry := range rows {
		typeIndex := int(entry.A)
		if typeIndex < len(words) && words[typeIndex].Value == sourceWord {
			return row, true
		}
	}
	return 0, false
}
