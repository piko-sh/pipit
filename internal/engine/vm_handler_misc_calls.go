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
	"errors"
	"fmt"
	"io"
	"log/slog"
	"reflect"
	"runtime"
	"runtime/debug"
	"strings"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/logging"
	"pipit.sh/pipit/internal/symtab/typemodel"

	"pipit.sh/pipit/internal/isa"
)

const (
	// maxPromotedMethodDepth caps recursion through embedded struct fields when resolving a
	// promoted method. Go itself rejects cyclic embedding, but user-crafted types with
	// deeply nested embeds (or a programmer error that introduces a cycle via pointer
	// embedding) would otherwise blow the stack.
	maxPromotedMethodDepth = 64

	// maxDeferStackSize caps the per-VM defer stack to bound the OOM surface from runaway
	// defer accumulation in user code.
	maxDeferStackSize = 1 << 16

	// DeferModeChain registers the defer on the VM-wide deferStack the way handleDefer
	// always has. Encoded as zero so existing bytecode and any Compiler emission paths that
	// haven't been updated continue to behave byte-for-byte identically.
	DeferModeChain uint8 = 0

	// DeferModeTrivial stashes the call in the frame's simpleDefer slot, bypassing the
	// deferStack append and the per-defer heap allocation.
	DeferModeTrivial uint8 = 1

	// DeferModeBuiltin defers a builtin call (`defer close(ch)`, `defer println(x)`):
	// operand A carries the isa.Builtin* identifier instead of a register and the extension
	// words carry the arguments, evaluated at the defer statement as usual.
	DeferModeBuiltin uint8 = 2

	// DeferModeSpread is a flag OR'ed into DeferModeChain when the deferred call spreads a
	// slice into a variadic parameter (`defer f(xs...)`): the last argument is the whole
	// variadic slice and is passed as such instead of being packed with its neighbours.
	DeferModeSpread uint8 = 0x80

	// GoModeBuiltin marks `go builtin(args)` on isa.OpGo the same way. The builtin runs
	// synchronously on the launching goroutine: none of the permitted builtins blocks, and
	// their effects (a close, a delete, a print) are the same from either goroutine.
	GoModeBuiltin uint8 = 1
)

var (
	// reflectTypeReflectType caches the reflect.Type for the reflect.Type interface itself,
	// used to detect when pipit code is calling a method on a `reflect.Type` value so the
	// dispatch can filter pipit-synth sentinel fields out of NumField/Field results.
	reflectTypeReflectType = reflect.TypeFor[reflect.Type]()

	// reflectValueReflectType caches the reflect.Type of reflect.Value itself, used to
	// detect pipit code calling a method on a reflect.Value receiver.
	reflectValueReflectType = reflect.TypeFor[reflect.Value]()
)

// runtimePanicError is a pipit-side runtime error compatible with both error and
// runtime.Error.
type runtimePanicError struct {
	// message is the preformatted runtime panic text.
	message string
}

// newRuntimePanicError formats a runtime-panic message and wraps it in a
// runtimePanicError so deferred recover() observes a value whose fmt.Sprintf("%v") output
// matches Go's native runtime error format.
//
// Takes format (string) which is the message format string.
// Takes args (...any) which are the format arguments.
//
// Returns a *runtimePanicError ready to pass to raiseNativePanicAsInterpreted.
func newRuntimePanicError(format string, args ...any) *runtimePanicError {
	return &runtimePanicError{message: fmt.Sprintf(format, args...)}
}

// Error returns the preformatted runtime panic message.
//
// Returns string which is the preformatted panic message.
func (e *runtimePanicError) Error() string { return e.message }

// RuntimeError marks the value as satisfying the runtime.Error interface so
// errors.As(err, &re) where re is runtime.Error keeps working for pipit-raised runtime
// panics.
func (*runtimePanicError) RuntimeError() {}

// Is preserves errors.Is compatibility with pipit sentinels.
//
// Compares against errIndexOutOfRange, errSliceOutOfRange, errNilPointerIndex, and
// errDivisionByZero. The runtime panic message is the canonical surface; the sentinel
// match lets unit tests and callers that check for specific runtime error categories
// continue to work after the panic-routing rework that promotes inline runtime errors to
// interpreted-side panics.
//
// Takes target (error) which is the sentinel being compared against.
//
// Returns bool which is true when the message's prefix matches the expected runtime
// category for target.
func (e *runtimePanicError) Is(target error) bool {
	switch target {
	case fault.ErrIndexOutOfRange:
		return strings.HasPrefix(e.message, "runtime error: index out of range")
	case fault.ErrSliceOutOfRange:
		return strings.HasPrefix(e.message, "runtime error: slice bounds out of range")
	case errNilPointerIndex, fault.ErrDivisionByZero:
		return strings.Contains(e.message, "nil pointer") || strings.Contains(e.message, "divide by zero")
	}
	return false
}

// invariantError is the panic value an opcode handler raises when it finds state the
// compiler promised could not occur. handleRecoveredHandlerPanic surfaces it as the run's
// error without consulting the defer stack, so interpreted recover() never observes an
// interpreter defect as if it were a program panic.
type invariantError struct {
	// message is the human-readable invariant violation text.
	message string
}

// newInvariantError builds the panic value for a broken interpreter invariant.
//
// Takes format (string) which is the message format string.
// Takes args (...any) which are the format arguments.
//
// Returns an *invariantError ready to panic with or to store in vm.evalError.
func newInvariantError(format string, args ...any) *invariantError {
	return &invariantError{message: fmt.Sprintf(format, args...)}
}

// Error returns the invariant message under the fault.ErrInterpreterInvariant prefix.
//
// Returns string which is the formatted error message.
func (e *invariantError) Error() string {
	return fault.ErrInterpreterInvariant.Error() + ": " + e.message
}

