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
	"reflect"
	"unicode/utf8"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
	"pipit.sh/pipit/internal/symtab/typemodel"
)

// appendSelectWakeArms adds context-cancellation and goroutine-panic wake arms unless the
// select has a default, because a default-bearing select never blocks.
//
// Takes numCases (int) which is the count of program-declared cases.
// Takes hasDefault (bool) which is true when one declared case is a default.
//
// Returns the case slice and the cancel/panic arm indices.
func (vm *VM) appendSelectWakeArms(numCases int, hasDefault bool) (cases []reflect.SelectCase, cancelIndex, panicIndex int) {
	if hasDefault {
		return vm.selectCasesBuffer[:numCases], -1, -1
	}
	count, cancelIndex, panicIndex := appendWakeCases(vm.selectCasesBuffer, numCases, vm.ctx.Done(), vm.Globals.goroutinePanicWakeChan())
	return vm.selectCasesBuffer[:count], cancelIndex, panicIndex
}

// handleStringToBytes converts a string register value to a byte slice and stores the
// result in the general register bank.
//
// Takes registers (*Registers) which holds the source string and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringToBytes(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.General[instruction.A] = reflect.ValueOf([]byte(registers.Strings[instruction.B]))
	return opContinue
}

// handleStringIndex retrieves a single byte from a string at the given index and stores
// it as a uint64 in the destination register.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the string, index and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringIndex(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	s := registers.Strings[instruction.B]
	index := int(registers.Ints[instruction.C])
	if index < 0 || index >= len(s) {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: index out of range [%d] with length %d", index, len(s)))
	}
	registers.Uints[instruction.A] = uint64(s[index])
	return opContinue
}

// raiseIndexOutOfRange raises Go's index-out-of-range panic as an interpreted panic so
// deferred recover can catch it.
//
// Takes vm (*VM) which carries the panic machinery.
// Takes index (int) which is the offending slice index.
// Takes length (int) which is the slice length.
//
// Returns OpResult which propagates the raised panic to the dispatch loop.
func raiseIndexOutOfRange(vm *VM, index, length int) OpResult {
	if index < 0 {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: index out of range [%d]", index))
	}
	return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: index out of range [%d] with length %d", index, length))
}

// handleStringIndexToInt retrieves a single byte from a string and stores it directly as
// an int64, fusing isa.OpStringIndex + isa.SubOpUintToInt into one operation.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the string, index and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleStringIndexToInt(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	s := registers.Strings[instruction.B]
	index := int(registers.Ints[instruction.C])
	if index < 0 || index >= len(s) {
		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("runtime error: index out of range [%d] with length %d", index, len(s)))
	}
	registers.Ints[instruction.A] = int64(s[index])
	return opContinue
}

// handleRuneToString converts an int64 register value to its UTF-8 string representation
// and stores the result in the string register bank.
//
// Takes vm (*VM) which provides the arena for string allocation.
// Takes registers (*Registers) which holds the rune and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleRuneToString(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	registers.Strings[instruction.A] = arenaRuneToString(vm.Arena, safeconv.Int64ToInt32(registers.Ints[instruction.B]))
	return opContinue
}

// handleSliceString performs a substring slice operation on a string register value using
// optional low and high bounds from int registers.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which provides the bounds extension word.
// Takes registers (*Registers) which holds the string, bounds, destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleSliceString(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	s := registers.Strings[instruction.B]
	low := 0
	high := len(s)
	if instruction.C&1 != 0 {
		low = int(registers.Ints[extensionWord.A])
	}
	if instruction.C&2 != 0 {
		high = int(registers.Ints[extensionWord.B])
	}
	if low < 0 || high < low || high > len(s) {
		return raiseNativePanicAsInterpreted(vm, sliceBoundsRuntimeError(low, high, len(s), false))
	}
	registers.Strings[instruction.A] = s[low:high]
	return opContinue
}

// sliceBoundsRuntimeError formats a runtime error matching Go's runtime for
// `s[low:high]`-style operations on a string or slice. Mirrors the message shape Go emits
// so deferred recover()s see a value whose Sprintf("%v") output is byte-for-byte equal to
// `go run` panic output.
//
// Takes low (int) which is the requested low bound.
// Takes high (int) which is the requested high bound.
// Takes size (int) which is the underlying length or capacity.
// Takes useCapacity (bool) which selects between "capacity" and "length" wording.
//
// Returns a *runtimePanicError suitable for raiseNativePanicAsInterpreted.
func sliceBoundsRuntimeError(low, high, size int, useCapacity bool) *runtimePanicError {
	switch {
	case low < 0:
		return newRuntimePanicError("runtime error: slice bounds out of range [%d:]", low)
	case high < low:
		return newRuntimePanicError("runtime error: slice bounds out of range [%d:%d]", low, high)
	default:
		if useCapacity {
			return newRuntimePanicError("runtime error: slice bounds out of range [:%d] with capacity %d", high, size)
		}
		return newRuntimePanicError("runtime error: slice bounds out of range [:%d] with length %d", high, size)
	}
}

