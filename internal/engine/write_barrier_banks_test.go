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

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/asm"
)

func typeHoldsPointers(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Pointer, reflect.UnsafePointer, reflect.Map, reflect.Chan, reflect.Func,
		reflect.Interface, reflect.String, reflect.Slice:
		return true
	case reflect.Array:
		return t.Len() > 0 && typeHoldsPointers(t.Elem())
	case reflect.Struct:
		for field := range t.Fields() {
			if typeHoldsPointers(field.Type) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func TestPointerBankBasesMatchDispatchContext(t *testing.T) {
	t.Parallel()
	bankBases := map[string]string{
		"Ints":         "CTX_INTS_BASE",
		"Floats":       "CTX_FLOATS_BASE",
		"Strings":      "CTX_STRINGS_BASE",
		"General":      "CTX_GENERALS_BASE",
		"Bools":        "CTX_BOOLS_BASE",
		"Uints":        "CTX_UINTS_BASE",
		"Complex":      "CTX_COMPLEX_BASE",
		"SlicesInt":    "CTX_SLICES_INT_BASE",
		"slicesFloat":  "CTX_SLICES_FLOAT_BASE",
		"slicesString": "CTX_SLICES_STRING_BASE",
		"slicesBool":   "CTX_SLICES_BOOL_BASE",
		"slicesUint":   "CTX_SLICES_UINT_BASE",
		"slicesByte":   "CTX_SLICES_BYTE_BASE",
	}

	banks := 0
	for field := range reflect.TypeFor[Registers]().Fields() {
		if field.Type.Kind() != reflect.Slice {
			continue
		}
		banks++
		define, mapped := bankBases[field.Name]
		require.True(t, mapped, "register bank %s has no dispatch-context base in this test", field.Name)
		holdsPointers, classified := asm.RegisterBankBases[define]
		require.True(t, classified, "%s is missing from asm.RegisterBankBases", define)
		require.Equal(t, typeHoldsPointers(field.Type.Elem()), holdsPointers,
			"%s: asm.RegisterBankBases must say whether bank %s holds pointers", define, field.Name)
	}
	require.Len(t, asm.RegisterBankBases, banks, "every classified base names a register bank")
}
