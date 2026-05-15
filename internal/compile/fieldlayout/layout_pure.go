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

// Package fieldlayout computes struct field layouts: walking a field path through reflect
// and go/types to decide whether a leaf can take a typed-register fast path, and the key
// the compiler interns layouts under. Pure functions; the interning stays with the
// Compiler.
package fieldlayout

import (
	"math"
	"reflect"
	"strconv"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

// StructFieldLayoutKey is the dedupe key used by the Compiler to share a single
// structLayoutTable entry across all field-access sites targeting the same (struct type,
// field path) pair within one CompiledFunction.
type StructFieldLayoutKey struct {
	// StructType is the reflect.Type of the deref'd containing struct.
	StructType reflect.Type

	// FieldPath is the joined reflect.StructField.Index sequence.
	FieldPath string
}

// WalkStructFieldPath walks the field-index chain into the struct.
//
// Takes reflectStructType (reflect.Type) which is the root struct.
// Takes fieldPath ([]int) which is the field-index chain.
//
// Returns the leaf reflect.Type, the cumulative byte offset, the encoded path, and true
// on success. Returns false when any step is out of bounds or the offset overflows
// uint32.
func WalkStructFieldPath(reflectStructType reflect.Type, fieldPath []int) (reflect.Type, uint32, [isa.StructFieldLayoutMaxPathDepth]uint8, bool) {
	var totalOffset uint32
	var encodedPath [isa.StructFieldLayoutMaxPathDepth]uint8
	currentType := reflectStructType
	for depth, fieldIndex := range fieldPath {
		if currentType.Kind() != reflect.Struct {
			return nil, 0, encodedPath, false
		}
		if fieldIndex < 0 || fieldIndex >= currentType.NumField() {
			return nil, 0, encodedPath, false
		}
		field := currentType.Field(fieldIndex)
		if field.Offset > math.MaxUint32 {
			return nil, 0, encodedPath, false
		}
		totalOffset += uint32(field.Offset)
		encodedPath[depth] = safeconv.MustIntToUint8(fieldIndex)
		if depth == len(fieldPath)-1 {
			currentType = field.Type
			continue
		}
		next := field.Type
		if next.Kind() == reflect.Pointer || next.Kind() != reflect.Struct {
			return nil, 0, encodedPath, false
		}
		currentType = next
	}
	return currentType, totalOffset, encodedPath, true
}

// LeafRegisterKindAdmitted reports whether a leaf is eligible for the typed-register fast
// path. General-bank leaves are admitted only for kinds the unsafe handler can snapshot,
// excluding Struct which has no single-word representation.
//
// Takes leafRegisterKind (isa.RegisterKind) which is the register kind of the leaf field.
// Takes leafKind (reflect.Kind) which is the reflect kind of the leaf field.
//
// Returns true when the leaf is admitted.
func LeafRegisterKindAdmitted(leafRegisterKind isa.RegisterKind, leafKind reflect.Kind) bool {
	if leafRegisterKind != isa.RegisterGeneral {
		return true
	}
	switch leafKind {
	case reflect.Pointer,
		reflect.Interface,
		reflect.Slice,
		reflect.Array,
		reflect.Map,
		reflect.Chan,
		reflect.Func:
		return true
	default:
	}
	return false
}

// LeafFieldIsCycleBroken reports whether the leaf field carries the cycle-broken marker
// tag.
//
// The marker is stamped by buildStructFields when the declared field type referenced the
// still-under-construction type. Layouts flagged this way are eligible for the
// isa.OpGetStructFieldRawPointerT0 fast path because the runtime held value is provably a
// pointer.
//
// Takes reflectStructType (reflect.Type) which is the root struct reflect.Type to walk
// from.
// Takes fieldPath ([]int) which is the field-index chain leading to the leaf.
//
// Returns true when the leaf carries the cycle-broken marker tag; false otherwise.
func LeafFieldIsCycleBroken(reflectStructType reflect.Type, fieldPath []int) bool {
	if len(fieldPath) == 0 {
		return false
	}
	currentType := reflectStructType
	for currentType.Kind() == reflect.Pointer {
		currentType = currentType.Elem()
	}
	for step, fieldIndex := range fieldPath {
		if currentType.Kind() != reflect.Struct {
			return false
		}
		if fieldIndex >= currentType.NumField() {
			return false
		}
		field := currentType.Field(fieldIndex)
		if step == len(fieldPath)-1 {
			return field.Tag.Get(isa.CycleBrokenTagKey) == isa.CycleBrokenTagValue
		}
		currentType = field.Type
		for currentType.Kind() == reflect.Pointer {
			currentType = currentType.Elem()
		}
	}
	return false
}

// StructFieldLayoutSupportsKind reports whether a reflect.Kind is one of the leaf kinds
// the struct-field fast path supports.
//
// Takes kind (reflect.Kind) which is the leaf field's kind.
//
// Returns true when the kind is in the supported set.
func StructFieldLayoutSupportsKind(kind reflect.Kind) bool {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64,
		reflect.Bool,
		reflect.String,
		reflect.Pointer,
		reflect.Interface,
		reflect.Slice,
		reflect.Array,
		reflect.Map, reflect.Chan, reflect.Func:
		return true
	default:
	}
	return false
}

// StructFieldLayoutIndexFitsTier0 reports whether the index fits in a tier-0 uint8
// operand.
//
// Takes layoutIndex (uint16) which is the structLayoutTable index.
//
// Returns true when the index fits in a uint8 operand.
func StructFieldLayoutIndexFitsTier0(layoutIndex uint16) bool {
	const tier0MaxLayoutIndex uint16 = 256
	return layoutIndex < tier0MaxLayoutIndex
}

// EncodeFieldPath serialises an []int field path into a compact string suitable for use
// as a dedupe map key. Used inside StructFieldLayoutKey to compare embedded paths without
// allocating a per-key slice.
//
// Takes path ([]int) which is the selection.Index() field path.
//
// Returns a stable string encoding suitable for map-key comparison.
func EncodeFieldPath(path []int) string {
	const reservedBytesPerEntry = 5
	const decimalBase = 10
	buffer := make([]byte, 0, len(path)*reservedBytesPerEntry)
	for _, index := range path {
		buffer = strconv.AppendInt(buffer, int64(index), decimalBase)
		buffer = append(buffer, ',')
	}
	return string(buffer)
}
