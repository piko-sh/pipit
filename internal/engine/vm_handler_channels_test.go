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
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab"
)

type namedIntChannel chan int

func newCancelledVM(t *testing.T) *VM {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	vm := NewVM(ctx, NewGlobalStore(), symtab.NewSymbolRegistry(nil))
	t.Cleanup(vm.ReleaseArena)
	return vm
}

func TestChannelSendDeliversEveryBankKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		channel any
		kind    isa.RegisterKind
		load    func(*Registers)
		want    any
	}{
		{
			name:    "an int channel takes its value from the int bank",
			channel: make(chan int, 1),
			kind:    isa.RegisterInt,
			load:    func(r *Registers) { r.Ints[1] = 42 },
			want:    42,
		},
		{
			name:    "an int64 channel takes its value from the int bank unwidened",
			channel: make(chan int64, 1),
			kind:    isa.RegisterInt,
			load:    func(r *Registers) { r.Ints[1] = -7 },
			want:    int64(-7),
		},
		{
			name:    "a string channel takes its value from the string bank",
			channel: make(chan string, 1),
			kind:    isa.RegisterString,
			load:    func(r *Registers) { r.Strings[1] = "hello" },
			want:    "hello",
		},
		{
			name:    "a bool channel takes its value from the bool bank",
			channel: make(chan bool, 1),
			kind:    isa.RegisterBool,
			load:    func(r *Registers) { r.Bools[1] = true },
			want:    true,
		},
		{
			name:    "a float64 channel takes its value from the float bank",
			channel: make(chan float64, 1),
			kind:    isa.RegisterFloat,
			load:    func(r *Registers) { r.Floats[1] = 2.5 },
			want:    2.5,
		},
		{
			name:    "a uint channel has no typed handle and falls to the reflect path",
			channel: make(chan uint32, 1),
			kind:    isa.RegisterUint,
			load:    func(r *Registers) { r.Uints[1] = 9 },
			want:    uint32(9),
		},
		{
			name:    "a named channel type defeats the typed handle assertion",
			channel: namedIntChannel(make(chan int, 1)),
			kind:    isa.RegisterInt,
			load:    func(r *Registers) { r.Ints[1] = 11 },
			want:    11,
		},
		{
			name:    "an interface channel boxes the general register",
			channel: make(chan any, 1),
			kind:    isa.RegisterGeneral,
			load:    func(r *Registers) { r.General[1] = reflect.ValueOf("boxed") },
			want:    "boxed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newStandardVM(t)
			registers.General[0] = reflect.ValueOf(tt.channel)
			tt.load(registers)

			got := HandleChannelSend(vm, frame, registers, op(0, 1, uint8(tt.kind)))
			require.Equal(t, opContinue, got, "a send into free buffer space never leaves the dispatch loop")

			received, ok := reflect.ValueOf(tt.channel).TryRecv()
			require.True(t, ok, "the buffered channel must hold the sent value")
			require.Equal(t, tt.want, received.Interface())
		})
	}
}

func TestChannelSendRefusesBrokenChannels(t *testing.T) {
	t.Parallel()

	t.Run("a send on a closed channel surfaces as an interpreted panic", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		channel := make(chan int, 1)
		close(channel)
		registers.General[0] = reflect.ValueOf(channel)
		registers.Ints[1] = 1

		got := HandleChannelSend(vm, frame, registers, op(0, 1, uint8(isa.RegisterInt)))
		requireRuntimePanic(t, vm, got, "send on closed channel")
	})

	t.Run("an invalid channel register panics through the register guard", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		require.Panics(t, func() {
			HandleChannelSend(vm, frame, registers, op(0, 1, uint8(isa.RegisterInt)))
		}, "an unset general register is an invariant break, not a runtime fault")
	})
}

