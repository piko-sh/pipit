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

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// handleMakeClosure creates a runtime closure value by capturing upvalues from the
// current frame and storing the result in the destination register.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which is the active call frame.
// Takes registers (*Registers) which holds the register banks.
// Takes instruction (instruction) which encodes the function index.
//
// Returns OpResult indicating the next execution step.
func handleMakeClosure(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	functionIndex := instruction.WideIndex()
	if int(functionIndex) >= len(vm.functions) {
		vMBoundsError(vm, frame, boundsTableFunction, int(functionIndex), len(vm.functions))
		return opPanicError
	}
	compiledFunction := vm.functions[functionIndex]
	if len(compiledFunction.UpvalueDescriptors) == 0 {
		return emitCachedZeroUpvalueClosure(vm, registers, instruction.A, functionIndex, compiledFunction)
	}
	closure := buildCapturingClosure(vm, registers, compiledFunction)
	registers.General[instruction.A] = reflect.ValueOf(closure)
	return opContinue
}

// emitCachedZeroUpvalueClosure stores the per-functionIndex cached reflect.Value for a
// zero-upvalue closure into the destination register, populating the cache on first use.
// Zero-upvalue closures are immutable so a single shared *RuntimeClosure can back every
// invocation.
//
// Takes vm (*VM) which owns the closure cache.
// Takes registers (*Registers) which receives the cached value.
// Takes destinationRegister (uint8) which is the target general-bank slot.
// Takes functionIndex (uint16) which selects the function in vm.functions.
// Takes compiledFunction (*CompiledFunction) which is the function to wrap on first use.
//
// Returns opContinue once the destination register holds the cached closure value.
func emitCachedZeroUpvalueClosure(vm *VM, registers *Registers, destinationRegister uint8, functionIndex uint16, compiledFunction *program.CompiledFunction) OpResult {
	if int(functionIndex) < len(vm.closureCache) {
		if cached := vm.closureCache[functionIndex]; cached.IsValid() {
			registers.General[destinationRegister] = cached
			return opContinue
		}
	} else {
		grown := make([]reflect.Value, len(vm.functions))
		copy(grown, vm.closureCache)
		vm.closureCache = grown
	}
	reflectValue := reflect.ValueOf(&RuntimeClosure{Function: compiledFunction, RootFunction: vm.rootFunction, inlineCells: [inlineUpvalueCells]*program.UpvalueCell{}, upvalues: nil, namedType: ""})
	vm.closureCache[functionIndex] = reflectValue
	registers.General[destinationRegister] = reflectValue
	return opContinue
}

// buildCapturingClosure allocates and populates a *RuntimeClosure for a function that
// captures at least one upvalue.
//
// Inline backing storage is used when the capture count fits inlineUpvalueCells. The
// parent frame's sharedCells dedup map is rented from the pool on first use.
//
// Takes vm (*VM) which owns the call stack and pools.
// Takes registers (*Registers) which provides captured values for local upvalues.
// Takes compiledFunction (*CompiledFunction) which describes the captured upvalues.
//
// Returns the populated *RuntimeClosure ready for invocation.
func buildCapturingClosure(vm *VM, registers *Registers, compiledFunction *program.CompiledFunction) *RuntimeClosure {
	parentFrame := &vm.CallStack[vm.FramePointer]
	if parentFrame.sharedCells == nil {
		parentFrame.sharedCells = acquireSharedCellMap()
	}
	closure := &RuntimeClosure{Function: compiledFunction, RootFunction: vm.rootFunction, inlineCells: [inlineUpvalueCells]*program.UpvalueCell{}, upvalues: nil, namedType: ""}
	upvalueCount := len(compiledFunction.UpvalueDescriptors)
	if upvalueCount <= inlineUpvalueCells {
		closure.upvalues = closure.inlineCells[:upvalueCount]
	} else {
		closure.upvalues = make([]*program.UpvalueCell, upvalueCount)
	}
	for i, descriptor := range compiledFunction.UpvalueDescriptors {
		closure.upvalues[i] = resolveUpvalueCell(vm, registers, parentFrame, descriptor)
	}
	return closure
}

