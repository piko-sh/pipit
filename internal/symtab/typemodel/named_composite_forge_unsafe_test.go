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

//go:build !safe && !(js && wasm)

package typemodel

import (
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
)

func TestForgeLayoutCheckPassesOnThisRelease(t *testing.T) {
	t.Parallel()

	require.NoError(t, forgeLayoutError())
	require.NoError(t, checkForgeLayout(), "the check is repeatable")
}

func TestAPanickingLayoutCheckIsNeverReportedAsSound(t *testing.T) {
	t.Parallel()

	calls := 0
	verdict := sync.OnceValue(func() error {
		calls++
		panic("descriptor mirror mismatch")
	})
	require.Panics(t, func() { _ = verdict() })
	require.Panics(t, func() { _ = verdict() }, "a later call panics again rather than returning a nil verdict")
	require.Equal(t, 1, calls, "the check itself runs once")
	require.IsType(t, verdict, forgeLayoutError, "forgeLayoutError is the same once-only construction over checkForgeLayout")
	require.Equal(t, forgeLayoutError(), forgeLayoutError(), "the verdict is computed once and reused")
}

func TestForgedDescriptorsMatchTheMirroredLayout(t *testing.T) {
	t.Parallel()

	require.Equal(t, uintptr(48), unsafe.Sizeof(abiTypeHeader{}), "abi.Type")
	require.Equal(t, uintptr(16), unsafe.Sizeof(abiUncommon{}), "abi.UncommonType")
	require.Equal(t, uintptr(56), unsafe.Sizeof(sliceTypeMirror{}), "abi.SliceType")
	require.Equal(t, uintptr(72), unsafe.Sizeof(arrayTypeMirror{}), "abi.ArrayType")
	require.Equal(t, uintptr(64), unsafe.Sizeof(chanTypeMirror{}), "abi.ChanType")
	require.Equal(t, uintptr(136), unsafe.Sizeof(mapTypeMirror{}), "abi.MapType")
	require.Equal(t, uintptr(56), unsafe.Sizeof(funcTypeMirror{}), "abi.FuncType")

	array := (*arrayTypeMirror)(typeDescriptor(reflect.TypeFor[[5]uint32]()))
	require.Equal(t, uintptr(5), array.length)
	require.Equal(t, typeDescriptor(reflect.TypeFor[uint32]()), array.elem)
	require.Equal(t, typeDescriptor(reflect.TypeFor[[]uint32]()), array.slice)

	mapType := (*mapTypeMirror)(typeDescriptor(reflect.TypeFor[map[int16]string]()))
	require.Equal(t, typeDescriptor(reflect.TypeFor[int16]()), mapType.key)
	require.Equal(t, typeDescriptor(reflect.TypeFor[string]()), mapType.elem)
	require.Equal(t, (*abiTypeHeader)(mapType.group).size, mapType.groupSize)
}

func TestForgedFuncTypesKeepTheirParameterAndResultTypes(t *testing.T) {
	t.Parallel()

	underlying := reflect.TypeFor[func(int8, string, ...float32) (bool, error)]()
	named, err := forgeNamedType(underlying, "probe.Signature", "example.com/probe")
	require.NoError(t, err)

	require.Equal(t, underlying.NumIn(), named.NumIn())
	require.Equal(t, underlying.NumOut(), named.NumOut())
	for index := range underlying.NumIn() {
		require.Equal(t, underlying.In(index), named.In(index))
	}
	for index := range underlying.NumOut() {
		require.Equal(t, underlying.Out(index), named.Out(index))
	}
	require.Equal(t, "probe.Signature", named.String())
}

func TestForgedTypesHoldPointersThroughCollections(t *testing.T) {
	t.Parallel()

	sliceType := mustNamedComposite(t, "Boxes", reflect.TypeFor[[]*[64]byte]())
	mapType := mustNamedComposite(t, "BoxIndex", reflect.TypeFor[map[string]*[64]byte]())
	values := make([]reflect.Value, 0, 64)
	for round := range 64 {
		slice := reflect.MakeSlice(sliceType, 0, 0)
		index := reflect.MakeMap(mapType)
		for element := range 32 {
			box := new([64]byte)
			box[0] = byte(round)
			box[63] = byte(element)
			slice = reflect.Append(slice, reflect.ValueOf(box))
			index.SetMapIndex(reflect.ValueOf(fmt.Sprint(element)), reflect.ValueOf(box))
		}
		values = append(values, slice, index)
		runtime.GC()
	}
	for round := range 64 {
		slice, index := values[2*round], values[2*round+1]
		for element := range 32 {
			box, ok := reflect.TypeAssert[*[64]byte](slice.Index(element))
			require.True(t, ok)
			require.Equal(t, [2]byte{byte(round), byte(element)}, [2]byte{box[0], box[63]})
			require.Same(t, box, index.MapIndex(reflect.ValueOf(fmt.Sprint(element))).Interface())
		}
	}
}

