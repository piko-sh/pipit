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

	"pipit.sh/pipit/internal/isa"
)

const (
	// blockWakeValue indicates the channel or select operation completed normally and
	// produced a value.
	blockWakeValue blockWake = iota

	// blockWakeCancelled indicates the VM's context was cancelled while the operation was
	// parked.
	blockWakeCancelled

	// blockWakePanic indicates a sibling goroutine panicked while the operation was parked.
	blockWakePanic
)

var (
	// reflectTypeChanElemInt is the canonical reflect.Type for an int channel element; used
	// by trySendTypedChannel pointer-equality dispatch.
	reflectTypeChanElemInt = reflect.TypeFor[int]()

	// reflectTypeChanElemInt64 is the canonical reflect.Type for an int64 channel element.
	//
	reflectTypeChanElemInt64 = reflect.TypeFor[int64]()

	// reflectTypeChanElemString is the canonical reflect.Type for a string channel element.
	//
	reflectTypeChanElemString = reflect.TypeFor[string]()

	// reflectTypeChanElemBool is the canonical reflect.Type for a bool channel element.
	//
	reflectTypeChanElemBool = reflect.TypeFor[bool]()

	// reflectTypeChanElemFloat64 is the canonical reflect.Type for a float64 channel
	// element.
	//
	reflectTypeChanElemFloat64 = reflect.TypeFor[float64]()
)

// blockWake reports how a ctx-aware blocking channel or select operation completed: with
// a value, via context cancellation, or because a sibling goroutine panicked.
type blockWake uint8

// surfaceBlockingWake converts a non-value wake reason into the terminal OpResult that
// aborts dispatch with the right error.
//
// Names the wake reasons that end the block. Any other blockWake means the wait resumes,
// which the default expresses by continuing.
//
// Takes wake (blockWake) which is the wake reason to surface.
//
// Returns opContinue for blockWakeValue, otherwise opPanicError after recording the
// cause.
func (vm *VM) surfaceBlockingWake(wake blockWake) OpResult {
	switch wake {
	case blockWakeCancelled:
		return vm.surfaceContextCancellation()
	case blockWakePanic:
		return vm.surfaceGoroutinePanicAbort()
	default:
		return opContinue
	}
}

// surfaceContextCancellation aborts dispatch because the VM's context was cancelled while
// a blocking channel or select operation was parked. It records the cancelled flag so
// subsequent periodic checks agree and stores the context cause as the evaluation error.
//
// Returns opPanicError so the dispatch loop unwinds.
func (vm *VM) surfaceContextCancellation() OpResult {
	vm.Cancelled.Store(1)
	if vm.ctx.Err() != nil {
		vm.evalError = cancellationError(vm.ctx)
	} else {
		vm.evalError = errBlockingOpUnblocked
	}
	return opPanicError
}

// surfaceGoroutinePanicAbort aborts dispatch because a sibling goroutine panicked while
// this VM was parked on a blocking channel or select operation, surfacing the recorded
// panic so it propagates instead of deadlocking.
//
// Returns opPanicError so the dispatch loop unwinds.
func (vm *VM) surfaceGoroutinePanicAbort() OpResult {
	if info := vm.Globals.goroutinePanic.Load(); info != nil {
		vm.evalError = newGoroutinePanicError(info.value)
	} else {
		vm.evalError = errBlockingOpUnblocked
	}
	return opPanicError
}