// resolveUpvalueCell returns the UpvalueCell for one descriptor, reusing the parent
// frame's upvalue when the descriptor is non-local, otherwise looking up or constructing
// the cell via the parent's sharedCells dedup map.
//
// Takes vm (*VM) which owns the call stack and registers.
// Takes registers (*Registers) which sources captured local values.
// Takes parentFrame (*CallFrame) which owns the dedup map.
// Takes descriptor (UpvalueDescriptor) which identifies the capture.
//
// Returns the resolved *UpvalueCell for the descriptor.
func resolveUpvalueCell(vm *VM, registers *Registers, parentFrame *CallFrame, descriptor program.UpvalueDescriptor) *program.UpvalueCell {
	if !descriptor.IsLocal && parentFrame.upvalues != nil {
		return parentFrame.upvalues[descriptor.Index].Value
	}
	key := isa.JoinWide(uint8(descriptor.Kind), descriptor.Index)
	if existing, ok := parentFrame.sharedCells.get(key); ok {
		if !descriptor.IsIndirect || sameIndirectCell(existing, registers.General[descriptor.Index]) {
			return existing
		}
	}
	cell := buildUpvalueCellFromDescriptor(vm, registers, descriptor)
	parentFrame.sharedCells.set(key, cell)
	return cell
}

// sameIndirectCell reports whether the shared upvalue cell still wraps the pointer cell
// currently held by the register, so a closure created now shares storage with the
// closures that captured the same variable earlier.
//
// Takes existing (*program.UpvalueCell) which is the table entry for the register.
// Takes current (reflect.Value) which is the register's present pointer cell.
//
// Returns bool which is true when both refer to the same pointee.
func sameIndirectCell(existing *program.UpvalueCell, current reflect.Value) bool {
	if !existing.IsIndirect || !existing.GeneralValue.IsValid() || !current.IsValid() {
		return false
	}
	if existing.GeneralValue.Kind() != reflect.Pointer || current.Kind() != reflect.Pointer {
		return false
	}
	return existing.GeneralValue.UnsafePointer() == current.UnsafePointer()
}

// buildUpvalueCellFromDescriptor builds an UpvalueCell for a capture.
//
// Indirect descriptors carry the *T heap pointer in generalValue and record the
// originalKind so closure-side reads can deref to the right typed bank; direct
// descriptors snapshot the parent register's value into the matching cell field.
// Centralising the per-kind dispatch keeps handleMakeClosure under the
// cognitive-complexity budget.
//
// Takes vm (*VM) which provides the arena for string materialisation.
// Takes registers (*Registers) which is the parent frame's register set being captured.
// Takes descriptor (UpvalueDescriptor) which describes which register to read and the
// closure-side cell semantics.
//
// Returns the populated UpvalueCell ready to be inserted into the closure's cells slice
// and the parent frame's sharedCells map.
func buildUpvalueCellFromDescriptor(vm *VM, registers *Registers, descriptor program.UpvalueDescriptor) *program.UpvalueCell {
	cell := &program.UpvalueCell{
		Kind:             descriptor.Kind,
		GeneralValue:     reflect.Value{},
		StringValue:      "",
		SliceIntValue:    nil,
		SliceFloatValue:  nil,
		SliceStringValue: nil,
		SliceBoolValue:   nil,
		SliceUintValue:   nil,
		SliceByteValue:   nil,
		IntValue:         0,
		FloatValue:       0,
		UintValue:        0,
		ComplexValue:     0,
		BoolValue:        false,
		IsIndirect:       false,
		OriginalKind:     0,
		IndirectKind:     0,
		IndirectElemKind: 0,
		IndirectTarget:   nil,
		IndirectType:     nil,
	}
	if descriptor.IsIndirect {
		cell.IsIndirect = true
		cell.OriginalKind = descriptor.OriginalKind
		cell.GeneralValue = registers.General[descriptor.Index]
		cacheIndirectTarget(cell)
		return cell
	}
	switch descriptor.Kind {
	case isa.RegisterInt:
		cell.IntValue = registers.Ints[descriptor.Index]
	case isa.RegisterFloat:
		cell.FloatValue = registers.Floats[descriptor.Index]
	case isa.RegisterString:
		cell.StringValue = materialiseStringUnconditional(vm.Arena, registers.Strings[descriptor.Index])
	case isa.RegisterGeneral:
		cell.GeneralValue = materialiseArenaValueUnconditional(vm.Arena, registers.General[descriptor.Index])
	case isa.RegisterBool:
		cell.BoolValue = registers.Bools[descriptor.Index]
	case isa.RegisterUint:
		cell.UintValue = registers.Uints[descriptor.Index]
	case isa.RegisterComplex:
		cell.ComplexValue = registers.Complex[descriptor.Index]
	case isa.RegisterSliceInt:
		cell.SliceIntValue = registers.SlicesInt[descriptor.Index]
	case isa.RegisterSliceFloat:
		cell.SliceFloatValue = registers.slicesFloat[descriptor.Index]
	case isa.RegisterSliceString:
		cell.SliceStringValue = registers.slicesString[descriptor.Index]
	case isa.RegisterSliceBool:
		cell.SliceBoolValue = registers.slicesBool[descriptor.Index]
	case isa.RegisterSliceUint:
		cell.SliceUintValue = registers.slicesUint[descriptor.Index]
	case isa.RegisterSliceByte:
		cell.SliceByteValue = registers.slicesByte[descriptor.Index]
	default:
	}
	return cell
}

