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
	"strings"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// boundsTableIntConstant is the table name for integer constants in bounds-check error
	// messages.
	boundsTableIntConstant = "int constant"

	// boundsTableFloatConstant is the table name for float constants in bounds-check error
	// messages.
	boundsTableFloatConstant = "float constant"

	// boundsTableStringConstant is the table name for string constants in bounds-check error
	// messages.
	boundsTableStringConstant = "string constant"

	// boundsTableGeneralConstant is the table name for general constants in bounds-check
	// error messages.
	boundsTableGeneralConstant = "general constant"

	// boundsTableBoolConstant is the table name for boolean constants in bounds-check error
	// messages.
	boundsTableBoolConstant = "bool constant"

	// boundsTableUintConstant is the table name for unsigned integer constants in
	// bounds-check error messages.
	boundsTableUintConstant = "uint constant"

	// boundsTableComplexConstant is the table name for complex number constants in
	// bounds-check error messages.
	boundsTableComplexConstant = "complex constant"

	// boundsTableFunction is the table name for functions in bounds-check error messages.
	boundsTableFunction = "function"

	// boundsTableTypeTable is the table name for type tables in bounds-check error messages.
	boundsTableTypeTable = "type table"

	// boundsTableCallSite is the table name for call sites in bounds-check error messages.
	boundsTableCallSite = "call site"

	// boundsTableRegisterKind is the table name used when a bytecode byte names a register
	// bank outside the defined range.
	boundsTableRegisterKind = "register kind"

	// boundsTableBody is the table name for the instruction body in bounds-check error
	// messages.
	boundsTableBody = "bytecode body"

	// registerRoleMap is the role name for map registers in diagnostic messages.
	registerRoleMap = "map"
)

// vMBoundsErr is a structured VM bounds-check error that keeps the Error() message
// low-cardinality for log aggregation while preserving detailed diagnostic information
// via DiagnosticDetail().
type vMBoundsErr struct {
	// TableName identifies the kind of table that was accessed out of range.
	TableName string

	// FunctionName holds the name of the function where the error occurred.
	FunctionName string

	// Index holds the out-of-range index that was requested.
	Index int

	// TableSize holds the actual size of the table.
	TableSize int

	// Pc holds the program counter at the point of the error.
	Pc int
}

// Error returns a low-cardinality error message suitable for log aggregation.
//
// Returns string containing the table name and "index out of range".
func (e *vMBoundsErr) Error() string {
	return fmt.Sprintf("%s index out of range", e.TableName)
}

// DiagnosticDetail returns the full diagnostic context for debugging, including the
// index, table size, program counter, and function name.
//
// Returns string containing the formatted diagnostic fields.
func (e *vMBoundsErr) DiagnosticDetail() string {
	return fmt.Sprintf(
		"index=%d tableSize=%d pc=%d funcName=%s",
		e.Index, e.TableSize, e.Pc, e.FunctionName,
	)
}

// vMBoundsError sets a diagnostic error on the VM when a bytecode-referenced table index
// is out of range.
//
// Takes vm (*VM) which is the virtual machine to set the error on.
// Takes frame (*CallFrame) which provides the current program counter and function name.
// Takes tableName (string) which identifies the table that was accessed out of range.
// Takes index (int) which is the requested index.
// Takes tableSize (int) which is the actual size of the table.
func vMBoundsError(vm *VM, frame *CallFrame, tableName string, index int, tableSize int) {
	vm.evalError = &vMBoundsErr{
		TableName:    tableName,
		Index:        index,
		TableSize:    tableSize,
		Pc:           frame.ProgramCounter,
		FunctionName: frame.Function.Name,
	}
}