// HandleChannelSend sends a value on a channel by reading the value from the appropriate
// register and converting it to the channel's element type.
//
// The send is attempted without blocking first, through the typed-channel closures and
// then reflect's TrySend, so a send that can complete immediately never allocates a
// select case set and never observes a cancellation that raced with it. Only a send that
// would block releases the interpreter lock and parks in the cancellable select.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the channel and value.
// Takes instruction (instruction) which encodes the channel and value regs.
//
// Returns OpResult indicating the next execution step.
func HandleChannelSend(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	channel := registers.General[instruction.A]
	if !channel.IsValid() {
		vMPanicInvalidRegister("handleChannelSend", "channel", instruction.A, instruction, frame, registers)
	}

	sent := false
	var value reflect.Value
	recovered, panicked := guardChannelOp(func() {
		if trySendTypedChannel(vm, channel, registers, instruction.B, isa.RegisterKind(instruction.C)) {
			sent = true
			return
		}
		ext := isa.NewInstruction(0, instruction.B, instruction.C, 0)
		value = materialiseArenaValueUnconditional(vm.Arena, buildSelectSendValue(vm, registers, ext, channel.Type().Elem()))
		sent = channel.TrySend(value)
	})
	if panicked {
		return raiseNativePanicAsInterpreted(vm, recovered)
	}
	if sent {
		return opContinue
	}
	return blockingChannelSend(vm, channel, value)
}

// handleChannelReceive receives a value from a channel and stores it in the destination
// register along with a boolean indicating success.
//
// The receive is attempted without blocking first, through the typed-channel closures and
// then reflect's TryRecv, so a value that is already available (or a closed channel) is
// consumed before any cancellation is observed. Only a receive that would block releases
// the interpreter lock and parks in the cancellable select.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the destination extension word.
// Takes registers (*Registers) which holds the channel and destination.
// Takes instruction (instruction) which encodes the channel register.
//
// Returns OpResult indicating the next execution step.
func handleChannelReceive(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	channel := registers.General[instruction.A]
	if !channel.IsValid() {
		vMPanicInvalidRegister("handleChannelReceive", "channel", instruction.A, instruction, frame, registers)
	}

	if tryRecvTypedChannel(vm, channel, registers, instruction.B, extensionWord.A, isa.RegisterKind(extensionWord.B)) {
		return opContinue
	}
	if result, ok := channel.TryRecv(); ok || result.IsValid() {
		writeChannelReceiveResult(registers, instruction.B, extensionWord, result, ok)
		return opContinue
	}

	done := vm.ctx.Done()
	panicWake := vm.Globals.goroutinePanicWakeChan()
	wake := blockWakeValue
	var result reflect.Value
	var ok bool
	token := vm.enterBlocking(vm.Globals.isInterpretedChannel(channel))
	released := vm.releaseAroundBlock()
	if done == nil && panicWake == nil {
		result, ok = channel.Recv()
	} else {
		result, ok, wake = selectChannelReceive(channel, done, panicWake)
	}
	vm.reacquireAfterBlock(released)
	vm.leaveBlocking(token)
	if wake != blockWakeValue {
		return vm.surfaceBlockingWake(wake)
	}
	writeChannelReceiveResult(registers, instruction.B, extensionWord, result, ok)
	return opContinue
}

// blockingChannelSend parks on a send that could not complete without blocking.
//
// Takes vm (*VM) which is the executing VM.
// Takes channel (reflect.Value) which is the channel value.
// Takes value (reflect.Value) which is the heap-materialised value to send.
//
// Returns OpResult indicating the next execution step.
func blockingChannelSend(vm *VM, channel, value reflect.Value) OpResult {
	done := vm.ctx.Done()
	panicWake := vm.Globals.goroutinePanicWakeChan()
	wake := blockWakeValue
	token := vm.enterBlocking(vm.Globals.isInterpretedChannel(channel))
	released := vm.releaseAroundBlock()
	recovered, panicked := guardChannelOp(func() {
		if done == nil && panicWake == nil {
			channel.Send(value)
			return
		}
		wake = selectChannelSend(channel, value, done, panicWake)
	})
	vm.reacquireAfterBlock(released)
	vm.leaveBlocking(token)
	if panicked {
		return raiseNativePanicAsInterpreted(vm, recovered)
	}
	if wake != blockWakeValue {
		return vm.surfaceBlockingWake(wake)
	}
	return opContinue
}

