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
	"pipit.sh/pipit/internal/isa"
)

func TestMoveGeneralDebugSnapshot(t *testing.T) {
	t.Parallel()
	for _, attached := range []bool{false, true} {
		t.Run(map[bool]string{false: "no debugger", true: "idle debugger"}[attached], func(t *testing.T) {
			t.Parallel()
			vm := newTestVM(t)
			if attached {
				vm.Limits.Debug = NewDebugSession()
			}
			source := struct {
				Value int
				Data  []byte
			}{3, []byte{4}}
			registers := &Registers{}
			registers.General = make([]reflect.Value, 2)
			registers.General[0] = reflect.ValueOf(&source).Elem()
			require.Equal(t, opContinue, handleMoveGeneral(vm, nil, registers, isa.NewInstruction(isa.OpMoveGeneral, 1, 0, MoveGeneralModeDebugSnapshot)))
			source.Value = 7
			source.Data[0] = 9
			expected := int64(7)
			if attached {
				expected = 3
			}
			require.Equal(t, expected, registers.General[1].Field(0).Int())
			require.Equal(t, byte(9), registers.General[1].Field(1).Bytes()[0])
		})
	}
}
