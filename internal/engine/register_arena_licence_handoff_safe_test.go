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

	"pipit.sh/pipit/internal/isa"
)

func TestUnboxedHandOffNotesRevokeTheEpochWithInexactProbes(t *testing.T) {
	registers := NewRegisters([isa.NumRegisterKinds]uint32{isa.RegisterSliceString: 1})
	registers.slicesString[0] = []string{"go heap"}
	for cause := range HandOffCause(numHandOffCauses) {
		for _, boxed := range []bool{false, true} {
			for _, unboxed := range []bool{false, true} {
				for _, slice := range []bool{false, true} {
					arena := NewRegisterArena()
					arena.attachLicencePolicy(&VMLimits{})
					switch {
					case slice && unboxed:
						arena.recordHandOffSliceRegister(cause, &registers, isa.RegisterSliceString, 0, boxed)
					case slice:
						arena.recordHandOff(cause, registerToReflectValue(nil, &registers, isa.RegisterSliceString, 0), boxed)
					case unboxed:
						arena.recordHandOffString(cause, "go heap", boxed)
					default:
						arena.recordHandOff(cause, BoxStringToGeneral(nil, "go heap"), boxed)
					}
					state := &arena.licenceState
					require.Equal(t, licenceRevokes(cause), state.epochRevoked, "cause %s boxed %v unboxed %v slice %v", cause, boxed, unboxed, slice)
					if state.epochRevoked {
						require.Equal(t, epochInexactProbe, state.epochReason)
						require.True(t, state.banksRevoked)
					}
				}
			}
		}
	}
}

func TestLicenceNoteKeyNeverMatchesASecondNote(t *testing.T) {
	v := reflect.ValueOf(&struct{ S string }{S: "kept"})
	require.NotEqual(t, licenceNoteKeyOf(v, false), licenceNoteKeyOf(v, false),
		"without the reflect.Value's words a second note of the same value is recorded again")
}

func TestInterfaceSharesArenaStorageCountsEveryUnaddressableValue(t *testing.T) {
	arena := &RegisterArena{}
	require.True(t, arena.interfaceSharesArenaStorage(reflect.ValueOf([2]int{1, 2})),
		"where a value that is not addressable is stored cannot be read, so it counts as sharing")
	require.False(t, arena.interfaceSharesArenaStorage(reflect.ValueOf(new(int))), "a pointer is copied into the interface")
	addressable := reflect.ValueOf(&[2]int{}).Elem()
	require.False(t, arena.interfaceSharesArenaStorage(addressable), "reflect copies an addressable value into the interface")
	require.False(t, arena.interfaceSharesArenaStorage(reflect.Value{}))
}