// handleRangeInit creates a range iterator for the collection in the source register and
// stores it in the destination general register.
//
// Takes registers (*Registers) which holds the collection and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleRangeInit(_ *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	collection := registers.General[instruction.C]

	if collection.Kind() == reflect.Pointer && collection.IsValid() && !collection.IsNil() {
		if elem := collection.Elem(); elem.Kind() == reflect.Array {
			collection = elem
		}
	}
	if collection.IsValid() && collection.Kind() == reflect.Array {
		collection = copyReflectValue(collection)
	}
	iterator := &rangeIterator{
		collection:   collection,
		mapIterator:  nil,
		valueScratch: reflect.Value{},
		keyScratch:   reflect.Value{},
		StringSource: "",
		boolSlice:    nil,
		floatSlice:   nil,
		stringSlice:  nil,
		intSlice:     nil,
		index:        0,
		isMap:        false,
		isChannel:    false,
		IsString:     false,
	}
	switch collection.Kind() {
	case reflect.Map:
		iterator.isMap = true
		iterator.mapIterator = collection.MapRange()
		mapType := collection.Type()
		iterator.keyScratch = reflect.New(mapType.Key()).Elem()
		iterator.valueScratch = reflect.New(mapType.Elem()).Elem()
	case reflect.Chan:
		iterator.isChannel = true
	case reflect.Slice, reflect.Array:
		if collection.CanInterface() {
			assignRangeSliceFastPath(iterator, collection)
		}
	case reflect.String:
		iterator.IsString = true
		iterator.StringSource = collection.String()
	default:
	}
	registers.General[instruction.B] = reflect.ValueOf(iterator)
	return opContinue
}

// assignRangeSliceFastPath attempts to extract a concrete typed slice from a
// reflect.Value and assign it to the corresponding fast-path field on iterator.
//
// Takes iterator (*rangeIterator) which receives the typed slice.
// Takes collection (reflect.Value) which holds the underlying slice.
func assignRangeSliceFastPath(iterator *rangeIterator, collection reflect.Value) {
	if s, ok := reflect.TypeAssert[[]int](collection); ok {
		iterator.intSlice = s
		return
	}
	if s, ok := reflect.TypeAssert[[]string](collection); ok {
		iterator.stringSlice = s
		return
	}
	if s, ok := reflect.TypeAssert[[]float64](collection); ok {
		iterator.floatSlice = s
		return
	}
	if s, ok := reflect.TypeAssert[[]bool](collection); ok {
		iterator.boolSlice = s
	}
}

// rangeNextChannel advances a channel range iterator by receiving the next value.
//
// Takes vm (*VM) which provides the range value writer.
// Takes registers (*Registers) which holds the destination banks.
// Takes iterator (*rangeIterator) which is the channel iterator to advance.
// Takes context (rangeNextContext) which describes the key/value destinations.
//
// Returns OpResult which reports whether the range loop should continue or exit.
func rangeNextChannel(vm *VM, registers *Registers, iterator *rangeIterator, context rangeNextContext) OpResult {
	done := vm.ctx.Done()
	panicWake := vm.Globals.goroutinePanicWakeChan()

	var value reflect.Value
	var ok bool
	if done == nil && panicWake == nil {
		released := vm.releaseAroundBlock()
		value, ok = iterator.collection.Recv()
		vm.reacquireAfterBlock(released)
	} else {
		released := vm.releaseAroundBlock()
		var wake blockWake
		value, ok, wake = selectChannelReceive(iterator.collection, done, panicWake)
		vm.reacquireAfterBlock(released)
		if wake != blockWakeValue {
			return vm.surfaceBlockingWake(wake)
		}
	}

	if !ok {
		registers.Ints[context.DoneDestination] = 0
		return opContinue
	}
	registers.Ints[context.DoneDestination] = 1
	if context.HasKey {
		vm.writeRangeValue(registers, value, context.KeyInstruction.B, isa.RegisterKind(context.KeyInstruction.C))
	}
	return opContinue
}

// rangeNextMap advances a map range iterator to the next key/value pair.
//
// Takes vm (*VM) which provides the range value writer.
// Takes registers (*Registers) which holds the destination banks.
// Takes iterator (*rangeIterator) which is the map iterator to advance.
// Takes context (rangeNextContext) which describes the key/value destinations.
func rangeNextMap(vm *VM, registers *Registers, iterator *rangeIterator, context rangeNextContext) {
	if !iterator.mapIterator.Next() {
		registers.Ints[context.DoneDestination] = 0
		return
	}
	registers.Ints[context.DoneDestination] = 1
	if context.HasKey {
		if iterator.keyScratch.IsValid() {
			iterator.keyScratch.SetIterKey(iterator.mapIterator)
			vm.writeRangeValue(registers, iterator.keyScratch, context.KeyInstruction.B, isa.RegisterKind(context.KeyInstruction.C))
		} else {
			vm.writeRangeValue(registers, iterator.mapIterator.Key(), context.KeyInstruction.B, isa.RegisterKind(context.KeyInstruction.C))
		}
	}
	if context.HasValue {
		if iterator.valueScratch.IsValid() {
			iterator.valueScratch.SetIterValue(iterator.mapIterator)
			vm.writeRangeValue(registers, iterator.valueScratch, context.ValInstruction.B, isa.RegisterKind(context.ValInstruction.C))
		} else {
			vm.writeRangeValue(registers, iterator.mapIterator.Value(), context.ValInstruction.B, isa.RegisterKind(context.ValInstruction.C))
		}
	}
}

// rangeNextSlice advances a slice/array/string range iterator by index.
//
// Takes vm (*VM) which provides the range value writer.
// Takes registers (*Registers) which holds the destination banks.
// Takes iterator (*rangeIterator) which is the slice iterator to advance.
// Takes context (rangeNextContext) which describes the key/value destinations.
func rangeNextSlice(vm *VM, registers *Registers, iterator *rangeIterator, context rangeNextContext) {
	if iterator.IsString {
		rangeNextString(registers, iterator, context)
		return
	}
	if iterator.index >= iterator.collection.Len() {
		registers.Ints[context.DoneDestination] = 0
		return
	}
	registers.Ints[context.DoneDestination] = 1
	if context.HasKey && isa.RegisterKind(context.KeyInstruction.C) == isa.RegisterInt {
		registers.Ints[context.KeyInstruction.B] = int64(iterator.index)
	}
	if context.HasValue {
		rangeSliceValue(vm, registers, iterator, context.ValInstruction.B, isa.RegisterKind(context.ValInstruction.C))
	}
	iterator.index++
}

