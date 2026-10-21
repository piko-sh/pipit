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
	"fmt"
	"math"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type globalRouteOutcome struct {
	general reflect.Value
	failure string
	text    string
	integer int64
	float   float64
	uint    uint64
	complex complex128
	boolean bool
}

func runGlobalRoute(t *testing.T, globals *GlobalStore, bases *program.SlotAllocation, slot uint8, kind isa.RegisterKind, assembly bool) (outcome globalRouteOutcome) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	instruction := isa.NewInstruction(isa.OpGetGlobal, 1, slot, uint8(kind))
	builder.body = append(builder.body, instruction)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	function := builder.build()
	function.GlobalBases = bases
	vm, frame, registers := newFramedVM(t, function)
	vm.Globals = globals
	registers.Ints[1], registers.Uints[1], registers.Floats[1] = -7, 7, -7.5
	defer func() {
		if recovered := recover(); recovered != nil {
			outcome.failure = fmt.Sprint("panic: ", recovered)
		}
		outcome.integer, outcome.uint, outcome.float = registers.Ints[1], registers.Uints[1], registers.Floats[1]
		outcome.boolean, outcome.text, outcome.general = registers.Bools[1], registers.Strings[1], registers.General[1]
		outcome.complex = registers.Complex[1]
		if outcome.failure == "" && vm.evalError != nil {
			outcome.failure = fmt.Sprint("error: ", vm.evalError)
		}
	}()
	if assembly {
		runInAssembly(t, vm)
		return outcome
	}
	handleGetGlobal(vm, frame, registers, instruction)
	return outcome
}

func TestGetGlobalRouteReadsAsTheGoHandlerDoes(t *testing.T) {
	kinds := []isa.RegisterKind{isa.RegisterInt64, isa.RegisterFloat64, isa.RegisterString, isa.RegisterGeneral, isa.RegisterBool, isa.RegisterUint64, isa.RegisterComplex128}
	for _, shared := range []bool{false, true} {
		for _, bases := range []*program.SlotAllocation{nil, {1, 2, 0, 1, 2, 1, 1}} {
			for _, kind := range kinds {
				for slot := range uint8(5) {
					label := fmt.Sprintf("shared %v, bases %v, kind %v, slot %d", shared, bases, kind, slot)
					globals := globalRouteStore()
					globals.shared.Store(shared)
					want := runGlobalRoute(t, globals, bases, slot, kind, false)
					got := runGlobalRoute(t, globals, bases, slot, kind, true)
					require.Equal(t, want.failure, got.failure, label)
					require.Equal(t, fmt.Sprint(want.general), fmt.Sprint(got.general), label)
					want.general, got.general = reflect.Value{}, reflect.Value{}
					require.Equal(t, want, got, label)
				}
			}
		}
	}
}

func runWideGlobalRoute(t *testing.T, globals *GlobalStore, bases *program.SlotAllocation, slot int, kind isa.RegisterKind, assembly bool) (outcome globalRouteOutcome, programCounter int) {
	t.Helper()
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	instruction := isa.NewTier1Instruction(isa.SubOpGetGlobalWide, 1, uint8(kind))
	builder.body = append(builder.body, instruction, isa.NewInstruction(isa.OpExtension, uint8(slot&0xFF), uint8(slot>>8), 0),
		isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, 5, 11))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	function := builder.build()
	function.GlobalBases = bases
	vm, frame, registers := newFramedVM(t, function)
	vm.Globals = globals
	registers.Ints[1], registers.Uints[1], registers.Floats[1] = -7, 7, -7.5
	defer func() {
		if recovered := recover(); recovered != nil {
			outcome.failure = fmt.Sprint("panic: ", recovered)
		}
		outcome.integer, outcome.uint, outcome.float = registers.Ints[1], registers.Uints[1], registers.Floats[1]
		outcome.boolean, outcome.text, outcome.general = registers.Bools[1], registers.Strings[1], registers.General[1]
		outcome.complex = registers.Complex[1]
		programCounter = int(registers.Ints[5])
		if outcome.failure == "" && vm.evalError != nil {
			outcome.failure = fmt.Sprint("error: ", vm.evalError)
		}
	}()
	if assembly {
		runInAssembly(t, vm)
		return outcome, programCounter
	}
	frame.ProgramCounter = 1
	if handleGetGlobalWide(vm, frame, registers, instruction) == opContinue && frame.ProgramCounter == 2 {
		registers.Ints[5] = 11
	}
	return outcome, programCounter
}

