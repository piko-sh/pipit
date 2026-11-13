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
	"errors"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func selfSlice(t *testing.T, registry *namedCompositeRegistry, name string) reflect.Type {
	t.Helper()
	placeholder, err := CyclePlaceholder(reflect.Slice, 0, 0, false, 0, nil)
	require.NoError(t, err)
	member := CycleMember{Placeholder: placeholder, Identity: cycleIdentity(name), Shape: "[]" + CycleShapeMember("example.com/cycleprobe", name)}
	built, err := registry.cycleFor(nil, []CycleMember{member}, func(forged map[string]reflect.Type) ([]reflect.Type, error) {
		return []reflect.Type{reflect.SliceOf(forged["cycleprobe."+name])}, nil
	})
	require.NoError(t, err)
	return built["cycleprobe."+name]
}

func cycleIdentity(name string) NamedCompositeIdentity {
	return NamedCompositeIdentity{PkgPath: "example.com/cycleprobe", PkgName: "cycleprobe", Name: name, BareName: name}
}

func TestASelfReferentialSliceTypeIsItsOwnElement(t *testing.T) {
	t.Parallel()
	registry := newForgingRegistry(t, DefaultNamedCompositeProcessLimit)

	list := selfSlice(t, registry, "List")

	require.Equal(t, list, list.Elem(), "type List []List")
	require.Equal(t, "cycleprobe.List", list.String())
	require.Equal(t, list, selfSlice(t, registry, "List"), "the same group is built once")
	require.NotEqual(t, list, selfSlice(t, registry, "Other"), "another name is another type")

	value := reflect.MakeSlice(list, 2, 2)
	value.Index(0).Set(reflect.MakeSlice(list, 1, 1))
	runtime.GC()
	require.Equal(t, 1, value.Index(0).Len())
	require.Equal(t, list, value.Index(0).Type())
	info, ok := registry.lookup(list)
	require.True(t, ok)
	require.Equal(t, list, info.Underlying.Elem())
}

func TestMutuallyReferentialTypesAreBuiltAsOneGroup(t *testing.T) {
	t.Parallel()
	registry := newForgingRegistry(t, DefaultNamedCompositeProcessLimit)
	slicePlaceholder, err := CyclePlaceholder(reflect.Slice, 0, 0, false, 0, nil)
	require.NoError(t, err)
	mapPlaceholder, err := CyclePlaceholder(reflect.Map, 0, 0, false, 0, nil)
	require.NoError(t, err)
	members := []CycleMember{
		{Placeholder: mapPlaceholder, Identity: cycleIdentity("Y"), Shape: "map[#string]" + CycleShapeMember("example.com/cycleprobe", "X")},
		{Placeholder: slicePlaceholder, Identity: cycleIdentity("X"), Shape: "[]" + CycleShapeMember("example.com/cycleprobe", "Y")},
	}
	built, err := registry.cycleFor(nil, members, func(forged map[string]reflect.Type) ([]reflect.Type, error) {
		return []reflect.Type{
			reflect.SliceOf(forged["cycleprobe.Y"]),
			reflect.MapOf(reflect.TypeFor[string](), forged["cycleprobe.X"]),
		}, nil
	})
	require.NoError(t, err)

	x, y := built["cycleprobe.X"], built["cycleprobe.Y"]
	require.Equal(t, y, x.Elem(), "type X []Y")
	require.Equal(t, x, y.Elem(), "type Y map[string]X")
	value := reflect.MakeMap(y)
	value.SetMapIndex(reflect.ValueOf("k"), reflect.MakeSlice(x, 3, 3))
	runtime.GC()
	require.Equal(t, 3, value.MapIndex(reflect.ValueOf("k")).Len())
}

