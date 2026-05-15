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
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

func stackNamedFunction(name string, bodyLength int) *program.CompiledFunction {
	function := program.NewNamedFunction(name)
	function.RuntimeName = "main." + name
	function.Body = make([]isa.Instruction, bodyLength)
	return function
}

func TestCallStackRegistryHandsOutStableCounters(t *testing.T) {
	t.Parallel()

	t.Run("a function keeps one entry across lookups", func(t *testing.T) {
		t.Parallel()

		registry := newCallStackRegistry()
		function := stackNamedFunction("run", 4)

		first := registry.forFunction(function)
		second := registry.forFunction(function)

		require.Same(t, first, second, "one entry per function keeps equality meaningful")
		require.Equal(t, "main.run", first.Name())
		require.NotZero(t, first.Entry())
	})

	t.Run("a function with no runtime name falls back to its own", func(t *testing.T) {
		t.Parallel()

		registry := newCallStackRegistry()
		function := program.NewNamedFunction("bare")

		require.Equal(t, "bare", registry.forFunction(function).Name())
	})

	t.Run("a pseudo frame keeps one entry per name", func(t *testing.T) {
		t.Parallel()

		registry := newCallStackRegistry()

		first := registry.pseudo(pseudoFrameGoexit)
		second := registry.pseudo(pseudoFrameGoexit)

		require.Same(t, first, second)
		require.Equal(t, pseudoFrameGoexit, first.Name())
		require.NotSame(t, first, registry.pseudo(pseudoFrameMain), "distinct names get distinct entries")
	})

	t.Run("two functions get distinct entry counters", func(t *testing.T) {
		t.Parallel()

		registry := newCallStackRegistry()

		first := registry.forFunction(stackNamedFunction("one", 2))
		second := registry.forFunction(stackNamedFunction("two", 2))

		require.NotEqual(t, first.Entry(), second.Entry())
	})
}

func TestCallStackRegistryDecodesItsOwnCounters(t *testing.T) {
	t.Parallel()

	registry := newCallStackRegistry()
	function := stackNamedFunction("run", 4)
	entry := registry.forFunction(function)

	tests := []struct {
		name  string
		pc    uintptr
		found bool
	}{
		{name: "the entry counter", pc: entry.Entry(), found: true},
		{name: "a counter inside the body", pc: entry.pc(2), found: true},
		{name: "the counter one past the body", pc: entry.pc(len(function.Body)), found: true},
		{name: "a counter past the body names no function", pc: entry.pc(len(function.Body) + 1), found: false},
		{name: "a zero counter names no function", pc: 0, found: false},
		{name: "a counter from no registry index", pc: entry.Entry() << 4, found: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			found := registry.lookup(tt.pc)
			if !tt.found {
				require.Nil(t, found)
				return
			}
			require.Same(t, entry, found)
		})
	}
}

func TestCallStackRegistryRefusesAForeignHandle(t *testing.T) {
	t.Parallel()

	registry := newCallStackRegistry()
	registry.forFunction(stackNamedFunction("run", 2))

	tests := []struct {
		name  string
		value reflect.Value
	}{
		{name: "an invalid value", value: reflect.Value{}},
		{name: "a value that is not a pointer", value: reflect.ValueOf(3)},
		{name: "a nil handle", value: reflect.ValueOf((*runtime.Func)(nil))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Nil(t, registry.fromHandle(tt.value))
		})
	}
}

func TestPipitFuncAnswersAsRuntimeFuncDoes(t *testing.T) {
	t.Parallel()

	t.Run("a nil receiver answers with the zero values", func(t *testing.T) {
		t.Parallel()

		var entry *pipitFunc

		require.Empty(t, entry.Name())
		require.Zero(t, entry.Entry())

		file, line := entry.FileLine(0)
		require.Empty(t, file)
		require.Zero(t, line)
	})

	t.Run("a pseudo frame has no source position", func(t *testing.T) {
		t.Parallel()

		entry := newCallStackRegistry().pseudo(pseudoFrameMain)

		file, line := entry.FileLine(entry.Entry())
		require.Empty(t, file)
		require.Zero(t, line)
	})

	t.Run("a synthesised counter carries its instruction index", func(t *testing.T) {
		t.Parallel()

		entry := newCallStackRegistry().forFunction(stackNamedFunction("run", 8))

		require.Equal(t, entry.Entry()|3, entry.pc(3))
		require.Equal(t, entry.Entry(), entry.pc(0))
	})
}

func TestPipitFramesIterateOnce(t *testing.T) {
	t.Parallel()

	t.Run("a nil iterator is exhausted", func(t *testing.T) {
		t.Parallel()

		var frames *pipitFrames

		frame, more := frames.Next()
		require.False(t, more)
		require.Zero(t, frame.PC)
	})

	t.Run("each frame comes back once, then the iterator is exhausted", func(t *testing.T) {
		t.Parallel()

		frames := &pipitFrames{frames: []runtime.Frame{{Function: "a"}, {Function: "b"}}}

		first, more := frames.Next()
		require.Equal(t, "a", first.Function)
		require.True(t, more, "another frame follows")

		second, more := frames.Next()
		require.Equal(t, "b", second.Function)
		require.False(t, more, "the last frame reports no successor")

		_, more = frames.Next()
		require.False(t, more)
	})
}