// rangeNextString advances a string range iterator one rune.
//
// The key register receives the byte index of the rune (Go's range-over-string semantics)
// and the value register receives the decoded rune. Invalid UTF-8 sequences yield
// utf8.RuneError and consume one byte, matching Go's runtime behaviour.
//
// Takes registers (*Registers) which holds the destination banks.
// Takes iterator (*rangeIterator) which is the string iterator.
// Takes context (rangeNextContext) which describes the destinations.
func rangeNextString(registers *Registers, iterator *rangeIterator, context rangeNextContext) {
	if iterator.index >= len(iterator.StringSource) {
		registers.Ints[context.DoneDestination] = 0
		return
	}
	registers.Ints[context.DoneDestination] = 1
	runeValue, runeWidth := utf8.DecodeRuneInString(iterator.StringSource[iterator.index:])
	if context.HasKey && isa.RegisterKind(context.KeyInstruction.C) == isa.RegisterInt {
		registers.Ints[context.KeyInstruction.B] = int64(iterator.index)
	}
	if context.HasValue && isa.RegisterKind(context.ValInstruction.C) == isa.RegisterInt {
		registers.Ints[context.ValInstruction.B] = int64(runeValue)
	}
	iterator.index += runeWidth
}

// rangeSliceValue writes the element at the current index to the destination register,
// using type-asserted fast paths where available.
//
// Takes vm (*VM) which provides the range value writer.
// Takes registers (*Registers) which holds the destination banks.
// Takes iterator (*rangeIterator) which is the slice iterator.
// Takes destination (uint8) which is the destination register index.
// Takes kind (isa.RegisterKind) which selects the typed bank for the value.
func rangeSliceValue(vm *VM, registers *Registers, iterator *rangeIterator, destination uint8, kind isa.RegisterKind) {
	switch {
	case iterator.intSlice != nil && kind == isa.RegisterInt:
		registers.Ints[destination] = int64(iterator.intSlice[iterator.index])
	case iterator.stringSlice != nil && kind == isa.RegisterString:
		registers.Strings[destination] = iterator.stringSlice[iterator.index]
	case iterator.floatSlice != nil && kind == isa.RegisterFloat:
		registers.Floats[destination] = iterator.floatSlice[iterator.index]
	case iterator.boolSlice != nil && kind == isa.RegisterBool:
		registers.Bools[destination] = iterator.boolSlice[iterator.index]
	default:
		vm.writeRangeValue(registers, iterator.collection.Index(iterator.index), destination, kind)
	}
}

// handleRangeNext advances a range iterator to the next element, dispatching to the
// appropriate helper based on the collection type.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which provides the context extension words.
// Takes registers (*Registers) which holds the iterator and destinations.
// Takes instruction (instruction) which encodes the iterator register.
//
// Returns OpResult indicating the next execution step.
func handleRangeNext(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	ext1 := readExtensionWord(frame)
	frame.ProgramCounter++
	ext2 := readExtensionWord(frame)
	frame.ProgramCounter++
	iteratorValue := registers.General[instruction.B]
	iterator, ok := reflect.TypeAssert[*rangeIterator](iteratorValue)
	if !ok {
		vm.evalError = errors.New("range iterator is not valid")
		return opPanicError
	}
	context := rangeNextContext{
		DoneDestination: instruction.C,
		HasKey:          ext1.A&1 != 0,
		HasValue:        ext1.A&2 != 0,
		KeyInstruction:  ext1,
		ValInstruction:  ext2,
	}
	switch {
	case iterator.isChannel:
		return rangeNextChannel(vm, registers, iterator, context)
	case iterator.isMap:
		rangeNextMap(vm, registers, iterator, context)
	default:
		rangeNextSlice(vm, registers, iterator, context)
	}
	return opContinue
}

// handleTypeAssert performs a type assertion on a general register value, storing the
// asserted value and a boolean success flag.
//
// Interface values are unwrapped before comparison to match Go's runtime behaviour. This
// is needed because operations like MapIndex on map[string]any return reflect.Values with
// Kind==Interface, but Go type assertions inspect the underlying concrete type.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which provides the type table index extension.
// Takes registers (*Registers) which holds the source and destination.
// Takes instruction (instruction) which encodes the operand indices.
//
// Returns OpResult indicating the next execution step.
func handleTypeAssert(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	typeIndex := uint16(extensionWord.A) | uint16(extensionWord.B)<<isa.WideBitShift
	if int(typeIndex) >= len(frame.Function.TypeTable) {
		vMBoundsError(vm, frame, boundsTableTypeTable, int(typeIndex), len(frame.Function.TypeTable))
		return opPanicError
	}
	reflectType := frame.Function.TypeTable[typeIndex]
	source := registers.General[instruction.B]
	if source.IsValid() && source.Kind() == reflect.Interface && !source.IsNil() {
		source = source.Elem()
	}
	original := source
	source, matched := matchTypeAssertion(source, reflectType)
	if matched {
		matched = typeAssertSatisfiesInterfaceMethods(vm, frame, source, typeIndex)
	} else if reflectType != nil && reflectType.Kind() == reflect.Interface && scriptTypeSatisfiesInterface(vm, frame, original, typeIndex) {
		source, matched = original, true
	}
	if matched {
		registers.General[instruction.A] = valueCopyForBoundaryArenaWithVM(vm.Arena, vm, source)
		registers.Ints[instruction.C] = 1
		return opContinue
	}
	return handleTypeAssertMissingMatch(vm, registers, instruction, extensionWord, source, reflectType)
}

