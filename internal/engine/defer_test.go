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
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
)

func nativeDefer(order *[]string, label string) deferredCall {
	return deferredCall{
		nativeFunction: reflect.ValueOf(func() { *order = append(*order, label) }),
	}
}

func TestRunDefersRunsThemLastInFirstOut(t *testing.T) {
	t.Parallel()

	t.Run("every armed defer runs in reverse order", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		var order []string
		frame.deferBase = 0
		vm.deferStack = []deferredCall{nativeDefer(&order, "first"), nativeDefer(&order, "second")}

		vm.runDefers()

		require.Equal(t, []string{"second", "first"}, order)
		require.Empty(t, vm.deferStack, "the stack is drained back to the frame's base")
	})

	t.Run("a defer already started is skipped", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		var order []string
		started := nativeDefer(&order, "started")
		started.started = true
		frame.deferBase = 0
		vm.deferStack = []deferredCall{nativeDefer(&order, "pending"), started}

		vm.runDefers()

		require.Equal(t, []string{"pending"}, order, "a defer the return path already took must not run twice")
	})

	t.Run("defers below the frame's base are left alone", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		var order []string
		frame.deferBase = 1
		vm.deferStack = []deferredCall{nativeDefer(&order, "caller"), nativeDefer(&order, "callee")}

		vm.runDefers()

		require.Equal(t, []string{"callee"}, order, "the caller's own defers are not this frame's to run")
		require.Len(t, vm.deferStack, 1)
	})
}

func TestRunRemainingDefersDrainsToTheGivenBase(t *testing.T) {
	t.Parallel()

	vm, _, _ := newStandardVM(t)
	var order []string
	vm.deferStack = []deferredCall{
		nativeDefer(&order, "kept"),
		nativeDefer(&order, "first"),
		nativeDefer(&order, "second"),
	}

	vm.runRemainingDefers(1)

	require.Equal(t, []string{"second", "first"}, order)
	require.Len(t, vm.deferStack, 1)
}

func TestExecuteDeferredCallRoutesByKind(t *testing.T) {
	t.Parallel()

	t.Run("a native function is called", func(t *testing.T) {
		t.Parallel()

		vm, _, _ := newStandardVM(t)
		var order []string

		vm.executeDeferredCall(nativeDefer(&order, "native"))

		require.Equal(t, []string{"native"}, order)
	})

	t.Run("a native function with arguments takes them", func(t *testing.T) {
		t.Parallel()

		vm, _, _ := newStandardVM(t)
		seen := 0

		vm.executeDeferredCall(deferredCall{
			nativeFunction: reflect.ValueOf(func(n int) { seen = n }),
			arguments:      []reflect.Value{reflect.ValueOf(7)},
		})

		require.Equal(t, 7, seen)
	})

	t.Run("an unset native function does nothing", func(t *testing.T) {
		t.Parallel()

		vm, _, _ := newStandardVM(t)

		require.NotPanics(t, func() { vm.executeDeferredCall(deferredCall{}) })
	})

	t.Run("a panicking native function records the panic", func(t *testing.T) {
		t.Parallel()

		vm, _, _ := newStandardVM(t)

		vm.executeDeferredCall(deferredCall{nativeFunction: reflect.ValueOf(func() { panic("boom") })})

		require.True(t, vm.panicking, "a deferred body that panics continues the unwind")
		require.Equal(t, "boom", vm.panicValue)
	})

	t.Run("a builtin print is written to stderr", func(t *testing.T) {
		t.Parallel()

		vm, _, _ := newStandardVM(t)
		output := &strings.Builder{}
		vm.SetStderrWriter(output)

		vm.executeDeferredCall(deferredCall{
			builtin:   isa.BuiltinPrintln,
			arguments: []reflect.Value{reflect.ValueOf("deferred")},
		})

		require.Equal(t, "deferred\n", output.String())
	})
}

