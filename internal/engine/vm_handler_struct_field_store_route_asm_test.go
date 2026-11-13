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
	"runtime/debug"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type storeRouteSetup struct {
	value       func(vm *VM) reflect.Value
	prepare     func(vm *VM)
	grants      map[int]bool
	name        string
	field       uint8
	cycleBroken bool
	revoked     bool
}

type storeRouteOutcome struct {
	words                      []unsafe.Pointer
	noteSeqDelta, revisionDiff uint64
	untraced, blind            int64
	revoked                    int
	epochRevoked, banksRevoked bool
	flagAfter                  bool
}

func storeRouteFrame(t *testing.T, setup storeRouteSetup, fields ...uint8) (*VM, *CallFrame, *Registers, *generalFields) {
	t.Helper()
	structType := reflect.TypeFor[generalFields]()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	for _, field := range fields {
		builder.body = append(builder.body, isa.NewInstruction(isa.OpSetStructFieldGeneral, 1, 2, field))
	}
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

	layouts := make([]program.StructFieldLayout, 0, structType.NumField())
	typeTable := make([]reflect.Type, 0, structType.NumField()+1)
	typeTable = append(typeTable, structType)
	for index := range structType.NumField() {
		layout := generalFieldLayout(t, index)
		if index == 1 && setup.cycleBroken {
			layout.Flags = program.StructFieldLayoutFlagCycleBroken
		}
		layouts = append(layouts, layout)
		typeTable = append(typeTable, structType.Field(index).Type)
	}
	builder.typeTable = typeTable
	compiledFunction := builder.build()
	compiledFunction.StructLayoutTable = layouts
	compiledFunction.FieldStoreArenaSafePCs = setup.grants

	vm, frame, registers := newFramedVM(t, compiledFunction)
	vm.Arena.licenceState.blindMarked = false
	vm.Arena.attachLicencePolicy(&VMLimits{})
	require.True(t, vm.Arena.licensed, "test harness: the arena is licensed")
	require.True(t, vm.Arena.servesNoNativeCall(), "test harness: the VM serves no native call")
	if setup.revoked {
		noteHandOff(vm.Arena, handOffIdentityKept, reflect.ValueOf(strings.ToUpper))
		noteHandOffString(vm.Arena, handOffUntracedContainer, "blind once the epoch is revoked")
		require.True(t, vm.Arena.licenceState.epochRevoked, "test harness: the epoch is revoked")
	}
	revokedEpochLocalBlind(vm.Arena)
	if setup.prepare != nil {
		setup.prepare(vm)
	}
	receiver := &generalFields{Pointer: &generalLeaf{Count: -1}, Boxed: -1}
	registers.General[1] = reflect.ValueOf(receiver)
	if setup.value != nil {
		registers.General[2] = setup.value(vm)
	}
	return vm, frame, registers, receiver
}

func observeStoreRoute(t *testing.T, setup storeRouteSetup, store func(vm *VM, frame *CallFrame, registers *Registers)) (storeRouteOutcome, bool) {
	t.Helper()
	vm, frame, registers, receiver := storeRouteFrame(t, setup, setup.field)
	state := &vm.Arena.licenceState
	seq, lastNotedSeq, revision := state.noteSeq, state.lastNotedSeq, state.revision
	notes := state.revokedEpochNotes
	store(vm, frame, registers)
	fieldType := reflect.TypeFor[generalFields]().Field(int(setup.field))
	field := reflect.NewAt(fieldType.Type, unsafe.Add(unsafe.Pointer(receiver), fieldType.Offset))
	outcome := storeRouteOutcome{
		words:        append([]unsafe.Pointer(nil), fieldWords(field)...),
		noteSeqDelta: state.noteSeq - seq,
		revisionDiff: state.revision - revision,
		untraced:     state.revokedEpochNotes.byCause[handOffUntracedContainer] - notes.byCause[handOffUntracedContainer],
		blind:        state.revokedEpochNotes.blind - notes.blind,
		revoked:      len(state.revoked),
		epochRevoked: state.epochRevoked,
		banksRevoked: state.banksRevoked,
		flagAfter:    state.revokedLocalBlind,
	}
	byAssembly := state.noteSeq != seq && state.lastNotedSeq == lastNotedSeq
	vm.Arena.Reset()
	return outcome, byAssembly
}

