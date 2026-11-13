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
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/symtab/descriptor"
)

func TestReconstructedArraySizeBound(t *testing.T) {
	t.Parallel()
	err := checkReconstructedArraySize(int(^uint(0)>>1), reflect.TypeFor[uint64]())
	require.ErrorIs(t, err, errCorruptTypeDescriptor, "array size multiplication is checked for overflow")
	require.NoError(t, checkReconstructedArraySize(int(^uint(0)>>1), reflect.TypeFor[struct{}]()), "a zero-size array costs nothing")
	element := &descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Uint8)}
	tests := []struct {
		name    string
		length  int
		wantErr error
	}{
		{name: "at the size limit", length: maximumReconstructedTypeBytes, wantErr: nil},
		{name: "one byte over the size limit", length: maximumReconstructedTypeBytes + 1, wantErr: errCorruptTypeDescriptor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			value := descriptor.TypeDescriptor{Kind: descriptor.KindArray, Length: tt.length, Element: element}
			resolved, err := ReflectTypeFor(t.Context(), value, nil)
			require.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr != nil {
				require.Nil(t, resolved, "an oversized array is rejected before reflection")
				return
			}
			require.Equal(t, uintptr(tt.length), resolved.Size())
		})
	}
}

func TestNestedReconstructedArraySizeBound(t *testing.T) {
	t.Parallel()
	element := &descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Uint64)}
	inner := &descriptor.TypeDescriptor{Kind: descriptor.KindArray, Length: 8192, Element: element}
	outer := descriptor.TypeDescriptor{Kind: descriptor.KindArray, Length: 8192, Element: inner}
	resolved, err := ReflectTypeFor(t.Context(), outer, nil)
	require.ErrorIs(t, err, errCorruptTypeDescriptor, "nested array sizes multiply towards the limit")
	require.Nil(t, resolved)
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
	resolved, err := ReflectTypeFor(t.Context(), value, nil)
	require.NoError(t, err, "a struct at the size limit is accepted")
	require.Equal(t, uintptr(maximumReconstructedTypeBytes), resolved.Size())
	value.Fields = append(value.Fields, descriptor.TypeDescriptorField{
		Name: "Extra", Typ: descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Uint8)},
	})
	resolved, err = ReflectTypeFor(t.Context(), value, nil)
	require.ErrorIs(t, err, errCorruptTypeDescriptor, "an oversized struct is rejected before reflection")
	require.Nil(t, resolved)
}

func TestReconstructedStructPaddingBound(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		fields []reflect.StructField
	}{
		{name: "field-alignment", fields: []reflect.StructField{
			{Name: "Large", Type: reflect.ArrayOf(maximumReconstructedTypeBytes-1, reflect.TypeFor[byte]())},
			{Name: "Aligned", Type: reflect.TypeFor[uint16]()},
		}},
		{name: "trailing-zero", fields: []reflect.StructField{
			{Name: "Large", Type: reflect.ArrayOf(maximumReconstructedTypeBytes, reflect.TypeFor[byte]())},
			{Name: "Empty", Type: reflect.TypeFor[struct{}]()},
		}},
		{name: "final-alignment", fields: []reflect.StructField{
			{Name: "Aligned", Type: reflect.TypeFor[uint64]()},
			{Name: "Large", Type: reflect.ArrayOf(maximumReconstructedTypeBytes-7, reflect.TypeFor[byte]())},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := checkReconstructedStructSize(tt.fields)
			require.ErrorIs(t, err, errCorruptTypeDescriptor, "padding counts towards the layout ceiling")
		})
	}
}

func TestReconstructedLayoutAdditionDoesNotOverflow(t *testing.T) {
	t.Parallel()
	size := uintptr(maximumReconstructedTypeBytes)
	err := addReconstructedLayout(&size, ^uintptr(0))
	require.ErrorIs(t, err, errCorruptTypeDescriptor, "layout addition is checked for overflow")
	require.Equal(t, uintptr(maximumReconstructedTypeBytes), size, "a rejected addition leaves the size unchanged")
}
