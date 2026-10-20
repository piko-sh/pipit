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

package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/engine"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/symtab"
)

func TestMaxAllocSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		options []Option
		code    string
		want    any
		wantErr error
	}{
		{name: "a slice above the limit", options: []Option{WithMaxAllocSize(100)}, code: `make([]int, 1000)`, wantErr: fault.ErrAllocationLimit},
		{name: "a slice below the limit", options: []Option{WithMaxAllocSize(100)}, code: `len(make([]int, 50))`, want: 50},
		{name: "a channel above the limit", options: []Option{WithMaxAllocSize(100)}, code: `make(chan int, 1000)`, wantErr: fault.ErrAllocationLimit},
		{name: "no limit", code: `len(make([]int, 10000))`, want: 10000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := NewService(tt.options...).Eval(t.Context(), tt.code)
			require.ErrorIs(t, err, tt.wantErr)
			if tt.wantErr != nil {
				return
			}
			require.Equal(t, tt.want, result)
		})
	}
}

func TestMaxGoroutines(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxGoroutines(3))

	_, err := service.Eval(t.Context(), `
for i := 0; i < 10; i++ {
	go func() {}()
}
`)
	require.ErrorIs(t, err, fault.ErrGoroutineLimit, "the goroutine limit applies")

	runtime.Gosched()
}

func TestMaxGoroutinesUnlimited(t *testing.T) {
	t.Parallel()
	service := NewService()

	_, err := service.Eval(t.Context(), `
func noop() {}
for i := 0; i < 10; i++ {
	go noop()
}
`)
	require.NoError(t, err)
}

func TestMaxCallDepth(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxCallDepth(50))

	_, err := service.Eval(t.Context(), `
func recurse(n int) int {
	return 1 + recurse(n + 1)
}
recurse(0)
`)
	require.ErrorIs(t, err, fault.ErrStackOverflow, "deep recursion overflows the stack")
}

func TestMaxCallDepthDefault(t *testing.T) {
	t.Parallel()
	service := NewService()

	_, err := service.Eval(t.Context(), `
func recurse(n int) int {
	if n <= 0 { return 0 }
	return recurse(n - 1)
}
recurse(100)
`)
	require.NoError(t, err)
}

func TestMaxOutputSize(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxOutputSize(50))

	var buffer bytes.Buffer
	svcVM := engine.NewVM(t.Context(), service.globals, service.symbols)
	svcVM.StderrWriter = &buffer
	svcVM.Limits = service.limits

	compiledFunction, err := service.Compile(t.Context(), `
for i := 0; i < 1000; i++ {
	println("hello world")
}
`)
	require.NoError(t, err)

	_, err = svcVM.Execute(compiledFunction)
	require.ErrorIs(t, err, fault.ErrOutputLimit, "the output limit applies")
}

func TestMaxOutputSizeUnlimited(t *testing.T) {
	t.Parallel()
	service := NewService()

	var buffer bytes.Buffer
	svcVM := engine.NewVM(t.Context(), service.globals, service.symbols)
	svcVM.StderrWriter = &buffer
	svcVM.Limits = service.limits

	compiledFunction, err := service.Compile(t.Context(), `
for i := 0; i < 10; i++ {
	println("hello")
}
`)
	require.NoError(t, err)

	_, err = svcVM.Execute(compiledFunction)
	require.NoError(t, err)
	require.NotEqual(t, 0, buffer.Len(), "the program prints before the limit")
}

func TestAtomicCancellationBackgroundCtx(t *testing.T) {
	t.Parallel()
	vm := engine.NewVM(context.Background(), engine.NewGlobalStore(), symtab.NewSymbolRegistry(nil))
	require.Equal(t, uint32(0), vm.Cancelled.Load(), "a background context leaves the cancelled flag clear")
}

