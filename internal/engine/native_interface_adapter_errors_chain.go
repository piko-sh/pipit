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
)

const (
	// errorAsMethodSuffix is the method-table suffix used to look up the source-level As
	// method for pipit-side error types.
	errorAsMethodSuffix = ".As"

	// errorIsMethodSuffix is the method-table suffix used to look up the source-level Is
	// method for pipit-side error types.
	errorIsMethodSuffix = ".Is"

	// errorUnwrapMethodSuffix is the method-table suffix used to look up the source-level
	// Unwrap method for pipit-side error types.
	errorUnwrapMethodSuffix = ".Unwrap"

	// errorUnwrapMethodName is the bare Go-side method name reflect uses for the stdlib
	// `Unwrap()`-error and `Unwrap()`-[]error protocols. Centralising the literal makes the
	// multiple lookup-by-name call sites uniform and keeps the lint budget happy.
	errorUnwrapMethodName = "Unwrap"

	// maxErrorsChainDepth caps the chain-walk depth at 100 to bound a cyclic or
	// self-referential joined error.
	maxErrorsChainDepth = 100
)

// pipitErrorsAs is the pipit-aware errors.As. Mirrors stdlib semantics but tolerates
// pipit-synthesised concrete types whose reflect.Type carries no MethodSet.
//
// Takes vm (*VM) which provides typeNames + MethodTable access for resolving pipit-side
// methods on the error chain.
// Takes err (reflect.Value) which is the first argument: the error being inspected. May
// be the typed nil interface value.
// Takes target (reflect.Value) which is the second argument: a non-nil pointer to a
// target type that the matched error must be assigned to.
//
// Returns true and writes through target when an error in the chain matches; returns
// false and leaves target untouched otherwise.
//
// Panics with the same diagnostic strings stdlib errors.As emits when target is nil, not
// a pointer, or its element type cannot hold an error.
func pipitErrorsAs(vm *VM, err, target reflect.Value) bool {
	target = peelInterface(target, "errors: target cannot be nil")
	if !target.IsValid() || target.Kind() != reflect.Pointer || target.IsNil() {
		panic("errors: target must be a non-nil pointer")
	}
	if !err.IsValid() || (err.Kind() == reflect.Interface && err.IsNil()) {
		return false
	}
	targetType := target.Type().Elem()
	if targetType.Kind() != reflect.Interface && !targetType.Implements(errorReflectType) && !errorsAsTargetIsPipitSynthesised(vm, targetType) {
		panic("errors: *target must be interface or implement error")
	}
	return walkErrorsAsChain(vm, err, target, targetType, 0)
}

// walkErrorsAsChain implements the chain-walking core of pipitErrorsAs. Extracted from
// pipitErrorsAs so the validation/panic prelude in the caller stays cognitively simple
// and the chain logic recurses when errors.Join multi-Unwrap fans out into sibling
// chains.
//
// Takes vm (*VM) which provides the method registry for As-method dispatch and Unwrap
// chain advancement.
// Takes err (reflect.Value) which is the current chain root, validated non-nil by the
// caller.
// Takes target (reflect.Value) which is the **T pointer the caller already validated.
// Takes targetType (reflect.Type) which is target.Elem().Type(), pre-computed by the
// caller.
// Takes depth (int) which is the current chain-walk recursion depth; the walk stops
// descending once it reaches maxErrorsChainDepth so a cyclic joined error cannot overflow
// the host stack.
//
// Returns true on a successful chain hit (target written), false when the chain exhausts
// without a match or the depth cap is reached.
func walkErrorsAsChain(vm *VM, err, target reflect.Value, targetType reflect.Type, depth int) bool {
	current := err
	for current.IsValid() {
		if depth >= maxErrorsChainDepth {
			return false
		}
		concrete := unwrapErrorInterfaceValue(current)
		if !concrete.IsValid() {
			return false
		}
		if errorsAsTryDirectMatch(vm, current, concrete, target, targetType) {
			return true
		}
		if siblings, ok := tryMultiUnwrap(vm, current); ok {
			return errorsAsTryAnyOf(vm, siblings, target, targetType, depth+1)
		}
		next, ok := advanceErrorChain(vm, current, concrete)
		if !ok {
			return false
		}
		current = next
		depth++
	}
	return false
}