func TestChannelSendOnFullChannelParksUntilCancelled(t *testing.T) {
	t.Parallel()

	vm := newCancelledVM(t)
	builder := newBytecodeBuilder()
	builder.numRegisters = wideRegCounts(4)
	builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
	compiledFunction := builder.build()
	vm.prepareForExecution(compiledFunction)
	vm.PushFrame(compiledFunction)
	frame := &vm.CallStack[vm.FramePointer]
	registers := &frame.Registers

	channel := make(chan int)
	registers.General[0] = reflect.ValueOf(channel)
	registers.Ints[1] = 1

	got := HandleChannelSend(vm, frame, registers, op(0, 1, uint8(isa.RegisterInt)))
	require.Equal(t, opPanicError, got, "a send that can never complete must abort rather than deadlock")
	require.Error(t, vm.evalError, "the cancellation must surface as the evaluation error")
	require.Equal(t, uint32(1), vm.Cancelled.Load(), "the cancelled flag must agree with the surfaced error")
}

func TestChannelReceiveWritesValueAndCommaOk(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		channel any
		kind    isa.RegisterKind
		fill    func(any)
		wantOk  int64
		inspect func(*testing.T, *Registers)
	}{
		{
			name:    "a buffered int arrives in the int bank",
			channel: make(chan int, 1),
			kind:    isa.RegisterInt,
			fill:    func(c any) { c.(chan int) <- 5 },
			wantOk:  1,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(5), r.Ints[3]) },
		},
		{
			name:    "a buffered int64 arrives in the int bank",
			channel: make(chan int64, 1),
			kind:    isa.RegisterInt,
			fill:    func(c any) { c.(chan int64) <- -5 },
			wantOk:  1,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(-5), r.Ints[3]) },
		},
		{
			name:    "a buffered string arrives in the string bank",
			channel: make(chan string, 1),
			kind:    isa.RegisterString,
			fill:    func(c any) { c.(chan string) <- "value" },
			wantOk:  1,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "value", r.Strings[3]) },
		},
		{
			name:    "a buffered bool arrives in the bool bank",
			channel: make(chan bool, 1),
			kind:    isa.RegisterBool,
			fill:    func(c any) { c.(chan bool) <- true },
			wantOk:  1,
			inspect: func(t *testing.T, r *Registers) { require.True(t, r.Bools[3]) },
		},
		{
			name:    "a buffered float64 arrives in the float bank",
			channel: make(chan float64, 1),
			kind:    isa.RegisterFloat,
			fill:    func(c any) { c.(chan float64) <- 1.25 },
			wantOk:  1,
			inspect: func(t *testing.T, r *Registers) { require.InDelta(t, 1.25, r.Floats[3], 0) },
		},
		{
			name:    "a closed int channel yields the zero value with a false comma-ok",
			channel: make(chan int, 1),
			kind:    isa.RegisterInt,
			fill:    func(c any) { close(c.(chan int)) },
			wantOk:  0,
			inspect: func(t *testing.T, r *Registers) { require.Zero(t, r.Ints[3]) },
		},
		{
			name:    "a closed string channel yields the empty string",
			channel: make(chan string, 1),
			kind:    isa.RegisterString,
			fill:    func(c any) { close(c.(chan string)) },
			wantOk:  0,
			inspect: func(t *testing.T, r *Registers) { require.Empty(t, r.Strings[3]) },
		},
		{
			name:    "a named channel type falls to the reflect receive",
			channel: namedIntChannel(make(chan int, 1)),
			kind:    isa.RegisterInt,
			fill:    func(c any) { c.(namedIntChannel) <- 8 },
			wantOk:  1,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, int64(8), r.Ints[3]) },
		},
		{
			name:    "an interface channel is unwrapped to its dynamic value",
			channel: make(chan any, 1),
			kind:    isa.RegisterString,
			fill:    func(c any) { c.(chan any) <- "dynamic" },
			wantOk:  1,
			inspect: func(t *testing.T, r *Registers) { require.Equal(t, "dynamic", r.Strings[3]) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm, frame, registers := newExtWordFrame(t, op(3, uint8(tt.kind), 0))
			registers.General[0] = reflect.ValueOf(tt.channel)
			tt.fill(tt.channel)

			got := handleChannelReceive(vm, frame, registers, op(0, 2, 0))
			require.Equal(t, opContinue, got, "a receive that completes immediately never leaves the dispatch loop")
			require.Equal(t, 2, frame.ProgramCounter, "the extension word must be stepped over exactly once")
			require.Equal(t, tt.wantOk, registers.Ints[2], "the comma-ok flag lands in the int bank")
			tt.inspect(t, registers)
		})
	}
}

