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

//go:build integration && go1.27

package language_test

import (
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestReflectValueLayout(t *testing.T) {
	t.Parallel()
	const (
		wantSize   = 24
		wantTypOff = 0
		wantPtrOff = unsafe.Sizeof(uintptr(0))
	)
	require.Equal(t, uintptr(wantSize), unsafe.Sizeof(reflect.Value{}),
		"the reflect.Value size matches the reflect_value_unsafe.go assumptions")

	const sentinel = int64(0x0123_4567_89AB_CDEF)
	heapValue := sentinel
	v := reflect.ValueOf(&heapValue).Elem()

	var view struct {
		typ  unsafe.Pointer
		ptr  unsafe.Pointer
		flag uintptr
	} = *(*struct {
		typ  unsafe.Pointer
		ptr  unsafe.Pointer
		flag uintptr
	})(unsafe.Pointer(&v))

	require.NotNil(t, view.typ, "word %d of reflect.Value is the *abi.Type", wantTypOff)
	require.NotNil(t, view.ptr, "the word at offset %d of reflect.Value is the data pointer", wantPtrOff)

	const flagKindMask = uintptr(0x1f)
	const flagAddr = uintptr(1 << 8)
	const flagIndir = uintptr(1 << 7)
	gotKind := reflect.Kind(view.flag & flagKindMask)
	require.Equal(t, reflect.Int64, gotKind, "the flag's low bits hold the Kind, as the reflect_value_unsafe.go constants assume")
	require.NotZero(t, view.flag&flagAddr, "an .Elem() Value has the flagAddr bit (flag %#x)", view.flag)
	require.NotZero(t, view.flag&flagIndir, "an .Elem() Value has the flagIndir bit (flag %#x)", view.flag)

	v.SetInt(0x5555_5555_5555_5555)
	require.Equal(t, int64(0x5555_5555_5555_5555), heapValue,
		"Value.SetInt updates the backing storage through the pointer at offset %d", wantPtrOff)
}