// trySendTypedChannel sends a typed-bank value via the cached channel handle without
// blocking.
//
// Uses the Go-typed channel handle cached on vm.typedHandleCache, avoiding the
// reflect.Value boxing + reflect.Value.Send overhead that the slow path pays. Returns
// true on a successful typed send; false when the channel's element type isn't one of the
// supported primitive types, when the source register kind doesn't align with the
// channel's element type, or when the send would block.
//
// Takes vm (*VM) which owns the typed handle cache.
// Takes channel (reflect.Value) which is the channel value.
// Takes registers (*Registers) which is the active register file.
// Takes sourceRegister (uint8) which is the source register index in the typed bank
// identified by sourceKind.
// Takes sourceKind (isa.RegisterKind) which selects the source bank.
//
// Returns true when the typed send fired, false when the slow reflect path must run.
func trySendTypedChannel(vm *VM, channel reflect.Value, registers *Registers, sourceRegister uint8, sourceKind isa.RegisterKind) bool {
	switch channel.Type().Elem() {
	case reflectTypeChanElemInt:
		return sendTypedChannel[chan int](vm, channel, sourceKind, isa.RegisterInt, func(typed chan int) bool {
			return offerTypedChannel(typed, int(registers.Ints[sourceRegister]))
		})
	case reflectTypeChanElemInt64:
		return sendTypedChannel[chan int64](vm, channel, sourceKind, isa.RegisterInt, func(typed chan int64) bool {
			return offerTypedChannel(typed, registers.Ints[sourceRegister])
		})
	case reflectTypeChanElemString:
		return sendTypedChannel[chan string](vm, channel, sourceKind, isa.RegisterString, func(typed chan string) bool {
			return offerTypedChannel(typed, materialiseString(vm.Arena, registers.Strings[sourceRegister]))
		})
	case reflectTypeChanElemBool:
		return sendTypedChannel[chan bool](vm, channel, sourceKind, isa.RegisterBool, func(typed chan bool) bool {
			return offerTypedChannel(typed, registers.Bools[sourceRegister])
		})
	case reflectTypeChanElemFloat64:
		return sendTypedChannel[chan float64](vm, channel, sourceKind, isa.RegisterFloat, func(typed chan float64) bool {
			return offerTypedChannel(typed, registers.Floats[sourceRegister])
		})
	default:
		return false
	}
}

// offerTypedChannel sends value on typed only if the send can complete immediately.
//
// Takes typed (chan T) which is the Go-typed channel handle.
// Takes value (T) which is the value to send; arena strings must already be materialised
// because a channel may outlive this execution's arena.
//
// Returns true when the value was handed to the channel, false when the send would block.
func offerTypedChannel[T any](typed chan T, value T) bool {
	select {
	case typed <- value:
		return true
	default:
		return false
	}
}

// sendTypedChannel performs a kind-checked non-blocking typed-channel send via the per-VM
// cache. Returns false when the source register kind doesn't match the channel's element
// kind, when the cache lookup fails because the underlying channel isn't actually of type
// T, or when the send would block.
//
// Takes vm (*VM) which owns the typed handle cache.
// Takes channel (reflect.Value) which is the channel value.
// Takes sourceKind (isa.RegisterKind) which is the source register's reported kind at the
// call site.
// Takes expectedKind (isa.RegisterKind) which is the kind required for the channel's
// element type.
// Takes send (func(T) bool) which is the callback that performs the actual typed send
// once the cache hit is confirmed and reports whether it completed.
//
// Returns true when the typed send completed, false otherwise.
func sendTypedChannel[T any](vm *VM, channel reflect.Value, sourceKind, expectedKind isa.RegisterKind, send func(T) bool) bool {
	if sourceKind != expectedKind {
		return false
	}
	typed, ok := lookupTypedChannel[T](vm, channel)
	if !ok {
		return false
	}
	return send(typed)
}

