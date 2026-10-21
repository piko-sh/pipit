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

package typemap

import (
	"context"
	"errors"
	"go/types"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/descriptor"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

func cyclePackage() *types.Package {
	return types.NewPackage("example.com/cycles", "cycles")
}

func declareIn(typesPackage *types.Package, name string) *types.Named {
	return types.NewNamed(types.NewTypeName(0, typesPackage, name, nil), nil, nil)
}

func requireNamedCompositeTypes(t *testing.T) {
	t.Helper()
	if !typemodel.NamedCompositeTypesSupported() {
		t.Skip("this build gives named composite types no runtime types of their own")
	}
}

func TestASelfReferentialNamedSliceGetsARuntimeTypeOfItsOwn(t *testing.T) {
	t.Parallel()
	requireNamedCompositeTypes(t)

	typesPackage := cyclePackage()
	list := declareIn(typesPackage, "List")
	list.SetUnderlying(types.NewSlice(list))
	other := declareIn(typesPackage, "Other")
	other.SetUnderlying(types.NewSlice(other))

	require.True(t, IsNamedCompositeType(list))
	converted := convertOne(t, list)
	require.Equal(t, "cycles.List", converted.String())
	require.Equal(t, converted, converted.Elem(), "type List []List is its own element")
	require.True(t, typemodel.IsNamedCompositeType(converted))
	require.NotEqual(t, converted, convertOne(t, other), "two such types are distinct")
	require.Equal(t, converted, convertOne(t, list), "a second compile gets the same type")
}

func TestMutuallyReferentialNamedTypesAreBuiltTogether(t *testing.T) {
	t.Parallel()
	requireNamedCompositeTypes(t)

	typesPackage := cyclePackage()
	x := declareIn(typesPackage, "X")
	y := declareIn(typesPackage, "Y")
	x.SetUnderlying(types.NewSlice(y))
	y.SetUnderlying(types.NewMap(types.Typ[types.String], x))
	pointer := declareIn(typesPackage, "P")
	pointer.SetUnderlying(types.NewPointer(x))
	function := declareIn(typesPackage, "F")
	function.SetUnderlying(types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(0, typesPackage, "", pointer)), types.NewTuple(types.NewVar(0, typesPackage, "", function)), false))

	cache := NewTypeCache()
	convertedX, err := TypeToReflectCached(t.Context(), x, symtab.NewSymbolRegistry(nil), cache, engine.NewGlobalStore())
	require.NoError(t, err)
	convertedY := cache.Lookup(y)
	require.NotNil(t, convertedY, "the whole group is cached")
	require.Equal(t, convertedY, convertedX.Elem())
	require.Equal(t, convertedX, convertedY.Elem())

	convertedF := convertOne(t, function)
	require.Equal(t, convertedF, convertedF.Out(0))
	require.Equal(t, reflect.PointerTo(convertedX), convertedF.In(0), "a named pointer type is its underlying pointer type")
}

func TestASelfReferenceThroughAStructBecomesAnInterfaceField(t *testing.T) {
	t.Parallel()
	requireNamedCompositeTypes(t)

	typesPackage := cyclePackage()
	list := declareIn(typesPackage, "Nodes")
	list.SetUnderlying(types.NewSlice(types.NewStruct([]*types.Var{types.NewField(0, typesPackage, "Next", list, false)}, nil)))

	converted := convertOne(t, list)
	require.Equal(t, "cycles.Nodes", converted.String())
	require.Equal(t, reflect.Interface, converted.Elem().Field(0).Type.Kind(),
		"a struct field leading back to the type is an interface, as for named structs")
}

func TestASelfReferentialArrayTypeIsBuiltFromItsLayout(t *testing.T) {
	t.Parallel()
	requireNamedCompositeTypes(t)

	typesPackage := cyclePackage()
	array := declareIn(typesPackage, "Ring")
	array.SetUnderlying(types.NewArray(types.NewPointer(array), 2))

	converted := convertOne(t, array)
	require.Equal(t, converted, converted.Elem().Elem())
	require.Equal(t, 2*reflect.TypeFor[*int]().Size(), converted.Size())
	require.True(t, converted.Comparable())
}

