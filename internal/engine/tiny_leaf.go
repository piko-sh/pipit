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

// SAFETY: a tiny leaf reads or writes one struct field by byte offset from the receiver's
// base pointer. The offset comes from TinyLeafLayout, which ClassifyTinyLeaf filled from
// the compile-time struct layout table for the callee's receiver type, and the dispatch
// inline cache admits the shortcut only when the live receiver's type word matches that
// type, so the offset is valid for the memory it is applied to. The base pointer is taken
// from an addressable reflect.Value, which keeps the struct alive across the access.

import (
	"reflect"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

// tinyLeafScalarArgument carries a scalar read for a set-from-argument leaf.
//
//exhaustruct:ignore
type tinyLeafScalarArgument struct {
	// intValue holds the int64 reading from the int bank.
	intValue int64

	// uintValue holds the uint64 reading from the uint bank.
	uintValue uint64

	// floatValue holds the float64 reading from the float bank.
	floatValue float64

	// boolValue holds the bool reading from the bool bank.
	boolValue bool
}

// ClassifyTinyLeaf recognises tiny-leaf shapes in compiledFunction and populates
// compiledFunction.TinyLeafShape and compiledFunction.TinyLeafLayout accordingly.
//
// Bails without setting a shape when the body length sits outside [1, 6] instructions,
// when the first op is not a recognised GET_STRUCT_FIELD_*_T0 opener, when subsequent ops
// do not match a known pattern, or when the structLayoutTable index is out of bounds.
// Runs once per function at the end of Optimise(); the allowlist is strict so anything
// not provably safe stays unclassified.
//
// Takes compiledFunction (*CompiledFunction) which is the function to classify.
func ClassifyTinyLeaf(compiledFunction *program.CompiledFunction) {
	const minTinyLeafBodyLength = 1
	const maxTinyLeafBodyLength = 6
	if len(compiledFunction.Body) < minTinyLeafBodyLength || len(compiledFunction.Body) > maxTinyLeafBodyLength {
		return
	}
	body := compiledFunction.Body
	first := findNextNonNop(body, 0)
	if first < 0 {
		return
	}
	op0 := body[first]
	switch op0.Op {
	case isa.OpGetStructFieldUint:
		tryClassifyReturnUintField(compiledFunction, body, first)
	case isa.OpGetStructFieldIntT0:
		tryClassifyEnvAtIntFieldSlotOrReturnIntField(compiledFunction, body, first)
	case isa.OpDrillTier1:
		tryClassifyIncField(compiledFunction, body, first)
	case isa.OpSetStructFieldIntT0, isa.OpSetStructFieldUint, isa.OpSetStructFieldFloat, isa.OpSetStructFieldBool:
		tryClassifySetScalarFieldFromArg(compiledFunction, body, first)
	default:
	}
}

// tryClassifyIncField matches the void `recv.fieldX++` body: a lone
// isa.SubOpIncStructFieldInt or isa.SubOpIncStructFieldUint on the receiver in general[0]
// with nothing but NOPs after it. The callee must take exactly the receiver as parameter.
//
// Takes compiledFunction (*CompiledFunction) which is the function whose shape is being
// classified.
// Takes body ([]isa.Instruction) which is the function body slice.
// Takes first (int) which is the index of the increment op.
func tryClassifyIncField(compiledFunction *program.CompiledFunction, body []isa.Instruction, first int) {
	op0 := body[first]
	var shape program.TinyLeafShape
	switch isa.SubOpcode(op0.A) {
	case isa.SubOpIncStructFieldInt:
		shape = program.TinyLeafIncIntField
	case isa.SubOpIncStructFieldUint:
		shape = program.TinyLeafIncUintField
	default:
		return
	}
	if op0.B != 0 || !tinyLeafReceiverIsOnlyParameter(compiledFunction) {
		return
	}
	layoutIndex := int(op0.C)
	if layoutIndex >= len(compiledFunction.StructLayoutTable) || hasNonNopAfter(body, first+1) {
		return
	}
	compiledFunction.TinyLeafLayout = compiledFunction.StructLayoutTable[layoutIndex]
	compiledFunction.TinyLeafShape = shape
}

// tryClassifySetScalarFieldFromArg matches the void `recv.fieldX = argument` body: a lone
// tier-0 scalar field set whose receiver is general[0] and whose value register is the
// callee's second parameter, with nothing but NOPs after it.
//
// Takes compiledFunction (*CompiledFunction) which is the function whose shape is being
// classified.
// Takes body ([]isa.Instruction) which is the function body slice.
// Takes first (int) which is the index of the set op.
func tryClassifySetScalarFieldFromArg(compiledFunction *program.CompiledFunction, body []isa.Instruction, first int) {
	op0 := body[first]
	if op0.A != 0 || len(compiledFunction.ParameterKinds) != 2 || len(compiledFunction.ParameterRegisters) != 2 {
		return
	}
	if compiledFunction.ParameterKinds[0] != isa.RegisterGeneral || compiledFunction.ParameterRegisters[0] != 0 {
		return
	}
	if compiledFunction.ParameterKinds[1] != tinyLeafSetOpBank(op0.Op) || compiledFunction.ParameterRegisters[1] != op0.B {
		return
	}
	layoutIndex := int(op0.C)
	if layoutIndex >= len(compiledFunction.StructLayoutTable) || hasNonNopAfter(body, first+1) {
		return
	}
	layout := compiledFunction.StructLayoutTable[layoutIndex]
	if tinyLeafScalarFieldBank(reflect.Kind(layout.Kind)) != compiledFunction.ParameterKinds[1] {
		return
	}
	compiledFunction.TinyLeafLayout = layout
	compiledFunction.TinyLeafShape = program.TinyLeafSetScalarFieldFromArg
}

// tinyLeafReceiverIsOnlyParameter reports whether the callee takes exactly one parameter
// held in general[0], the receiver every tiny-leaf shape reads.
//
// Takes compiledFunction (*CompiledFunction) which is the callee to inspect.
//
// Returns true when the parameter list is the lone general receiver.
func tinyLeafReceiverIsOnlyParameter(compiledFunction *program.CompiledFunction) bool {
	return len(compiledFunction.ParameterKinds) == 1 && compiledFunction.ParameterKinds[0] == isa.RegisterGeneral &&
		len(compiledFunction.ParameterRegisters) == 1 && compiledFunction.ParameterRegisters[0] == 0
}

// tinyLeafSetOpBank returns the register bank a tier-0 scalar field set reads its value
// from.
//
// Takes op (opcode) which is one of the tier-0 scalar field set opcodes.
//
// Returns the value bank, or isa.RegisterGeneral for any other opcode.
func tinyLeafSetOpBank(op isa.Opcode) isa.RegisterKind {
	switch op {
	case isa.OpSetStructFieldIntT0:
		return isa.RegisterInt
	case isa.OpSetStructFieldUint:
		return isa.RegisterUint
	case isa.OpSetStructFieldFloat:
		return isa.RegisterFloat
	case isa.OpSetStructFieldBool:
		return isa.RegisterBool
	default:
		return isa.RegisterGeneral
	}
}

// tinyLeafScalarFieldBank returns the register bank that carries values of a scalar
// struct-field kind at a call site.
//
// Takes kind (reflect.Kind) which is the field's kind.
//
// Returns the bank, or isa.RegisterGeneral for kinds no scalar bank carries.
func tinyLeafScalarFieldBank(kind reflect.Kind) isa.RegisterKind {
	switch kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return isa.RegisterInt
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return isa.RegisterUint
	case reflect.Float32, reflect.Float64:
		return isa.RegisterFloat
	case reflect.Bool:
		return isa.RegisterBool
	default:
		return isa.RegisterGeneral
	}
}

