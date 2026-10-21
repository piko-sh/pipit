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
	"go/types"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func probeIdentity(name string) NamedCompositeIdentity {
	return NamedCompositeIdentity{PkgPath: "example.com/probe", PkgName: "probe", Name: name, BareName: name}
}

func mustNamedComposite(t *testing.T, name string, underlying reflect.Type) reflect.Type {
	t.Helper()
	requireForging(t)
	named, err := NamedCompositeTypeFor(probeIdentity(name), underlying)
	require.NoError(t, err)
	return named
}

func TestNamedCompositeTypeForGivesEachNamedTypeItsOwnType(t *testing.T) {
	t.Parallel()

	ints := reflect.TypeFor[[]int]()
	celsius := mustNamedComposite(t, "Celsius", ints)
	fahrenheit := mustNamedComposite(t, "Fahrenheit", ints)

	require.NotEqual(t, celsius, fahrenheit, "two named types over one underlying type")
	require.NotEqual(t, ints, celsius, "a named type and its underlying type")
	require.Equal(t, celsius, mustNamedComposite(t, "Celsius", ints), "the same identity twice")
	require.NotEqual(t, celsius, mustNamedComposite(t, "Celsius", reflect.TypeFor[[]int8]()),
		"one name declared over two underlying types")
}

func TestNamedCompositeTypesNameThemselvesAsGoDoes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		underlying reflect.Type
		name       string
	}{
		{name: "Points", underlying: reflect.TypeFor[[]float64]()},
		{name: "Grid", underlying: reflect.TypeFor[[3][3]int]()},
		{name: "Index", underlying: reflect.TypeFor[map[string][]int]()},
		{name: "Queue", underlying: reflect.TypeFor[chan<- string]()},
		{name: "Handler", underlying: reflect.TypeFor[func(string, ...int) error]()},
		{name: "Stack[int]", underlying: reflect.TypeFor[[]int]()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			named := mustNamedComposite(t, tt.name, tt.underlying)

			require.Equal(t, tt.name, named.Name())
			require.Equal(t, "probe."+tt.name, named.String())
			require.Equal(t, "example.com/probe", named.PkgPath())
			require.Equal(t, tt.underlying.Kind(), named.Kind())
			require.Equal(t, tt.underlying.Size(), named.Size())
			require.Zero(t, named.NumMethod())
			require.True(t, named.ConvertibleTo(tt.underlying))
			require.True(t, tt.underlying.ConvertibleTo(named))
			require.Equal(t, "*probe."+tt.name, reflect.PointerTo(named).String())
			require.Equal(t, "[]probe."+tt.name, reflect.SliceOf(named).String())
		})
	}
}

func TestNamedSliceValuesShareTheirElementsWithTheUnderlyingType(t *testing.T) {
	t.Parallel()

	named := mustNamedComposite(t, "Shared", reflect.TypeFor[[]int]())
	value := reflect.MakeSlice(named, 3, 3)
	plain, ok := reflect.TypeAssert[[]int](value.Convert(reflect.TypeFor[[]int]()))
	require.True(t, ok)
	plain[1] = 42

	require.Equal(t, int64(42), value.Index(1).Int(), "the conversion copies no elements")
	appended := reflect.Append(value, reflect.ValueOf(7))
	require.Equal(t, named, appended.Type(), "append keeps the named type")
	_, isPlain := reflect.TypeAssert[[]int](reflect.ValueOf(appended.Interface()))
	require.False(t, isPlain, "a named slice does not assert to its underlying type")
}

func TestNamedArrayValuesCompareByType(t *testing.T) {
	t.Parallel()

	named := mustNamedComposite(t, "Pair", reflect.TypeFor[[2]int]())
	value := reflect.New(named).Elem()
	value.Index(0).SetInt(1)
	plain := [2]int{1, 0}

	require.NotEqual(t, any(plain), value.Interface(), "interface equality compares dynamic types")
	keys := map[any]int{value.Interface(): 1, plain: 2}
	require.Len(t, keys, 2)
	require.Equal(t, 1, keys[value.Interface()])
	require.Equal(t, value.Interface(), reflect.ValueOf(plain).Convert(named).Interface())
}

