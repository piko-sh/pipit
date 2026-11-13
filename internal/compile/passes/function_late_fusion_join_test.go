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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	joinedUnsigned = 3
	joinedSigned   = 7
	joinedView     = 2
	joinedIndex    = 10
)

func joinedConversionBody(t *testing.T, missLine, hitLine []isa.Instruction, tail ...isa.Instruction) (body []isa.Instruction, conversion int) {
	t.Helper()
	body = make([]isa.Instruction, 0, len(missLine)+len(hitLine)+len(tail)+4)
	body = append(body, mk(isa.OpJumpIfTrue, joinFlag, 0, 0))
	body = append(body, missLine...)
	jump := len(body)
	body = append(body, tier1Jump(0))
	hit := len(body)
	body = append(body, hitLine...)
	conversion = len(body)
	body = append(body, isa.NewTier1Instruction(isa.SubOpConvertUint64ToInt64, joinedSigned, joinedUnsigned))
	body = append(body, tail...)
	body = append(body, returnVoid())
	require.True(t, program.SetJumpTarget(body, 0, hit))
	require.True(t, program.SetJumpTarget(body, jump, conversion))
	return body, conversion
}

func TestFuseJoinedReadConversionGivesEveryDefinitionItsIntForm(t *testing.T) {
	t.Parallel()
	literal := isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, joinedUnsigned, 200)
	byteRead := []isa.Instruction{
		isa.NewTier1Instruction(isa.SubOpGetSliceByteElement, joinedUnsigned, joinedView), mk(isa.OpExtension, joinedIndex, 0, 0),
	}
	elementRead := mk(isa.OpGetElementUint64, joinedUnsigned, 4, 5)

	tests := []struct {
		name     string
		miss     []isa.Instruction
		hit      []isa.Instruction
		wantMiss []isa.Instruction
		wantHit  []isa.Instruction
	}{
		{
			name: "a literal and a byte read",
			miss: []isa.Instruction{literal}, hit: byteRead,
			wantMiss: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, joinedSigned, 200)},
			wantHit: []isa.Instruction{
				isa.NewTier1Instruction(isa.SubOpGetSliceByteElementInt64, joinedSigned, joinedView), mk(isa.OpExtension, joinedIndex, 0, 0),
			},
		},
		{
			name: "a byte read and an element read, with padding before the join",
			miss: byteRead, hit: []isa.Instruction{elementRead, nop()},
			wantMiss: []isa.Instruction{
				isa.NewTier1Instruction(isa.SubOpGetSliceByteElementInt64, joinedSigned, joinedView), mk(isa.OpExtension, joinedIndex, 0, 0),
			},
			wantHit: []isa.Instruction{mk(isa.OpGetElementInt64, joinedSigned, 4, 5), nop()},
		},
		{
			name: "two literals",
			miss: []isa.Instruction{literal}, hit: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, joinedUnsigned, 0)},
			wantMiss: []isa.Instruction{isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, joinedSigned, 200)},
			wantHit:  []isa.Instruction{isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, joinedSigned, 0)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, conversion := joinedConversionBody(t, tt.miss, tt.hit, mk(isa.OpAddInt64, 8, joinedSigned, joinedSigned))
			compiledFunction := joinMoveFunction(body)

			require.True(t, fuseJoinedReadConversion(compiledFunction, body, conversion, jumpTargetsFor(nil, body), &joinEntries{}))

			require.Equal(t, tt.wantMiss, body[1:1+len(tt.wantMiss)])
			hit := 2 + len(tt.miss)
			require.Equal(t, tt.wantHit, body[hit:hit+len(tt.wantHit)])
			require.Equal(t, nop(), body[conversion])
		})
	}
}

