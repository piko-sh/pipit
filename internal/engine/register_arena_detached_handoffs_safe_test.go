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

func TestSafeBuildDetachesNothing(t *testing.T) {
	require.Nil(t, detachNativeStringArguments(nil, &Registers{}, nil, reflect.Value{}, nil))
	arguments := []reflect.Value{reflect.ValueOf("kept")}
	detachNativeMethodStringArguments(nil, reflect.ValueOf(func(string) {}), arguments)
	require.Equal(t, "kept", arguments[0].String())
}

func TestUndoRestoresRegistersStillHoldingTheirClones(t *testing.T) {
	restore := &nativeArgumentRestore{nStrings: 2, nGeneral: 2}
	restore.strings[0] = nativeStringRestore{original: "original", clone: "clone", register: 0}
	restore.strings[1] = nativeStringRestore{original: "original", clone: "clone", register: 1}
	clone := reflect.ValueOf(new(int))
	restore.generals[0] = nativeGeneralRestore{original: reflect.ValueOf(1), clone: clone, register: 0}
	restore.generals[1] = nativeGeneralRestore{original: reflect.ValueOf(1), clone: clone, register: 1}
	registers := &Registers{Strings: []string{"clone", "result"}, General: []reflect.Value{clone, reflect.ValueOf("result")}}
	restore.undo(registers)
	require.Equal(t, []string{"original", "result"}, registers.Strings)
	require.Equal(t, 1, registers.General[0].Interface())
	require.Equal(t, "result", registers.General[1].Interface(), "a register the call rewrote is left alone")
	require.Zero(t, restore.nStrings)
}