// errorsAsTryDirectMatch performs the per-node match attempts that happen at each step of
// the chain walk: assignability into the target type, and dispatch through the pipit-side
// As(target) bool method.
//
// Takes vm (*VM) which provides the method registry.
// Takes original (reflect.Value) which is the chain node before adapter unwrapping (used
// for interface-target assignment).
// Takes concrete (reflect.Value) which is the chain node after adapter unwrapping (used
// for concrete-target assignment and method lookup).
// Takes target (reflect.Value) which is the **T pointer.
// Takes targetType (reflect.Type) which is target's element type.
//
// Returns true when either the assignment succeeded or the As method reported a match
// (with target written in either case).
func errorsAsTryDirectMatch(vm *VM, original, concrete, target reflect.Value, targetType reflect.Type) bool {
	assignment := chooseErrorsAsAssignmentValue(original, concrete, targetType)
	if assignment.IsValid() && pipitErrorsAsAssignTarget(target, assignment, targetType) {
		return true
	}
	return invokeAsMethodIfPresent(vm, concrete, target)
}

// errorsAsTryAnyOf recursively walks each sibling produced by an Unwrap() []error fan-out
// and reports the first match. Stops the outer chain walk by returning false when no
// sibling matches, mirroring stdlib errors.As's "no fallthrough across siblings"
// semantics.
//
// Takes vm (*VM) which provides the method registry.
// Takes siblings ([]reflect.Value) which are the multi-Unwrap results.
// Takes target (reflect.Value) which is the **T pointer.
// Takes targetType (reflect.Type) which is target's element type.
// Takes depth (int) which is the chain-walk depth carried into each sibling walk so a
// cyclic fan-out is bounded.
//
// Returns true when any sibling chain succeeds; false otherwise.
func errorsAsTryAnyOf(vm *VM, siblings []reflect.Value, target reflect.Value, targetType reflect.Type, depth int) bool {
	if depth >= maxErrorsChainDepth {
		return false
	}
	for _, sibling := range siblings {
		if walkErrorsAsChain(vm, sibling, target, targetType, depth) {
			return true
		}
	}
	return false
}

// tryMultiUnwrap implements the stdlib `interface{ Unwrap() []error }` protocol used by
// errors.Join. Invokes under a recover guard so a panicking native Unwrap cannot crash
// the host.
//
// Takes vm (*VM) which carries the dispatchDepth used to decide whether a recovered panic
// must be re-raised.
// Takes value (reflect.Value) which is the candidate multi-error.
//
// Returns the sibling errors and true when the value has an Unwrap() []error method that
// returned a non-nil slice.
func tryMultiUnwrap(vm *VM, value reflect.Value) ([]reflect.Value, bool) {
	method, ok := lookupMultiUnwrapMethod(value)
	if !ok {
		return nil, false
	}
	results, ok := safeNativeUnwrapCall(vm, method)
	if !ok || len(results) == 0 {
		return nil, false
	}
	slice := results[0]
	if !slice.IsValid() || slice.Kind() != reflect.Slice {
		return nil, false
	}
	siblings := make([]reflect.Value, 0, slice.Len())
	for i := range slice.Len() {
		siblings = append(siblings, slice.Index(i))
	}
	return siblings, true
}

// safeNativeUnwrapCall invokes a zero-argument native Unwrap method under a recover guard
// so a panicking native error implementation cannot crash the host VM. Re-raises
// interpreted panics when dispatchDepth > 0.
//
// Takes vm (*VM) which carries the dispatchDepth re-panic discipline.
// Takes method (reflect.Value) which is the bound zero-argument Unwrap method.
//
// Returns the method's results and true on a clean call, or nil and false when the call
// panicked.
//
// Panics when the recovered value is an interpreted panic and the dispatcher is still on
// the host stack; the re-raise propagates to an upstream interpreted defer/recover.
func safeNativeUnwrapCall(vm *VM, method reflect.Value) (results []reflect.Value, ok bool) {
	defer func() {
		recovered := recover()
		if recovered == nil {
			return
		}
		if vm != nil && vm.Globals != nil && vm.Globals.dispatchDepth.Load() > 0 {
			panic(recovered)
		}
		results = nil
		ok = false
	}()
	return method.Call(nil), true
}