// tinyLeafSiteCompatible reports whether a static call site's argument and return slots
// line up with the callee's tiny-leaf shape.
//
// Method dispatch guarantees the layout by construction; static calls are checked because
// the Compiler may route a scalar through another bank. Switches over the tiny-leaf
// shapes this path services. The remaining shapes are handled by the default, which
// declines the fast path and falls back to the ordinary call.
//
// Takes site (*CallSite) which describes the call.
// Takes callee (*CompiledFunction) which carries the classified shape.
//
// Returns true when the inline runner can execute the call at this site.
func tinyLeafSiteCompatible(site *program.CallSite, callee *program.CompiledFunction) bool {
	if len(site.Arguments) != len(callee.ParameterKinds) {
		return false
	}
	for i, argument := range site.Arguments {
		if argument.Kind != callee.ParameterKinds[i] && (argument.Kind != isa.RegisterGeneral || i == 0) {
			return false
		}
	}
	switch callee.TinyLeafShape {
	case program.TinyLeafIncIntField, program.TinyLeafIncUintField, program.TinyLeafSetScalarFieldFromArg:
		return len(site.Returns) == 0
	case program.TinyLeafReturnIntField:
		return len(site.Returns) == 1 && site.Returns[0].Kind == isa.RegisterInt
	default:
		return len(site.Returns) == 1 && site.Returns[0].Kind == isa.RegisterUint
	}
}

