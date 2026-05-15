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
	"errors"
	"reflect"
	"testing"

	"pipit.sh/pipit/internal/symtab/descriptor"
)

func TestReconstructedArraySizeBound(t *testing.T) {
	t.Parallel()
	element := &descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Uint8)}
	for _, length := range []int{maximumReconstructedTypeBytes, maximumReconstructedTypeBytes + 1} {
		value := descriptor.TypeDescriptor{Kind: descriptor.KindArray, Length: length, Element: element}
		resolved, err := ReflectTypeFor(value, nil)
		if length > maximumReconstructedTypeBytes {
			if resolved != nil || !errors.Is(err, errCorruptTypeDescriptor) {
				t.Fatal("oversized array reached reflection:", err)
			}
		} else if err != nil || resolved.Size() != uintptr(length) {
			t.Fatal("boundary array rejected:", err)
		}
	}
	if err := checkReconstructedArraySize(int(^uint(0)>>1), reflect.TypeFor[uint64]()); !errors.Is(err, errCorruptTypeDescriptor) {
		t.Fatal("array multiplication could overflow:", err)
	}
	if err := checkReconstructedArraySize(int(^uint(0)>>1), reflect.TypeFor[struct{}]()); err != nil {
		t.Fatal("zero-size array incorrectly charged:", err)
	}
}

func TestNestedReconstructedArraySizeBound(t *testing.T) {
	t.Parallel()
	element := &descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Uint64)}
	inner := &descriptor.TypeDescriptor{Kind: descriptor.KindArray, Length: 8192, Element: element}
	outer := descriptor.TypeDescriptor{Kind: descriptor.KindArray, Length: 8192, Element: inner}
	resolved, err := ReflectTypeFor(outer, nil)
	if resolved != nil || !errors.Is(err, errCorruptTypeDescriptor) {
		t.Fatal("nested layout multiplication bypassed limit:", err)
	}
}

func TestReconstructedStructSizeBound(t *testing.T) {
	t.Parallel()
	half := descriptor.TypeDescriptor{
		Kind: descriptor.KindArray, Length: maximumReconstructedTypeBytes / 2,
		Element: &descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Uint8)},
	}
	value := descriptor.TypeDescriptor{Kind: descriptor.KindStruct, Fields: []descriptor.TypeDescriptorField{
		{Name: "First", Typ: half},
		{Name: "Second", Typ: half},
	}}
	resolved, err := ReflectTypeFor(value, nil)
	if err != nil || resolved.Size() != maximumReconstructedTypeBytes {
		t.Fatal("boundary struct rejected:", err)
	}
	value.Fields = append(value.Fields, descriptor.TypeDescriptorField{
		Name: "Extra", Typ: descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Uint8)},
	})
	resolved, err = ReflectTypeFor(value, nil)
	if resolved != nil || !errors.Is(err, errCorruptTypeDescriptor) {
		t.Fatal("oversized struct reached reflection:", err)
	}
}

func TestReconstructedStructPaddingBound(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"field-alignment", "trailing-zero", "final-alignment"} {
		t.Run(mode, func(t *testing.T) {
			var fields []reflect.StructField
			switch mode {
			case "field-alignment":
				fields = []reflect.StructField{
					{Name: "Large", Type: reflect.ArrayOf(maximumReconstructedTypeBytes-1, reflect.TypeFor[byte]())},
					{Name: "Aligned", Type: reflect.TypeFor[uint16]()},
				}
			case "trailing-zero":
				fields = []reflect.StructField{
					{Name: "Large", Type: reflect.ArrayOf(maximumReconstructedTypeBytes, reflect.TypeFor[byte]())},
					{Name: "Empty", Type: reflect.TypeFor[struct{}]()},
				}
			case "final-alignment":
				fields = []reflect.StructField{
					{Name: "Aligned", Type: reflect.TypeFor[uint64]()},
					{Name: "Large", Type: reflect.ArrayOf(maximumReconstructedTypeBytes-7, reflect.TypeFor[byte]())},
				}
			}
			if err := checkReconstructedStructSize(fields); !errors.Is(err, errCorruptTypeDescriptor) {
				t.Fatal("padding bypassed layout ceiling:", err)
			}
		})
	}
}

func TestReconstructedLayoutAdditionDoesNotOverflow(t *testing.T) {
	t.Parallel()
	size := uintptr(maximumReconstructedTypeBytes)
	if err := addReconstructedLayout(&size, ^uintptr(0)); !errors.Is(err, errCorruptTypeDescriptor) || size != maximumReconstructedTypeBytes {
		t.Fatal("layout addition wrapped or changed rejected size:", size, err)
	}
}
