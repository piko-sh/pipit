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
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// cacheIndirectTarget is a no-op in the safe build, where closure writes always take the
// reflect path.
//
// Takes cell (*UpvalueCell) which is ignored in the safe build.
func cacheIndirectTarget(cell *program.UpvalueCell) {}

// writeIndirectCellDirect never stores directly in the safe build.
//
// Takes arena (*RegisterArena) which is ignored in the safe build.
// Takes cell (*UpvalueCell) which is ignored in the safe build.
// Takes registers (*Registers) which is ignored in the safe build.
// Takes kind (isa.RegisterKind) which is ignored in the safe build.
// Takes index (byte) which is ignored in the safe build.
//
// Returns false so the caller takes the reflect path.
func writeIndirectCellDirect(arena *RegisterArena, cell *program.UpvalueCell, registers *Registers, kind isa.RegisterKind, index byte) bool {
	return false
}
