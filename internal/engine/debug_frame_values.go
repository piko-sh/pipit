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
	"pipit.sh/pipit/internal/fault"
	"pipit.sh/pipit/internal/isa"
)

// FrameVariables reads every variable live at pc in frame, copying arena-backed values to
// the Go heap so they stay valid after the VM resumes. Call it only while the VM is
// paused in the debugger.
//
// Takes frame (*CallFrame) which is a frame of this VM's call stack.
// Takes pc (int) which is the instruction index to read liveness at.
//
// Returns []VariableInfo which is nil when the function carries no debug var table.
func (vm *VM) FrameVariables(frame *CallFrame, pc int) []VariableInfo {
	if frame == nil || frame.Function == nil || frame.Function.DebugVarTable == nil {
		return nil
	}
	entries := frame.Function.DebugVarTable.LiveVariables(pc)
	variables := make([]VariableInfo, 0, len(entries))
	for _, entry := range entries {
		value := vm.ReadFrameVariable(frame, entry)
		var boxed any
		if value.IsValid() {
			boxed = value.Interface()
		}
		variables = append(variables, VariableInfo{Name: entry.Name, Value: boxed, Kind: variableKindName(entry.Location), Type: ""})
	}
	return variables
}

// ReadFrameVariable reads one variable of frame and severs any aliasing of the VM's
// arena.
//
// Takes frame (*CallFrame) which is a frame of this VM's call stack.
// Takes entry (program.DebugVarEntry) which locates the variable.
//
// Returns reflect.Value which is invalid when the location is out of range or empty.
func (vm *VM) ReadFrameVariable(frame *CallFrame, entry program.DebugVarEntry) reflect.Value {
	location := entry.Location
	var value reflect.Value
	if location.IsUpvalue {
		value = readUpvalueLocation(frame, location)
	} else {
		value = readRegisterLocation(&frame.Registers, location)
	}
	if !value.IsValid() {
		return value
	}
	if location.IsIndirect && value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return reflect.Value{}
		}
		value = value.Elem()
	}
	return vm.materialiseVariable(value)
}

// WriteFrameVariable stores value into a scalar or string variable of frame.
//
// Typed-slice, heap-promoted, captured and general-bank variables whose type does not
// match are refused with fault.ErrVariableNotWritable; scalar values are converted to the
// bank's type when the conversion is exact.
//
// Takes frame (*CallFrame) which is a frame of this VM's call stack.
// Takes entry (program.DebugVarEntry) which locates the variable.
// Takes value (reflect.Value) which is the new value.
//
// Returns error which is fault.ErrVariableNotWritable or a conversion error.
func (*VM) WriteFrameVariable(frame *CallFrame, entry program.DebugVarEntry, value reflect.Value) error {
	location := entry.Location
	if location.IsIndirect || location.IsCaptured || !value.IsValid() {
		return fault.ErrVariableNotWritable
	}
	if location.IsUpvalue {
		return writeUpvalueLocation(frame, location, value)
	}
	return writeRegisterLocation(&frame.Registers, location, value)
}

// materialiseVariable copies a value out of the VM's arena when it points into it.
//
// Takes value (reflect.Value) which may alias arena storage.
//
// Returns reflect.Value which is safe to keep after the VM resumes.
func (vm *VM) materialiseVariable(value reflect.Value) reflect.Value {
	if vm.Arena == nil {
		return value
	}
	if value.Kind() == reflect.String && value.Type() == reflect.TypeFor[string]() {
		return reflect.ValueOf(materialiseStringUnconditional(vm.Arena, value.String()))
	}
	return MaterialiseArenaValue(vm.Arena, value)
}

// registerIndex returns the bank index a location addresses, following spilled registers
// into the spill area.
//
// Takes location (program.VarLocation) which is the register location.
//
// Returns int which is the index into the bank's slice.
func registerIndex(location program.VarLocation) int {
	if location.IsSpilled {
		return isa.SpillAreaOffset + int(location.SpillSlot)
	}
	return int(location.Register)
}

