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

package compile

import (
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

const incDecTypes = `
type stepped struct {
	N  int
	U  uint64
	F  float64
	Narrow int32
}

type steppedWrapper struct {
	stepped
	Tag string
}

var steppedGlobal int
var steppedGlobalFloat float64
`

func TestSteppingALocalUsesTheSingleWordOpOfItsBank(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   isa.SubOpcodeTier2
	}{
		{name: "an integer incremented", source: "func f(n int) int { n++; return n }", want: isa.SubOpTier2IncInt},
		{name: "an integer decremented", source: "func f(n int) int { n--; return n }", want: isa.SubOpTier2DecInt},
		{name: "an unsigned value incremented", source: "func f(n uint64) uint64 { n++; return n }", want: isa.SubOpTier2IncUint},
		{name: "an unsigned value decremented", source: "func f(n uint64) uint64 { n--; return n }", want: isa.SubOpTier2DecUint},
		{name: "a value behind a pointer incremented", source: "func f(p *int) { *p++ }", want: isa.SubOpTier2IncInt},
		{name: "a value behind a pointer decremented", source: "func f(p *int) { *p-- }", want: isa.SubOpTier2DecInt},
		{name: "a package-level integer incremented", source: "func f() { steppedGlobal++ }", want: isa.SubOpTier2IncInt},
		{name: "a package-level integer decremented", source: "func f() { steppedGlobal-- }", want: isa.SubOpTier2DecInt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+incDecTypes+"\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.Truef(t, bodyContainsTier2SubOp(compiled, tt.want),
				"a step of one needs no constant and no binary operator, so %v must carry it", tt.want)
		})
	}
}

func TestSteppingAFloatGoesThroughAdditionOfOne(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   isa.Opcode
	}{
		{name: "a local incremented", source: "func f(x float64) float64 { x++; return x }", want: isa.OpAddFloat},
		{name: "a local decremented", source: "func f(x float64) float64 { x--; return x }", want: isa.OpSubFloat},
		{name: "a value behind a pointer incremented", source: "func f(p *float64) { *p++ }", want: isa.OpAddFloat},
		{name: "a package-level float decremented", source: "func f() { steppedGlobalFloat-- }", want: isa.OpSubFloat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+incDecTypes+"\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.Truef(t, bodyContainsOpcode(compiled, tt.want),
				"the float bank has no single-word step, so the constant one must be loaded and %s applied", tt.want)
			require.True(t, bodyContainsOpcode(compiled, isa.OpLoadFloatConst),
				"the one added or subtracted has to come from the float constant pool")
		})
	}
}

func TestSteppingACapturedVariableWritesBackThroughItsCell(t *testing.T) {
	t.Parallel()

	root := compileSnippet(t, "package main\n"+incDecTypes+"\nfunc f() func() { n := 0; return func() { n++ } }\n")

	closure := findCompiledFunction(t, root, "<closure>")

	require.True(t, bodyContainsTier2SubOp(closure, isa.SubOpTier2IncInt),
		"the captured integer still steps with the single-word op once it is in a register")
	require.True(t, bodyContainsOpcode(closure, isa.OpGetUpvalue),
		"the current value has to be read out of the shared cell first")
	require.True(t, bodyContainsOpcode(closure, isa.OpSetUpvalue),
		"the stepped value has to be written back, or the closure would step a copy")
}

func TestSteppingAStructFieldFusesIntoASingleWord(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   isa.SubOpcode
	}{
		{name: "an integer field incremented", source: "func f(s *stepped) { s.N++ }", want: isa.SubOpIncStructFieldInt},
		{name: "an integer field decremented", source: "func f(s *stepped) { s.N-- }", want: isa.SubOpDecStructFieldInt},
		{name: "an unsigned field incremented", source: "func f(s *stepped) { s.U++ }", want: isa.SubOpIncStructFieldUint},
		{name: "an unsigned field decremented", source: "func f(s *stepped) { s.U-- }", want: isa.SubOpDecStructFieldUint},
		{name: "a promoted integer field incremented", source: "func f(w *steppedWrapper) { w.N++ }", want: isa.SubOpIncStructFieldInt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+incDecTypes+"\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.Truef(t, bodyContainsTier1SubOp(compiled, tt.want),
				"a full-width scalar field at a tier-0 layout steps in place, so %v must replace the read-step-write trio", tt.want)
			require.False(t, bodyContainsOpcode(compiled, isa.OpGetFieldInt),
				"the fused step reads nothing into a register, so no field read may be emitted alongside it")
		})
	}
}

func TestSteppingAFieldTheFusedWordCannotTakeFallsBackToReadAndWrite(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		wantOps []isa.Opcode
	}{
		{
			name:    "a narrow integer field has to be truncated after the step",
			source:  "func f(s *stepped) { s.Narrow++ }",
			wantOps: []isa.Opcode{isa.OpGetFieldInt, isa.OpSetFieldInt},
		},
		{
			name:    "a floating-point field is not a step the fused word handles",
			source:  "func f(s *stepped) { s.F++ }",
			wantOps: []isa.Opcode{isa.OpGetField, isa.OpUnpackInterface, isa.OpAddFloat, isa.OpPackInterface, isa.OpSetField},
		},
		{
			name:    "a promoted floating-point field is reached through its embedded pointer",
			source:  "func f(w *steppedWrapper) { w.F++ }",
			wantOps: []isa.Opcode{isa.OpDeref, isa.OpUnpackInterface, isa.OpAddFloat, isa.OpPackInterface, isa.OpSetField},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+incDecTypes+"\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			for _, op := range tt.wantOps {
				require.Truef(t, bodyContainsOpcode(compiled, op),
					"the fallback reads the field, steps it and writes it back, so %s belongs in the body", op)
			}
			require.False(t, bodyContainsTier1SubOp(compiled, isa.SubOpIncStructFieldInt),
				"the fused word must not be emitted for a field it cannot step correctly")
		})
	}
}

func TestSteppingAnElementReusesTheCompoundAssignmentPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
		want   isa.Opcode
	}{
		{name: "a slice element incremented", source: "func f(s []int, i int) { s[i]++ }", want: isa.OpAddInt},
		{name: "a slice element decremented", source: "func f(s []int, i int) { s[i]-- }", want: isa.OpSubInt},
		{name: "a floating-point slice element incremented", source: "func f(s []float64, i int) { s[i]++ }", want: isa.OpAddFloat},
		{name: "an unsigned slice element incremented", source: "func f(s []uint64, i int) { s[i]++ }", want: isa.OpAddUint},
		{name: "an array element incremented", source: "func f(a *[4]int, i int) { a[i]++ }", want: isa.OpAddInt},
		{name: "a map entry incremented", source: "func f(m map[int]int, k int) { m[k]++ }", want: isa.OpMapAddIntInt},
		{name: "a map entry decremented", source: "func f(m map[int]int, k int) { m[k]-- }", want: isa.OpSubInt},
		{name: "an entry of a string-keyed map incremented", source: "func f(m map[string]int, k string) { m[k]++ }", want: isa.OpMapAddStringInt},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+incDecTypes+"\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.Truef(t, bodyContainsOpcode(compiled, tt.want),
				"stepping an element is a compound assignment of one, so %s must carry it", tt.want)
		})
	}
}
