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
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"reflect"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/link"
	"pipit.sh/pipit/internal/symtab/descriptor"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

func TestDescriptorToReflectTypeRefusesExcessiveNesting(t *testing.T) {
	t.Parallel()
	typeDescriptor := &descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int)}
	for range descriptor.MaxTypeDescriptorDepth + 16 {
		typeDescriptor = &descriptor.TypeDescriptor{Kind: descriptor.KindPtr, Element: typeDescriptor}
	}
	reconstructed, err := ReflectTypeFor(t.Context(), *typeDescriptor, nil)
	require.ErrorIs(t, err, errCorruptTypeDescriptor)
	require.Nil(t, reconstructed)
}

func TestDescriptorToReflectTypeAllowsModerateNesting(t *testing.T) {
	t.Parallel()
	typeDescriptor := &descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int)}
	for range 8 {
		typeDescriptor = &descriptor.TypeDescriptor{Kind: descriptor.KindPtr, Element: typeDescriptor}
	}
	reconstructed, err := ReflectTypeFor(t.Context(), *typeDescriptor, nil)
	require.NoError(t, err)
	require.Equal(t, reflect.Pointer, reconstructed.Kind())
}

func hostNamedTypesRegistry() *SymbolRegistry {
	return NewSymbolRegistry(SymbolExports{
		"fmt": {"Stringer": reflect.ValueOf((*fmt.Stringer)(nil))},
		"io":  {"Reader": reflect.ValueOf((*io.Reader)(nil))},
		"net/http": {
			"Header":      reflect.ValueOf((*http.Header)(nil)),
			"HandlerFunc": reflect.ValueOf((*http.HandlerFunc)(nil)),
		},
		"io/fs":   {"FileMode": reflect.ValueOf((*fs.FileMode)(nil))},
		"reflect": {"Kind": reflect.ValueOf((*reflect.Kind)(nil))},
		"sort":    {"IntSlice": reflect.ValueOf((*sort.IntSlice)(nil))},
		"time": {
			"Duration": reflect.ValueOf((*time.Duration)(nil)),
			"Month":    reflect.ValueOf((*time.Month)(nil)),
			"Time":     reflect.ValueOf((*time.Time)(nil)),
		},
	})
}

func TestDescriptorRoundTripPreservesHostNamedTypes(t *testing.T) {
	t.Parallel()
	registry := hostNamedTypesRegistry()
	for _, want := range []reflect.Type{
		reflect.TypeFor[time.Time](),
		reflect.TypeFor[time.Duration](),
		reflect.TypeFor[time.Month](),
		reflect.TypeFor[reflect.Kind](),
		reflect.TypeFor[fs.FileMode](),
		reflect.TypeFor[http.Header](),
		reflect.TypeFor[sort.IntSlice](),
		reflect.TypeFor[[]time.Duration](),
		reflect.TypeFor[map[string]time.Month](),
		reflect.TypeFor[func(time.Duration) string](),
		reflect.TypeFor[[3]time.Month](),
		reflect.TypeFor[*time.Duration](),
		reflect.TypeFor[http.HandlerFunc](),
		reflect.TypeFor[io.Reader](),
		reflect.TypeFor[fmt.Stringer](),
		reflect.TypeFor[error](),
		reflect.TypeFor[[]error](),
		reflect.TypeFor[chan error](),
		reflect.TypeFor[map[string]io.Reader](),
	} {
		t.Run(want.String(), func(t *testing.T) {
			t.Parallel()
			reconstructed, err := ReflectTypeFor(t.Context(), descriptor.ReflectTypeToDescriptor(want), registry)
			require.NoError(t, err)
			require.Same(t, want, reconstructed)
		})
	}
}

func TestDescriptorRoundTripPreservesNamedCompositeTypes(t *testing.T) {
	t.Parallel()
	requireNamedCompositeTypes(t)
	registry := hostNamedTypesRegistry()
	build := func(name string, underlying reflect.Type) reflect.Type {
		t.Helper()
		identity := typemodel.NamedCompositeIdentity{PkgPath: "example.com/shapes", PkgName: "shapes", Name: name, BareName: name}
		named, err := typemodel.NamedCompositeTypeFor(identity, underlying)
		require.NoError(t, err)
		return named
	}
	points := build("Points", reflect.TypeFor[[]float64]())
	for _, want := range []reflect.Type{
		points,
		build("Grid", reflect.TypeFor[[2][2]time.Month]()),
		build("Index", reflect.TypeFor[map[string]time.Duration]()),
		build("Lane", reflect.TypeFor[<-chan error]()),
		build("Handler", reflect.TypeFor[func(time.Duration, ...string) error]()),
		build("Stack[int]", reflect.TypeFor[[]int]()),
		reflect.SliceOf(points),
		reflect.MapOf(reflect.TypeFor[string](), points),
		reflect.PointerTo(points),
	} {
		t.Run(want.String(), func(t *testing.T) {
			t.Parallel()
			encoded := descriptor.ReflectTypeToDescriptor(want)
			reconstructed, err := ReflectTypeFor(t.Context(), encoded, registry)
			require.NoError(t, err)
			require.Same(t, want, reconstructed)
		})
	}
}