// readRegisterLocation reads a register from any of the thirteen banks.
//
// Takes registers (*Registers) which is the frame's register set.
// Takes location (program.VarLocation) which selects the bank and index.
//
// Returns reflect.Value which is invalid when the index is out of range.
func readRegisterLocation(registers *Registers, location program.VarLocation) reflect.Value {
	index := registerIndex(location)
	switch location.Kind {
	case isa.RegisterInt:
		return indexedValue(registers.Ints, index)
	case isa.RegisterFloat:
		return indexedValue(registers.Floats, index)
	case isa.RegisterString:
		return indexedValue(registers.Strings, index)
	case isa.RegisterGeneral:
		if index < len(registers.General) {
			return registers.General[index]
		}
		return reflect.Value{}
	case isa.RegisterBool:
		return indexedValue(registers.Bools, index)
	case isa.RegisterUint:
		return indexedValue(registers.Uints, index)
	case isa.RegisterComplex:
		return indexedValue(registers.Complex, index)
	case isa.RegisterSliceInt:
		return indexedValue(registers.SlicesInt, index)
	case isa.RegisterSliceFloat:
		return indexedValue(registers.slicesFloat, index)
	case isa.RegisterSliceString:
		return indexedValue(registers.slicesString, index)
	case isa.RegisterSliceBool:
		return indexedValue(registers.slicesBool, index)
	case isa.RegisterSliceUint:
		return indexedValue(registers.slicesUint, index)
	case isa.RegisterSliceByte:
		return indexedValue(registers.slicesByte, index)
	default:
		return reflect.Value{}
	}
}

// indexedValue wraps bank[index] as a reflect.Value.
//
// Takes bank ([]T) which is the register bank.
// Takes index (int) which is the register index.
//
// Returns reflect.Value which is invalid when index is out of range.
func indexedValue[T any](bank []T, index int) reflect.Value {
	if index < 0 || index >= len(bank) {
		return reflect.Value{}
	}
	return reflect.ValueOf(bank[index])
}

// readUpvalueLocation reads a captured variable's cell from the frame's upvalue table.
//
// Takes frame (*CallFrame) which owns the upvalue table.
// Takes location (program.VarLocation) which selects the upvalue.
//
// Returns reflect.Value which is invalid when the upvalue is missing or empty.
func readUpvalueLocation(frame *CallFrame, location program.VarLocation) reflect.Value {
	if location.UpvalueIndex < 0 || location.UpvalueIndex >= len(frame.upvalues) {
		return reflect.Value{}
	}
	cell := frame.upvalues[location.UpvalueIndex].Value
	if cell == nil {
		return reflect.Value{}
	}
	return upvalueCellValue(cell)
}

// upvalueCellValue reads the payload an upvalue cell carries for its kind.
//
// Takes cell (*program.UpvalueCell) which is the cell.
//
// Returns reflect.Value which is invalid for an empty general cell or unknown kind.
func upvalueCellValue(cell *program.UpvalueCell) reflect.Value {
	switch cell.Kind {
	case isa.RegisterInt:
		return reflect.ValueOf(cell.IntValue)
	case isa.RegisterFloat:
		return reflect.ValueOf(cell.FloatValue)
	case isa.RegisterString:
		return reflect.ValueOf(cell.StringValue)
	case isa.RegisterGeneral:
		return cell.GeneralValue
	case isa.RegisterBool:
		return reflect.ValueOf(cell.BoolValue)
	case isa.RegisterUint:
		return reflect.ValueOf(cell.UintValue)
	case isa.RegisterComplex:
		return reflect.ValueOf(cell.ComplexValue)
	case isa.RegisterSliceInt:
		return reflect.ValueOf(cell.SliceIntValue)
	case isa.RegisterSliceFloat:
		return reflect.ValueOf(cell.SliceFloatValue)
	case isa.RegisterSliceString:
		return reflect.ValueOf(cell.SliceStringValue)
	case isa.RegisterSliceBool:
		return reflect.ValueOf(cell.SliceBoolValue)
	case isa.RegisterSliceUint:
		return reflect.ValueOf(cell.SliceUintValue)
	case isa.RegisterSliceByte:
		return reflect.ValueOf(cell.SliceByteValue)
	default:
		return reflect.Value{}
	}
}

// writeRegisterLocation stores a scalar or string into a register bank.
//
// Takes registers (*Registers) which is the frame's register set.
// Takes location (program.VarLocation) which selects the bank and index.
// Takes value (reflect.Value) which is converted to the bank's element type.
//
// Returns error which is fault.ErrVariableNotWritable for unsupported banks or values.
func writeRegisterLocation(registers *Registers, location program.VarLocation, value reflect.Value) error {
	index := registerIndex(location)
	switch location.Kind {
	case isa.RegisterInt:
		return storeScalar(registers.Ints, index, value)
	case isa.RegisterFloat:
		return storeScalar(registers.Floats, index, value)
	case isa.RegisterString:
		return storeScalar(registers.Strings, index, value)
	case isa.RegisterBool:
		return storeScalar(registers.Bools, index, value)
	case isa.RegisterUint:
		return storeScalar(registers.Uints, index, value)
	case isa.RegisterComplex:
		return storeScalar(registers.Complex, index, value)
	case isa.RegisterGeneral:
		if index >= len(registers.General) {
			return fault.ErrVariableNotWritable
		}
		existing := registers.General[index]
		if !existing.IsValid() || !value.Type().AssignableTo(existing.Type()) {
			return fault.ErrVariableNotWritable
		}
		registers.General[index] = value
		return nil
	default:
		return fault.ErrVariableNotWritable
	}
}

