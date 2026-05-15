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

//go:build go1.27 && !safe

package engine

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestUnsafeNewAtParity(t *testing.T) {
	type nestedStruct struct {
		Name  string
		Count int
	}
	cases := []struct {
		ty   reflect.Type
		name string
	}{
		{name: "int64", ty: reflect.TypeFor[int64]()},
		{name: "int32", ty: reflect.TypeFor[int32]()},
		{name: "uint64", ty: reflect.TypeFor[uint64]()},
		{name: "float64", ty: reflect.TypeFor[float64]()},
		{name: "bool", ty: reflect.TypeFor[bool]()},
		{name: "string", ty: reflect.TypeFor[string]()},
		{name: "pointer-to-int", ty: reflect.TypeFor[*int]()},
		{name: "slice-of-byte", ty: reflect.TypeFor[[]byte]()},
		{name: "struct", ty: reflect.TypeFor[nestedStruct]()},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {

			backing := reflect.New(tc.ty).Elem()
			p := backing.Addr().UnsafePointer()

			want := reflect.NewAt(tc.ty, p).Elem()

			got := unsafeNewAt(reflectValueABIType(tc.ty), p, tc.ty.Kind())

			require.Equal(t, want.Type(), got.Type(), "type mismatch")
			require.Equal(t, want.Kind(), got.Kind(), "kind mismatch")
			require.Equal(t, want.CanAddr(), got.CanAddr(), "CanAddr mismatch")
			require.Equal(t, want.CanSet(), got.CanSet(), "CanSet mismatch")
			require.Equal(t, want.Addr().UnsafePointer(), got.Addr().UnsafePointer(), "Addr().UnsafePointer mismatch")

			switch tc.ty.Kind() {
			case reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8, reflect.Int:
				got.SetInt(0x4242_4242)
				require.Equal(t, int64(0x4242_4242), want.Int(), "SetInt via unsafe → safe read")
			case reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8, reflect.Uint:
				got.SetUint(0x4242)
				require.Equal(t, uint64(0x4242), want.Uint(), "SetUint via unsafe → safe read")
			case reflect.Float64, reflect.Float32:
				got.SetFloat(3.14)
				require.Equal(t, 3.14, want.Float(), "SetFloat via unsafe → safe read")
			case reflect.Bool:
				got.SetBool(true)
				require.True(t, want.Bool(), "SetBool via unsafe → safe read")
			case reflect.String:
				got.SetString("hello")
				require.Equal(t, "hello", want.String(), "SetString via unsafe → safe read")
			default:
			}
		})
	}
}

func TestUnsafeReadOnlyValue(t *testing.T) {
	p := unsafe.Pointer(new(int64(42)))
	intType := reflect.TypeFor[int64]()

	got := unsafeReadOnlyValue(reflectValueABIType(intType), p, intType.Kind())
	require.Equal(t, intType, got.Type(), "type mismatch")
	require.Equal(t, int64(42), got.Int(), "read mismatch")
	require.False(t, got.CanAddr(), "CanAddr should be false for read-only Value")
	require.False(t, got.CanSet(), "CanSet should be false for read-only Value")
}

func TestReflectValueABITypeExtraction(t *testing.T) {
	a := reflect.TypeFor[int64]()
	b := reflect.TypeFor[int64]()
	require.Equal(t, reflectValueABIType(a), reflectValueABIType(b), "ABI type pointers should match for the same Go type")
	require.NotNil(t, reflectValueABIType(a), "ABI type pointer should never be nil for a valid reflect.Type")
}