func TestFuseJoinedReadConversionKeepsAConversionItCannotAccountFor(t *testing.T) {
	t.Parallel()
	literal := isa.NewTier1Instruction(isa.SubOpLoadUint64Immediate, joinedUnsigned, 9)
	elementRead := mk(isa.OpGetElementUint64, joinedUnsigned, 4, 5)
	readsUnsigned := mk(isa.OpAddUint64, 8, joinedUnsigned, joinedUnsigned)

	tests := []struct {
		name  string
		build func(t *testing.T) (body []isa.Instruction, conversion int)
	}{
		{
			name: "one way in ends with another kind of definition",
			build: func(t *testing.T) ([]isa.Instruction, int) {
				t.Helper()
				return joinedConversionBody(t, []isa.Instruction{literal}, []isa.Instruction{mk(isa.OpAddUint64, joinedUnsigned, 5, 6)})
			},
		},
		{
			name: "one way in ends with a definition of another register",
			build: func(t *testing.T) ([]isa.Instruction, int) {
				t.Helper()
				return joinedConversionBody(t, []isa.Instruction{literal}, []isa.Instruction{mk(isa.OpGetElementUint64, joinedUnsigned+1, 4, 5)})
			},
		},
		{
			name: "something follows the definition on one way in",
			build: func(t *testing.T) ([]isa.Instruction, int) {
				t.Helper()
				return joinedConversionBody(t, []isa.Instruction{literal}, []isa.Instruction{elementRead, readsUnsigned})
			},
		},
		{
			name: "the uint register is read after the conversion",
			build: func(t *testing.T) ([]isa.Instruction, int) {
				t.Helper()
				return joinedConversionBody(t, []isa.Instruction{literal}, []isa.Instruction{elementRead}, readsUnsigned)
			},
		},
		{
			name: "a conditional jump lands on the conversion",
			build: func(t *testing.T) ([]isa.Instruction, int) {
				t.Helper()
				body, conversion := joinedConversionBody(t, []isa.Instruction{literal}, []isa.Instruction{elementRead})
				body = append(body, literal, mk(isa.OpJumpIfTrue, joinFlag, 0, 0), returnVoid())
				require.True(t, program.SetJumpTarget(body, len(body)-2, conversion))
				return body, conversion
			},
		},
		{
			name: "a conditional jump lands on the jump that closes a line",
			build: func(t *testing.T) ([]isa.Instruction, int) {
				t.Helper()
				body, conversion := joinedConversionBody(t, []isa.Instruction{literal}, []isa.Instruction{elementRead})
				body = append(body, mk(isa.OpJumpIfTrue, joinFlag, 0, 0), returnVoid())
				require.True(t, program.SetJumpTarget(body, len(body)-2, joinMoveClosingJump))
				return body, conversion
			},
		},
		{
			name: "a jump lands on padding after a definition",
			build: func(t *testing.T) ([]isa.Instruction, int) {
				t.Helper()
				body, conversion := joinedConversionBody(t, []isa.Instruction{literal}, []isa.Instruction{elementRead, nop()})
				body = append(body, tier1Jump(0))
				require.True(t, program.SetJumpTarget(body, len(body)-1, conversion-1))
				return body, conversion
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body, conversion := tt.build(t)
			original := slices.Clone(body)
			compiledFunction := joinMoveFunction(body)

			require.False(t, fuseJoinedReadConversion(compiledFunction, body, conversion, jumpTargetsFor(nil, body), &joinEntries{}))
			require.Equal(t, original, body)
		})
	}
}

func TestFuseJoinedReadConversionLeavesAConversionNoJumpLandsOn(t *testing.T) {
	t.Parallel()
	body := []isa.Instruction{
		mk(isa.OpGetElementUint64, joinedUnsigned, 4, 5),
		isa.NewTier1Instruction(isa.SubOpConvertUint64ToInt64, joinedSigned, joinedUnsigned),
		returnVoid(),
	}
	original := slices.Clone(body)

	require.False(t, fuseJoinedReadConversion(joinMoveFunction(body), body, 1, jumpTargetsFor(nil, body), &joinEntries{}),
		"a conversion one line reaches is the straight-line fusion's to take")
	require.Equal(t, original, body)
}
