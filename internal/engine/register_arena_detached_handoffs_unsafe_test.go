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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func keepingNative(text string, value, other any, numbers []int64) {}

func TestNativeMayKeepArgumentsFollowsTheBorrowClass(t *testing.T) {
	arena := NewRegisterArena()
	require.False(t, nativeMayKeepArguments(arena, reflect.ValueOf(strings.Contains)), "strings.Contains borrows its arguments")
	require.True(t, nativeMayKeepArguments(arena, reflect.ValueOf(keepingNative)), "an unknown native may keep")
}

func TestDetachNativeStringArgumentsDetachesArenaArguments(t *testing.T) {
	arena := NewRegisterArena()
	heapText := "heap text"
	numbers := arena.AllocIntBacking(4)
	registers := &Registers{
		Strings:   []string{arenaString(arena, "arena text")},
		General:   []reflect.Value{reflect.ValueOf(keepingNative), reflect.ValueOf(arenaString(arena, "boxed text")), reflect.ValueOf(heapText)},
		SlicesInt: [][]int64{numbers},
	}
	site := &program.CallSite{IsNative: true, Arguments: []program.VarLocation{
		{Register: 0, Kind: isa.RegisterString},
		{Register: 1, Kind: isa.RegisterGeneral},
		{Register: 2, Kind: isa.RegisterGeneral},
		{Register: 0, Kind: isa.RegisterSliceInt64},
		{Register: 9, Kind: isa.RegisterString},
		{Register: 9, Kind: isa.RegisterGeneral},
	}}
	originalString, originalGeneral := registers.Strings[0], registers.General[1]
	vm := newTestVM(t)
	vm.Arena = arena

	restore := detachArgumentsReusedWithoutLicence(vm, registers, site)
	require.NotNil(t, restore)
	requireClone(t, originalString, registers.Strings[0], "an arena string argument is cloned")
	requireClone(t, originalGeneral.String(), registers.General[1].String(), "an arena string in a general register is cloned")
	require.Equal(t, heapText, registers.General[2].String(), "a heap string stays")
	require.True(t, arena.unpooled, "a typed slice backed by the arena keeps the arena from the pool")
	require.Equal(t, 1, restore.nStrings)
	require.Equal(t, 1, restore.nGeneral)

	registers.General[1] = reflect.ValueOf("a result written over the argument")
	restore.undo(registers)
	require.Same(t, unsafe.StringData(originalString), unsafe.StringData(registers.Strings[0]), "the guest's register holds its own string again")
	require.Equal(t, "a result written over the argument", registers.General[1].String(), "a register the call rewrote is left alone")
	require.Zero(t, restore.nStrings)
}

func TestDetachNativeStringArgumentsLeavesBorrowedArgumentsAlone(t *testing.T) {
	arena := NewRegisterArena()
	registers := &Registers{Strings: []string{arenaString(arena, "lent")}}
	site := &program.CallSite{Arguments: []program.VarLocation{{Register: 0, Kind: isa.RegisterString}}}
	vm := newTestVM(t)
	require.Nil(t, detachNativeStringArguments(arena, registers, site, reflect.ValueOf(strings.Contains), vm))
	require.Nil(t, detachNativeStringArguments(arena, registers, site, reflect.ValueOf(3), vm), "not a function")
	registers.Strings[0] = "heap"
	require.Nil(t, detachNativeStringArguments(arena, registers, site, reflect.ValueOf(keepingNative), vm),
		"a heap-only call needs no record")
}

func TestDetachNativeArgumentsRecordsAtMostItsSlots(t *testing.T) {
	arena := NewRegisterArena()
	registers := &Registers{}
	site := &program.CallSite{}
	for index := range nativeArgumentRestoreSlots + 2 {
		registers.Strings = append(registers.Strings, arenaString(arena, "argument"))
		registers.General = append(registers.General, reflect.ValueOf(arenaString(arena, "general")))
		site.Arguments = append(site.Arguments,
			program.VarLocation{Register: uint8(index), Kind: isa.RegisterString},
			program.VarLocation{Register: uint8(index), Kind: isa.RegisterGeneral})
	}
	restore := detachNativeStringArguments(arena, registers, site, reflect.ValueOf(keepingNative), newTestVM(t))
	require.Equal(t, nativeArgumentRestoreSlots, restore.nStrings, "later strings keep their clones")
	require.Equal(t, nativeArgumentRestoreSlots, restore.nGeneral)
}

