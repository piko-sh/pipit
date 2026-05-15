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

// copySameKind copies a register value between frames when the source and destination
// kinds are identical.
//
// Takes destination (*Registers) which specifies the destination register banks.
// Takes source (*Registers) which specifies the source register banks.
// Takes kind (isa.RegisterKind) which specifies which register bank to copy from.
// Takes destinationRegister (uint8) which specifies the destination register index.
// Takes sourceRegister (uint8) which specifies the source register index.
//
// copyRegisterSlot shares one register set; this crosses frames.
func copySameKind(destination, source *Registers, kind isa.RegisterKind, destinationRegister, sourceRegister uint8) {
	switch kind {
	case isa.RegisterInt:
		destination.Ints[destinationRegister] = source.Ints[sourceRegister]
	case isa.RegisterFloat:
		destination.Floats[destinationRegister] = source.Floats[sourceRegister]
	case isa.RegisterString:
		destination.Strings[destinationRegister] = source.Strings[sourceRegister]
	case isa.RegisterGeneral:
		destination.General[destinationRegister] = source.General[sourceRegister]
	case isa.RegisterBool:
		destination.Bools[destinationRegister] = source.Bools[sourceRegister]
	case isa.RegisterUint:
		destination.Uints[destinationRegister] = source.Uints[sourceRegister]
	case isa.RegisterComplex:
		destination.Complex[destinationRegister] = source.Complex[sourceRegister]
	case isa.RegisterSliceInt:
		destination.SlicesInt[destinationRegister] = source.SlicesInt[sourceRegister]
	case isa.RegisterSliceFloat:
		destination.slicesFloat[destinationRegister] = source.slicesFloat[sourceRegister]
	case isa.RegisterSliceString:
		destination.slicesString[destinationRegister] = source.slicesString[sourceRegister]
	case isa.RegisterSliceBool:
		destination.slicesBool[destinationRegister] = source.slicesBool[sourceRegister]
	case isa.RegisterSliceUint:
		destination.slicesUint[destinationRegister] = source.slicesUint[sourceRegister]
	case isa.RegisterSliceByte:
		destination.slicesByte[destinationRegister] = source.slicesByte[sourceRegister]
	default:
	}
}

// copyReturnFromGeneral unpacks a general-register value into the caller's typed register
// bank, handling interface unwrapping and zero-value defaults for generics compiled as
// any.
//
// Takes callerFrame (*CallFrame) which specifies the frame to write the value into.
// Takes reflectValue (reflect.Value) which specifies the general-register value to
// unpack.
// Takes destination (VarLocation) which specifies the destination typed register
// location.
func copyReturnFromGeneral(callerFrame *CallFrame, reflectValue reflect.Value, destination program.VarLocation) {
	if reflectValue.IsValid() && reflectValue.Kind() == reflect.Interface {
		reflectValue = reflectValue.Elem()
	}
	if !reflectValue.IsValid() {
		zeroTypedRegister(&callerFrame.Registers, destination)
		return
	}
	unpackGeneralToTyped(&callerFrame.Registers, reflectValue, destination)
}

// zeroTypedRegister writes a zero value into the destination register.
//
// Takes registers (*Registers) which specifies the register banks to write into.
// Takes destination (VarLocation) which specifies the register location to zero.
func zeroTypedRegister(registers *Registers, destination program.VarLocation) {
	switch destination.Kind {
	case isa.RegisterInt:
		registers.Ints[destination.Register] = 0
	case isa.RegisterFloat:
		registers.Floats[destination.Register] = 0
	case isa.RegisterString:
		registers.Strings[destination.Register] = ""
	case isa.RegisterBool:
		registers.Bools[destination.Register] = false
	case isa.RegisterUint:
		registers.Uints[destination.Register] = 0
	case isa.RegisterComplex:
		registers.Complex[destination.Register] = 0
	case isa.RegisterSliceInt:
		registers.SlicesInt[destination.Register] = nil
	case isa.RegisterSliceFloat:
		registers.slicesFloat[destination.Register] = nil
	case isa.RegisterSliceString:
		registers.slicesString[destination.Register] = nil
	case isa.RegisterSliceBool:
		registers.slicesBool[destination.Register] = nil
	case isa.RegisterSliceUint:
		registers.slicesUint[destination.Register] = nil
	case isa.RegisterSliceByte:
		registers.slicesByte[destination.Register] = nil
	case isa.RegisterGeneral:

		registers.General[destination.Register] = reflect.Value{}
	default:
	}
}