// tryClassifyReturnUintField matches `GET_STRUCT_FIELD_UINT_T0 + TIER2_RETURN`.
//
// Requires GET to write uint[0] with a==0 and b==0 (receiver is param 0) and the next
// non-NOP op to be isa.OpDrillTier1 carrying the isa.SubOpDrillTier2 +
// isa.SubOpTier2Return decoder bytes. On match, stores the layout and sets the shape.
//
// Takes compiledFunction (*CompiledFunction) which is the function whose shape is being
// classified.
// Takes body ([]isa.Instruction) which is the function body slice.
// Takes first (int) which is the index of the opening GET op.
func tryClassifyReturnUintField(compiledFunction *program.CompiledFunction, body []isa.Instruction, first int) {
	op0 := body[first]
	if op0.A != 0 || op0.B != 0 {
		return
	}
	layoutIndex := int(op0.C)
	if layoutIndex >= len(compiledFunction.StructLayoutTable) {
		return
	}
	next := findNextNonNop(body, first+1)
	if next < 0 || !isTier2Return(body[next]) {
		return
	}
	if hasNonNopAfter(body, next+1) {
		return
	}
	compiledFunction.TinyLeafLayout = compiledFunction.StructLayoutTable[layoutIndex]
	compiledFunction.TinyLeafShape = program.TinyLeafReturnUintField
}

// tryClassifyEnvAtIntFieldSlotOrReturnIntField classifies bodies that begin with
// GET_STRUCT_FIELD_INT_T0. The body is either the 2-op `return recv.intField`
// (TinyLeafReturnIntField) or the 3-op `return env[recv.slot]`
// (tinyLeafReturnEnvUintAtIntFieldSlot); disambiguates by inspecting the next non-NOP op.
//
// Takes compiledFunction (*CompiledFunction) which is the function whose shape is being
// classified.
// Takes body ([]isa.Instruction) which is the function body slice.
// Takes first (int) which is the index of the opening GET op.
func tryClassifyEnvAtIntFieldSlotOrReturnIntField(compiledFunction *program.CompiledFunction, body []isa.Instruction, first int) {
	op0 := body[first]
	if op0.A != 0 || op0.B != 0 {
		return
	}
	layoutIndex := int(op0.C)
	if layoutIndex >= len(compiledFunction.StructLayoutTable) {
		return
	}
	next := findNextNonNop(body, first+1)
	if next < 0 {
		return
	}
	op1 := body[next]
	switch {
	case isTier2Return(op1):
		classifyReturnIntFieldShape(compiledFunction, body, next, layoutIndex)
	case op1.Op == isa.OpSliceGetUint:
		classifyEnvUintAtSliceGetUint(compiledFunction, body, next, layoutIndex, op1)
	case op1.Op == isa.OpDrillTier1 && isa.SubOpcode(op1.A) == isa.SubOpSliceGetUintDirect:
		classifyEnvUintAtSliceGetUintDirect(compiledFunction, body, next, layoutIndex, op1)
	}
}

// classifyReturnIntFieldShape finalises TinyLeafReturnIntField when the GET op is
// immediately followed by a tier-2 return with no other non-NOP ops trailing.
//
// Takes compiledFunction (*CompiledFunction) which is the function whose shape is being
// classified.
// Takes body ([]isa.Instruction) which is the function body.
// Takes retPos (int) which is the position of the return op.
// Takes layoutIndex (int) which indexes into structLayoutTable.
func classifyReturnIntFieldShape(compiledFunction *program.CompiledFunction, body []isa.Instruction, retPos, layoutIndex int) {
	if hasNonNopAfter(body, retPos+1) {
		return
	}
	compiledFunction.TinyLeafLayout = compiledFunction.StructLayoutTable[layoutIndex]
	compiledFunction.TinyLeafShape = program.TinyLeafReturnIntField
}