// scriptTypeSatisfiesInterface reports whether source, a value reflect cannot match to
// the interface at typeIndex, satisfies the interface's recorded method requirements
// through the program's compiled method tables. An interface without recorded
// requirements is not satisfied this way.
//
// Takes vm (*VM) which owns the method tables.
// Takes frame (*CallFrame) whose function's type table carries the requirements.
// Takes source (reflect.Value) which is the asserted value.
// Takes typeIndex (uint16) which is the interface's type-table index.
//
// Returns bool which is true when every required method is available.
func scriptTypeSatisfiesInterface(vm *VM, frame *CallFrame, source reflect.Value, typeIndex uint16) bool {
	if !source.IsValid() || int(typeIndex) >= len(frame.Function.TypeTableInterfaceMethods) {
		return false
	}
	required := frame.Function.TypeTableInterfaceMethods[typeIndex]
	if len(required) == 0 {
		return false
	}
	return sourceImplementsAllMethods(vm, source, required)
}

// typeAssertSatisfiesInterfaceMethods verifies that source implements every method
// recorded for the case clause's interface type. Needed because pipit collapses all
// interfaces to `any`, so the membership check restores the original discrimination.
//
// Takes vm (*VM) which owns the method table.
// Takes frame (*CallFrame) which carries the function metadata.
// Takes source (reflect.Value) which is the value being asserted.
// Takes typeIndex (uint16) which indexes the case's interface type.
//
// Returns bool which is true when source implements every recorded method.
func typeAssertSatisfiesInterfaceMethods(vm *VM, frame *CallFrame, source reflect.Value, typeIndex uint16) bool {
	if int(typeIndex) >= len(frame.Function.TypeTableInterfaceMethods) {
		return true
	}
	required := frame.Function.TypeTableInterfaceMethods[typeIndex]
	if len(required) == 0 {
		return true
	}
	return sourceImplementsAllMethods(vm, source, required)
}

// handleTypeAssertMissingMatch produces the failure-branch result.
//
// Behaviour depends on the extensionWord mode: panic for `.(T)`, zero-write for `.(T) ->
// (T, bool)`, and continue without writing for a type-switch arm.
//
// Takes vm (*VM) which records the panic value when needed.
// Takes registers (*Registers) which receives the failure flag and zero value.
// Takes instruction (instruction) which encodes destination indices.
// Takes extensionWord (instruction) which carries the mode.
// Takes source (reflect.Value) which is the value being asserted.
// Takes reflectType (reflect.Type) which is the target type.
//
// Returns OpResult which is opPanicError for `.(T)` failure and opContinue otherwise.
func handleTypeAssertMissingMatch(vm *VM, registers *Registers, instruction, extensionWord isa.Instruction, source reflect.Value, reflectType reflect.Type) OpResult {
	if extensionWord.C == typeAssertModePanic {
		srcType := "nil"
		if source.IsValid() {
			srcType = typemodel.NamedScalarDisplayType(source.Type())
		}

		return raiseNativePanicAsInterpreted(vm, newRuntimePanicError("interface conversion: interface {} is %s, not %s", srcType, typemodel.NamedScalarDisplayType(reflectType)))
	}
	registers.Ints[instruction.C] = 0
	if extensionWord.C == TypeAssertModeTypeSwitch {
		return opContinue
	}
	if reflectType != nil {
		registers.General[instruction.A] = reflect.Zero(reflectType)
	} else {
		registers.General[instruction.A] = reflect.Value{}
	}
	return opContinue
}

// matchTypeAssertion checks whether source matches reflectType and returns the (possibly
// converted) value alongside the match result.
//
// Takes source (reflect.Value) which is the value to test.
// Takes reflectType (reflect.Type) which is the target type to match against.
//
// Returns reflect.Value which is the (possibly converted) source value.
// Returns bool which indicates whether the assertion matched.
func matchTypeAssertion(source reflect.Value, reflectType reflect.Type) (reflect.Value, bool) {
	if reflectType == nil {
		return source, !source.IsValid()
	}
	if !source.IsValid() {
		return source, false
	}

	source = unwrapAdapterUnderlying(source)
	srcType := source.Type()
	if srcType == runtimeClosureReflectType {
		return source, reflectType.Kind() == reflect.Func && closureSignatureType(source) == reflectType
	}
	switch {
	case srcType == reflectType,
		reflectType.Kind() == reflect.Interface && srcType.Implements(reflectType),
		srcType.AssignableTo(reflectType):
		return source, true
	}
	return source, false
}

// closureSignatureType returns the static func type recorded for a boxed closure, or nil
// when the value is not a closure or its function carries no signature.
//
// Takes value (reflect.Value) which holds a *RuntimeClosure.
//
// Returns reflect.Type which is the signature or nil.
func closureSignatureType(value reflect.Value) reflect.Type {
	closure := closureFromValue(value)
	if closure == nil || closure.Function == nil {
		return nil
	}
	return closure.Function.SignatureReflectType
}

// closureFromValue returns the *RuntimeClosure a value holds, or nil when the value is
// not a closure. It reads the pointer directly, so a closure reached through an
// unexported struct field (which reflect refuses to Interface) still resolves.
//
// Takes value (reflect.Value) which may hold a closure.
//
// Returns *RuntimeClosure which is the closure or nil.
func closureFromValue(value reflect.Value) *RuntimeClosure {
	if !value.IsValid() || value.Type() != runtimeClosureReflectType || value.IsNil() {
		return nil
	}
	return (*RuntimeClosure)(value.UnsafePointer())
}

// nativeMethodArityMatches reports whether a Go method has the parameter and result
// counts an interface requires; negative counts are unchecked. The receiver is the method
// type's first input and is not counted.
//
// Takes method (reflect.Method) which is the method found on the dynamic type.
// Takes params (int) which is the required parameter count, or -1.
// Takes results (int) which is the required result count, or -1.
//
// Returns true when the shapes agree.
func nativeMethodArityMatches(method reflect.Method, params, results int) bool {
	if params < 0 || results < 0 || method.Type == nil {
		return true
	}
	return method.Type.NumIn()-1 == params && method.Type.NumOut() == results
}