func TestLoadedBytecodeResolvesSelfReferentialTypesToTheCompiledOnes(t *testing.T) {
	t.Parallel()
	requireNamedCompositeTypes(t)

	typesPackage := types.NewPackage("example.com/cycleload", "cycleload")
	list := declareIn(typesPackage, "List")
	list.SetUnderlying(types.NewSlice(list))
	x := declareIn(typesPackage, "X")
	y := declareIn(typesPackage, "Y")
	x.SetUnderlying(types.NewMap(types.Typ[types.String], y))
	y.SetUnderlying(types.NewChan(types.RecvOnly, types.NewArray(types.NewPointer(x), 3)))
	function := declareIn(typesPackage, "F")
	function.SetUnderlying(types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(0, typesPackage, "", types.NewSlice(types.Typ[types.Int]))),
		types.NewTuple(types.NewVar(0, typesPackage, "", function)), true))
	holder := declareIn(typesPackage, "Holder")
	holder.SetUnderlying(types.NewSlice(list))

	for _, named := range []*types.Named{list, x, y, function, holder} {
		compiled := convertOne(t, named)
		loaded, err := symtab.ReflectTypeFor(t.Context(), descriptor.ReflectTypeToDescriptor(compiled), nil)
		require.NoError(t, err, named.Obj().Name())
		require.Equal(t, compiled, loaded, "%s resolves to the compiled type", named.Obj().Name())
	}
}

const childConversionEnvironment = "PIPIT_TYPEMAP_CHILD_CONVERSION"

func TestANamedPointerTypeOnACycleConvertsWithoutOverflowingTheStack(t *testing.T) {
	if os.Getenv(childConversionEnvironment) == "1" {
		typesPackage := types.NewPackage("example.com/pointercycle", "pointercycle")
		list := declareIn(typesPackage, "L")
		pointer := declareIn(typesPackage, "P")
		list.SetUnderlying(types.NewSlice(pointer))
		pointer.SetUnderlying(types.NewPointer(types.NewMap(pointer, list)))
		converted := convertOne(t, list)
		require.Equal(t, reflect.Slice, converted.Kind())
		require.Equal(t, reflect.Pointer, converted.Elem().Kind())
		if typemodel.NamedCompositeTypesSupported() {
			require.Equal(t, "pointercycle.L", converted.String())
			require.Equal(t, converted, converted.Elem().Elem().Elem(), "the map's value is L itself")
		}
		return
	}
	t.Parallel()

	executable, err := os.Executable()
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), executable,
		"-test.run=^TestANamedPointerTypeOnACycleConvertsWithoutOverflowingTheStack$", "-test.count=1")
	command.Env = append(os.Environ(), childConversionEnvironment+"=1")
	output, err := command.CombinedOutput()
	require.NoError(t, err, "type L []P; type P *map[P]L must convert in the child:\n%s", output)
	require.NotContains(t, string(output), "stack overflow")
}

func TestAPartReachingAMemberOnlyThroughTheSearchPathStaysInTheGroup(t *testing.T) {
	t.Parallel()
	requireNamedCompositeTypes(t)

	typesPackage := types.NewPackage("example.com/pathcut", "pathcut")
	member := declareIn(typesPackage, "A")
	pointer := declareIn(typesPackage, "X")
	inner := declareIn(typesPackage, "Z")
	member.SetUnderlying(types.NewSlice(pointer))
	pointer.SetUnderlying(types.NewPointer(types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(0, typesPackage, "", inner), types.NewVar(0, typesPackage, "", member)), nil, false)))
	inner.SetUnderlying(types.NewPointer(types.NewSlice(pointer)))

	compiled := convertOne(t, member)
	require.Equal(t, "pathcut.A", compiled.String())
	loaded, err := symtab.ReflectTypeFor(t.Context(), descriptor.ReflectTypeToDescriptor(compiled), nil)
	require.NoError(t, err)
	require.Equal(t, compiled, loaded, "the compiled shape matches the one bytecode gives, so a load finds the same type")
}

func ringOfSlices(count int, closed bool, path string) []*types.Named {
	typesPackage := types.NewPackage(path, "ring")
	ring := make([]*types.Named, count)
	for index := range ring {
		ring[index] = declareIn(typesPackage, "T"+strconv.Itoa(index))
	}
	for index, named := range ring {
		if index+1 < count {
			named.SetUnderlying(types.NewSlice(ring[index+1]))
			continue
		}
		if closed {
			named.SetUnderlying(types.NewSlice(ring[0]))
			continue
		}
		named.SetUnderlying(types.NewSlice(types.Typ[types.Int]))
	}
	return ring
}

