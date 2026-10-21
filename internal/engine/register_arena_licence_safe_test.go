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

//go:build safe || (js && wasm)

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func safeArena(t *testing.T) *RegisterArena {
	t.Helper()
	arena := NewRegisterArena()
	arena.attachLicencePolicy(&VMLimits{})
	require.True(t, arena.licensed)
	return arena
}

func TestSafeBuildReadsValuesAsTheyAre(t *testing.T) {
	value := reflect.ValueOf("text")
	require.Equal(t, value, licenceReadable(value))
	copied := reflect.ValueOf("copy")
	require.Equal(t, copied, licenceKeepReadOnly(copied, value))
	slab := make([]int64, 4)
	require.Equal(t, reflect.ValueOf(&slab[0]).Pointer(), licenceSlabAddress(slab))
}

func TestSafeBuildReadsAHeldReflectValue(t *testing.T) {
	inner := reflect.ValueOf(7)
	require.Equal(t, inner.Interface(), reflectValueHeld(reflect.ValueOf(inner)).Interface(), "an exported reflect.Value")

	holder := struct{ held reflect.Value }{held: inner}
	addressable := reflect.ValueOf(&holder).Elem().Field(0)
	require.Equal(t, inner.Interface(), reflectValueHeld(addressable).Interface(), "an unexported field read through its address")
	require.False(t, reflectValueHeld(reflect.ValueOf(holder).Field(0)).IsValid(), "an unexported field with no address cannot be read")
}