func TestDetachNativeArgumentCopiesWhatLiesInTheArena(t *testing.T) {
	type label string
	arena := NewRegisterArena()

	require.False(t, detachNativeArgument(arena, reflect.Value{}).IsValid())
	heap := reflect.ValueOf("heap")
	require.Equal(t, heap, detachNativeArgument(arena, heap))

	named := reflect.ValueOf(label(arenaString(arena, "named")))
	detached := detachNativeArgument(arena, named)
	require.Equal(t, reflect.TypeFor[label](), detached.Type(), "the clone keeps the argument's type")
	requireClone(t, named.String(), detached.String(), "an arena string is cloned")

	require.False(t, arena.unpooled)
	box := arena.allocIntBox(7)
	require.Equal(t, box, detachNativeArgument(arena, reflect.ValueOf(box)).Interface(), "a pointer keeps its identity")
	require.True(t, arena.unpooled, "so the arena it points into is not reused")
}

func TestDetachNativeMethodStringArgumentsDetachesForAKeepingMethod(t *testing.T) {
	arena := NewRegisterArena()
	text := arenaString(arena, "method argument")
	arguments := []reflect.Value{reflect.ValueOf(text)}
	detachNativeMethodStringArguments(arena, reflect.ValueOf(keepingNative), arguments)
	requireClone(t, text, arguments[0].String(), "a method that may keep its argument receives a clone")

	lent := arenaString(arena, "lent")
	arguments = []reflect.Value{reflect.ValueOf(lent)}
	detachNativeMethodStringArguments(arena, reflect.ValueOf(strings.Contains), arguments)
	require.Same(t, unsafe.StringData(lent), unsafe.StringData(arguments[0].String()), "a borrowing method is lent the string")
	detachNativeMethodStringArguments(arena, reflect.Value{}, arguments)
}

func TestCellReferencesArenaChecksEachKind(t *testing.T) {
	arena := NewRegisterArena()
	budget := func() *int { return new(nativeArgumentProbeBudget) }
	cases := []struct {
		cell *program.UpvalueCell
		name string
		want bool
	}{
		{name: "an arena string", cell: &program.UpvalueCell{Kind: isa.RegisterString, StringValue: arenaString(arena, "captured")}, want: true},
		{name: "a heap string", cell: &program.UpvalueCell{Kind: isa.RegisterString, StringValue: "heap"}, want: false},
		{name: "an arena pointer", cell: &program.UpvalueCell{Kind: isa.RegisterGeneral, GeneralValue: reflect.ValueOf(arena.allocIntBox(1))}, want: true},
		{name: "an indirect cell holding a heap pointer", cell: &program.UpvalueCell{IsIndirect: true, GeneralValue: reflect.ValueOf(new(int))}, want: false},
		{name: "an empty general cell", cell: &program.UpvalueCell{Kind: isa.RegisterGeneral}, want: false},
		{name: "arena ints", cell: &program.UpvalueCell{Kind: isa.RegisterSliceInt64, SliceIntValue: arena.AllocIntBacking(2)}, want: true},
		{name: "arena floats", cell: &program.UpvalueCell{Kind: isa.RegisterSliceFloat64, SliceFloatValue: arena.AllocFloatBacking(2)}, want: true},
		{name: "arena strings", cell: &program.UpvalueCell{Kind: isa.RegisterSliceString, SliceStringValue: arena.AllocStringBacking(2)}, want: true},
		{name: "heap strings", cell: &program.UpvalueCell{Kind: isa.RegisterSliceString, SliceStringValue: []string{"heap"}}, want: false},
		{name: "arena bools", cell: &program.UpvalueCell{Kind: isa.RegisterSliceBool, SliceBoolValue: arena.AllocBoolBacking(2)}, want: true},
		{name: "arena uints", cell: &program.UpvalueCell{Kind: isa.RegisterSliceUint64, SliceUintValue: arena.AllocUintBacking(2)}, want: true},
		{name: "arena bytes", cell: &program.UpvalueCell{Kind: isa.RegisterSliceByte, SliceByteValue: arena.AllocByteBacking(2)}, want: true},
		{name: "an integer", cell: &program.UpvalueCell{Kind: isa.RegisterInt64, IntValue: 4}, want: false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			require.Equal(t, testCase.want, cellReferencesArena(arena, testCase.cell, nativeArgumentProbeDepth, budget()))
		})
	}
}