// Unwrap exposes fault.ErrInterpreterInvariant to errors.Is.
//
// Returns error which is fault.ErrInterpreterInvariant.
func (*invariantError) Unwrap() error {
	return fault.ErrInterpreterInvariant
}

// uncaughtPanicError is the error an execution returns when a panic unwinds every frame
// without being recovered. Its text keeps the "panic: value" (or "goroutine panicked:
// value") shape callers match on, errors.Is(err, fault.ErrUncaughtPanic) identifies the
// class, and an error-valued panic stays reachable through errors.Is and errors.As.
type uncaughtPanicError struct {
	// value is the panic value as the program raised it.
	value any

	// prefix is the text before the value in the error message.
	prefix string

	// stack is the rendered interpreted frames when the panic started, or "" when no stack
	// was recorded.
	stack string
}

// newuncaughtPanicError wraps the value of a panic that reached the top of the main
// goroutine.
//
// Takes value (any) which is the panic value.
//
// Returns *uncaughtPanicError whose text reads "panic: value".
func newuncaughtPanicError(value any) *uncaughtPanicError {
	return &uncaughtPanicError{value: value, prefix: "panic: ", stack: ""}
}

// newGoroutinePanicError wraps the value of a panic that killed a spawned goroutine.
//
// Takes value (any) which is the panic value.
//
// Returns *uncaughtPanicError whose text reads "goroutine panicked: value".
func newGoroutinePanicError(value any) *uncaughtPanicError {
	return &uncaughtPanicError{value: value, prefix: "goroutine panicked: ", stack: ""}
}

// Error renders the panic value under its prefix.
//
// Returns string which is the formatted error message.
func (e *uncaughtPanicError) Error() string {
	return e.prefix + fmt.Sprint(e.value)
}

// Unwrap exposes fault.ErrUncaughtPanic and, when the panic value is an error, that
// error.
//
// Returns []error containing the sentinel and the value error.
func (e *uncaughtPanicError) Unwrap() []error {
	if err, ok := e.value.(error); ok {
		return []error{fault.ErrUncaughtPanic, err}
	}
	return []error{fault.ErrUncaughtPanic}
}

// Value returns the panic value as the program raised it.
//
// Returns any which is the original panic value.
func (e *uncaughtPanicError) Value() any {
	return e.value
}

// Stack returns the interpreted frames recorded when the panic started, rendered as Go's
// traceback renders them; empty when the panic carries no stack.
//
// Returns string which is the rendered frames.
func (e *uncaughtPanicError) Stack() string {
	return e.stack
}

// NewGoroutinePanicError is newGoroutinePanicError for the service layer, which observes
// a goroutine's panic after the main goroutine returned.
//
// Takes value (any) which is the panic value.
//
// Returns error whose text reads "goroutine panicked: value".
func NewGoroutinePanicError(value any) error {
	return newGoroutinePanicError(value)
}

// UncaughtPanicValue returns the value of the panic behind an uncaught-panic error.
//
// Takes err (error) which is an execution error.
//
// Returns the panic value and true, or nil and false when err is not an uncaught panic.
func UncaughtPanicValue(err error) (any, bool) {
	if panicErr, ok := errors.AsType[*uncaughtPanicError](err); ok {
		return panicErr.Value(), true
	}
	return nil, false
}

// UncaughtPanicStack returns the interpreted stack recorded when the panic behind an
// uncaught-panic error started, rendered as Go's traceback renders frames.
//
// Takes err (error) which may wrap an uncaught panic.
//
// Returns string which is empty when err is not an uncaught panic or carries no stack.
func UncaughtPanicStack(err error) string {
	var uncaught *uncaughtPanicError
	if !errors.As(err, &uncaught) {
		return ""
	}
	return uncaught.stack
}

// resolveReceiverTypeName picks the pipit receiver type name.
//
// It tries four signals in order: the reflect.Type's own Name() (which covers named
// non-generic types installed via reflect.StructOf with a recovered name), then
// site.ArgumentStaticTypeNames[0] (which covers generic instantiations such as Box[int]
// -> "Box" and typedef-of- builtin receivers where the reflect.Type collapses to the
// underlying primitive), then the typeNames map registered by registerMethodReceiver (for
// struct-typed pipit receivers whose reflect.Type is anonymous), and finally the pipit
// `_pipitID_<Name>` sentinel field on the synthesised struct.
//
// Takes vm (*VM) which exposes the typeNames map registered at compile time.
// Takes site (*CallSite) which carries the per-call-site static type metadata.
// Takes receiverType (reflect.Type) which is the runtime type of the method receiver.
//
// Returns string which is the source-level type name; empty when unresolved so the slow
// path falls through to the native reflect method-set check.
func resolveReceiverTypeName(vm *VM, site *program.CallSite, receiverType reflect.Type) string {
	if info, ok := typemodel.LookupNamedScalarPoolInfo(receiverType); ok {
		return info.BareName
	}
	name := receiverType.Name()
	if name != "" && !isBuiltinScalarTypeName(name) {
		return name
	}
	if site != nil && len(site.ArgumentStaticTypeNames) > 0 {
		if staticName := site.ArgumentStaticTypeNames[0]; staticName != "" {
			return staticName
		}
	}
	if vm != nil && vm.rootFunction != nil && vm.rootFunction.TypeNames != nil {
		if registered := vm.rootFunction.TypeNames[receiverType]; registered != "" {
			return registered
		}
	}
	if bareName := bareSentinelName(receiverType); bareName != "" {
		return bareName
	}
	return name
}

