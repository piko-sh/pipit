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

package engine

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

type dispatchBase struct{ Value int }

type dispatchOuter struct {
	dispatchBase
	Name string
}

type dispatchPointerOuter struct {
	*dispatchBase
	Name string
}

type dispatchInterfaceOuter struct {
	dispatchSpeaker
	Name string
}

type dispatchSpeaker interface{ Speak() string }

func methodTableVM(t *testing.T, table map[string]uint16) *VM {
	t.Helper()

	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(4)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	root := builder.build()
	root.SetMethodTable(table)

	vm := newTestVM(t)
	vm.prepareForExecution(root)
	t.Cleanup(vm.ReleaseArena)
	return vm
}

func TestLookupMethodTableForReceiverNamesTheReceiverType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		receiver  reflect.Value
		method    string
		wantIndex uint16
		wantFound bool
	}{
		{
			name: "a named struct resolves by its own name", receiver: reflect.ValueOf(dispatchBase{}),
			method: "Ping", wantIndex: 3, wantFound: true,
		},
		{
			name: "a pointer receiver resolves through its element type", receiver: reflect.ValueOf(&dispatchBase{}),
			method: "Ping", wantIndex: 3, wantFound: true,
		},
		{
			name: "a method the table does not hold is a miss", receiver: reflect.ValueOf(dispatchBase{}),
			method: "Absent", wantFound: false,
		},
		{
			name: "an unnamed type has no table key", receiver: reflect.ValueOf(struct{ X int }{}),
			method: "Ping", wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := methodTableVM(t, map[string]uint16{"dispatchBase.Ping": 3})
			index, found := lookupMethodTableForReceiver(vm, tt.receiver, tt.method)
			require.Equal(t, tt.wantFound, found)
			if tt.wantFound {
				require.Equal(t, tt.wantIndex, index)
			}
		})
	}
}

func TestResolvePromotedMethodWalksEmbeddedFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		receiver     reflect.Value
		method       string
		wantIndex    uint16
		wantFound    bool
		wantReceiver any
	}{
		{
			name: "a value embedding promotes the method", receiver: reflect.ValueOf(dispatchOuter{dispatchBase{Value: 1}, "n"}),
			method: "Ping", wantIndex: 3, wantFound: true, wantReceiver: dispatchBase{Value: 1},
		},
		{
			name:     "a pointer receiver is dereferenced first",
			receiver: reflect.ValueOf(&dispatchOuter{dispatchBase{Value: 2}, "n"}),
			method:   "Ping", wantIndex: 3, wantFound: true, wantReceiver: dispatchBase{Value: 2},
		},
		{
			name:     "a pointer embedding is followed",
			receiver: reflect.ValueOf(dispatchPointerOuter{&dispatchBase{Value: 4}, "n"}),
			method:   "Ping", wantIndex: 3, wantFound: true, wantReceiver: dispatchBase{Value: 4},
		},
		{
			name: "a method no embedding carries is a miss", receiver: reflect.ValueOf(dispatchOuter{}),
			method: "Absent", wantFound: false,
		},
		{
			name: "a receiver that is not a struct is a miss", receiver: reflect.ValueOf(4),
			method: "Ping", wantFound: false,
		},
		{
			name: "a nil embedded interface is a miss", receiver: reflect.ValueOf(dispatchInterfaceOuter{}),
			method: "Ping", wantFound: false,
		},
		{
			name: "a struct with no embedding at all is a miss", receiver: reflect.ValueOf(recordHolder{}),
			method: "Ping", wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := methodTableVM(t, map[string]uint16{"dispatchBase.Ping": 3})
			index, resolved, found := resolvePromotedMethod(vm, tt.receiver, tt.method)
			require.Equal(t, tt.wantFound, found)
			if tt.wantFound {
				require.Equal(t, tt.wantIndex, index)
				require.Equal(t, tt.wantReceiver, resolved.Interface())
			}
		})
	}
}

