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
	"reflect"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func arenaString(arena *RegisterArena, text string) string {
	bytes := arena.AllocStringBytes(len(text))
	copy(bytes, text)
	return unsafe.String(unsafe.SliceData(bytes), len(bytes))
}

func requireClone(t *testing.T, want, got string, message string) {
	t.Helper()
	require.Equal(t, want, got, message)
	require.NotSame(t, unsafe.StringData(want), unsafe.StringData(got), message)
}

func requireSameBytes(t *testing.T, want, got string, message string) {
	t.Helper()
	require.Same(t, unsafe.StringData(want), unsafe.StringData(got), message)
}

func TestForeignStringBarriersCloneOnlyForAnArenaThatClonesUnownedStrings(t *testing.T) {
	t.Parallel()
	parent := GetRegisterArena()
	defer PutRegisterArena(parent)
	callback := GetRegisterArena()
	defer PutRegisterArena(callback)
	foreign := arenaString(parent, "foreign-key")
	own := arenaString(callback, "own-key")

	fieldBase := callback.AllocBytes(16, 8)
	backing := callback.AllocStringBacking(2)

	requireSameBytes(t, foreign, materialiseString(callback, foreign), "without the flag a foreign string is stored raw")

	callback.cloneUnownedStrings = true
	requireClone(t, foreign, materialiseString(callback, foreign), "the plain barrier clones a foreign string")
	requireClone(t, foreign, materialiseStringUnconditional(callback, foreign), "the boundary barrier clones a foreign string")
	requireClone(t, own, materialiseString(callback, own), "the arena's own string is still cloned")
	require.Empty(t, materialiseString(callback, ""), "the empty string needs no clone")

	requireClone(t, foreign, MaterialiseStringForFieldStore(callback, fieldBase, foreign),
		"a foreign string is cloned even into a struct the arena owns")
	requireSameBytes(t, own, MaterialiseStringForFieldStore(callback, fieldBase, own),
		"an own string stored into an own struct is left for the escape walk")
	requireClone(t, foreign, MaterialiseStringForTypedSliceStore(callback, backing, foreign),
		"a foreign string is cloned even into a backing the arena owns")
	requireSameBytes(t, own, MaterialiseStringForTypedSliceStore(callback, backing, own),
		"an own string stored into an own backing is left for the escape walk")

	stored := MaterialiseArenaValue(callback, reflect.ValueOf(foreign))
	requireClone(t, foreign, stored.String(), "a foreign string stored directly is cloned")
	boxed := MaterialiseArenaValue(callback, reflect.ValueOf(&[]any{foreign}).Elem().Index(0))
	requireClone(t, foreign, boxed.Elem().String(), "a foreign string stored as an interface is cloned")

	shared := map[string]string{"key": foreign}
	materialiseArenaValueUnconditional(callback, reflect.ValueOf(shared))
	requireSameBytes(t, foreign, shared["key"], "the escape walk leaves a map's foreign strings in place, so it never writes a shared map")

	cloned := materialiseString(callback, foreign)
	parent.Reset()
	arenaString(parent, "overwritten")
	require.Equal(t, "foreign-key", cloned, "the clone survives the parent arena's reuse")
}

type foreignStringPair struct {
	Name  string
	Boxed any
}

func TestEscapeCopiesCloneForeignStrings(t *testing.T) {
	t.Parallel()

	for _, clones := range []bool{false, true} {
		parent := GetRegisterArena()
		callback := GetRegisterArena()
		callback.cloneUnownedStrings = clones
		foreign := arenaString(parent, "foreign-element")
		backing := callback.AllocStringBacking(1)
		backing[0] = foreign

		copiedSlice := MaterialiseArenaSliceUnconditional(callback, reflect.ValueOf(backing))
		copiedPair := materialiseArenaCompositeFields(callback, reflect.ValueOf(foreignStringPair{Name: foreign, Boxed: foreign}), false)

		element := copiedSlice.Index(0).String()
		pair, ok := reflect.TypeAssert[foreignStringPair](copiedPair)
		require.True(t, ok)
		if clones {
			boxed, ok := pair.Boxed.(string)
			require.True(t, ok)
			requireClone(t, foreign, element, "a slice copied out of a callback arena clones a foreign element")
			requireClone(t, foreign, pair.Name, "a struct copied out of a callback arena clones a foreign field")
			requireClone(t, foreign, boxed, "and a foreign string inside an interface field")
		} else {
			requireSameBytes(t, foreign, element, "an arena that does not clone unowned strings leaves foreign ones alone")
			requireSameBytes(t, foreign, pair.Name, "an arena that does not clone unowned strings leaves foreign ones alone")
		}
		PutRegisterArena(callback)
		PutRegisterArena(parent)
	}
}

func TestArenaResetStopsCloningUnownedStrings(t *testing.T) {
	t.Parallel()
	arena := GetRegisterArena()
	defer PutRegisterArena(arena)
	arena.cloneUnownedStrings = true
	arena.Reset()
	require.False(t, arena.cloneUnownedStrings)
}

func TestReentrantVMArenasCloneUnownedStrings(t *testing.T) {
	t.Parallel()
	for _, reentrant := range []bool{false, true} {
		vm := NewVM(t.Context(), NewGlobalStore(), nil)
		vm.reentrantInterpreterVM = reentrant
		arena := vm.acquireArena()
		require.Equal(t, reentrant, arena.cloneUnownedStrings, "reentrant=%v", reentrant)
		PutRegisterArena(arena)
	}
}