func TestSelfReferentialChannelFuncAndArrayTypesWork(t *testing.T) {
	t.Parallel()
	registry := newForgingRegistry(t, DefaultNamedCompositeProcessLimit)
	chanPlaceholder, err := CyclePlaceholder(reflect.Chan, 0, 0, false, 0, nil)
	require.NoError(t, err)
	funcPlaceholder, err := CyclePlaceholder(reflect.Func, 1, 1, false, 0, nil)
	require.NoError(t, err)
	arrayPlaceholder, err := CyclePlaceholder(reflect.Array, 0, 0, false, 2, reflect.TypeFor[*byte]())
	require.NoError(t, err)
	members := []CycleMember{
		{Placeholder: chanPlaceholder, Identity: cycleIdentity("C"), Shape: "chan3 @C"},
		{Placeholder: funcPlaceholder, Identity: cycleIdentity("F"), Shape: "func(#int)(@F)"},
		{Placeholder: arrayPlaceholder, Identity: cycleIdentity("A"), Shape: "[2]*@A"},
	}
	built, err := registry.cycleFor(nil, members, func(forged map[string]reflect.Type) ([]reflect.Type, error) {
		return []reflect.Type{
			reflect.ArrayOf(2, reflect.PointerTo(forged["cycleprobe.A"])),
			reflect.ChanOf(reflect.BothDir, forged["cycleprobe.C"]),
			reflect.FuncOf([]reflect.Type{reflect.TypeFor[int]()}, []reflect.Type{forged["cycleprobe.F"]}, false),
		}, nil
	})
	require.NoError(t, err)

	channel, function, array := built["cycleprobe.C"], built["cycleprobe.F"], built["cycleprobe.A"]
	require.Equal(t, channel, channel.Elem())
	require.Equal(t, function, function.Out(0))
	require.Equal(t, array, array.Elem().Elem())
	c := reflect.MakeChan(channel, 1)
	c.Send(c)
	received, ok := c.Recv()
	require.True(t, ok)
	require.Equal(t, c.Pointer(), received.Pointer())

	a := reflect.New(array)
	a.Elem().Index(0).Set(a)
	other := reflect.New(array)
	require.True(t, a.Elem().Equal(a.Elem()), "the array compares through its element type")
	require.False(t, a.Elem().Equal(other.Elem()))
}

func TestAGroupWhoseLayoutWouldChangeIsRefused(t *testing.T) {
	t.Parallel()
	registry := newForgingRegistry(t, DefaultNamedCompositeProcessLimit)
	wrongLayout, err := CyclePlaceholder(reflect.Array, 0, 0, false, 2, reflect.TypeFor[byte]())
	require.NoError(t, err)
	members := []CycleMember{{Placeholder: wrongLayout, Identity: cycleIdentity("Bad"), Shape: "[2]*@Bad"}}
	_, err = registry.cycleFor(nil, members, func(forged map[string]reflect.Type) ([]reflect.Type, error) {
		return []reflect.Type{reflect.ArrayOf(2, reflect.PointerTo(forged["cycleprobe.Bad"]))}, nil
	})
	require.ErrorIs(t, err, ErrNamedCompositeUnsupported)
	require.Contains(t, err.Error(), "layout")

	_, err = registry.cycleFor(nil, members, func(map[string]reflect.Type) ([]reflect.Type, error) {
		return nil, errors.New("conversion failed")
	})
	require.ErrorContains(t, err, "conversion failed")
}

func TestAGroupPastTheLimitIsRefused(t *testing.T) {
	t.Parallel()
	registry := newForgingRegistry(t, 0)
	placeholder, err := CyclePlaceholder(reflect.Slice, 0, 0, false, 0, nil)
	require.NoError(t, err)
	members := []CycleMember{{Placeholder: placeholder, Identity: cycleIdentity("Full"), Shape: "[]@Full"}}
	_, err = registry.cycleFor(nil, members, func(forged map[string]reflect.Type) ([]reflect.Type, error) {
		return []reflect.Type{reflect.SliceOf(forged["cycleprobe.Full"])}, nil
	})
	require.ErrorIs(t, err, ErrNamedCompositeLimit)
}

func TestTheFirstBuildOfAGroupIsKept(t *testing.T) {
	t.Parallel()
	registry := newForgingRegistry(t, DefaultNamedCompositeProcessLimit)
	placeholder, err := CyclePlaceholder(reflect.Slice, 0, 0, false, 0, nil)
	require.NoError(t, err)
	members := func() []CycleMember {
		return []CycleMember{{Placeholder: placeholder, Identity: cycleIdentity("Race"), Shape: "[]@Race"}}
	}
	var inner reflect.Type
	outer, err := registry.cycleFor(nil, members(), func(forged map[string]reflect.Type) ([]reflect.Type, error) {
		recorded, innerErr := registry.cycleFor(nil, members(), func(innerForged map[string]reflect.Type) ([]reflect.Type, error) {
			return []reflect.Type{reflect.SliceOf(innerForged["cycleprobe.Race"])}, nil
		})
		require.NoError(t, innerErr)
		inner = recorded["cycleprobe.Race"]
		return []reflect.Type{reflect.SliceOf(forged["cycleprobe.Race"])}, nil
	})
	require.NoError(t, err)
	require.Equal(t, inner, outer["cycleprobe.Race"], "a build that finishes second takes the recorded group")
	require.Equal(t, inner, inner.Elem())
}

