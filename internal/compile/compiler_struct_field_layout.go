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

package compile

import (
	"context"
	"go/types"
	"reflect"

	"pipit.sh/pipit/internal/compile/fieldlayout"
	"pipit.sh/pipit/internal/compile/isaselect"
	"pipit.sh/pipit/internal/compile/typemap"
	"pipit.sh/pipit/internal/engine/program"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// StructFieldLayoutFlagEmbedded indicates that the layout entry walks one or more
	// embedded value-struct fields before reaching the leaf, shared with the runtime through
	// program.StructFieldLayoutFlagEmbedded.
	StructFieldLayoutFlagEmbedded = program.StructFieldLayoutFlagEmbedded

	// structFieldLayoutFlagCycleBroken marks an any-typed leaf whose declared type was *Self
	// (or a container of *Self), substituted because reflect cannot construct it during the
	// type build. The runtime value is provably a pointer, so the fast path can skip the
	// abi.Type kind walk.
	structFieldLayoutFlagCycleBroken = program.StructFieldLayoutFlagCycleBroken
)

// tryResolveStructFieldLayout attempts to register a fast-path layout entry for a struct
// field access. Receivers containing abstract type parameters are refused because the
// abstract layout's field sizes would not match the concrete instantiation.
//
// Takes selection (*types.Selection) which is the type-checker's selection metadata for
// the field access.
//
// Returns (uint16, true) with the structLayoutTable index on success.
// Returns (0, false) when the access is not eligible.
func (c *Compiler) tryResolveStructFieldLayout(ctx context.Context, selection *types.Selection) (uint16, bool) {
	if selection == nil || selection.Kind() != types.FieldVal {
		return 0, false
	}
	fieldPath := selection.Index()
	if len(fieldPath) == 0 {
		return 0, false
	}
	receiverType := selection.Recv()
	if receiverType == nil {
		return 0, false
	}
	if _, isInterface := receiverType.Underlying().(*types.Interface); isInterface {
		return 0, false
	}
	if typemap.ContainsTypeParameter(receiverType) {
		return 0, false
	}
	reflectStructType := c.TypeToReflect(ctx, receiverType)
	if reflectStructType == nil {
		return 0, false
	}
	return c.registerStructFieldLayoutFromReflect(reflectStructType, fieldPath)
}

// registerStructFieldLayoutFromReflect registers a fast-path layout entry directly from a
// reflect.Type and field path.
//
// Takes reflectStructType (reflect.Type) which must be a struct kind (pointer-wrapped is
// unwrapped by the caller).
// Takes fieldPath ([]int) which is the field-index chain into the struct (length 1 for
// top-level fields, >1 for embedded selections).
//
// Returns (structLayoutTable index, true) on success, or (0, false) when the access is
// not eligible.
func (c *Compiler) registerStructFieldLayoutFromReflect(reflectStructType reflect.Type, fieldPath []int) (uint16, bool) {
	for reflectStructType.Kind() == reflect.Pointer {
		reflectStructType = reflectStructType.Elem()
	}
	if reflectStructType.Kind() != reflect.Struct {
		return 0, false
	}
	if len(fieldPath) > isa.StructFieldLayoutMaxPathDepth {
		return 0, false
	}

	leafType, totalOffset, encodedPath, ok := fieldlayout.WalkStructFieldPath(reflectStructType, fieldPath)
	if !ok {
		return 0, false
	}
	leafKind := leafType.Kind()
	if !fieldlayout.StructFieldLayoutSupportsKind(leafKind) {
		return 0, false
	}
	leafRegisterKind := isaselect.RegisterKindForReflectKind(leafKind)
	if !fieldlayout.LeafRegisterKindAdmitted(leafRegisterKind, leafKind) {
		return 0, false
	}

	layout, ok := c.buildStructFieldLayout(reflectStructType, leafType, leafRegisterKind, leafKind, totalOffset, encodedPath, fieldPath)
	if !ok {
		return 0, false
	}
	return c.internStructFieldLayout(reflectStructType, fieldPath, layout)
}

