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
	"unsafe"

	"pipit.sh/pipit/internal/isa"
)

// UpvalueCell is a heap-allocated box for a captured variable. All closures that capture
// the same variable share the same cell.
type UpvalueCell struct {
	// IndirectType caches the pointee's runtime type word, which runtimeTypedmemmove needs
	// to keep the write barrier for pointer-carrying pointees (strings, slice headers).
	IndirectType unsafe.Pointer

	// IndirectTarget caches the pointee's address for direct stores. Populated once at cell
	// creation by cacheIndirectTarget; nil in the safe build and for ineligible cells.
	IndirectTarget unsafe.Pointer

	// GeneralValue holds the captured value when the kind is registerGeneral.
	GeneralValue reflect.Value

	// StringValue holds the captured value when the kind is registerString.
	StringValue string

	// SliceIntValue holds the captured slice header when the kind is registerSliceInt. Slice
	// headers are value types; mutating elements affects the array shared with the declaring
	// frame, and re-slicing produces a fresh header local to the closure.
	SliceIntValue []int64

	// SliceFloatValue mirrors SliceIntValue for registerSliceFloat.
	SliceFloatValue []float64

	// SliceStringValue mirrors SliceIntValue for registerSliceString.
	SliceStringValue []string

	// SliceBoolValue mirrors SliceIntValue for registerSliceBool.
	SliceBoolValue []bool

	// SliceUintValue mirrors SliceIntValue for registerSliceUint.
	SliceUintValue []uint64

	// SliceByteValue mirrors SliceIntValue for registerSliceByte.
	SliceByteValue []byte

	// ComplexValue holds the captured value when the kind is registerComplex.
	ComplexValue complex128

	// UintValue holds the captured value when the kind is registerUint.
	UintValue uint64

	// IndirectKind caches the pointee's kind for an indirect cell so a closure write can
	// pick the direct store path without a reflect call. reflect.Invalid when the cache is
	// unpopulated or the pointee shape is not eligible.
	IndirectKind reflect.Kind

	// IndirectElemKind caches the element kind of a slice pointee, so a typed-bank slice
	// header is only copied over a pointee whose elements share its layout.
	IndirectElemKind reflect.Kind

	// FloatValue holds the captured value when the kind is registerFloat.
	FloatValue float64

	// IntValue holds the captured value when the kind is registerInt.
	IntValue int64

	// BoolValue holds the captured value when the kind is registerBool.
	BoolValue bool

	// Kind identifies which register bank this cell corresponds to.
	Kind isa.RegisterKind

	// IsIndirect signals that GeneralValue holds a heap-box pointer shared between the
	// declaring frame and every closure that captures the variable.
	IsIndirect bool

	// OriginalKind names the register bank the variable had before heap promotion.
	// Meaningful only when IsIndirect is true.
	OriginalKind isa.RegisterKind
}