// classifyEnvUintAtSliceGetUint finalises tinyLeafReturnEnvUintAtIntFieldSlot when the
// GET op is followed by isa.OpSliceGetUint env[recv.field] and a tier-2 return.
//
// Takes compiledFunction (*CompiledFunction) which is the function whose shape is being
// classified.
// Takes body ([]isa.Instruction) which is the function body.
// Takes next (int) which is the position immediately after the GET.
// Takes layoutIndex (int) which indexes into structLayoutTable.
// Takes op1 (isa.Instruction) which is the candidate slice-get instruction.
func classifyEnvUintAtSliceGetUint(compiledFunction *program.CompiledFunction, body []isa.Instruction, next, layoutIndex int, op1 isa.Instruction) {
	if op1.A != 0 || op1.C != 0 {
		return
	}
	ret := findNextNonNop(body, next+1)
	if ret < 0 || !isTier2Return(body[ret]) {
		return
	}
	if hasNonNopAfter(body, ret+1) {
		return
	}
	compiledFunction.TinyLeafLayout = compiledFunction.StructLayoutTable[layoutIndex]
	compiledFunction.TinyLeafShape = program.TinyLeafReturnEnvUintAtIntFieldSlot
}

// classifyEnvUintAtSliceGetUintDirect finalises typed-direct shape.
//
// Finalises tinyLeafReturnEnvUintAtIntFieldSlot for the typed-direct variant emitted when
// narrow-uint widths are promoted to isa.RegisterSliceUint at the call-slot. Encoding:
//
// DRILL_TIER1 a=isa.SubOpSliceGetUintDirect b=0 c=envSliceReg EXT a=indexReg b=0 c=0
// TIER2_RETURN (1 value)
//
// b=0 because the typed-direct op writes the uint result into uints[0] (the leaf's return
// slot); c is the slicesUint register holding env.
//
// Takes compiledFunction (*CompiledFunction) which is the function whose shape is being
// classified.
// Takes body ([]isa.Instruction) which is the function body.
// Takes next (int) which is the position immediately after the GET.
// Takes layoutIndex (int) which indexes into structLayoutTable.
// Takes op1 (isa.Instruction) which is the candidate direct slice-get instruction.
func classifyEnvUintAtSliceGetUintDirect(compiledFunction *program.CompiledFunction, body []isa.Instruction, next, layoutIndex int, op1 isa.Instruction) {
	if op1.B != 0 {
		return
	}
	extPos := next + 1
	if extPos >= len(body) || body[extPos].Op != isa.OpExt {
		return
	}
	ret := findNextNonNop(body, extPos+1)
	if ret < 0 || !isTier2Return(body[ret]) {
		return
	}
	if hasNonNopAfter(body, ret+1) {
		return
	}
	compiledFunction.TinyLeafLayout = compiledFunction.StructLayoutTable[layoutIndex]
	compiledFunction.TinyLeafShape = program.TinyLeafReturnEnvUintAtIntFieldSlot
}

// findNextNonNop returns the index of the first non-NOP instruction at or after start, or
// -1 if none. NOPs in pipit are encoded as {isa.OpDrillTier1, 0, 0, 0} (the all-zero word
// that the dispatch fast-path treats as a no-op).
//
// Takes body ([]isa.Instruction) which is the function body slice.
// Takes start (int) which is the starting index.
//
// Returns the index of the first non-NOP at or after start, or -1 when only NOPs remain.
func findNextNonNop(body []isa.Instruction, start int) int {
	for i := start; i < len(body); i++ {
		instr := body[i]
		if instr.Op == isa.OpDrillTier1 && instr.A == 0 && instr.B == 0 && instr.C == 0 {
			continue
		}
		return i
	}
	return -1
}

// hasNonNopAfter reports whether any instruction at or after start is not a NOP. Used to
// guard against accidental "match" on bodies that have trailing real ops the classifier
// does not recognise.
//
// Takes body ([]isa.Instruction) which is the function body slice.
// Takes start (int) which is the starting index.
//
// Returns true when at least one non-NOP exists at or after start.
func hasNonNopAfter(body []isa.Instruction, start int) bool {
	return findNextNonNop(body, start) >= 0
}

// isTier2Return reports whether instr is the tier-2 TIER2_RETURN op. The encoding is
// {isa.OpDrillTier1, isa.SubOpDrillTier2, isa.SubOpTier2Return, count}.
//
// Takes instr (isa.Instruction) which is the candidate instruction.
//
// Returns true when instr matches the TIER2_RETURN encoding.
func isTier2Return(instr isa.Instruction) bool {
	return instr.Op == isa.OpDrillTier1 &&
		isa.SubOpcode(instr.A) == isa.SubOpDrillTier2 &&
		isa.SubOpcodeTier2(instr.B) == isa.SubOpTier2Return
}

