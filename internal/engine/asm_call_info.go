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
	"pipit.sh/pipit/internal/isa"
)

var (
	// maxASMArgsByKind maps each register kind to its maximum ASM argument count.
	// isa.RegisterComplex and the typed-slice banks other than isa.RegisterSliceByte are
	// unsupported by the ASM fast path.
	maxASMArgsByKind = [isa.NumRegisterKinds]int{
		isa.RegisterInt:       maxASMIntArgs,
		isa.RegisterFloat:     maxASMFloatArgs,
		isa.RegisterString:    maxASMStringArgs,
		isa.RegisterGeneral:   maxASMGeneralArgs,
		isa.RegisterBool:      maxASMBoolArgs,
		isa.RegisterUint:      maxASMUintArgs,
		isa.RegisterComplex:   0,
		isa.RegisterSliceByte: maxASMSliceByteArgs,
	}
)

// AsmCallInfo holds pre-computed metadata for one call site, enabling the ASM dispatch
// loop to handle interpreted-to-interpreted calls without trampolining to Go. Field
// offsets are hardcoded in the ASM handlers and verified by TestASMCallInfoOffsets.
type AsmCallInfo struct {
	// calleeFunction is the unsafe pointer to the callee CompiledFunction.
	calleeFunction uintptr

	// calleeBody is the pointer to the first instruction of the callee function body.
	calleeBody uintptr

	// calleeBodyLength is the number of instructions in the callee function body.
	calleeBodyLength int64

	// calleeIntConstants is the pointer to the callee function's int constant table.
	calleeIntConstants uintptr

	// calleeFloatConstants is the pointer to the callee function's float constant table.
	calleeFloatConstants uintptr

	// calleeIntCount is the number of int registers required by the callee function.
	calleeIntCount int64

	// calleeFloatCount is the number of float registers required by the callee function.
	calleeFloatCount int64

	// intArgumentCount is the number of integer arguments to copy from caller to callee.
	intArgumentCount int64

	// intArgumentSources holds the caller int register index for each integer argument.
	intArgumentSources [8]int64

	// floatArgumentCount is the number of float arguments to copy from caller to callee.
	floatArgumentCount int64

	// floatArgumentSources holds the caller float register index for each float argument.
	floatArgumentSources [8]int64

	// ReturnCount is the number of return values, from zero to asmMaxInlineReturns.
	ReturnCount int64

	// returnDestinationKind is the register kind for the return destination, int or float.
	returnDestinationKind int64

	// returnDestinationRegister is the caller register index for storing the return value.
	returnDestinationRegister int64

	// returnDestinationPointer is the pointer to the first return location descriptor.
	returnDestinationPointer uintptr

	// returnDestinationLen is the number of entries in the return location descriptor slice.
	returnDestinationLen int64

	// calleeCallInfo is the pointer to the callee function's AsmCallInfo table base.
	calleeCallInfo uintptr

	// isFastPath indicates the ASM inline dispatch mode. Values are defined by the fastPath*
	// constants.
	isFastPath int64

	// calleeStringCount is the number of string registers required by the callee function.
	calleeStringCount int64

	// calleeBoolCount is the number of bool registers required by the callee function.
	calleeBoolCount int64

	// calleeUintCount is the number of uint registers required by the callee function.
	calleeUintCount int64

	// stringArgumentCount is the number of string arguments to copy from caller to callee.
	stringArgumentCount int64

	// stringArgumentSources holds the caller string register index for each string argument.
	stringArgumentSources [8]int64

	// boolArgumentCount is the number of bool arguments to copy from caller to callee.
	boolArgumentCount int64

	// boolArgumentSources holds the caller bool register index for each bool argument.
	boolArgumentSources [8]int64

	// uintArgumentCount is the number of uint arguments to copy from caller to callee.
	uintArgumentCount int64

	// uintArgumentSources holds the caller uint register index for each uint argument.
	uintArgumentSources [8]int64

	// calleeStringConstants is a pointer to the callee function's string constant table
	// (16-byte Go string headers). Loaded into the dispatch context by the inline ASM call
	// handler so the callee's opLoadStringConst handler reads the right table.
	calleeStringConstants uintptr

	// calleeBoolConstants is a pointer to the callee function's bool constant table (1-byte
	// entries). Loaded into the dispatch context by the inline ASM call handler.
	calleeBoolConstants uintptr

	// calleeUintConstants is a pointer to the callee function's uint constant table
	// (installed into CTX_UINT_CONSTS_BASE by the inline call fast path).
	calleeUintConstants uintptr

	// calleeGeneralCount is the number of general (reflect.Value) registers required by the
	// callee function. isFastPath=3 uses this to size the callee's general-bank arena
	// allocation from the dispatch context's arenaGeneralSlab.
	calleeGeneralCount int64

	// generalArgumentCount is the number of general-bank arguments to copy from caller to
	// callee.
	generalArgumentCount int64

	// generalArgumentSources holds the caller general register index for each general
	// argument.
	generalArgumentSources [8]int64

	// calleeSliceByteCount is the number of slicesByte (typed []byte) registers required by
	// the callee function. The inline byte-slice path uses this to size the callee's
	// slicesByte bank from arena.slicesByteSlab via the dispatch context.
	calleeSliceByteCount int64

	// sliceByteArgumentCount is the number of []byte arguments to copy from caller to
	// callee.
	sliceByteArgumentCount int64

	// sliceByteArgumentSources holds the caller slicesByte register index for each []byte
	// argument.
	sliceByteArgumentSources [8]int64

	// closurePointer is the RuntimeClosure data word for this entry, zero for static callees
	// and verified by the ASM inline call before dispatch so a mismatched closure falls back
	// to the Go path.
	closurePointer uintptr

	// closureValueRegister is the general-bank register holding the closure value at this
	// call site, read by the ASM guard.
	closureValueRegister int64

	// closureUpvaluesPtr is the data pointer of the closure's upvalue cell slice header for
	// the CF_UPVALUES install. The cells are kept alive by the call site's strong closure
	// cache, so the raw words never dangle.
	closureUpvaluesPtr uintptr

	// closureUpvaluesLen is the length of the closure's upvalue cell slice.
	closureUpvaluesLen int64

	// closureUpvaluesCap is the capacity of the closure's upvalue cell slice.
	closureUpvaluesCap int64

	// receiverValueRegister is the general-bank register holding the receiver at a method
	// call site, read by the ASM inline method-call handler to fetch the receiver's type
	// word. Only meaningful when methodEntryCount is non-zero.
	receiverValueRegister int64

	// methodEntryCount is the number of populated receiver types in methodTypeWords,
	// published last so a non-zero count always covers valid words and zero directs the
	// handler to the Go path.
	methodEntryCount int64

	// methodTypeWords holds the dereferenced receiver *abi.Type words (the inline-cache key)
	// for each populated entry, compared linearly by the ASM handler. A single monomorphic
	// word would never hit on a polymorphic site, hence the small table.
	methodTypeWords [MaxASMMethodEntries]uintptr

	// methodEntriesPtr points at the first of methodEntryCount AsmCallInfo entries, one per
	// receiver type in methodTypeWords order. The entries live in the owning VM's
	// asmMethodEntryTables, which keeps them reachable while this raw word is live.
	methodEntriesPtr uintptr

	// calleeFunctionIndex is the callee's index in the root's function list, or the reserved
	// trailing slot for a callee outside it. The ASM inline call indexes
	// callInfoBasesByFunction with it to find the callee's AsmCallInfo table base.
	calleeFunctionIndex int64

	// _padding extends the struct size to 1024 bytes so the inline call dispatcher indexes
	// the per-site table via (siteIndex << ACI_SIZE_SHIFT) where the shift is 10. The
	// remaining bytes are reserved.
	_padding [208]byte
}