// handleGetUpvalue reads a captured upvalue cell and copies its value into the
// appropriate typed register bank of the current frame.
//
// Takes frame (*CallFrame) which provides the upvalue array.
// Takes registers (*Registers) which is the destination register set.
// Takes instruction (instruction) which encodes the upvalue and register.
//
// Returns OpResult indicating the next execution step.
func handleGetUpvalue(_ *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	cell := frame.upvalues[instruction.B].Value
	if cell.IsIndirect {
		readIndirectCellValue(registers, cell, instruction.A, isa.RegisterKind(instruction.C))
		return opContinue
	}
	syncCellToRegister(registers, cell, program.VarLocation{Register: instruction.A, Kind: isa.RegisterKind(instruction.C)})
	return opContinue
}

// readIndirectCellValue dereferences an indirect cell's *T pointer.
//
// Stores the pointed-to value into the destination register's typed bank, unless
// destinationKind is the UpvalueKindAsPointer sentinel.
//
// Takes registers (*Registers) which is the destination register set.
// Takes cell (*UpvalueCell) which is the indirect cell carrying the pointer.
// Takes destination (uint8) which is the destination register index.
// Takes destinationKind (isa.RegisterKind) which selects the bank, or
// UpvalueKindAsPointer to copy the cell's *T pointer untouched. unpackInterfaceSlice runs
// first and services the typed-slice banks; reaching the default with a slice kind means
// that unpack declined, and leaving the register untouched is the documented fallback.
func readIndirectCellValue(registers *Registers, cell *program.UpvalueCell, destination uint8, destinationKind isa.RegisterKind) {
	pointer := cell.GeneralValue
	if !pointer.IsValid() {
		return
	}
	if destinationKind == program.UpvalueKindAsPointer {
		registers.General[destination] = pointer
		return
	}
	target := pointer.Elem()
	if unpackInterfaceSlice(registers, destinationKind, destination, target) {
		return
	}
	switch destinationKind {
	case isa.RegisterInt:
		registers.Ints[destination] = target.Int()
	case isa.RegisterFloat:
		registers.Floats[destination] = target.Float()
	case isa.RegisterString:
		registers.Strings[destination] = target.String()
	case isa.RegisterGeneral:
		registers.General[destination] = target
	case isa.RegisterBool:
		registers.Bools[destination] = target.Bool()
	case isa.RegisterUint:
		registers.Uints[destination] = target.Uint()
	case isa.RegisterComplex:
		registers.Complex[destination] = target.Complex()
	default:
	}
}