func TestRuntimeCallerReportsTheInterpretedFrames(t *testing.T) {
	t.Parallel()

	vm, _, _ := newStandardVM(t)

	t.Run("skip zero names the running function", func(t *testing.T) {
		t.Parallel()

		results := vm.runtimeCaller(0)
		require.Len(t, results, 4)
		require.True(t, results[3].Bool(), "the running frame is always reportable")
		require.NotZero(t, results[0].Interface())
	})

	t.Run("a skip past the stack reports not-ok", func(t *testing.T) {
		t.Parallel()

		results := vm.runtimeCaller(99)
		require.False(t, results[3].Bool())
		require.Zero(t, results[0].Interface())
	})

	t.Run("a negative skip reports not-ok", func(t *testing.T) {
		t.Parallel()

		require.False(t, vm.runtimeCaller(-1)[3].Bool())
	})
}

func TestRuntimeCallersFillsTheCallerSlice(t *testing.T) {
	t.Parallel()

	vm, _, _ := newStandardVM(t)

	t.Run("the tokens start with the Callers frame and end with goexit", func(t *testing.T) {
		t.Parallel()

		tokens := vm.callerTokens()
		require.GreaterOrEqual(t, len(tokens), 3)

		registry := vm.Globals.callStack
		require.Equal(t, pseudoFrameCallers, registry.lookup(tokens[0]).Name())
		require.Equal(t, pseudoFrameGoexit, registry.lookup(tokens[len(tokens)-1]).Name())
	})

	t.Run("the destination slice bounds the count", func(t *testing.T) {
		t.Parallel()

		destination := make([]uintptr, 2)
		count := vm.runtimeCallers(0, reflect.ValueOf(destination))
		require.Equal(t, 2, count)
		require.NotZero(t, destination[0])
	})

	t.Run("a skip past the token list writes nothing", func(t *testing.T) {
		t.Parallel()

		destination := make([]uintptr, 4)
		require.Zero(t, vm.runtimeCallers(99, reflect.ValueOf(destination)))
	})

	t.Run("a negative skip is treated as zero", func(t *testing.T) {
		t.Parallel()

		destination := make([]uintptr, 1)
		require.Equal(t, 1, vm.runtimeCallers(-5, reflect.ValueOf(destination)))
	})
}

func TestCallersFramesDecodesEveryCounter(t *testing.T) {
	t.Parallel()

	vm, _, _ := newStandardVM(t)
	tokens := vm.callerTokens()
	destination := make([]uintptr, len(tokens))
	vm.runtimeCallers(0, reflect.ValueOf(destination))

	frames := vm.callersFrames(reflect.ValueOf(destination))

	first, _ := frames.Next()
	require.Equal(t, pseudoFrameCallers, first.Function)
	require.Equal(t, destination[0], first.PC)

	t.Run("a counter naming no function decodes to a bare frame", func(t *testing.T) {
		t.Parallel()

		bare := vm.callersFrames(reflect.ValueOf([]uintptr{0}))
		frame, more := bare.Next()
		require.False(t, more)
		require.Empty(t, frame.Function)
	})
}

func TestFormatStackRendersAGoroutineHeader(t *testing.T) {
	t.Parallel()

	vm, _, _ := newStandardVM(t)

	rendered := string(vm.formatStack())
	require.True(t, strings.HasPrefix(rendered, "goroutine 1 [running]:\n"), "the header names the goroutine")
	require.Contains(t, rendered, "runtime/debug.Stack()")
}

func TestPrintStackWritesToTheConfiguredStderr(t *testing.T) {
	t.Parallel()

	vm, _, _ := newStandardVM(t)
	output := &strings.Builder{}
	vm.SetStderrWriter(output)

	vm.printStack()

	require.Contains(t, output.String(), "goroutine 1 [running]:")
}

func TestAnswerRuntimeFuncMethodNeedsAReceiver(t *testing.T) {
	t.Parallel()

	vm, _, _ := newStandardVM(t)

	t.Run("no arguments answers nothing", func(t *testing.T) {
		t.Parallel()

		_, ok := vm.answerRuntimeFuncMethod(runtimeFuncNamePointer, nil)
		require.False(t, ok)
	})

	t.Run("an unknown method expression answers nothing", func(t *testing.T) {
		t.Parallel()

		_, ok := vm.answerRuntimeFuncMethod(0, []reflect.Value{reflect.ValueOf((*runtime.Func)(nil))})
		require.False(t, ok)
	})

	t.Run("a foreign handle still answers Name with the empty string", func(t *testing.T) {
		t.Parallel()

		results, ok := vm.answerRuntimeFuncMethod(runtimeFuncNamePointer,
			[]reflect.Value{reflect.ValueOf((*runtime.Func)(nil))})
		require.True(t, ok)
		require.Empty(t, results[0].String())
	})

	t.Run("a foreign handle answers Entry with zero", func(t *testing.T) {
		t.Parallel()

		results, ok := vm.answerRuntimeFuncMethod(runtimeFuncEntryPointer,
			[]reflect.Value{reflect.ValueOf((*runtime.Func)(nil))})
		require.True(t, ok)
		require.Zero(t, results[0].Interface())
	})

	t.Run("FileLine needs its counter argument", func(t *testing.T) {
		t.Parallel()

		_, ok := vm.answerRuntimeFuncMethod(runtimeFuncFileLinePointer,
			[]reflect.Value{reflect.ValueOf((*runtime.Func)(nil))})
		require.False(t, ok)

		results, ok := vm.answerRuntimeFuncMethod(runtimeFuncFileLinePointer,
			[]reflect.Value{reflect.ValueOf((*runtime.Func)(nil)), reflect.ValueOf(uintptr(0))})
		require.True(t, ok)
		require.Empty(t, results[0].String())
		require.Zero(t, results[1].Interface())
	})
}
