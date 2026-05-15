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
	"sync/atomic"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// StructLiteralQualifiesForArena reports whether struct literals of t may be carved from
// the arena's generic-bytes slab: the type must be a struct without pointers (so the GC
// never scans the slab), non-empty, and aligned within the slab's guarantee.
//
// Takes t (reflect.Type) which is the literal's struct type.
//
// Returns true when the arena fast path applies.
func StructLiteralQualifiesForArena(t reflect.Type) bool {
	if t == nil || t.Kind() != reflect.Struct || t.Size() == 0 {
		return false
	}
	align := t.Align()
	if align <= 0 || uintptr(align) > arenaMaxAlignment {
		return false
	}
	return typeIsPointerFree(t)
}

// StructLiteralAllocationSubOp selects the tier-1 sub-op the Compiler emits for a struct
// literal of t: the arena allocation sub-op for qualifying pointer-free structs,
// otherwise the generic make path that routes through the boundary snapshot slabs.
//
// Takes t (reflect.Type) which is the literal's struct type.
//
// Returns the tier-2 sub-opcode to Emit.
func StructLiteralAllocationSubOp(t reflect.Type) isa.SubOpcodeTier2 {
	if StructLiteralQualifiesForArena(t) {
		return isa.SubOpTier2AllocStructLiteral
	}
	return isa.SubOpTier2MakeMap
}

// EnsureStructLiteralTable builds the function's struct-literal table on first use.
//
// Publishes it to assembly. The base is published before the length so a concurrent
// assembly reader that observes a non-zero length also observes the base. Any stale
// combination sends the handler to the Go path.
//
// Takes compiledFunction (*program.CompiledFunction) which owns the table to populate.
func EnsureStructLiteralTable(compiledFunction *program.CompiledFunction) {
	compiledFunction.StructLiteralTableOnce.Do(func() {
		entries := make([]program.StructLiteralEntry, len(compiledFunction.TypeTable))
		for index, entry := range compiledFunction.TypeTable {
			if !StructLiteralQualifiesForArena(entry) {
				continue
			}
			typeWord, flag := structLiteralEntryWords(entry)
			entries[index] = program.StructLiteralEntry{
				TypeWord:  typeWord,
				Size:      entry.Size(),
				AlignMask: uintptr(entry.Align()) - 1,
				Flag:      flag,
			}
		}
		compiledFunction.StructLiteralTable = entries
		if len(entries) == 0 {
			return
		}
		atomic.StoreUintptr(&compiledFunction.StructLiteralTableBase, structLiteralTableAddress(entries))
		atomic.StoreInt64(&compiledFunction.StructLiteralTableLength, int64(len(entries)))
	})
}