// compiledMethodAvailable consults the program's method table for tableName: a
// pointer-receiver method is not in a value type's method set, a recorded signature shape
// must match the requirement's shape, and otherwise the arities must agree.
//
// Takes vm (*VM) which owns the method table.
// Takes srcType (reflect.Type) which is the dynamic type checked.
// Takes tableName (string) which is the `Type.Method` key.
// Takes requirement (program.InterfaceMethodRequirement) which is the interface's method.
//
// Returns whether the method satisfies the requirement, and whether the table has it.
func compiledMethodAvailable(vm *VM, srcType reflect.Type, tableName string, requirement program.InterfaceMethodRequirement) (available, found bool) {
	index, ok := vm.rootFunction.MethodTable()[tableName]
	if !ok {
		return false, false
	}
	if int(index) < len(vm.functions) && vm.functions[index].IsPointerReceiver && srcType.Kind() != reflect.Pointer {
		return false, true
	}
	if shape, known := vm.rootFunction.MethodSignatures[tableName]; known && requirement.Shape != "" {
		return program.SubstituteShapeTypeArgs(shape, sentinelTypeArgs(srcType)) == requirement.Shape, true
	}
	return compiledMethodArityMatches(vm, index, requirement.Params, requirement.Results), true
}

// compiledMethodArityMatches reports whether a compiled method has the parameter and
// result counts an interface requires; negative counts are unchecked. The receiver
// occupies the first parameter slot and is not counted.
//
// Takes vm (*VM) which owns the function table.
// Takes index (uint16) which is the method's function index.
// Takes params (int) which is the required parameter count, or -1.
// Takes results (int) which is the required result count, or -1.
//
// Returns true when the shapes agree.
func compiledMethodArityMatches(vm *VM, index uint16, params, results int) bool {
	if params < 0 || results < 0 || int(index) >= len(vm.functions) {
		return true
	}
	callee := vm.functions[index]
	parameterCount := len(callee.ParameterKinds)
	if callee.HasReceiver {
		parameterCount--
	}
	return parameterCount == params && len(callee.ResultKinds) == results
}

// sourceImplementsAllMethods reports whether source's reflect.Type exposes every method
// named in required. Used by handleTypeAssert to enforce a non-empty interface's method
// set when the typeTable entry has been collapsed to reflect.TypeFor[any]() - see the
// typeTableInterfaceMethods sidecar on CompiledFunction.
//
// Pointer-receiver methods on `*T` count for an addressable T, so the check first widens
// to `*T` when the source is a non-pointer value before walking the method names. Method
// names registered in pipit's externalMethods registry (cross-package methods on synth
// structs) are also consulted because reflect.MethodByName on the synth type itself
// returns invalid for those.
//
// Takes vm (*VM) which provides the cross-package method registry.
// Takes source (reflect.Value) which is the value under test.
// Takes required ([]string) which is the method-name set the original *types.Interface
// declared (sorted, deduplicated).
//
// Returns true when every required method is observable on source via
// reflect.Type.MethodByName or pipit's external-method registry.
func sourceImplementsAllMethods(vm *VM, source reflect.Value, required []string) bool {
	if !source.IsValid() {
		return false
	}
	srcType := source.Type()
	pointerType := srcType
	if pointerType.Kind() != reflect.Pointer {
		pointerType = reflect.PointerTo(srcType)
	}
	srcName := bareSentinelName(srcType)
	if srcName == "" {
		if info, ok := typemodel.LookupNamedScalarPoolInfo(srcType); ok {
			srcName = info.BareName
		}
	}
	for _, encoded := range required {
		requirement := program.DecodeInterfaceMethodRequirement(encoded)
		if methodAvailableOnType(vm, srcType, pointerType, srcName, requirement) {
			continue
		}

		if _, _, promoted := resolvePromotedMethod(vm, source, requirement.Name); !promoted {
			return false
		}
	}
	return true
}

// methodAvailableOnType returns true when a method with methodName is reachable from
// either srcType, its pointer counterpart, or pipit's cross-package external method
// registry keyed by the bare sentinel name.
//
// Takes vm (*VM) which provides the cross-package method registry.
// Takes srcType (reflect.Type) which is the source value's type.
// Takes pointerType (reflect.Type) which is reflect.PointerTo(srcType) when srcType is
// not already a pointer.
// Takes srcName (string) which is the sentinel-extracted bare name for pipit-synth
// structs; empty for non-synth types.
// Takes requirement (InterfaceMethodRequirement) which describes the method being
// checked.
//
// Returns true when the method is observable on any of the three sources.
func methodAvailableOnType(vm *VM, srcType, pointerType reflect.Type, srcName string, requirement program.InterfaceMethodRequirement) bool {
	methodName := requirement.Name
	if method, ok := srcType.MethodByName(methodName); ok {
		return nativeMethodArityMatches(method, requirement.Params, requirement.Results)
	}
	if pointerType != srcType {
		if method, ok := pointerType.MethodByName(methodName); ok {
			return nativeMethodArityMatches(method, requirement.Params, requirement.Results)
		}
	}
	if srcName == "" {
		return false
	}

	if vm != nil && vm.rootFunction != nil && vm.rootFunction.MethodTable() != nil {
		if available, found := compiledMethodAvailable(vm, srcType, srcName+"."+methodName, requirement); found {
			return available
		}
	}
	if vm == nil || vm.Globals == nil {
		return false
	}
	_, ok := vm.Globals.lookupExternalMethod(srcName + "." + methodName)
	return ok
}