func TestDescriptorHostNamedTypeFallsBackToShape(t *testing.T) {
	t.Parallel()
	durationDescriptor := descriptor.ReflectTypeToDescriptor(reflect.TypeFor[time.Duration]())
	monthNamedInt64 := descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int64), PackagePath: "time", Name: "Month"}
	headerDescriptor := descriptor.ReflectTypeToDescriptor(reflect.TypeFor[http.Header]())
	readerDescriptor := descriptor.ReflectTypeToDescriptor(reflect.TypeFor[io.Reader]())

	tests := []struct {
		name       string
		descriptor descriptor.TypeDescriptor
		registry   *SymbolRegistry
		want       reflect.Type
	}{
		{name: "nil registry", descriptor: durationDescriptor, registry: nil, want: reflect.TypeFor[int64]()},
		{name: "unregistered package", descriptor: durationDescriptor, registry: NewSymbolRegistry(nil), want: reflect.TypeFor[int64]()},
		{name: "scoped out", descriptor: durationDescriptor, registry: hostNamedTypesRegistry().Scoped([]string{"sort"}), want: reflect.TypeFor[int64]()},
		{name: "kind mismatch", descriptor: monthNamedInt64, registry: hostNamedTypesRegistry(), want: reflect.TypeFor[int64]()},
		{name: "named map unregistered", descriptor: headerDescriptor, registry: NewSymbolRegistry(nil), want: reflect.TypeFor[map[string][]string]()},
		{name: "named interface unregistered", descriptor: readerDescriptor, registry: NewSymbolRegistry(nil), want: reflect.TypeFor[any]()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			reconstructed, err := ReflectTypeFor(t.Context(), tt.descriptor, tt.registry)
			require.NoError(t, err)
			require.Same(t, tt.want, reconstructed)
		})
	}
}

func TestDescriptorHostNamedTypeResolvesThroughAlias(t *testing.T) {
	t.Parallel()
	registry := NewSymbolRegistry(SymbolExports{
		"os": {"FileMode": reflect.ValueOf((*fs.FileMode)(nil))},
	})
	want := reflect.TypeFor[fs.FileMode]()
	reconstructed, err := ReflectTypeFor(t.Context(), descriptor.ReflectTypeToDescriptor(want), registry)
	require.NoError(t, err)
	require.Same(t, want, reconstructed)
}

func TestHostNamedTypeIndexFollowsRegistration(t *testing.T) {
	t.Parallel()
	registry := NewSymbolRegistry(nil)
	_, found := registry.HostNamedType("time", "Duration")
	require.False(t, found)

	registry.RegisterPackage("facade", map[string]reflect.Value{"Duration": reflect.ValueOf((*time.Duration)(nil))})
	resolved, found := registry.HostNamedType("time", "Duration")
	require.True(t, found, "a registration after the first lookup must be indexed")
	require.Equal(t, reflect.TypeFor[time.Duration](), resolved)

	registry.OverlayPackage("other", map[string]reflect.Value{"Month": reflect.ValueOf((*time.Month)(nil))})
	resolved, found = registry.HostNamedType("time", "Month")
	require.True(t, found, "an overlay after the first lookup must be indexed")
	require.Equal(t, reflect.TypeFor[time.Month](), resolved)
}

func TestAnEmbeddedFieldStaysEmbeddedThroughADescriptor(t *testing.T) {
	t.Parallel()

	type Inner struct{ A int }
	original := reflect.StructOf([]reflect.StructField{
		{Name: "Inner", Type: reflect.TypeFor[Inner](), Anonymous: true},
		{Name: "B", Type: reflect.TypeFor[string]()},
	})
	encoded := descriptor.ReflectTypeToDescriptor(original)
	require.True(t, encoded.Fields[0].Embedded)
	require.False(t, encoded.Fields[1].Embedded)
	decoded, err := ReflectTypeFor(t.Context(), encoded, nil)
	require.NoError(t, err)
	require.True(t, decoded.Field(0).Anonymous)
	require.Equal(t, original.Field(0).Name, decoded.Field(0).Name)
}

func TestTheErasedInstantiationOfANativeBackedGenericTypeResolvesToTheNativeType(t *testing.T) {
	t.Parallel()

	erased := reflect.TypeFor[atomic.Pointer[struct{}]]()
	registry := NewSymbolRegistry(SymbolExports{"sync/atomic": {
		"Pointer": reflect.ValueOf(link.WrapNativeGenericType("Pointer", 1, erased, reflect.TypeFor[struct{}]())),
	}})
	described := descriptor.ReflectTypeToDescriptor(erased)

	resolved, ok := nativeBackedInstantiation(described, registry)
	require.True(t, ok)
	require.Equal(t, erased, resolved, "the loaded type keeps the native methods every instantiation runs on")

	described.Name = "Pointer[int]"
	_, ok = nativeBackedInstantiation(described, registry)
	require.False(t, ok, "only the erased instantiation itself is matched")
	_, ok = nativeBackedInstantiation(descriptor.ReflectTypeToDescriptor(reflect.TypeFor[atomic.Int64]()), registry)
	require.False(t, ok)
}

