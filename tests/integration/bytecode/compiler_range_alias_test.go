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

//go:build integration

package bytecode_test

import (
	"context"
	"strings"
	"testing"

	"pipit.sh/pipit/internal/app"

	"github.com/stretchr/testify/require"
)

const (
	rangeStructReadOnlyProgram = `package main
type body struct{ x, y float64 }
func sum(bodies []body) float64 {
	total := 0.0
	for _, b := range bodies {
		total += b.x*b.x + b.y
	}
	return total
}
func EntrypointRun() float64 {
	return sum([]body{{x: 1, y: 2}, {x: 3, y: 4}})
}`
)

const (
	rangeStructMutatingProgram = `package main
type body struct{ x, y float64 }
func sum(bodies []body) float64 {
	total := 0.0
	for _, b := range bodies {
		bodies[1].x = 100
		total += b.x
	}
	return total
}
func EntrypointRun() float64 {
	return sum([]body{{x: 1, y: 2}, {x: 3, y: 4}})
}`
)

func rangeValueMoveCount(dump string) int {
	count := 0
	for line := range strings.SplitSeq(dump, "\n") {
		if strings.Contains(line, "MOVE_GENERAL") {
			count++
		}
	}
	return count
}

func TestRangeStructValueNoMoveWhenReadOnly(t *testing.T) {
	t.Parallel()
	dump := compileSourceForOpcodeCheck(t, rangeStructReadOnlyProgram)
	require.Zerof(t, rangeValueMoveCount(dump), "read-only struct range value must alias the element; disasm:\n%s", dump)
	require.Positivef(t, strings.Count(dump, ":INDEX "), "expected the element index into the value register; disasm:\n%s", dump)

	result, err := app.NewService().Eval(context.Background(), `
type body struct{ x, y float64 }
bodies := []body{{x: 1, y: 2}, {x: 3, y: 4}}
total := 0.0
for _, b := range bodies {
	total += b.x*b.x + b.y
}
total`)
	require.NoError(t, err)
	require.InDelta(t, 16.0, result, 1e-9)
}

func TestRangeStructValueCopiesWhenBodyMutatesSlice(t *testing.T) {
	t.Parallel()
	dump := compileSourceForOpcodeCheck(t, rangeStructMutatingProgram)
	require.Positivef(t, rangeValueMoveCount(dump), "a body that writes the slice must keep the snapshot; disasm:\n%s", dump)

	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": rangeStructMutatingProgram})
	require.NoError(t, err)
	result, err := service.ExecuteEntrypoint(context.Background(), compiled, "EntrypointRun")
	require.NoError(t, err)
	require.InDelta(t, 101.0, result, 1e-9, "first iteration sees the original element, second sees the write")
}

func TestRangeStructValueSnapshotWhenValueAssignedOrAddressed(t *testing.T) {
	t.Parallel()
	for name, body := range map[string]string{
		"assigned":  "b.x = 5\n\t\ttotal += b.x",
		"addressed": "p := &b\n\t\ttotal += p.x",
		"incdec":    "b.x++\n\t\ttotal += b.x",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dump := compileSourceForOpcodeCheck(t, strings.Replace(rangeStructReadOnlyProgram, "total += b.x*b.x + b.y", body, 1))
			require.Positivef(t, rangeValueMoveCount(dump), "expected the snapshot to be kept; disasm:\n%s", dump)
		})
	}
}

func TestRangeStructValueAliasDecidedByCalleePurity(t *testing.T) {
	t.Parallel()
	pure := compileSourceForOpcodeCheck(t, `package main
type body struct{ x, y float64 }
func scale(v float64) float64 { return v * 2 }
func sum(bodies []body) float64 {
	total := 0.0
	for _, b := range bodies {
		total += scale(b.x)
	}
	return total
}
func EntrypointRun() float64 { return sum([]body{{x: 1, y: 2}}) }`)
	require.Zerof(t, rangeValueMoveCount(pure), "a heap-pure callee keeps the alias; disasm:\n%s", pure)

	impure := compileSourceForOpcodeCheck(t, `package main
type body struct{ x, y float64 }
var sink []body
func poke(v float64) float64 { sink[0].x = v; return v }
func sum(bodies []body) float64 {
	total := 0.0
	for _, b := range bodies {
		total += poke(b.x)
	}
	return total
}
func EntrypointRun() float64 { return sum([]body{{x: 1, y: 2}}) }`)
	require.Positivef(t, rangeValueMoveCount(impure), "a heap-mutating callee forces the snapshot; disasm:\n%s", impure)
}