// storeScalar converts value to the bank's element type and stores it.
//
// Takes bank ([]T) which is the register bank.
// Takes index (int) which is the register index.
// Takes value (reflect.Value) which is converted.
//
// Returns error which is fault.ErrVariableNotWritable when the index is out of range or
// the value cannot be converted without loss.
func storeScalar[T any](bank []T, index int, value reflect.Value) error {
	if index < 0 || index >= len(bank) {
		return fault.ErrVariableNotWritable
	}
	converted, err := convertExact(value, reflect.TypeFor[T]())
	if err != nil {
		return err
	}
	typed, ok := reflect.TypeAssert[T](converted)
	if !ok {
		return fault.ErrVariableNotWritable
	}
	bank[index] = typed
	return nil
}

// writeUpvalueLocation stores a scalar or string into a captured variable's cell.
//
// Takes frame (*CallFrame) which owns the upvalue table.
// Takes location (program.VarLocation) which selects the upvalue.
// Takes value (reflect.Value) which is converted to the cell's kind.
//
// Returns error which is fault.ErrVariableNotWritable for unsupported kinds or values.
func writeUpvalueLocation(frame *CallFrame, location program.VarLocation, value reflect.Value) error {
	if location.UpvalueIndex < 0 || location.UpvalueIndex >= len(frame.upvalues) {
		return fault.ErrVariableNotWritable
	}
	cell := frame.upvalues[location.UpvalueIndex].Value
	if cell == nil {
		return fault.ErrVariableNotWritable
	}
	switch cell.Kind {
	case isa.RegisterInt:
		return storeCellScalar(&cell.IntValue, value)
	case isa.RegisterFloat:
		return storeCellScalar(&cell.FloatValue, value)
	case isa.RegisterString:
		return storeCellScalar(&cell.StringValue, value)
	case isa.RegisterBool:
		return storeCellScalar(&cell.BoolValue, value)
	case isa.RegisterUint:
		return storeCellScalar(&cell.UintValue, value)
	case isa.RegisterComplex:
		return storeCellScalar(&cell.ComplexValue, value)
	default:
		return fault.ErrVariableNotWritable
	}
}

// storeCellScalar converts value to the cell field's type and stores it.
//
// Takes target (*T) which is the cell field.
// Takes value (reflect.Value) which is converted.
//
// Returns error which is fault.ErrVariableNotWritable when the value cannot be converted
// without loss.
func storeCellScalar[T any](target *T, value reflect.Value) error {
	converted, err := convertExact(value, reflect.TypeFor[T]())
	if err != nil {
		return err
	}
	typed, ok := reflect.TypeAssert[T](converted)
	if !ok {
		return fault.ErrVariableNotWritable
	}
	*target = typed
	return nil
}

// convertExact converts value to target, refusing conversions that would change the value
// (overflow, fractional truncation, or non-convertible kinds).
//
// Takes value (reflect.Value) which is the source.
// Takes target (reflect.Type) which is the destination type.
//
// Returns reflect.Value which has type target.
// Returns error which wraps fault.ErrVariableNotWritable when the conversion is not
// exact.
func convertExact(value reflect.Value, target reflect.Type) (reflect.Value, error) {
	if value.Kind() == reflect.Interface && !value.IsNil() {
		value = value.Elem()
	}
	if !value.IsValid() || !value.Type().ConvertibleTo(target) {
		return reflect.Value{}, fault.ErrVariableNotWritable
	}
	if value.Kind() == reflect.String && target.Kind() != reflect.String {
		return reflect.Value{}, fault.ErrVariableNotWritable
	}
	converted := value.Convert(target)
	if converted.Kind() != reflect.String && converted.Type().ConvertibleTo(value.Type()) {
		roundTrip := converted.Convert(value.Type())
		if !roundTrip.Equal(value) {
			return reflect.Value{}, fmt.Errorf("%w: %v does not fit %s", fault.ErrVariableNotWritable, value.Interface(), target)
		}
	}
	return converted, nil
}

// variableKindName names the register bank a variable lives in.
//
// Takes location (program.VarLocation) which is the variable's location.
//
// Returns string which is the bank name, e.g. "int" or "string".
func variableKindName(location program.VarLocation) string {
	kind := location.Kind
	if location.IsIndirect && location.OriginalKind != 0 {
		kind = location.OriginalKind
	}
	return kind.String()
}