func TestFindingGroupsVisitsEachTypeOncePerCompile(t *testing.T) {
	t.Parallel()
	requireNamedCompositeTypes(t)

	const count = 2000
	for _, tt := range []struct {
		name   string
		closed bool
	}{{name: "ring", closed: true}, {name: "chain", closed: false}} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ring := ringOfSlices(count, tt.closed, "example.com/linear/"+tt.name)
			cache := NewTypeCache()
			symbols := symtab.NewSymbolRegistry(nil)
			symbols.SetNamedCompositeQuota(typemodel.NewNamedCompositeQuota(2 * count))
			for _, named := range ring {
				converted, err := TypeToReflectCached(t.Context(), named, symbols, cache, engine.NewGlobalStore())
				require.NoError(t, err)
				require.Equal(t, "ring."+named.Obj().Name(), converted.String())
			}
			require.LessOrEqual(t, cache.cycles.edgeVisits, 2*count, "each type's parts are followed once")
		})
	}
}

func TestAConversionStopsWhenItsContextIsDone(t *testing.T) {
	t.Parallel()

	ring := ringOfSlices(4000, true, "example.com/cancelled")
	ctx, cancel := context.WithCancelCause(t.Context())
	cause := errors.New("compile deadline")
	cancel(cause)
	_, err := TypeToReflectCached(ctx, ring[0], symtab.NewSymbolRegistry(nil), NewTypeCache(), engine.NewGlobalStore())
	require.ErrorIs(t, err, cause)
}

func TestNamedPointerTypesReferringToOneAnotherHitTheStepBudget(t *testing.T) {
	t.Parallel()

	typesPackage := types.NewPackage("example.com/factorial", "factorial")
	const count = 12
	pointers := make([]*types.Named, count)
	for index := range pointers {
		pointers[index] = declareIn(typesPackage, "P"+strconv.Itoa(index))
	}
	for index, named := range pointers {
		parameters := make([]*types.Var, 0, count-1)
		for other, target := range pointers {
			if other != index {
				parameters = append(parameters, types.NewVar(0, typesPackage, "", target))
			}
		}
		named.SetUnderlying(types.NewPointer(types.NewSignatureType(nil, nil, nil, types.NewTuple(parameters...), nil, false)))
	}
	cache := NewTypeCache()
	_, err := TypeToReflectCached(t.Context(), pointers[0], symtab.NewSymbolRegistry(nil), cache, engine.NewGlobalStore())
	require.ErrorIs(t, err, fault.ErrCompileTypeTooComplex, "every path through the pointers is distinct, so the work is factorial")
	_, err = TypeToReflectCached(t.Context(), pointers[1], symtab.NewSymbolRegistry(nil), cache, engine.NewGlobalStore())
	require.ErrorIs(t, err, fault.ErrCompileTypeTooComplex, "the cache returns its failure at once")
}

func TestAQuotaFailureIsReportedAndRemembered(t *testing.T) {
	t.Parallel()
	if !typemodel.NamedCompositeTypesSupported() {
		t.Skip("this build gives named composite types no runtime types of their own")
	}

	typesPackage := types.NewPackage("example.com/quotafail", "quotafail")
	first := declareIn(typesPackage, "First")
	first.SetUnderlying(types.NewSlice(types.Typ[types.Int32]))
	second := declareIn(typesPackage, "Second")
	second.SetUnderlying(types.NewSlice(types.Typ[types.Int32]))
	symbols := symtab.NewSymbolRegistry(nil)
	symbols.SetNamedCompositeQuota(typemodel.NewNamedCompositeQuota(1))
	cache := NewTypeCache()

	_, err := TypeToReflectCached(t.Context(), first, symbols, cache, engine.NewGlobalStore())
	require.NoError(t, err)
	_, err = TypeToReflectCached(t.Context(), second, symbols, cache, engine.NewGlobalStore())
	require.ErrorIs(t, err, typemodel.ErrNamedCompositeQuota)
	require.ErrorIs(t, err, fault.ErrNamedCompositeQuota)
	_, err = TypeToReflectCached(t.Context(), first, symbols, cache, engine.NewGlobalStore())
	require.ErrorIs(t, err, typemodel.ErrNamedCompositeQuota, "a capped compile fails fast")
}