func TestRuntimeNamesResolveToTheirText(t *testing.T) {
	t.Parallel()

	long := string(make([]byte, 300))
	named, err := forgeNamedType(reflect.TypeFor[[]int](), "probe.T"+long, "example.com/probe")
	require.NoError(t, err)
	require.Equal(t, "probe.T"+long, named.String(), "a name longer than one varint byte")
}

func TestOnlyTheMirroredGoReleaseMayForge(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"go1.27", "go1.27.1", "go1.27.12"} {
		require.NoError(t, goReleaseError(version), version)
	}
	for _, version := range []string{"go1.28", "go1.270", "go1.26.5", "devel go1.28-abc", ""} {
		require.ErrorContains(t, goReleaseError(version), "mirrored from", version)
	}
	require.NoError(t, checkGoRelease())
	require.NoError(t, checkDescriptorMirrors(), "the read-only mirror check passes before anything is forged")
}

func TestPatchingRefusesAMemberWhoseLayoutDiffers(t *testing.T) {
	t.Parallel()

	slice, err := forgeNamedType(reflect.TypeFor[[]*byte](), "probe.PatchSlice", "example.com/probe")
	require.NoError(t, err)
	require.NoError(t, patchCycleMember(slice, reflect.TypeFor[[]*int]()), "a slice header holds one pointer whatever its element")
	require.ErrorContains(t, patchCycleMember(slice, reflect.TypeFor[map[int]int]()), "is not a slice")
	require.ErrorContains(t, patchCycleMember(slice, nil), "is not a slice")

	array, err := forgeNamedType(reflect.TypeFor[[2]*byte](), "probe.PatchArray", "example.com/probe")
	require.NoError(t, err)
	require.ErrorContains(t, patchCycleMember(array, reflect.TypeFor[[2]uintptr]()), "layout")
	mixed, err := forgeNamedType(reflect.TypeFor[[1]struct {
		first  *byte
		middle uintptr
		last   *byte
	}](), "probe.PatchMixed", "example.com/probe")
	require.NoError(t, err)
	require.ErrorContains(t, patchCycleMember(mixed, reflect.TypeFor[[1]struct {
		first  *byte
		middle *byte
		last   *byte
	}]()), "pointer layout differs")
	require.ErrorContains(t, patchCycleMember(array, reflect.TypeFor[[2]func()]()), "comparability differs")

	function, err := forgeNamedType(reflect.TypeFor[func(*byte) *byte](), "probe.PatchFunc", "example.com/probe")
	require.NoError(t, err)
	require.ErrorContains(t, patchCycleMember(function, reflect.TypeFor[func(*byte, *byte) *byte]()), "parameters and results")
	require.ErrorContains(t, patchCycleParts(reflect.Struct, nil, nil), "cannot be a member")
}

func assertSharesPointerMask(t *testing.T, registry *namedCompositeRegistry, named reflect.Type) {
	t.Helper()
	info, ok := registry.lookup(named)
	require.True(t, ok)
	header := (*abiTypeHeader)(typeDescriptor(named))
	require.NotZero(t, header.tflag&tflagGCMaskOnDemand, "the array is large enough for an on-demand mask")
	require.Equal(t, (*abiTypeHeader)(typeDescriptor(info.Underlying)).gcData, header.gcData)
}

func TestALargeSelfReferentialArrayTakesItsUnderlyingPointerMask(t *testing.T) {
	t.Parallel()
	registry := newForgingRegistry(t, DefaultNamedCompositeProcessLimit)
	const length = 20000
	placeholder, err := CyclePlaceholder(reflect.Array, 0, 0, false, length, reflect.TypeFor[*byte]())
	require.NoError(t, err)
	members := []CycleMember{{Placeholder: placeholder, Identity: cycleIdentity("Big"), Shape: "[20000]*@Big"}}
	built, err := registry.cycleFor(nil, members, func(forged map[string]reflect.Type) ([]reflect.Type, error) {
		return []reflect.Type{reflect.ArrayOf(length, reflect.PointerTo(forged["cycleprobe.Big"]))}, nil
	})
	require.NoError(t, err)
	big := built["cycleprobe.Big"]
	require.Equal(t, big, big.Elem().Elem())
	assertSharesPointerMask(t, registry, big)

	holder := reflect.New(big)
	for index := 0; index < length; index += 997 {
		holder.Elem().Index(index).Set(reflect.New(big))
		holder.Elem().Index(index).Elem().Index(0).Set(holder)
	}
	runtime.GC()
	for index := 0; index < length; index += 997 {
		require.Equal(t, holder.Pointer(), holder.Elem().Index(index).Elem().Index(0).Pointer(), "the collector kept every element")
	}
}