func TestChannelReceiveRefusesBrokenChannels(t *testing.T) {
	t.Parallel()

	t.Run("an invalid channel register panics through the register guard", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newExtWordFrame(t, op(3, uint8(isa.RegisterInt), 0))
		require.Panics(t, func() {
			handleChannelReceive(vm, frame, registers, op(0, 2, 0))
		}, "an unset general register is an invariant break, not a runtime fault")
	})

	t.Run("a receive that can never complete aborts on cancellation", func(t *testing.T) {
		t.Parallel()

		vm := newCancelledVM(t)
		builder := newBytecodeBuilder()
		builder.numRegisters = wideRegCounts(4)
		builder.Emit(isa.OpExt, 3, uint8(isa.RegisterInt), 0)
		builder.Emit(isa.OpDrillTier1, uint8(isa.SubOpDrillTier2), uint8(isa.SubOpTier2Return), 0)
		compiledFunction := builder.build()
		vm.prepareForExecution(compiledFunction)
		vm.PushFrame(compiledFunction)
		frame := &vm.CallStack[vm.FramePointer]
		registers := &frame.Registers
		registers.General[0] = reflect.ValueOf(make(chan int))

		got := handleChannelReceive(vm, frame, registers, op(0, 2, 0))
		require.Equal(t, opPanicError, got, "a receive that can never complete must abort rather than deadlock")
		require.Error(t, vm.evalError)
	})
}

func TestChannelCloseEndsTheChannelOnce(t *testing.T) {
	t.Parallel()

	t.Run("closing an open channel makes a later receive report not-ok", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		channel := make(chan int, 1)
		registers.General[0] = reflect.ValueOf(channel)

		got := handleChannelClose(vm, frame, registers, op(0, 0, 0))
		require.Equal(t, opContinue, got, "closing an open channel is not a fault")

		_, ok := <-channel
		require.False(t, ok, "the channel must report itself closed")
	})

	t.Run("closing a closed channel surfaces as an interpreted panic", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		channel := make(chan int, 1)
		close(channel)
		registers.General[0] = reflect.ValueOf(channel)

		got := handleChannelClose(vm, frame, registers, op(0, 0, 0))
		requireRuntimePanic(t, vm, got, "close of closed channel")
	})

	t.Run("an invalid channel register panics through the register guard", func(t *testing.T) {
		t.Parallel()

		vm, frame, registers := newStandardVM(t)
		require.Panics(t, func() {
			handleChannelClose(vm, frame, registers, op(0, 0, 0))
		}, "an unset general register is an invariant break, not a runtime fault")
	})
}

func TestTypedChannelHandleCacheRejectsUnassertableChannels(t *testing.T) {
	t.Parallel()

	t.Run("a nil channel has no address to key the cache with", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		_, ok := lookupTypedChannel[chan int](vm, reflect.ValueOf((chan int)(nil)))
		require.False(t, ok, "a nil channel must not be cached")
		require.Empty(t, vm.typedHandleCache)
	})

	t.Run("a second lookup is served from the cache", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		channel := reflect.ValueOf(make(chan int, 1))

		first, ok := lookupTypedChannel[chan int](vm, channel)
		require.True(t, ok)
		require.Len(t, vm.typedHandleCache, 1, "the first lookup must populate the cache")

		second, ok := lookupTypedChannel[chan int](vm, channel)
		require.True(t, ok)
		require.Equal(t, first, second, "the cached handle must be the same channel")
	})

	t.Run("a cached handle of the wrong element type is reported as a miss", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		channel := reflect.ValueOf(make(chan int, 1))

		_, ok := lookupTypedChannel[chan int](vm, channel)
		require.True(t, ok)

		_, ok = lookupTypedChannel[chan string](vm, channel)
		require.False(t, ok, "the cache is keyed by address, so the assertion still has to hold")
	})

	t.Run("a named channel type defeats the bare type assertion", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		_, ok := lookupTypedChannel[chan int](vm, reflect.ValueOf(namedIntChannel(make(chan int, 1))))
		require.False(t, ok, "chan int and a named chan int are distinct dynamic types")
	})
}