func TestSelfReferentialArraysAreLaidOutThroughEveryKindOfElement(t *testing.T) {
	t.Parallel()
	requireNamedCompositeTypes(t)

	typesPackage := types.NewPackage("example.com/arraylayouts", "arraylayouts")
	nested := declareIn(typesPackage, "Nested")
	nested.SetUnderlying(types.NewArray(types.NewArray(types.NewPointer(nested), 3), 2))
	slices := declareIn(typesPackage, "Slices")
	slices.SetUnderlying(types.NewArray(types.NewSlice(slices), 2))
	maps := declareIn(typesPackage, "Maps")
	maps.SetUnderlying(types.NewArray(types.NewMap(types.Typ[types.String], maps), 2))
	channels := declareIn(typesPackage, "Channels")
	channels.SetUnderlying(types.NewArray(types.NewChan(types.SendOnly, channels), 1))
	functions := declareIn(typesPackage, "Functions")
	functions.SetUnderlying(types.NewArray(types.NewSignatureType(nil, nil, nil,
		types.NewTuple(types.NewVar(0, typesPackage, "", functions)), types.NewTuple(types.NewVar(0, typesPackage, "", functions)), false), 1))
	through := declareIn(typesPackage, "Through")
	pointer := declareIn(typesPackage, "Pointer")
	through.SetUnderlying(types.NewArray(pointer, 2))
	pointer.SetUnderlying(types.NewPointer(types.NewSlice(through)))

	converted := convertOne(t, nested)
	require.Equal(t, converted, converted.Elem().Elem().Elem())
	require.Equal(t, uintptr(6)*reflect.TypeFor[*int]().Size(), converted.Size())
	converted = convertOne(t, slices)
	require.Equal(t, converted, converted.Elem().Elem())
	converted = convertOne(t, maps)
	require.Equal(t, converted, converted.Elem().Elem())
	converted = convertOne(t, channels)
	require.Equal(t, converted, converted.Elem().Elem())
	require.Equal(t, reflect.SendDir, converted.Elem().ChanDir())
	converted = convertOne(t, functions)
	require.Equal(t, converted, converted.Elem().Out(0))
	converted = convertOne(t, through)
	require.Equal(t, converted, converted.Elem().Elem().Elem(), "the named pointer between them is laid out as a pointer")
}

func TestChannelDirectionsMapToReflectsOwn(t *testing.T) {
	t.Parallel()

	require.Equal(t, reflect.SendDir, channelDirection(types.SendOnly))
	require.Equal(t, reflect.RecvDir, channelDirection(types.RecvOnly))
	require.Equal(t, reflect.BothDir, channelDirection(types.SendRecv))
}

func TestASharedNamedCompositeTypeIsCachedUnlessItIsSelfReferential(t *testing.T) {
	t.Parallel()

	typesPackage := types.NewPackage("example.com/shared", "shared")
	plain := declareIn(typesPackage, "Plain")
	plain.SetUnderlying(types.NewSlice(types.Typ[types.Int]))
	cyclic := declareIn(typesPackage, "Cyclic")
	cyclic.SetUnderlying(types.NewSlice(cyclic))
	cache := NewTypeCache()
	state := &typeConverterState{
		symbols: nil, processing: make(map[types.Type]bool), cache: cache.types, globals: nil,
		processingStructs: nil, cycleTypes: nil, cycles: cache.cycles, conversionErr: nil, steps: 0, depth: 0,
	}

	require.Equal(t, reflect.TypeFor[[]int](), convertSharedNamedComposite(t.Context(), state, plain, false))
	require.Equal(t, reflect.TypeFor[[]int](), cache.Lookup(plain))
	require.Equal(t, reflect.TypeFor[[]any](), convertSharedNamedComposite(t.Context(), state, cyclic, true),
		"the recurrence is `any`, as before named composite types had their own types")
	require.Nil(t, cache.Lookup(cyclic), "a self-referential type depends on its path, so it is not cached")

	group := &cycleGroup{state: state, members: nil, component: 0}
	failure := errors.New("group failed")
	require.Equal(t, reflect.TypeFor[any](), group.fail(failure))
	require.ErrorIs(t, state.conversionErr, failure)
	require.Equal(t, reflect.TypeFor[any](), convertType(t.Context(), state, plain), "a failed conversion converts nothing more")
}