func TestGetGlobalWideRouteReadsAsTheGoHandlerDoes(t *testing.T) {
	spec, known := isa.SpecAt(isa.TierSub1, uint8(isa.SubOpGetGlobalWide))
	require.True(t, known)
	require.Equal(t, isa.DispatchAsm, spec.Dispatch(), "the wide read has an assembly body")

	kinds := []isa.RegisterKind{isa.RegisterInt64, isa.RegisterFloat64, isa.RegisterString, isa.RegisterGeneral, isa.RegisterBool, isa.RegisterUint64, isa.RegisterComplex128}
	read := map[isa.RegisterKind]int{}
	for _, shared := range []bool{false, true} {
		for _, bases := range []*program.SlotAllocation{nil, {1, 2, 0, 1, 2, 1, 1}} {
			for _, kind := range kinds {
				for _, slot := range []int{0, 2, 3, 255, 256, 298, 299, 300, 0xFFFF} {
					label := fmt.Sprintf("shared %v, bases %v, kind %v, slot %d", shared, bases, kind, slot)
					globals := wideGlobalRouteStore()
					globals.shared.Store(shared)
					want, wantCounter := runWideGlobalRoute(t, globals, bases, slot, kind, false)
					got, gotCounter := runWideGlobalRoute(t, globals, bases, slot, kind, true)
					require.Equal(t, want.failure, got.failure, label)
					require.Equal(t, fmt.Sprint(want.general), fmt.Sprint(got.general), label)
					want.general, got.general = reflect.Value{}, reflect.Value{}
					require.Equal(t, want, got, label)
					if want.failure == "" {
						require.Equal(t, wantCounter, gotCounter, "%s: the extension word is stepped over exactly once", label)
						read[kind]++
					}
				}
			}
		}
	}
	for _, kind := range kinds {
		require.Positivef(t, read[kind], "some read of kind %v has to succeed, or the comparison shows nothing", kind)
	}
}

func globalStoreContents(globals *GlobalStore) string {
	generals := make([]string, 0, len(globals.general))
	for _, value := range globals.general {
		if value.Kind() == reflect.Pointer && !value.IsNil() {
			value = value.Elem()
		}
		generals = append(generals, fmt.Sprint(value))
	}
	return fmt.Sprint(globals.ints, globals.floats, globals.Strings, globals.bools, globals.uints, generals, globals.complexes)
}

func runGlobalStoreRoute(t *testing.T, globals *GlobalStore, bases *program.SlotAllocation, slot int, kind isa.RegisterKind, wide, assembly bool) (contents, failure string, steppedOver bool) {
	t.Helper()
	const source, witness, fellThrough = 1, 5, 11
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	instruction := isa.NewInstruction(isa.OpSetGlobal, source, uint8(slot), uint8(kind))
	builder.body = append(builder.body, instruction)
	if wide {
		instruction = isa.NewTier1Instruction(isa.SubOpSetGlobalWide, source, uint8(kind))
		builder.body = append(builder.body[:0], instruction, isa.NewInstruction(isa.OpExtension, uint8(slot&0xFF), uint8(slot>>8), 0))
	}
	builder.body = append(builder.body, isa.NewTier1Instruction(isa.SubOpLoadInt64Immediate, witness, fellThrough))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	function := builder.build()
	function.GlobalBases = bases
	vm, frame, registers := newFramedVM(t, function)
	vm.Globals = globals
	registers.Ints[source], registers.Uints[source], registers.Floats[source] = -77, math.MaxUint64-77, -77.5
	registers.Bools[source], registers.Strings[source], registers.Complex[source] = true, "stored", complex(7, 7)
	registers.General[source] = reflect.ValueOf("stored value")
	defer func() {
		if recovered := recover(); recovered != nil {
			failure = fmt.Sprint("panic: ", recovered)
		}
		if failure == "" && vm.evalError != nil {
			failure = fmt.Sprint("error: ", vm.evalError)
		}
		contents = globalStoreContents(globals)
	}()
	if assembly {
		runInAssembly(t, vm)
		return contents, failure, registers.Ints[witness] == fellThrough
	}
	frame.ProgramCounter = 1
	handler := handleSetGlobal
	if wide {
		handler = handleSetGlobalWide
	}
	result := handler(vm, frame, registers, instruction)
	return contents, failure, result == opContinue && frame.ProgramCounter == len(function.Body)-2
}