// lookupTypedChannel returns the cached typed-handle for the channel.
//
// On cache hit the stored handle is returned; on first encounter the handle is extracted
// from the reflect.Value and cached. The bool reports whether the underlying channel
// actually has the expected element type. The bool is false when channel.Type() reports a
// named type such as `type Named chan int` rather than the bare `chan int` the type
// assertion targets.
//
// Takes vm (*VM) which owns the cache.
// Takes channel (reflect.Value) which is the channel value.
//
// Returns the typed handle.
// Returns a bool indicating whether the type assertion succeeded.
func lookupTypedChannel[T any](vm *VM, channel reflect.Value) (T, bool) {
	var zero T
	pointer := channel.Pointer()
	if pointer == 0 {
		return zero, false
	}
	if cached, ok := vm.typedHandleCache[pointer]; ok {
		typed, ok := cached.(T)
		return typed, ok
	}
	extracted := channel.Interface()
	typed, ok := extracted.(T)
	if !ok {
		return zero, false
	}
	if vm.typedHandleCache == nil {
		vm.typedHandleCache = make(map[uintptr]any, initialTypedHandleCacheCapacity)
	}
	if len(vm.typedHandleCache) >= maxTypedHandleCacheEntries {
		return typed, true
	}
	vm.typedHandleCache[pointer] = extracted
	return typed, true
}

// writeChannelReceiveResult stores a received value and its comma-ok flag into the
// destination registers. Interface results are unwrapped to their dynamic value first so
// the destination bank receives the concrete type.
//
// Takes registers (*Registers) which receives the value and ok flag.
// Takes okRegister (uint8) which is the int-bank register for the comma-ok bool.
// Takes extensionWord (instruction) whose a/b fields carry the destination register index
// and kind.
// Takes result (reflect.Value) which is the received value.
// Takes ok (bool) which is true when the channel produced a value (false when closed).
func writeChannelReceiveResult(registers *Registers, okRegister uint8, extensionWord isa.Instruction, result reflect.Value, ok bool) {
	registers.Ints[okRegister] = boolToInt64(ok)
	if result.IsValid() && result.Kind() == reflect.Interface {
		result = result.Elem()
	}
	writeRegisterValue(registers, extensionWord.A, isa.RegisterKind(extensionWord.B), result)
}

// selectChannelReceive performs a receive with wake cancellation.
//
// Also wakes on context cancellation or a sibling goroutine panic. done and panicWake are
// each nil when their wake source is inactive; at least one is non-nil (the caller takes
// a plain receive otherwise). The fixed-size case array avoids a per-receive heap
// allocation.
//
// Takes channel (reflect.Value) which is the channel to receive from.
// Takes done (<-chan struct{}) which is the context Done channel, or nil.
// Takes panicWake (<-chan struct{}) which is the goroutine-panic signal, or nil.
//
// Returns the received value, its comma-ok flag, and the wake reason.
func selectChannelReceive(channel reflect.Value, done, panicWake <-chan struct{}) (reflect.Value, bool, blockWake) {
	var storage [3]reflect.SelectCase
	storage[0] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: channel}
	count, cancelIndex, panicIndex := appendWakeCases(storage[:], 1, done, panicWake)
	chosen, received, recvOK := reflect.Select(storage[:count])
	if chosen == cancelIndex {
		return reflect.Value{}, false, blockWakeCancelled
	}
	if chosen == panicIndex {
		return reflect.Value{}, false, blockWakePanic
	}
	return received, recvOK, blockWakeValue
}

// selectChannelSend performs a send with wake cancellation.
//
// Also wakes on context cancellation or a sibling goroutine panic. Semantics mirror
// selectChannelReceive. A send on a closed channel still panics; the caller guards the
// call with guardChannelOp.
//
// Takes channel (reflect.Value) which is the channel to send on.
// Takes value (reflect.Value) which is the value to send.
// Takes done (<-chan struct{}) which is the context Done channel, or nil.
// Takes panicWake (<-chan struct{}) which is the goroutine-panic signal, or nil.
//
// Returns the wake reason.
func selectChannelSend(channel, value reflect.Value, done, panicWake <-chan struct{}) blockWake {
	var storage [3]reflect.SelectCase
	storage[0] = reflect.SelectCase{Dir: reflect.SelectSend, Chan: channel, Send: value}
	count, cancelIndex, panicIndex := appendWakeCases(storage[:], 1, done, panicWake)
	chosen, _, _ := reflect.Select(storage[:count])
	if chosen == cancelIndex {
		return blockWakeCancelled
	}
	if chosen == panicIndex {
		return blockWakePanic
	}
	return blockWakeValue
}

