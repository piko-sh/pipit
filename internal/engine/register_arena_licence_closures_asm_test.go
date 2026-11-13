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

//go:build !safe && !(js && wasm) && (amd64 || arm64)

package engine

import (
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func heapClosureOver(cells ...*program.UpvalueCell) *RuntimeClosure {
	closure := &RuntimeClosure{Function: &program.CompiledFunction{}}
	closure.upvalues = closure.inlineCells[:0]
	for _, cell := range cells {
		if len(closure.upvalues) < inlineUpvalueCells {
			closure.upvalues = append(closure.upvalues, cell)
			continue
		}
		closure.upvalues = append([]*program.UpvalueCell(nil), closure.upvalues...)
		closure.upvalues = append(closure.upvalues, cell)
	}
	return closure
}

func dataSlabStart(t *testing.T, a *RegisterArena, p unsafe.Pointer) uintptr {
	t.Helper()
	base, ok := licenceDataSlabOf(a, uintptr(p))
	require.True(t, ok, "test harness: %p must lie in a data slab of the arena", p)
	return base
}

func TestRuntimeClosureFieldsAreCellsAndCode(t *testing.T) {
	want := map[string]string{
		"inlineCells":  "[4]*program.UpvalueCell",
		"Function":     "*program.CompiledFunction",
		"RootFunction": "*program.CompiledFunction",
		"upvalues":     "[]*program.UpvalueCell",
		"namedType":    "uint32",
	}
	closureType := reflect.TypeFor[RuntimeClosure]()
	got := map[string]string{}
	for field := range closureType.Fields() {
		got[field.Name] = field.Type.String()
	}
	require.Equal(t, want, got, "RuntimeClosure's fields changed: classify the new field for the closure walk (register_arena_licence_closures.go)")
}

func TestUpvalueCellFieldsAreWalked(t *testing.T) {
	walked := map[string]bool{
		"GeneralValue": true, "StringValue": true, "SliceIntValue": true, "SliceFloatValue": true,
		"SliceStringValue": true, "SliceBoolValue": true, "SliceUintValue": true, "SliceByteValue": true,
	}
	derived := map[string]bool{"IndirectTarget": true, "IndirectType": true}
	scalar := map[string]bool{
		"ComplexValue": true, "UintValue": true, "FloatValue": true, "IntValue": true, "BoolValue": true,
		"IndirectKind": true, "IndirectElemKind": true, "Kind": true, "IsIndirect": true, "OriginalKind": true,
		"HandedOff": true, "Maker": true,
	}
	cellType := reflect.TypeFor[program.UpvalueCell]()
	for field := range cellType.Fields() {
		switch {
		case walked[field.Name], derived[field.Name]:
		case scalar[field.Name]:
			require.True(t, typeIsPointerFree(field.Type), "%s is classified as a scalar but holds pointers", field.Name)
		default:
			require.Failf(t, "unclassified UpvalueCell field", "UpvalueCell field %s (%s) is not classified for the cell walk (register_arena_licence_closures.go)", field.Name, field.Type)
		}
	}
	require.Equal(t, len(walked)+len(derived)+len(scalar), cellType.NumField(), "a classified field disappeared from UpvalueCell")
}

func TestCellWalkRevokesWhatEachKindHolds(t *testing.T) {
	cases := []struct {
		name string
		cell func(a *RegisterArena) (*program.UpvalueCell, unsafe.Pointer)
	}{
		{name: "slice-int", cell: func(a *RegisterArena) (*program.UpvalueCell, unsafe.Pointer) {
			backing := a.AllocIntBacking(4)
			return &program.UpvalueCell{Kind: isa.RegisterSliceInt64, SliceIntValue: backing}, unsafe.Pointer(unsafe.SliceData(backing))
		}},
		{name: "slice-string", cell: func(a *RegisterArena) (*program.UpvalueCell, unsafe.Pointer) {
			backing := a.AllocStringBacking(4)
			return &program.UpvalueCell{Kind: isa.RegisterSliceString, SliceStringValue: backing}, unsafe.Pointer(unsafe.SliceData(backing))
		}},
		{name: "string", cell: func(a *RegisterArena) (*program.UpvalueCell, unsafe.Pointer) {
			s := arenaString(a, "captured string")
			return &program.UpvalueCell{Kind: isa.RegisterString, StringValue: s}, unsafe.Pointer(unsafe.StringData(s))
		}},
		{name: "general", cell: func(a *RegisterArena) (*program.UpvalueCell, unsafe.Pointer) {
			backing := a.AllocIntBacking(4)
			return &program.UpvalueCell{Kind: isa.RegisterGeneral, GeneralValue: reflect.ValueOf(backing)}, unsafe.Pointer(unsafe.SliceData(backing))
		}},
		{name: "indirect", cell: func(a *RegisterArena) (*program.UpvalueCell, unsafe.Pointer) {
			backing := a.AllocIntBacking(4)
			return &program.UpvalueCell{Kind: isa.RegisterGeneral, IsIndirect: true, GeneralValue: reflect.ValueOf(&backing[1])}, unsafe.Pointer(&backing[1])
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			arena := newArenaInTree(t, &ResourceTracker{})
			defer arena.Reset()
			cell, target := tc.cell(arena)
			base := dataSlabStart(t, arena, target)
			noteHandOff(arena, handOffIdentityKept, reflect.ValueOf(heapClosureOver(cell)))
			require.False(t, arena.licenceState.epochRevoked, "a closure's hand-off walks its cells instead of revoking the epoch")
			require.True(t, arena.licenceRevokedBase(base), "the slab the cell's value reaches must be revoked")
			require.True(t, cellHandedOff(cell), "the walk marks the cell handed off")
		})
	}
}

func TestOpaqueFunctionStillRevokesTheEpoch(t *testing.T) {
	a := newArenaInTree(t, &ResourceTracker{})
	defer a.Reset()
	noteHandOff(a, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
	require.True(t, a.licenceState.epochRevoked, "a function value the side table does not know stays opaque")
}

func TestClosureWrapperTableNamesOnlyLiveRegisteredWrappers(t *testing.T) {
	arena := newArenaInTree(t, &ResourceTracker{})
	defer arena.Reset()
	vm := &VM{Arena: arena}
	backing := arena.AllocIntBacking(4)
	base := dataSlabStart(t, arena, unsafe.Pointer(unsafe.SliceData(backing)))
	closure := heapClosureOver(&program.UpvalueCell{Kind: isa.RegisterSliceInt64, SliceIntValue: backing})
	wrapper := reflect.MakeFunc(reflect.TypeFor[func() []int64](), func([]reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(closure.upvalues[0].SliceIntValue)}
	})
	registerClosureWrapper(vm, wrapper, closure)
	found, bound, ok := closureWrapperLookup(wrapper)
	require.True(t, ok)
	require.Same(t, closure, found)
	require.Nil(t, bound)
	_, _, ok = closureWrapperLookup(reflect.ValueOf(strings.ToLower))
	require.False(t, ok, "an unregistered native function is not in the table")
	method := reflect.ValueOf(&strings.Builder{}).MethodByName("Len")
	require.Nil(t, funcValueObject(method), "a method value reflect made has no function object of its own")
	var asAny any = wrapper.Interface()
	noteHandOff(arena, handOffIdentityKept, reflect.ValueOf(&asAny).Elem())
	require.False(t, arena.licenceState.epochRevoked, "a registered wrapper held in an interface is walked, not opaque")
	require.True(t, arena.licenceRevokedBase(base))

	receiver := arena.AllocIntBacking(2)
	receiverBase := dataSlabStart(t, arena, unsafe.Pointer(unsafe.SliceData(receiver)))
	held := &wrapperBoundReceiver{receiver: reflect.ValueOf(receiver)}
	methodValue := reflect.MakeFunc(reflect.TypeFor[func() int](), func([]reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(held.receiver.Len())}
	})
	registerBoundMethodWrapper(vm, methodValue, held)
	noteHandOff(arena, handOffIdentityKept, methodValue)
	require.False(t, arena.licenceState.epochRevoked)
	require.True(t, arena.licenceRevokedBase(receiverBase), "a method value's bound receiver is walked")

	object := uintptr(funcValueObject(wrapper))
	wrapper, closure, asAny = reflect.Value{}, nil, nil
	for range 4 {
		runtime.GC()
	}
	shard := closureWrapperShardOf(object)
	shard.mu.Lock()
	entry := shard.entries[object]
	shard.mu.Unlock()
	require.Nil(t, entry.wrapper.Value(), "the table holds no wrapper alive")
	runtime.KeepAlive(methodValue)
}

