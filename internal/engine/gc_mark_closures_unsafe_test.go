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
	"slices"
	"testing"
	"weak"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type markedClosureHolder struct {
	Name string
	Run  func() int
	Held any
}

func newLicensedMarkVM(t *testing.T) *VM {
	t.Helper()
	vm := newGCTestVM()
	vm.Arena.attachLicencePolicy(&VMLimits{})
	setArenaBudget(t, vm.Arena, 1<<30)
	return vm
}

func retiredPair(t *testing.T, arena *RegisterArena) (*gcTracePair, int) {
	t.Helper()
	pairType := reflect.TypeFor[gcTracePair]()
	pointer, index := retireGenericBytesAllocation(t, arena, pairType.Size(), uintptr(pairType.Align()))
	return (*gcTracePair)(pointer), index
}

func generalCell(value any) *program.UpvalueCell {
	return &program.UpvalueCell{Kind: isa.RegisterGeneral, GeneralValue: reflect.ValueOf(value)}
}

func registeredWrapper(closure *RuntimeClosure) reflect.Value {

	wrapper := reflect.MakeFunc(reflect.TypeFor[func() int](), func([]reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(len(closure.upvalues))}
	})
	closureWrapperRegister(wrapper, closureWrapperEntry{closure: weak.Make(closure), wrapper: weak.Pointer[byte]{}, bound: weak.Pointer[wrapperBoundReceiver]{}})
	return wrapper
}

func TestLicenceMarkTracesAClosureByItsCells(t *testing.T) {
	vm := newLicensedMarkVM(t)
	pair, index := retiredPair(t, vm.Arena)
	function := &program.CompiledFunction{DebugEmitHook: func(int) {}}
	closure := &RuntimeClosure{Function: function, RootFunction: function}
	closure.upvalues = []*program.UpvalueCell{generalCell(pair)}

	state := markOf(t, vm, reflect.ValueOf(any(closure)))
	defer releaseGCMarkState(state)
	require.Zero(t, state.failClosed, "a closure is traced through its cells, not its program-lifetime code")
	require.True(t, state.OldGenericBytesLive[index], "the value a cell holds keeps its chunk live")
}

func TestLicenceMarkTracesARegisteredWrappersClosure(t *testing.T) {
	vm := newLicensedMarkVM(t)
	pair, index := retiredPair(t, vm.Arena)
	closure := &RuntimeClosure{}
	closure.upvalues = []*program.UpvalueCell{generalCell(pair)}
	wrapper := registeredWrapper(closure)
	holder := &markedClosureHolder{Name: "hook", Run: requireAs[func() int](t, wrapper.Interface())}

	state := markOf(t, vm, reflect.ValueOf(holder))
	defer releaseGCMarkState(state)
	require.Zero(t, state.failClosed, "a registered wrapper is traced through its closure")
	require.True(t, state.OldGenericBytesLive[index], "the value its closure captured keeps its chunk live")
}

func TestLicenceMarkTracesABoundMethodWrappersReceiver(t *testing.T) {
	vm := newLicensedMarkVM(t)
	pair, index := retiredPair(t, vm.Arena)
	bound := &wrapperBoundReceiver{receiver: reflect.ValueOf(pair)}
	wrapper := reflect.MakeFunc(reflect.TypeFor[func() int](), func([]reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(int(bound.receiver.Elem().Field(0).Int()))}
	})
	closureWrapperRegister(wrapper, closureWrapperEntry{bound: weak.Make(bound), wrapper: weak.Pointer[byte]{}, closure: weak.Pointer[RuntimeClosure]{}})

	state := markOf(t, vm, wrapper)
	defer releaseGCMarkState(state)
	require.Zero(t, state.failClosed)
	require.True(t, state.OldGenericBytesLive[index], "the bound receiver keeps its chunk live")
}

func TestLicenceMarkTracesAClosureThatCapturesItself(t *testing.T) {
	vm := newLicensedMarkVM(t)
	pair, index := retiredPair(t, vm.Arena)
	closure := &RuntimeClosure{}
	wrapper := registeredWrapper(closure)
	closure.upvalues = []*program.UpvalueCell{generalCell(requireAs[func() int](t, wrapper.Interface())), generalCell(closure), generalCell(pair)}

	state := markOf(t, vm, wrapper)
	defer releaseGCMarkState(state)
	require.Zero(t, state.failClosed, "a closure reached again through its own cells is traced once")
	require.True(t, state.OldGenericBytesLive[index])
}