func TestCycleShapesWriteEachForm(t *testing.T) {
	t.Parallel()

	member := CycleShapeMember("example.com/p", "L")
	require.Equal(t, "@example.com/p.L", member)
	require.Equal(t, "[]"+member, CycleShapeComposite(reflect.Slice, 0, false, []string{member}, 0))
	require.Equal(t, "[4]"+member, CycleShapeComposite(reflect.Array, 4, false, []string{member}, 0))
	require.Equal(t, "*"+member, CycleShapeComposite(reflect.Pointer, 0, false, []string{member}, 0))
	require.Equal(t, "chan1 "+member, CycleShapeComposite(reflect.Chan, int(reflect.RecvDir), false, []string{member}, 0))
	require.Equal(t, "map[k]"+member, CycleShapeComposite(reflect.Map, 0, false, []string{"k", member}, 0))
	require.Equal(t, "func(a,b...)("+member+")", CycleShapeComposite(reflect.Func, 0, true, []string{"a", "b", member}, 2))
	require.True(t, strings.HasPrefix(CycleShapeLeaf(reflect.TypeFor[int]()), "#"))
	require.NotEqual(t, CycleShapeLeaf(reflect.TypeFor[int]()), CycleShapeLeaf(reflect.TypeFor[int8]()))
	require.Equal(t, "#nil", CycleShapeLeaf(nil))
	_, err := CyclePlaceholder(reflect.Struct, 0, 0, false, 0, nil)
	require.ErrorIs(t, err, ErrNamedCompositeUnsupported)
	_, err = CyclePlaceholder(reflect.Array, 0, 0, false, 1, nil)
	require.ErrorIs(t, err, ErrNamedCompositeUnsupported)
}

func TestAFailedGroupStaysChargedSoRepeatedFailuresReachTheCap(t *testing.T) {
	t.Parallel()
	registry := newForgingRegistry(t, 2)
	placeholder, err := CyclePlaceholder(reflect.Slice, 0, 0, false, 0, nil)
	require.NoError(t, err)
	failing := func(map[string]reflect.Type) ([]reflect.Type, error) { return nil, errors.New("conversion failed") }
	quota := NewNamedCompositeQuota(10)

	for range 2 {
		members := []CycleMember{{Placeholder: placeholder, Identity: cycleIdentity("Broken"), Shape: "[]@Broken"}}
		_, err = registry.cycleFor(quota, members, failing)
		require.ErrorContains(t, err, "conversion failed")
	}
	require.Equal(t, 2, registry.forged, "each failed build forged a member that is never freed")
	require.Equal(t, 2, quota.Used(), "and each is charged to the quota")

	members := []CycleMember{{Placeholder: placeholder, Identity: cycleIdentity("Broken"), Shape: "[]@Broken"}}
	_, err = registry.cycleFor(quota, members, failing)
	require.ErrorIs(t, err, ErrNamedCompositeLimit, "a third failure is refused before forging")
	require.Equal(t, 2, registry.forged)
}