func TestClosureWalkRevokesATypedSliceCellsBacking(t *testing.T) {
	arena := newArenaInTree(t, &ResourceTracker{})
	defer arena.Reset()
	backing := arena.AllocStringBacking(4)
	base := dataSlabStart(t, arena, unsafe.Pointer(unsafe.SliceData(backing)))
	closure := heapClosureOver(&program.UpvalueCell{Kind: isa.RegisterSliceString, SliceStringValue: backing})
	noteHandOff(arena, handOffIdentityKept, reflect.ValueOf(closure))
	t.Logf("typed-slice cell backing revoked=%v epoch=%v", arena.licenceRevokedBase(base), arena.licenceState.epochRevoked)
	require.True(t, arena.licenceRevokedBase(base), "the backing a typed-slice cell holds must be revoked")
}

func TestClosureChunksAreRevokedAndReusedPerChunk(t *testing.T) {
	arena := newArenaInTree(t, &ResourceTracker{})
	closure := arena.newChunkClosure()
	cell := arena.newChunkCell()
	closureBase, class, ok := arena.closureChunkSlabOf(uintptr(unsafe.Pointer(closure)))
	require.True(t, ok)
	require.True(t, licenceRegisteredPage(arena, uintptr(unsafe.Pointer(closure))), "a new chunk is on a registered page")
	require.True(t, licenceRegisteredPage(arena, uintptr(unsafe.Pointer(cell))))
	arena.revokeAddress(uintptr(unsafe.Pointer(closure)), handOffIdentityKept)
	require.Equal(t, licenceSlabCell, class)
	require.False(t, arena.licenceState.epochRevoked, "a chunk is revoked on its own, as the cell class")
	require.True(t, arena.licenceRevokedBase(closureBase))
	_, _, cellChunked := arena.closureChunkSlabOf(uintptr(unsafe.Pointer(cell)))
	require.True(t, cellChunked)
	arena.Reset()
	_, _, kept := arena.closureChunkSlabOf(uintptr(unsafe.Pointer(closure)))
	require.False(t, kept, "a revoked chunk is detached at release")
	_, _, cellKept := arena.closureChunkSlabOf(uintptr(unsafe.Pointer(cell)))
	require.True(t, cellKept, "a clean chunk stays for reuse")
	require.True(t, reflect.ValueOf(cell).Elem().IsZero(), "a reused chunk is cleared")
}