// bareSentinelName extracts the source-level type name from a pipit synth struct's
// `_pipitID_<Name>` sentinel field. Unlike extractPipitSentinelTypeName (which returns
// the fmt-friendly `[*]main.<Name>` form), this helper returns just `<Name>` for
// methodTable key lookup, which is registered as `<Name>.<MethodName>` (no package
// prefix, no pointer indicator).
//
// Takes t (reflect.Type) which is the candidate struct type (auto-unwrapped if a
// Pointer).
//
// Returns the bare source-level name, or the empty string when t has no pipit sentinel
// field.
func bareSentinelName(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return ""
	}
	for field := range t.Fields() {
		if strings.HasPrefix(field.Name, pipitIDFieldPrefix) {
			return field.Name[len(pipitIDFieldPrefix):]
		}
	}
	return ""
}

// sentinelTypeArgs returns an instantiated generic type's rendered type arguments from
// the sentinel tag, or nil for a type that is not an instantiation.
//
// Takes t (reflect.Type) which is the synthesised struct type or a pointer to it.
//
// Returns []string which are the type arguments in declaration order.
func sentinelTypeArgs(t reflect.Type) []string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	for field := range t.Fields() {
		if !strings.HasPrefix(field.Name, pipitIDFieldPrefix) {
			continue
		}
		args, ok := field.Tag.Lookup(program.SentinelTypeArgsTagKey)
		if !ok || args == "" {
			return nil
		}
		return strings.Split(args, program.TypeArgsTagSeparator)
	}
	return nil
}

// sentinelTypeArgsSuffix returns the "[int,string]" suffix an instantiated generic type
// prints with, read from the type-argument tag the compiler leaves on the sentinel field,
// or "" for a type that is not an instantiation. Method tables key on the bare name, so
// this is only for rendering.
//
// Takes t (reflect.Type) which is the synthesised struct type or a pointer to it.
//
// Returns string which is the bracketed suffix or "".
func sentinelTypeArgsSuffix(t reflect.Type) string {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return ""
	}
	for field := range t.Fields() {
		if !strings.HasPrefix(field.Name, pipitIDFieldPrefix) {
			continue
		}
		args := sentinelTypeArgs(t)
		if len(args) == 0 {
			return ""
		}
		return "[" + strings.Join(args, ",") + "]"
	}
	return ""
}

// isBuiltinScalarTypeName reports whether n names a Go scalar type.
//
// Used by resolveReceiverTypeName to detect when reflect.Type.Name() returned the
// underlying primitive rather than the source-level defined type (e.g. `type Tag int`'s
// reflect.Type reports "int64", not "Tag").
//
// Takes n (string) which is the reflect.Type.Name() value to test.
//
// Returns bool which is true when n names a Go built-in scalar type.
func isBuiltinScalarTypeName(n string) bool {
	switch n {
	case "int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64", "uintptr",
		"byte", "rune",
		"float32", "float64",
		"complex64", "complex128",
		"bool", "string":
		return true
	}
	return false
}

// handleCallBuiltin dispatches a call to a builtin function such as print, println, or
// clear by reading arguments from extension words.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which provides the bytecode extension words.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the builtin ID and count.
//
// Returns OpResult indicating the next execution step.
func handleCallBuiltin(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	argumentCount := int(instruction.C)
	if cap(vm.builtinArgumentsBuffer) < argumentCount {
		vm.builtinArgumentsBuffer = make([]any, argumentCount)
	}
	arguments := vm.builtinArgumentsBuffer[:argumentCount]
	readBuiltinArguments(arguments, frame, registers, argumentCount)
	result := dispatchBuiltin(vm, frame, registers, instruction.B, argumentCount, arguments)
	clear(arguments)
	return result
}

// readBuiltinArguments decodes extension words from the bytecode stream and populates the
// arguments slice with concrete register values.
//
// Takes arguments ([]any) which is the destination slice to populate.
// Takes frame (*CallFrame) which provides access to the bytecode body.
// Takes registers (*Registers) which holds the source values.
// Takes argumentCount (int) which specifies how many extension words to consume.
func readBuiltinArguments(arguments []any, frame *CallFrame, registers *Registers, argumentCount int) {
	for i := range argumentCount {
		extensionWord := readExtensionWord(frame)
		frame.ProgramCounter++
		arguments[i] = readOneBuiltinArg(registers, extensionWord)
	}
}

// readOneBuiltinArg extracts a single value from the registers based on the kind encoded
// in the extension word.
//
// Named banks are read directly; the default reads the general bank, which is where a
// builtin argument of any other kind is held.
//
// Takes registers (*Registers) which holds the source values.
// Takes extensionWord (isa.Instruction) which encodes the register index and kind.
//
// Returns any which holds the value, or nil for unrecognised kinds.
func readOneBuiltinArg(registers *Registers, extensionWord isa.Instruction) any {
	switch isa.RegisterKind(extensionWord.B) {
	case isa.RegisterInt:
		return registers.Ints[extensionWord.A]
	case isa.RegisterFloat:
		return registers.Floats[extensionWord.A]
	case isa.RegisterString:
		return registers.Strings[extensionWord.A]
	case isa.RegisterGeneral:
		if registers.General[extensionWord.A].IsValid() {
			return registers.General[extensionWord.A].Interface()
		}
		return nil
	case isa.RegisterBool:
		return registers.Bools[extensionWord.A]
	case isa.RegisterUint:
		return registers.Uints[extensionWord.A]
	case isa.RegisterComplex:
		return registers.Complex[extensionWord.A]
	default:
		return nil
	}
}

// dispatchBuiltin executes the appropriate builtin operation (print, println, or clear)
// and returns the resulting op status.
//
// Takes vm (*VM) which provides the output writer.
// Takes frame (*CallFrame) which provides access to the bytecode.
// Takes registers (*Registers) which holds the current register values.
// Takes builtinID (uint8) which selects which builtin to invoke.
// Takes argumentCount (int) which is the argument count.
// Takes arguments ([]any) which holds the concrete argument values.
//
// Returns OpResult indicating success or panic on error.
func dispatchBuiltin(vm *VM, frame *CallFrame, registers *Registers, builtinID uint8, argumentCount int, arguments []any) OpResult {
	switch builtinID {
	case isa.BuiltinPrint:
		return execBuiltinPrint(vm, arguments, false)
	case isa.BuiltinPrintln:
		return execBuiltinPrint(vm, arguments, true)
	case isa.BuiltinClear:
		execBuiltinClear(frame, registers, argumentCount)
	}
	return opContinue
}