// handleSetUpvalue writes a register value into a captured upvalue cell, materialising
// arena strings to ensure they outlive the current frame.
//
// Takes vm (*VM) which is the executing VM.
// Takes frame (*CallFrame) which provides the upvalue array.
// Takes registers (*Registers) which holds the source register banks.
// Takes instruction (instruction) which encodes the upvalue and register.
//
// Returns OpResult indicating the next execution step.
func handleSetUpvalue(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	cell := frame.upvalues[instruction.B].Value
	writeRegisterToCell(vm, vm.Arena, cell, registers, isa.RegisterKind(instruction.C), instruction.A)
	return opContinue
}

// writeRegisterToCell copies the value from the specified register bank at the given
// index into the upvalue cell.
//
// Takes vm (*VM) which is the virtual machine.
// Takes arena (*RegisterArena) which provides string materialisation.
// Takes cell (*UpvalueCell) which is the destination upvalue cell.
// Takes registers (*Registers) which holds the source register banks.
// Takes kind (isa.RegisterKind) which selects the register bank to read from.
// Takes registerIndex (byte) which is the index within the selected register bank.
func writeRegisterToCell(vm *VM, arena *RegisterArena, cell *program.UpvalueCell, registers *Registers, kind isa.RegisterKind, registerIndex byte) {
	if cell.IsIndirect {
		writeRegisterThroughIndirectCell(vm, arena, cell, registers, kind, registerIndex)
		return
	}
	switch kind {
	case isa.RegisterInt:
		cell.IntValue = registers.Ints[registerIndex]
	case isa.RegisterFloat:
		cell.FloatValue = registers.Floats[registerIndex]
	case isa.RegisterString:
		cell.StringValue = materialiseStringUnconditional(arena, registers.Strings[registerIndex])
	case isa.RegisterGeneral:
		cell.GeneralValue = materialiseArenaValueUnconditional(arena, registers.General[registerIndex])
		if !arenaUsesUnsafeSlabs {
			cell.GeneralValue = safeBuildDetachSliceHeader(cell.GeneralValue)
		}
	case isa.RegisterBool:
		cell.BoolValue = registers.Bools[registerIndex]
	case isa.RegisterUint:
		cell.UintValue = registers.Uints[registerIndex]
	case isa.RegisterComplex:
		cell.ComplexValue = registers.Complex[registerIndex]
	case isa.RegisterSliceInt:
		cell.SliceIntValue = registers.SlicesInt[registerIndex]
	case isa.RegisterSliceFloat:
		cell.SliceFloatValue = registers.slicesFloat[registerIndex]
	case isa.RegisterSliceString:
		cell.SliceStringValue = registers.slicesString[registerIndex]
	case isa.RegisterSliceBool:
		cell.SliceBoolValue = registers.slicesBool[registerIndex]
	case isa.RegisterSliceUint:
		cell.SliceUintValue = registers.slicesUint[registerIndex]
	case isa.RegisterSliceByte:
		cell.SliceByteValue = registers.slicesByte[registerIndex]
	default:
	}
}