func TestGoStatementNamesTheClosureChunkItHandsOver(t *testing.T) {
	arena := newArenaInTree(t, &ResourceTracker{})
	defer arena.Reset()
	closure := arena.newChunkClosure()
	closure.Function = &program.CompiledFunction{}
	base, _, ok := arena.closureChunkSlabOf(uintptr(unsafe.Pointer(closure)))
	require.True(t, ok)
	noteGoroutineSpawn(arena, reflect.ValueOf(closure), nil, false)
	t.Logf("spawned closure chunk revoked=%v", arena.licenceRevokedBase(base))
	require.True(t, arena.licenceRevokedBase(base), "a go statement names the closure object it hands over")
}

func TestCallSiteCacheMatchesAClosureByItsCode(t *testing.T) {
	cached, other := &program.CompiledFunction{}, &program.CompiledFunction{}
	site := &program.CallSite{CachedClosureCallee: cached}
	require.True(t, cachedClosureMatches(site, &RuntimeClosure{Function: cached}))
	matched := cachedClosureMatches(site, &RuntimeClosure{Function: other})
	t.Logf("another closure at the cached slot matched=%v", matched)
	require.False(t, matched, "a closure at a reused slot running other code must miss the cache")
}

func TestClosureChunksSkipProgramsWithAGoStatement(t *testing.T) {
	quiet := &program.CompiledFunction{Body: []isa.Instruction{{Op: isa.OpMoveGeneral}}}
	quiet.Functions = []*program.CompiledFunction{{Body: []isa.Instruction{{Op: isa.OpMoveGeneral}}}}
	require.False(t, programSpawns(quiet))
	spawning := &program.CompiledFunction{Body: []isa.Instruction{{Op: isa.OpMoveGeneral}}}
	spawning.Functions = []*program.CompiledFunction{{Body: []isa.Instruction{{Op: isa.OpGo}}}}
	require.True(t, programSpawns(spawning), "a program with a go statement is not placed (its first closure would share cells with goroutines)")
	require.True(t, programSpawns(nil))
	arena := newArenaInTree(t, &ResourceTracker{})
	defer arena.Reset()
	require.True(t, closurePlacing(&VM{Arena: arena, rootFunction: quiet}))
	require.False(t, closurePlacing(&VM{Arena: arena, rootFunction: spawning}))
	require.False(t, closurePlacing(&VM{Arena: arena, rootFunction: quiet, hasGoroutines: true}))
	require.False(t, closurePlacing(&VM{Arena: arena, rootFunction: quiet, reentrantInterpreterVM: true}))
}