// unpackGeneralToTyped extracts a concrete value from a reflect.Value into the
// appropriate typed register bank.
//
// One clean switch over isa.RegisterKind reads better than splitting per scalar/slice; Go
// compiles the dense enum switch to a jump table so the apparent complexity is constant
// runtime cost. The switch is over the typed bank in this transfer; isa.RegisterGeneral
// is the other side of it and is handled by the caller rather than by an arm here.
//
// Takes registers (*Registers) which specifies the register banks to write into.
// Takes v (reflect.Value) which specifies the reflect.Value to extract the concrete value
// from.
// Takes destination (VarLocation) which specifies the destination typed register
// location.
//
//nolint:revive // Reasons above.
func unpackGeneralToTyped(registers *Registers, v reflect.Value, destination program.VarLocation) {
	switch destination.Kind {
	case isa.RegisterInt:
		if v.Kind() == reflect.Bool {
			registers.Ints[destination.Register] = boolToInt64(v.Bool())
		} else {
			registers.Ints[destination.Register] = v.Int()
		}
	case isa.RegisterFloat:
		registers.Floats[destination.Register] = v.Float()
	case isa.RegisterString:
		registers.Strings[destination.Register] = v.String()
	case isa.RegisterBool:
		registers.Bools[destination.Register] = v.Bool()
	case isa.RegisterUint:
		registers.Uints[destination.Register] = v.Uint()
	case isa.RegisterComplex:
		registers.Complex[destination.Register] = v.Complex()
	case isa.RegisterSliceInt:
		if slice, ok := sliceFromReflectField[int64](v); ok {
			registers.SlicesInt[destination.Register] = slice
		}
	case isa.RegisterSliceFloat:
		if slice, ok := sliceFromReflectField[float64](v); ok {
			registers.slicesFloat[destination.Register] = slice
		}
	case isa.RegisterSliceString:
		if slice, ok := sliceFromReflectField[string](v); ok {
			registers.slicesString[destination.Register] = slice
		}
	case isa.RegisterSliceBool:
		if slice, ok := sliceFromReflectField[bool](v); ok {
			registers.slicesBool[destination.Register] = slice
		}
	case isa.RegisterSliceUint:
		if slice, ok := sliceFromReflectField[uint64](v); ok {
			registers.slicesUint[destination.Register] = slice
		}
	case isa.RegisterSliceByte:
		if slice, ok := sliceFromReflectField[byte](v); ok {
			registers.slicesByte[destination.Register] = slice
		}
	default:
	}
}

// copyReturnToGeneral boxes a typed register value into a reflect.Value and stores it in
// the caller's general register.
//
// The switch is over the typed bank in this transfer; isa.RegisterGeneral is the other
// side of it and is handled by the caller rather than by an arm here.
//
// Takes callerFrame (*CallFrame) which specifies the frame whose general register
// receives the value.
// Takes sourceRegisters (*Registers) which specifies the source register banks containing
// the typed value.
// Takes kind (isa.RegisterKind) which specifies which typed register bank to read from.
// Takes sourceRegister (uint8) which specifies the source register index.
// Takes destinationRegister (uint8) which specifies the destination general register
// index.
func copyReturnToGeneral(callerFrame *CallFrame, sourceRegisters *Registers, kind isa.RegisterKind, sourceRegister, destinationRegister uint8) {
	switch kind {
	case isa.RegisterInt:
		callerFrame.Registers.General[destinationRegister] = reflect.ValueOf(int(sourceRegisters.Ints[sourceRegister]))
	case isa.RegisterFloat:
		callerFrame.Registers.General[destinationRegister] = reflect.ValueOf(sourceRegisters.Floats[sourceRegister])
	case isa.RegisterString:
		callerFrame.Registers.General[destinationRegister] = reflect.ValueOf(sourceRegisters.Strings[sourceRegister])
	case isa.RegisterBool:
		callerFrame.Registers.General[destinationRegister] = reflect.ValueOf(sourceRegisters.Bools[sourceRegister])
	case isa.RegisterUint:
		callerFrame.Registers.General[destinationRegister] = reflect.ValueOf(sourceRegisters.Uints[sourceRegister])
	case isa.RegisterComplex:
		callerFrame.Registers.General[destinationRegister] = reflect.ValueOf(sourceRegisters.Complex[sourceRegister])
	case isa.RegisterSliceInt:
		callerFrame.Registers.General[destinationRegister] = reflect.ValueOf(sourceRegisters.SlicesInt[sourceRegister])
	case isa.RegisterSliceFloat:
		callerFrame.Registers.General[destinationRegister] = reflect.ValueOf(sourceRegisters.slicesFloat[sourceRegister])
	case isa.RegisterSliceString:
		callerFrame.Registers.General[destinationRegister] = reflect.ValueOf(sourceRegisters.slicesString[sourceRegister])
	case isa.RegisterSliceBool:
		callerFrame.Registers.General[destinationRegister] = reflect.ValueOf(sourceRegisters.slicesBool[sourceRegister])
	case isa.RegisterSliceUint:
		callerFrame.Registers.General[destinationRegister] = reflect.ValueOf(sourceRegisters.slicesUint[sourceRegister])
	case isa.RegisterSliceByte:
		callerFrame.Registers.General[destinationRegister] = reflect.ValueOf(sourceRegisters.slicesByte[sourceRegister])
	default:
	}
}