func storeRouteSetups() []storeRouteSetup {
	heapLeaf := &generalLeaf{Count: 41}
	heapValue := func(*VM) reflect.Value { return reflect.ValueOf(heapLeaf) }
	heapNode := &pointerStoreNode{value: 3}
	heapMap := map[string]int{"a": 1}
	return []storeRouteSetup{
		{name: "a pointer into its own type's field", field: 0, revoked: true, value: heapValue},
		{name: "a pointer into a cycle-broken any field", field: 1, cycleBroken: true, revoked: true, value: heapValue},
		{name: "a pointer of another type into a cycle-broken any field", field: 1, cycleBroken: true, revoked: true,
			value: func(*VM) reflect.Value { return reflect.ValueOf(heapNode) }},
		{name: "a pointer in an epoch not revoked", field: 0, value: heapValue},
		{name: "a pointer into an any field not cycle-broken", field: 1, revoked: true, value: heapValue},
		{name: "a pointer once the flag was emptied", field: 0, revoked: true, value: heapValue,
			prepare: func(vm *VM) { vm.Arena.setLicenceVM(vm) }},
		{name: "a pointer once a native call is served", field: 0, revoked: true, value: heapValue,
			prepare: func(vm *VM) {
				vm.Arena.setLicenceVM(&VM{reentrantInterpreterVM: true, debugKind: DebugThreadCallback})
			}},
		{name: "a pointer in a function that grants another store", field: 0, revoked: true, value: heapValue,
			grants: map[int]bool{7: true}},
		{name: "a pointer in a function with an empty grant map", field: 0, revoked: true, value: heapValue,
			grants: map[int]bool{}},
		{name: "a granted pointer store", field: 0, revoked: true, value: heapValue, grants: map[int]bool{0: true}},
		{name: "a nil pointer", field: 0, revoked: true,
			value: func(*VM) reflect.Value { return reflect.ValueOf((*generalLeaf)(nil)) }},
		{name: "a map into a cycle-broken any field", field: 1, cycleBroken: true, revoked: true,
			value: func(*VM) reflect.Value { return reflect.ValueOf(heapMap) }},
		{name: "a pointer held indirectly", field: 0, revoked: true,
			value: func(*VM) reflect.Value { return reflect.ValueOf(&[1]*generalLeaf{heapLeaf}).Elem().Index(0) }},
	}
}

func TestSetStructFieldAssemblyRouteMakesTheGoHandlersDecision(t *testing.T) {
	previous := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previous)
	runtime.GC()
	counting := licenceCounting
	defer SetLicenceCountingForTest(counting)

	routed := map[string]bool{
		"a pointer into its own type's field":                     true,
		"a pointer into a cycle-broken any field":                 true,
		"a pointer of another type into a cycle-broken any field": true,
		"a pointer held indirectly":                               true,
	}
	for _, on := range []bool{true, false} {
		SetLicenceCountingForTest(on)
		for _, setup := range storeRouteSetups() {
			if setup.name == "a nil pointer" && storeCensusBuilt {

				continue
			}
			name := setup.name
			if !on {
				name += ", counters off"
			}
			goOutcome, _ := observeStoreRoute(t, setup, func(vm *VM, frame *CallFrame, registers *Registers) {
				frame.ProgramCounter = 1
				require.Equal(t, opContinue, handleSetStructFieldGeneral(vm, frame, registers, isa.NewInstruction(isa.OpSetStructFieldGeneral, 1, 2, setup.field)), name)
			})
			asmOutcome, byAssembly := observeStoreRoute(t, setup, func(vm *VM, _ *CallFrame, _ *Registers) { runInAssembly(t, vm) })

			require.Equal(t, routed[setup.name] && revokedEpochStoreRouteBuilt, byAssembly, "%s: which handler made the store", name)
			require.Equal(t, goOutcome, asmOutcome, "%s: the assembly makes the Go handler's store and note", name)
			if routed[setup.name] && !storeCensusBuilt {
				require.EqualValues(t, 1, goOutcome.noteSeqDelta, "%s: the store is noted", name)
				require.Equal(t, map[bool]int64{true: 1, false: 0}[on], goOutcome.blind, "%s: counted only while the counters are on", name)
			}
		}
	}
}

func TestReuseHazardAssemblyPointerStoresSurviveReleaseAndReuse(t *testing.T) {
	previous := debug.SetGCPercent(-1)
	defer debug.SetGCPercent(previous)
	runtime.GC()

	setup := storeRouteSetup{name: "hazard", cycleBroken: true, revoked: true, value: func(vm *VM) reflect.Value {
		pointer := allocateIndirectPointee(vm, reflect.TypeFor[generalLeaf](), true, false)
		pointer.Interface().(*generalLeaf).Count = 41
		require.True(t, vm.Arena.ownsPointee(pointer.UnsafePointer()), "test harness: the pointee is arena storage")
		return pointer
	}}
	vm, _, _, kept := storeRouteFrame(t, setup, 0, 1)
	state := &vm.Arena.licenceState
	seq, lastNotedSeq := state.noteSeq, state.lastNotedSeq
	runInAssembly(t, vm)
	require.EqualValues(t, 2, state.noteSeq-seq, "both stores are noted")
	if revokedEpochStoreRouteBuilt {
		require.Equal(t, lastNotedSeq, state.lastNotedSeq, "the assembly made both stores")
	}
	require.Same(t, kept.Pointer, kept.Boxed.(*generalLeaf), "both fields hold the pointee")

	arena := vm.Arena
	for round := range 4 {
		arena.Reset()
		arena.attachLicencePolicy(&VMLimits{})
		for range 64 {
			pointer := allocateIndirectPointee(vm, reflect.TypeFor[generalLeaf](), true, false)
			pointer.Interface().(*generalLeaf).Count = int64(-1 - round)
		}
	}

	require.Equal(t, int64(41), kept.Pointer.Count, "the stored pointee is intact after the arena was reused")
	require.Equal(t, int64(41), kept.Boxed.(*generalLeaf).Count, "the pointee stored into any is intact after the arena was reused")
	arena.Reset()
}