func TestResolvePromotedMethodUnwrapsInterfaceReceivers(t *testing.T) {
	t.Parallel()

	t.Run("an interface holding the embedding resolves to the concrete value", func(t *testing.T) {
		t.Parallel()

		vm := methodTableVM(t, map[string]uint16{"dispatchBase.Ping": 3})
		boxed := reflect.ValueOf(map[string]any{"k": dispatchOuter{dispatchBase{Value: 6}, "n"}}).MapIndex(reflect.ValueOf("k"))

		index, resolved, found := resolvePromotedMethod(vm, boxed, "Ping")
		require.True(t, found)
		require.Equal(t, uint16(3), index)
		require.Equal(t, dispatchBase{Value: 6}, resolved.Interface())
	})

	t.Run("an interface holding the receiver itself resolves without a walk", func(t *testing.T) {
		t.Parallel()

		vm := methodTableVM(t, map[string]uint16{"dispatchBase.Ping": 3})
		boxed := reflect.ValueOf(map[string]any{"k": dispatchBase{Value: 7}}).MapIndex(reflect.ValueOf("k"))

		index, resolved, found := resolvePromotedMethod(vm, boxed, "Ping")
		require.True(t, found)
		require.Equal(t, uint16(3), index)
		require.Equal(t, dispatchBase{Value: 7}, resolved.Interface())
	})

	t.Run("a nil interface is a miss", func(t *testing.T) {
		t.Parallel()

		vm := methodTableVM(t, map[string]uint16{"dispatchBase.Ping": 3})
		boxed := reflect.ValueOf(map[string]any{"k": nil}).MapIndex(reflect.ValueOf("k"))

		_, _, found := resolvePromotedMethod(vm, boxed, "Ping")
		require.False(t, found)
	})

	t.Run("the walk stops at the recursion ceiling", func(t *testing.T) {
		t.Parallel()

		vm := methodTableVM(t, map[string]uint16{"dispatchBase.Ping": 3})

		_, _, found := resolvePromotedMethodAtDepth(vm, reflect.ValueOf(dispatchOuter{}), "Ping", maxPromotedMethodDepth)
		require.False(t, found, "a cyclic embedding must not recurse without bound")
	})
}

func TestMethodInlineCacheServesTheSecondCall(t *testing.T) {
	t.Parallel()

	t.Run("a cached receiver type is served from the slots", func(t *testing.T) {
		t.Parallel()

		site := &program.CallSite{}
		callee := program.NewNamedFunction("Ping")
		receiverType := reflect.TypeFor[dispatchBase]()

		updateMethodIC(site, receiverType, 3, callee, false)

		typeWord := uintptr(reflectValueABIType(receiverType))
		require.Same(t, callee, lookupMethodInlineCache(site, typeWord))
		require.Equal(t, typeWord, site.LastReceiverTypeWord, "a slot hit promotes itself to the short path")
		require.Same(t, callee, lookupMethodInlineCache(site, typeWord), "the second call takes the short path")
	})

	t.Run("a zero type word never matches", func(t *testing.T) {
		t.Parallel()

		site := &program.CallSite{}
		updateMethodIC(site, reflect.TypeFor[dispatchBase](), 3, program.NewNamedFunction("Ping"), false)

		require.Nil(t, lookupMethodInlineCache(site, 0))
	})

	t.Run("an unseen receiver type misses", func(t *testing.T) {
		t.Parallel()

		site := &program.CallSite{}
		updateMethodIC(site, reflect.TypeFor[dispatchBase](), 3, program.NewNamedFunction("Ping"), false)

		require.Nil(t, lookupMethodInlineCache(site, uintptr(reflectValueABIType(reflect.TypeFor[recordHolder]()))))
	})

	t.Run("a promoted resolution is never cached", func(t *testing.T) {
		t.Parallel()

		site := &program.CallSite{}
		receiverType := reflect.TypeFor[dispatchBase]()

		updateMethodIC(site, receiverType, 3, program.NewNamedFunction("Ping"), true)

		require.Nil(t, lookupMethodInlineCache(site, uintptr(reflectValueABIType(receiverType))),
			"the cache cannot record the field walk a promoted call needs")
	})

	t.Run("recording the same receiver twice fills only one slot", func(t *testing.T) {
		t.Parallel()

		site := &program.CallSite{}
		receiverType := reflect.TypeFor[dispatchBase]()
		callee := program.NewNamedFunction("Ping")

		updateMethodIC(site, receiverType, 3, callee, false)
		updateMethodIC(site, receiverType, 3, program.NewNamedFunction("Other"), false)

		require.Same(t, callee, lookupMethodInlineCache(site, uintptr(reflectValueABIType(receiverType))),
			"a receiver type already in the cache must not evict itself")
	})
}

func TestReceiverDispatchTypeWordKeysPointerAndValueAlike(t *testing.T) {
	t.Parallel()

	value := reflect.ValueOf(dispatchBase{})
	pointer := reflect.ValueOf(&dispatchBase{})
	invalid := reflect.Value{}

	require.Equal(t, receiverDispatchTypeWord(&value), receiverDispatchTypeWord(&pointer),
		"a method set is the same whether reached through T or *T")
	require.Zero(t, receiverDispatchTypeWord(&invalid), "an invalid receiver carries no type")
}