func TestSetGlobalRoutesStoreAsTheGoHandlersDo(t *testing.T) {
	narrow, known := isa.SpecFor(isa.OpSetGlobal)
	require.True(t, known)
	require.Equal(t, isa.DispatchAsm, narrow.Dispatch(), "a scalar store has an assembly body")
	wideSpec, known := isa.SpecAt(isa.TierSub1, uint8(isa.SubOpSetGlobalWide))
	require.True(t, known)
	require.Equal(t, isa.DispatchAsm, wideSpec.Dispatch(), "the wide store has an assembly body")

	kinds := []isa.RegisterKind{isa.RegisterInt64, isa.RegisterFloat64, isa.RegisterString, isa.RegisterGeneral, isa.RegisterBool, isa.RegisterUint64, isa.RegisterComplex128}
	stored := map[isa.RegisterKind]int{}
	forms := []struct {
		slots []int
		wide  bool
	}{
		{wide: false, slots: []int{0, 1, 2, 3, 4, 255}},
		{wide: true, slots: []int{0, 2, 3, 255, 256, 298, 299, 300, 0xFFFF}},
	}
	for _, form := range forms {
		for _, shared := range []bool{false, true} {
			for _, bases := range []*program.SlotAllocation{nil, {1, 2, 0, 1, 2, 1, 1}} {
				for _, kind := range kinds {
					for _, slot := range form.slots {
						label := fmt.Sprintf("wide %v, shared %v, bases %v, kind %v, slot %d", form.wide, shared, bases, kind, slot)
						before := globalStoreContents(wideGlobalRouteStore())
						byHandler, byAssembly := wideGlobalRouteStore(), wideGlobalRouteStore()
						byHandler.shared.Store(shared)
						byAssembly.shared.Store(shared)
						want, wantFailure, wantStepped := runGlobalStoreRoute(t, byHandler, bases, slot, kind, form.wide, false)
						got, gotFailure, gotStepped := runGlobalStoreRoute(t, byAssembly, bases, slot, kind, form.wide, true)
						require.Equal(t, wantFailure, gotFailure, label)
						require.Truef(t, want == got, "%s: the stores hold the same values afterwards", label)
						if wantFailure != "" {
							continue
						}
						stored[kind]++
						require.Equal(t, wantStepped, gotStepped, "%s: the words after the store run exactly once", label)
						if kind != isa.RegisterBool {
							require.Truef(t, before != want, "%s: the Go handler changed the variable", label)
						}
					}
				}
			}
		}
	}
	for _, kind := range kinds {
		require.Positivef(t, stored[kind], "some store of kind %v has to succeed, or the comparison shows nothing", kind)
	}
}

func TestGetGlobalRouteReadsAGeneralValueWhileTheCollectorMarks(t *testing.T) {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
				runtime.GC()
			}
		}
	}()
	defer func() { close(stop); <-done }()
	globals := globalRouteStore()
	for round := range 2000 {
		slot := uint8(round % 3)
		got := runGlobalRoute(t, globals, nil, slot, isa.RegisterGeneral, true)
		require.Empty(t, got.failure)
		require.Equal(t, fmt.Sprint(globals.GetGeneral(int(slot))), fmt.Sprint(got.general), "round %d", round)
	}
}

func TestGetGlobalRouteTakesTheStoreLockOnceShared(t *testing.T) {
	for _, kind := range []isa.RegisterKind{isa.RegisterInt64, isa.RegisterGeneral} {
		globals := globalRouteStore()
		globals.shared.Store(true)
		globals.mu.Lock()
		finished := make(chan globalRouteOutcome, 1)
		go func() { finished <- runGlobalRoute(t, globals, nil, 1, kind, true) }()
		select {
		case <-finished:
			globals.mu.Unlock()
			require.Failf(t, "read a shared store without its lock", "kind %v", kind)
		case <-time.After(50 * time.Millisecond):
		}
		globals.mu.Unlock()
		require.Empty(t, (<-finished).failure, "kind %v", kind)
	}
}
