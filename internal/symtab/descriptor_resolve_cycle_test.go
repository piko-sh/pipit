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

package symtab

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/symtab/descriptor"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

func requireNamedCompositeTypes(t *testing.T) {
	t.Helper()
	if !typemodel.NamedCompositeTypesSupported() {
		t.Skip("this build gives named composite types no runtime types of their own")
	}
}

func selfSliceDescriptor(name string) descriptor.TypeDescriptor {
	return descriptor.TypeDescriptor{
		Kind:        descriptor.KindNamed,
		PackagePath: "example.com/cycledecode",
		Name:        name,
		Element: &descriptor.TypeDescriptor{
			Kind:    descriptor.KindSlice,
			Element: &descriptor.TypeDescriptor{Kind: descriptor.KindNamed, PackagePath: "example.com/cycledecode", Name: name, BackReference: true},
		},
	}
}

func TestADescriptorWithABackReferenceResolvesToASelfReferentialType(t *testing.T) {
	t.Parallel()
	requireNamedCompositeTypes(t)

	resolved, err := ReflectTypeFor(t.Context(), selfSliceDescriptor("List"), nil)
	require.NoError(t, err)
	require.Equal(t, "cycledecode.List", resolved.String())
	require.Equal(t, resolved, resolved.Elem())

	again, err := ReflectTypeFor(t.Context(), selfSliceDescriptor("List"), nil)
	require.NoError(t, err)
	require.Equal(t, resolved, again, "a second load resolves to the same type")

	encoded := descriptor.ReflectTypeToDescriptor(resolved)
	require.Equal(t, descriptor.KindNamed, encoded.Kind)
	require.NotNil(t, encoded.Element)
	require.True(t, encoded.Element.Element.BackReference, "the recurrence is written as a back-reference")
	decoded, err := ReflectTypeFor(t.Context(), encoded, nil)
	require.NoError(t, err)
	require.Equal(t, resolved, decoded)
}

func TestAMutualGroupResolvesFromEitherMember(t *testing.T) {
	t.Parallel()
	requireNamedCompositeTypes(t)

	backTo := func(name string) *descriptor.TypeDescriptor {
		return &descriptor.TypeDescriptor{Kind: descriptor.KindNamed, PackagePath: "example.com/cycledecode", Name: name, BackReference: true}
	}
	named := func(name string, underlying *descriptor.TypeDescriptor) *descriptor.TypeDescriptor {
		return &descriptor.TypeDescriptor{Kind: descriptor.KindNamed, PackagePath: "example.com/cycledecode", Name: name, Element: underlying}
	}
	slice := func(element *descriptor.TypeDescriptor) *descriptor.TypeDescriptor {
		return &descriptor.TypeDescriptor{Kind: descriptor.KindSlice, Element: element}
	}
	fromX := named("MX", slice(named("MY", slice(backTo("MX")))))
	fromY := named("MY", slice(named("MX", slice(backTo("MY")))))

	x, err := ReflectTypeFor(t.Context(), *fromX, nil)
	require.NoError(t, err)
	y, err := ReflectTypeFor(t.Context(), *fromY, nil)
	require.NoError(t, err)
	require.Equal(t, y, x.Elem())
	require.Equal(t, x, y.Elem())
}

func TestABackReferenceOutsideItsDescriptionIsRefused(t *testing.T) {
	t.Parallel()

	stray := descriptor.TypeDescriptor{Kind: descriptor.KindNamed, PackagePath: "example.com/cycledecode", Name: "Stray", BackReference: true}
	_, err := ReflectTypeFor(t.Context(), stray, nil)
	require.ErrorIs(t, err, errCorruptTypeDescriptor)

	misplaced := descriptor.TypeDescriptor{
		Kind: descriptor.KindNamed, PackagePath: "example.com/cycledecode", Name: "Holder",
		Element: &descriptor.TypeDescriptor{Kind: descriptor.KindSlice, Element: &stray},
	}
	_, err = ReflectTypeFor(t.Context(), misplaced, nil)
	require.ErrorIs(t, err, errCorruptTypeDescriptor)
}

