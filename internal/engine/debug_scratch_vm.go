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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"
)

// NewScratchVM builds a side VM that shares a paused program's globals and symbols and
// never pauses itself, for the debugger to evaluate expressions on.
//
// Takes globals (*GlobalStore) which is the paused program's global store.
// Takes symbols (*SymbolRegistry) which is the paused program's symbol registry.
// Takes limits (VMLimits) which are the program's limits; the debugger session, tracker,
// cost budget and safe mode are cleared and Go dispatch is forced.
//
// Returns *VM ready for RunCompiledFunction.
func NewScratchVM(ctx context.Context, globals *GlobalStore, symbols *symtab.SymbolRegistry, limits VMLimits) *VM {
	vm := NewVM(ctx, globals, symbols)
	limits.Debug = nil
	limits.Tracker = nil
	limits.CostBudget = 0
	limits.SafeMode = false
	limits.ForceGoDispatch = true
	vm.Limits = limits
	vm.reentrantInterpreterVM = true
	vm.hasGoroutines = true
	return vm
}

// RunCompiledFunction runs function with host-supplied arguments on this VM, bound to the
// file set whose function table the function shares, and returns a heap-safe result.
//
// Takes cfs (*program.CompiledFileSet) which is the program the function belongs to.
// Takes function (*program.CompiledFunction) which is the function to run.
// Takes arguments ([]reflect.Value) which supply one value per parameter.
//
// Returns any which is the function's first result, copied out of the arena.
// Returns error when binding the arguments or running the function fails.
func (vm *VM) RunCompiledFunction(cfs *program.CompiledFileSet, function *program.CompiledFunction, arguments []reflect.Value) (result any, err error) {
	defer RecoverArenaBudgetError(&err)
	arena := vm.PrepareForFileSet(cfs, function)
	defer vm.ReleaseFileSetArena(arena)
	defer vm.FinishWatcher()
	vm.PushFrame(function)
	if len(arguments) > 0 || len(function.ParameterKinds) > 0 {
		if bindErr := vm.AssignFrameArguments(arguments); bindErr != nil {
			return nil, bindErr
		}
	}
	result, err = vm.RunDispatchedGuarded(0)
	if joinErr := vm.JoinSpawnedGoroutines(); joinErr != nil && err == nil {
		err = joinErr
	}
	result = MaterialiseAnyForArena(arena, result)
	return result, err
}