// readRegisterConvert reads a value from the typed register identified by kind and source
// index, converting it to targetType.
//
// Named banks are read directly; the default reads the general bank and converts, which
// is the path any other kind takes.
//
// Takes registers (*Registers) which holds the source values.
// Takes source (uint8) which is the register index to read from.
// Takes kind (isa.RegisterKind) which selects the typed bank to read.
// Takes targetType (reflect.Type) which is the type to convert to.
//
// Returns reflect.Value which is the source converted to targetType.
func readRegisterConvert(registers *Registers, source uint8, kind isa.RegisterKind, targetType reflect.Type) reflect.Value {
	switch kind {
	case isa.RegisterInt:
		return reflect.ValueOf(registers.Ints[source]).Convert(targetType)
	case isa.RegisterFloat:
		return reflect.ValueOf(registers.Floats[source]).Convert(targetType)
	case isa.RegisterString:
		return reflect.ValueOf(registers.Strings[source]).Convert(targetType)
	case isa.RegisterBool:
		return reflect.ValueOf(registers.Bools[source]).Convert(targetType)
	case isa.RegisterUint:
		return reflect.ValueOf(registers.Uints[source]).Convert(targetType)
	case isa.RegisterComplex:
		return reflect.ValueOf(registers.Complex[source]).Convert(targetType)
	case isa.RegisterSliceInt, isa.RegisterSliceFloat, isa.RegisterSliceString, isa.RegisterSliceBool,
		isa.RegisterSliceUint, isa.RegisterSliceByte:

		value, _ := sliceBankToReflect(registers, kind, source)
		if value.IsValid() && value.Type() != targetType && value.Type().ConvertibleTo(targetType) {
			return value.Convert(targetType)
		}
		return value
	default:
		value := registers.General[source]
		if value.IsValid() && value.Type() != targetType && value.Type().ConvertibleTo(targetType) {
			return value.Convert(targetType)
		}
		return value
	}
}

// handleAllocIndirect allocates a new pointer of a type from the type table, initialises
// it with a converted register value, and stores the pointer.
//
// Takes vm (*VM) which is the virtual machine.
// Takes frame (*CallFrame) which provides the type table index extension.
// Takes registers (*Registers) which holds the source and destination.
// Takes instruction (instruction) which encodes the source register and kind.
//
// Returns OpResult indicating the next execution step.
func handleAllocIndirect(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	allocPC := frame.ProgramCounter - 1
	arenaSafe := frame.Function.ArenaSafeAllocPCs[allocPC]
	extensionWord := readExtensionWord(frame)
	frame.ProgramCounter++
	typeIndex := uint16(extensionWord.A) | uint16(extensionWord.B)<<isa.WideBitShift
	heapCell := extensionWord.C == isa.AllocIndirectHeapCell
	if int(typeIndex) >= len(frame.Function.TypeTable) {
		vMBoundsError(vm, frame, boundsTableTypeTable, int(typeIndex), len(frame.Function.TypeTable))
		return opPanicError
	}
	reflectType := frame.Function.TypeTable[typeIndex]
	source := readRegisterConvert(registers, instruction.B, isa.RegisterKind(instruction.C), reflectType)
	if !heapCell && source.IsValid() && source.CanAddr() && source.Type() == reflectType {
		registers.General[instruction.A] = source.Addr()
		return opContinue
	}
	pointer := allocIndirectPointee(vm, reflectType, arenaSafe, heapCell)
	if source.IsValid() {
		source = coerceValue(vm, source, reflectType)
		if source.IsValid() && source.Type().AssignableTo(reflectType) {
			pointer.Elem().Set(materialiseAllocIndirectSource(vm, source, arenaSafe, heapCell))
		}
	}
	registers.General[instruction.A] = pointer
	return opContinue
}

// materialiseAllocIndirectSource detaches source from the arena unless the escape pass
// proved the pointer cannot outlive the frame. heapCell forces materialisation because
// that form deliberately outlives the frame.
//
// Takes vm (*VM) which owns the arena.
// Takes source (reflect.Value) which is the value being written through the pointer.
// Takes arenaSafe (bool) which is the escape pass's verdict for this PC.
// Takes heapCell (bool) which marks the deliberately heap-allocated form.
//
// Returns source unchanged when arena-safe and not heapCell, else the arena-detached
// copy.
func materialiseAllocIndirectSource(vm *VM, source reflect.Value, arenaSafe, heapCell bool) reflect.Value {
	if arenaSafe && !heapCell {
		return source
	}
	return MaterialiseArenaValue(vm.Arena, source)
}

// allocIndirectPointee returns a *T pointing at zero-initialised storage, routing slice
// pointees through the arena slice-header slab, pointer-free pointees through the byte
// slab, struct/array pointees through boundary-snapshot chunks, and everything else
// through reflect.New.
//
// Takes vm (*VM) which provides the arena slabs and boundary chunks.
// Takes reflectType (reflect.Type) which is the pointee element type.
// Takes arenaSafe (bool) which gates the byte-slab route.
// Takes heapCell (bool) which forces a reflect.New cell that outlives every frame.
//
// Returns reflect.Value of type *reflectType pointing at zero-init storage.
func allocIndirectPointee(vm *VM, reflectType reflect.Type, arenaSafe bool, heapCell bool) reflect.Value {
	if heapCell {
		return reflect.New(reflectType)
	}
	arena := vm.Arena
	if arena != nil && reflectType.Kind() == reflect.Slice {
		slot := arena.allocSliceHeader()
		return reflect.NewAt(reflectType, unsafe.Pointer(slot))
	}
	align := safeconv.IntToUintptr(reflectType.Align())
	if arenaSafe && arena != nil && align <= arenaMaxAlignment && typeIsPointerFree(reflectType) {
		elemSize := reflectType.Size()
		if align == 0 {
			align = 1
		}
		dataPtr := arena.AllocBytes(elemSize, align)
		if elemSize > 0 {
			clear(unsafe.Slice((*byte)(dataPtr), elemSize))
		}
		return reflect.NewAt(reflectType, dataPtr)
	}
	if kind := reflectType.Kind(); kind == reflect.Struct || kind == reflect.Array {
		return vm.acquireBoundarySnapshot(reflectType).Addr()
	}
	return reflect.New(reflectType)
}