func selfArrayDescriptor(name string, length int) descriptor.TypeDescriptor {
	return descriptor.TypeDescriptor{
		Kind:        descriptor.KindNamed,
		PackagePath: "example.com/cycledecode",
		Name:        name,
		Element: &descriptor.TypeDescriptor{
			Kind:   descriptor.KindArray,
			Length: length,
			Element: &descriptor.TypeDescriptor{
				Kind:    descriptor.KindPtr,
				Element: &descriptor.TypeDescriptor{Kind: descriptor.KindNamed, PackagePath: "example.com/cycledecode", Name: name, BackReference: true},
			},
		},
	}
}

func TestABackReferencingArrayWithABadLengthIsRefused(t *testing.T) {
	t.Parallel()
	requireNamedCompositeTypes(t)

	for _, length := range []int{-1, 1 << 40} {
		_, err := ReflectTypeFor(t.Context(), selfArrayDescriptor("BadArray", length), nil)
		require.ErrorIs(t, err, errCorruptTypeDescriptor, "length %d", length)
	}

	nested := selfArrayDescriptor("NestedBadArray", 2)
	nested.Element.Element = &descriptor.TypeDescriptor{
		Kind:   descriptor.KindArray,
		Length: -3,
		Element: &descriptor.TypeDescriptor{
			Kind:    descriptor.KindPtr,
			Element: &descriptor.TypeDescriptor{Kind: descriptor.KindNamed, PackagePath: "example.com/cycledecode", Name: "NestedBadArray", BackReference: true},
		},
	}
	_, err := ReflectTypeFor(t.Context(), nested, nil)
	require.ErrorIs(t, err, errCorruptTypeDescriptor, "a negative length inside an array member's element")

	resolved, err := ReflectTypeFor(t.Context(), selfArrayDescriptor("GoodArray", 2), nil)
	require.NoError(t, err)
	require.Equal(t, resolved, resolved.Elem().Elem())
}

func TestANamedCompositeDescriptorMayNotClaimAHostPackage(t *testing.T) {
	t.Parallel()

	registry := NewSymbolRegistry(SymbolExports{"net/http": {"Get": reflect.ValueOf(func() {})}})
	header := descriptor.TypeDescriptor{
		Kind: descriptor.KindNamed, PackagePath: "net/http", Name: "Header",
		Element: &descriptor.TypeDescriptor{Kind: descriptor.KindSlice, Element: &descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int)}},
	}
	_, err := ReflectTypeFor(t.Context(), header, registry)
	require.ErrorIs(t, err, errCorruptTypeDescriptor)
	require.ErrorContains(t, err, "host package")

	long := header
	long.PackagePath = "example.com/long"
	long.Name = strings.Repeat("N", maximumNamedDescriptorBytes+1)
	_, err = ReflectTypeFor(t.Context(), long, registry)
	require.ErrorIs(t, err, errCorruptTypeDescriptor)

	unnamed := header
	unnamed.PackagePath = "example.com/unnamed"
	unnamed.Name = ""
	_, err = ReflectTypeFor(t.Context(), unnamed, registry)
	require.ErrorIs(t, err, errCorruptTypeDescriptor)
}

func TestARegistryChargesItsQuotaForNewTypes(t *testing.T) {
	t.Parallel()
	requireNamedCompositeTypes(t)

	registry := NewSymbolRegistry(nil)
	require.Equal(t, typemodel.DefaultNamedCompositeQuota, registry.NamedCompositeQuota().Limit())
	quota := typemodel.NewNamedCompositeQuota(1)
	registry.SetNamedCompositeQuota(quota)
	require.Same(t, quota, registry.Scoped([]string{"fmt"}).NamedCompositeQuota(), "a scoped registry charges the same quota")

	_, err := ReflectTypeFor(t.Context(), selfSliceDescriptor("QuotaList"), registry)
	require.NoError(t, err)
	require.Equal(t, 1, quota.Used())
	_, err = ReflectTypeFor(t.Context(), selfSliceDescriptor("QuotaList"), registry)
	require.NoError(t, err, "a type already built costs nothing")
	_, err = ReflectTypeFor(t.Context(), selfSliceDescriptor("QuotaOther"), registry)
	require.ErrorIs(t, err, typemodel.ErrNamedCompositeQuota)

	var none *SymbolRegistry
	require.Nil(t, none.NamedCompositeQuota())
}

