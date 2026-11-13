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
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/sdk/stdlib"
)

const fieldReadRunSource = `package main

import (
	"fmt"
	"image/color"
)

type narrow struct {
	a uint8
	b uint16
	c uint32
	d uint8
	e uint8
}

var palette [4]color.RGBA

var cells []narrow

func fill() {
	for i := range palette {
		palette[i] = color.RGBA{R: uint8(10 + i), G: uint8(100 + i), B: uint8(200 + i), A: 255}
	}
	cells = []narrow{{a: 1, b: 300, c: 70000, d: 4, e: 5}, {a: 6, b: 7, c: 8, d: 9, e: 10}}
}

func blit(source []byte) []byte {
	out := make([]byte, 4*len(source))
	for i := 0; i < len(source); i++ {
		col := palette[source[i]]
		position := i * 4
		out[position] = col.R
		out[position+1] = col.G
		out[position+2] = col.B
		out[position+3] = 0xff
	}
	return out
}

func sums() uint64 {
	var total uint64
	for i := 0; i < len(cells); i++ {
		cell := cells[i]
		total += uint64(cell.a) + uint64(cell.b) + uint64(cell.c) + uint64(cell.d) + uint64(cell.e)
	}
	return total
}

func past() (message string) {
	defer func() { message = fmt.Sprint(recover()) }()
	col := palette[len(cells)+7]
	return fmt.Sprint(col.R, col.G, col.B)
}

func Run() string {
	fill()
	return fmt.Sprint(blit([]byte{0, 3, 1}), " ", sums(), " ", past())
}
`

const fieldReadRunWant = "[10 100 200 255 13 103 203 255 11 101 201 255] 70350 runtime error: index out of range [9] with length 4"

func functionUsesFieldReadRun(compiled *program.CompiledFileSet, name string) bool {
	return functionHoldsOneOf(compiled, name, isa.SubOpGetStructFieldUint64Run, isa.SubOpGetElementFieldUint64Run)
}

func functionReadsElementFieldsInOneInstruction(compiled *program.CompiledFileSet, name string) bool {
	return functionHoldsOneOf(compiled, name, isa.SubOpGetElementFieldUint64Run)
}

func functionHoldsOneOf(compiled *program.CompiledFileSet, name string, operations ...isa.SubOpcode) bool {
	for _, function := range compiled.Root().Functions {
		if function.Name != name {
			continue
		}
		holds := slices.ContainsFunc(function.Body, func(word isa.Instruction) bool {
			return slices.ContainsFunc(operations, func(operation isa.SubOpcode) bool { return isa.InstrIsTier1SubOp(word, operation) })
		})
		if holds {
			return true
		}
	}
	return false
}

func runFieldReadRunProgram(t *testing.T, options ...app.Option) (*program.CompiledFileSet, any) {
	t.Helper()
	service := app.NewService(options...)
	service.UseSymbolProviders(stdlib.Providers()...)
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": fieldReadRunSource})
	require.NoError(t, err, "test harness: compiling the guest program")
	require.NoError(t, service.ExecuteInits(t.Context(), compiled), "test harness: running the package initialisers")
	result, err := service.CallFunction(t.Context(), compiled, "Run")
	require.NoError(t, err, "test harness: calling Run")
	return compiled, result
}

func TestACopiedStructsNarrowFieldsAreReadInOneInstructionUnderDebugInfo(t *testing.T) {
	t.Parallel()

	compiled, result := runFieldReadRunProgram(t, app.WithDebugInfo())
	require.True(t, functionUsesFieldReadRun(compiled, "blit"),
		"test harness: the copy of a palette entry kept for the debugger is followed by three narrow reads, which the run joins")
	require.True(t, functionUsesFieldReadRun(compiled, "sums"),
		"test harness: five narrow reads make a run of four and one read left over")
	require.Equal(t, fieldReadRunWant, result)
}

func TestACopiedStructsNarrowFieldsReadTheSameWithoutDebugInfo(t *testing.T) {
	t.Parallel()

	_, result := runFieldReadRunProgram(t)
	require.Equal(t, fieldReadRunWant, result)
}

func TestTheFieldsOfACopiedElementAreReadStraightFromTheElement(t *testing.T) {
	t.Parallel()

	compiled, result := runFieldReadRunProgram(t, app.WithDebugInfo())
	require.True(t, functionReadsElementFieldsInOneInstruction(compiled, "blit"),
		"test harness: the palette entry is read only for its three fields, so the element read and the run are one instruction")
	require.Equal(t, fieldReadRunWant, result, "an index past the palette still reports the element read's panic")
}
