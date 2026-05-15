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

package symtab

import (
	"fmt"
	"reflect"
)

// maximumReconstructedTypeBytes is the byte ceiling for reconstructed array and struct
// layouts.
const maximumReconstructedTypeBytes = 64 << 20

// checkReconstructedArraySize rejects oversized layouts before reflect.ArrayOf.
//
// Runtime type construction can allocate metadata proportional to the layout size.
//
// Takes length (int) which is the array length.
// Takes element (reflect.Type) which is the element type.
//
// Returns error when the layout exceeds the byte ceiling.
func checkReconstructedArraySize(length int, element reflect.Type) error {
	if length < 0 {
		return fmt.Errorf("%w: negative array length", errCorruptTypeDescriptor)
	}
	size := element.Size()
	if size != 0 && uint64(length) > maximumReconstructedTypeBytes/uint64(size) {
		return fmt.Errorf("%w: array layout exceeds %d bytes", errCorruptTypeDescriptor, maximumReconstructedTypeBytes)
	}
	return nil
}

// checkReconstructedStructSize bounds aligned field layout before reflect.StructOf.
//
// The trailing zero-size-field rule and final alignment are included.
//
// Takes fields ([]reflect.StructField) which are the resolved struct fields.
//
// Returns error when the layout exceeds the byte ceiling.
func checkReconstructedStructSize(fields []reflect.StructField) error {
	var size uintptr
	alignment := uintptr(1)
	for _, field := range fields {
		fieldAlignment := uintptr(field.Type.Align())
		alignment = max(alignment, fieldAlignment)
		padding := (fieldAlignment - size%fieldAlignment) % fieldAlignment
		if err := addReconstructedLayout(&size, padding); err != nil {
			return err
		}
		if err := addReconstructedLayout(&size, field.Type.Size()); err != nil {
			return err
		}
	}
	if len(fields) != 0 && size != 0 && fields[len(fields)-1].Type.Size() == 0 {
		if err := addReconstructedLayout(&size, 1); err != nil {
			return err
		}
	}
	return addReconstructedLayout(&size, (alignment-size%alignment)%alignment)
}

// addReconstructedLayout adds one size contribution without overflow.
//
// Takes size (*uintptr) which is the accumulated size.
// Takes extra (uintptr) which is the field or padding contribution.
//
// Returns error when the total exceeds the byte ceiling.
func addReconstructedLayout(size *uintptr, extra uintptr) error {
	if *size > maximumReconstructedTypeBytes || extra > maximumReconstructedTypeBytes-*size {
		return fmt.Errorf("%w: struct layout exceeds %d bytes", errCorruptTypeDescriptor, maximumReconstructedTypeBytes)
	}
	*size += extra
	return nil
}
