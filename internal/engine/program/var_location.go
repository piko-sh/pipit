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

package program

import (
	"reflect"

	"pipit.sh/pipit/internal/isa"
)

// GlobalVariableInfo describes a package-level variable's location in the GlobalStore.
type GlobalVariableInfo struct {
	// Index is the slot index within the GlobalStore for the variable.
	Index int

	// Kind is the register kind that determines which typed store holds the variable.
	Kind isa.RegisterKind

	// IsIndirect marks a package variable whose address the program takes. Index then names
	// a general-bank slot holding a pointer cell; reads and writes go through it.
	IsIndirect bool
}

// SlotKind returns the register bank whose numbering Index uses: the general bank for an
// indirect variable, whose slot holds the pointer cell, otherwise the value's own Kind.
//
// Returns isa.RegisterKind which selects the bank of the slot.
func (g GlobalVariableInfo) SlotKind() isa.RegisterKind {
	if g.IsIndirect {
		return isa.RegisterGeneral
	}
	return g.Kind
}

// VarLocation records where a variable's value lives at runtime. The field layout is
// accessed by ASM dispatch code via auto-generated VL_* offsets in
// asm_dispatch_offsets.h.
//
//exhaustruct:ignore
type VarLocation struct {
	// SourceType is the exact source-level reflect.Type of the value this location holds,
	// used by emitBoxToGeneral to preserve the precise type identity (such as int vs int64)
	// so type assertions do not misfire. nil when the type is unknown.
	SourceType reflect.Type

	// UpvalueIndex is the index into the upvalue table when IsUpvalue is true.
	UpvalueIndex int

	// SpillSlot is the spill slot index (0-based) when IsSpilled is true. The actual
	// register file index is 256 + SpillSlot.
	SpillSlot uint16

	// Register is the register index within the bank.
	Register uint8

	// Kind is the register bank (int, float, string, general, bool, uint, complex).
	Kind isa.RegisterKind

	// IsUpvalue is true when the variable has been captured by a closure and lives on the
	// heap rather than in a register.
	IsUpvalue bool

	// IsIndirect is true when the variable's address has been taken (&x). The general
	// register holds a pointer (reflect.Value of *T) and reads/writes must go through the
	// pointee.
	IsIndirect bool

	// OriginalKind stores the register bank the variable had before it was heap-escaped by
	// &x. Used to unpack indirect reads back into the correct typed register.
	OriginalKind isa.RegisterKind

	// IsCaptured is true when the variable has been captured by a closure. Writes to
	// captured variables must Emit opWriteSharedCell to keep the upvalue cell in sync with
	// the register.
	IsCaptured bool

	// IsSpilled is true when the variable lives in the spill area (register file index >=
	// 256) rather than a directly-addressable register. Must be materialised via
	// isa.SubOpReload before use in instructions and stored back via isa.SubOpSpill after
	// writes.
	IsSpilled bool
}