// appendWakeCases fills the context-cancellation and goroutine-panic receive arms into
// cases starting at index start, skipping each arm whose channel is nil.
//
// Takes cases ([]reflect.SelectCase) which is the backing array to populate.
// Takes start (int) which is the first free index (after the user's own cases).
// Takes done (<-chan struct{}) which is the context Done channel, or nil.
// Takes panicWake (<-chan struct{}) which is the goroutine-panic signal, or nil.
//
// Returns the total case count, the cancel-arm index (-1 when absent), and the panic-arm
// index (-1 when absent).
func appendWakeCases(cases []reflect.SelectCase, start int, done, panicWake <-chan struct{}) (count, cancelIndex, panicIndex int) {
	count, cancelIndex, panicIndex = start, -1, -1
	if done != nil {
		cancelIndex = count
		cases[count] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(done)}
		count++
	}
	if panicWake != nil {
		panicIndex = count
		cases[count] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: reflect.ValueOf(panicWake)}
		count++
	}
	return count, cancelIndex, panicIndex
}

// tryRecvTypedChannel attempts a non-blocking receive from a Go-typed channel handle
// cached on vm.typedHandleCache, dispatching directly into the destination register's
// typed bank without paying for reflect.Value.Recv's per-call result allocation.
//
// Takes vm (*VM) which owns the typed handle cache.
// Takes channel (reflect.Value) which is the channel value.
// Takes registers (*Registers) which is the active register file.
// Takes okRegister (uint8) which receives the comma-ok bool as int64.
// Takes destinationRegister (uint8) which is the result register index in the typed bank
// identified by destinationKind.
// Takes destinationKind (isa.RegisterKind) which selects the destination bank.
//
// Returns true when the typed receive fired, false when the slow reflect path must run.
func tryRecvTypedChannel(vm *VM, channel reflect.Value, registers *Registers, okRegister, destinationRegister uint8, destinationKind isa.RegisterKind) bool {
	switch channel.Type().Elem() {
	case reflectTypeChanElemInt:
		return recvTypedChannel[chan int](vm, channel, destinationKind, isa.RegisterInt, func(typed chan int) bool {
			return receiveTypedInto(typed, registers, okRegister, func(value int) { registers.Ints[destinationRegister] = int64(value) })
		})
	case reflectTypeChanElemInt64:
		return recvTypedChannel[chan int64](vm, channel, destinationKind, isa.RegisterInt, func(typed chan int64) bool {
			return receiveTypedInto(typed, registers, okRegister, func(value int64) { registers.Ints[destinationRegister] = value })
		})
	case reflectTypeChanElemString:
		return recvTypedChannel[chan string](vm, channel, destinationKind, isa.RegisterString, func(typed chan string) bool {
			return receiveTypedInto(typed, registers, okRegister, func(value string) { registers.Strings[destinationRegister] = value })
		})
	default:
		return tryRecvTypedChannelScalar(vm, channel, registers, okRegister, destinationRegister, destinationKind)
	}
}

// tryRecvTypedChannelScalar completes tryRecvTypedChannel for the bool and float64
// element types.
//
// Takes vm (*VM) which owns the typed handle cache.
// Takes channel (reflect.Value) which is the channel value.
// Takes registers (*Registers) which is the active register file.
// Takes okRegister (uint8) which receives the comma-ok bool as int64.
// Takes destinationRegister (uint8) which is the result register index in the typed bank
// identified by destinationKind.
// Takes destinationKind (isa.RegisterKind) which selects the destination bank.
//
// Returns true when the typed receive fired, false when the slow reflect path must run.
func tryRecvTypedChannelScalar(vm *VM, channel reflect.Value, registers *Registers, okRegister, destinationRegister uint8, destinationKind isa.RegisterKind) bool {
	switch channel.Type().Elem() {
	case reflectTypeChanElemBool:
		return recvTypedChannel[chan bool](vm, channel, destinationKind, isa.RegisterBool, func(typed chan bool) bool {
			return receiveTypedInto(typed, registers, okRegister, func(value bool) { registers.Bools[destinationRegister] = value })
		})
	case reflectTypeChanElemFloat64:
		return recvTypedChannel[chan float64](vm, channel, destinationKind, isa.RegisterFloat, func(typed chan float64) bool {
			return receiveTypedInto(typed, registers, okRegister, func(value float64) { registers.Floats[destinationRegister] = value })
		})
	default:
		return false
	}
}

