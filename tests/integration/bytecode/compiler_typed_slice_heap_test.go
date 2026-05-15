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

func TestTypedSliceLocalStoredIntoContainerUsesHeapMake(t *testing.T) {
	t.Parallel()
	dump := compileSourceForOpcodeCheck(t, `package main
func buildWeights(n int) [][]float64 {
	weights := make([][]float64, n)
	for i := 0; i < n; i++ {
		row := make([]float64, n)
		for j := 0; j < n; j++ {
			row[j] = float64(i*j) * 0.5
		}
		weights[i] = row
	}
	return weights
}
func EntrypointRun() int { return len(buildWeights(4)) }`)

	require.Positivef(t, strings.Count(dump, "MAKE_SLICE_HEAP"),
		"expected the row stored into weights[i] to be heap-placed; disasm:\n%s", dump)
	require.Zerof(t, strings.Count(dump, "MAKE_SLICE_FLOAT"),
		"expected no arena make for the escaping row; disasm:\n%s", dump)
}

func TestTypedSliceLocalKeptInFrameUsesArenaMake(t *testing.T) {
	t.Parallel()
	dump := compileSourceForOpcodeCheck(t, `package main
func rowSum(n int) float64 {
	row := make([]float64, n)
	for j := 0; j < n; j++ {
		row[j] = float64(j)
	}
	sum := 0.0
	for j := 0; j < len(row); j++ {
		sum += row[j]
	}
	return sum
}
func EntrypointRun() float64 { return rowSum(8) }`)

	require.Positivef(t, strings.Count(dump, "MAKE_SLICE_FLOAT"),
		"expected a frame-local row to stay in the arena; disasm:\n%s", dump)
	require.Zerof(t, strings.Count(dump, "MAKE_SLICE_HEAP"),
		"expected no heap make for a frame-local row; disasm:\n%s", dump)
}

func TestTypedSliceLocalCapturedOrSentUsesHeapMake(t *testing.T) {
	t.Parallel()
	dump := compileSourceForOpcodeCheck(t, `package main
func captured(n int) func() int {
	buffer := make([]int64, n)
	buffer[0] = 7
	return func() int { return int(buffer[0]) }
}
func sent(n int, ch chan []uint64) {
	buffer := make([]uint64, n)
	buffer[0] = 9
	ch <- buffer
}
func EntrypointRun() int {
	ch := make(chan []uint64, 1)
	sent(2, ch)
	return captured(2)() + int((<-ch)[0])
}`)

	require.Equalf(t, 2, strings.Count(dump, "MAKE_SLICE_HEAP"),
		"expected both the captured and the sent buffers to be heap-placed; disasm:\n%s", dump)
	require.Zerof(t, strings.Count(dump, "MAKE_SLICE_INT")+strings.Count(dump, "MAKE_SLICE_UINT"),
		"expected no arena make for escaping buffers; disasm:\n%s", dump)
}

func TestHeapMakeSliceBanksRoundTripThroughContainers(t *testing.T) {
	t.Parallel()
	result, err := app.NewService().Eval(context.Background(), `
ints := make([][]int64, 2)
floats := make([][]float64, 2)
strs := make([][]string, 2)
bools := make([][]bool, 2)
uints := make([][]uint64, 2)
bytes := make([][]byte, 2)
for i := 0; i < 2; i++ {
	ri := make([]int64, 3)
	rf := make([]float64, 3, 4)
	rs := make([]string, 3)
	rb := make([]bool, 3)
	ru := make([]uint64, 3)
	ry := make([]byte, 3)
	for j := 0; j < 3; j++ {
		ri[j] = int64(i*10 + j)
		rf[j] = float64(i) + 0.5
		rs[j] = "s" + string(rune('0'+j))
		rb[j] = j == i
		ru[j] = uint64(j + 1)
		ry[j] = byte('a' + j)
	}
	ints[i] = ri
	floats[i] = rf
	strs[i] = rs
	bools[i] = rb
	uints[i] = ru
	bytes[i] = ry
}
s := ""
for i := 0; i < 4096; i++ { s = s + "0123456789abcdef" }
total := 0
for i := 0; i < 2; i++ {
	for j := 0; j < 3; j++ {
		total += int(ints[i][j]) + int(floats[i][j]*2) + len(strs[i][j]) + int(uints[i][j]) + int(bytes[i][j]-'a')
		if bools[i][j] { total += 100 }
	}
}
total + cap(floats[1]) + len(s)/4096`)
	require.NoError(t, err)
	require.EqualValues(t, 36+12+12+12+6+200+4+16, result)
}
