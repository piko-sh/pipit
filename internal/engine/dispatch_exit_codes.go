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

	// exitSetField is the per-op direct exit for isa.OpSetField.
	exitSetField int64 = 8

	// exitGetField is the per-op direct exit for isa.OpGetField. Mirrors exitSetField; saves
	// the handlerTable[op] indirect call on the trampoline's first op.
	exitGetField int64 = 9

	// exitGetMap is the per-op direct exit for isa.OpGetMap. Mirrors exitSetField.
	exitGetMap int64 = 10

	// exitAppend is the per-op direct exit for isa.OpAppend. Mirrors exitSetField; saves the
	// handlerTable[op] indirect call on the trampoline's first op.
	exitAppend int64 = 11

	// exitAppendByte is the tier-0 specialised []byte-append direct-exit reason emitted by
	// compileBuiltinAppend when the slice element is statically `byte`.
	exitAppendByte int64 = 12

	// exitSetSliceStringElement is the direct-exit reason for the tier-1 []string element
	// store. Exits to Go because the arena string barrier cannot be checked cheaply from
	// assembly.
	exitSetSliceStringElement int64 = 30

	// exitAppendStruct is the direct-exit reason for isa.OpAppendStruct.
	exitAppendStruct int64 = 31

	// exitAppendInt64 is the direct-exit reason for isa.OpAppendInt64.
	exitAppendInt64 int64 = 32

	// exitAppendFloat64 is the direct-exit reason for isa.OpAppendFloat64.
	exitAppendFloat64 int64 = 33

	// exitAppendString is the direct-exit reason for isa.OpAppendString.
	exitAppendString int64 = 34

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
