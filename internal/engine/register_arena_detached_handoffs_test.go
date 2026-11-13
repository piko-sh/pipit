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

	"pipit.sh/pipit/internal/engine/program"
)

func TestRestoreRecordIsEmptiedForEachCall(t *testing.T) {
	vm := newTestVM(t)
	detach := vm.restoreRecord(false)
	detach.strings[0] = nativeStringRestore{original: "kept", clone: "copy", register: 1}
	detach.generals[0] = nativeGeneralRestore{original: reflect.ValueOf(1), clone: reflect.ValueOf(2), register: 2}
	detach.nStrings, detach.nGeneral = 1, 1

	require.Same(t, detach, vm.restoreRecord(false), "one scratch record per kind")
	require.Zero(t, detach.nStrings)
	require.Zero(t, detach.nGeneral)
	require.Empty(t, detach.strings[0].original, "the reset drops what the record held")
	require.False(t, detach.generals[0].original.IsValid())

	require.NotSame(t, detach, vm.restoreRecord(true), "native copies use the other record")
}

func TestDetachArgumentsReusedWithoutLicenceNeedsSuchAnArena(t *testing.T) {
	vm := newTestVM(t)
	registers := &Registers{General: []reflect.Value{reflect.ValueOf(func(string) {})}}
	site := &program.CallSite{IsNative: true}

	vm.Arena = nil
	require.Nil(t, detachArgumentsReusedWithoutLicence(vm, registers, site), "no arena")
	vm.Arena = &RegisterArena{licensed: true}
	require.Nil(t, detachArgumentsReusedWithoutLicence(vm, registers, site), "a licensed arena")
	vm.Arena = &RegisterArena{unpooled: true}
	require.Nil(t, detachArgumentsReusedWithoutLicence(vm, registers, site), "an arena that is never reused")
	require.Equal(t, arenaUsesUnsafeSlabs, reusedWithoutLicence(&RegisterArena{}), "a pooled arena without a licence, where slabs are reused")
}