// lookupMultiUnwrapMethod locates the `Unwrap() []error` method on value, peeling
// interface and pointer envelopes as needed to expose the underlying concrete type's
// method set. Returns the bound method and true when found with the expected signature.
//
// Takes value (reflect.Value) which is the candidate multi-error envelope (possibly
// wrapped in interface{} or **T).
//
// Returns the bound Unwrap method and true when found, or zero and false otherwise.
func lookupMultiUnwrapMethod(value reflect.Value) (reflect.Value, bool) {
	if !value.IsValid() {
		return reflect.Value{}, false
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return reflect.Value{}, false
		}
		value = value.Elem()
	}
	method := value.MethodByName(errorUnwrapMethodName)
	if !method.IsValid() && value.Kind() == reflect.Pointer && !value.IsNil() {
		method = value.Elem().MethodByName(errorUnwrapMethodName)
	}
	if !method.IsValid() {
		return reflect.Value{}, false
	}
	methodType := method.Type()
	if methodType.NumIn() != 0 || methodType.NumOut() != 1 {
		return reflect.Value{}, false
	}
	resultType := methodType.Out(0)
	if resultType.Kind() != reflect.Slice || resultType.Elem() != errorReflectType {
		return reflect.Value{}, false
	}
	return method, true
}

// peelInterface unwraps an interface-Kind reflect.Value to its concrete dynamic value.
//
// Non-interface values pass through unchanged so the helper is safe to call
// unconditionally on values that may already have been peeled.
//
// Takes value (reflect.Value) which may wrap a concrete dynamic value behind an interface
// envelope.
// Takes nilPanicMessage (string) which is the panic message raised when an interface-kind
// value wraps nil (matching stdlib's "errors: target cannot be nil" path).
//
// Returns the concrete reflect.Value contained in the interface, or the input unchanged
// when it is not an interface envelope.
//
// Panics with the supplied nilPanicMessage when the unwrapped interface value is nil.
func peelInterface(value reflect.Value, nilPanicMessage string) reflect.Value {
	if !value.IsValid() {
		return value
	}
	if value.Kind() != reflect.Interface {
		return value
	}
	if value.IsNil() {
		panic(nilPanicMessage)
	}
	return value.Elem()
}

// chooseErrorsAsAssignmentValue picks which reflect.Value should be assigned to *target
// on a successful errors.As match.
//
// Takes original (reflect.Value) which is the chain node before adapter unwrapping (still
// a *pipitErrorAdapter when applicable).
// Takes concrete (reflect.Value) which is the chain node after unwrapping the adapter
// (the pipit-synthesised value).
// Takes targetType (reflect.Type) which is the target's element type.
//
// Returns the value to attempt assignment from, or an invalid reflect.Value when neither
// shape is appropriate.
func chooseErrorsAsAssignmentValue(original, concrete reflect.Value, targetType reflect.Type) reflect.Value {
	if targetType.Kind() == reflect.Interface {
		if targetType.NumMethod() == 0 {
			return concrete
		}
		if original.IsValid() && original.Type().AssignableTo(targetType) {
			return original
		}
	}
	return concrete
}

// pipitErrorsIs is the pipit-aware errors.Is.
//
// Walks the Unwrap chain and tests each error against target via direct equality, the Is
// method (native or pipit), and reflect- comparable fallbacks. Avoids the stdlib's
// MethodSet checks so pipit-synthesised sentinel error types match correctly.
//
// Takes vm (*VM) which provides method-table lookups.
// Takes err (reflect.Value) which is the first argument.
// Takes target (reflect.Value) which is the second argument.
//
// Returns true when err (or any unwrap-reachable error) matches target.
func pipitErrorsIs(vm *VM, err, target reflect.Value) bool {
	targetConcrete := unwrapErrorInterfaceValue(target)
	if !targetConcrete.IsValid() {
		return !unwrapErrorInterfaceValue(err).IsValid()
	}
	return walkErrorsIsChain(vm, err, target, targetConcrete, 0)
}

// walkErrorsIsChain mirrors walkErrorsAsChain for the errors.Is protocol: walk the Unwrap
// chain, succeed on direct equality or a pipit-side Is(target error) bool method, and
// recurse on multi-Unwrap fan-outs (errors.Join).
//
// Takes vm (*VM) which provides the method registry.
// Takes err (reflect.Value) which is the current chain root.
// Takes target (reflect.Value) which is the original target value (used when invoking
// pipit-side Is methods that may want the interface form).
// Takes targetConcrete (reflect.Value) which is target with any interface envelope
// already peeled, used for direct equality checks.
// Takes depth (int) which is the current chain-walk recursion depth; the walk stops
// descending once it reaches maxErrorsChainDepth so a cyclic joined error cannot overflow
// the host stack.
//
// Returns true on a successful match, false when the chain exhausts or the depth cap is
// reached.
func walkErrorsIsChain(vm *VM, err, target, targetConcrete reflect.Value, depth int) bool {
	current := err
	for current.IsValid() {
		if depth >= maxErrorsChainDepth {
			return false
		}
		concrete := unwrapErrorInterfaceValue(current)
		if !concrete.IsValid() {
			return false
		}
		if errorsIsTryDirectMatch(vm, concrete, target, targetConcrete) {
			return true
		}
		if siblings, ok := tryMultiUnwrap(vm, current); ok {
			return errorsIsTryAnyOf(vm, siblings, target, targetConcrete, depth+1)
		}
		next, ok := advanceErrorChain(vm, current, concrete)
		if !ok {
			return false
		}
		current = next
		depth++
	}
	return false
}

