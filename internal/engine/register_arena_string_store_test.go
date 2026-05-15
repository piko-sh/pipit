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

//go:build safe

package engine

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestMaterialiseStringSafeBuildPassThrough(t *testing.T) {
	t.Parallel()
	arena := NewRegisterArena()
	s := strings.Repeat("safe", 4)
	require.Equal(t, unsafe.StringData(s), unsafe.StringData(materialiseString(arena, s)))
	require.Equal(t, unsafe.StringData(s), unsafe.StringData(MaterialiseStringForTypedSliceStore(arena, make([]string, 1), s)))
	require.Equal(t, unsafe.StringData(s), unsafe.StringData(MaterialiseStringForGoAppend(arena, make([]string, 1), s)))
	require.Equal(t, unsafe.StringData(s), unsafe.StringData(MaterialiseStringForFieldStore(arena, unsafe.Pointer(new([1]string)), s)))
	boxed := reflect.ValueOf(s)
	require.Equal(t, boxed, MaterialiseArenaValue(arena, boxed))
}