// runTinyLeafInline executes the recognised tiny-leaf body of callee directly in the
// caller's frame, writing the result into the caller's return slot defined by
// site.Returns[0]. The path bypasses pushCompiledFrame entirely.
//
// runTinyLeafInline dispatches a classified tiny-leaf callee without pushing a frame.
// Unrecognised shapes fall through and return opContinue so the caller takes the ordinary
// call path.
//
// Takes vm (*VM) which carries the panic machinery.
// Takes registers (*Registers) which is the caller's register file.
// Takes site (*CallSite) which carries argument and return slot metadata.
// Takes callee (*CompiledFunction) which carries the tiny-leaf shape and layout.
//
// Returns opContinue.
func runTinyLeafInline(vm *VM, registers *Registers, site *program.CallSite, callee *program.CompiledFunction) OpResult {
	switch callee.TinyLeafShape {
	case program.TinyLeafReturnUintField:
		return runTinyLeafReturnUintField(vm, registers, site, callee)
	case program.TinyLeafReturnIntField:
		return runTinyLeafReturnIntField(vm, registers, site, callee)
	case program.TinyLeafReturnEnvUintAtIntFieldSlot:
		return runTinyLeafReturnEnvUintAtIntFieldSlot(vm, registers, site, callee)
	case program.TinyLeafIncIntField:
		return runTinyLeafIncIntField(vm, registers, site, callee)
	case program.TinyLeafIncUintField:
		return runTinyLeafIncUintField(vm, registers, site, callee)
	case program.TinyLeafSetScalarFieldFromArg:
		return runTinyLeafSetScalarFieldFromArg(vm, registers, site, callee)
	default:
	}
	return opContinue
}

// tinyLeafMutateFallback finishes a mutating tiny-leaf call whose receiver the inline
// path could not write through: a nil pointer receiver raises the interpreted
// nil-dereference panic, anything else runs the callee in a full frame so the regular
// handlers apply their own reflect fallbacks.
//
// Takes vm (*VM) which carries the panic machinery.
// Takes registers (*Registers) which is the caller's register file.
// Takes site (*CallSite) which describes the call.
// Takes callee (*CompiledFunction) which is the classified leaf.
// Takes recv (reflect.Value) which is the receiver that could not be written.
//
// Returns the raised panic or the framed call's result.
func tinyLeafMutateFallback(vm *VM, registers *Registers, site *program.CallSite, callee *program.CompiledFunction, recv reflect.Value) OpResult {
	if structFieldReceiverIsNilPointer(recv) {
		return raiseNilPointerDereference(vm)
	}
	return pushCompiledFrame(vm, registers, site, callee)
}

// tinyLeafArgumentValue reads the second call-site argument for a set-from-argument leaf.
//
// The caller holds the value either in the scalar bank the field kind selects or, as
// method call sites do, boxed in the general bank; a boxed value of the wrong class
// reports false so the caller falls back to a full frame. A tiny-leaf argument is
// admitted only from the four scalar banks, gated before this runs. The default is the
// bool case rather than a fallthrough.
//
// Takes registers (*Registers) which is the caller's register file.
// Takes site (*CallSite) which describes the call.
// Takes kind (reflect.Kind) which is the field's kind.
//
// Returns the scalar reading and whether the argument could be read.
func tinyLeafArgumentValue(registers *Registers, site *program.CallSite, kind reflect.Kind) (tinyLeafScalarArgument, bool) {
	argument := site.Arguments[1]
	bank := tinyLeafScalarFieldBank(kind)
	if argument.Kind == isa.RegisterGeneral {
		return tinyLeafBoxedArgumentValue(registers.General[argument.Register], bank)
	}
	if argument.Kind != bank {
		return tinyLeafScalarArgument{}, false
	}
	switch bank {
	case isa.RegisterInt:
		return tinyLeafScalarArgument{intValue: registers.Ints[argument.Register]}, true
	case isa.RegisterUint:
		return tinyLeafScalarArgument{uintValue: registers.Uints[argument.Register]}, true
	case isa.RegisterFloat:
		return tinyLeafScalarArgument{floatValue: registers.Floats[argument.Register]}, true
	default:
		return tinyLeafScalarArgument{boolValue: registers.Bools[argument.Register]}, true
	}
}