// vMDiagnosticContext generates rich diagnostic context for VM panics, including
// disassembled bytecode and nearby registers.
//
// Takes frame (*CallFrame) which provides the current program counter and function
// bytecode.
// Takes registers (*Registers) which provides the register file to inspect.
// Takes focusRegister (int) which is the register index to centre the diagnostic output
// around.
//
// Returns a multi-line string with disassembled bytecode around the current program
// counter and the types of nearby general registers.
func vMDiagnosticContext(frame *CallFrame, registers *Registers, focusRegister int) string {
	var b strings.Builder

	pc := frame.ProgramCounter
	start := max(pc-6, 0)
	end := pc + 2
	b.WriteString("bytecode around pc:\n")
	b.WriteString(frame.Function.DisassembleRange(start, end))
	b.WriteByte('\n')

	regBase := max(focusRegister-3, 0)
	regEnd := min(focusRegister+4, len(registers.General))
	b.WriteString("nearby registers:\n")
	for i := regBase; i < regEnd; i++ {
		v := registers.General[i]
		if v.IsValid() {
			fmt.Fprintf(&b, "  general[%d]: %v (%s)\n", i, v.Type(), v.Kind())
		} else {
			fmt.Fprintf(&b, "  general[%d]: <zero>\n", i)
		}
	}

	return b.String()
}

// vMCallSiteDiagnostic generates diagnostic context specific to a native call site,
// including argument and return register mappings.
//
// Takes frame (*CallFrame) which provides the current program counter and function
// bytecode for inspecting preceding instructions.
// Takes site (*CallSite) which is the call site to diagnose.
//
// Returns a multi-line string with the current site's return and argument mappings, plus
// the preceding CALL_NATIVE site if present.
func vMCallSiteDiagnostic(frame *CallFrame, site *program.CallSite) string {
	var b strings.Builder

	b.WriteString("current site returns:\n")
	for i, ret := range site.Returns {
		fmt.Fprintf(&b, "  returns[%d]: kind=%d register=%d\n", i, ret.Kind, ret.Register)
	}
	b.WriteString("current site args:\n")
	for i, argument := range site.Arguments {
		fmt.Fprintf(&b, "  args[%d]: kind=%d register=%d\n", i, argument.Kind, argument.Register)
	}

	vmDumpPrecedingCallNative(&b, frame)

	return b.String()
}

// vmDumpPrecedingCallNative appends diagnostic information about the preceding
// instruction when it is a CALL_NATIVE.
//
// Takes b (*strings.Builder) which is the buffer to append diagnostic output to.
// Takes frame (*CallFrame) which provides the current program counter and function
// bytecode.
func vmDumpPrecedingCallNative(b *strings.Builder, frame *CallFrame) {
	pc := frame.ProgramCounter
	if pc < 2 {
		return
	}
	prevInstr := frame.Function.Body[pc-2]
	if !isa.InstrIsTier1SubOp(prevInstr, isa.SubOpCallNative) {
		return
	}
	prevSiteIndex := prevInstr.WideIndex()
	if int(prevSiteIndex) >= len(frame.Function.CallSites) {
		return
	}
	prevSite := &frame.Function.CallSites[prevSiteIndex]
	fmt.Fprintf(b,
		"preceding CALL_NATIVE (pc-2) site %d: nativeRegister=%d isMethod=%v methodReceiverRegister=%d args=%d returns=%d\n",
		prevSiteIndex, prevSite.NativeRegister,
		prevSite.IsMethod, prevSite.MethodReceiverRegister,
		len(prevSite.Arguments), len(prevSite.Returns),
	)
	for i, ret := range prevSite.Returns {
		fmt.Fprintf(b, "  prev.returns[%d]: kind=%d register=%d\n", i, ret.Kind, ret.Register)
	}
	for i, argument := range prevSite.Arguments {
		fmt.Fprintf(b, "  prev.args[%d]: kind=%d register=%d\n", i, argument.Kind, argument.Register)
	}
}

// vMPanicInvalidRegister raises an interpreted panic when a VM handler encounters a zero
// reflect.Value in a general register. Does not return.
//
// Takes handler (string) which is the name of the VM handler that detected the error.
// Takes registerRole (string) which describes the role of the register (e.g. "map" or
// "struct").
// Takes registerIndex (uint8) which is the index of the invalid register.
// Takes inst (isa.Instruction) which is the current instruction.
// Takes frame (*CallFrame) which provides the current program counter and function name.
// Takes registers (*Registers) which provides the register file for diagnostic context.
//
// Does not return; raises a recoverable panic with a formatted diagnostic message.
func vMPanicInvalidRegister(handler string, registerRole string, registerIndex uint8, inst isa.Instruction, frame *CallFrame, registers *Registers) {
	panic(newInvariantError(
		"%s - general[%d] (%s) is zero reflect.Value; "+
			"pc=%d funcName=%s; registers: a=%d b=%d c=%d\n%s",
		handler, registerIndex, registerRole,
		frame.ProgramCounter, frame.Function.Name,
		inst.A, inst.B, inst.C,
		vMDiagnosticContext(frame, registers, int(registerIndex)),
	))
}

