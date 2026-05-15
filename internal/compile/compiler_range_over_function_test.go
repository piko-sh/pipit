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

const iteratorFunctions = `
func seq0(yield func() bool) {
	for i := 0; i < 3; i++ {
		if !yield() {
			return
		}
	}
}

func seq1(yield func(int) bool) {
	for i := 0; i < 3; i++ {
		if !yield(i) {
			return
		}
	}
}

func seq2(yield func(int, string) bool) {
	for i := 0; i < 3; i++ {
		if !yield(i, "x") {
			return
		}
	}
}
`

func TestRangingOverAFunctionCallsItWithASynthesisedYieldClosure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "an iterator that yields no values",
			source: "func f() int { n := 0; for range seq0 { n++ }; return n }",
		},
		{
			name:   "an iterator that yields one value",
			source: "func f() int { n := 0; for v := range seq1 { n += v }; return n }",
		},
		{
			name:   "an iterator that yields two values",
			source: "func f() string { s := \"\"; for _, v := range seq2 { s += v }; return s }",
		},
		{
			name:   "an iterator whose second value is discarded",
			source: "func f() int { n := 0; for k := range seq2 { n += k }; return n }",
		},
		{
			name:   "a body that breaks out of the iteration",
			source: "func f() int { n := 0; for v := range seq1 { if v == 1 { break }; n += v }; return n }",
		},
		{
			name:   "a body that continues to the next yield",
			source: "func f() int { n := 0; for v := range seq1 { if v == 1 { continue }; n += v }; return n }",
		},
		{
			name:   "a body that reads a variable of the enclosing function",
			source: "func f(base int) int { n := 0; for v := range seq1 { n += v + base }; return n }",
		},
		{
			name:   "a body nested inside an ordinary loop",
			source: "func f() int { n := 0; for i := 0; i < 2; i++ { for v := range seq1 { n += v } }; return n }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+iteratorFunctions+"\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.True(t, bodyContainsOpcode(compiled, isa.OpMakeClosure),
				"the loop body becomes a yield callback, so a closure over it has to be built")
			require.True(t, bodyContainsTier1SubOp(compiled, isa.SubOpCallNative),
				"the iterator is called once with that callback, through the native call path")
			require.True(t, bodyContainsTier2SubOp(compiled, isa.SubOpTier2SyncClosureUpvalues),
				"the callback writes to the caller's registers through upvalues, which have to be synced back on return")
		})
	}
}

func TestReturningFromARangeOverFunctionBodyIsStashedAndReplayed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{
			name:   "a single return value",
			source: "func f() int { for v := range seq1 { if v == 1 { return v } }; return -1 }",
		},
		{
			name:   "two return values of the same bank",
			source: "func f() (int, int) { for v := range seq1 { if v == 1 { return v, v } }; return -1, -1 }",
		},
		{
			name:   "two return values of different banks",
			source: "func f() (int, string) { for k, v := range seq2 { if k == 1 { return k, v } }; return -1, \"\" }",
		},
		{
			name:   "a bare return from a function with no results",
			source: "func f(p *int) { for v := range seq1 { if v == 1 { return }; *p += v } }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+iteratorFunctions+"\n"+tt.source+"\n")

			compiled := findCompiledFunction(t, root, "f")

			require.True(t, bodyContainsTier2SubOp(compiled, isa.SubOpTier2SyncClosureUpvalues),
				"the state flag the callback set lives in an upvalue and has to come back before it is read")
			require.True(t, bodyContainsTier2SubOp(compiled, isa.SubOpTier2Return),
				"a return inside the body has to be replayed as a real return once the iterator has unwound")
		})
	}
}

func TestLabelledJumpsOutOfARangeOverFunctionBodyDispatchOnTheStateFlag(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		source  string
		control string
	}{
		{
			name:    "a labelled break of the enclosing loop",
			source:  "func f() int { n := 0; outer: for i := 0; i < 2; i++ { for v := range seq1 { if v == 1 { break outer }; n += v } }; return n }",
			control: "func f() int { n := 0; for i := 0; i < 2; i++ { for v := range seq1 { if v == 1 { break }; n += v } }; return n }",
		},
		{
			name:    "a labelled continue of the enclosing loop",
			source:  "func f() int { n := 0; outer: for i := 0; i < 2; i++ { for v := range seq1 { if v == 1 { continue outer }; n += v } }; return n }",
			control: "func f() int { n := 0; for i := 0; i < 2; i++ { for v := range seq1 { if v == 1 { continue }; n += v } }; return n }",
		},
		{
			name:    "a labelled break of an enclosing range loop",
			source:  "func f(xs []int) int { n := 0; outer: for _, x := range xs { for v := range seq1 { if v == x { break outer }; n += v } }; return n }",
			control: "func f(xs []int) int { n := 0; for _, x := range xs { for v := range seq1 { if v == x { break }; n += v } }; return n }",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			compiled := findCompiledFunction(t, compileSnippet(t, "package main\n"+iteratorFunctions+"\n"+tt.source+"\n"), "f")
			control := findCompiledFunction(t, compileSnippet(t, "package main\n"+iteratorFunctions+"\n"+tt.control+"\n"), "f")

			require.True(t, bodyContainsTier2SubOp(compiled, isa.SubOpTier2SyncClosureUpvalues),
				"the flag is only readable in the caller once the callback's upvalues have been synced")
			require.Greater(t,
				countTier1SubOp(compiled, isa.SubOpEqIntConstJumpFalse),
				countTier1SubOp(control, isa.SubOpEqIntConstJumpFalse),
				"a jump that leaves the callback for a labelled outer loop needs its own flag check in the caller, which the unlabelled form does not")
		})
	}
}
