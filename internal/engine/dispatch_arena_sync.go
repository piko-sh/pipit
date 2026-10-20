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
	"unsafe"
)

// syncArenaIndicesFromASM copies arena cursors back from the context.
//
// Takes arena (*RegisterArena) which receives the cursor values.
// Takes ctx (*dispatchContext) which holds the ASM-advanced cursors.
func syncArenaIndicesFromASM(arena *RegisterArena, ctx *dispatchContext) {
	arena.IntIndex = int(ctx.arenaIntIndex)
	arena.FloatIndex = int(ctx.arenaFloatIndex)
	arena.StringIndex = int(ctx.arenaStringIndex)

	arena.GeneralIndex = int(ctx.arenaGeneralIndex)
	arena.BoolIndex = int(ctx.arenaBoolIndex)
	arena.UintIndex = int(ctx.arenaUintIndex)
	arena.SlicesByteIndex = int(ctx.arenaSliceByteIndex)
	arena.SlicesIntIndex = int(ctx.arenaSliceIntIndex)
	arena.SlicesFloatIndex = int(ctx.arenaSliceFloatIndex)
	arena.SlicesStringIndex = int(ctx.arenaSliceStringIndex)
	arena.SlicesBoolIndex = int(ctx.arenaSliceBoolIndex)
	arena.SlicesUintIndex = int(ctx.arenaSliceUintIndex)
	if ctx.arenaGenericBytesCharged != 0 {
		arena.noteAlloc(ctx.arenaGenericBytesCharged)
		ctx.arenaGenericBytesCharged = 0
	}
}

// publishCallInfoRoot republishes the current root and its call-info bases after a Go
// handler may have changed them.
//
// Takes ctx (*dispatchContext) which is the live dispatch context.
func (vm *VM) publishCallInfoRoot(ctx *dispatchContext) {
	if len(vm.asmCallInfoBasesByFunction) > 0 {
		ctx.callInfoBasesByFunction = uintptr(unsafe.Pointer(&vm.asmCallInfoBasesByFunction[0]))
	}
	if vm.rootFunction == vm.asmCallInfoRoot {
		ctx.callInfoBasesRootMatch = 1
	} else {
		ctx.callInfoBasesRootMatch = 0
	}
}