// pollTypedChannel receives from typed only if a value (or the closed state) is
// immediately available.
//
// Takes typed (chan T) which is the Go-typed channel handle.
//
// Returns the received value, whether the channel was open, and whether the receive
// completed at all (false when it would have blocked).
func pollTypedChannel[T any](typed chan T) (value T, channelOpen, received bool) {
	select {
	case value, channelOpen = <-typed:
		return value, channelOpen, true
	default:
		return value, false, false
	}
}

// receiveTypedInto polls typed and, when a value or the closed state is immediately
// available, hands the value to store and records the comma-ok flag.
//
// Takes typed (chan T) which is the Go-typed channel handle.
// Takes registers (*Registers) which receives the comma-ok flag.
// Takes okRegister (uint8) which is the int-bank register for the comma-ok flag.
// Takes store (func(T)) which writes the received value into its destination bank.
//
// Returns true when the receive completed, false when it would have blocked.
func receiveTypedInto[T any](typed chan T, registers *Registers, okRegister uint8, store func(T)) bool {
	value, channelOpen, received := pollTypedChannel(typed)
	if !received {
		return false
	}
	store(value)
	registers.Ints[okRegister] = boolToInt64(channelOpen)
	return true
}

// recvTypedChannel performs a kind-checked non-blocking typed-channel receive via the
// per-VM cache, returning false when the destination register kind doesn't match the
// channel's element kind, when the cached typed-handle assertion fails, or when the
// receive would block.
//
// Takes vm (*VM) which owns the typed handle cache.
// Takes channel (reflect.Value) which is the channel value.
// Takes destinationKind (isa.RegisterKind) which is the register kind the caller expects
// to write into.
// Takes expectedKind (isa.RegisterKind) which is the kind required for the channel's
// element type.
// Takes recv (func(T) bool) which performs the receive and returns true when complete.
//
// Returns true when the typed receive completed, false otherwise.
func recvTypedChannel[T any](vm *VM, channel reflect.Value, destinationKind, expectedKind isa.RegisterKind, recv func(T) bool) bool {
	if destinationKind != expectedKind {
		return false
	}
	typed, ok := lookupTypedChannel[T](vm, channel)
	if !ok {
		return false
	}
	return recv(typed)
}

// handleChannelClose closes the channel stored in the general register bank at the index
// specified by the instruction.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the channel to close.
// Takes instruction (instruction) which encodes the channel register index.
//
// Returns OpResult indicating the next execution step.
func handleChannelClose(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	channel := registers.General[instruction.A]
	if !channel.IsValid() {
		vMPanicInvalidRegister("handleChannelClose", "channel", instruction.A, instruction, frame, registers)
	}
	if recovered, panicked := guardChannelOp(func() { channel.Close() }); panicked {
		return raiseNativePanicAsInterpreted(vm, recovered)
	}
	return opContinue
}

// guardChannelOp runs op and converts any panic into a returned recovery value.
//
// Used to catch runtime panics from channel send/receive/close operations (e.g. "send on
// closed channel") so handlers can re-raise them as interpreted panics rather than
// terminate the host process.
//
// Takes op (func()) which is the channel operation to guard.
//
// Returns recovered (any) which is the panic value (nil when op completed normally).
// Returns panicked (bool) which is true when op panicked, false otherwise.
func guardChannelOp(op func()) (recovered any, panicked bool) {
	defer func() {
		if r := recover(); r != nil {
			recovered = r
			panicked = true
		}
	}()
	op()
	return nil, false
}