func TestAbandonIfArgumentReferencesArenaProbesClosures(t *testing.T) {
	arena := NewRegisterArena()
	heapOnly := &RuntimeClosure{upvalues: []*program.UpvalueCell{nil, {Kind: isa.RegisterString, StringValue: "heap"}}}
	abandonIfArgumentReferencesArena(arena, reflect.ValueOf(heapOnly))
	require.False(t, arena.abandonAtRelease, "a closure over heap values leaves the arena pooled")

	inner := &RuntimeClosure{upvalues: []*program.UpvalueCell{{Kind: isa.RegisterString, StringValue: arenaString(arena, "inner")}}}
	outer := &RuntimeClosure{upvalues: []*program.UpvalueCell{{Kind: isa.RegisterGeneral, GeneralValue: reflect.ValueOf(inner)}}}
	abandonIfArgumentReferencesArena(arena, reflect.ValueOf(outer))
	require.True(t, arena.abandonAtRelease, "a closure reaching arena storage through a nested closure abandons the arena at release")
	require.False(t, arena.unpooled)

	abandonIfArgumentReferencesArena(arena, reflect.ValueOf(keepingNative))
	abandonIfArgumentReferencesArena(arena, reflect.ValueOf("text"))
	require.False(t, arena.unpooled, "functions and strings are not probed")
}

func TestClosureReferencesArenaAnswersYesWhenItCannotFinish(t *testing.T) {
	arena := NewRegisterArena()
	closure := &RuntimeClosure{}
	budget := nativeArgumentProbeBudget
	require.False(t, closureReferencesArena(arena, nil, 1, &budget))
	require.True(t, closureReferencesArena(arena, closure, 0, &budget), "out of depth")
	spent := 0
	require.True(t, closureReferencesArena(arena, closure, 1, &spent), "out of budget")
	require.False(t, closureReferencesArena(arena, closure, 1, &budget), "a closure with no cells")
	require.False(t, generalReferencesArena(arena, reflect.Value{}, 1, &budget))
}

func TestTypedSliceBackingReadsEachBank(t *testing.T) {
	registers := &Registers{
		SlicesInt:    [][]int64{make([]int64, 1)},
		slicesFloat:  [][]float64{make([]float64, 1)},
		slicesString: [][]string{make([]string, 1)},
		slicesBool:   [][]bool{make([]bool, 1)},
		slicesUint:   [][]uint64{make([]uint64, 1)},
		slicesByte:   [][]byte{nil, make([]byte, 1)},
	}
	for _, kind := range []isa.RegisterKind{isa.RegisterSliceInt64, isa.RegisterSliceFloat64, isa.RegisterSliceString, isa.RegisterSliceBool, isa.RegisterSliceUint64} {
		backing, ok := typedSliceBacking(registers, kind, 0)
		require.True(t, ok, "kind %d", kind)
		require.NotNil(t, backing)
	}
	_, ok := typedSliceBacking(registers, isa.RegisterSliceByte, 0)
	require.False(t, ok, "a slice with no capacity has no backing")
	_, ok = typedSliceBacking(registers, isa.RegisterSliceByte, 1)
	require.True(t, ok)
	_, ok = typedSliceBacking(registers, isa.RegisterSliceInt64, 5)
	require.False(t, ok, "a register out of range")
	_, ok = typedSliceBacking(registers, isa.RegisterInt64, 0)
	require.False(t, ok, "not a typed-slice bank")
}
