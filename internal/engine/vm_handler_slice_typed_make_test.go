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
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestTypedMakeHandlersCheckLengthAndCapacity(t *testing.T) {
	t.Parallel()

	handlers := []struct {
		name    string
		subOp   isa.SubOpcode
		handler opcodeHandler
		measure func(*Registers) (int, int)
	}{
		{name: "int", subOp: isa.SubOpMakeSliceInt, handler: handleSubOpMakeSliceInt,
			measure: func(r *Registers) (int, int) { return len(r.SlicesInt[0]), cap(r.SlicesInt[0]) }},
		{name: "float", subOp: isa.SubOpMakeSliceFloat, handler: handleSubOpMakeSliceFloat,
			measure: func(r *Registers) (int, int) { return len(r.slicesFloat[0]), cap(r.slicesFloat[0]) }},
		{name: "string", subOp: isa.SubOpMakeSliceString, handler: handleSubOpMakeSliceString,
			measure: func(r *Registers) (int, int) { return len(r.slicesString[0]), cap(r.slicesString[0]) }},
		{name: "bool", subOp: isa.SubOpMakeSliceBool, handler: handleSubOpMakeSliceBool,
			measure: func(r *Registers) (int, int) { return len(r.slicesBool[0]), cap(r.slicesBool[0]) }},
		{name: "uint", subOp: isa.SubOpMakeSliceUint, handler: handleSubOpMakeSliceUint,
			measure: func(r *Registers) (int, int) { return len(r.slicesUint[0]), cap(r.slicesUint[0]) }},
		{name: "byte", subOp: isa.SubOpMakeSliceByte, handler: handleSubOpMakeSliceByte,
			measure: func(r *Registers) (int, int) { return len(r.slicesByte[0]), cap(r.slicesByte[0]) }},
	}
	cases := []struct {
		name      string
		length    int64
		capacity  int64
		wantPanic string
	}{
		{name: "a length within the capacity", length: 2, capacity: 5, wantPanic: ""},
		{name: "a length past the capacity", length: 2, capacity: 1, wantPanic: "makeslice: cap out of range"},
		{name: "a negative length", length: -1, capacity: 3, wantPanic: "makeslice: len out of range"},
		{name: "a negative capacity", length: 0, capacity: -1, wantPanic: "makeslice: cap out of range"},
	}

	for _, h := range handlers {
		for _, c := range cases {
			t.Run(h.name+"/"+c.name, func(t *testing.T) {
				t.Parallel()

				vm, frame, registers := newExtWordFrame(t, isa.NewInstruction(isa.OpExt, 2, 0, 0))
				registers.Ints[1] = c.length
				registers.Ints[2] = c.capacity

				result := h.handler(vm, frame, registers, isa.NewTier1Instruction(h.subOp, 0, 1))

				if c.wantPanic == "" {
					require.Equal(t, opContinue, result)
					length, capacity := h.measure(registers)
					require.Equal(t, int(c.length), length)
					require.Equal(t, int(c.capacity), capacity)
					return
				}
				require.NotEqual(t, opContinue, result)
				require.Contains(t, fmt.Sprint(vm.panicValue), c.wantPanic)
			})
		}
	}
}