func TestAtomicCancellationTimeout(t *testing.T) {
	t.Parallel()
	service := NewService(WithMaxExecutionTime(100 * time.Millisecond))
	service.UseSymbols(newOSSymbols())

	_, err := service.Eval(t.Context(), `
import (
	"os"
)
x := 0
for {
	x++
	os.Getenv("PATH")
}
`)
	require.ErrorIs(t, err, context.DeadlineExceeded, "the deadline stops execution")
}

func TestAtomicCancellationFlagSet(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancelCause(t.Context())
	vm := engine.NewVM(ctx, engine.NewGlobalStore(), symtab.NewSymbolRegistry(nil))

	require.Equal(t, uint32(0), vm.Cancelled.Load(), "the cancelled flag is clear before cancellation")

	cancel(errors.New("test: cancellation flag propagation"))
	require.Eventually(t, func() bool { return vm.Cancelled.Load() != 0 }, time.Second, time.Millisecond,
		"cancelling the context sets the VM's cancelled flag")
}

func TestWideGlobalVariables(t *testing.T) {
	t.Parallel()

	const globalCount = 300

	t.Run("int globals exceed uint8 boundary", func(t *testing.T) {
		t.Parallel()

		var source strings.Builder
		source.WriteString("package main\n\n")
		for i := range globalCount {
			fmt.Fprintf(&source, "var g%d int\n", i)
		}
		source.WriteString("\nfunc run() int {\n")
		source.WriteString("\tg255 = 100\n")
		source.WriteString("\tg256 = 200\n")
		source.WriteString("\tg299 = 300\n")
		source.WriteString("\treturn g255 + g256 + g299\n")
		source.WriteString("}\nfunc main() {}\n")

		service := NewService()
		result, err := service.EvalFile(t.Context(), source.String(), "run")
		require.NoError(t, err)
		require.Equal(t, 600, result)
	})

	t.Run("string globals exceed uint8 boundary", func(t *testing.T) {
		t.Parallel()

		var source strings.Builder
		source.WriteString("package main\n\n")
		for i := range globalCount {
			fmt.Fprintf(&source, "var s%d string\n", i)
		}
		source.WriteString("\nfunc run() string {\n")
		source.WriteString("\ts256 = \"hello\"\n")
		source.WriteString("\ts257 = \" world\"\n")
		source.WriteString("\treturn s256 + s257\n")
		source.WriteString("}\nfunc main() {}\n")

		service := NewService()
		result, err := service.EvalFile(t.Context(), source.String(), "run")
		require.NoError(t, err)
		require.Equal(t, "hello world", result)
	})

	t.Run("bool globals exceed uint8 boundary", func(t *testing.T) {
		t.Parallel()

		var source strings.Builder
		source.WriteString("package main\n\n")
		for i := range globalCount {
			fmt.Fprintf(&source, "var b%d bool\n", i)
		}
		source.WriteString("\nfunc run() bool {\n")
		source.WriteString("\tb256 = true\n")
		source.WriteString("\treturn b256\n")
		source.WriteString("}\nfunc main() {}\n")

		service := NewService()
		result, err := service.EvalFile(t.Context(), source.String(), "run")
		require.NoError(t, err)
		require.Equal(t, true, result)
	})

	t.Run("global initialiser with wide index", func(t *testing.T) {
		t.Parallel()

		var source strings.Builder
		source.WriteString("package main\n\n")
		for i := range globalCount {
			if i == 260 {
				fmt.Fprintf(&source, "var g%d int = 42\n", i)
			} else {
				fmt.Fprintf(&source, "var g%d int\n", i)
			}
		}
		source.WriteString("\nfunc run() int { return g260 }\n")
		source.WriteString("func main() {}\n")

		service := NewService()
		result, err := service.EvalFile(t.Context(), source.String(), "run")
		require.NoError(t, err)
		require.Equal(t, 42, result)
	})
}

func TestGoroutineTrackerDecrement(t *testing.T) {
	t.Parallel()
	tracker := &engine.ResourceTracker{}
	tracker.GoroutineCount.Store(5)
	tracker.GoroutineCount.Add(-1)
	require.Equal(t, int32(4), tracker.GoroutineCount.Load())
}