// errorsIsTryDirectMatch checks the per-node match conditions for errors.Is: direct
// comparable-equality and the pipit-side Is(target) bool method. Mirrors
// errorsAsTryDirectMatch but specialised to the Is protocol's signature.
//
// Takes vm (*VM) which provides the method registry.
// Takes concrete (reflect.Value) which is the unwrapped chain value.
// Takes target (reflect.Value) which is the original target value (passed to method
// dispatch which may want the interface form).
// Takes targetConcrete (reflect.Value) which is the unwrapped comparison value (used for
// direct equality).
//
// Returns true when any per-node match succeeds.
func errorsIsTryDirectMatch(vm *VM, concrete, target, targetConcrete reflect.Value) bool {
	if errorsIsDirectEqual(concrete, targetConcrete) {
		return true
	}
	return invokeIsMethodIfPresent(vm, concrete, target)
}

// errorsIsTryAnyOf recursively walks each sibling produced by an Unwrap() []error fan-out
// for the errors.Is protocol.
//
// Takes vm (*VM) which provides the method registry.
// Takes siblings ([]reflect.Value) which are the multi-Unwrap results.
// Takes target (reflect.Value) which is the original target.
// Takes targetConcrete (reflect.Value) which is the unwrapped target.
// Takes depth (int) which is the chain-walk depth carried into each sibling walk so a
// cyclic fan-out is bounded.
//
// Returns true when any sibling chain succeeds.
func errorsIsTryAnyOf(vm *VM, siblings []reflect.Value, target, targetConcrete reflect.Value, depth int) bool {
	if depth >= maxErrorsChainDepth {
		return false
	}
	for _, sibling := range siblings {
		if walkErrorsIsChain(vm, sibling, target, targetConcrete, depth) {
			return true
		}
	}
	return false
}

// errorsAsTargetIsPipitSynthesised reports whether the target-element type comes from
// pipit's StructOf-built reflect.Type registry. Used by pipitErrorsAs to relax the stdlib
// panic when target.Elem().Type() is a pipit-synthesised concrete type that pipit knows
// has an Error() method.
//
// Takes vm (*VM) which provides typeNames access.
// Takes targetType (reflect.Type) which is target.Elem().Type().
//
// Returns true when targetType (or its pointee) is in vm.RootFunction.TypeNames.
func errorsAsTargetIsPipitSynthesised(vm *VM, targetType reflect.Type) bool {
	if vm == nil || vm.rootFunction == nil {
		return false
	}
	t := targetType
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	_, ok := vm.rootFunction.TypeNames[t]
	return ok
}

// unwrapErrorInterfaceValue strips interface and adapter envelopes from an error
// reflect.Value.
//
// Collapses an interface-wrapped error or a *pipitErrorAdapter to the concrete
// pipit-synthesised value the wrapper carries; the chain walk uses this so type matches
// go against the underlying value, not the transport adapter. Concrete values with no
// envelope pass through unchanged.
//
// Takes value (reflect.Value) which may wrap an interface or adapter.
//
// Returns the unwrapped concrete reflect.Value, or an invalid value when the input is a
// typed nil interface.
func unwrapErrorInterfaceValue(value reflect.Value) reflect.Value {
	if !value.IsValid() {
		return value
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return value
	}
	if value.Kind() == reflect.Pointer && !value.IsNil() {
		if adapter, ok := reflect.TypeAssert[*pipitErrorAdapter](value); ok {
			return adapter.underlying
		}
	}
	return value
}