func TestTheMemoDropsOnlyWhatARolledBackBuildAdded(t *testing.T) {
	t.Parallel()

	first, second, third := &descriptor.TypeDescriptor{}, &descriptor.TypeDescriptor{}, &descriptor.TypeDescriptor{}
	memo := newReflectTypeMemo(nil)
	memo.store(first, reflect.TypeFor[int]())
	mark := len(memo.added)
	memo.store(second, reflect.TypeFor[string]())
	memo.store(first, reflect.TypeFor[int8]())
	memo.store(third, reflect.TypeFor[bool]())
	memo.rollback(mark)

	require.Len(t, memo.types, 1)
	require.Contains(t, memo.types, first)
	require.Len(t, memo.added, 1)
}

func TestTheUnionCompressesItsPaths(t *testing.T) {
	t.Parallel()

	union := newCycleWalk().union
	keys := []string{"a", "b", "c", "d", "e"}
	for _, key := range keys {
		union.add(key)
	}
	for index := 1; index < len(keys); index++ {
		union.join(keys[index-1], keys[index])
	}
	root := union.find("e")
	for _, key := range keys {
		require.Equal(t, root, union.find(key))
		require.Equal(t, root, union.parent[key], "every key points at the representative after a find")
	}
	require.Equal(t, len(keys), union.groupSize(root))
	union.cyclic[root] = true
	union.add("f")
	union.cyclic["f"] = false
	union.join("f", "a")
	require.True(t, union.cyclic[union.find("f")], "the cyclic mark survives a join")
}

func TestADescriptorNestingManyNamedTypesIsWalkedOnce(t *testing.T) {
	t.Parallel()
	requireNamedCompositeTypes(t)

	const depth = 200
	inner := &descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int)}
	for level := range depth {
		inner = &descriptor.TypeDescriptor{
			Kind: descriptor.KindNamed, PackagePath: "example.com/walkonce", Name: "Level" + strconv.Itoa(level),
			Element: &descriptor.TypeDescriptor{Kind: descriptor.KindSlice, Element: inner},
		}
	}
	memo := newReflectTypeMemo(inner)
	resolved, err := descriptorToReflectTypeAtDepth(t.Context(), *inner, nil, 0, memo)
	require.NoError(t, err)
	require.Equal(t, "walkonce.Level199", resolved.String())
	require.NotNil(t, memo.cycles)
	require.Len(t, memo.cycles.full, depth, "the top-level walk saw every named type once")
}

func TestANamedCompositeDescriptorMayNameAnInterpretedPackage(t *testing.T) {
	t.Parallel()
	if !typemodel.NamedCompositeTypesSupported() {
		t.Skip("this build gives a named composite type its underlying runtime type")
	}

	registry := NewSymbolRegistry(SymbolExports{"example.com/lib": {"Run": reflect.ValueOf(func() {})}})
	filters := descriptor.TypeDescriptor{
		Kind: descriptor.KindNamed, PackagePath: "example.com/lib", Name: "Filters",
		Element: &descriptor.TypeDescriptor{Kind: descriptor.KindSlice, Element: &descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.String)}},
	}
	_, err := ReflectTypeFor(t.Context(), filters, registry)
	require.ErrorContains(t, err, "host package", "until the package is known to be interpreted")

	registry.MarkScriptPackage("example.com/lib")
	resolved, err := ReflectTypeFor(t.Context(), filters, registry)
	require.NoError(t, err, "a loaded module's own named types resolve")
	require.Equal(t, reflect.Slice, resolved.Kind())
	require.True(t, registry.HasPackage("example.com/lib"))
	require.False(t, registry.HasHostPackage("example.com/lib"))
}