func TestRunFrameSimpleDeferRunsTheArmedSlotOnce(t *testing.T) {
	t.Parallel()

	t.Run("an unarmed frame does nothing", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)

		require.NotPanics(t, func() { vm.runFrameSimpleDefer(frame) })
	})

	t.Run("an inactive record does nothing", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		var order []string
		frame.simpleDefer = &SimpleDeferRecord{nativeFunction: reflect.ValueOf(func() { order = append(order, "ran") })}

		vm.runFrameSimpleDefer(frame)

		require.Empty(t, order)
	})

	t.Run("an armed record runs and disarms", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		var order []string
		frame.simpleDefer = &SimpleDeferRecord{
			active:         true,
			nativeFunction: reflect.ValueOf(func() { order = append(order, "ran") }),
		}

		vm.runFrameSimpleDefer(frame)

		require.Equal(t, []string{"ran"}, order)
		require.False(t, frame.simpleDefer.active, "the slot must not run twice")
		require.False(t, frame.simpleDefer.nativeFunction.IsValid(), "the record releases what it held")
	})

	t.Run("a record the return path already took is only disarmed", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		var order []string
		frame.simpleDefer = &SimpleDeferRecord{
			active:         true,
			consumed:       true,
			nativeFunction: reflect.ValueOf(func() { order = append(order, "ran") }),
		}

		vm.runFrameSimpleDefer(frame)

		require.Empty(t, order, "the inline-defer path already ran it")
		require.False(t, frame.simpleDefer.active)
		require.False(t, frame.simpleDefer.consumed)
	})
}

func TestUnwindGoexitRunsEveryFrameDefer(t *testing.T) {
	t.Parallel()

	callee := voidCallee()
	vm, frame, _ := closureHostVM(t, callee)
	var order []string

	frame.deferBase = 0
	vm.deferStack = []deferredCall{nativeDefer(&order, "outer")}

	vm.PushFrame(callee)
	inner := &vm.CallStack[vm.FramePointer]
	inner.deferBase = len(vm.deferStack)
	vm.deferStack = append(vm.deferStack, nativeDefer(&order, "inner"))
	inner.simpleDefer = &SimpleDeferRecord{
		active:         true,
		nativeFunction: reflect.ValueOf(func() { order = append(order, "inner slot") }),
	}

	vm.unwindGoexit()

	require.Equal(t, []string{"inner slot", "inner", "outer"}, order,
		"goexit runs every armed defer from the innermost frame down")
	require.Less(t, vm.FramePointer, vm.baseFramePointer, "every frame is popped")
	require.Empty(t, vm.deferStack)
}

func TestTakeInlineDeferOnlyTakesWhatItMayRunInline(t *testing.T) {
	t.Parallel()

	t.Run("an unarmed frame yields nothing", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)

		_, ok := vm.takeInlineDefer(frame)
		require.False(t, ok)
	})

	t.Run("a record with no compiled target yields nothing", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		frame.simpleDefer = &SimpleDeferRecord{active: true}

		_, ok := vm.takeInlineDefer(frame)
		require.False(t, ok, "a native target keeps its own recover semantics on the nested path")
	})

	t.Run("a record already consumed yields nothing", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		frame.simpleDefer = &SimpleDeferRecord{
			active:   true,
			consumed: true,
			target:   &RuntimeClosure{Function: voidCallee()},
		}

		_, ok := vm.takeInlineDefer(frame)
		require.False(t, ok)
	})

	t.Run("a target that can recover yields nothing", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		callee := voidCallee()
		callee.HasRecover = true
		frame.simpleDefer = &SimpleDeferRecord{active: true, target: &RuntimeClosure{Function: callee}}

		_, ok := vm.takeInlineDefer(frame)
		require.False(t, ok, "a body that can recover has to run under its own dispatch")
	})

	t.Run("a plain compiled target is taken and the record marked consumed", func(t *testing.T) {
		t.Parallel()

		vm, frame, _ := newStandardVM(t)
		closure := &RuntimeClosure{Function: voidCallee()}
		frame.simpleDefer = &SimpleDeferRecord{active: true, target: closure}

		call, ok := vm.takeInlineDefer(frame)
		require.True(t, ok)
		require.Same(t, closure, call.Function)
		require.True(t, frame.simpleDefer.consumed, "the slot must not be taken twice")
		require.Nil(t, frame.simpleDefer.target)
	})
}