// assignReflectParams writes reflect.Value arguments into the typed register banks
// according to the compiled parameter kinds.
//
// Takes registers (*Registers) which specifies the register banks to write into.
// Takes parameterKinds ([]isa.RegisterKind) which specifies the register kind for each
// parameter position.
// Takes parameterRegisters ([]uint8) which maps each parameter position to its register
// when the callee records the table (nil otherwise).
// Takes arguments ([]reflect.Value) which provides the reflect.Value arguments to assign.
func assignReflectParams(registers *Registers, parameterKinds []isa.RegisterKind, parameterRegisters []uint8, arguments []reflect.Value) {
	var parameterIndex [isa.NumRegisterKinds]uint8
	scattered := len(parameterRegisters) == len(parameterKinds)
	for i, argument := range arguments {
		if i >= len(parameterKinds) {
			break
		}
		if argument.Kind() == reflect.Interface && !argument.IsNil() {
			argument = argument.Elem()
		}
		kind := parameterKinds[i]
		register := parameterIndex[kind]
		parameterIndex[kind]++
		if scattered {
			register = parameterRegisters[i]
		}
		assignReflectArg(registers, kind, register, argument)
	}
}

// assignReflectArg writes a single reflect.Value argument into the register bank for the
// given kind.
//
// One clean switch over isa.RegisterKind reads better than splitting per scalar/slice; Go
// compiles the dense enum switch to a jump table so the apparent complexity is constant
// runtime cost. Only the banks with a direct reflect accessor are listed. The default
// stores the value into the general bank, which is where any other kind belongs.
//
// Takes registers (*Registers) which specifies the register banks to write into.
// Takes kind (isa.RegisterKind) which specifies which register bank to target.
// Takes register (uint8) which specifies the register index within the bank.
// Takes argument (reflect.Value) which provides the reflect.Value to assign.
//
//nolint:revive // Reasons above.
func assignReflectArg(registers *Registers, kind isa.RegisterKind, register uint8, argument reflect.Value) {
	switch kind {
	case isa.RegisterInt:
		registers.Ints[register] = argument.Int()
	case isa.RegisterFloat:
		registers.Floats[register] = argument.Float()
	case isa.RegisterString:
		registers.Strings[register] = argument.String()
	case isa.RegisterBool:
		registers.Bools[register] = argument.Bool()
	case isa.RegisterUint:
		registers.Uints[register] = argument.Uint()
	case isa.RegisterComplex:
		registers.Complex[register] = argument.Complex()
	case isa.RegisterSliceInt:
		if slice, ok := reflect.TypeAssert[[]int64](argument); ok {
			registers.SlicesInt[register] = slice
		} else if slice, ok := sliceFromReflectField[int64](argument); ok {
			registers.SlicesInt[register] = slice
		}
	case isa.RegisterSliceFloat:
		if slice, ok := reflect.TypeAssert[[]float64](argument); ok {
			registers.slicesFloat[register] = slice
		} else if slice, ok := sliceFromReflectField[float64](argument); ok {
			registers.slicesFloat[register] = slice
		}
	case isa.RegisterSliceString:
		if slice, ok := reflect.TypeAssert[[]string](argument); ok {
			registers.slicesString[register] = slice
		} else if slice, ok := sliceFromReflectField[string](argument); ok {
			registers.slicesString[register] = slice
		}
	case isa.RegisterSliceBool:
		if slice, ok := reflect.TypeAssert[[]bool](argument); ok {
			registers.slicesBool[register] = slice
		}
	case isa.RegisterSliceUint:
		if slice, ok := reflect.TypeAssert[[]uint64](argument); ok {
			registers.slicesUint[register] = slice
		} else if slice, ok := sliceFromReflectField[uint64](argument); ok {
			registers.slicesUint[register] = slice
		}
	case isa.RegisterSliceByte:
		if slice, ok := reflect.TypeAssert[[]byte](argument); ok {
			registers.slicesByte[register] = slice
		} else if argument.Kind() == reflect.Slice && argument.Type().Elem().Kind() == reflect.Uint8 {
			registers.slicesByte[register] = argument.Bytes()
		}
	default:

		registers.General[register] = ValueCopyForBoundary(argument)
	}
}

