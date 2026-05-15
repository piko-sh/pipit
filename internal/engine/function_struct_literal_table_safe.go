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
)

// structLiteralEntryWords is the safe-build twin: no assembly reads the table, so no type
// or flag words are published and every entry keeps a zero type word.
//
// Takes t (reflect.Type) which is the qualifying struct type.
//
// Returns zero words.
func structLiteralEntryWords(reflect.Type) (typeWord, flag uintptr) {
	return 0, 0
}

// structLiteralTableAddress is the safe-build twin: the table is never published to
// assembly.
//
// Takes entries ([]structLiteralEntry) which is the table.
//
// Returns zero.
func structLiteralTableAddress([]program.StructLiteralEntry) uintptr {
	return 0
}