// buildSelectSendValue reads the send value from the appropriate register and converts it
// to the channel's element type.
//
// Named banks build the send value directly; the default reads it from the general bank,
// which is where any other kind is held.
//
// Takes vm (*VM) which provides the arena for string materialisation.
// Takes registers (*Registers) which holds the source values.
// Takes ext2 (instruction) which encodes the source register and kind.
// Takes channelElementType (reflect.Type) which is the channel element type.
//
// Returns reflect.Value which is ready for sending on the channel.
func buildSelectSendValue(vm *VM, registers *Registers, ext2 isa.Instruction, channelElementType reflect.Type) reflect.Value {
	switch isa.RegisterKind(ext2.B) {
	case isa.RegisterInt:
		return reflect.ValueOf(registers.Ints[ext2.A]).Convert(channelElementType)
	case isa.RegisterFloat:
		return reflect.ValueOf(registers.Floats[ext2.A]).Convert(channelElementType)
	case isa.RegisterString:
		return reflect.ValueOf(materialiseStringUnconditional(vm.Arena, registers.Strings[ext2.A])).Convert(channelElementType)
	case isa.RegisterBool:
		return reflect.ValueOf(registers.Bools[ext2.A]).Convert(channelElementType)
	case isa.RegisterUint:
		return reflect.ValueOf(registers.Uints[ext2.A]).Convert(channelElementType)
	case isa.RegisterComplex:
		return reflect.ValueOf(registers.Complex[ext2.A]).Convert(channelElementType)
	case isa.RegisterSliceInt:
		return reflect.ValueOf(registers.SlicesInt[ext2.A]).Convert(channelElementType)
	case isa.RegisterSliceFloat:
		return reflect.ValueOf(registers.slicesFloat[ext2.A]).Convert(channelElementType)
	case isa.RegisterSliceString:
		return reflect.ValueOf(registers.slicesString[ext2.A]).Convert(channelElementType)
	case isa.RegisterSliceBool:
		return reflect.ValueOf(registers.slicesBool[ext2.A]).Convert(channelElementType)
	case isa.RegisterSliceUint:
		return reflect.ValueOf(registers.slicesUint[ext2.A]).Convert(channelElementType)
	case isa.RegisterSliceByte:
		return reflect.ValueOf(registers.slicesByte[ext2.A]).Convert(channelElementType)
	default:
		value := registers.General[ext2.A]
		if !value.IsValid() {
			return reflect.Zero(channelElementType)
		}

		return coerceValue(vm, value, channelElementType)
	}
}

// writeRegisterValue stores a reflect.Value into the typed register identified by kind
// and dest index.
//
// Takes registers (*Registers) which is the destination register set.
// Takes dest (uint8) which is the register index to write to.
// Takes kind (isa.RegisterKind) which selects the typed bank to write to.
// Takes value (reflect.Value) which is the value to store.
func writeRegisterValue(registers *Registers, dest uint8, kind isa.RegisterKind, value reflect.Value) {
	switch kind {
	case isa.RegisterInt:
		registers.Ints[dest] = value.Int()
	case isa.RegisterFloat:
		registers.Floats[dest] = value.Float()
	case isa.RegisterString:
		registers.Strings[dest] = value.String()
	case isa.RegisterGeneral:
		registers.General[dest] = ValueCopyForBoundary(value)
	case isa.RegisterBool:
		registers.Bools[dest] = value.Bool()
	case isa.RegisterUint:
		registers.Uints[dest] = value.Uint()
	case isa.RegisterComplex:
		registers.Complex[dest] = value.Complex()
	case isa.RegisterSliceInt:
		registers.SlicesInt[dest] = sliceFromReflectAs[int64](value)
	case isa.RegisterSliceFloat:
		registers.slicesFloat[dest] = sliceFromReflectAs[float64](value)
	case isa.RegisterSliceString:
		registers.slicesString[dest] = sliceFromReflectAs[string](value)
	case isa.RegisterSliceBool:
		registers.slicesBool[dest] = sliceFromReflectAs[bool](value)
	case isa.RegisterSliceUint:
		registers.slicesUint[dest] = sliceFromReflectAs[uint64](value)
	case isa.RegisterSliceByte:
		registers.slicesByte[dest] = sliceFromReflectAs[byte](value)
	default:
	}
}

// sliceFromReflectAs extracts a Go-typed slice from a reflect.Value, falling back to
// per-element conversion when the dynamic type differs but elements are convertible.
//
// Takes value (reflect.Value) which is the source value to convert.
//
// Returns []E which is the converted slice, or nil when conversion is not possible.
func sliceFromReflectAs[E any](value reflect.Value) []E {
	if !value.IsValid() {
		return nil
	}
	if typed, ok := reflect.TypeAssert[[]E](value); ok {
		return typed
	}
	if value.Kind() != reflect.Slice {
		return nil
	}
	n := value.Len()
	out := make([]E, n)
	elementType := reflect.TypeFor[E]()
	for i := range n {
		element := value.Index(i)
		if element.Type() != elementType && element.Type().ConvertibleTo(elementType) {
			element = element.Convert(elementType)
		}
		if converted, ok := reflect.TypeAssert[E](element); ok {
			out[i] = converted
		}
	}
	return out
}

