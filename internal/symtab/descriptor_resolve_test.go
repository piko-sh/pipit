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
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"pipit.sh/pipit/internal/symtab/descriptor"
)

func TestDescriptorToReflectTypeRefusesExcessiveNesting(t *testing.T) {
	t.Parallel()
	typeDescriptor := &descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int)}
	for range descriptor.MaxTypeDescriptorDepth + 16 {
		typeDescriptor = &descriptor.TypeDescriptor{Kind: descriptor.KindPtr, Element: typeDescriptor}
	}
	reconstructed, err := ReflectTypeFor(*typeDescriptor, nil)
	require.ErrorIs(t, err, errCorruptTypeDescriptor)
	require.Nil(t, reconstructed)
}

func TestDescriptorToReflectTypeAllowsModerateNesting(t *testing.T) {
	t.Parallel()
	typeDescriptor := &descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int)}
	for range 8 {
		typeDescriptor = &descriptor.TypeDescriptor{Kind: descriptor.KindPtr, Element: typeDescriptor}
	}
	reconstructed, err := ReflectTypeFor(*typeDescriptor, nil)
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
			reconstructed, err := ReflectTypeFor(descriptor.ReflectTypeToDescriptor(want), registry)
			require.NoError(t, err)
			require.Truef(t, reconstructed == want, "round trip produced %v, want %v", reconstructed, want)
		})
	}
}

func TestDescriptorHostNamedTypeFallsBackToShape(t *testing.T) {
	t.Parallel()
	durationDescriptor := descriptor.ReflectTypeToDescriptor(reflect.TypeFor[time.Duration]())
	monthNamedInt64 := descriptor.TypeDescriptor{Kind: descriptor.KindBasic, BasicKind: uint8(reflect.Int64), PackagePath: "time", Name: "Month"}
	headerDescriptor := descriptor.ReflectTypeToDescriptor(reflect.TypeFor[http.Header]())
	readerDescriptor := descriptor.ReflectTypeToDescriptor(reflect.TypeFor[io.Reader]())

	for _, tc := range []struct {
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
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reconstructed, err := ReflectTypeFor(tc.descriptor, tc.registry)
			require.NoError(t, err)
			require.Truef(t, reconstructed == tc.want, "resolved %v, want %v", reconstructed, tc.want)
		})
	}
}

func TestDescriptorHostNamedTypeResolvesThroughAlias(t *testing.T) {
	t.Parallel()
	registry := NewSymbolRegistry(SymbolExports{
		"os": {"FileMode": reflect.ValueOf((*fs.FileMode)(nil))},
	})
	want := reflect.TypeFor[fs.FileMode]()
	reconstructed, err := ReflectTypeFor(descriptor.ReflectTypeToDescriptor(want), registry)
	require.NoError(t, err)
	require.Truef(t, reconstructed == want, "resolved %v, want %v", reconstructed, want)
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