// tinyLeafBoxedArgumentValue extracts a scalar of the given bank class from a boxed
// general-bank value.
//
// A tiny-leaf argument is admitted only from the four scalar banks, gated before this
// runs. The default is the bool case rather than a fallthrough.
//
// Takes value (reflect.Value) which is the boxed argument.
// Takes bank (isa.RegisterKind) which is the scalar bank the field expects.
//
// Returns the scalar reading and whether value belongs to bank.
func tinyLeafBoxedArgumentValue(value reflect.Value, bank isa.RegisterKind) (tinyLeafScalarArgument, bool) {
	if !value.IsValid() {
		return tinyLeafScalarArgument{}, false
	}
	if value.Kind() == reflect.Interface {
		value = value.Elem()
	}
	if tinyLeafScalarFieldBank(value.Kind()) != bank {
		return tinyLeafScalarArgument{}, false
	}
	switch bank {
	case isa.RegisterInt:
		return tinyLeafScalarArgument{intValue: value.Int()}, true
	case isa.RegisterUint:
		return tinyLeafScalarArgument{uintValue: value.Uint()}, true
	case isa.RegisterFloat:
		return tinyLeafScalarArgument{floatValue: value.Float()}, true
	default:
		return tinyLeafScalarArgument{boolValue: value.Bool()}, true
	}
}

// runTinyLeafReturnUintField implements the inlined body for the `return recv.fieldX`
// shape where the field is uint-kind.
//
// Takes vm (*VM) which carries the panic machinery for nil-pointer receivers.
// Takes registers (*Registers) which is the caller's register file.
// Takes site (*CallSite) which carries argument and return slot metadata.
// Takes callee (*CompiledFunction) which carries the layout selected by the classifier.
//
// Returns opContinue once the result has been written into the caller's return register,
// or the raised interpreted panic when the receiver is a nil pointer.
//
//nolint:dupl // hot-path twin
func runTinyLeafReturnUintField(vm *VM, registers *Registers, site *program.CallSite, callee *program.CompiledFunction) OpResult {
	recv := registers.General[site.Arguments[0].Register]
	if base, ok := structFieldUnsafeBaseFromValue(recv); ok {
		fieldPointer := unsafe.Add(base, uintptr(callee.TinyLeafLayout.Offset))
		registers.Uints[site.Returns[0].Register] = readUintAt(fieldPointer, reflect.Kind(callee.TinyLeafLayout.Kind))
		return opContinue
	}
	field, fieldOK := tinyLeafReflectField(recv, callee.TinyLeafLayout)
	if !fieldOK {
		if structFieldReceiverIsNilPointer(recv) {
			return raiseNilPointerDereference(vm)
		}
		return opContinue
	}
	registers.Uints[site.Returns[0].Register] = field.Uint()
	return opContinue
}

// runTinyLeafReturnIntField implements the inlined body for the `return recv.fieldX`
// shape where the field is int-kind.
//
// Takes vm (*VM) which carries the panic machinery for nil-pointer receivers.
// Takes registers (*Registers) which is the caller's register file.
// Takes site (*CallSite) which carries argument and return slot metadata.
// Takes callee (*CompiledFunction) which carries the layout selected by the classifier.
//
// Returns opContinue once the result has been written into the caller's return register,
// or the raised interpreted panic when the receiver is a nil pointer.
//
//nolint:dupl // hot-path twin
func runTinyLeafReturnIntField(vm *VM, registers *Registers, site *program.CallSite, callee *program.CompiledFunction) OpResult {
	recv := registers.General[site.Arguments[0].Register]
	if base, ok := structFieldUnsafeBaseFromValue(recv); ok {
		fieldPointer := unsafe.Add(base, uintptr(callee.TinyLeafLayout.Offset))
		registers.Ints[site.Returns[0].Register] = readIntAt(fieldPointer, reflect.Kind(callee.TinyLeafLayout.Kind))
		return opContinue
	}
	field, fieldOK := tinyLeafReflectField(recv, callee.TinyLeafLayout)
	if !fieldOK {
		if structFieldReceiverIsNilPointer(recv) {
			return raiseNilPointerDereference(vm)
		}
		return opContinue
	}
	registers.Ints[site.Returns[0].Register] = field.Int()
	return opContinue
}

