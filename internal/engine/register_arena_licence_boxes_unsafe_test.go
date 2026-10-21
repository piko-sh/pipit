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

//go:build !safe && !(js && wasm)

package engine

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestBorrowersShareOnlyTrueBoxes(t *testing.T) {
	arena := newArena(t)
	require.Equal(t, BorrowBorrows, arena.nativeBorrowClass(reflect.ValueOf(strings.Contains)))
	box := BoxStringToGeneral(arena, arenaString(arena, "boxed"))
	require.True(t, box.CanAddr())
	require.True(t, arena.trueBox(box, isa.RegisterString))
	shared := readOnlyBox(box)
	require.False(t, shared.CanAddr())
	asInterface := shared.Convert(reflect.TypeFor[any]())
	require.Equal(t, ReflectValuePtr(box), (*[2]unsafe.Pointer)(unsafe.Pointer(&[]any{asInterface.Interface()}[0]))[1],
		"the interface's data word is the arena box itself")
	cell := arena.AllocBytes(16, 8)
	general := reflect.NewAt(reflect.TypeFor[string](), cell).Elem()
	require.False(t, arena.trueBox(general, isa.RegisterString), "storage outside the box slabs is never shared")
	require.False(t, arena.trueBox(box, isa.RegisterGeneral), "a General register's value is never shared")
	arena.Reset()
}

func TestBoxShareCallSharesAnArenaBoxWithABorrowingCallee(t *testing.T) {
	arena := newArena(t)
	box := reflect.ValueOf(arena.allocStringBox("boxed")).Elem()
	require.True(t, boxIndirect(box))
	call := newBoxShareCall(arena, reflect.ValueOf(fmt.Sprint))

	shared, ok := call.share(&program.CallSite{}, 0, isa.RegisterString, box)
	require.True(t, ok)
	require.False(t, shared.CanAddr(), "the callee gets a read-only box")
	require.Equal(t, "boxed", shared.String())
	require.True(t, call.decided)
	call.note(shared)

	_, ok = call.share(&program.CallSite{}, 0, isa.RegisterGeneral, box)
	require.False(t, ok, "a general register's value is never shared")
	_, ok = call.share(&program.CallSite{}, 0, isa.RegisterString, reflect.ValueOf(new(string)).Elem())
	require.False(t, ok, "a heap value is not a box")
}

func TestBoxShareCallNeedsAnInterfaceParameter(t *testing.T) {
	arena := newArena(t)
	box := reflect.ValueOf(arena.allocStringBox("boxed")).Elem()
	call := newBoxShareCall(arena, reflect.ValueOf(strings.Contains))
	_, ok := call.share(&program.CallSite{}, 0, isa.RegisterString, box)
	require.False(t, ok, "a string parameter is passed the value, not the box")
}

func TestBoxShareCallNeedsABorrowingCallee(t *testing.T) {
	arena := newArena(t)
	box := reflect.ValueOf(arena.allocStringBox("boxed")).Elem()
	call := newBoxShareCall(arena, reflect.ValueOf(func(value any) {}))
	_, ok := call.share(&program.CallSite{}, 0, isa.RegisterString, box)
	require.False(t, ok, "a callee that may keep the value is not shared the box")
	require.False(t, call.shares)
}
