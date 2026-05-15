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

// typeSwitchCaseIndex finds the table row whose type is the dynamic type of source by
// comparing reflect.Type identities against the function's type table.
//
// Takes compiledFunction (*CompiledFunction) which owns the type table.
// Takes rows ([]instruction) which are the isa.OpTypeSwitchCase rows of the table.
// Takes source (reflect.Value) which is the unwrapped switched value.
//
// Returns the row index and true on a match, or zero and false when no row matches.
func typeSwitchCaseIndex(compiledFunction *program.CompiledFunction, rows []isa.Instruction, source reflect.Value) (int, bool) {
	sourceType := source.Type()
	if sourceType == runtimeClosureReflectType {
		sourceType = closureSignatureType(source)
	}
	for row, entry := range rows {
		typeIndex := int(entry.A)
		if typeIndex < len(compiledFunction.TypeTable) && compiledFunction.TypeTable[typeIndex] == sourceType {
			return row, true
		}
	}
	return 0, false
}