func TestAMalformedGroupForgesNothing(t *testing.T) {
	t.Parallel()
	registry := newForgingRegistry(t, DefaultNamedCompositeProcessLimit)
	slicePlaceholder, err := CyclePlaceholder(reflect.Slice, 0, 0, false, 0, nil)
	require.NoError(t, err)
	tests := []struct {
		name    string
		members []CycleMember
	}{
		{name: "empty", members: nil},
		{name: "no placeholder", members: []CycleMember{{Placeholder: nil, Identity: cycleIdentity("A"), Shape: "[]@A"}}},
		{name: "named placeholder", members: []CycleMember{{Placeholder: reflect.TypeFor[time.Duration](), Identity: cycleIdentity("A"), Shape: "[]@A"}}},
		{name: "struct placeholder", members: []CycleMember{{Placeholder: reflect.TypeFor[struct{ X int }](), Identity: cycleIdentity("A"), Shape: "[]@A"}}},
		{name: "no shape", members: []CycleMember{{Placeholder: slicePlaceholder, Identity: cycleIdentity("A"), Shape: ""}}},
		{name: "duplicate", members: []CycleMember{
			{Placeholder: slicePlaceholder, Identity: cycleIdentity("A"), Shape: "[]@A"},
			{Placeholder: slicePlaceholder, Identity: cycleIdentity("A"), Shape: "[]@A"},
		}},
	}
	for _, tt := range tests {
		_, err := registry.cycleFor(nil, tt.members, func(map[string]reflect.Type) ([]reflect.Type, error) {
			require.Fail(t, "a malformed group is never built", tt.name)
			return nil, nil
		})
		require.ErrorIs(t, err, ErrNamedCompositeUnsupported, tt.name)
	}
	require.Zero(t, registry.forged)
}

func TestABuildReturningTheWrongCountIsRefused(t *testing.T) {
	t.Parallel()
	registry := newForgingRegistry(t, DefaultNamedCompositeProcessLimit)
	placeholder, err := CyclePlaceholder(reflect.Slice, 0, 0, false, 0, nil)
	require.NoError(t, err)
	members := []CycleMember{{Placeholder: placeholder, Identity: cycleIdentity("Short"), Shape: "[]@Short"}}
	_, err = registry.cycleFor(nil, members, func(map[string]reflect.Type) ([]reflect.Type, error) { return nil, nil })
	require.ErrorIs(t, err, ErrNamedCompositeUnsupported)
}

func TestAQuotaRefusesAGroupThatWouldPassIt(t *testing.T) {
	t.Parallel()
	registry := newForgingRegistry(t, DefaultNamedCompositeProcessLimit)
	slicePlaceholder, err := CyclePlaceholder(reflect.Slice, 0, 0, false, 0, nil)
	require.NoError(t, err)
	members := []CycleMember{
		{Placeholder: slicePlaceholder, Identity: cycleIdentity("QuotaX"), Shape: "[]@QuotaY"},
		{Placeholder: slicePlaceholder, Identity: cycleIdentity("QuotaY"), Shape: "[]@QuotaX"},
	}
	quota := NewNamedCompositeQuota(1)
	_, err = registry.cycleFor(quota, members, func(map[string]reflect.Type) ([]reflect.Type, error) {
		require.Fail(t, "a group past the quota is never built")
		return nil, nil
	})
	require.ErrorIs(t, err, ErrNamedCompositeQuota)
	require.Zero(t, quota.Used())
	require.Zero(t, registry.forged)
}

func TestCyclePlaceholdersRefuseWhatReflectCannotBuild(t *testing.T) {
	t.Parallel()

	_, err := CyclePlaceholder(reflect.Array, 0, 0, false, -1, reflect.TypeFor[int]())
	require.ErrorIs(t, err, ErrNamedCompositeUnsupported)
	_, err = CyclePlaceholder(reflect.Array, 0, 0, false, 1<<62, reflect.TypeFor[int64]())
	require.ErrorIs(t, err, ErrNamedCompositeUnsupported, "an array too large to address")
	_, err = CyclePlaceholder(reflect.Func, -1, 0, false, 0, nil)
	require.ErrorIs(t, err, ErrNamedCompositeUnsupported)
	_, err = CyclePlaceholder(reflect.Func, 100, 100, false, 0, nil)
	require.ErrorIs(t, err, ErrNamedCompositeUnsupported)
	_, err = CyclePlaceholder(reflect.Func, 0, 1, true, 0, nil)
	require.ErrorIs(t, err, ErrNamedCompositeUnsupported, "a variadic function needs a parameter")
	variadic, err := CyclePlaceholder(reflect.Func, 2, 1, true, 0, nil)
	require.NoError(t, err)
	require.True(t, variadic.IsVariadic())
	require.Equal(t, reflect.Slice, variadic.In(1).Kind())
	array, err := ArrayOfChecked(3, reflect.TypeFor[uint8]())
	require.NoError(t, err)
	require.Equal(t, reflect.TypeFor[[3]uint8](), array)
}