// runTinyLeafReturnEnvUintAtIntFieldSlot implements the inlined body for `return
// env[recv.slot]`.
//
// Reads the int slot field from the receiver, then indexes into the uint slice held in
// the env argument (arguments[1] for the recognised shape, varNode.Eval style). Switches
// over the env argument's bank. The remaining kinds are handled by the default, which
// delegates to tinyLeafEnvGeneralIndex().
//
// Takes vm (*VM) which carries the panic machinery.
// Takes registers (*Registers) which is the caller's register file.
// Takes site (*CallSite) which carries argument and return slot metadata.
// Takes callee (*CompiledFunction) which carries the layout selected by the classifier.
//
// Returns opContinue once the result has been written into the caller's return register.
func runTinyLeafReturnEnvUintAtIntFieldSlot(vm *VM, registers *Registers, site *program.CallSite, callee *program.CompiledFunction) OpResult {
	if len(site.Arguments) < 2 {
		return opContinue
	}
	recv := registers.General[site.Arguments[0].Register]
	base, ok := structFieldUnsafeBaseFromValue(recv)
	if !ok {
		if structFieldReceiverIsNilPointer(recv) {
			return raiseNilPointerDereference(vm)
		}
		return opContinue
	}

	fieldPointer := unsafe.Add(base, uintptr(callee.TinyLeafLayout.Offset))
	slot := readIntAt(fieldPointer, reflect.Kind(callee.TinyLeafLayout.Kind))

	switch envArg := site.Arguments[1]; envArg.Kind {
	case isa.RegisterSliceUint:
		envSlice := registers.slicesUint[envArg.Register]
		if int(slot) < 0 || int(slot) >= len(envSlice) {
			return tinyLeafIndexPanic(vm, int(slot), len(envSlice))
		}
		registers.Uints[site.Returns[0].Register] = envSlice[int(slot)]
		return opContinue
	case isa.RegisterSliceInt:
		envSlice := registers.SlicesInt[envArg.Register]
		if int(slot) < 0 || int(slot) >= len(envSlice) {
			return tinyLeafIndexPanic(vm, int(slot), len(envSlice))
		}
		registers.Uints[site.Returns[0].Register] = safeconv.Int64ToUint64Reinterpret(envSlice[int(slot)])
		return opContinue
	default:
		return tinyLeafEnvGeneralIndex(vm, registers, site, envArg, int(slot))
	}
}

// tinyLeafEnvGeneralIndex handles the reflect fallback for the env-index tiny leaf when
// the env argument lives in the general bank.
//
// Indexes the general-bank slice value reflectively. Split out as the cold path so the
// typed cases stay inline on the hot walker.
//
// Takes vm (*VM) which carries the panic machinery.
// Takes registers (*Registers) which is the caller's register file.
// Takes site (*CallSite) which carries return slot metadata.
// Takes envArg (VarLocation) which locates the env argument in the general bank.
// Takes slot (int) which is the env index derived from the receiver field.
//
// Returns opContinue once the result has been written, or opPanicError on an out-of-range
// slot.
func tinyLeafEnvGeneralIndex(vm *VM, registers *Registers, site *program.CallSite, envArg program.VarLocation, slot int) OpResult {
	envValue := registers.General[envArg.Register]
	if !envValue.IsValid() || envValue.Kind() != reflect.Slice {
		return opContinue
	}
	if slot < 0 || slot >= envValue.Len() {
		return tinyLeafIndexPanic(vm, slot, envValue.Len())
	}
	registers.Uints[site.Returns[0].Register] = envValue.Index(slot).Uint()
	return opContinue
}

// tinyLeafIndexPanic raises Go's mandatory index-out-of-range panic for the inlined
// `return env[recv.slot]` leaf when the slot is outside the env slice. The classifier
// normally guarantees the slot is in range, but a runtime-derived slot can still be out
// of bounds, and silently returning a stale register value would mask the error.
//
// Takes vm (*VM) which carries the panic machinery.
// Takes index (int) which is the offending slot index.
// Takes length (int) which is the env slice length.
//
// Returns OpResult which propagates the raised panic to the dispatch loop.
func tinyLeafIndexPanic(vm *VM, index, length int) OpResult {
	return raiseIndexOutOfRange(vm, index, length)
}

