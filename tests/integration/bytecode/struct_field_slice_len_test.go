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
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/debug"
)

func compileForSliceLen(t *testing.T, source string) string {
	t.Helper()
	service := app.NewService()
	compiled, err := service.CompileFileSet(t.Context(), map[string]string{"main.go": source})
	require.NoErrorf(t, err, "compile failed: %v", err)
	return debug.DisassembleAssembly(compiled)
}

func TestStructFieldSliceLen(t *testing.T) {
	t.Parallel()
	runDisassemblyExpectations(t, compileForSliceLen, []disassemblyExpectation{
		{
			name: "fires on pointer receiver byte field",
			source: `package main

type parser struct {
	source   []byte
	position int
}

func EntrypointRun() int {
	p := &parser{source: []byte("abcdef")}
	total := 0
	for p.position < len(p.source) {
		total += int(p.source[p.position])
		p.position++
	}
	return total
}`,
			contains: []string{"GET_STRUCT_FIELD_SLICE_LEN"},
		},
		{
			name: "fires on value receiver int field",
			source: `package main

type holder struct {
	values []int
}

func EntrypointRun() int {
	h := holder{values: []int{1, 2, 3}}
	return len(h.values)
}`,
			contains: []string{"GET_STRUCT_FIELD_SLICE_LEN"},
		},
		{
			name: "fires on methodless named slice field",
			source: `package main

type row []int

type grid struct {
	cells row
}

func EntrypointRun() int {
	g := grid{cells: row{4, 5}}
	return len(g.cells)
}`,
			contains: []string{"GET_STRUCT_FIELD_SLICE_LEN"},
		},
		{
			name: "refuses named slice with methods",
			source: `package main

type stack []int

func (s stack) top() int {
	if len(s) == 0 {
		return 0
	}
	return s[len(s)-1]
}

type machine struct {
	operands stack
}

func EntrypointRun() int {
	m := machine{operands: stack{7}}
	return len(m.operands) + m.operands.top()
}`,
			absent: []string{"GET_STRUCT_FIELD_SLICE_LEN"},
		},
		{
			name: "refuses map and array fields",
			source: `package main

type box struct {
	index map[string]int
	quad  [4]int
}

func EntrypointRun() int {
	b := box{index: map[string]int{"a": 1}}
	return len(b.index) + len(b.quad)
}`,
			absent: []string{"GET_STRUCT_FIELD_SLICE_LEN"},
		},
		{
			name: "refuses local slice",
			source: `package main

func EntrypointRun() int {
	local := []int{1, 2, 3}
	return len(local)
}`,
			absent: []string{"GET_STRUCT_FIELD_SLICE_LEN"},
		},
	})
}