// writeRegisterThroughIndirectCell stores the source register's value through the cell's
// *T pointer so every reader of the cell observes the new value, both the declaring frame
// and any other closure that captured the same variable. Mirrors readIndirectCellValue:
// the source kind selects the typed bank to read from.
//
// Writes the banks an indirect cell can hold. The default leaves the cell untouched,
// which is the same outcome the reflect path produces for an unwritable target.
//
// Takes vm (*VM) which is the virtual machine.
// Takes arena (*RegisterArena) which is unused for non-string banks and provides string
// materialisation for strings.
// Takes cell (*UpvalueCell) which carries the *T pointer.
// Takes registers (*Registers) which is the source register set.
// Takes kind (isa.RegisterKind) which selects the source bank.
// Takes registerIndex (byte) which is the source register index.
func writeRegisterThroughIndirectCell(vm *VM, arena *RegisterArena, cell *program.UpvalueCell, registers *Registers, kind isa.RegisterKind, registerIndex byte) {
	pointer := cell.GeneralValue
	if !pointer.IsValid() {
		return
	}
	if writeIndirectCellDirect(arena, cell, registers, kind, registerIndex) {
		return
	}
	target := pointer.Elem()
	if bankValue, ok := sliceBankToReflect(registers, kind, registerIndex); ok {
		source := materialiseArenaValueUnconditional(arena, coerceValue(vm, bankValue, target.Type()))
		if !arenaUsesUnsafeSlabs {
			source = safeBuildDetachSliceHeader(source)
		}
		target.Set(source)
		return
	}
	switch kind {
	case isa.RegisterInt:
		target.SetInt(registers.Ints[registerIndex])
	case isa.RegisterFloat:
		target.SetFloat(registers.Floats[registerIndex])
	case isa.RegisterString:
		target.SetString(materialiseStringUnconditional(arena, registers.Strings[registerIndex]))
	case isa.RegisterGeneral:
		source := materialiseArenaValueUnconditional(arena, coerceValue(vm, registers.General[registerIndex], target.Type()))
		if !arenaUsesUnsafeSlabs {
			source = safeBuildDetachSliceHeader(source)
		}
		target.Set(source)
	case isa.RegisterBool:
		target.SetBool(registers.Bools[registerIndex])
	case isa.RegisterUint:
		target.SetUint(registers.Uints[registerIndex])
	case isa.RegisterComplex:
		target.SetComplex(registers.Complex[registerIndex])
	default:
	}
}

// sliceBankToReflect boxes the typed-slice register bank slot named by kind into a
// reflect.Value carrying the bank's storage slice header.
//
// Used by writeRegisterThroughIndirectCell to wrap the source bank before coerceValue
// bridges the storage type ([]int64/[]uint64) to the cell's declared static type
// ([]int/[]uint) at the write boundary. Membership test for the six typed-slice banks.
// The default is the "not a typed slice" answer, and every caller uses the false return
// to take the non-slice path.
//
// Takes registers (*Registers) which provides the typed-slice register banks.
// Takes kind (isa.RegisterKind) which selects the source bank.
// Takes registerIndex (byte) which is the index within the selected bank.
//
// Returns the boxed slice header and true when kind is a typed-slice bank, or the zero
// reflect.Value and false otherwise.
func sliceBankToReflect(registers *Registers, kind isa.RegisterKind, registerIndex byte) (reflect.Value, bool) {
	switch kind {
	case isa.RegisterSliceInt:
		return reflect.ValueOf(registers.SlicesInt[registerIndex]), true
	case isa.RegisterSliceFloat:
		return reflect.ValueOf(registers.slicesFloat[registerIndex]), true
	case isa.RegisterSliceString:
		return reflect.ValueOf(registers.slicesString[registerIndex]), true
	case isa.RegisterSliceBool:
		return reflect.ValueOf(registers.slicesBool[registerIndex]), true
	case isa.RegisterSliceUint:
		return reflect.ValueOf(registers.slicesUint[registerIndex]), true
	case isa.RegisterSliceByte:
		return reflect.ValueOf(registers.slicesByte[registerIndex]), true
	default:
	}
	return reflect.Value{}, false
}

// handleSyncClosureUpvalues synchronises a called closure's shared upvalue cells back to
// the caller's registers after the callee may have written the captured variables.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the caller's register banks.
// Takes instruction (instruction) which names the closure register in C.
//
// Returns OpResult indicating the next execution step.
func handleSyncClosureUpvalues(vm *VM, _ *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if vm.CallStack[vm.FramePointer].sharedCells == nil {
		return opContinue
	}
	syncClosureSharedUpvalues(vm, registers, instruction.C)
	return opContinue
}

// handleSyncIIFEUpvalues copies the local upvalue cells of the immediately invoked
// function expression that has just returned back into this frame's registers.
//
// Takes vm (*VM) which is the executing VM.
// Takes registers (*Registers) which holds the caller's register banks.
//
// Returns OpResult indicating the next execution step.
func handleSyncIIFEUpvalues(vm *VM, _ *CallFrame, registers *Registers, _ isa.Instruction) OpResult {
	syncCalleeUpvalues(vm, registers)
	return opContinue
}

