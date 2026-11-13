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

package engine

import (
	"reflect"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

type tier0General struct {
	Values  []int64
	Boxed   any
	Pointer *tier0Fields
	Text    string
}

type pointerStoreNode struct {
	next  *pointerStoreNode
	value int
}

func TestUnwrapInterfaceLeafPeelsOnlyANonNilInterface(t *testing.T) {
	t.Parallel()

	var boxed any = 42
	interfaceValue := reflect.ValueOf(&boxed).Elem()

	var empty any
	nilInterface := reflect.ValueOf(&empty).Elem()

	tests := []struct {
		field reflect.Value
		check func(t *testing.T, got reflect.Value)
		name  string
	}{
		{
			name:  "a non-nil interface is peeled to its dynamic value",
			field: interfaceValue,
			check: func(t *testing.T, got reflect.Value) {
				t.Helper()
				require.Equal(t, reflect.Int, got.Kind())
			},
		},
		{
			name:  "a nil interface becomes the invalid value",
			field: nilInterface,
			check: func(t *testing.T, got reflect.Value) {
				t.Helper()
				require.False(t, got.IsValid())
			},
		},
		{
			name:  "a concrete value passes through",
			field: reflect.ValueOf("pipit"),
			check: func(t *testing.T, got reflect.Value) {
				t.Helper()
				require.Equal(t, "pipit", got.String())
			},
		},
		{
			name:  "an invalid value stays invalid",
			field: reflect.Value{},
			check: func(t *testing.T, got reflect.Value) {
				t.Helper()
				require.False(t, got.IsValid())
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.check(t, unwrapInterfaceLeaf(tt.field))
		})
	}
}

func TestSnapshotPointerLeafOnlyRewritesAnAddressablePointer(t *testing.T) {
	t.Parallel()

	t.Run("a non-pointer field passes through", func(t *testing.T) {
		t.Parallel()
		field := reflect.ValueOf("pipit")

		require.Equal(t, field, snapshotPointerLeaf(field))
	})

	t.Run("an invalid value passes through", func(t *testing.T) {
		t.Parallel()
		require.False(t, snapshotPointerLeaf(reflect.Value{}).IsValid())
	})

	t.Run("a non-addressable pointer passes through", func(t *testing.T) {
		t.Parallel()
		field := reflect.ValueOf(&tier0Fields{})

		require.Equal(t, field.Pointer(), snapshotPointerLeaf(field).Pointer())
	})

	t.Run("an addressable pointer field keeps pointing at the same object", func(t *testing.T) {
		t.Parallel()
		target := &tier0Fields{Signed: 42}
		holder := &tier0General{Pointer: target}
		field := reflect.ValueOf(holder).Elem().Field(2)

		snapshot := snapshotPointerLeaf(field)

		require.Equal(t, reflect.Pointer, snapshot.Kind())
		require.Equal(t, field.Pointer(), snapshot.Pointer(),
			"the snapshot must address the same object the field did")
	})
}

func TestLayoutStructMatchesReceiverRefusesAnUnknownTypeIndex(t *testing.T) {
	t.Parallel()

	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	compiledFunction := builder.build()

	_, frame, registers := newFramedVM(t, compiledFunction)
	registers.General[1] = reflect.ValueOf(&tier0Fields{})

	layout := tier0Layout(0)
	layout.TypeIndex = 99

	require.False(t, layoutStructMatchesReceiver(frame, registers, 1, layout),
		"a type index past the word table cannot be matched against the receiver")
}

func arenaLeafPointer(t *testing.T, vm *VM) reflect.Value {
	t.Helper()
	pointer := allocateIndirectPointee(vm, reflect.TypeFor[generalLeaf](), true, false)
	pointer.Interface().(*generalLeaf).Count = 41
	require.True(t, vm.Arena.ownsPointee(pointer.UnsafePointer()), "the pointee must be arena storage")
	return pointer
}

func TestSetStructFieldPointerHandsOffThePointee(t *testing.T) {
	runWithLicence(t, func() any { return &generalFields{} }, func(t *testing.T, vm *VM, frame *CallFrame, registers *Registers) {
		receiver := requireAs[*generalFields](t, registers.General[1].Interface())
		value := arenaLeafPointer(t, vm)
		registers.General[2] = value
		require.Equal(t, opContinue, handleSetStructFieldGeneral(vm, frame, registers, op(1, 2, 0)))
		require.Equal(t, int64(41), receiver.Pointer.Count)
		requirePointeeHandedOff(t, vm.Arena, value.UnsafePointer())
	})
}

func TestSetStructFieldInterfaceHandsOffThePointee(t *testing.T) {
	runWithLicence(t, func() any { return &generalFields{} }, func(t *testing.T, vm *VM, frame *CallFrame, registers *Registers) {
		receiver := requireAs[*generalFields](t, registers.General[1].Interface())
		value := arenaLeafPointer(t, vm)
		registers.General[2] = value
		require.Equal(t, opContinue, handleSetStructFieldGeneral(vm, frame, registers, op(1, 2, 1)))
		require.Equal(t, int64(41), receiver.Boxed.(*generalLeaf).Count)
		requirePointeeHandedOff(t, vm.Arena, value.UnsafePointer())
	})
}

func TestSetStructFieldReflectFallbackHandsOffThePointee(t *testing.T) {
	runWithLicence(t, func() any { return &generalFields{} }, func(t *testing.T, vm *VM, frame *CallFrame, registers *Registers) {
		receiver := requireAs[*generalFields](t, registers.General[1].Interface())
		frame.Function.StructLayoutTable[1].FieldTypeIndex = uint16(len(frame.Function.TypeTable))
		value := arenaLeafPointer(t, vm)
		registers.General[2] = value
		require.Equal(t, opContinue, handleSetStructFieldGeneral(vm, frame, registers, op(1, 2, 1)),
			"a field type index past the type table takes the reflect fallback")
		require.Equal(t, int64(41), receiver.Boxed.(*generalLeaf).Count)
		requirePointeeHandedOff(t, vm.Arena, value.UnsafePointer())
	})
}

func TestRevokedEpochFieldRoutesStoreThePointerAndStandAside(t *testing.T) {
	if storeCensusBuilt {
		t.Skip("the revoked-epoch routes stand aside in the census build")
	}
	arena := revokedEpochGoroutineArena(t)
	defer arena.Reset()
	node := &pointerStoreNode{value: 2}
	nodeType := reflect.TypeFor[*pointerStoreNode]()

	var pointerField *pointerStoreNode
	require.True(t, storePointerFieldRevokedEpoch(arena, unsafe.Pointer(&pointerField), nodeType, reflect.ValueOf(node), false))
	require.Same(t, node, pointerField)
	var anyField any
	require.True(t, storePointerInAnyFieldRevokedEpoch(arena, unsafe.Pointer(&anyField), reflect.TypeFor[any](), reflect.ValueOf(node), false))
	require.Same(t, node, anyField.(*pointerStoreNode))

	var copiedPointer *pointerStoreNode
	require.True(t, copyPointerFieldRevokedEpoch(arena, nodeType, unsafe.Pointer(&pointerField), unsafe.Pointer(&copiedPointer)))
	require.Same(t, node, copiedPointer)
	var copiedAny any
	require.True(t, copyPointerFieldRevokedEpoch(arena, reflect.TypeFor[any](), unsafe.Pointer(&anyField), unsafe.Pointer(&copiedAny)))
	require.Same(t, node, copiedAny.(*pointerStoreNode))

	var nilNode *pointerStoreNode
	require.False(t, copyPointerFieldRevokedEpoch(arena, nodeType, unsafe.Pointer(&nilNode), unsafe.Pointer(&copiedPointer)), "a nil pointer")
	var held any = 3
	require.False(t, copyPointerFieldRevokedEpoch(arena, reflect.TypeFor[any](), unsafe.Pointer(&held), unsafe.Pointer(&copiedAny)), "an interface holding no pointer")
	require.False(t, storePointerFieldRevokedEpoch(arena, unsafe.Pointer(&pointerField), nodeType, reflect.ValueOf(nilNode), false), "a nil pointer value")

	fresh := newGoroutineArenaInTree(t, &ResourceTracker{})
	defer fresh.Reset()
	require.False(t, storePointerFieldRevokedEpoch(fresh, unsafe.Pointer(&pointerField), nodeType, reflect.ValueOf(node), false), "an epoch not revoked")
}

func TestPointerStoreWithoutLicenceStoresThePointerAsTheBarrierDoes(t *testing.T) {
	arena := NewRegisterArena()
	require.False(t, arena.licensed)
	require.NotEmpty(t, arena.upvalueCellSlab, "the arena has a cell slab")
	require.NotEmpty(t, arena.intSlab, "the arena has an int slab")

	heapNode := &pointerStoreNode{value: 1}
	heapInt := new(int64)
	sliceLocal := []int{1, 2}
	cases := []struct {
		name  string
		value reflect.Value
	}{
		{"heap struct with pointers", reflect.ValueOf(heapNode)},
		{"heap pointer-free", reflect.ValueOf(heapInt)},
		{"heap slice", reflect.ValueOf(&sliceLocal)},
		{"upvalue cell", reflect.ValueOf(&arena.upvalueCellSlab[0])},
		{"int slab", reflect.ValueOf(&arena.intSlab[0])},
	}
	if len(arena.sliceHeaderSlab) > 0 {
		cases = append(cases, struct {
			name  string
			value reflect.Value
		}{"slice-header slab", reflect.ValueOf((*[]int)(unsafe.Pointer(&arena.sliceHeaderSlab[0])))})
	}
	for _, c := range cases {
		typ, pointer, ok := pointerStoreWithoutLicence(arena, c.value, nil)
		require.True(t, ok, "%s: the route answers", c.name)
		require.Equal(t, reflectValueABIType(c.value.Type()), typ, "%s: the value's type", c.name)
		require.Equal(t, c.value.UnsafePointer(), pointer, "%s: the stored pointer", c.name)
	}
}

func TestPointerStoreWithoutLicenceStandsAside(t *testing.T) {
	node := &pointerStoreNode{}
	nodeType := reflectValueABIType(reflect.TypeFor[*pointerStoreNode]())

	withLicence := NewRegisterArena()
	withLicence.licensed = true
	_, _, ok := pointerStoreWithoutLicence(withLicence, reflect.ValueOf(node), nodeType)
	require.False(t, ok, "licensed arena")

	withChunk := NewRegisterArena()
	withChunk.kref.sorted = append(withChunk.kref.sorted, &krefChunk{})
	_, _, ok = pointerStoreWithoutLicence(withChunk, reflect.ValueOf(node), nodeType)
	require.False(t, ok, "arena holding a KRef chunk")

	arena := NewRegisterArena()
	_, _, ok = pointerStoreWithoutLicence(arena, reflect.ValueOf(new(int)), nodeType)
	require.False(t, ok, "another pointer type")

	holder := struct{ hidden *pointerStoreNode }{hidden: node}
	_, _, ok = pointerStoreWithoutLicence(arena, reflect.ValueOf(holder).Field(0), nil)
	require.False(t, ok, "read-only value")

	_, _, ok = pointerStoreWithoutLicence(arena, reflect.ValueOf((*pointerStoreNode)(nil)), nil)
	require.False(t, ok, "nil pointer")

	_, _, ok = pointerStoreWithoutLicence(arena, reflect.ValueOf(3), nil)
	require.False(t, ok, "not a pointer")
}

func TestStorePointerInAnyFieldWithoutLicenceWritesTheBarriersEface(t *testing.T) {
	arena := NewRegisterArena()
	node := &pointerStoreNode{value: 7}
	var viaRoute, viaBarrier any
	require.True(t, storePointerInAnyFieldWithoutLicence(arena, unsafe.Pointer(&viaRoute), reflect.TypeFor[any](), reflect.ValueOf(node)))
	setStructInterfaceField(nil, reflect.ValueOf(node), unsafe.Pointer(&viaBarrier), reflect.TypeFor[any]())
	require.Equal(t, *(*[2]unsafe.Pointer)(unsafe.Pointer(&viaBarrier)), *(*[2]unsafe.Pointer)(unsafe.Pointer(&viaRoute)))
	require.Same(t, node, viaRoute.(*pointerStoreNode))

	var stringer interface{ String() string }
	require.False(t, storePointerInAnyFieldWithoutLicence(arena, unsafe.Pointer(&stringer), reflect.TypeFor[interface{ String() string }](), reflect.ValueOf(node)))
}

func TestStorePointerInAnySlotWithoutLicenceWritesTheElementAsTheReflectRoute(t *testing.T) {
	arena := NewRegisterArena()
	node := &pointerStoreNode{value: 3}
	viaRoute := make([]any, 2)
	viaReflect := make([]any, 2)
	require.True(t, storePointerInAnySlotWithoutLicence(arena, reflect.ValueOf(viaRoute).Index(1), reflect.ValueOf(node)))
	require.True(t, writeAnyInterfaceSlotFast(reflect.ValueOf(viaReflect).Index(1), reflect.ValueOf(node)))
	require.Equal(t, *(*[2]unsafe.Pointer)(unsafe.Pointer(&viaReflect[1])), *(*[2]unsafe.Pointer)(unsafe.Pointer(&viaRoute[1])))
	require.Same(t, node, viaRoute[1].(*pointerStoreNode))
	require.Nil(t, viaRoute[0])

	cell := reflect.ValueOf(&arena.upvalueCellSlab[0])
	require.True(t, storePointerInAnySlotWithoutLicence(arena, reflect.ValueOf(viaRoute).Index(0), cell))
	require.Equal(t, cell.UnsafePointer(), reflect.ValueOf(viaRoute[0]).UnsafePointer(), "a cell pointer is stored as it is")

	typed := make([]*pointerStoreNode, 1)
	require.False(t, storePointerInAnySlotWithoutLicence(arena, reflect.ValueOf(typed).Index(0), reflect.ValueOf(node)), "an element of another type")
	require.False(t, storePointerInAnySlotWithoutLicence(arena, reflect.ValueOf(viaRoute).Index(0), reflect.ValueOf(7)), "a value that is not a pointer")
}

func storeIndexedElementBarrier(t *testing.T, arena *RegisterArena, target, value reflect.Value) {
	t.Helper()
	value = coerceValue(nil, value, target.Type())
	require.False(t, optimisticStringSlotStore(arena, target, value), "test harness: no string store")
	if !optimisticOwnValueStore(arena, target, value) {
		value = materialiseArenaValueIntoSlot(arena, value, target.Type())
	}
	if !writeAnyInterfaceSlotFast(target, value) {
		target.Set(value)
	}
}

func TestStorePointerInAnySlotRevokedEpochStoresThePointerAndStandsAside(t *testing.T) {
	if storeCensusBuilt {
		t.Skip("the revoked-epoch routes stand aside in the census build")
	}
	arena := revokedEpochGoroutineArena(t)
	defer arena.Reset()
	node := &pointerStoreNode{value: 4}
	elements := make([]any, 2)
	seq := arena.licenceState.noteSeq
	require.True(t, storePointerInAnySlotRevokedEpoch(arena, reflect.ValueOf(elements).Index(1), reflect.ValueOf(node)))
	require.Same(t, node, elements[1].(*pointerStoreNode))
	require.Nil(t, elements[0])
	require.Equal(t, seq+1, arena.licenceState.noteSeq, "the store is noted")

	typed := make([]*pointerStoreNode, 1)
	require.False(t, storePointerInAnySlotRevokedEpoch(arena, reflect.ValueOf(typed).Index(0), reflect.ValueOf(node)), "an element of another type")
	var stringers [1]interface{ String() string }
	require.False(t, storePointerInAnySlotRevokedEpoch(arena, reflect.ValueOf(&stringers).Elem().Index(0), reflect.ValueOf(&strings.Builder{})), "an interface with methods")
	require.False(t, storePointerInAnySlotRevokedEpoch(arena, reflect.ValueOf([1]any{}).Index(0), reflect.ValueOf(node)), "an element without an address")
	require.False(t, storePointerInAnySlotRevokedEpoch(arena, reflect.ValueOf(elements).Index(0), reflect.ValueOf(7)), "a value that is not a pointer")
	require.False(t, storePointerInAnySlotRevokedEpoch(arena, reflect.ValueOf(elements).Index(0), reflect.ValueOf((*pointerStoreNode)(nil))), "a nil pointer")
	holder := struct{ hidden *pointerStoreNode }{hidden: node}
	require.False(t, storePointerInAnySlotRevokedEpoch(arena, reflect.ValueOf(elements).Index(0), reflect.ValueOf(holder).Field(0)), "a read-only value")
	slot := krefTestSlot(t, arena, reflect.TypeFor[[1]any]())
	require.False(t, storePointerInAnySlotRevokedEpoch(arena, slot.Index(0), reflect.ValueOf(node)), "an element in a KRef chunk")
	require.Nil(t, elements[0], "no stand-aside case stored")
	require.Equal(t, seq+1, arena.licenceState.noteSeq, "no stand-aside case noted")

	fresh := newGoroutineArenaInTree(t, &ResourceTracker{})
	defer fresh.Reset()
	require.False(t, storePointerInAnySlotRevokedEpoch(fresh, reflect.ValueOf(elements).Index(0), reflect.ValueOf(node)), "an epoch not revoked")
	require.False(t, storePointerInAnySlotRevokedEpoch(nil, reflect.ValueOf(elements).Index(0), reflect.ValueOf(node)), "no arena")
	require.False(t, storePointerInAnySlotRevokedEpoch(NewRegisterArena(), reflect.ValueOf(elements).Index(0), reflect.ValueOf(node)), "an arena without a licence")
}

func TestRevokedEpochAnySlotRouteIsTheBarriers(t *testing.T) {
	if storeCensusBuilt {
		t.Skip("the revoked-epoch routes stand aside in the census build")
	}
	type outcome struct {
		words                                [2]unsafe.Pointer
		noteSeqDelta, revisionDelta, revoked uint64
		epochRevoked, banksRevoked           bool
		lastNotedSame                        bool
		handOffs, blind, local               int64
	}
	heapNode := &pointerStoreNode{value: 5}
	values := []struct {
		name  string
		value func(a *RegisterArena) reflect.Value
	}{
		{"heap pointer", func(*RegisterArena) reflect.Value { return reflect.ValueOf(heapNode) }},
		{"heap pointer-free pointee", func(*RegisterArena) reflect.Value { return reflect.ValueOf(new(int64)) }},
		{"arena int slab", func(a *RegisterArena) reflect.Value {
			backing := a.AllocIntBacking(1)
			return reflect.ValueOf(&backing[0])
		}},
		{"arena string slot", func(a *RegisterArena) reflect.Value {
			backing := a.AllocStringBacking(1)
			backing[0] = arenaString(a, "kept by identity")
			return reflect.ValueOf(&backing[0])
		}},
	}
	run := func(name string, value func(a *RegisterArena) reflect.Value, store func(a *RegisterArena, target, v reflect.Value)) outcome {
		arena := revokedEpochGoroutineArena(t)
		require.True(t, revokedEpochLocalBlind(arena), "%s: test harness", name)
		v := value(arena)
		elements := make([]any, 1)
		seq, revision := arena.licenceState.noteSeq, arena.licenceState.revision
		handOffs := licenceTotal().handOffs[handOffUntracedContainer].Load()
		blind, local := licenceTotal().blindHandOffs.Load(), blindScopeStatistics().LocalBlindHandOffs
		store(arena, reflect.ValueOf(elements).Index(0), v)
		arena.flushRevokedEpochNotes()
		observed := outcome{
			words:         *(*[2]unsafe.Pointer)(unsafe.Pointer(&elements[0])),
			noteSeqDelta:  arena.licenceState.noteSeq - seq,
			revisionDelta: uint64(arena.licenceState.revision - revision),
			revoked:       uint64(len(arena.licenceState.revoked)),
			epochRevoked:  arena.licenceState.epochRevoked,
			banksRevoked:  arena.licenceState.banksRevoked,
			lastNotedSame: arena.licenceState.lastNoted == licenceNoteKeyOf(v, false) && arena.licenceState.lastNotedSeq == arena.licenceState.noteSeq,
			handOffs:      licenceTotal().handOffs[handOffUntracedContainer].Load() - handOffs,
			blind:         licenceTotal().blindHandOffs.Load() - blind,
			local:         blindScopeStatistics().LocalBlindHandOffs - local,
		}
		require.Equal(t, v.UnsafePointer(), observed.words[1], "%s: the element holds the pointer", name)
		require.Equal(t, reflectValueABIType(v.Type()), observed.words[0], "%s: the element holds the pointer's type", name)
		arena.Reset()
		return observed
	}
	for _, c := range values {
		barrier := run(c.name, c.value, func(a *RegisterArena, target, v reflect.Value) {
			storeIndexedElementBarrier(t, a, target, v)
		})
		route := run(c.name, c.value, func(a *RegisterArena, target, v reflect.Value) {
			require.True(t, storePointerInAnySlotRevokedEpoch(a, target, v), "%s: the route takes the store", c.name)
		})
		barrier.words, route.words = [2]unsafe.Pointer{}, [2]unsafe.Pointer{}
		require.Equal(t, barrier, route, "%s: the route makes the barrier's store and note", c.name)
		require.EqualValues(t, 1, barrier.noteSeqDelta, "%s: the barrier notes the store once", c.name)
	}
}

func TestReuseHazardAnySlotPointerStoredInARevokedEpochSurvivesReleaseAndReuse(t *testing.T) {
	runWithLicence(t, func() any { return &generalFields{} }, func(t *testing.T, vm *VM, frame *CallFrame, registers *Registers) {
		arena := vm.Arena
		noteHandOff(arena, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
		noteHandOffString(arena, handOffUntracedContainer, "blind once the epoch is revoked")
		require.True(t, arena.licenceState.epochRevoked, "test harness: the epoch is revoked")
		require.Equal(t, !storeCensusBuilt, revokedEpochLocalBlind(arena), "test harness: the route's state")

		pointee := allocateIndirectPointee(vm, reflect.TypeFor[generalLeaf](), true, false)
		pointee.Interface().(*generalLeaf).Count = 41
		require.True(t, arena.ownsPointee(pointee.UnsafePointer()), "test harness: the pointee is arena storage")
		kept := make([]any, 2)
		registers.General[3] = reflect.ValueOf(kept)
		registers.Ints[0] = 1
		registers.General[2] = pointee
		seq := arena.licenceState.noteSeq
		require.Equal(t, opContinue, handleSetElement(vm, frame, registers, op(3, 0, 2)))
		require.EqualValues(t, 1, arena.licenceState.noteSeq-seq, "the store is noted")
		require.Same(t, pointee.Interface().(*generalLeaf), kept[1].(*generalLeaf))

		for round := range 4 {
			arena.Reset()
			arena.attachLicencePolicy(&VMLimits{})
			for range 64 {
				reused := allocateIndirectPointee(vm, reflect.TypeFor[generalLeaf](), true, false)
				reused.Interface().(*generalLeaf).Count = int64(-1 - round)
			}
		}

		require.Equal(t, int64(41), kept[1].(*generalLeaf).Count, "the pointee stored into the element is intact after the arena was reused")
		require.Nil(t, kept[0])
		arena.Reset()
	})
}

func TestNilPointerFieldStoresMakeNoHandOff(t *testing.T) {
	if storeCensusBuilt {
		t.Skip("the census build keeps the barrier for nil stores")
	}
	runWithLicence(t, func() any { return &generalFields{Pointer: &generalLeaf{Count: 1}, Boxed: 5} }, func(t *testing.T, vm *VM, frame *CallFrame, registers *Registers) {
		receiver := requireAs[*generalFields](t, registers.General[1].Interface())
		registers.General[2] = reflect.ValueOf((*generalLeaf)(nil))
		before := vm.Arena.licenceState.noteSeq

		require.Equal(t, opContinue, handleSetStructFieldGeneral(vm, frame, registers, op(1, 2, 0)))
		require.Nil(t, receiver.Pointer)
		require.Equal(t, opContinue, handleSetStructFieldGeneral(vm, frame, registers, op(1, 2, 1)))
		require.IsType(t, (*generalLeaf)(nil), receiver.Boxed)
		require.Nil(t, receiver.Boxed.(*generalLeaf))
		require.Equal(t, before, vm.Arena.licenceState.noteSeq, "a nil pointer store makes no note")
	})
}

func TestStoreNilPointerFieldStandsAside(t *testing.T) {
	if storeCensusBuilt {
		t.Skip("the census build keeps the barrier for nil stores")
	}
	leafType := reflect.TypeFor[*generalLeaf]()
	var pointerField *generalLeaf
	var anyField any
	var stringer interface{ String() string }
	holder := struct{ hidden *generalLeaf }{}
	cases := []struct {
		name      string
		kind      reflect.Kind
		field     unsafe.Pointer
		fieldType reflect.Type
		value     reflect.Value
	}{
		{"a non-nil pointer", reflect.Pointer, unsafe.Pointer(&pointerField), leafType, reflect.ValueOf(&generalLeaf{})},
		{"a nil pointer of another type", reflect.Pointer, unsafe.Pointer(&pointerField), leafType, reflect.ValueOf((*int)(nil))},
		{"a read-only nil pointer", reflect.Pointer, unsafe.Pointer(&pointerField), leafType, reflect.ValueOf(holder).Field(0)},
		{"an untyped nil", reflect.Pointer, unsafe.Pointer(&pointerField), leafType, reflect.Value{}},
		{"a value that is not a pointer", reflect.Interface, unsafe.Pointer(&anyField), reflect.TypeFor[any](), reflect.ValueOf(3)},
		{"an interface with methods", reflect.Interface, unsafe.Pointer(&stringer), reflect.TypeFor[interface{ String() string }](), reflect.ValueOf((*generalLeaf)(nil))},
		{"a field of another kind", reflect.Slice, unsafe.Pointer(&pointerField), leafType, reflect.ValueOf((*generalLeaf)(nil))},
	}
	for _, c := range cases {
		require.False(t, storeNilPointerField(c.kind, c.field, c.fieldType, c.value), c.name)
	}
}

func TestStoreNilPointerFieldWritesWhatTheBarrierWrites(t *testing.T) {
	if storeCensusBuilt {
		t.Skip("the census build keeps the barrier for nil stores")
	}
	leafType := reflect.TypeFor[*generalLeaf]()
	anyType := reflect.TypeFor[any]()
	holder := struct{ hidden *generalLeaf }{}
	values := []struct {
		name  string
		value reflect.Value
	}{
		{"a nil pointer", reflect.ValueOf((*generalLeaf)(nil))},
		{"a nil pointer of another type", reflect.ValueOf((*int)(nil))},
		{"a non-nil heap pointer", reflect.ValueOf(&generalLeaf{Count: 3})},
		{"a read-only nil pointer", reflect.ValueOf(holder).Field(0)},
		{"an untyped nil", reflect.Value{}},
		{"an integer", reflect.ValueOf(7)},
	}
	arenas := []struct {
		name  string
		arena func(t *testing.T) *RegisterArena
	}{
		{"a walking epoch", func(t *testing.T) *RegisterArena { t.Helper(); return newArena(t) }},
		{"a callback's walking epoch", func(t *testing.T) *RegisterArena {
			t.Helper()
			return newCallbackArenaInTree(t, &ResourceTracker{})
		}},
		{"a revoked epoch", revokedEpochGoroutineArena},
	}
	taken := 0
	for _, arenaCase := range arenas {
		for _, valueCase := range values {
			for _, fieldType := range []reflect.Type{leafType, anyType} {
				label := arenaCase.name + ", " + valueCase.name + ", " + fieldType.String()
				arena := arenaCase.arena(t)
				routeField := reflect.New(fieldType)
				if !storeNilPointerField(fieldType.Kind(), routeField.UnsafePointer(), fieldType, valueCase.value) {
					arena.Reset()
					continue
				}
				taken++
				state := &arena.licenceState
				walking := !state.epochRevoked
				revision, revoked, banksRevoked := state.revision, len(state.revoked), state.banksRevoked
				blind := arena.blindCounters().all.word.Load()
				barrierField := reflect.New(fieldType)
				require.NotPanics(t, func() {
					barrierField.Elem().Set(MaterialiseArenaValue(arena, coerceValue(nil, valueCase.value, fieldType)))
				}, label)
				require.Equal(t, fieldWords(barrierField), fieldWords(routeField), "%s: the route writes the barrier's words", label)
				if walking {
					require.Equal(t, revision, state.revision, "%s: the barrier's walk revokes nothing", label)
					require.Len(t, state.revoked, revoked, label)
					require.False(t, state.epochRevoked, label)
					require.Equal(t, banksRevoked, state.banksRevoked, label)
					require.Equal(t, blind, arena.blindCounters().all.word.Load(), "%s: and finds nothing it cannot see", label)
				}
				arena.Reset()
			}
		}
	}
	require.Equal(t, 3*len(arenas), taken, "the route takes a nil pointer into its own type's field and into any")
}

func fieldWords(field reflect.Value) []unsafe.Pointer {
	return unsafe.Slice((*unsafe.Pointer)(field.UnsafePointer()), field.Type().Elem().Size()/unsafe.Sizeof(uintptr(0)))
}

func krefSourceCopyFrame(t *testing.T, field uint8) (*VM, *CallFrame, *Registers, *generalFields) {
	t.Helper()
	vm, frame, registers := newGeneralFieldFrame(t, &generalFields{}, op(field, 0, 0))
	vm.Arena.licenceState.blindMarked = false
	vm.Arena.attachLicencePolicy(&VMLimits{})
	require.True(t, vm.Arena.licensed)
	slot := krefTestSlot(t, vm.Arena, reflect.TypeFor[generalFields]())
	source := requireAs[*generalFields](t, slot.Addr().Interface())
	require.True(t, copySourceIsKRefMember(vm.Arena, unsafe.Pointer(source)), "test harness: the source is a KRef member")
	registers.General[1] = slot.Addr()
	return vm, frame, registers, source
}

func TestFusedCopyOfANilWordFromAKRefSourceMakesNoHandOff(t *testing.T) {
	if storeCensusBuilt {
		t.Skip("the census build keeps the barrier for nil copies")
	}
	for _, field := range []uint8{0, 1} {
		vm, frame, registers, source := krefSourceCopyFrame(t, field)
		source.Pointer, source.Boxed = nil, (*generalLeaf)(nil)
		destination := &generalFields{Pointer: &generalLeaf{Count: 1}, Boxed: 4}
		registers.General[2] = reflect.ValueOf(destination)
		before := vm.Arena.licenceState.noteSeq

		require.Equal(t, opContinue, handleCopyStructFieldGeneral(vm, frame, registers, op(1, 2, field)))
		require.Equal(t, before, vm.Arena.licenceState.noteSeq, "field %d: a nil copy makes no note", field)
		if field == 0 {
			require.Nil(t, destination.Pointer)
		} else {
			require.IsType(t, (*generalLeaf)(nil), destination.Boxed)
		}
		vm.Arena.Reset()
	}
}

func TestCopiesNilReferenceReadsOnlyTheDataWord(t *testing.T) {
	if storeCensusBuilt {
		t.Skip("the census build keeps the barrier for nil copies")
	}
	leafType := reflect.TypeFor[*generalLeaf]()
	anyType := reflect.TypeFor[any]()
	var nilLeaf *generalLeaf
	leaf := &generalLeaf{}
	var nilAny any
	var typedNil any = (*generalLeaf)(nil)
	var nilMap any = map[string]int(nil)
	var heldLeaf any = leaf
	var heldInt any = 3
	var nilStringer interface{ String() string }
	var heldSlice []int

	require.True(t, copiesNilReference(leafType, unsafe.Pointer(&nilLeaf)), "a nil pointer")
	require.True(t, copiesNilReference(anyType, unsafe.Pointer(&nilAny)), "a nil interface")
	require.True(t, copiesNilReference(anyType, unsafe.Pointer(&typedNil)), "an interface holding a nil pointer")
	require.True(t, copiesNilReference(anyType, unsafe.Pointer(&nilMap)), "an interface holding a nil map")
	require.False(t, copiesNilReference(leafType, unsafe.Pointer(&leaf)), "a non-nil pointer")
	require.False(t, copiesNilReference(anyType, unsafe.Pointer(&heldLeaf)), "an interface holding a pointer")
	require.False(t, copiesNilReference(anyType, unsafe.Pointer(&heldInt)), "an interface holding a boxed value")
	require.False(t, copiesNilReference(reflect.TypeFor[interface{ String() string }](), unsafe.Pointer(&nilStringer)),
		"an interface with methods keeps the barrier, which may wrap the value")
	require.False(t, copiesNilReference(reflect.TypeFor[[]int](), unsafe.Pointer(&heldSlice)), "a slice")
}

func TestCopiesNilReferenceCopiesWhatTheBarrierCopies(t *testing.T) {
	if storeCensusBuilt {
		t.Skip("the census build keeps the barrier for nil copies")
	}
	sources := []struct {
		name   string
		source generalFields
		field  uint8
	}{
		{"a nil pointer", generalFields{}, 0},
		{"a non-nil pointer", generalFields{Pointer: &generalLeaf{Count: 2}}, 0},
		{"a nil interface", generalFields{}, 1},
		{"a nil pointer in an interface", generalFields{Boxed: (*generalLeaf)(nil)}, 1},
		{"a nil map in an interface", generalFields{Boxed: map[string]int(nil)}, 1},
		{"a pointer in an interface", generalFields{Boxed: &generalLeaf{Count: 3}}, 1},
	}
	taken := 0
	for _, c := range sources {
		vm, frame, registers, source := krefSourceCopyFrame(t, c.field)
		*source = c.source
		fieldType := frame.Function.TypeTable[c.field+1]
		layout := frame.Function.StructLayoutTable[c.field]
		sourceField := unsafe.Add(unsafe.Pointer(source), uintptr(layout.Offset))
		if !copiesNilReference(fieldType, sourceField) {
			vm.Arena.Reset()
			continue
		}
		taken++
		state := &vm.Arena.licenceState
		revision, revoked, epochRevoked, banksRevoked := state.revision, len(state.revoked), state.epochRevoked, state.banksRevoked

		viaRoute := &generalFields{Pointer: &generalLeaf{}, Boxed: 1}
		routeField := unsafe.Add(unsafe.Pointer(viaRoute), uintptr(layout.Offset))
		runtimeTypedmemmove(reflectValueABIType(fieldType), routeField, sourceField)

		viaBarrier := &generalFields{Pointer: &generalLeaf{}, Boxed: 1}
		registers.General[2] = reflect.ValueOf(viaBarrier)
		fetched := unsafeNewAt(reflectValueABIType(fieldType), sourceField, fieldType.Kind())
		setProxy := isa.Instruction{Op: isa.OpDrillTier1, A: 2}
		require.Equal(t, opContinue, setStructFieldGeneralReflectFallback(vm, frame, registers, setProxy, layout, fetched, false), c.name)

		require.Equal(t, fieldWords(reflect.NewAt(fieldType, unsafe.Add(unsafe.Pointer(viaBarrier), uintptr(layout.Offset)))),
			fieldWords(reflect.NewAt(fieldType, routeField)), "%s: the plain copy writes the barrier's words", c.name)
		require.Equal(t, revision, state.revision, "%s: the barrier revokes nothing", c.name)
		require.Len(t, state.revoked, revoked, c.name)
		require.Equal(t, epochRevoked, state.epochRevoked, c.name)
		require.Equal(t, banksRevoked, state.banksRevoked, c.name)
		vm.Arena.Reset()
	}
	require.Equal(t, 4, taken, "the route takes every nil data word")
}

func TestReuseHazardNilFieldStoresSurviveReleaseAndReuse(t *testing.T) {
	kept := &generalFields{Pointer: &generalLeaf{Count: 1}, Boxed: 5, Label: "kept"}
	copied := &generalFields{Pointer: &generalLeaf{Count: 2}, Boxed: 6, Label: "copied"}

	vm, frame, registers, source := krefSourceCopyFrame(t, 0)
	source.Pointer = nil
	registers.General[2] = reflect.ValueOf(copied)
	require.Equal(t, opContinue, handleCopyStructFieldGeneral(vm, frame, registers, op(1, 2, 0)))
	registers.General[1] = reflect.ValueOf(kept)
	registers.General[2] = reflect.ValueOf((*generalLeaf)(nil))
	require.Equal(t, opContinue, handleSetStructFieldGeneral(vm, frame, registers, op(1, 2, 0)))
	require.Equal(t, opContinue, handleSetStructFieldGeneral(vm, frame, registers, op(1, 2, 1)))

	arena := vm.Arena
	for round := range 4 {
		arena.Reset()
		arena.attachLicencePolicy(&VMLimits{})
		for range 64 {
			pointer := allocateIndirectPointee(vm, reflect.TypeFor[generalLeaf](), true, false)
			pointer.Interface().(*generalLeaf).Count = int64(-1 - round)
		}
		slot := krefTestSlot(t, arena, reflect.TypeFor[generalFields]())
		reused := requireAs[*generalFields](t, slot.Addr().Interface())
		reused.Pointer, reused.Boxed, reused.Label = &generalLeaf{Count: -9}, -9, "poison"
	}

	require.Nil(t, kept.Pointer, "the stored nil pointer is still nil after the arena was reused")
	require.IsType(t, (*generalLeaf)(nil), kept.Boxed)
	require.Nil(t, kept.Boxed.(*generalLeaf))
	require.Equal(t, "kept", kept.Label)
	require.Nil(t, copied.Pointer, "the copied nil pointer is still nil after the arena was reused")
	require.Equal(t, 6, copied.Boxed)
	require.Equal(t, "copied", copied.Label)
	arena.Reset()
}
