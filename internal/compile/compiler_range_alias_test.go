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

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const rangeAliasTypes = `
type element struct {
	X int
	Y int
}

func (e element) Sum() int { return e.X + e.Y }

func consume(int) {}
`

func rangeValueMoveMode(t *testing.T, compiledFunction *program.CompiledFunction) (uint8, bool) {
	t.Helper()
	for _, instruction := range compiledFunction.Body {
		if instruction.Op == isa.OpMoveGeneral {
			return instruction.C, true
		}
	}
	return 0, false
}

func TestARangeValueOnlyReadIsMarkedAsAnAliasCandidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "a field read once", source: "func f(xs []element) int { n := 0; for _, v := range xs { n += v.X }; return n }"},
		{name: "two fields read", source: "func f(xs []element) int { n := 0; for _, v := range xs { n += v.X + v.Y }; return n }"},
		{name: "a field read inside a condition", source: "func f(xs []element) int { n := 0; for _, v := range xs { if v.X > 0 { n += v.Y } }; return n }"},
		{name: "the value passed on by field", source: "func f(xs []element) { for _, v := range xs { consume(v.X) } }"},
		{name: "a local written from the value", source: "func f(xs []element) int { n := 0; for _, v := range xs { w := v.X; n += w }; return n }"},
		{name: "an unrelated name assigned in the body", source: "func f(xs []element, ys []int) int { n := 0; for i, v := range xs { ys[i] = v.X; n++ }; return n }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+rangeAliasTypes+"\n"+tt.source+"\n")

			mode, found := rangeValueMoveMode(t, findCompiledFunction(t, root, "f"))

			require.True(t, found, "a struct range value is stored through a general move")
			require.Equal(t, isa.MoveGeneralModeSnapshotRangeCandidate, mode,
				"a body that only reads the value cannot observe the difference, so the copy can be elided later")
		})
	}
}

func TestARangeValueTheBodyCouldObserveKeepsItsSnapshot(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		source string
	}{
		{name: "a field written through the value", source: "func f(xs []element) { for _, v := range xs { v.X = 1 } }"},
		{name: "a field stepped through the value", source: "func f(xs []element) { for _, v := range xs { v.X++ } }"},
		{name: "the value reassigned outright", source: "func f(xs []element) int { n := 0; for _, v := range xs { v = element{}; n += v.X }; return n }"},
		{name: "the address of the value taken", source: "func f(xs []element) { for _, v := range xs { p := &v; _ = p } }"},
		{name: "the ranged slice reassigned in the body", source: "func f(xs []element) int { n := 0; for _, v := range xs { n += v.X; xs = nil }; return n }"},
		{name: "a closure in the body", source: "func f(xs []element) int { n := 0; for _, v := range xs { g := func() int { return v.X }; n += g() }; return n }"},
		{name: "a goroutine started from the body", source: "func f(xs []element) { for _, v := range xs { go consume(v.X) } }"},
		{name: "a call deferred from the body", source: "func f(xs []element) { for _, v := range xs { defer consume(v.X) } }"},
		{name: "a method value bound to the value", source: "func f(xs []element) int { n := 0; for _, v := range xs { h := v.Sum; n += h() }; return n }"},
		{name: "a value declared outside the loop", source: "func f(xs []element) int { n := 0; var v element; for _, v = range xs { n += v.X }; return n }"},
		{name: "a collection that is not a plain name", source: "func g() []element { return nil }\nfunc f() int { n := 0; for _, v := range g() { n += v.X }; return n }"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := compileSnippet(t, "package main\n"+rangeAliasTypes+"\n"+tt.source+"\n")

			mode, found := rangeValueMoveMode(t, findCompiledFunction(t, root, "f"))

			require.True(t, found, "a struct range value is stored through a general move")
			require.Equal(t, engine.MoveGeneralModeSnapshot, mode,
				"the body can reach the element, so each iteration has to get its own copy")
		})
	}
}
