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
	"math"
	"reflect"
	"runtime"

	"pipit.sh/pipit/internal/engine/program"
)

// runtimeFuncRecord is the memory a *runtime.Func handle points at.
//
//nolint:govet // fieldalignment: field order mirrors the runtime's funcinl record
type runtimeFuncRecord struct {
	// fn is zero-sized and sits at offset zero, so &fn is a *runtime.Func that addresses the
	// record without a pointer conversion.
	fn runtime.Func

	// ones is all bits set, the marker the runtime reads to treat the record as inlined.
	ones uint32

	// entry is the synthesised entry program counter.
	entry uintptr

	// name is the qualified function name.
	name string

	// file is the source file of the function's first instruction, empty when unknown.
	file string

	// line is the source line of the function's first instruction, 0 when unknown.
	line int32

	// startLine repeats line; the runtime reads it as the line of the func keyword.
	startLine int32
}

// newRuntimeFuncRecord describes a registry entry in the runtime's funcinl form.
//
// Takes function (*program.CompiledFunction) which is the function, nil for a runtime
// pseudo frame.
// Takes name (string) which is the qualified name.
// Takes entryPC (uintptr) which is the entry's synthesised entry program counter.
//
// Returns runtimeFuncRecord which is the record the entry's handle points at.
func newRuntimeFuncRecord(function *program.CompiledFunction, name string, entryPC uintptr) runtimeFuncRecord {
	file, line := "", 0
	if function != nil {
		file, line = nearestSourcePosition(function, 0)
	}
	line32 := int32(0)
	if line > 0 && line <= math.MaxInt32 {
		line32 = int32(line)
	}
	return runtimeFuncRecord{
		fn:        runtime.Func{},
		ones:      ^uint32(0),
		entry:     entryPC,
		name:      name,
		file:      file,
		line:      line32,
		startLine: line32,
	}
}

// funcHandle presents a registry entry as the *runtime.Func a script's static types
// expect.
//
// Takes entry (*pipitFunc) which is the registry entry, nil for no function.
//
// Returns *runtime.Func which is the handle, nil for a nil entry.
func funcHandle(entry *pipitFunc) *runtime.Func {
	if entry == nil {
		return nil
	}
	return &entry.handle.fn
}

// handleKey returns the registry key a handle to entry decodes with.
//
// Takes entry (*pipitFunc) which is the registry entry.
//
// Returns uintptr which is the handle's address.
func handleKey(entry *pipitFunc) uintptr {
	return reflect.ValueOf(funcHandle(entry)).Pointer()
}

// handleAddress returns the registry key of a *runtime.Func value a script holds.
//
// Takes value (reflect.Value) which holds the handle.
//
// Returns uintptr which is the key, 0 for a nil handle.
func handleAddress(value reflect.Value) uintptr {
	if !value.IsValid() || value.Kind() != reflect.Pointer || value.IsNil() {
		return 0
	}
	return value.Pointer()
}