// execBuiltinPrint writes arguments to stderr, optionally appending a newline.
//
// Takes vm (*VM) which provides the limited stderr writer.
// Takes arguments ([]any) which holds the values to print.
// Takes newline (bool) which controls whether a trailing newline is added.
//
// Returns OpResult indicating success or panic if the write fails.
func execBuiltinPrint(vm *VM, arguments []any, newline bool) OpResult {
	_, err := io.WriteString(vm.limitedStderr(), formatBuiltinPrint(arguments, newline))
	if err != nil {
		vm.evalError = err
		return opPanicError
	}
	if vm.checkOutputLimit() {
		return opPanicError
	}
	return opContinue
}

// execBuiltinClear implements the builtin clear() for maps and slices.
//
// Takes frame (*CallFrame) which provides the bytecode extension word.
// Takes registers (*Registers) which holds the collection to clear.
// Takes argumentCount (int) which must be 1 for the operation to proceed.
func execBuiltinClear(frame *CallFrame, registers *Registers, argumentCount int) {
	if argumentCount != 1 {
		return
	}
	extensionWord := frame.Function.Body[frame.ProgramCounter-1]
	v := registers.General[extensionWord.A]
	if !v.IsValid() {
		return
	}
	switch v.Kind() {
	case reflect.Map, reflect.Slice:
		v.Clear()
	default:
	}
}

// handleDefer captures a deferred closure call and dispatches by mode.
//
// The mode is encoded in instruction.c. Mode zero (chain) preserves the pre-existing
// append-to-deferStack path so bytecode that does not opt in continues to behave
// identically. Mode one (trivial) writes the function and args into a frame-local slot,
// avoiding the per-defer slice allocation and stack append.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which provides the bytecode extension words.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the closure register, argument count, and
// mode.
//
// Returns OpResult indicating the next execution step.
func handleDefer(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	argumentCount := int(instruction.B)
	mode := instruction.C &^ DeferModeSpread
	spread := instruction.C&DeferModeSpread != 0
	if mode == DeferModeBuiltin {
		return registerBuiltinDefer(vm, frame, registers, instruction.A, argumentCount)
	}
	target := registers.General[instruction.A]

	if mode == DeferModeTrivial {
		return registerTrivialDefer(vm, frame, registers, target, argumentCount)
	}

	if len(vm.deferStack) >= maxDeferStackSize {
		vm.evalError = fmt.Errorf("%w: %d entries", errDeferStackExhausted, maxDeferStackSize)
		return opPanicError
	}

	arguments := unpackReflectArgs(frame, registers, argumentCount)
	materialiseReflectStringArgs(vm.Arena, arguments)
	materialiseReflectDeferArgs(vm.Arena, arguments)
	if closure, ok := reflect.TypeAssert[*RuntimeClosure](target); ok {
		arguments = packDeferredVariadicArguments(vm, closure.Function, arguments, spread)
		vm.deferStack = append(vm.deferStack, deferredCall{Function: closure, arguments: arguments, frameIndex: vm.FramePointer,
			direct: nil, nativeFunction: reflect.Value{}, started: false, builtin: 0, spread: spread})
		return opContinue
	}
	if !target.IsValid() || target.Kind() != reflect.Func {
		vm.evalError = errors.New("defer target is not callable")
		return opPanicError
	}
	vm.deferStack = append(vm.deferStack, deferredCall{nativeFunction: target, arguments: arguments, frameIndex: vm.FramePointer,
		Function: nil, direct: nil, started: false, builtin: 0, spread: spread})
	return opContinue
}

// packDeferredVariadicArguments packs the trailing arguments of a deferred variadic call
// into the callee's variadic slice.
//
// A spread call (`defer f(xs...)`) already carries the slice as its last argument and a
// fixed-arity callee needs nothing; both come back unchanged. A callee loaded from
// bytecode has no recorded slice type and packs into []any.
//
// Takes vm (*VM) which is the virtual machine.
// Takes callee (*program.CompiledFunction) which is the deferred function.
// Takes arguments ([]reflect.Value) which are the evaluated operands.
// Takes spread (bool) which is true for the `f(xs...)` form.
//
// Returns []reflect.Value which is the argument list the callee's parameters expect.
func packDeferredVariadicArguments(vm *VM, callee *program.CompiledFunction, arguments []reflect.Value, spread bool) []reflect.Value {
	if callee == nil || !callee.IsVariadic || spread {
		return arguments
	}
	fixedCount := min(max(len(callee.ParameterKinds)-1, 0), len(arguments))
	sliceType := callee.VariadicSliceType
	if sliceType == nil {
		sliceType = reflect.TypeFor[[]any]()
	}
	elementType := sliceType.Elem()
	variadicCount := len(arguments) - fixedCount
	packed := reflect.MakeSlice(sliceType, variadicCount, variadicCount)
	for i := range variadicCount {
		value := coerceValue(vm, arguments[fixedCount+i], elementType)
		if value.IsValid() && value.Type().AssignableTo(elementType) {
			packed.Index(i).Set(value)
		}
	}
	out := make([]reflect.Value, 0, fixedCount+1)
	out = append(out, arguments[:fixedCount]...)
	return append(out, packed)
}