// syncCalleeUpvalues copies modified upvalue cells from the just-returned callee frame
// back into the caller's registers (post-IIFE writeback).
//
// Takes vm (*VM) which provides access to the call stack.
// Takes registers (*Registers) which is the caller's register set.
func syncCalleeUpvalues(vm *VM, registers *Registers) {
	calleeFrame := &vm.CallStack[vm.FramePointer+1]
	for i, descriptor := range calleeFrame.Function.UpvalueDescriptors {
		if !descriptor.IsLocal {
			continue
		}
		syncUpvalueCellToRegister(registers, descriptor, calleeFrame.upvalues[i].Value)
	}
}

// syncClosureSharedUpvalues writes back upvalue cells that are shared between the caller
// frame and a closure, updating the caller's registers with any values mutated by the
// closure during execution.
//
// Takes vm (*VM) which provides access to the call stack.
// Takes registers (*Registers) which is the caller's register set.
// Takes closureRegister (uint8) which holds the closure to sync upvalues for.
func syncClosureSharedUpvalues(vm *VM, registers *Registers, closureRegister uint8) {
	closure, ok := reflect.TypeAssert[*RuntimeClosure](registers.General[closureRegister])
	if !ok {
		return
	}
	callerFrame := &vm.CallStack[vm.FramePointer]
	if callerFrame.sharedCells == nil {
		return
	}
	for i, descriptor := range closure.Function.UpvalueDescriptors {
		if !descriptor.IsLocal {
			continue
		}
		key := isa.JoinWide(uint8(descriptor.Kind), descriptor.Index)
		if !callerFrame.sharedCells.has(key) {
			continue
		}
		syncUpvalueCellToRegister(registers, descriptor, closure.upvalues[i])
	}
}

// syncUpvalueCellToRegister copies a shared upvalue cell's payload back into the register
// the descriptor names.
//
// Takes registers (*Registers) which receives the value.
// Takes descriptor (UpvalueDescriptor) which names the destination bank and index.
// Takes cell (*UpvalueCell) which holds the captured payload.
func syncUpvalueCellToRegister(registers *Registers, descriptor program.UpvalueDescriptor, cell *program.UpvalueCell) {
	syncCellToRegister(registers, cell, program.VarLocation{Register: descriptor.Index, Kind: descriptor.Kind})
}

// handleResetSharedCell removes a shared upvalue cell entry so that subsequent writes to
// that register no longer propagate to closures.
//
// Takes frame (*CallFrame) which holds the shared cell map to update.
// Takes instruction (instruction) which encodes the kind and register index.
//
// Returns OpResult indicating the next execution step.
func handleResetSharedCell(_ *VM, frame *CallFrame, _ *Registers, instruction isa.Instruction) OpResult {
	if frame.sharedCells != nil {
		key := isa.JoinWide(instruction.C, instruction.B)
		frame.sharedCells.remove(key)
	}
	return opContinue
}

// handleWriteSharedCell copies the current register value into the corresponding shared
// upvalue cell, keeping the cell in sync after a parent-frame write to a captured
// variable.
//
// Takes vm (*VM) which provides the arena for string materialisation.
// Takes frame (*CallFrame) which holds the shared cell map.
// Takes registers (*Registers) which holds the source register banks.
// Takes instruction (instruction) which encodes the register (A) and kind (B).
//
// Returns OpResult indicating the next execution step.
func handleWriteSharedCell(vm *VM, frame *CallFrame, registers *Registers, instruction isa.Instruction) OpResult {
	if frame.sharedCells == nil {
		return opContinue
	}
	key := isa.JoinWide(instruction.C, instruction.B)
	cell, ok := frame.sharedCells.get(key)
	if !ok {
		return opContinue
	}
	writeRegisterToCell(vm, vm.Arena, cell, registers, isa.RegisterKind(instruction.C), instruction.B)
	return opContinue
}
