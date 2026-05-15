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

// # Execution model
//
// This file is documentation only. It records the contract the dispatch code keeps and
// that every handler, trampoline and pass may rely on.
//
// # The five frame-push paths
//
// A CallFrame is pushed by exactly five paths, and each one leaves the frame in the same
// state: Function, ProgramCounter = 0, Registers allocated for the callee's register
// counts (from the arena when one is attached, else heap slices), deferBase = the defer
// stack length, and no upvalues or return destination unless the path sets them.
//
//  1. VM.PushFrame (vm.go): the general path used by the Go call handlers and by
//     execution entry; guards the call depth and records the arena save point.
//  2. pushCompiledFrame (vm_handler_misc_calls_method_dispatch.go): the method-call path,
//     which also records the site's return destinations and the variadic slice type.
//  3. handlerCallInline / asmSetupCalleeGeneralBank (the generated assembly and
//     asm_call_trampolines.go): the assembly call path. The assembly writes the frame
//     fields itself from the AsmCallInfo table; asmSetupCalleeGeneralBank is the Go
//     trampoline it calls when the callee has a general bank to allocate.
//  4. runTinyLeafInline (tiny_leaf.go): no frame at all. A callee classified as a tiny
//     leaf (one field read or write) runs inside the caller's frame; the classification
//     is the proof that it needs no registers, defers or upvalues of its own.
//  5. VM.pushDeferredFrame (defer.go): the unwind path, which places the deferred call's
//     arguments before the frame becomes current and swaps the closure root.
//
// # Path A and path B
//
// On amd64 and arm64 without the safe tag, bytecode runs on path A: the assembly dispatch
// loop (dispatchLoop) threads through handlers for the register-only opcodes. Everything
// else is path B: the loop exits to Go with an exit reason in the dispatchContext,
// VM.handleDispatchExit services it, and runPathBBatch keeps running consecutive
// Go-fallback opcodes through the flat switch before handing control back. The safe, wasm
// and other-architecture lanes run path B alone through VM.run.
//
// Exit protocol: the assembly writes exitReason, exitProgramCounter and the frame pointer
// into the dispatchContext and returns; Go re-derives frame and registers from
// VM.FramePointer, services the exit, and either returns (loopReturn), re-enters the
// assembly with the same context (loopContinue), or rebuilds the context's cached
// pointers first (loopRebuild, after a frame change). Terminal results resolve through
// VM.resolveTerminalResult on every path, so a divide by zero, a panic, a stack overflow
// or program completion means the same thing whichever lane produced it.
//
// # Who owns what
//
// Four structures hold interpreter state, and the phase says which one is authoritative:
//
//   - VM: the call stack, frame pointer, defer stack, globals, limits, cost budget and
//     the per-VM caches (nativeState, goroutineState, deferState). Authoritative whenever
//     Go code runs.
//   - CallFrame: the program counter and the register banks of one activation.
//     Authoritative for its registers at all times; for its program counter only while Go
//     runs (the assembly keeps the live counter in the context and writes it back on
//     exit).
//   - dispatchContext: the assembly's view of the current frame: base pointers of every
//     register bank and constant table, the code base and length, the program counter,
//     the call stack base and the exit fields. Authoritative only between entering and
//     leaving dispatchLoop; every Go-side change to a frame or the arena must be followed
//     by rebuildDispatchPointers (or a fresh buildDispatchContext) before re-entry.
//   - RegisterArena: the slabs the register banks live in. Authoritative for allocation;
//     a GC checkpoint (MinorGC) may retire slabs, which is why runDispatchCheckpoint
//     rebuilds the context's pointers afterwards and why checkpoints run only at the top
//     of the dispatch loop, the one moment when all state is synchronised into frames.
//
// # Debugger safe points
//
// A VM debugged through VMLimits.Debug parks at four kinds of safe point, and at no
// other:
//
//   - The top of the Go dispatch loop (run), before each instruction: the per-instruction
//     check is a nil test on Limits.Debug and one atomic load of the session's active
//     flag until a breakpoint, step, pause request or stop exists. The assembly loop is
//     never entered while a session is installed, so this check sees every instruction.
//   - The return from a blocking operation (reacquireAfterBlock), before the operation's
//     result is stored: a thread blocked on a channel, select or blocking native is
//     reported as blocked, not running, and parks as soon as it wakes if the world
//     stopped meanwhile.
//   - The start of an interpreted panic (debugPanicPoint), after beginPanicFrames and
//     before panicking is set, so a pause-on-panic sees the frames intact and runs before
//     any deferred function or recover.
//   - Registration: run() registers the VM with the session on its outermost entry and
//     deregisters it on exit (debugEnter/debugLeave), so goroutine, callback and
//     bound-method VMs appear as threads for exactly as long as they execute.
//
// While a thread is parked its frames, registers and arena are immutable and the arena is
// neither reset nor recycled, which is what makes inspection from the debugger's
// goroutine safe. Parking releases the safe-mode interpreter lock and returns on a
// resume, on the execution's cancellation or on the debugger's stop; a stop makes run()
// return fault.ErrDebuggerStop, which cancellationError passes through bare and
// surfaceGoroutineRunError treats as silent.
//
// # Initialisation order
//
// The generated per-architecture init (vm_dispatch_<arch>_generated.go) runs the declared
// step list: initJumpTable, the CPU-feature patches, then installDispatchTables, which
// installs the tier dispatchers, the sub-op jump tables, the per-op direct exits, the
// flat jump table and finally the Go-fallback opcode set. The order is load-bearing and
// is emitted from cmd/asmgen's step list, not from the order the installers happen to be
// declared in.