// registerTrivialDefer is the fast path for the trivial-defer classification. It writes
// the deferred call's function and arguments into the frame's simpleDefer record
// (allocated lazily on first use, reused across re-arming), avoiding the per-defer slice
// allocation, the slice-grow on the deferStack append, and the LIFO walk in runDefers.
//
// Takes vm (*VM) which provides the arena for string materialisation.
// Takes frame (*CallFrame) which owns the simpleDefer slot.
// Takes registers (*Registers) which provides the source registers for argument
// resolution.
// Takes target (reflect.Value) which is the deferred function value.
// Takes argumentCount (int) which is the eagerly-evaluated argument count from the
// instruction's B operand.
//
// Returns OpResult indicating the next execution step.
func registerTrivialDefer(vm *VM, frame *CallFrame, registers *Registers, target reflect.Value, argumentCount int) OpResult {
	if frame.simpleDefer == nil {
		frame.simpleDefer = &SimpleDeferRecord{
			target:         nil,
			nativeFunction: reflect.Value{},
			arguments:      nil,
			active:         false,
			consumed:       false,
			direct:         false,
			ArgumentCount:  0,
			argumentKinds:  [isa.MaxTrivialDeferArgs]isa.RegisterKind{},
			argumentSlots:  [isa.MaxTrivialDeferArgs]uint8{},
			directBools:    [directDeferSlotsPerKind]bool{},
			banks: Registers{
				Ints:          nil,
				Floats:        nil,
				Strings:       nil,
				General:       nil,
				Bools:         nil,
				Uints:         nil,
				Complex:       nil,
				SlicesInt:     nil,
				slicesFloat:   nil,
				slicesString:  nil,
				slicesBool:    nil,
				slicesUint:    nil,
				slicesByte:    nil,
				lastAllocMask: 0,
			},
			directStrings:   [directDeferSlotsPerKind]string{},
			directGenerals:  [directDeferSlotsPerKind]reflect.Value{},
			directInts:      [directDeferSlotsPerKind]int64{},
			directFloats:    [directDeferSlotsPerKind]float64{},
			directUints:     [directDeferSlotsPerKind]uint64{},
			directComplexes: [directDeferSlotsPerKind]complex128{},
		}
	}
	record := frame.simpleDefer
	record.consumed = false
	if closure, ok := trivialDeferClosure(target); ok && record.TryCaptureDirect(vm, frame, registers, argumentCount) {
		record.arguments = record.arguments[:0]
		record.target = closure
		record.nativeFunction = reflect.Value{}
		record.active = true
		return opContinue
	}
	record.direct = false
	if cap(record.arguments) < argumentCount {
		record.arguments = make([]reflect.Value, argumentCount)
	} else {
		record.arguments = record.arguments[:argumentCount]
	}
	for i := range argumentCount {
		extensionWord := readExtensionWord(frame)
		frame.ProgramCounter++
		record.arguments[i] = registerToReflectValue(vm.Arena, registers, isa.RegisterKind(extensionWord.C), extensionWord.B)
	}
	materialiseReflectStringArgs(vm.Arena, record.arguments)
	materialiseReflectDeferArgs(vm.Arena, record.arguments)
	if closure, ok := reflect.TypeAssert[*RuntimeClosure](target); ok {
		record.target = closure
		record.nativeFunction = reflect.Value{}
	} else {
		if !target.IsValid() || target.Kind() != reflect.Func {
			vm.evalError = errors.New("defer target is not callable")
			return opPanicError
		}
		record.target = nil
		record.nativeFunction = target
	}
	record.active = true
	return opContinue
}

// materialiseReflectStringArgs clones any arena-owned string arguments to ensure they
// remain valid after the arena region is reclaimed.
//
// Takes arena (*RegisterArena) which is used to check string ownership.
// Takes arguments ([]reflect.Value) which holds the argument values to check.
func materialiseReflectStringArgs(arena *RegisterArena, arguments []reflect.Value) {
	for i, argument := range arguments {
		if argument.Kind() == reflect.String && arena.OwnsString(argument.String()) {
			arguments[i] = reflect.ValueOf(strings.Clone(argument.String()))
		}
	}
}

// materialiseReflectStructArgs replaces arena-resident struct, array, and slice arguments
// with heap-anchored copies for cross-arena escapes.
//
// Takes arena (*RegisterArena) which owns the slabs to test against.
// Takes arguments ([]reflect.Value) which is updated in place.
func materialiseReflectStructArgs(arena *RegisterArena, arguments []reflect.Value) {
	if arena == nil {
		return
	}
	for i, argument := range arguments {
		arguments[i] = materialiseArenaValueUnconditional(arena, argument)
	}
}

// materialiseReflectDeferArgs is the defer-capture sibling of
// materialiseReflectStructArgs, using materialiseArenaValueAliasing for slices to
// preserve Data-pointer aliasing with the caller's slice.
//
// Takes arena (*RegisterArena) which owns the slabs to test against.
// Takes arguments ([]reflect.Value) which is updated in place.
func materialiseReflectDeferArgs(arena *RegisterArena, arguments []reflect.Value) {
	if arena == nil {
		return
	}
	for i, argument := range arguments {
		arguments[i] = materialiseArenaValueAliasing(arena, argument)
	}
}

// handlePanic initiates a panic in the VM by setting the panic value from the source
// register and unwinding the call stack to find a recover point.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the panic value register.
// Takes instruction (instruction) which encodes the source register index.
//
// Returns OpResult indicating frame change or panic error.
func handlePanic(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	v := registers.General[instruction.A]
	var newPanicValue any
	if v.IsValid() {
		v = MaterialiseArenaValue(vm.Arena, v)
		newPanicValue = v.Interface()
	} else {
		newPanicValue = new(runtime.PanicNilError)
	}
	if vm.panicking {
		vm.panicValue = newPanicValue
		nested := newuncaughtPanicError(newPanicValue)
		nested.stack = vm.panicStackWithCallback()
		vm.evalError = nested

		for vm.FramePointer >= vm.baseFramePointer {
			vm.popFrame()
		}
		return opPanicError
	}
	vm.panicValue = newPanicValue
	vm.panicUnwound = nil
	vm.beginPanicFrames()
	if vm.debugPanicPoint(newPanicValue) {
		vm.evalError = fault.ErrDebuggerStop
		return opPanicError
	}
	vm.panicking = true
	err := vm.unwindPanic()
	if err == nil {
		if vm.FramePointer < vm.baseFramePointer {
			return opDone
		}
		return opFrameChanged
	}
	vm.evalError = err
	return opPanicError
}