func TestTypedChannelKindGuardsRefuseMismatchedBanks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourceKind isa.RegisterKind
		want       bool
	}{
		{name: "the matching bank reaches the typed send", sourceKind: isa.RegisterInt, want: true},
		{name: "a float source is refused for an int channel", sourceKind: isa.RegisterFloat, want: false},
		{name: "a string source is refused for an int channel", sourceKind: isa.RegisterString, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			vm := newTestVM(t)
			registers := standardRegisters()
			registers.Ints[1] = 3
			channel := reflect.ValueOf(make(chan int, 1))

			require.Equal(t, tt.want, trySendTypedChannel(vm, channel, &registers, 1, tt.sourceKind))
		})
	}
}

func TestTypedChannelPollingDistinguishesEmptyFromClosed(t *testing.T) {
	t.Parallel()

	t.Run("an empty open channel reports that nothing was received", func(t *testing.T) {
		t.Parallel()

		value, channelOpen, received := pollTypedChannel(make(chan int, 1))
		require.False(t, received, "a poll must never block")
		require.False(t, channelOpen, "no receive happened, so the open flag carries no information")
		require.Zero(t, value)
	})

	t.Run("a closed channel reports a completed receive of the zero value", func(t *testing.T) {
		t.Parallel()

		channel := make(chan int, 1)
		close(channel)

		value, channelOpen, received := pollTypedChannel(channel)
		require.True(t, received, "a closed channel always has a result ready")
		require.False(t, channelOpen)
		require.Zero(t, value)
	})

	t.Run("a full buffer refuses a further offer", func(t *testing.T) {
		t.Parallel()

		channel := make(chan int, 1)
		require.True(t, offerTypedChannel(channel, 1), "the first value fits the buffer")
		require.False(t, offerTypedChannel(channel, 2), "the second would block")
	})
}

func TestChannelWakeCasesSkipInactiveSources(t *testing.T) {
	t.Parallel()

	done := make(chan struct{})
	panicWake := make(chan struct{})

	tests := []struct {
		name      string
		done      <-chan struct{}
		panicWake <-chan struct{}
		wantCount int
		wantMap   [2]int
	}{
		{name: "neither wake source is active", wantCount: 1, wantMap: [2]int{-1, -1}},
		{name: "only the context is cancellable", done: done, wantCount: 2, wantMap: [2]int{1, -1}},
		{name: "only the goroutine panic signal is live", panicWake: panicWake, wantCount: 2, wantMap: [2]int{-1, 1}},
		{name: "both wake sources are active", done: done, panicWake: panicWake, wantCount: 3, wantMap: [2]int{1, 2}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cases := make([]reflect.SelectCase, 3)
			count, cancelIndex, panicIndex := appendWakeCases(cases, 1, tt.done, tt.panicWake)
			require.Equal(t, tt.wantCount, count, "the user's own case always occupies index zero")
			require.Equal(t, tt.wantMap[0], cancelIndex)
			require.Equal(t, tt.wantMap[1], panicIndex)
		})
	}
}