// vMPanicNotStruct raises an interpreted panic when a VM handler expects a struct but
// finds a different kind. Does not return.
//
// Takes handler (string) which is the name of the VM handler that detected the error.
// Takes registerIndex (uint8) which is the index of the register containing the
// non-struct value.
// Takes actual (reflect.Kind) which is the kind that was found instead of struct.
// Takes inst (isa.Instruction) which is the current instruction.
// Takes frame (*CallFrame) which provides the current program counter and function name.
// Takes registers (*Registers) which provides the register file for diagnostic context.
//
// Does not return; raises a recoverable panic with a formatted diagnostic message.
func vMPanicNotStruct(handler string, registerIndex uint8, actual reflect.Kind, inst isa.Instruction, frame *CallFrame, registers *Registers) {
	panic(newInvariantError(
		"%s - general[%d] is %v, expected struct; "+
			"pc=%d funcName=%s; registers: a=%d b=%d c=%d\n%s",
		handler, registerIndex, actual,
		frame.ProgramCounter, frame.Function.Name,
		inst.A, inst.B, inst.C,
		vMDiagnosticContext(frame, registers, int(registerIndex)),
	))
}

// vmPanicNilPointerReceiver raises Go's nil-pointer dereference runtime panic when a
// struct field is accessed through a nil pointer receiver, using a panic string matching
// the Go runtime's message so the parity oracle sees the same recover() value.
func vmPanicNilPointerReceiver() {
	panic(nilDereferenceMessage)
}

// vMPanicFieldIndex panics with a diagnostic message when a struct field index is out of
// range.
//
// Takes handler (string) which is the name of the VM handler that detected the error.
// Takes structType (reflect.Type) which is the type of the struct being accessed.
// Takes fieldIndex (uint8) which is the out-of-range field index.
// Takes inst (isa.Instruction) which is the current instruction.
// Takes frame (*CallFrame) which provides the current program counter and function name.
// Takes registers (*Registers) which provides the register file for diagnostic context.
//
// Panics unconditionally with a formatted diagnostic message.
func vMPanicFieldIndex(handler string, structType reflect.Type, fieldIndex uint8, inst isa.Instruction, frame *CallFrame, registers *Registers) {
	panic(newInvariantError(
		"%s - field index %d out of range for struct %v (has %d fields); "+
			"pc=%d funcName=%s; registers: a=%d b=%d c=%d\n%s",
		handler, fieldIndex, structType, structType.NumField(),
		frame.ProgramCounter, frame.Function.Name,
		inst.A, inst.B, inst.C,
		vMDiagnosticContext(frame, registers, int(fieldIndex)),
	))
}

// vMPanicTypeMismatch panics with a diagnostic message when a Set operation would fail
// due to incompatible types.
//
// Takes handler (string) which is the name of the VM handler that detected the error.
// Takes expected (reflect.Type) which is the type that was expected.
// Takes actual (reflect.Type) which is the type that was found.
// Takes inst (isa.Instruction) which is the current instruction.
// Takes frame (*CallFrame) which provides the current program counter and function name.
// Takes registers (*Registers) which provides the register file for diagnostic context.
//
// Panics unconditionally with a formatted diagnostic message.
func vMPanicTypeMismatch(handler string, expected, actual reflect.Type, inst isa.Instruction, frame *CallFrame, registers *Registers) {
	panic(newInvariantError(
		"%s type mismatch - expected %v, got %v; "+
			"pc=%d funcName=%s; registers: a=%d b=%d c=%d\n%s",
		handler, expected, actual,
		frame.ProgramCounter, frame.Function.Name,
		inst.A, inst.B, inst.C,
		vMDiagnosticContext(frame, registers, int(inst.A)),
	))
}