// raiseNativePanicAsInterpreted converts a recovered Go panic from a native operation
// into an interpreter-level panic that interpreted defer/recover can observe. Used by
// handlers that call into reflect operations which may themselves panic (channel
// send/recv on closed/nil channels, sync.* misuse, reflect.Select with no cases, etc.).
//
// Takes vm (*VM) which is the executing VM.
// Takes recovered (any) which is the value returned by Go's recover().
//
// Returns OpResult after attempting unwind to a recover handler.
func raiseNativePanicAsInterpreted(vm *VM, recovered any) OpResult {
	if recovered == nil {
		vm.panicValue = new(runtime.PanicNilError)
		vm.callbackPanicStack = ""
	} else {
		vm.recordRecoveredPanic(recovered)
	}
	vm.panicUnwound = nil
	vm.beginPanicFrames()
	if vm.debugPanicPoint(vm.panicValue) {
		vm.evalError = fault.ErrDebuggerStop
		return opPanicError
	}
	vm.panicking = true
	err := vm.unwindPanic()
	if err == nil {
		if vm.FramePointer < vm.baseFramePointer {
			return opDone
		}
		return opFrameChanged
	}
	vm.evalError = err
	return opPanicError
}

// handleRecover attempts to recover from an active panic, storing the panic value in the
// destination register or an invalid value if not panicking.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the destination register bank.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleRecover(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if vm.panicking && vm.FramePointer == vm.recoverEligibleFrame {
		vm.panicking = false
		if vm.panicValue != nil {
			registers.General[instruction.A] = reflect.ValueOf(vm.panicValue)
		} else {
			registers.General[instruction.A] = reflect.Value{}
		}
		vm.panicValue = nil
		vm.callbackPanicStack = ""
		vm.evalError = nil
	} else {
		registers.General[instruction.A] = reflect.Value{}
	}
	return opContinue
}

// handleGo spawns a new goroutine to execute the function or closure stored in the source
// register, enforcing the configured goroutine limit.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which provides the bytecode extension words.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the closure register and count.
//
// Returns OpResult indicating the next execution step.
func handleGo(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if instruction.C == GoModeBuiltin {
		arguments := unpackReflectArgs(frame, registers, int(instruction.B))
		materialiseReflectStringArgs(vm.Arena, arguments)
		materialiseReflectDeferArgs(vm.Arena, arguments)
		vm.executeBuiltinDeferredCall(deferredCall{Function: nil, direct: nil, nativeFunction: reflect.Value{},
			arguments: arguments, frameIndex: vm.FramePointer, started: false, builtin: instruction.A, spread: false})
		if vm.panicking {
			return raiseNativePanicAsInterpreted(vm, vm.panicValue)
		}
		return opContinue
	}
	goroutineLimit := vm.ensureGoroutineLimit()
	if vm.Limits.Tracker.GoroutineCount.Add(1) > goroutineLimit {
		vm.Limits.Tracker.GoroutineCount.Add(-1)
		vm.evalError = fmt.Errorf("%w: limit %d", fault.ErrGoroutineLimit, goroutineLimit)
		return opPanicError
	}
	vm.Limits.Tracker.RegisterGoroutine()
	if !vm.hasGoroutines {
		vm.hasGoroutines = true
		vm.syncHasGoroutinesFlag()
		vm.Globals.markShared()
		materialiseGlobalStrings(vm.Globals, vm.Arena)
	}
	closure := registers.General[instruction.A]
	arguments := unpackReflectArgs(frame, registers, int(instruction.B))
	materialiseReflectStringArgs(vm.Arena, arguments)
	materialiseReflectStructArgs(vm.Arena, arguments)
	if hook := vm.Limits.CapabilityHook; hook != nil {
		fnPath := resolveNativeFunctionPath(closure)
		if err := hook.CheckFunctionCall(capabilityHookContext(vm), vm.modulePath, "go "+fnPath, arguments); err != nil {
			vm.Limits.Tracker.AbandonGoroutine()
			vm.evalError = err
			return opPanicError
		}
	}
	if closure.Type() == reflect.TypeFor[*RuntimeClosure]() {
		return launchCompiledGoroutine(vm, closure, arguments)
	}
	coerceNativeGoroutineArgs(vm, closure, arguments)
	launchNativeGoroutine(vm, vm.Limits, closure, arguments)
	return opContinue
}

// launchCompiledGoroutine spawns a new goroutine that executes a compiled closure in a
// fresh child VM with its own arena and call stack.
//
// Takes vm (*VM) which provides context and functions.
// Takes closure (reflect.Value) which holds the runtime closure to execute.
// Takes arguments ([]reflect.Value) which contains the arguments to pass.
//
// Returns OpResult after the goroutine is launched.
func launchCompiledGoroutine(vm *VM, closure reflect.Value, arguments []reflect.Value) OpResult {
	closureValue, ok := reflect.TypeAssert[*RuntimeClosure](closure)
	if !ok {
		vm.Limits.Tracker.AbandonGoroutine()
		return opContinue
	}
	parentLimits := vm.Limits
	go runCompiledGoroutine(vm, parentLimits, closureValue, arguments)
	return opContinue
}