func TestLicenceMarkTracesEveryKindACellHolds(t *testing.T) {
	vm := newLicensedMarkVM(t)
	arena := vm.Arena
	text := arenaString(arena, "held only by a captured string")
	arena.GrowByteSlab(InitialByteSlabSize)
	ints := arena.AllocIntBacking(4)
	arena.GrowIntBackingSlab(1)
	pair, index := retiredPair(t, arena)
	box := &pair
	closure := &RuntimeClosure{}
	closure.upvalues = []*program.UpvalueCell{
		{Kind: isa.RegisterString, StringValue: text},
		{Kind: isa.RegisterSliceInt64, SliceIntValue: ints},
		{Kind: isa.RegisterGeneral, IsIndirect: true, GeneralValue: reflect.ValueOf(box)},
	}

	state := markOf(t, vm, registeredWrapper(closure))
	defer releaseGCMarkState(state)
	require.Zero(t, state.failClosed)
	require.True(t, state.OldByteSlabLive[0], "a captured string keeps its byte slab live")
	require.True(t, state.OldIntBackingLive[0], "a captured int slice keeps its backing live")
	require.True(t, state.OldGenericBytesLive[index], "an indirect cell's shared variable is traced")
}

func TestLicenceMarkVouchesOnlyForKnownFunctionValues(t *testing.T) {
	captured := 3
	hostConstant := func() int { return captured }
	hostOther := func() int { return captured + 1 }
	unregistered := reflect.MakeFunc(reflect.TypeFor[func() int](), func([]reflect.Value) []reflect.Value {
		return []reflect.Value{reflect.ValueOf(captured)}
	})
	callee := &program.CompiledFunction{GeneralConstants: []reflect.Value{reflect.ValueOf(hostConstant), unregistered}}
	root := &program.CompiledFunction{Functions: []*program.CompiledFunction{callee}}
	for _, testCase := range []struct {
		name       string
		value      reflect.Value
		failClosed bool
	}{
		{"a host closure among the program's constants", reflect.ValueOf(hostConstant), false},
		{"a host closure that is not a constant", reflect.ValueOf(hostOther), true},
		{"an unregistered reflect.MakeFunc result, even as a constant", unregistered, true},
		{"a registered wrapper", registeredWrapper(&RuntimeClosure{}), false},
	} {
		vm := newLicensedMarkVM(t)
		vm.rootFunction = root
		state := markOf(t, vm, testCase.value)
		if testCase.failClosed {
			require.Equal(t, failClosedFunction, state.failClosed, testCase.name)
		} else {
			require.Zero(t, state.failClosed, testCase.name)
		}
		releaseGCMarkState(state)
	}
}

func TestProgramConstantFunctionsAreCollectedOncePerRoot(t *testing.T) {
	host := func() int { return 1 }
	root := &program.CompiledFunction{GeneralConstants: []reflect.Value{reflect.ValueOf(host)}}
	first := programConstantFunctions.of(root)
	require.Len(t, first, 1)
	root.GeneralConstants = nil
	require.Len(t, programConstantFunctions.of(root), 1, "a root's set is collected once")
	require.False(t, programConstantFunction(nil, reflect.ValueOf(host)), "no program, no constant")
}

func TestReuseHazardValueCapturedByAWrapperClosureSurvivesCompactionAndReuse(t *testing.T) {
	observations := &hazardObservations{}
	vm := newLicensedMarkVM(t)
	arena := vm.Arena
	arena.recycleChunks = true
	for len(arena.genericBytesSlab) < genericBytesSlabChunk {
		arena.growGenericBytesSlab(1)
	}
	pairType := reflect.TypeFor[gcTracePair]()
	captured := (*gcTracePair)(arena.AllocBytes(pairType.Size(), uintptr(pairType.Align())))
	captured.A, captured.B = 41, 42
	capturedChunk := slabBase(arena.genericBytesSlab)
	arena.growGenericBytesSlab(1)
	deadChunk := slabBase(arena.genericBytesSlab)
	arena.growGenericBytesSlab(1)

	closure := &RuntimeClosure{}
	closure.upvalues = []*program.UpvalueCell{generalCell(captured)}
	holder := &markedClosureHolder{Name: "hook", Run: requireAs[func() int](t, registeredWrapper(closure).Interface())}
	vm.CallStack = []CallFrame{{Registers: Registers{General: []reflect.Value{reflect.ValueOf(holder)}}}}
	vm.FramePointer = 0
	arena.MinorGC(vm)

	retained := basesOf(arena.oldGenericByteSlabs)
	require.NotContains(t, retained, deadChunk, "precondition: the cycle compacted, dropping the dead chunk")
	if !slices.Contains(retained, capturedChunk) {
		observations.fire("dropped-captured-chunk", "the chunk %#x a wrapper's closure reaches was compacted away", capturedChunk)
	}
	free := basesOf(arena.freeGenericByteChunks)
	require.True(t, arenaAuditEnabled || len(free) > 0, "precondition: the cycle recycled the dead chunk (the audit build abandons it instead)")
	for _, base := range free {
		if base == capturedChunk {
			observations.fire("recycled-captured-chunk", "the chunk %#x a wrapper's closure reaches reached the free list", base)
		}
	}
	for range len(free) {
		arena.growGenericBytesSlab(1)
		poison := arena.genericBytesSlab[:cap(arena.genericBytesSlab)]
		for index := range poison {
			poison[index] = 0xa5
		}
	}
	if captured.A != 41 || captured.B != 42 {
		observations.fire("guest-value-changed", "the captured pair reads %d, %d after reuse", captured.A, captured.B)
	}
	assertNoHazard(t, observations, "")
}
