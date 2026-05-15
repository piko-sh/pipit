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
	"go/types"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func TestEmitNarrowIntegerTruncationEmitsForNarrowUint(t *testing.T) {
	t.Parallel()

	c := newTestCompiler(t)
	location := program.VarLocation{Register: 5, Kind: isa.RegisterUint}
	startLength := len(c.Function.Body)
	c.EmitNarrowIntegerTruncation(location, types.Typ[types.Uint8])

	require.Len(t, c.Function.Body, startLength+1,
		"a single opTruncateNarrow instruction should be emitted for narrow uint8")
	emitted := c.Function.Body[startLength]
	require.Equal(t, isa.OpTruncateNarrow, emitted.Op)
	require.Equal(t, uint8(5), emitted.A, "operand A is the register being truncated")
	require.Equal(t, uint8(8), emitted.B, "operand B is the bit width")
	require.Equal(t, uint8(isa.RegisterUint), emitted.C, "operand C is the bank kind")
}

func TestEmitNarrowIntegerTruncationEmitsForNarrowInt(t *testing.T) {
	t.Parallel()

	c := newTestCompiler(t)
	location := program.VarLocation{Register: 3, Kind: isa.RegisterInt}
	startLength := len(c.Function.Body)
	c.EmitNarrowIntegerTruncation(location, types.Typ[types.Int16])

	require.Len(t, c.Function.Body, startLength+1)
	emitted := c.Function.Body[startLength]
	require.Equal(t, isa.OpTruncateNarrow, emitted.Op)
	require.Equal(t, uint8(3), emitted.A)
	require.Equal(t, uint8(16), emitted.B)
	require.Equal(t, uint8(isa.RegisterInt), emitted.C)
}

func TestEmitNarrowIntegerTruncationSkipsFullWidthTypes(t *testing.T) {
	t.Parallel()

	c := newTestCompiler(t)
	tests := []struct {
		t    types.Type
		name string
	}{
		{name: "int", t: types.Typ[types.Int]},
		{name: "int64", t: types.Typ[types.Int64]},
		{name: "uint", t: types.Typ[types.Uint]},
		{name: "uint64", t: types.Typ[types.Uint64]},
		{name: "string", t: types.Typ[types.String]},
		{name: "float64", t: types.Typ[types.Float64]},
		{name: "bool", t: types.Typ[types.Bool]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			startLength := len(c.Function.Body)
			c.EmitNarrowIntegerTruncation(program.VarLocation{Register: 0, Kind: isa.RegisterInt}, tt.t)
			require.Equal(t, startLength, len(c.Function.Body),
				"no instruction should be emitted for full-width or non-integer types")
		})
	}
}

func TestEmitNarrowIntegerTruncationSkipsWrongBank(t *testing.T) {
	t.Parallel()

	c := newTestCompiler(t)
	startLength := len(c.Function.Body)
	c.EmitNarrowIntegerTruncation(program.VarLocation{Register: 0, Kind: isa.RegisterGeneral}, types.Typ[types.Uint8])
	require.Equal(t, startLength, len(c.Function.Body),
		"truncation only applies to int and uint banks; general bank is left untouched because the cell-side wrap is handled by reflect.SetUint when the boxed value is unpacked")
}

func TestEmitNarrowIntegerTruncationSkipsNilType(t *testing.T) {
	t.Parallel()

	c := newTestCompiler(t)
	startLength := len(c.Function.Body)
	c.EmitNarrowIntegerTruncation(program.VarLocation{Register: 0, Kind: isa.RegisterInt}, nil)
	require.Equal(t, startLength, len(c.Function.Body),
		"nil static type means the call site has no type info; skip the truncation rather than panic")
}