func TestNamedMapAndChannelValuesWork(t *testing.T) {
	t.Parallel()

	mapType := mustNamedComposite(t, "Ages", reflect.TypeFor[map[string]int]())
	ages := reflect.MakeMap(mapType)
	for index := range 100 {
		ages.SetMapIndex(reflect.ValueOf(string(rune('a'+index%26))+string(rune('a'+index/26))), reflect.ValueOf(index))
	}
	require.Equal(t, 100, ages.Len())
	require.Equal(t, int64(27), ages.MapIndex(reflect.ValueOf("bb")).Int())

	chanType := mustNamedComposite(t, "Lane", reflect.TypeFor[chan int]())
	lane := reflect.MakeChan(chanType, 1)
	lane.Send(reflect.ValueOf(9))
	received, ok := lane.Recv()
	require.True(t, ok)
	require.Equal(t, int64(9), received.Int())
}

func TestNamedFuncValuesCall(t *testing.T) {
	t.Parallel()

	underlying := reflect.TypeFor[func(string, ...int) int]()
	named := mustNamedComposite(t, "Counter", underlying)
	require.True(t, named.IsVariadic())
	require.Equal(t, underlying.In(1), named.In(1))

	function := reflect.MakeFunc(named, func(arguments []reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(len(arguments[0].String()) + arguments[1].Len())}
	})
	results := function.Call([]reflect.Value{reflect.ValueOf("abc"), reflect.ValueOf(1), reflect.ValueOf(2)})
	require.Equal(t, int64(5), results[0].Int())
	_, isPlain := reflect.TypeAssert[func(string, ...int) int](reflect.ValueOf(function.Interface()))
	require.False(t, isPlain)
}

func TestNamedCompositeIDsNameTheirTypes(t *testing.T) {
	t.Parallel()

	named := mustNamedComposite(t, "Identified", reflect.TypeFor[func()]())
	id := NamedCompositeID(named)

	require.NotZero(t, id)
	require.Equal(t, named, NamedCompositeTypeByID(id))
	require.Nil(t, NamedCompositeTypeByID(0))
	require.Zero(t, NamedCompositeID(reflect.TypeFor[func()]()))
	require.Nil(t, NamedCompositeTypeByID(^uint32(0)))
}

func TestNamedCompositeRegistryRefusesPastItsLimit(t *testing.T) {
	t.Parallel()

	registry := newForgingRegistry(t, 1)
	underlying := reflect.TypeFor[[]string]()
	_, err := registry.typeFor(nil, probeIdentity("First"), underlying)
	require.NoError(t, err)
	_, err = registry.typeFor(nil, probeIdentity("First"), underlying)
	require.NoError(t, err, "a known identity needs no new entry")

	_, err = registry.typeFor(nil, probeIdentity("Second"), underlying)
	require.ErrorIs(t, err, ErrNamedCompositeLimit)
}

func requireForging(t *testing.T) {
	t.Helper()
	if !NamedCompositeTypesSupported() {
		t.Skip("this build gives named composite types no runtime types of their own")
	}
}

func newForgingRegistry(t *testing.T, limit int) *namedCompositeRegistry {
	t.Helper()
	requireForging(t)
	return newNamedCompositeRegistry(limit)
}

func TestNamedCompositeTypeForRefusesUnderlyingTypesItCannotName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		underlying reflect.Type
		name       string
	}{
		{name: "named", underlying: reflect.TypeFor[time.Duration]()},
		{name: "struct", underlying: reflect.TypeFor[struct{ X int }]()},
		{name: "pointer", underlying: reflect.TypeFor[*int]()},
		{name: "missing", underlying: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NamedCompositeTypeFor(probeIdentity("Refused"+tt.name), tt.underlying)
			require.ErrorIs(t, err, ErrNamedCompositeUnsupported)
		})
	}
}

func TestLookupNamedTypeInfoCoversPoolAndCompositeTypes(t *testing.T) {
	t.Parallel()

	composite := mustNamedComposite(t, "Looked", reflect.TypeFor[[]byte]())
	info, ok := LookupNamedTypeInfo(composite)
	require.True(t, ok)
	require.Equal(t, NamedTypeInfo{QualifiedName: "probe.Looked", Name: "Looked", BareName: "Looked", PkgName: "probe", PkgPath: "example.com/probe"}, info)

	pool := NamedScalarPoolTypeFor(t.Context(), newNamedBasicType("LookedScalar", types.Int))
	require.NotNil(t, pool)
	info, ok = LookupNamedTypeInfo(pool)
	require.True(t, ok)
	require.Equal(t, "LookedScalar", info.BareName)

	_, ok = LookupNamedTypeInfo(reflect.TypeFor[[]byte]())
	require.False(t, ok)
	require.Equal(t, "probe.Looked", NamedTypeDisplayType(composite))
}

