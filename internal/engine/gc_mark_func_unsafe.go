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

//go:build !safe && !(js && wasm)

package engine

import (
	"reflect"
	"runtime"
	"strings"
	"sync"
	"unsafe"
	"weak"

	"pipit.sh/pipit/internal/engine/program"
)

// programConstantFunctions is the process-wide memo of each program's host function
// constants (programConstantFunction).
var programConstantFunctions = programConstantFunctionTable{sets: make(map[weak.Pointer[program.CompiledFunction]]map[uintptr]struct{}), mu: sync.RWMutex{}}

// programConstantFunctionTable maps a program's root function, held weakly, to the
// function objects of its host function constants.
type programConstantFunctionTable struct {
	// sets maps a root to its constants' function objects.
	sets map[weak.Pointer[program.CompiledFunction]]map[uintptr]struct{}

	// mu guards sets.
	mu sync.RWMutex
}

// markFunctionValue traces what a non-nil function value can reference, for the licensed
// mark.
//
// Takes arena (*RegisterArena) which owns the slabs being marked.
// Takes state (*gcMarkState) which records the marks.
// Takes value (reflect.Value) which is a non-nil function value.
// Takes depth (int) which is the current trace depth.
//
// Returns bool which is true when the value's references were traced; false means the
// cycle fails closed.
func markFunctionValue(arena *RegisterArena, state *gcMarkState, value reflect.Value, depth int) bool {
	header := (*unsafeReflectValue)(unsafe.Pointer(&value))
	if header.flag&flagMethodBit != 0 {
		if header.typ == nil {
			return false
		}
		kind := uintptr(*(*uint8)(unsafe.Add(header.typ, abiTypeKindOffset))) & flagKindMask
		receiver := unsafeReflectValue{typ: header.typ, ptr: header.ptr, flag: header.flag&(licenceFlagRO|flagIndir) | kind}
		state.pushMark(*(*reflect.Value)(unsafe.Pointer(&receiver)), depth+1)
		return true
	}
	if closure, bound, found := closureWrapperLookup(value); found {
		markClosureCells(arena, state, closure, depth+1)
		if bound != nil && bound.receiver.IsValid() {
			state.pushMark(bound.receiver, depth+1)
		}
		return true
	}
	return programConstantFunction(state.program, value) || functionCapturesNothing(value.Pointer())
}

// programConstantFunction reports whether value is one of the host function constants of
// the program whose root is root: a function value in some function's GeneralConstants
// with the same function object, whose code is not reflect's MakeFunc stub.
//
// Takes root (*program.CompiledFunction) which is the program's root; nil answers false.
// Takes value (reflect.Value) which is a non-nil function value.
//
// Returns bool which is true for one of the program's host function constants.
func programConstantFunction(root *program.CompiledFunction, value reflect.Value) bool {
	if root == nil || value.Pointer() == reflectMakeFuncStubPointer {
		return false
	}
	object := funcValueObject(value)
	if object == nil {
		return false
	}
	_, found := programConstantFunctions.of(root)[uintptr(object)]
	return found
}

// of returns the function objects of root's host function constants, collecting them on
// the first request for root.
//
// Takes root (*program.CompiledFunction) which is the program's root.
//
// Returns map[uintptr]struct{} which holds each constant's function object; read only.
//
// Concurrency: takes mu for reading, and for writing when it records a new root.
func (table *programConstantFunctionTable) of(root *program.CompiledFunction) map[uintptr]struct{} {
	key := weak.Make(root)
	table.mu.RLock()
	set, found := table.sets[key]
	table.mu.RUnlock()
	if found {
		return set
	}
	set = collectConstantFunctions(root)
	table.mu.Lock()
	defer table.mu.Unlock()
	if existing, raced := table.sets[key]; raced {
		return existing
	}
	table.sets[key] = set
	runtime.AddCleanup(root, table.forget, key)
	return set
}

// forget drops a root's set once the root is unreachable.
//
// Takes key (weak.Pointer[program.CompiledFunction]) which names the root.
//
// Concurrency: takes mu for writing.
func (table *programConstantFunctionTable) forget(key weak.Pointer[program.CompiledFunction]) {
	table.mu.Lock()
	defer table.mu.Unlock()
	delete(table.sets, key)
}

// collectConstantFunctions gathers the function objects of the non-MakeFunc function
// constants of root and of every function in its table.
//
// Takes root (*program.CompiledFunction) which is the program's root.
//
// Returns map[uintptr]struct{} which holds the function objects.
func collectConstantFunctions(root *program.CompiledFunction) map[uintptr]struct{} {
	set := make(map[uintptr]struct{})
	collect := func(function *program.CompiledFunction) {
		if function == nil {
			return
		}
		for _, constant := range function.GeneralConstants {
			if !constant.IsValid() || constant.Kind() != reflect.Func || constant.IsNil() || constant.Pointer() == reflectMakeFuncStubPointer {
				continue
			}
			if object := funcValueObject(constant); object != nil {
				set[uintptr(object)] = struct{}{}
			}
		}
	}
	collect(root)
	for _, function := range root.Functions {
		collect(function)
	}
	return set
}

// markClosureOrCellPointer traces a pointer to a guest closure or to an upvalue cell by
// its fields' meaning (markClosureCells, markCellContents) rather than reflectively.
//
// Takes arena (*RegisterArena) which owns the slabs being marked.
// Takes state (*gcMarkState) which records the marks.
// Takes value (reflect.Value) which is a pointer.
// Takes depth (int) which is the pointer's distance from its root.
//
// Returns bool which is true when value was one of the two and is traced.
func markClosureOrCellPointer(arena *RegisterArena, state *gcMarkState, value reflect.Value, depth int) bool {
	switch value.Type() {
	case runtimeClosureReflectType:
		markClosureCells(arena, state, (*RuntimeClosure)(value.UnsafePointer()), depth)
		return true
	case upvalueCellPointerType:
		markCellContents(arena, state, (*program.UpvalueCell)(value.UnsafePointer()), depth)
		return true
	default:
		return false
	}
}

// functionCapturesNothing reports whether the function whose code starts at
// programCounter is a top-level function or method expression, whose function value is
// static and holds no captured variable.
//
// Takes programCounter (uintptr) which is the function value's code pointer.
//
// Returns bool which is true when the function captures nothing.
func functionCapturesNothing(programCounter uintptr) bool {
	info := runtime.FuncForPC(programCounter)
	if info == nil || info.Entry() != programCounter {
		return false
	}
	name := info.Name()
	short := name[strings.LastIndexByte(name, '/')+1:]
	if short == "" || strings.HasPrefix(short, "reflect.") {
		return false
	}
	for _, marker := range [...]string{".func", "-fm", "-range", ".deferwrap", ".gowrap"} {
		if strings.Contains(short, marker) {
			return false
		}
	}
	return true
}
