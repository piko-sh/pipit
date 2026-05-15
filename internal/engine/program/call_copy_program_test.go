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

package program

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func TestBuildCallArgCopyProgramEmitsOneEntryPerArgument(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		arguments  []VarLocation
		paramKinds []isa.RegisterKind
		paramRegs  []uint8
		wantLen    int
	}{
		{
			name:       "one same-bank argument",
			arguments:  []VarLocation{{Kind: isa.RegisterInt, Register: 3}},
			paramKinds: []isa.RegisterKind{isa.RegisterInt}, paramRegs: []uint8{0},
			wantLen: 1,
		},
		{
			name: "several arguments across banks",
			arguments: []VarLocation{
				{Kind: isa.RegisterInt, Register: 1},
				{Kind: isa.RegisterString, Register: 2},
				{Kind: isa.RegisterGeneral, Register: 3},
			},
			paramKinds: []isa.RegisterKind{isa.RegisterInt, isa.RegisterString, isa.RegisterGeneral},
			paramRegs:  []uint8{0, 0, 0},
			wantLen:    3,
		},
		{
			name: "more arguments than parameters stops at the shorter list",
			arguments: []VarLocation{
				{Kind: isa.RegisterInt, Register: 1},
				{Kind: isa.RegisterInt, Register: 2},
			},
			paramKinds: []isa.RegisterKind{isa.RegisterInt}, paramRegs: []uint8{0},
			wantLen: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			copyProgram := BuildCallArgCopyProgram(tt.arguments, tt.paramKinds, tt.paramRegs)

			require.Len(t, copyProgram, tt.wantLen)
		})
	}
}

func TestBuildCallArgCopyProgramDeclinesMalformedInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		arguments  []VarLocation
		paramKinds []isa.RegisterKind
		paramRegs  []uint8
	}{
		{name: "no arguments", arguments: nil, paramKinds: []isa.RegisterKind{isa.RegisterInt}, paramRegs: []uint8{0}},
		{name: "no parameters", arguments: []VarLocation{{Kind: isa.RegisterInt}}, paramKinds: nil, paramRegs: nil},
		{
			name:       "a register list that does not match the kind list",
			arguments:  []VarLocation{{Kind: isa.RegisterInt}},
			paramKinds: []isa.RegisterKind{isa.RegisterInt, isa.RegisterInt}, paramRegs: []uint8{0},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Nil(t, BuildCallArgCopyProgram(tt.arguments, tt.paramKinds, tt.paramRegs),
				"a malformed call shape must produce no copy program rather than a partial one")
		})
	}
}

func TestBuildCallArgCopyProgramRecordsSourceAndDestination(t *testing.T) {
	t.Parallel()

	copyProgram := BuildCallArgCopyProgram(
		[]VarLocation{{Kind: isa.RegisterInt, Register: 5}},
		[]isa.RegisterKind{isa.RegisterInt},
		[]uint8{2},
	)

	require.Len(t, copyProgram, 1)
	require.Equal(t, uint8(5), copyProgram[0].SourceRegister)
	require.Equal(t, uint8(2), copyProgram[0].DestinationRegister)
}

func TestSameKindCopyOpCoversEveryRegisterBank(t *testing.T) {
	t.Parallel()

	banks := []struct {
		name string
		kind isa.RegisterKind
	}{
		{name: "the integer bank", kind: isa.RegisterInt},
		{name: "the float bank", kind: isa.RegisterFloat},
		{name: "the string bank", kind: isa.RegisterString},
		{name: "the general bank", kind: isa.RegisterGeneral},
		{name: "the boolean bank", kind: isa.RegisterBool},
		{name: "the unsigned bank", kind: isa.RegisterUint},
		{name: "the complex bank", kind: isa.RegisterComplex},
		{name: "the integer slice bank", kind: isa.RegisterSliceInt},
		{name: "the float slice bank", kind: isa.RegisterSliceFloat},
		{name: "the string slice bank", kind: isa.RegisterSliceString},
		{name: "the boolean slice bank", kind: isa.RegisterSliceBool},
		{name: "the unsigned slice bank", kind: isa.RegisterSliceUint},
		{name: "the byte slice bank", kind: isa.RegisterSliceByte},
	}

	seen := make(map[callArgCopyOp]string, len(banks))
	for _, tt := range banks {
		copyOp, ok := sameKindCopyOp(tt.kind)

		require.Truef(t, ok, "%s must have a same-kind copy operation", tt.name)
		require.Emptyf(t, seen[copyOp], "%s reuses the copy operation of %s", tt.name, seen[copyOp])
		seen[copyOp] = tt.name
	}
	require.Len(t, seen, len(banks), "every bank needs a distinct copy operation")

	t.Run("an upvalue pointer has no same-kind copy", func(t *testing.T) {
		t.Parallel()
		_, ok := sameKindCopyOp(UpvalueKindAsPointer)

		require.False(t, ok, "an upvalue is copied through the boxing path, not a bank move")
	})
}

func TestDetectTailCallArgsAliasSpotsAnOverwrittenSource(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		arguments  []VarLocation
		paramKinds []isa.RegisterKind
		want       bool
	}{
		{
			name: "arguments already in their destination order do not alias",
			arguments: []VarLocation{
				{Kind: isa.RegisterInt, Register: 0},
				{Kind: isa.RegisterInt, Register: 1},
			},
			paramKinds: []isa.RegisterKind{isa.RegisterInt, isa.RegisterInt},
			want:       false,
		},
		{
			name: "a later argument reading an earlier destination aliases",
			arguments: []VarLocation{
				{Kind: isa.RegisterInt, Register: 5},
				{Kind: isa.RegisterInt, Register: 0},
			},
			paramKinds: []isa.RegisterKind{isa.RegisterInt, isa.RegisterInt},
			want:       true,
		},
		{
			name: "a clash in a different bank does not alias",
			arguments: []VarLocation{
				{Kind: isa.RegisterInt, Register: 5},
				{Kind: isa.RegisterString, Register: 0},
			},
			paramKinds: []isa.RegisterKind{isa.RegisterInt, isa.RegisterString},
			want:       false,
		},
		{
			name:       "a single argument cannot alias",
			arguments:  []VarLocation{{Kind: isa.RegisterInt, Register: 0}},
			paramKinds: []isa.RegisterKind{isa.RegisterInt},
			want:       false,
		},
		{
			name:       "no arguments cannot alias",
			arguments:  nil,
			paramKinds: nil,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.want, DetectTailCallArgsAlias(tt.arguments, tt.paramKinds))
		})
	}
}
