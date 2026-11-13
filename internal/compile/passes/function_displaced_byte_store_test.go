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

package passes

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	displacedSlice = 2
	displacedIndex = 7
	displacedTemp  = 9
	displacedValue = 3
)

func displacedStoreBody(between []isa.Instruction, extension isa.Instruction, after ...isa.Instruction) []isa.Instruction {
	body := []isa.Instruction{mk(isa.OpAddInt64Constant, displacedTemp, displacedIndex, 1)}
	body = append(body, between...)
	body = append(body, isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, displacedSlice, displacedTemp), extension)
	body = append(body, after...)
	return append(body, returnVoid())
}

func TestFuseDisplacedByteStoreFoldsTheAddIntoTheStore(t *testing.T) {
	t.Parallel()
	nop := mk(isa.OpNoOperation, 0, 0, 0)
	cases := []struct {
		name         string
		constant     int64
		between      []isa.Instruction
		displacement uint8
		want         uint8
	}{
		{name: "adjacent", constant: 3, want: 3},
		{name: "across no-operation words", constant: 1, between: []isa.Instruction{nop, nop}, want: 1},
		{name: "the largest displacement", constant: 255, want: 255},
		{name: "zero", constant: 0, want: 0},
		{name: "added to a displacement already there", constant: 200, displacement: 55, want: 255},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			body := displacedStoreBody(testCase.between, mk(isa.OpExtension, displacedValue, testCase.displacement, 0))
			compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{-4, testCase.constant}}
			store := 1 + len(testCase.between)

			require.True(t, fuseDisplacedByteStore(compiledFunction, body, 0, program.JumpTargetsOf(body)))

			require.True(t, isCanonicalNoOperation(body[0]), "the add is gone")
			require.Equal(t, isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, displacedSlice, displacedIndex), body[store],
				"the store indexes by the add's source register")
			require.Equal(t, mk(isa.OpExtension, displacedValue, testCase.want, 0), body[store+1])
		})
	}
}

func TestFuseDisplacedByteStoreRefusals(t *testing.T) {
	t.Parallel()
	plainExtension := mk(isa.OpExtension, displacedValue, 0, 0)
	readTemp := mk(isa.OpAddInt64, 5, displacedTemp, 1)
	cases := []struct {
		name        string
		constant    int64
		body        []isa.Instruction
		jumpTargets map[int]bool
	}{
		{name: "a negative constant", constant: -1, body: displacedStoreBody(nil, plainExtension)},
		{name: "a constant past the byte", constant: 256, body: displacedStoreBody(nil, plainExtension)},
		{name: "a sum past the byte", constant: 200, body: displacedStoreBody(nil, mk(isa.OpExtension, displacedValue, 56, 0))},
		{name: "the sum is read after the store", constant: 1, body: displacedStoreBody(nil, plainExtension, readTemp)},
		{name: "another instruction lies between", constant: 1, body: displacedStoreBody([]isa.Instruction{mk(isa.OpAddInt64, 5, 1, 1)}, plainExtension)},
		{name: "the store is a jump target", constant: 1, body: displacedStoreBody(nil, plainExtension), jumpTargets: map[int]bool{1: true}},
		{name: "a word between is a jump target", constant: 1, body: displacedStoreBody([]isa.Instruction{mk(isa.OpNoOperation, 0, 0, 0)}, plainExtension), jumpTargets: map[int]bool{1: true}},
		{
			name: "the store indexes by another register", constant: 1,
			body: []isa.Instruction{
				mk(isa.OpAddInt64Constant, displacedTemp, displacedIndex, 1),
				isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, displacedSlice, displacedIndex), plainExtension, returnVoid(),
			},
		},
		{
			name: "a store of another kind", constant: 1,
			body: []isa.Instruction{
				mk(isa.OpAddInt64Constant, displacedTemp, displacedIndex, 1),
				isa.NewTier1Instruction(isa.SubOpSetSliceUint64Element, displacedSlice, displacedTemp), plainExtension, returnVoid(),
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			compiledFunction := &program.CompiledFunction{Body: testCase.body, IntConstants: []int64{-4, testCase.constant}}
			jumpTargets := testCase.jumpTargets
			if jumpTargets == nil {
				jumpTargets = program.JumpTargetsOf(testCase.body)
			}
			original := append([]isa.Instruction(nil), testCase.body...)

			require.False(t, fuseDisplacedByteStore(compiledFunction, testCase.body, 0, jumpTargets))
			require.Equal(t, original, testCase.body, "a refused fusion leaves the body untouched")
		})
	}
}

func TestFuseDisplacedByteStoreKeepsAnIndexUpdatedInPlaceOnlyWhenItIsDead(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpAddInt64Constant, displacedIndex, displacedIndex, 1),
		isa.NewTier1Instruction(isa.SubOpSetSliceByteElement, displacedSlice, displacedIndex),
		mk(isa.OpExtension, displacedValue, 0, 0),
		mk(isa.OpLoadInt64Constant, displacedIndex, 0, 0),
		returnVoid(),
	}
	compiledFunction := &program.CompiledFunction{Body: body, IntConstants: []int64{0, 2}}

	require.True(t, fuseDisplacedByteStore(compiledFunction, body, 0, program.JumpTargetsOf(body)),
		"an index bumped in place and then overwritten is the same store at a displacement")
	require.Equal(t, mk(isa.OpExtension, displacedValue, 2, 0), body[2])
}