// runCompiledGoroutine is the goroutine body for a compiled closure. It sets up a child
// VM, copies arguments into the initial frame, and runs to completion.
//
// Takes parentVM (*VM) which is the parent VM providing shared state.
// Takes limits (VMLimits) which carries the goroutine and resource limits.
// Takes closure (*RuntimeClosure) which is the compiled closure to execute.
// Takes arguments ([]reflect.Value) which holds arguments for the initial frame.
func runCompiledGoroutine(parentVM *VM, limits VMLimits, closure *RuntimeClosure, arguments []reflect.Value) {
	if limits.Tracker != nil {
		limits.Tracker.MarkGoroutineStarted()
		defer limits.Tracker.ReleaseGoroutine()
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			recordGoroutinePanicValue(parentVM, recovered)
		}
	}()
	childArena := GetRegisterArena()
	childArena.MaxArenaBytes = limits.MaxArenaBytes
	childArena.MaxAllocSize = limits.MaxAllocSize
	childArena.AttachBudgetTracker(limits.Tracker)
	defer func() {
		PutRegisterArena(childArena)
	}()
	childVM := buildChildGoroutineVM(parentVM, limits, closure, childArena)
	defer childVM.ReturnUnspentCostChunk()
	defer childVM.FinishWatcher()
	defer func() {
		childVM.drainCallbackVMs()
		childVM.CallStack = nil
		childVM.AsmCallInfoTables = nil
		childVM.dropRuntimeCallInfoFills()
		childVM.asmCallInfoBases = nil
		childVM.asmDispatchSaves = nil
	}()
	childVM.PushFrame(closure.Function)
	childFrame := childVM.currentFrame()
	if closure.upvalues != nil {
		childFrame.initialiseUpvalues(closure.upvalues, childVM.Arena)
	}
	placeReflectArgs(&childFrame.Registers, arguments, closure.Function.ParameterKinds, childArena)
	if _, err := childVM.RunDispatchedGuarded(0); err != nil {
		surfaceGoroutineRunError(parentVM, childVM, err)
	}
}

// recordGoroutinePanicValue logs a recovered panic from a compiled goroutine and stores
// it on the parent VM's globals so the host can re-raise it deterministically.
//
// Takes parentVM (*VM) which owns the goroutinePanic slot.
// Takes recovered (any) which is the non-nil recover() result.
func recordGoroutinePanicValue(parentVM *VM, recovered any) {
	stack := string(debug.Stack())
	goroutineLogger := logging.LoggerFrom(parentVM.ctx)
	goroutineLogger.Debug("Compiled goroutine panicked",
		slog.String("panic", fmt.Sprintf("%v", recovered)),
		slog.String("stack", stack))
	parentVM.Globals.recordGoroutinePanic(recovered, stack)
}

// buildChildGoroutineVM constructs the child VM that executes a goroutine launched from a
// compiled closure. The returned VM has its arena bound, ASM dispatch state seeded, and
// the call-info base for the closure's entry function pre-populated.
//
// Takes parentVM (*VM) which provides the shared globals, symbols and context.
// Takes limits (VMLimits) which carries the child VM's resource caps.
// Takes closure (*RuntimeClosure) which is the entry closure.
// Takes childArena (*RegisterArena) which is the freshly-acquired arena pinned to the
// child VM's lifetime.
//
// Returns the fully initialised child VM.
func buildChildGoroutineVM(parentVM *VM, limits VMLimits, closure *RuntimeClosure, childArena *RegisterArena) *VM {
	childVM := NewVM(parentVM.ctx, parentVM.Globals, parentVM.symbols)
	childVM.debugKind = debugThreadGoroutine
	childVM.goroutineID = parentVM.Globals.nextGoroutineID.Add(1) + rootGoroutineID
	childVM.Limits = limits

	childVM.hasGoroutines = true
	if closure.RootFunction != nil {
		childVM.functions = closure.RootFunction.Functions
		childVM.rootFunction = closure.RootFunction
	} else {
		childVM.functions = parentVM.functions
		childVM.rootFunction = parentVM.rootFunction
	}
	childVM.usesTypedSliceBanks = parentVM.usesTypedSliceBanks || bundleUsesTypedSliceBanks(childVM.rootFunction)
	childVM.Arena = childArena

	childVM.sizeArenaForEntry(childVM.rootFunction, closure.Function)
	childVM.CallStack = childArena.frameStack()
	childVM.AsmCallInfoTables = EnsureASMCallInfoTables(childVM.rootFunction)
	childVM.buildCallInfoBasesByFunction(childVM.rootFunction)
	childVM.asmCallInfoBases = childArena.callInfoBases()
	childVM.asmDispatchSaves = childArena.dispatchSaves()
	if table := childVM.AsmCallInfoTables[closure.Function]; len(table) > 0 {
		childVM.asmCallInfoBases[0] = uintptr(unsafe.Pointer(&table[0]))
	}
	return childVM
}

// surfaceGoroutineRunError forwards a dispatch error from a compiled goroutine to the
// parent VM's host channel.
//
// errGoexit is handled silently because runtime.Goexit must terminate the goroutine
// without re-raising. All other errors are logged and stored on the goroutinePanic slot
// for the host to observe.
//
// Takes parentVM (*VM) which receives the panic record.
// Takes childVM (*VM) which holds the panicValue captured during dispatch.
// Takes err (error) which is the dispatch error to surface.
func surfaceGoroutineRunError(parentVM, childVM *VM, err error) {
	if errors.Is(err, fault.ErrGoexit) || errors.Is(err, fault.ErrDebuggerStop) || stoppedBecauseMainReturned(childVM.ctx, err) {
		return
	}
	goroutineLogger := logging.LoggerFrom(parentVM.ctx)
	goroutineLogger.Debug("Compiled goroutine returned error",
		logging.ErrAttr(err))
	panicValue := any(err)
	if childVM.panicValue != nil {
		panicValue = childVM.panicValue
	}
	parentVM.Globals.recordGoroutinePanic(panicValue, "")
}

