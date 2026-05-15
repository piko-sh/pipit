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

const (
	// asmMaxInlineReturns is the largest number of scalar results the inline return handlers
	// copy bank to bank; call sites with more results take the Go return path.
	asmMaxInlineReturns = 4

	// exitEndOfCode indicates the dispatch loop exited because the program counter reached
	// the end of the code.
	exitEndOfCode int64 = 0

	// exitPathB indicates the dispatch loop exited because a tier 2 opcode requires Go-side
	// handling.
	exitPathB int64 = 1

	// exitDivByZero indicates the dispatch loop exited due to a division by zero error.
	exitDivByZero int64 = 2

	// exitCall indicates the dispatch loop exited to perform a function call via Go.
	exitCall int64 = 3

	// exitReturn indicates the dispatch loop exited to perform a function return via Go.
	exitReturn int64 = 4

	// exitReturnVoid indicates the dispatch loop exited to perform a void function return
	// via Go.
	exitReturnVoid int64 = 5

	// exitTailCall indicates the dispatch loop exited to perform a tail call via Go.
	exitTailCall int64 = 6

	// exitCallOverflow indicates the dispatch loop exited because the call depth limit was
	// exceeded.
	exitCallOverflow int64 = 7

	// exitSetField is the per-op direct exit for isa.OpSetField.
	exitSetField int64 = 8

	// exitGetField is the per-op direct exit for isa.OpGetField. Mirrors exitSetField; saves
	// the handlerTable[op] indirect call on the trampoline's first op.
	exitGetField int64 = 9

	// exitMapIndex is the per-op direct exit for isa.OpMapIndex. Mirrors exitSetField; saves
	// the handlerTable[op] indirect call on the trampoline's first op.
	exitMapIndex int64 = 10

	// exitAppend is the per-op direct exit for isa.OpAppend. Mirrors exitSetField; saves the
	// handlerTable[op] indirect call on the trampoline's first op.
	exitAppend int64 = 11

	// exitAppendByteFast is the tier-0 specialised []byte-append direct-exit reason emitted
	// by compileBuiltinAppend when the slice element is statically `byte`.
	exitAppendByteFast int64 = 12

	// exitGetStructFieldIntT0 is the direct-exit reason for the tier-0 int struct-field
	// reader. The matching processExit dispatcher calls the existing Go handler directly,
	// skipping the handlerTable[op] indirect.
	exitGetStructFieldIntT0 int64 = 13

	// exitGetStructFieldUintT0 is the direct-exit reason for the tier-0 uint struct-field
	// reader.
	exitGetStructFieldUintT0 int64 = 14

	// exitGetStructFieldFloatT0 is the direct-exit reason for the tier-0 float struct-field
	// reader.
	exitGetStructFieldFloatT0 int64 = 15

	// exitGetStructFieldBoolT0 is the direct-exit reason for the tier-0 bool struct-field
	// reader.
	exitGetStructFieldBoolT0 int64 = 16

	// exitSetStructFieldIntT0 is the direct-exit reason for the tier-0 int struct-field
	// writer. Primitive kind only; no GC barrier required (primitives carry no pointers).
	exitSetStructFieldIntT0 int64 = 17

	// exitSetStructFieldUintT0 is the direct-exit reason for the tier-0 uint struct-field
	// writer.
	exitSetStructFieldUintT0 int64 = 18

	// exitSetStructFieldFloatT0 is the direct-exit reason for the tier-0 float struct-field
	// writer.
	exitSetStructFieldFloatT0 int64 = 19

	// exitSetStructFieldBoolT0 is the direct-exit reason for the tier-0 bool struct-field
	// writer.
	exitSetStructFieldBoolT0 int64 = 20

	// exitFrameChanged is the tier-2 ASM-lift exit code for opFrameChanged.
	exitFrameChanged int64 = 21

	// exitPanicErrorPathB is the tier-2 ASM-lift exit code for opPanicError. See
	// exitFrameChanged for the OpResult-to-exitReason mapping table.
	exitPanicErrorPathB int64 = 22

	// exitStackOverflowPathB is the tier-2 ASM-lift exit code for OpStackOverflow. See
	// exitFrameChanged for the OpResult-to-exitReason mapping table.
	exitStackOverflowPathB int64 = 23

	// exitDonePathB is the tier-2 ASM-lift exit code for OpDone. See exitFrameChanged for
	// the OpResult-to-exitReason mapping table.
	exitDonePathB int64 = 24

	// exitGetStructFieldGeneralT0 is the direct-exit reason for the tier-0 general-bank
	// (pointer/interface) struct-field reader. The matching
	// processExitGetStructFieldGeneralT0 calls handleGetStructFieldGeneralT0 directly,
	// skipping the handlerTable[op] indirect on the trampoline's first op.
	exitGetStructFieldGeneralT0 int64 = 25

	// exitSetStructFieldGeneralT0 is the direct-exit reason for the tier-0 general-bank
	// struct-field writer. The handler still needs runtime_typedmemmove for the
	// pointer/interface store (no GC-safe NOSPLIT|NOFRAME path); the direct exit only saves
	// the handlerTable[op] indirect on the trampoline's first op.
	exitSetStructFieldGeneralT0 int64 = 26

	// exitTestNilJumpFalse is the direct-exit reason for the tier-2 nil-test-and-jump op
	// (branch taken when the tested pointer is non-nil). Skipping the handlerTable[op]
	// indirect on the trampoline's first op pays off for tree- and list-style
	// pointer-walking workloads.
	exitTestNilJumpFalse int64 = 27

	// exitTestNilJumpTrue mirrors exitTestNilJumpFalse for the inverted-sense nil test.
	exitTestNilJumpTrue int64 = 28

	// exitPoll forces a periodic return to Go from a tier-1 back edge so the dispatch loop
	// can observe context cancellation and deadline expiry even inside a tight all-ASM loop.
	exitPoll int64 = 29

	// exitSliceSetStringDirect is the direct-exit reason for the tier-1 []string element
	// store. Exits to Go because the arena string barrier cannot be checked cheaply from
	// assembly.
	exitSliceSetStringDirect int64 = 30

	// exitAppendStructFast is the direct-exit reason for isa.OpAppendStructFast.
	exitAppendStructFast int64 = 31

	// exitAppendIntFast is the direct-exit reason for isa.OpAppendIntFast.
	exitAppendIntFast int64 = 32

	// exitAppendFloatFast is the direct-exit reason for isa.OpAppendFloatFast.
	exitAppendFloatFast int64 = 33

	// exitAppendStringFast is the direct-exit reason for isa.OpAppendStringFast.
	exitAppendStringFast int64 = 34

	// dispatchPollBudget is the number of tier-1 back edges the ASM dispatch loop may take
	// before returning to Go to poll cancellation. Mirrors the Go loop's
	// cancellationCheckMask+1 instruction cadence.
	dispatchPollBudget int64 = 1024

	// maxASMIntArgs is the maximum number of int arguments the ASM fast-path call handler
	// can copy.
	maxASMIntArgs = 8

	// maxASMFloatArgs is the maximum number of float arguments the ASM fast-path call
	// handler can copy.
	maxASMFloatArgs = 8

	// maxASMStringArgs is the maximum number of string arguments the ASM fast-path call
	// handler can copy.
	maxASMStringArgs = 8

	// maxASMBoolArgs is the maximum number of bool arguments the ASM fast-path call handler
	// can copy.
	maxASMBoolArgs = 8

	// maxASMUintArgs is the maximum number of uint arguments the ASM fast-path call handler
	// can copy.
	maxASMUintArgs = 8

	// maxASMSliceByteArgs is the maximum number of []byte arguments the ASM fast-path call
	// handler can copy.
	maxASMSliceByteArgs = 8

	// maxASMGeneralArgs is the maximum number of general (reflect.Value) arguments the ASM
	// fast-path call handler can copy via the handlerCallInlineSetupGeneralBank trampoline.
	// Matches the 8-entry generalArgumentSources array in AsmCallInfo.
	maxASMGeneralArgs = 8

	// fastPathIneligible marks a call site that cannot use the inline ASM dispatcher and
	// must trampoline to Go.
	fastPathIneligible int64 = 0

	// fastPathExtendedBanks marks a call site whose callee uses string/bool/uint registers;
	// the inline path allocates each bank.
	fastPathExtendedBanks int64 = 1

	// fastPathPureIntFloat marks a call site whose callee uses only int and float registers;
	// the inline path zeros extended bank headers and skips per-bank allocation (lean path).
	fastPathPureIntFloat int64 = 2

	// fastPathGeneralBank marks a call site whose callee uses the reflect.Value general
	// bank; the inline path allocates the standard banks and CALLs
	// handlerCallInlineSetupGeneralBank for the general bank.
	fastPathGeneralBank int64 = 3
)
