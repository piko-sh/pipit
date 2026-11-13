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

//go:build go1.27 && !safe && !(js && wasm)

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReflectFreeStringStoreMakesTheReflectRoutesBarrierDecision(t *testing.T) {
	type store struct {
		stored   string
		owned    bool
		noteSeq  uint64
		revision uint64
	}
	run := func(routed bool, arenaBacking bool, array bool) store {
		t.Helper()
		arena := newArena(t)
		defer arena.Reset()
		var collection reflect.Value
		switch {
		case array:
			collection = reflect.ValueOf(&[2]string{}).Elem()
		case arenaBacking:
			backing := arena.AllocStringBacking(2)
			collection = reflect.ValueOf(&backing).Elem()
		default:
			collection = reflect.ValueOf(&[]string{"", ""}).Elem()
		}
		text := arenaString(arena, "stored into a typed collection")
		if routed {
			require.True(t, setElementStringWithoutReflect(arena, collection, 1, text))
		} else {
			collection.Index(1).SetString(materialiseStringForIndexedStore(arena, collection, text))
		}
		stored := collection.Index(1).String()
		return store{stored: stored, owned: arena.OwnsString(stored), noteSeq: arena.licenceState.noteSeq, revision: arena.licenceState.revision}
	}
	for _, shape := range []struct {
		name                string
		arenaBacking, array bool
	}{{"heap backing", false, false}, {"arena backing", true, false}, {"array", false, true}} {
		require.Equal(t, run(false, shape.arenaBacking, shape.array), run(true, shape.arenaBacking, shape.array), shape.name)
	}
}