// structFieldUnsafeBaseFromValue extracts the receiver's struct base pointer from a
// reflect.Value held in the caller's general bank. Mirrors structFieldUnsafeBase but
// takes the Value directly because the leaf-inline path does not have a register index
// handy.
//
// Takes v (reflect.Value) which is the receiver value.
//
// Returns the base pointer and true when the Value is a Pointer to a struct (or an
// addressable struct directly).
// Returns (nil, false) for invalid Values, interfaces over non-pointer types, and any
// case the existing helper would refuse.
func structFieldUnsafeBaseFromValue(v reflect.Value) (unsafe.Pointer, bool) {
	if !v.IsValid() {
		return nil, false
	}

	rv := (*unsafeReflectValue)(unsafe.Pointer(&v))
	switch reflect.Kind(rv.flag & flagKindMask) {
	case reflect.Pointer:
		if rv.typ == nil {
			return nil, false
		}
		var base unsafe.Pointer
		if rv.flag&flagIndir != 0 {
			base = *(*unsafe.Pointer)(rv.ptr)
		} else {
			base = rv.ptr
		}
		if base == nil {
			return nil, false
		}
		return base, true
	case reflect.Struct:
		if rv.flag&flagAddr == 0 {
			return nil, false
		}
		return rv.ptr, true
	default:
	}
	return nil, false
}

// tinyLeafReflectField walks the layout's path via reflect to produce the leaf field when
// the unsafe base extraction failed. Conservative slow-path used by runTinyLeafInline for
// receivers the unsafe helper cannot crack (e.g., interface-typed receivers holding
// non-pointer concrete values).
//
// Takes recv (reflect.Value) which is the receiver value.
// Takes layout (StructFieldLayout) which carries the field path.
//
// Returns the leaf field and true on success.
// Returns a zero reflect.Value and false when the path cannot be walked through reflect.
func tinyLeafReflectField(recv reflect.Value, layout program.StructFieldLayout) (reflect.Value, bool) {
	if !recv.IsValid() {
		return reflect.Value{}, false
	}
	if recv.Kind() == reflect.Pointer || recv.Kind() == reflect.Interface {
		recv = recv.Elem()
	}
	if recv.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	current := recv
	for i := uint8(0); i < layout.PathLength; i++ {
		if current.Kind() != reflect.Struct {
			return reflect.Value{}, false
		}
		fieldIndex := int(layout.Path[i])
		if fieldIndex < 0 || fieldIndex >= current.NumField() {
			return reflect.Value{}, false
		}
		current = current.Field(fieldIndex)
		if i+1 < layout.PathLength && (current.Kind() == reflect.Pointer || current.Kind() == reflect.Interface) {
			current = current.Elem()
		}
	}
	return current, true
}

// readUintAt loads a uint8/16/32/64-kind field from fieldPtr. Mirrors the switch in
// handleGetStructFieldUintT0.
//
// Takes p (unsafe.Pointer) which addresses the field bytes.
// Takes kind (reflect.Kind) which selects the load width.
//
// Returns the loaded value widened to uint64, or 0 when kind is not a recognised uint
// kind.
func readUintAt(p unsafe.Pointer, kind reflect.Kind) uint64 {
	switch kind {
	case reflect.Uint:
		return uint64(*(*uint)(p))
	case reflect.Uint8:
		return uint64(*(*uint8)(p))
	case reflect.Uint16:
		return uint64(*(*uint16)(p))
	case reflect.Uint32:
		return uint64(*(*uint32)(p))
	case reflect.Uint64:
		return *(*uint64)(p)
	case reflect.Uintptr:
		return uint64(*(*uintptr)(p))
	default:
	}
	return 0
}

// readIntAt loads an int8/16/32/64-kind field from fieldPtr. Mirrors the switch in
// handleGetStructFieldIntT0.
//
// Takes p (unsafe.Pointer) which addresses the field bytes.
// Takes kind (reflect.Kind) which selects the load width.
//
// Returns the loaded value widened to int64, or 0 when kind is not a recognised int kind.
func readIntAt(p unsafe.Pointer, kind reflect.Kind) int64 {
	switch kind {
	case reflect.Int:
		return int64(*(*int)(p))
	case reflect.Int8:
		return int64(*(*int8)(p))
	case reflect.Int16:
		return int64(*(*int16)(p))
	case reflect.Int32:
		return int64(*(*int32)(p))
	case reflect.Int64:
		return *(*int64)(p)
	default:
	}
	return 0
}