// handleSelect executes a select statement by building reflect.SelectCase entries from
// extension words and dispatching via reflect.Select.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which provides the per-case extension words.
// Takes registers (*Registers) which holds channels and send/recv values.
// Takes instruction (instruction) which encodes the case count and done reg.
//
// Returns OpResult indicating the next execution step.
func handleSelect(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	numCases := int(instruction.B)
	if numCases == 0 {
		return handleSelectNoCases(vm)
	}

	const wakeArmReserve = 2
	if cap(vm.selectCasesBuffer) < numCases+wakeArmReserve {
		vm.selectCasesBuffer = make([]reflect.SelectCase, numCases+wakeArmReserve)
		vm.selectInfosBuffer = make([]selectCaseInfo, numCases)
	}
	cases := vm.selectCasesBuffer[:numCases]
	caseInfos := vm.selectInfosBuffer[:numCases]
	hasDefault := false
	for i := range numCases {
		decodeSelectCase(vm, frame, registers, cases, caseInfos, i)
		if cases[i].Dir == reflect.SelectDefault {
			hasDefault = true
		}
	}
	selectCases, cancelIndex, panicIndex := vm.appendSelectWakeArms(numCases, hasDefault)
	token := vm.enterBlocking(!hasDefault && vm.Globals.selectIsCountable(cases))
	released := vm.releaseAroundBlock()
	chosen, receiver, receiveOK := reflect.Select(selectCases)
	vm.reacquireAfterBlock(released)
	vm.leaveBlocking(token)
	if chosen == cancelIndex {
		clear(selectCases)
		return vm.surfaceContextCancellation()
	}
	if chosen == panicIndex {
		clear(selectCases)
		return vm.surfaceGoroutinePanicAbort()
	}
	chosenDirection := cases[chosen].Dir
	clear(selectCases)
	registers.Ints[instruction.C] = int64(chosen)
	if chosenDirection == reflect.SelectRecv {
		applySelectReceiveResult(registers, caseInfos[chosen], receiver, receiveOK)
	}
	return opContinue
}

// handleSelectNoCases handles the degenerate `select {}` form by blocking until the VM's
// context is cancelled or a sibling goroutine panics, then surfacing the cause as an
// evaluation error. With no cancellable context and no goroutines it blocks forever,
// matching Go's own `select {}`.
//
// Takes vm (*VM) which provides the context and evalError slot.
//
// Returns opPanicError so the dispatcher unwinds to the host.
func handleSelectNoCases(vm *VM) OpResult {
	done := vm.ctx.Done()
	panicWake := vm.Globals.goroutinePanicWakeChan()
	if done == nil && panicWake == nil {
		select {}
	}
	token := vm.enterBlocking(true)
	released := vm.releaseAroundBlock()
	select {
	case <-done:
		vm.reacquireAfterBlock(released)
		vm.leaveBlocking(token)
		return vm.surfaceContextCancellation()
	case <-panicWake:
		vm.reacquireAfterBlock(released)
		vm.leaveBlocking(token)
		return vm.surfaceGoroutinePanicAbort()
	}
}

// decodeSelectCase reads the per-case extension words from frame and populates
// cases[index] / caseInfos[index] accordingly.
//
// Takes vm (*VM) which provides the buffer scratch for send-value building.
// Takes frame (*CallFrame) which provides the extension words and advancing program
// counter.
// Takes registers (*Registers) which holds the channel and value registers.
// Takes cases ([]reflect.SelectCase) which receives the populated case at index.
// Takes caseInfos ([]selectCaseInfo) which receives the matching destination metadata.
// Takes index (int) which is the case slot to populate.
func decodeSelectCase(vm *VM, frame *CallFrame, registers *Registers, cases []reflect.SelectCase, caseInfos []selectCaseInfo, index int) {
	ext1 := readExtensionWord(frame)
	frame.ProgramCounter++
	switch ext1.A {
	case isa.SelectDirectionReceive:
		cases[index] = reflect.SelectCase{Dir: reflect.SelectRecv, Chan: registers.General[ext1.B]}
		ext2 := readExtensionWord(frame)
		frame.ProgramCounter++
		caseInfos[index] = selectCaseInfo{
			destinationRegister: ext2.A,
			destinationKind:     isa.RegisterKind(ext2.B),
			hasOk:               ext1.C != 0,
			okRegister:          ext2.C,
		}
	case isa.SelectDirectionSend:
		cases[index] = reflect.SelectCase{Dir: reflect.SelectSend, Chan: registers.General[ext1.B]}
		ext2 := readExtensionWord(frame)
		frame.ProgramCounter++

		cases[index].Send = materialiseArenaValueUnconditional(vm.Arena, buildSelectSendValue(vm, registers, ext2, registers.General[ext1.B].Type().Elem()))
	case isa.SelectDirectionDefault:
		cases[index] = reflect.SelectCase{Dir: reflect.SelectDefault}
	}
}

// applySelectReceiveResult writes the receive-arm outputs after reflect.Select picked a
// receive case.
//
// Takes registers (*Registers) which receives the value and the optional comma-ok flag.
// Takes info (selectCaseInfo) which is the destination metadata.
// Takes receiver (reflect.Value) which is the received value.
// Takes receiveOK (bool) which is true when the channel produced a value (false when the
// channel was closed).
func applySelectReceiveResult(registers *Registers, info selectCaseInfo, receiver reflect.Value, receiveOK bool) {
	if receiver.IsValid() {
		writeRegisterValue(registers, info.destinationRegister, info.destinationKind, receiver)
	}
	if info.hasOk {
		if receiveOK {
			registers.Ints[info.okRegister] = 1
		} else {
			registers.Ints[info.okRegister] = 0
		}
	}
}
