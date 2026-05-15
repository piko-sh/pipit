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
	"strings"

	"pipit.sh/pipit/internal/isa"
)

const (
	// StructFieldLayoutFlagEmbedded marks a row that walks one or more embedded value-struct
	// fields before reaching the leaf.
	StructFieldLayoutFlagEmbedded uint8 = 1 << 0

	// StructFieldLayoutFlagCycleBroken marks an any-typed leaf the compiler substituted for
	// a self-referential pointer field; the held value is always a pointer of the
	// cycle-causing type.
	StructFieldLayoutFlagCycleBroken uint8 = 1 << 1
)

// StructFieldLayout describes the compile-time-resolved memory layout of a single struct
// field, enabling direct unsafe-pointer typed access without entering reflect.
type StructFieldLayout struct {
	// Offset is the byte offset of the leaf field within the deref'd struct. The unsafe
	// build adds this to the struct base pointer and casts to the field's typed pointer
	// directly.
	Offset uint32

	// TypeIndex is the typeTable index of the deref'd struct type. Kept so verifier panics
	// can name the struct involved.
	TypeIndex uint16

	// Path holds the field indices walked from the struct root to the leaf field, with
	// length PathLength and entries beyond that zero-padded. The safe build walks each level
	// via reflect.Value.Field while the unsafe build uses Offset directly.
	Path [isa.StructFieldLayoutMaxPathDepth]uint8

	// PathLength is the number of valid entries in Path.
	PathLength uint8

	// Kind is the reflect.Kind of the leaf field, stored as uint8 so the safe build compiles
	// without unsafe. Runtime handlers cast back to reflect.Kind for the typed load/store
	// dispatch.
	Kind uint8

	// RegisterKind is the RegisterKind of the leaf field. Determines which sub-op the
	// Compiler emits and which register bank the runtime handler reads/writes.
	RegisterKind uint8

	// Flags carries metadata about the layout. See structFieldLayoutFlag* constants.
	Flags uint8

	// FieldTypeIndex is the typeTable index of the leaf field's reflect.Type. Populated only
	// for registerGeneral leaves; zero for scalar leaves.
	FieldTypeIndex uint16
}

// StructLiteralEntry is one row of CompiledFunction.StructLiteralTable, indexed by
// TypeTable position. The assembly struct-literal allocation handler reads it directly,
// so the field order is pinned through the SLE_* offsets in the generated header.
type StructLiteralEntry struct {
	// TypeWord is the runtime type word of the struct type, or zero when the type does not
	// qualify for arena allocation and the handler must fall back to Go.
	TypeWord uintptr

	// Size is the struct's byte size.
	Size uintptr

	// AlignMask is the struct's alignment minus one, applied to the bump cursor.
	AlignMask uintptr

	// Flag is the reflect.Value flag word of an addressable, indirect struct view.
	Flag uintptr
}

// IsAnonymousField reports whether field represents an embedded field at the source
// level, including unexported types renamed with the isa.EmbeddedUnexportedPrefix marker.
//
// Takes field (reflect.StructField) which is the reflect.StructField to test.
//
// Returns true when field is embedded (anonymous or marker-prefixed).
func IsAnonymousField(field reflect.StructField) bool {
	if field.Anonymous {
		return true
	}
	return strings.HasPrefix(field.Name, isa.EmbeddedUnexportedPrefix)
}
