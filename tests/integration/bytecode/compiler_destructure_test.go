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
	"pipit.sh/pipit/internal/debug"

	"github.com/stretchr/testify/require"
)

func compileForDestructure(t *testing.T, source string) string {
	t.Helper()
	service := app.NewService()
	compiled, err := service.CompileFileSet(context.Background(), map[string]string{"main.go": source})
	require.NoErrorf(t, err, "compile failed: %v", err)
	return debug.DisassembleAssembly(compiled)
}

const (
	destructureParserSource = `package main

type token struct {
	kind  int64
	value int64
}

type parser struct {
	tokens   []token
	position int
}

func EntrypointRun() int64 {
	p := &parser{tokens: []token{{kind: 1, value: 10}, {kind: 2, value: 20}}}
	total := int64(0)
	for p.position < len(p.tokens) {
		current := p.tokens[p.position]
		total += current.kind * 100
		total += current.value
		p.position++
	}
	return total
}`
)

func TestDestructuredSliceElem_FiresOnStructFieldSlice(t *testing.T) {
	t.Parallel()
	disasm := compileForDestructure(t, destructureParserSource)
	require.Positivef(t, strings.Count(disasm, "GET_STRUCT_FIELD_SLICE_INDEX_SCALAR"),
		"expected per-field fused loads for `current := p.tokens[p.position]`; got:\n%s", disasm)
}

func TestDestructuredSliceElem_RefusesWhenLocalEscapes(t *testing.T) {
	t.Parallel()
	disasm := compileForDestructure(t, `package main

type token struct {
	kind  int64
	value int64
}

type parser struct {
	tokens []token
}

func consume(t token) int64 {
	return t.kind + t.value
}

func EntrypointRun() int64 {
	p := &parser{tokens: []token{{kind: 1, value: 10}}}
	current := p.tokens[0]
	return consume(current)
}`)
	require.Zerof(t, strings.Count(disasm, "GET_STRUCT_FIELD_SLICE_INDEX_SCALAR"),
		"a local passed whole to a call must refuse per-field destructuring; got:\n%s", disasm)
}

func TestDestructuredSliceElem_RefusesAddressTakenLocal(t *testing.T) {
	t.Parallel()
	disasm := compileForDestructure(t, `package main

type token struct {
	kind  int64
	value int64
}

type parser struct {
	tokens []token
}

func EntrypointRun() int64 {
	p := &parser{tokens: []token{{kind: 1, value: 10}}}
	current := p.tokens[0]
	ptr := &current
	ptr.kind = 5
	return current.kind + current.value
}`)
	require.Zerof(t, strings.Count(disasm, "GET_STRUCT_FIELD_SLICE_INDEX_SCALAR"),
		"an address-taken local must refuse per-field destructuring; got:\n%s", disasm)
}
