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
	"fmt"
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

const (
	// maxReflectValueUnwrapDepth limits how many nested reflect.Value layers
	// unwrapReflectValueResult will peel. One layer covers the expected sibling contract;
	// additional layers guard against accidentally double-wrapped returns without allowing
	// unbounded recursion on crafted inputs.
	maxReflectValueUnwrapDepth = 4
)

// handleCallLinkedReflect dispatches a //piko:link-routed generic call. The site's
// linkedTypeArgs slice is prepended as reflect.Type arguments before the regular call
// arguments; returned reflect.Value values are unwrapped back to their concrete inner
// reflect.Values so parametric return positions round-trip through user code as the
// instantiated T.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the register banks.
// Takes site (*CallSite) which describes the linked call site.
// Takes sibling (reflect.Value) which is the non-generic sibling the directive linked to.
//
// Returns OpResult indicating the next execution step.
func handleCallLinkedReflect(vm *VM, registers *Registers, site *program.CallSite, sibling reflect.Value) OpResult {
	if err := validateLinkedCallShape(sibling, site); err != nil {
		vm.evalError = err
		return opPanicError
	}

	regularArgs := buildReflectArgs(vm, registers, site, sibling)
	defer releaseReflectValueBuffer(regularArgs)
	arguments := make([]reflect.Value, 0, len(site.LinkedTypeArgs)+len(regularArgs))
	for _, typeArgument := range site.LinkedTypeArgs {
		arguments = append(arguments, reflect.ValueOf(typeArgument))
	}
	arguments = append(arguments, regularArgs...)

	if denial := consultCapabilityHookForNativeCall(vm, site, sibling, regularArgs); denial != nil {
		vm.evalError = denial
		return opPanicError
	}

	results, err := safeInvokeLinkedSibling(sibling, arguments)
	if err != nil {
		vm.evalError = err
		return opPanicError
	}

	nResults := len(results)
	unwrapped := make([]reflect.Value, nResults)
	for index, reflectValue := range results {
		unwrapped[index] = unwrapReflectValueResult(reflectValue)
	}
	storeReflectResults(registers, site.Returns, unwrapped)
	return opContinue
}

// safeInvokeLinkedSibling invokes the sibling under a recover guard.
//
// reflect.Value.Call panics on shape mismatch (wrong arity, assignable-to drift, variadic
// disagreement). Recovering here lets the VM surface a structured runtime error instead
// of crashing the host process.
//
// Takes sibling (reflect.Value) which is the function to invoke.
// Takes arguments ([]reflect.Value) which are the fully-built args.
//
// Returns the result slice plus any recovered panic wrapped as an error with the
// errLinkedSiblingPanic sentinel.
func safeInvokeLinkedSibling(sibling reflect.Value, arguments []reflect.Value) (results []reflect.Value, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			results = nil
			err = fmt.Errorf("%w: %v", errLinkedSiblingPanic, recovered)
		}
	}()
	results = sibling.Call(arguments)
	return results, nil
}

// validateLinkedCallShape rejects argument-count mismatches between the prepared call
// site and the sibling's reflect signature before reflect.Call runs. The check is cheap
// and lets the VM report a useful error (with the linked call site info) instead of a
// panic caught deeper in safeInvokeLinkedSibling.
//
// Takes sibling (reflect.Value) which is the target function.
// Takes site (*CallSite) which describes type args and regular args.
//
// Returns nil when the shape matches, or errLinkedSiblingShapeMismatch wrapping a
// human-readable description.
func validateLinkedCallShape(sibling reflect.Value, site *program.CallSite) error {
	if !sibling.IsValid() || sibling.Kind() != reflect.Func {
		return fmt.Errorf("%w: sibling is not a valid function value", errLinkedSiblingShapeMismatch)
	}
	siblingType := sibling.Type()
	expectedArgs := len(site.LinkedTypeArgs) + len(site.Arguments)
	if siblingType.IsVariadic() {
		minRequired := siblingType.NumIn() - 1
		if expectedArgs < minRequired {
			return fmt.Errorf("%w: sibling expects at least %d arguments (variadic), call site supplies %d",
				errLinkedSiblingShapeMismatch, minRequired, expectedArgs)
		}
		return nil
	}
	if siblingType.NumIn() != expectedArgs {
		return fmt.Errorf("%w: sibling expects %d arguments, call site supplies %d (type args %d, regular %d)",
			errLinkedSiblingShapeMismatch, siblingType.NumIn(), expectedArgs,
			len(site.LinkedTypeArgs), len(site.Arguments))
	}
	return nil
}

// unwrapReflectValueResult peels up to maxReflectValueUnwrapDepth reflect.Value wrappers
// when a //piko:link sibling returns a reflect.Value at a position the generic declares
// as a type parameter. Any non-reflect.Value return passes through unchanged.
//
// Takes value (reflect.Value) which is the possibly-wrapped result.
//
// Returns the original value or the unwrapped inner value.
func unwrapReflectValueResult(value reflect.Value) reflect.Value {
	for range maxReflectValueUnwrapDepth {
		if !value.IsValid() {
			return value
		}
		if value.Type() != typemodel.LinkedResultReflectValueType {
			return value
		}
		inner, ok := reflect.TypeAssert[reflect.Value](value)
		if !ok {
			return value
		}
		value = inner
	}
	return value
}