// pipitErrorsAsAssignTarget tests whether concrete is assignable to targetType (target's
// element type) and, on success, performs the assignment. Used by pipitErrorsAs to
// satisfy the "type assertion succeeds" branch without going through reflect.Implements.
//
// Takes target (reflect.Value) which is the **T pointer pre-validated by the caller.
// Takes concrete (reflect.Value) which is the current chain value.
// Takes targetType (reflect.Type) which is target.Elem().Type().
//
// Returns true when concrete was assigned to target.
func pipitErrorsAsAssignTarget(target, concrete reflect.Value, targetType reflect.Type) bool {
	if !concrete.IsValid() {
		return false
	}
	if concrete.Type() == targetType || concrete.Type().AssignableTo(targetType) {
		target.Elem().Set(concrete)
		return true
	}
	return false
}

// invokeAsMethodIfPresent dispatches a pipit-side As method on concrete.
//
// Takes vm (*VM) which provides the method registry.
// Takes concrete (reflect.Value) which is the current chain value.
// Takes target (reflect.Value) which is the **T pointer.
//
// Returns true when the As method ran and reported a match.
func invokeAsMethodIfPresent(vm *VM, concrete, target reflect.Value) bool {
	return invokeBoolMethodOnConcrete(vm, concrete, target, errorAsMethodSuffix)
}

// invokeBoolMethodOnConcrete is the shared helper backing the chain-walk As() and Is()
// method dispatches. It looks up the named suffix (`.As` or `.Is`) on concrete's
// pipit-side type via the method table, and when present invokes the method via
// boundMethodVM with target as a single argument, returning the method's bool result.
//
// Centralising the dispatch removes the verbatim duplication between
// invokeAsMethodIfPresent and invokeIsMethodIfPresent that golangci's dupl linter
// flagged. The two callers differ only in which suffix they pass.
//
// Takes vm (*VM) which provides the method registry.
// Takes concrete (reflect.Value) which is the current chain value.
// Takes target (reflect.Value) which is the comparison or assignment target passed as the
// method argument.
// Takes methodSuffix (string) which is `.As` or `.Is` so the lookup goes into the right
// slot in pipit's per-type method table.
//
// Returns true when the dispatched method returned true.
func invokeBoolMethodOnConcrete(vm *VM, concrete, target reflect.Value, methodSuffix string) bool {
	typeName, ok := pipitTypeName(vm, concrete)
	if !ok {
		return false
	}
	methodIndex, ok := vm.rootFunction.MethodTable()[typeName+methodSuffix]
	if !ok {
		return false
	}
	if int(methodIndex) >= len(vm.rootFunction.Functions) {
		return false
	}
	callee := vm.rootFunction.Functions[methodIndex]
	bound := &boundMethodVM{vm: vm, callee: callee, limits: vm.Limits, rootFunctionOverride: nil}
	receiver := pointerReceiverFor(concrete)
	results := bound.invoke(receiver, []reflect.Value{target}, identityArg)
	return len(results) > 0 && results[0].IsValid() && results[0].Bool()
}

// errorsIsDirectEqual tests whether the chain's concrete error matches the target value
// directly. Comparable types use reflect.Value.Equal; non-comparable types fall through
// to false.
//
// Takes concrete (reflect.Value) which is the current chain value.
// Takes target (reflect.Value) which is the comparison target.
//
// Returns true when concrete equals target.
func errorsIsDirectEqual(concrete, target reflect.Value) bool {
	if !concrete.IsValid() || !target.IsValid() {
		return false
	}
	if concrete.Type() != target.Type() {
		return false
	}
	if !concrete.Comparable() {
		return false
	}
	return concrete.Equal(target)
}

// invokeIsMethodIfPresent dispatches a pipit-side Is method on concrete.
//
// Takes vm (*VM) which provides the method registry.
// Takes concrete (reflect.Value) which is the current chain value.
// Takes target (reflect.Value) which is the comparison target passed as the method
// argument.
//
// Returns true when the Is method ran and reported a match.
func invokeIsMethodIfPresent(vm *VM, concrete, target reflect.Value) bool {
	return invokeBoolMethodOnConcrete(vm, concrete, target, errorIsMethodSuffix)
}