// buildReflectResults packages the VM's results into a slice of reflect.Value matching
// the function's output signature.
//
// Takes allResults ([]any) which specifies all return values from the VM execution.
// Takes functionType (reflect.Type) which defines the function signature whose output
// types determine the result packaging.
//
// Returns a slice of reflect.Values with results converted to match functionType's output
// types.
func buildReflectResults(allResults []any, functionType reflect.Type) []reflect.Value {
	outputCount := functionType.NumOut()
	results := make([]reflect.Value, outputCount)
	for i := range outputCount {
		outputType := functionType.Out(i)
		if i < len(allResults) && allResults[i] != nil {
			reflectValue := reflect.ValueOf(allResults[i])
			if reflectValue.Type().ConvertibleTo(outputType) {
				results[i] = reflectValue.Convert(outputType)
			} else {
				results[i] = reflectValue
			}
		} else {
			results[i] = reflect.Zero(outputType)
		}
	}
	return results
}

// syncCellToRegister copies the value from an upvalue cell back into the register at the
// given location.
//
// Takes registers (*Registers) which specifies the register banks to write into.
// Takes cell (*UpvalueCell) which specifies the upvalue cell to read from.
// Takes location (VarLocation) which specifies the register location to write to.
func syncCellToRegister(registers *Registers, cell *program.UpvalueCell, location program.VarLocation) {
	switch location.Kind {
	case isa.RegisterInt:
		registers.Ints[location.Register] = cell.IntValue
	case isa.RegisterFloat:
		registers.Floats[location.Register] = cell.FloatValue
	case isa.RegisterString:
		registers.Strings[location.Register] = cell.StringValue
	case isa.RegisterGeneral:
		registers.General[location.Register] = cell.GeneralValue
	case isa.RegisterBool:
		registers.Bools[location.Register] = cell.BoolValue
	case isa.RegisterUint:
		registers.Uints[location.Register] = cell.UintValue
	case isa.RegisterComplex:
		registers.Complex[location.Register] = cell.ComplexValue
	case isa.RegisterSliceInt:
		registers.SlicesInt[location.Register] = cell.SliceIntValue
	case isa.RegisterSliceFloat:
		registers.slicesFloat[location.Register] = cell.SliceFloatValue
	case isa.RegisterSliceString:
		registers.slicesString[location.Register] = cell.SliceStringValue
	case isa.RegisterSliceBool:
		registers.slicesBool[location.Register] = cell.SliceBoolValue
	case isa.RegisterSliceUint:
		registers.slicesUint[location.Register] = cell.SliceUintValue
	case isa.RegisterSliceByte:
		registers.slicesByte[location.Register] = cell.SliceByteValue
	default:
	}
}

// copyRegisterSlot copies a value within the same register bank from sourceRegister to
// destinationRegister. Delegates to copySameKind passing the same register set as both
// source and destination.
//
// Takes registers (*Registers) which specifies the register banks to operate on.
// Takes kind (isa.RegisterKind) which specifies which register bank to copy within.
// Takes destinationRegister (uint8) which specifies the destination register index.
// Takes sourceRegister (uint8) which specifies the source register index.
func copyRegisterSlot(registers *Registers, kind isa.RegisterKind, destinationRegister, sourceRegister uint8) {
	copySameKind(registers, registers, kind, destinationRegister, sourceRegister)
}

// conformReflectResults makes a call's results assignable to a function type's result
// slots, which a precise signature makes stricter than the erased shape: a nil interface
// result becomes the slot's typed zero, an interface-kind value is unwrapped to its
// dynamic value, and a convertible value is converted.
//
// Takes results ([]reflect.Value) which are the results as extracted from registers.
// Takes functionType (reflect.Type) which declares the result types.
//
// Returns []reflect.Value which reflect.MakeFunc accepts for functionType.
func conformReflectResults(results []reflect.Value, functionType reflect.Type) []reflect.Value {
	for i := 0; i < functionType.NumOut() && i < len(results); i++ {
		outputType := functionType.Out(i)
		value := results[i]
		if value.IsValid() && value.Kind() == reflect.Interface {
			if value.IsNil() {
				value = reflect.Value{}
			} else {
				value = value.Elem()
			}
		}
		switch {
		case !value.IsValid():
			results[i] = reflect.Zero(outputType)
		case value.Type().AssignableTo(outputType):
			results[i] = value
		case value.Type().ConvertibleTo(outputType):
			results[i] = value.Convert(outputType)
		}
	}
	return results
}