func TestAQuotaChargesOnlyTheTypesItsInterpreterAdds(t *testing.T) {
	t.Parallel()
	registry := newForgingRegistry(t, DefaultNamedCompositeProcessLimit)
	underlying := reflect.TypeFor[[]uint64]()
	first := NewNamedCompositeQuota(1)

	shared, err := registry.typeFor(first, probeIdentity("QuotaShared"), underlying)
	require.NoError(t, err)
	_, err = registry.typeFor(first, probeIdentity("QuotaShared"), underlying)
	require.NoError(t, err, "a type already built costs nothing")
	require.Equal(t, 1, first.Used())
	_, err = registry.typeFor(first, probeIdentity("QuotaSecond"), underlying)
	require.ErrorIs(t, err, ErrNamedCompositeQuota)
	require.Equal(t, 1, first.Used(), "a refused type is not charged")

	second := NewNamedCompositeQuota(1)
	again, err := registry.typeFor(second, probeIdentity("QuotaShared"), underlying)
	require.NoError(t, err)
	require.Equal(t, shared, again, "an identical declaration from another interpreter shares the entry")
	require.Zero(t, second.Used())
	require.Equal(t, 1, registry.forged)
}

func TestQuotasDefaultAndReportTheirLimits(t *testing.T) {
	t.Parallel()

	require.Equal(t, DefaultNamedCompositeQuota, NewNamedCompositeQuota(0).Limit())
	require.Equal(t, 7, NewNamedCompositeQuota(7).Limit())
	var none *NamedCompositeQuota
	require.Zero(t, none.Limit())
	require.Zero(t, none.Used())
	require.NoError(t, none.reserve(1<<20, "unbounded"))
}

func TestQuotaTypeForAndCycleForUseTheProcessRegistry(t *testing.T) {
	t.Parallel()
	requireForging(t)
	quota := NewNamedCompositeQuota(4)

	named, err := quota.TypeFor(probeIdentity("ThroughQuota"), reflect.TypeFor[[]int16]())
	require.NoError(t, err)
	require.True(t, IsNamedCompositeType(named))
	placeholder, err := CyclePlaceholder(reflect.Slice, 0, 0, false, 0, nil)
	require.NoError(t, err)
	members := []CycleMember{{Placeholder: placeholder, Identity: probeIdentity("ThroughQuotaList"), Shape: "[]@example.com/probe.ThroughQuotaList"}}
	built, err := quota.CycleFor(members, func(forged map[string]reflect.Type) ([]reflect.Type, error) {
		return []reflect.Type{reflect.SliceOf(forged["probe.ThroughQuotaList"])}, nil
	})
	require.NoError(t, err)
	list := built["probe.ThroughQuotaList"]
	require.Equal(t, list, list.Elem())
	require.LessOrEqual(t, quota.Used(), 2)
}

func TestTheProcessLimitCanBeReplaced(t *testing.T) {
	t.Parallel()

	registry := newNamedCompositeRegistry(3)
	require.Equal(t, 3, registry.setLimit(5))
	require.Equal(t, 5, registry.limit)

	previous := SetNamedCompositeProcessLimit(0)
	require.Equal(t, DefaultNamedCompositeProcessLimit, SetNamedCompositeProcessLimit(previous),
		"zero restores the default")
}

func TestTheProcessRegistryAnswersForTheTypesItBuilt(t *testing.T) {
	t.Parallel()
	requireForging(t)

	identity := probeIdentity("ProcessLooked")
	require.Equal(t, "probe.ProcessLooked", identity.Qualified())
	named := mustNamedComposite(t, "ProcessLooked", reflect.TypeFor[map[string]uint]())
	info, ok := LookupNamedCompositeInfo(named)
	require.True(t, ok)
	require.Equal(t, reflect.TypeFor[map[string]uint](), info.Underlying)
	_, ok = LookupNamedCompositeInfo(reflect.TypeFor[map[string]uint]())
	require.False(t, ok)
	_, ok = LookupNamedCompositeInfo(nil)
	require.False(t, ok)

	placeholder, err := CyclePlaceholder(reflect.Slice, 0, 0, false, 0, nil)
	require.NoError(t, err)
	members := []CycleMember{{Placeholder: placeholder, Identity: probeIdentity("ProcessList"), Shape: "[]@example.com/probe.ProcessList"}}
	built, err := NamedCompositeCycleFor(members, func(forged map[string]reflect.Type) ([]reflect.Type, error) {
		return []reflect.Type{reflect.SliceOf(forged["probe.ProcessList"])}, nil
	})
	require.NoError(t, err)
	require.True(t, IsNamedCompositeType(built["probe.ProcessList"]))
}