// placeReflectArgs unpacks reflect.Value arguments into typed register banks according to
// the callee's parameter kinds.
//
// Takes regs (*Registers) which is the destination register set.
// Takes arguments ([]reflect.Value) which holds the arguments to place.
// Takes parameterKinds ([]isa.RegisterKind) which is the expected kind per param.
// Takes arena (*RegisterArena) which provides bump-allocation for any cross-element-width
// slice widening (e.g. []int -> []int64 reinterpret on 64-bit targets); may be nil for
// test/library entry without an active VM.
func placeReflectArgs(regs *Registers, arguments []reflect.Value, parameterKinds []isa.RegisterKind, arena *RegisterArena) {
	var kindIndex [isa.NumRegisterKinds]int
	for i, argument := range arguments {
		if i >= len(parameterKinds) {
			break
		}
		kind := parameterKinds[i]
		dest := kindIndex[kind]
		kindIndex[kind]++
		placeOneReflectArgument(regs, argument, kind, dest, arena)
	}
}

// placeOneReflectArgument writes a single reflect.Value into the appropriate typed
// register bank at the given destination index.
//
// For typed-slice destinations the same-storage-type fast path is a direct
// reflect.TypeAssert ([]int64 / []uint64 / []float64 / []string / []bool / []byte). When
// the source's element type uses platform-width int / uint ([]int / []uint),
// unboxToTypedIntSlice and unboxToTypedUintSlice handle the storage-aliasing reinterpret
// so mutations through the typed-bank view propagate back to the caller's slice. This
// matches the unbox path used by handleCall via CopyOneCallArgument ->
// UnboxGeneralToScalar, and is required for deferred calls whose argument was captured as
// a general-bank reflect.Value (registerToReflectValue routes typed-slice registers
// through packTypedSliceToGeneral, but a general-bank source register may already hold a
// []int rather than []int64).
//
// Takes regs (*Registers) which is the destination register set.
// Takes argument (reflect.Value) which is the value to store.
// Takes kind (isa.RegisterKind) which selects the typed bank.
// Takes dest (int) which is the index within that bank.
// Takes arena (*RegisterArena) which is forwarded to the slice unboxers for narrow-int
// widening; may be nil for test/library entry without an active VM.
func placeOneReflectArgument(regs *Registers, argument reflect.Value, kind isa.RegisterKind, dest int, arena *RegisterArena) {
	switch kind {
	case isa.RegisterInt:
		regs.Ints[dest] = argument.Int()
	case isa.RegisterFloat:
		regs.Floats[dest] = argument.Float()
	case isa.RegisterString:
		regs.Strings[dest] = argument.String()
	case isa.RegisterGeneral:
		regs.General[dest] = argument
	case isa.RegisterBool:
		regs.Bools[dest] = argument.Bool()
	case isa.RegisterUint:
		regs.Uints[dest] = argument.Uint()
	case isa.RegisterComplex:
		regs.Complex[dest] = argument.Complex()
	case isa.RegisterSliceInt:
		regs.SlicesInt[dest] = unboxToTypedIntSlice(argument, arena)
	case isa.RegisterSliceFloat:
		if slice, ok := reflect.TypeAssert[[]float64](argument); ok {
			regs.slicesFloat[dest] = slice
		}
	case isa.RegisterSliceString:
		if slice, ok := reflect.TypeAssert[[]string](argument); ok {
			regs.slicesString[dest] = slice
		}
	case isa.RegisterSliceBool:
		if slice, ok := reflect.TypeAssert[[]bool](argument); ok {
			regs.slicesBool[dest] = slice
		}
	case isa.RegisterSliceUint:
		regs.slicesUint[dest] = unboxToTypedUintSlice(argument, arena)
	case isa.RegisterSliceByte:
		if slice, ok := reflect.TypeAssert[[]byte](argument); ok {
			regs.slicesByte[dest] = slice
		}
	default:
	}
}

// coerceNativeGoroutineArgs adjusts arguments to match the native function's parameter
// types, mirroring the coercion done by buildReflectArgs for regular native calls.
//
// Takes vm (*VM) which provides context for closure coercion.
// Takes closure (reflect.Value) which is the native function to inspect.
// Takes arguments ([]reflect.Value) which are the arguments to coerce in place.
func coerceNativeGoroutineArgs(vm *VM, closure reflect.Value, arguments []reflect.Value) {
	functionType := closure.Type()
	for i := range arguments {
		if i < functionType.NumIn() {
			arguments[i] = coerceReflectArgument(vm, arguments[i], functionType.In(i), argumentTypeContext{staticTypeName: "", staticTypeString: "", skipInterfaceAdapter: false, keepClosures: false})
		}
	}
}

// launchNativeGoroutine spawns a goroutine that calls a native function via reflect. A
// panic is recorded on the parent VM's goroutinePanic slot.
//
// Takes parentVM (*VM) which owns the goroutinePanic slot and context.
// Takes limits (VMLimits) which carries the goroutine limit and tracker.
// Takes reflectedFunction (reflect.Value) which is the native function to call.
// Takes arguments ([]reflect.Value) which holds the arguments to pass.
func launchNativeGoroutine(parentVM *VM, limits VMLimits, reflectedFunction reflect.Value, arguments []reflect.Value) {
	go func() {
		if limits.Tracker != nil {
			limits.Tracker.MarkGoroutineStarted()
			defer limits.Tracker.ReleaseGoroutine()
		}
		defer func() {
			if recovered := recover(); recovered != nil {
				recordGoroutinePanicValue(parentVM, recovered)
			}
		}()
		reflectedFunction.Call(arguments)
	}()
}
