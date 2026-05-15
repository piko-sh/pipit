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

package typemodel

import (
	"fmt"
	"go/token"
	"go/types"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"
)

func newNamedBasicType(name string, kind types.BasicKind) *types.Named {
	pkg := types.NewPackage("example.com/named_scalar_pool_probe", "probe")
	typeName := types.NewTypeName(token.NoPos, pkg, name, nil)
	return types.NewNamed(typeName, types.Typ[kind], nil)
}

func newNamedAliasBasicType(t *testing.T, name, alias string) *types.Named {
	t.Helper()
	basic, ok := types.Universe.Lookup(alias).Type().(*types.Basic)
	require.True(t, ok, "universe %s must be a basic type", alias)
	pkg := types.NewPackage("example.com/named_scalar_pool_probe", "probe")
	typeName := types.NewTypeName(token.NoPos, pkg, name, nil)
	return types.NewNamed(typeName, basic, nil)
}

func TestNamedScalarPoolUnifiesAliasBasicSpellings(t *testing.T) {
	t.Parallel()
	for _, probe := range []struct {
		alias string
		kind  types.BasicKind
	}{
		{alias: "rune", kind: types.Int32},
		{alias: "byte", kind: types.Uint8},
	} {
		t.Run(probe.alias, func(t *testing.T) {
			t.Parallel()
			registry := newNamedScalarPoolRegistry()

			fromSource := registry.typeFor(newNamedAliasBasicType(t, "Operator", probe.alias), maxNamedScalarPoolKeys, false)
			fromDescriptor := registry.typeFor(newNamedBasicType("Operator", probe.kind), maxNamedScalarPoolKeys, true)

			require.NotNil(t, fromSource)
			require.Equal(t, fromSource, fromDescriptor,
				"%s and %s spell one source type and must share a pool column", probe.alias, types.Typ[probe.kind].Name())
			require.Len(t, registry.byKey, 1)
		})
	}
}

func TestNamedScalarPoolPrefersTypeCheckedPackageName(t *testing.T) {
	t.Parallel()
	registry := newNamedScalarPoolRegistry()

	synthesised := types.NewPackage("github.com/hashicorp/go-version", "go-version")
	synthesisedName := types.NewTypeName(token.NoPos, synthesised, "operator", nil)
	poolType := registry.typeFor(types.NewNamed(synthesisedName, types.Typ[types.Int32], nil), maxNamedScalarPoolKeys, true)
	require.NotNil(t, poolType)
	require.Equal(t, "go-version", registry.info[poolType].PkgName)

	checked := types.NewPackage("github.com/hashicorp/go-version", "version")
	checkedName := types.NewTypeName(token.NoPos, checked, "operator", nil)
	require.Equal(t, poolType, registry.typeFor(types.NewNamed(checkedName, types.Typ[types.Int32], nil), maxNamedScalarPoolKeys, false))
	require.Equal(t, "version", registry.info[poolType].PkgName, "the type-checked package name must win")
	require.Equal(t, "version.operator", registry.info[poolType].QualifiedName)
}

func TestNamedScalarPoolKeyCapBoundsRetention(t *testing.T) {
	t.Parallel()
	registry := newNamedScalarPoolRegistry()
	const keyCap = 2

	first := registry.typeFor(newNamedBasicType("First", types.Int), keyCap, false)
	second := registry.typeFor(newNamedBasicType("Second", types.Int), keyCap, false)
	require.NotNil(t, first)
	require.NotNil(t, second)
	require.NotEqual(t, first, second, "distinct named types must get distinct pool types")
	require.Len(t, registry.byKey, keyCap)

	third := registry.typeFor(newNamedBasicType("Third", types.Int), keyCap, false)
	require.Nil(t, third, "a distinct type past the cap must collapse rather than be stored")
	require.Len(t, registry.byKey, keyCap, "the cap must bound the map")

	require.Equal(t, first, registry.typeFor(newNamedBasicType("First", types.Int), keyCap, false),
		"an already-assigned type must still resolve past the cap")
	require.Len(t, registry.byKey, keyCap)
}

func TestNamedScalarPoolExhaustionCountsAndCollapses(t *testing.T) {
	registry := newNamedScalarPoolRegistry()
	for index := range namedScalarPoolGeneratedColumns {
		require.NotNil(t, registry.typeFor(newNamedBasicType(fmt.Sprintf("Word%d", index), types.String), maxNamedScalarPoolKeys, false))
	}

	before := NamedScalarPoolExhaustions()
	require.Nil(t, registry.typeFor(newNamedBasicType("Overflow", types.String), maxNamedScalarPoolKeys, false))
	require.Equal(t, before+1, NamedScalarPoolExhaustions())
	require.True(t, namedScalarPoolExhaustionWarned.Load())

	require.Nil(t, registry.typeFor(newNamedBasicType("Overflow", types.String), maxNamedScalarPoolKeys, false))
	require.Equal(t, before+1, NamedScalarPoolExhaustions(), "a recorded collapse must not count again")

	require.NotNil(t, registry.typeFor(newNamedBasicType("Count", types.Int), maxNamedScalarPoolKeys, false),
		"other kinds keep their own columns")
	require.Equal(t, before+1, NamedScalarPoolExhaustions())
}

func TestNamedScalarPoolHasGeneratedColumnCount(t *testing.T) {
	t.Parallel()
	require.Equal(t, 512, namedScalarPoolGeneratedColumns)
	require.Len(t, namedScalarPoolColumns, namedScalarPoolGeneratedColumns)

	seen := make(map[reflect.Type]struct{}, len(namedScalarPoolColumns)*namedScalarKindRows)
	for columnIndex := range namedScalarPoolColumns {
		for row, poolType := range &namedScalarPoolColumns[columnIndex] {
			require.NotNil(t, poolType)
			_, duplicate := seen[poolType]
			require.False(t, duplicate, "column %d row %d repeats %v", columnIndex, row, poolType)
			seen[poolType] = struct{}{}
		}
	}
	require.Len(t, namedScalarPoolMembers, len(seen))
}

func TestNamedScalarPoolTypeForResultReportsExhaustion(t *testing.T) {
	registry := newNamedScalarPoolRegistry()
	for index := range namedScalarPoolGeneratedColumns {
		require.NotNil(t, registry.typeFor(newNamedBasicType(fmt.Sprintf("Level%d", index), types.Int), maxNamedScalarPoolKeys, false))
	}
	poolType, exhausted := registry.typeForResult(newNamedBasicType("Overflow", types.Int), maxNamedScalarPoolKeys)
	require.Nil(t, poolType)
	require.True(t, exhausted, "a poolable kind past the last column must report exhaustion")

	structType := types.NewNamed(types.NewTypeName(token.NoPos, nil, "S", nil), types.NewStruct(nil, nil), nil)
	poolType, exhausted = registry.typeForResult(structType, maxNamedScalarPoolKeys)
	require.Nil(t, poolType)
	require.False(t, exhausted, "a non-basic type is not poolable and must not report exhaustion")
}