func TestIsNilInterfaceReceiverNamesTheNilCall(t *testing.T) {
	t.Parallel()

	nilInterface := reflect.ValueOf(map[string]any{"k": nil}).MapIndex(reflect.ValueOf("k"))

	tests := []struct {
		name     string
		receiver reflect.Value
		want     bool
	}{
		{name: "an invalid receiver", receiver: reflect.Value{}, want: true},
		{name: "a nil interface", receiver: nilInterface, want: true},
		{name: "a concrete value", receiver: reflect.ValueOf(dispatchBase{}), want: false},
		{name: "a nil pointer is still a real receiver", receiver: reflect.ValueOf((*dispatchBase)(nil)), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, isNilInterfaceReceiver(tt.receiver))
		})
	}
}

func methodDispatchFrame(t *testing.T, methodName string, table map[string]uint16, callee *program.CompiledFunction) (*VM, *CallFrame, *Registers, uint16) {
	t.Helper()

	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(8)
	builder.functions = []*program.CompiledFunction{callee}
	nameIndex := builder.addStringConst(methodName)
	siteIndex := builder.AddCallSite(&program.CallSite{
		Arguments: []program.VarLocation{{Kind: isa.RegisterGeneral, Register: 1}},
		Returns:   []program.VarLocation{{Kind: isa.RegisterInt, Register: 0}},
	})
	builder.Emit(isa.OpDrillTier1, 0, 0, 0)
	builder.body = append(builder.body, op(nameIndex, 0, 0))
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)

	compiledFunction := builder.build()
	compiledFunction.SetMethodTable(table)

	vm, frame, registers := newFramedVM(t, compiledFunction)
	frame.ProgramCounter = 1
	return vm, frame, registers, siteIndex
}

func pingMethod(t *testing.T) *program.CompiledFunction {
	t.Helper()

	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(4)
	builder.returnInt()
	builder.parameterKinds = []isa.RegisterKind{isa.RegisterGeneral}
	builder.body = append(builder.body, tier2Return(1))

	method := builder.build()
	method.Name = "Ping"
	method.HasReceiver = true
	method.ParameterRegisters = []uint8{0}
	return method
}

func TestCallMethodResolvesThroughTheMethodTable(t *testing.T) {
	t.Parallel()

	callee := pingMethod(t)
	vm, frame, registers, siteIndex := methodDispatchFrame(t,
		"Ping", map[string]uint16{"methodCarrier.Ping": 0}, callee)
	registers.General[1] = reflect.ValueOf(methodCarrier{Count: 4})
	caller := vm.FramePointer

	require.Equal(t, opFrameChanged, handleCallMethod(vm, frame, registers, callSlotOperands(siteIndex)))
	require.Equal(t, caller+1, vm.FramePointer)
	require.Same(t, callee, vm.CallStack[vm.FramePointer].Function)
	require.Equal(t, 2, frame.ProgramCounter, "the method-name word must be stepped over")

	site := &frame.Function.CallSites[siteIndex]
	require.NotNil(t, lookupMethodInlineCache(site, uintptr(reflectValueABIType(reflect.TypeFor[methodCarrier]()))),
		"a resolved call must leave the site's cache warm for the next one")
}

func TestCallMethodTakesTheWarmInlineCache(t *testing.T) {
	t.Parallel()

	callee := pingMethod(t)
	vm, frame, registers, siteIndex := methodDispatchFrame(t,
		"Ping", map[string]uint16{"methodCarrier.Ping": 0}, callee)
	registers.General[1] = reflect.ValueOf(methodCarrier{})

	require.Equal(t, opFrameChanged, handleCallMethod(vm, frame, registers, callSlotOperands(siteIndex)))
	vm.popFrame()
	frame.ProgramCounter = 1

	before := vm.MethodCallGoDispatches
	require.Equal(t, opFrameChanged, handleCallMethod(vm, frame, registers, callSlotOperands(siteIndex)))
	require.Greater(t, vm.MethodCallGoDispatches, before, "the second call is served from the cache")
}

