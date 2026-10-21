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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestImmediateLoadsWriteTheOperandIntoTheIntBank(t *testing.T) {
	t.Parallel()

	handlers := map[string]func(*VM, *CallFrame, *Registers, isa.Instruction) OpResult{
		"int":         handleLoadInt64Immediate,
		"bool as int": handleLoadBoolImmediateAsInt64,
	}
	for name, handler := range handlers {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, value := range []uint8{0, 1} {
				registers := &Registers{Ints: make([]int64, 3)}
				registers.Ints[2] = 99
				require.Equal(t, opContinue, handler(nil, nil, registers, op(0, 2, value)))
				require.Equal(t, int64(value), registers.Ints[2])
			}
		})
	}
}