// advanceErrorChain returns the next error in the Unwrap chain. Tries the pipit-side
// Unwrap method first (so chains constructed entirely of pipit-synthesised types unwrap
// correctly), then falls back to the stdlib errors.Unwrap (which handles native error
// types and any chain link that satisfies the public Unwrap()-error interface).
//
// Takes vm (*VM) which provides the method registry.
// Takes original (reflect.Value) which is the chain node before adapter unwrapping, tried
// first by the native Unwrap fallback.
// Takes concrete (reflect.Value) which is the current chain value.
//
// Returns the next chain value and true when an unwrap method produces a non-nil error;
// returns an invalid value and false when the chain ends.
func advanceErrorChain(vm *VM, original, concrete reflect.Value) (reflect.Value, bool) {
	if next, ok := invokeUnwrapMethodIfPresent(vm, concrete); ok {
		return next, true
	}
	if next, ok := advanceViaNativeUnwrap(vm, original); ok {
		return next, true
	}
	return advanceViaNativeUnwrap(vm, concrete)
}

// pipitErrorsUnwrap is the pipit-aware errors.Unwrap. Mirrors stdlib semantics but
// tolerates pipit-synthesised concrete types whose reflect.Type carries no MethodSet.
//
// Takes vm (*VM) which provides typeNames + MethodTable access for resolving pipit-side
// Unwrap methods.
// Takes err (reflect.Value) which is the error to unwrap.
//
// Returns the unwrapped reflect.Value wrapping the next error in the chain, or an invalid
// reflect.Value when the chain ends.
func pipitErrorsUnwrap(vm *VM, err reflect.Value) reflect.Value {
	concrete := unwrapErrorInterfaceValue(err)
	if !concrete.IsValid() {
		return reflect.Value{}
	}
	next, ok := advanceErrorChain(vm, err, concrete)
	if !ok || !next.IsValid() {
		return reflect.Value{}
	}
	return next
}

// invokeUnwrapMethodIfPresent dispatches a pipit-side Unwrap()-error method on concrete
// and returns its result. Returns an invalid value and false when no pipit Unwrap method
// is registered or the call yields nil.
//
// Takes vm (*VM) which provides the method registry.
// Takes concrete (reflect.Value) which is the current chain value.
//
// Returns the unwrapped value and true on success.
func invokeUnwrapMethodIfPresent(vm *VM, concrete reflect.Value) (reflect.Value, bool) {
	typeName, ok := pipitTypeName(vm, concrete)
	if !ok {
		return reflect.Value{}, false
	}
	methodIndex, ok := vm.rootFunction.MethodTable()[typeName+errorUnwrapMethodSuffix]
	if !ok {
		return reflect.Value{}, false
	}
	if int(methodIndex) >= len(vm.rootFunction.Functions) {
		return reflect.Value{}, false
	}
	callee := vm.rootFunction.Functions[methodIndex]
	bound := &boundMethodVM{vm: vm, callee: callee, limits: vm.Limits, rootFunctionOverride: nil}
	receiver := pointerReceiverFor(concrete)
	results := bound.invoke(receiver, nil, identityArg)
	if len(results) == 0 {
		return reflect.Value{}, false
	}
	next := unwrapErrorInterfaceValue(results[0])
	if !next.IsValid() {
		return reflect.Value{}, false
	}
	return next, true
}

// advanceViaNativeUnwrap walks the chain by invoking concrete's Unwrap() method through
// native reflect when concrete's reflect.Type declares one. This handles the stdlib-error
// case (native errors, fmt.Errorf("%w", ...) results, etc.) without pipit-side wrapping.
//
// The native Unwrap() call is contained by a recover guard (safeNativeUnwrapCall):
// errors.As / errors.Is reach this path directly rather than through
// safeReflectCallOrCallSlice, so a panicking native Unwrap() would otherwise crash the
// host.
//
// Takes vm (*VM) which carries the dispatchDepth re-panic discipline.
// Takes concrete (reflect.Value) which is the current chain value.
//
// Returns the unwrapped value and true on success.
func advanceViaNativeUnwrap(vm *VM, concrete reflect.Value) (reflect.Value, bool) {
	if !concrete.IsValid() {
		return reflect.Value{}, false
	}
	method := concrete.MethodByName(errorUnwrapMethodName)
	if !method.IsValid() {
		if concrete.Kind() == reflect.Pointer && !concrete.IsNil() {
			method = concrete.Elem().MethodByName(errorUnwrapMethodName)
		}
		if !method.IsValid() {
			return reflect.Value{}, false
		}
	}
	if method.Type().NumIn() != 0 || method.Type().NumOut() != 1 {
		return reflect.Value{}, false
	}
	results, ok := safeNativeUnwrapCall(vm, method)
	if !ok || len(results) == 0 {
		return reflect.Value{}, false
	}
	next := unwrapErrorInterfaceValue(results[0])
	if !next.IsValid() {
		return reflect.Value{}, false
	}
	return next, true
}