func TestCallMethodRefusesBrokenSites(t *testing.T) {
	t.Parallel()

	t.Run("a call site index past the table", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers, _ := methodDispatchFrame(t, "Ping", map[string]uint16{}, pingMethod(t))

		require.Equal(t, opPanicError, handleCallMethod(vm, frame, registers, callSlotOperands(99)))
		require.Error(t, vm.evalError)
	})

	t.Run("a site with no receiver argument", func(t *testing.T) {
		t.Parallel()

		vm, _, registers := newStandardVM(t)

		require.Equal(t, opPanicError, dispatchMethodCallSite(vm, nil, registers, &program.CallSite{}, op(0, 0, 0)))
		require.ErrorContains(t, vm.evalError, "no receiver argument")
	})

	t.Run("a nil interface receiver raises a dereference", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers, siteIndex := methodDispatchFrame(t, "Ping", map[string]uint16{}, pingMethod(t))
		registers.General[1] = reflect.ValueOf(map[string]any{"k": nil}).MapIndex(reflect.ValueOf("k"))

		got := handleCallMethod(vm, frame, registers, callSlotOperands(siteIndex))
		require.NotEqual(t, opContinue, got)
		require.Error(t, vm.evalError)
	})

	t.Run("a method-name index past the constants", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers, siteIndex := methodDispatchFrame(t, "Ping", map[string]uint16{}, pingMethod(t))
		frame.Function.Body[1] = op(9, 0, 0)
		registers.General[1] = reflect.ValueOf(methodCarrier{})

		require.Equal(t, opPanicError, handleCallMethod(vm, frame, registers, callSlotOperands(siteIndex)))
		require.Error(t, vm.evalError)
	})

	t.Run("a function index past the table", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers, siteIndex := methodDispatchFrame(t,
			"Ping", map[string]uint16{"methodCarrier.Ping": 9}, pingMethod(t))
		registers.General[1] = reflect.ValueOf(methodCarrier{})

		require.Equal(t, opPanicError, handleCallMethod(vm, frame, registers, callSlotOperands(siteIndex)))
		require.Error(t, vm.evalError)
	})

	t.Run("a method no table holds is reported by name", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers, siteIndex := methodDispatchFrame(t, "Absent", map[string]uint16{}, pingMethod(t))
		registers.General[1] = reflect.ValueOf(plainEmbedded{})

		require.Equal(t, opPanicError, handleCallMethod(vm, frame, registers, callSlotOperands(siteIndex)))
		require.ErrorContains(t, vm.evalError, "undefined method: plainEmbedded.Absent")
	})
}

func TestCallMethodFallsBackToTheNativeMethodSet(t *testing.T) {
	t.Parallel()

	vm, frame, registers, siteIndex := methodDispatchFrame(t, "Value", map[string]uint16{}, pingMethod(t))
	registers.General[1] = reflect.ValueOf(methodCarrier{Count: 6})

	require.Equal(t, opContinue, handleCallMethod(vm, frame, registers, callSlotOperands(siteIndex)),
		"a receiver whose method Go itself declares is dispatched through reflect")
	require.Equal(t, int64(6), registers.Ints[0])
}

func TestCallMethodResolvesAPromotedMethod(t *testing.T) {
	t.Parallel()

	callee := pingMethod(t)
	vm, frame, registers, siteIndex := methodDispatchFrame(t,
		"Ping", map[string]uint16{"dispatchBase.Ping": 0}, callee)
	original := reflect.ValueOf(dispatchOuter{dispatchBase{Value: 2}, "n"})
	registers.General[1] = original

	require.Equal(t, opFrameChanged, handleCallMethod(vm, frame, registers, callSlotOperands(siteIndex)))
	require.Equal(t, dispatchBase{Value: 2}, vm.CallStack[vm.FramePointer].Registers.General[0].Interface(),
		"the callee receives the embedded value the method was declared on")
	require.Equal(t, original.Interface(), registers.General[1].Interface(),
		"the caller's receiver register is restored after the projection")
}

func TestCallMethodResolvesThroughTheExternalTable(t *testing.T) {
	t.Parallel()

	callee := pingMethod(t)
	vm, frame, registers, siteIndex := methodDispatchFrame(t, "Ping", map[string]uint16{}, pingMethod(t))

	foreignBuilder := newBytecodeBuilder()
	foreignBuilder.numRegisters = wideRegCounts(4)
	foreignBuilder.functions = []*program.CompiledFunction{callee}
	foreignBuilder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	foreignRoot := foreignBuilder.build()

	vm.Globals.RegisterExternalMethod("methodCarrier.Ping", foreignRoot, 0)
	registers.General[1] = reflect.ValueOf(methodCarrier{})

	require.Equal(t, opFrameChanged, handleCallMethod(vm, frame, registers, callSlotOperands(siteIndex)),
		"a method another package registered still has to be reachable")
	require.Same(t, callee, vm.CallStack[vm.FramePointer].Function)
}