var counterVariable = 3

func TestConstantsAreRebuiltFromTheirDescriptors(t *testing.T) {
	t.Parallel()

	type config struct{ Name string }
	registry := NewSymbolRegistry(SymbolExports{"example.com/host": {
		"Answer":  reflect.ValueOf(42),
		"Counter": reflect.ValueOf(&counterVariable).Elem(),
		"Config":  reflect.ValueOf((*config)(nil)),
	}})

	value, err := ReconstructConstant(t.Context(), descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantPackageSymbol, PackagePath: "example.com/host", SymbolName: "Answer"}, registry)
	require.NoError(t, err)
	require.EqualValues(t, 42, value.Int())
	address, err := ReconstructConstant(t.Context(), descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantPackageSymbolAddress, PackagePath: "example.com/host", SymbolName: "Counter"}, registry)
	require.NoError(t, err)
	require.Equal(t, &counterVariable, address.Interface())
	_, err = ReconstructConstant(t.Context(), descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantPackageSymbolAddress, PackagePath: "example.com/host", SymbolName: "Answer"}, registry)
	require.ErrorContains(t, err, "not a settable variable")
	zero, err := ReconstructConstant(t.Context(), descriptor.GeneralConstantDescriptor{Kind: descriptor.GeneralConstantNamedTypeZero, PackagePath: "example.com/host", SymbolName: "Config"}, registry)
	require.NoError(t, err)
	require.Equal(t, config{}, zero.Interface())
	composite, err := ReconstructConstant(t.Context(), descriptor.GeneralConstantDescriptor{
		Kind:           descriptor.GeneralConstantCompositeZero,
		TypeDescriptor: descriptor.TypeDescriptor{Kind: descriptor.KindSlice, Element: &descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.String)}},
	}, registry)
	require.NoError(t, err)
	require.Equal(t, reflect.TypeFor[[]string](), composite.Type())

	for _, missing := range []descriptor.GeneralConstantDescriptor{
		{Kind: descriptor.GeneralConstantPackageSymbol, PackagePath: "example.com/host", SymbolName: "Gone"},
		{Kind: descriptor.GeneralConstantPackageSymbolAddress, PackagePath: "example.com/host", SymbolName: "Gone"},
		{Kind: descriptor.GeneralConstantNamedTypeZero, PackagePath: "example.com/host", SymbolName: "Gone"},
		{Kind: descriptor.GeneralConstantCompositeZero, TypeDescriptor: descriptor.TypeDescriptor{Kind: descriptor.KindNil}},
		{Kind: descriptor.GeneralConstantCompositeZero, TypeDescriptor: descriptor.TypeDescriptor{Kind: descriptor.KindNamed, PackagePath: "example.com/host", Name: "Gone"}},
		{Kind: descriptor.GeneralConstantKind(99)},
	} {
		_, err := ReconstructConstant(t.Context(), missing, registry)
		require.Error(t, err, "%+v", missing)
	}
}

func TestANamedDescriptorWithoutAnUnderlyingTypeResolvesByName(t *testing.T) {
	t.Parallel()

	type config struct{ Name string }
	registry := NewSymbolRegistry(SymbolExports{"example.com/host": {"Config": reflect.ValueOf((*config)(nil))}})
	resolved, err := ReflectTypeFor(t.Context(), descriptor.TypeDescriptor{Kind: descriptor.KindNamed, PackagePath: "example.com/host", Name: "Config"}, registry)
	require.NoError(t, err)
	require.Equal(t, reflect.TypeFor[config](), resolved)

	pooled, err := ReflectTypeFor(t.Context(), descriptor.TypeDescriptor{Kind: descriptor.KindNamed, PackagePath: "example.com/script", Name: "Celsius", BasicKind: uint8(reflect.Float64)}, registry)
	require.NoError(t, err)
	require.Equal(t, reflect.Float64, pooled.Kind(), "a script's named basic type resolves to its pool type")

	_, err = ReflectTypeFor(t.Context(), descriptor.TypeDescriptor{Kind: descriptor.KindNamed, PackagePath: "example.com/script", Name: "Unknown"}, registry)
	require.ErrorContains(t, err, "not found")

	for kind := reflect.Bool; kind <= reflect.String; kind++ {
		_, scalar := basicKindForReflectKind(kind)
		switch kind {
		case reflect.Array, reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
			require.False(t, scalar, kind.String())
		default:
			require.True(t, scalar, kind.String())
		}
	}
}
