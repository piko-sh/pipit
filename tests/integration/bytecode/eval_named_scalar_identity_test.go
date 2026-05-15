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
	"go/token"
	"go/types"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/app"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

func TestEvalNamedScalarAssertIdentity(t *testing.T) {
	tests := []struct {
		name   string
		code   string
		expect string
	}{
		{
			name: "string_named_vs_plain",
			code: `type Word string
func check() string {
	var v any = Word("w")
	_, plain := v.(string)
	_, named := v.(Word)
	if !plain && named {
		return "exact"
	}
	return "leak"
}
check()`,
			expect: "exact",
		},
		{
			name: "bool_named_vs_plain",
			code: `type Flag bool
func check() string {
	var f Flag = true
	var v any = f
	_, plain := v.(bool)
	_, named := v.(Flag)
	if !plain && named {
		return "exact"
	}
	return "leak"
}
check()`,
			expect: "exact",
		},
		{
			name: "float_named_vs_plain",
			code: `type Celsius float64
func check() string {
	var v any = Celsius(20)
	_, plain := v.(float64)
	_, named := v.(Celsius)
	if !plain && named {
		return "exact"
	}
	return "leak"
}
check()`,
			expect: "exact",
		},
		{
			name: "plain_never_satisfies_named",
			code: `type Word string
func check() string {
	var v any = "w"
	_, named := v.(Word)
	_, plain := v.(string)
	if plain && !named {
		return "exact"
	}
	return "leak"
}
check()`,
			expect: "exact",
		},
		{
			name: "interface_equality_named_vs_plain",
			code: `type Word string
func check() string {
	var v any = Word("w")
	if v == "w" {
		return "collapsed"
	}
	if v == Word("w") {
		return "exact"
	}
	return "lost"
}
check()`,
			expect: "exact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := app.NewService()
			result, err := service.Eval(context.Background(), tt.code)
			require.NoError(t, err)
			require.Equal(t, tt.expect, result)
		})
	}
}

func TestNamedScalarPoolAssignment(t *testing.T) {
	t.Parallel()

	named := declareNamedBasic(t, "PoolProbeWord", types.Typ[types.String])
	first := typemodel.NamedScalarPoolTypeFor(named)
	require.NotNil(t, first)
	require.Equal(t, reflect.String, first.Kind())
	require.True(t, typemodel.IsNamedScalarPoolType(first))

	second := typemodel.NamedScalarPoolTypeFor(named)
	require.Equal(t, first, second, "repeat assignment must be stable")

	info, ok := typemodel.LookupNamedScalarPoolInfo(first)
	require.True(t, ok)
	require.Equal(t, "PoolProbeWord", info.BareName)
	require.Equal(t, "poolprobe.PoolProbeWord", info.QualifiedName)

	require.Equal(t, "poolprobe.PoolProbeWord", typemodel.NamedScalarDisplayType(first))
	require.Equal(t, "string", typemodel.NamedScalarDisplayType(reflect.TypeFor[string]()))

	otherKind := declareNamedBasic(t, "PoolProbeWord", types.Typ[types.Int])
	intPool := typemodel.NamedScalarPoolTypeFor(otherKind)
	require.NotNil(t, intPool)
	require.Equal(t, reflect.Int, intPool.Kind())
	require.NotEqual(t, first, intPool, "same name with different underlying kinds must not share a pool entry")
}

func TestNamedScalarPoolUnifiesCompileAndLoadSpellings(t *testing.T) {
	t.Parallel()

	runeBasic, ok := types.Universe.Lookup("rune").Type().(*types.Basic)
	require.True(t, ok)

	fromCompile := typemodel.NamedScalarPoolTypeFor(declareNamedBasic(t, "PoolProbeOperator", runeBasic))
	require.NotNil(t, fromCompile)
	require.Equal(t, reflect.Int32, fromCompile.Kind())

	fromLoad := typemodel.NamedScalarPoolTypeForPath("pipit.test/poolprobe", "PoolProbeOperator", types.Int32)
	require.Equal(t, fromCompile, fromLoad, "one source type must own one pool column")
}

func declareNamedBasic(t *testing.T, name string, basic *types.Basic) *types.Named {
	t.Helper()
	pkg := types.NewPackage("pipit.test/poolprobe", "poolprobe")
	object := types.NewTypeName(token.NoPos, pkg, name, nil)
	return types.NewNamed(object, basic, nil)
}