func TestChannelSelectWakesOnCancellation(t *testing.T) {
	t.Parallel()

	t.Run("a receive wakes on the cancelled context rather than the channel", func(t *testing.T) {
		t.Parallel()

		done := make(chan struct{})
		close(done)

		_, ok, wake := selectChannelReceive(reflect.ValueOf(make(chan int)), done, nil)
		require.False(t, ok)
		require.Equal(t, blockWakeCancelled, wake)
	})

	t.Run("a receive wakes on a sibling goroutine panic", func(t *testing.T) {
		t.Parallel()

		panicWake := make(chan struct{})
		close(panicWake)

		_, ok, wake := selectChannelReceive(reflect.ValueOf(make(chan int)), nil, panicWake)
		require.False(t, ok)
		require.Equal(t, blockWakePanic, wake)
	})

	t.Run("a receive that finds a value reports a value wake", func(t *testing.T) {
		t.Parallel()

		channel := make(chan int, 1)
		channel <- 4

		value, ok, wake := selectChannelReceive(reflect.ValueOf(channel), nil, make(chan struct{}))
		require.True(t, ok)
		require.Equal(t, blockWakeValue, wake)
		require.Equal(t, 4, value.Interface())
	})

	t.Run("a send wakes on the cancelled context", func(t *testing.T) {
		t.Parallel()

		done := make(chan struct{})
		close(done)

		wake := selectChannelSend(reflect.ValueOf(make(chan int)), reflect.ValueOf(1), done, nil)
		require.Equal(t, blockWakeCancelled, wake)
	})

	t.Run("a send wakes on a sibling goroutine panic", func(t *testing.T) {
		t.Parallel()

		panicWake := make(chan struct{})
		close(panicWake)

		wake := selectChannelSend(reflect.ValueOf(make(chan int)), reflect.ValueOf(1), nil, panicWake)
		require.Equal(t, blockWakePanic, wake)
	})

	t.Run("a send into free buffer space reports a value wake", func(t *testing.T) {
		t.Parallel()

		channel := make(chan int, 1)

		wake := selectChannelSend(reflect.ValueOf(channel), reflect.ValueOf(6), nil, make(chan struct{}))
		require.Equal(t, blockWakeValue, wake)
		require.Equal(t, 6, <-channel)
	})
}

func TestBlockingWakeSurfacesTheRightCause(t *testing.T) {
	t.Parallel()

	t.Run("a value wake continues dispatch", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		require.Equal(t, opContinue, vm.surfaceBlockingWake(blockWakeValue))
		require.NoError(t, vm.evalError)
	})

	t.Run("a cancellation wake records the context cause", func(t *testing.T) {
		t.Parallel()

		vm := newCancelledVM(t)
		require.Equal(t, opPanicError, vm.surfaceBlockingWake(blockWakeCancelled))
		require.Error(t, vm.evalError)
		require.Equal(t, uint32(1), vm.Cancelled.Load())
	})

	t.Run("a cancellation wake on a live context falls back to the unblocked sentinel", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		require.Equal(t, opPanicError, vm.surfaceBlockingWake(blockWakeCancelled))
		require.ErrorIs(t, vm.evalError, errBlockingOpUnblocked)
	})

	t.Run("a panic wake with no recorded panic falls back to the unblocked sentinel", func(t *testing.T) {
		t.Parallel()

		vm := newTestVM(t)
		require.Equal(t, opPanicError, vm.surfaceBlockingWake(blockWakePanic))
		require.ErrorIs(t, vm.evalError, errBlockingOpUnblocked)
	})
}

func TestGuardChannelOpConvertsPanicsIntoValues(t *testing.T) {
	t.Parallel()

	t.Run("a clean operation reports no panic", func(t *testing.T) {
		t.Parallel()

		ran := false
		recovered, panicked := guardChannelOp(func() { ran = true })
		require.True(t, ran)
		require.False(t, panicked)
		require.Nil(t, recovered)
	})

	t.Run("a panicking operation hands back the panic value", func(t *testing.T) {
		t.Parallel()

		recovered, panicked := guardChannelOp(func() { panic("boom") })
		require.True(t, panicked)
		require.Equal(t, "boom", recovered)
	})
}