// buildStructFieldLayout constructs the StructFieldLayout record from the walk results
// and the leaf metadata.
//
// Takes reflectStructType (reflect.Type) which is the root struct reflect.Type the access
// is rooted at.
// Takes leafType (reflect.Type) which is the reflect.Type of the resolved leaf field.
// Takes leafRegisterKind (isa.RegisterKind) which is the register kind of the leaf field.
// Takes leafKind (reflect.Kind) which is the reflect kind of the leaf field.
// Takes totalOffset (uint32) which is the cumulative byte offset of the leaf within the
// deref'd struct.
// Takes encodedPath which is the field-index path padded to
// isa.StructFieldLayoutMaxPathDepth.
// Takes fieldPath ([]int) which is the original field-index chain from the type-checker.
//
// Returns the populated StructFieldLayout record.
func (c *Compiler) buildStructFieldLayout(
	reflectStructType, leafType reflect.Type,
	leafRegisterKind isa.RegisterKind,
	leafKind reflect.Kind,
	totalOffset uint32,
	encodedPath [isa.StructFieldLayoutMaxPathDepth]uint8,
	fieldPath []int,
) (program.StructFieldLayout, bool) {
	typeIndex, err := program.AddTypeRef(c.Function, reflectStructType)
	if err != nil {
		c.recordStickyError(err)
		return program.StructFieldLayout{}, false
	}
	var flags uint8
	if len(fieldPath) > 1 {
		flags |= StructFieldLayoutFlagEmbedded
	}
	if fieldlayout.LeafFieldIsCycleBroken(reflectStructType, fieldPath) {
		flags |= structFieldLayoutFlagCycleBroken
	}
	var fieldTypeIndex uint16
	if leafRegisterKind == isa.RegisterGeneral {
		index, leafErr := program.AddTypeRef(c.Function, leafType)
		if leafErr != nil {
			c.recordStickyError(leafErr)
			return program.StructFieldLayout{}, false
		}
		fieldTypeIndex = index
	}
	return program.StructFieldLayout{
		Offset:         totalOffset,
		TypeIndex:      typeIndex,
		Path:           encodedPath,
		PathLength:     safeconv.MustIntToUint8(len(fieldPath)),
		Kind:           safeconv.UintToUint8(uint(leafKind)),
		RegisterKind:   uint8(leafRegisterKind),
		Flags:          flags,
		FieldTypeIndex: fieldTypeIndex,
	}, true
}

// internStructFieldLayout dedupes layout against c.structLayoutIndex.
//
// When the same (struct type, field path) pair was already registered, the existing index
// is returned; otherwise a fresh layout is appended to the StructLayoutTable and its
// newly allocated index is returned.
//
// Takes reflectStructType (reflect.Type) which is the root struct reflect.Type the layout
// is keyed on.
// Takes fieldPath ([]int) which is the field-index chain forming the second half of the
// key.
// Takes layout (StructFieldLayout) which is the layout record to intern when no match
// exists.
//
// Returns the StructLayoutTable index (existing or newly allocated) and true on success.
func (c *Compiler) internStructFieldLayout(reflectStructType reflect.Type, fieldPath []int, layout program.StructFieldLayout) (uint16, bool) {
	key := fieldlayout.StructFieldLayoutKey{
		StructType: reflectStructType,
		FieldPath:  fieldlayout.EncodeFieldPath(fieldPath),
	}
	if c.structLayoutIndex == nil {
		c.structLayoutIndex = make(map[fieldlayout.StructFieldLayoutKey]uint16)
	}
	if existingIndex, ok := c.structLayoutIndex[key]; ok {
		return existingIndex, true
	}
	index := safeconv.MustIntToUint16(len(c.Function.StructLayoutTable))
	c.Function.StructLayoutTable = append(c.Function.StructLayoutTable, layout)
	if c.structLayoutIndex == nil {
		c.structLayoutIndex = make(map[fieldlayout.StructFieldLayoutKey]uint16)
	}
	c.structLayoutIndex[key] = index
	return index, true
}

// emitStructFieldLayoutExtension emits the isa.OpExt extension word carrying the 16-bit
// StructLayoutTable index following a subOpGet/SetStructFieldXxx primary word.
//
// Takes layoutIndex (uint16) which is the structLayoutTable index to encode.
func (c *Compiler) emitStructFieldLayoutExtension(layoutIndex uint16) {
	program.Emit(c.Function, isa.OpExt, uint8(layoutIndex&0xFF), uint8(layoutIndex>>8), 0)
}
