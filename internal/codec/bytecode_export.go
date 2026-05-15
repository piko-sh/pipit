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

package codec

import (
	"reflect"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab"
	"pipit.sh/pipit/internal/symtab/descriptor"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

// Type aliases expose internal types to the adapter layer for bytecode serialisation
// without duplicating struct definitions.
type (
	// RegisterKindValue is the exported alias for isa.RegisterKind.
	RegisterKindValue = isa.RegisterKind

	// InstructionValue is the exported alias for isa.Instruction.
	InstructionValue = isa.Instruction

	// GeneralConstantDescriptorInternal is the exported alias for
	// symtab.GeneralConstantDescriptor.
	GeneralConstantDescriptorInternal = descriptor.GeneralConstantDescriptor

	// TypeDescriptorInternal is the exported alias for symtab.TypeDescriptor.
	TypeDescriptorInternal = descriptor.TypeDescriptor

	// CallSiteInternal is the exported alias for engine.CallSite.
	CallSiteInternal = program.CallSite

	// VarLocationInternal is the exported alias for engine.VarLocation.
	VarLocationInternal = program.VarLocation
)

// InstructionData is a serialisation-safe representation of a single bytecode
// instruction.
type InstructionData struct {
	// Operation is the opcode byte.
	Operation uint8

	// A is the first operand (typically the destination register).
	A uint8

	// B is the second operand (source register or low immediate byte).
	B uint8

	// C is the third operand (source register or high immediate byte).
	C uint8
}

// UpvalueDescriptorData is a serialisation-safe representation of an upvalue descriptor.
type UpvalueDescriptorData struct {
	// Index is the register index in the enclosing scope.
	Index uint8

	// Kind is the register bank of the captured variable.
	Kind uint8

	// OriginalKind names the typed register bank the variable had before heap promotion.
	// Only meaningful when IsIndirect is true.
	OriginalKind uint8

	// IsLocal is true when the upvalue captures directly from the enclosing function.
	IsLocal bool

	// IsIndirect is true when the captured variable is heap-promoted in the enclosing scope.
	// Preserved across pack/unpack so the runtime dereferences the *T heap pointer instead
	// of copying it untouched into the closure body's register.
	IsIndirect bool
}

// StructFieldLayoutData is a serialisation-safe representation of a compile-time-resolved
// struct field layout entry, mirroring the internal StructFieldLayout type with public
// fields suitable for FlatBuffers packing.
type StructFieldLayoutData struct {
	// Offset is the byte offset of the leaf field within the deref'd struct.
	Offset uint32

	// TypeIndex is the TypeTable index of the deref'd struct type.
	TypeIndex uint16

	// Path is the field-index walk from the struct root to the leaf, with PathLength entries
	// used.
	Path [isa.StructFieldLayoutMaxPathDepth]uint8

	// PathLength is the number of valid entries in Path.
	PathLength uint8

	// Kind is the reflect.Kind of the leaf field.
	Kind uint8

	// RegisterKind is the RegisterKind of the leaf field.
	RegisterKind uint8

	// Flags carries metadata about the layout (see structFieldLayoutFlag* constants).
	Flags uint8

	// FieldTypeIndex is the TypeTable index of the LEAF field's reflect.Type. Required for
	// general-bank tier-0 store handlers; zero for scalar leaves.
	FieldTypeIndex uint16
}

// VarLocationData is a serialisation-safe representation of a variable's storage
// location.
type VarLocationData struct {
	// UpvalueIndex is the index into the upvalue table when IsUpvalue is true.
	UpvalueIndex int32

	// Register is the register index within the bank.
	Register uint8

	// Kind is the register bank identifier.
	Kind uint8

	// IsUpvalue is true when the variable lives on the heap.
	IsUpvalue bool

	// IsIndirect is true when the variable's address has been taken.
	IsIndirect bool

	// OriginalKind is the register bank before heap-escaping.
	OriginalKind uint8

	// IsSpilled is true when the variable lives in the spill area.
	IsSpilled bool

	// SpillSlot is the 0-based spill slot index when IsSpilled is true.
	SpillSlot uint16
}

// CallSiteData is a serialisation-safe representation of a function call site's static
// metadata.
type CallSiteData struct {
	// Arguments records where each argument lives in the caller's frame.
	Arguments []VarLocationData

	// Returns records where to store each return value in the caller's frame.
	Returns []VarLocationData

	// FunctionIndex is the index into the enclosing function's child functions slice for the
	// callee.
	FunctionIndex uint16

	// ClosureRegister is the general register holding the closure value.
	ClosureRegister uint8

	// NativeRegister is the general register holding the native function value.
	NativeRegister uint8

	// IsClosure is true when the callee is a closure stored in a general register.
	IsClosure bool

	// IsNative is true when the callee is a native Go function.
	IsNative bool

	// IsMethod is true when the callee is a bound method.
	IsMethod bool

	// MethodReceiverRegister is the general register holding the method receiver.
	MethodReceiverRegister uint8

	// IsEllipsisSpread is true when the source call used the `...` spread syntax, meaning
	// the trailing argument is already a slice to forward as-is rather than pack.
	IsEllipsisSpread bool

	// BlocksHostGoroutine is true when the callee is a native function that may block, so
	// the interpreter lock is released around the call.
	BlocksHostGoroutine bool
}

// GeneralConstantDescriptorData is a serialisation-safe representation of a general
// constant descriptor.
type GeneralConstantDescriptorData struct {
	// PackagePath is the import path of the package containing the symbol or type.
	PackagePath string

	// SymbolName is the name of the symbol or type within its package.
	SymbolName string

	// TypeDescriptor describes the composite type for composite zero values.
	TypeDescriptor descriptor.TypeDescriptorData

	// Kind identifies the source of the general constant.
	Kind uint8
}

// TypeNameData holds a type name entry paired with its type descriptor for serialisation.
type TypeNameData struct {
	// Name is the string name associated with the type.
	Name string

	// TypeDescriptor is the serialisable type descriptor.
	TypeDescriptor descriptor.TypeDescriptorData
}

// CompiledFunctionData holds serialisation-safe fields for constructing a
// CompiledFunction. Used by the unpack adapter to rebuild after deserialisation.
type CompiledFunctionData struct {
	// VariableInitFunction is the package-level variable initialisation function, or nil if
	// none exists.
	VariableInitFunction *program.CompiledFunction

	// SignatureReflectType is the function's own static Go type, or nil when the bundle
	// recorded none (an erased generic body, or a bundle written before the field existed).
	SignatureReflectType reflect.Type

	// MethodTable is the mapping of method names to their indices in the child functions
	// slice.
	MethodTable map[string]uint16

	// TypeNames is the mapping of reflect types to their string names.
	TypeNames map[reflect.Type]string

	// Name is the qualified name of the function.
	Name string

	// SourceFile is the source file path where the function was defined.
	SourceFile string

	// ResultKinds is the register bank for each return value.
	ResultKinds []isa.RegisterKind

	// Body is the slice of bytecode instructions forming the function body.
	Body []isa.Instruction

	// BoolConstants is the boolean constant pool referenced by bytecode.
	BoolConstants []bool

	// IntConstants is the int64 constant pool referenced by bytecode.
	IntConstants []int64

	// FloatConstants is the float64 constant pool referenced by bytecode.
	FloatConstants []float64

	// UintConstants is the uint64 constant pool referenced by bytecode.
	UintConstants []uint64

	// ComplexConstants is the complex128 constant pool referenced by bytecode.
	ComplexConstants []complex128

	// StringConstants is the string constant pool referenced by bytecode.
	StringConstants []string

	// GeneralConstants is the general constant pool holding reflect values referenced by
	// bytecode.
	GeneralConstants []reflect.Value

	// GeneralConstantDescriptors is the slice of descriptors that describe how to
	// reconstruct general constants.
	GeneralConstantDescriptors []descriptor.GeneralConstantDescriptor

	// TypeTable is the slice of reflect types used by the function.
	TypeTable []reflect.Type

	// TypeTableDescriptors is the slice of type descriptors for serialising the type table.
	TypeTableDescriptors []descriptor.TypeDescriptor

	// TypeTableInterfaceMethods records, aligned with TypeTable, the method-name set for
	// each entry whose source-level type was a non-empty interface. Empty entries (nil or
	// zero-length) mark non-interface or empty-interface slots.
	TypeTableInterfaceMethods [][]string

	// ParamKinds is the register bank for each parameter.
	ParamKinds []isa.RegisterKind

	// ParamRegisters records the destination register slot for each parameter as the
	// Compiler assigned it. Empty when the Compiler did not record explicit slots (older
	// bytecode bundles); the runtime then falls back to a per-bank counter.
	ParamRegisters []uint8

	// CallSites is the slice of call site metadata for each function call in the bytecode.
	CallSites []program.CallSite

	// UpvalueDescriptors is the slice of upvalue descriptors that describe captured
	// variables.
	UpvalueDescriptors []program.UpvalueDescriptor

	// Functions is the slice of child function definitions.
	Functions []*program.CompiledFunction

	// NamedResultLocations is the slice of variable locations for named return values.
	NamedResultLocations []program.VarLocation

	// StructLayoutTable is the slice of compile-time-resolved struct field layout entries
	// referenced by the opGet/SetStructField fast-path opcodes.
	StructLayoutTable []StructFieldLayoutData

	// NumRegisters is the per-bank peak register usage counts.
	NumRegisters [isa.NumRegisterKinds]uint32

	// IsVariadic is true when the function's last parameter is variadic.
	IsVariadic bool

	// HasRecover is true when the body, including nested function literals, calls recover. A
	// deferred call whose callee lacks recover runs as an ordinary frame rather than a
	// nested dispatch, so a false value here disables recover in the loaded function.
	HasRecover bool

	// IsPointerReceiver is true for a method whose declared receiver is a pointer. The VM
	// consults this when applying Go's method-set rule during stdlib-interface adapter
	// selection (see methodReceiverSatisfiesValueIn).
	IsPointerReceiver bool

	// HasReceiver is true when the function is a method, so its first parameter is the
	// receiver rather than a declared argument.
	HasReceiver bool
}

// importDescriptorMemo caches internal descriptors already built for decoded ones, keyed
// by pointer. Without it, shared subtrees would cause exponential walks during import.
type importDescriptorMemo map[*descriptor.TypeDescriptorData]*descriptor.TypeDescriptor

// ExportName returns the function name for serialisation.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns string which is the function's qualified name.
func ExportName(compiledFunction *program.CompiledFunction) string { return compiledFunction.Name }

// ExportSourceFile returns the source file name for serialisation.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns string which is the source file path where the function was defined.
func ExportSourceFile(compiledFunction *program.CompiledFunction) string {
	return compiledFunction.SourceFile
}

// ExportIsVariadic reports whether variadic arguments are accepted.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns bool which is true when the function's last parameter is variadic.
func ExportIsVariadic(compiledFunction *program.CompiledFunction) bool {
	return compiledFunction.IsVariadic
}

// ExportIsPointerReceiver reports whether the function is a method with a pointer
// receiver.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns bool which is true for pointer-receiver methods.
func ExportIsPointerReceiver(compiledFunction *program.CompiledFunction) bool {
	return compiledFunction.IsPointerReceiver
}

// ExportHasRecover reports whether the body, including nested function literals, calls
// the recover builtin.
//
// Takes compiledFunction (*CompiledFunction) which is the function to inspect.
//
// Returns true when the body can cancel a panic unwinding through it.
func ExportHasRecover(compiledFunction *program.CompiledFunction) bool {
	return compiledFunction.HasRecover
}

// ExportHasReceiver reports whether the function is a method, so its first parameter is
// the receiver.
//
// Takes compiledFunction (*CompiledFunction) which is the function to inspect.
//
// Returns true when the first parameter is a receiver.
func ExportHasReceiver(compiledFunction *program.CompiledFunction) bool {
	return compiledFunction.HasReceiver
}

// ExportSignatureReflectType returns the function's own signature type, or nil when it
// has none (an erased generic body).
//
// Takes compiledFunction (*CompiledFunction) which is the function to inspect.
//
// Returns reflect.Type which is the signature, or nil.
func ExportSignatureReflectType(compiledFunction *program.CompiledFunction) reflect.Type {
	return compiledFunction.SignatureReflectType
}

// NumRegistersSlice returns the per-bank register counts as a slice.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []uint32 which holds the peak register usage for each register bank.
func NumRegistersSlice(compiledFunction *program.CompiledFunction) []uint32 {
	return compiledFunction.NumRegisters[:]
}

// ParamKinds returns the parameter register kinds as uint8 values.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []uint8 where each element is a RegisterKind cast to uint8.
func ParamKinds(compiledFunction *program.CompiledFunction) []uint8 {
	result := make([]uint8, len(compiledFunction.ParameterKinds))
	for i, kind := range compiledFunction.ParameterKinds {
		result[i] = uint8(kind)
	}
	return result
}

// ParamRegisters returns the per-parameter destination register slot table assigned by
// the compiler.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []uint8 where each element is the register index in the parameter's bank that
// the caller must write the argument into. Empty when the compiler did not record
// explicit slots.
func ParamRegisters(compiledFunction *program.CompiledFunction) []uint8 {
	if len(compiledFunction.ParameterRegisters) == 0 {
		return nil
	}
	result := make([]uint8, len(compiledFunction.ParameterRegisters))
	copy(result, compiledFunction.ParameterRegisters)
	return result
}

// TypeTableInterfaceMethods returns the per-type-table-entry sets of interface method
// names recorded by the compiler.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns [][]string aligned with the TypeTable. Nil entries correspond to type-table
// slots that are not derived from a non-empty interface.
func TypeTableInterfaceMethods(compiledFunction *program.CompiledFunction) [][]string {
	if len(compiledFunction.TypeTableInterfaceMethods) == 0 {
		return nil
	}
	result := make([][]string, len(compiledFunction.TypeTableInterfaceMethods))
	for i, methods := range compiledFunction.TypeTableInterfaceMethods {
		if len(methods) == 0 {
			continue
		}
		copied := make([]string, len(methods))
		copy(copied, methods)
		result[i] = copied
	}
	return result
}

// ResultKinds returns the result register kinds as uint8 values.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []uint8 where each element is a RegisterKind cast to uint8.
func ResultKinds(compiledFunction *program.CompiledFunction) []uint8 {
	result := make([]uint8, len(compiledFunction.ResultKinds))
	for i, kind := range compiledFunction.ResultKinds {
		result[i] = uint8(kind)
	}
	return result
}

// Body returns the instruction body as serialisation-safe data.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []InstructionData which holds all bytecode instructions.
func Body(compiledFunction *program.CompiledFunction) []InstructionData {
	result := make([]InstructionData, len(compiledFunction.Body))
	for i, instr := range compiledFunction.Body {
		result[i] = InstructionData{Operation: uint8(instr.Op), A: instr.A, B: instr.B, C: instr.C}
	}
	return result
}

// BoolConstants returns the bool constant pool.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []bool which holds all boolean constants referenced by bytecode.
func BoolConstants(compiledFunction *program.CompiledFunction) []bool {
	return compiledFunction.BoolConstants
}

// IntConstants returns the int64 constant pool.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []int64 which holds all integer constants referenced by bytecode.
func IntConstants(compiledFunction *program.CompiledFunction) []int64 {
	return compiledFunction.IntConstants
}

// FloatConstants returns the float64 constant pool.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []float64 which holds all floating-point constants referenced by bytecode.
func FloatConstants(compiledFunction *program.CompiledFunction) []float64 {
	return compiledFunction.FloatConstants
}

// UintConstants returns the uint64 constant pool.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []uint64 which holds all unsigned integer constants referenced by bytecode.
func UintConstants(compiledFunction *program.CompiledFunction) []uint64 {
	return compiledFunction.UintConstants
}

// ComplexConstants returns the complex128 constant pool.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []complex128 which holds all complex number constants referenced by bytecode.
func ComplexConstants(compiledFunction *program.CompiledFunction) []complex128 {
	return compiledFunction.ComplexConstants
}

// StringConstants returns the string constant pool.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []string which holds all string constants referenced by bytecode.
func StringConstants(compiledFunction *program.CompiledFunction) []string {
	return compiledFunction.StringConstants
}

// GeneralConstantDescriptors returns the general constant descriptors as
// serialisation-safe data.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []GeneralConstantDescriptorData which describes how to reconstruct each general
// constant from its serialised form.
func GeneralConstantDescriptors(compiledFunction *program.CompiledFunction) []GeneralConstantDescriptorData {
	result := make([]GeneralConstantDescriptorData, len(compiledFunction.GeneralConstantDescriptors))
	for i := range compiledFunction.GeneralConstantDescriptors {
		result[i] = exportGeneralConstantDescriptor(compiledFunction.GeneralConstantDescriptors[i])
	}
	return result
}

// TypeTableDescriptors returns the type table descriptors as serialisation-safe data.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []TypeDescriptorData which describes how to reconstruct each type in the type
// table.
func TypeTableDescriptors(compiledFunction *program.CompiledFunction) []descriptor.TypeDescriptorData {
	result := make([]descriptor.TypeDescriptorData, len(compiledFunction.TypeTable))
	for i := range compiledFunction.TypeTable {
		result[i] = ExportTypeDescriptor(program.TypeTableDescriptorAt(compiledFunction, i))
	}
	return result
}

// TypeNames returns the type names map as serialisation-safe data.
//
// Driven by compiledFunction.TypeNames, which is the authoritative set. A name still
// needs a descriptor to survive the round trip (the reader rebuilds the reflect.Type from
// the descriptor), so names whose type is absent from TypeTable are omitted.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns map[reflect.Type]TypeNameData which pairs each type with its string name and
// serialisable descriptor.
func TypeNames(compiledFunction *program.CompiledFunction) map[reflect.Type]TypeNameData {
	if len(compiledFunction.TypeNames) == 0 {
		return nil
	}
	descriptorFor := make(map[reflect.Type]descriptor.TypeDescriptor, len(compiledFunction.TypeTable))
	for i, reflectType := range compiledFunction.TypeTable {
		if _, seen := descriptorFor[reflectType]; !seen {
			descriptorFor[reflectType] = program.TypeTableDescriptorAt(compiledFunction, i)
		}
	}

	result := make(map[reflect.Type]TypeNameData, len(compiledFunction.TypeNames))
	for reflectType, name := range compiledFunction.TypeNames {
		typeDescriptor, ok := descriptorFor[reflectType]
		if !ok {
			continue
		}
		result[reflectType] = TypeNameData{
			Name:           name,
			TypeDescriptor: ExportTypeDescriptor(typeDescriptor),
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// CallSites returns the call sites as serialisation-safe data.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []CallSiteData which holds the static metadata for each function call in the
// bytecode.
func CallSites(compiledFunction *program.CompiledFunction) []CallSiteData {
	result := make([]CallSiteData, len(compiledFunction.CallSites))
	for i := range compiledFunction.CallSites {
		result[i] = exportCallSite(&compiledFunction.CallSites[i])
	}
	return result
}

// UpvalueDescriptors returns the upvalue descriptors as serialisation-safe data.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []UpvalueDescriptorData which describes how each upvalue is captured.
func UpvalueDescriptors(compiledFunction *program.CompiledFunction) []UpvalueDescriptorData {
	result := make([]UpvalueDescriptorData, len(compiledFunction.UpvalueDescriptors))
	for i, upvalueDescriptor := range compiledFunction.UpvalueDescriptors {
		result[i] = UpvalueDescriptorData{
			Index:        upvalueDescriptor.Index,
			Kind:         uint8(upvalueDescriptor.Kind),
			OriginalKind: uint8(upvalueDescriptor.OriginalKind),
			IsLocal:      upvalueDescriptor.IsLocal,
			IsIndirect:   upvalueDescriptor.IsIndirect,
		}
	}
	return result
}

// NamedResultLocations returns the named result locations as serialisation-safe data.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []VarLocationData which describes where each named result is stored.
func NamedResultLocations(compiledFunction *program.CompiledFunction) []VarLocationData {
	return exportVarLocations(compiledFunction.NamedResultLocations)
}

// MethodTable returns the method table mapping method names to function indices.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns map[string]uint16 which maps method names to their indices in the child
// functions slice.
func MethodTable(compiledFunction *program.CompiledFunction) map[string]uint16 {
	return compiledFunction.MethodTable()
}

// StructLayoutTable returns the struct-field layout table as serialisation-safe data.
// Each entry encodes a compile-time-resolved struct field reference used by the
// opGet/SetStructField fast-path opcodes.
//
// Takes compiledFunction (*CompiledFunction) which is the source function.
//
// Returns []StructFieldLayoutData with one entry per StructLayoutTable slot in
// declaration order.
func StructLayoutTable(compiledFunction *program.CompiledFunction) []StructFieldLayoutData {
	result := make([]StructFieldLayoutData, len(compiledFunction.StructLayoutTable))
	for i, layout := range compiledFunction.StructLayoutTable {
		result[i] = StructFieldLayoutData(layout)
	}
	return result
}

// NewCompiledFileSetFromData constructs a CompiledFileSet from serialisation-safe data.
//
// Takes root (*CompiledFunction) which is the root function container.
// Takes variableInitFunction (*CompiledFunction) which is the variable initialiser.
// Takes entrypoints (map[string]uint16) which maps function names to indices.
// Takes initFunctionIndices ([]uint16) which holds init function indices.
//
// Returns *CompiledFileSet which is the reconstructed compiled file set.
func NewCompiledFileSetFromData(
	root *program.CompiledFunction,
	variableInitFunction *program.CompiledFunction,
	entrypoints map[string]uint16,
	initFunctionIndices []uint16,
) *program.CompiledFileSet {
	return program.NewCompiledFileSet(root, entrypoints, initFunctionIndices, variableInitFunction)
}

// NewCompiledFileSetFromDataWithVars builds a CompiledFileSet with slot allocation and
// package variable metadata.
//
// Takes root (*CompiledFunction) which is the root function.
// Takes variableInitFunction (*CompiledFunction) which is the package-level variable
// initialiser, or nil.
// Takes entrypoints (map[string]uint16) which maps entrypoint names to function indices.
// Takes initFunctionIndices ([]uint16) which lists init function indices in declaration
// order.
// Takes slotAllocation (SlotAllocation) which records the package's global slot layout.
// Takes packageVariables ([]PackageVariableMetadata) which describes the package-level
// variables.
//
// Returns *CompiledFileSet which is the populated file set.
func NewCompiledFileSetFromDataWithVars(
	root *program.CompiledFunction,
	variableInitFunction *program.CompiledFunction,
	entrypoints map[string]uint16,
	initFunctionIndices []uint16,
	slotAllocation program.SlotAllocation,
	packageVariables []program.PackageVariableMetadata,
) *program.CompiledFileSet {
	fileSet := program.NewCompiledFileSet(root, entrypoints, initFunctionIndices, variableInitFunction)
	fileSet.SetSlotAllocation(slotAllocation)
	fileSet.SetPackageVariables(packageVariables)

	return fileSet
}

// NewCompiledFunctionFromData constructs a CompiledFunction from serialisation-safe data.
// Used by the unpack adapter to rebuild after deserialisation.
//
// Takes d (*CompiledFunctionData) which holds all the fields needed to reconstruct the
// compiled function.
//
// Returns *CompiledFunction which is the reconstructed compiled function.
func NewCompiledFunctionFromData(d *CompiledFunctionData) *program.CompiledFunction {
	function := &program.CompiledFunction{
		Name:                       d.Name,
		SourceFile:                 d.SourceFile,
		IsVariadic:                 d.IsVariadic,
		IsPointerReceiver:          d.IsPointerReceiver,
		HasReceiver:                d.HasReceiver,
		SignatureReflectType:       d.SignatureReflectType,
		HasRecover:                 d.HasRecover,
		NumRegisters:               d.NumRegisters,
		ParameterKinds:             d.ParamKinds,
		ParameterRegisters:         d.ParamRegisters,
		ResultKinds:                d.ResultKinds,
		Body:                       d.Body,
		BoolConstants:              d.BoolConstants,
		IntConstants:               d.IntConstants,
		FloatConstants:             d.FloatConstants,
		UintConstants:              d.UintConstants,
		ComplexConstants:           d.ComplexConstants,
		StringConstants:            d.StringConstants,
		GeneralConstants:           d.GeneralConstants,
		GeneralConstantDescriptors: d.GeneralConstantDescriptors,
		TypeTable:                  d.TypeTable,
		TypeTableDescriptors:       d.TypeTableDescriptors,
		TypeTableInterfaceMethods:  d.TypeTableInterfaceMethods,
		TypeNames:                  d.TypeNames,
		CallSites:                  d.CallSites,
		UpvalueDescriptors:         d.UpvalueDescriptors,
		Functions:                  d.Functions,
		NamedResultLocations:       d.NamedResultLocations,

		VariableInitFunction: d.VariableInitFunction,
		StructLayoutTable:    makeStructLayoutTableFromData(d.StructLayoutTable),
	}
	function.SetMethodTable(d.MethodTable)

	return function
}

// makeStructLayoutTableFromData reconstructs the in-memory layout table from its
// serialisable representation.
//
// Takes data ([]StructFieldLayoutData) which is the round-trip data.
//
// Returns []StructFieldLayout suitable for assignment to
// CompiledFunction.StructLayoutTable.
// Returns nil for empty input.
func makeStructLayoutTableFromData(data []StructFieldLayoutData) []program.StructFieldLayout {
	if len(data) == 0 {
		return nil
	}
	result := make([]program.StructFieldLayout, len(data))
	for i, entry := range data {
		result[i] = program.StructFieldLayout(entry)
	}
	return result
}

// MakeRegisterKind creates a isa.RegisterKind from a uint8 value.
//
// Takes value (uint8) which is the register bank identifier.
//
// Returns isa.RegisterKind which is the typed register bank value.
func MakeRegisterKind(value uint8) isa.RegisterKind { return isa.RegisterKind(value) }

// MakeVarLocation creates a VarLocation from serialisation-safe data.
//
// Takes data (VarLocationData) which holds the variable location fields.
//
// Returns VarLocation which is the internal variable location.
func MakeVarLocation(data VarLocationData) program.VarLocation {
	return program.VarLocation{
		UpvalueIndex: int(data.UpvalueIndex),
		Register:     data.Register,
		Kind:         isa.RegisterKind(data.Kind),
		IsUpvalue:    data.IsUpvalue,
		IsIndirect:   data.IsIndirect,
		OriginalKind: isa.RegisterKind(data.OriginalKind),
		IsSpilled:    data.IsSpilled,
		SpillSlot:    data.SpillSlot,
	}
}

// MakeUpvalueDescriptor creates an UpvalueDescriptor from serialisation-safe data.
//
// Takes data (UpvalueDescriptorData) which holds the upvalue descriptor fields.
//
// Returns UpvalueDescriptor which is the internal upvalue descriptor.
func MakeUpvalueDescriptor(data UpvalueDescriptorData) program.UpvalueDescriptor {
	return program.UpvalueDescriptor{
		Index:        data.Index,
		Kind:         isa.RegisterKind(data.Kind),
		OriginalKind: isa.RegisterKind(data.OriginalKind),
		IsLocal:      data.IsLocal,
		IsIndirect:   data.IsIndirect,
	}
}

// MakeCallSite creates a CallSite from serialisation-safe data.
//
// Takes data (CallSiteData) which holds the call site fields.
//
// Returns CallSite which is the internal call site.
func MakeCallSite(data CallSiteData) program.CallSite {
	arguments := make([]program.VarLocation, len(data.Arguments))
	for i, argument := range data.Arguments {
		arguments[i] = MakeVarLocation(argument)
	}
	returns := make([]program.VarLocation, len(data.Returns))
	for i, returnLocation := range data.Returns {
		returns[i] = MakeVarLocation(returnLocation)
	}
	return program.CallSite{
		Arguments:              arguments,
		Returns:                returns,
		FunctionIndex:          data.FunctionIndex,
		ClosureRegister:        data.ClosureRegister,
		NativeRegister:         data.NativeRegister,
		IsClosure:              data.IsClosure,
		IsNative:               data.IsNative,
		IsMethod:               data.IsMethod,
		MethodReceiverRegister: data.MethodReceiverRegister,
		IsEllipsisSpread:       data.IsEllipsisSpread,
		BlocksHostGoroutine:    data.BlocksHostGoroutine,
	}
}

// ImportTypeDescriptor converts serialisation-safe symtab.TypeDescriptorData back to an
// internal symtab.TypeDescriptor.
//
// Takes data (descriptor.TypeDescriptorData) which holds the serialised type descriptor
// fields.
//
// Returns descriptor.TypeDescriptor which is the internal type descriptor.
func ImportTypeDescriptor(data descriptor.TypeDescriptorData) descriptor.TypeDescriptor {
	return importTypeDescriptorAtDepth(data, 0, importDescriptorMemo{})
}

// ImportGeneralConstantDescriptor converts serialisation-safe data back to an internal
// symtab.GeneralConstantDescriptor.
//
// Takes data (GeneralConstantDescriptorData) which holds the serialised descriptor
// fields.
//
// Returns symtab.GeneralConstantDescriptor which is the internal descriptor.
func ImportGeneralConstantDescriptor(data GeneralConstantDescriptorData) descriptor.GeneralConstantDescriptor {
	return descriptor.GeneralConstantDescriptor{
		PackagePath:    data.PackagePath,
		SymbolName:     data.SymbolName,
		TypeDescriptor: ImportTypeDescriptor(data.TypeDescriptor),
		Kind:           descriptor.GeneralConstantKind(data.Kind),
	}
}

// ReconstructGeneralConstant rebuilds a reflect.Value from a serialisation-safe
// descriptor using the symtab.SymbolRegistry.
//
// Takes constantDescriptor (GeneralConstantDescriptorData) which describes the constant
// to reconstruct.
// Takes registry (*symtab.SymbolRegistry) which provides symbol lookups.
//
// Returns reflect.Value which is the reconstructed runtime value.
// Returns error when the symbol or type cannot be found.
func ReconstructGeneralConstant(constantDescriptor GeneralConstantDescriptorData, registry *symtab.SymbolRegistry) (reflect.Value, error) {
	return symtab.ReconstructConstant(ImportGeneralConstantDescriptor(constantDescriptor), registry)
}

// DescriptorToReflectType reconstructs a reflect.Type from serialisation-safe descriptor
// data using the symtab.SymbolRegistry.
//
// Takes typeDescriptorData (descriptor.TypeDescriptorData) which describes the type to
// reconstruct.
// Takes registry (*symtab.SymbolRegistry) which provides named type lookups.
//
// Returns reflect.Type which is the reconstructed runtime type.
// Returns error when a named type cannot be found.
func DescriptorToReflectType(typeDescriptorData descriptor.TypeDescriptorData, registry *symtab.SymbolRegistry) (reflect.Type, error) {
	return symtab.ReflectTypeFor(ImportTypeDescriptor(typeDescriptorData), registry)
}

// ExportTypeDescriptor converts an internal symtab.TypeDescriptor to serialisation-safe
// symtab.TypeDescriptorData.
//
// Takes typeDescriptor (descriptor.TypeDescriptor) which is the internal type descriptor
// to convert.
//
// Returns descriptor.TypeDescriptorData which is the serialisation-safe representation.
func ExportTypeDescriptor(typeDescriptor descriptor.TypeDescriptor) descriptor.TypeDescriptorData {
	return exportTypeDescriptorAtDepth(typeDescriptor, 0)
}

// importTypeDescriptorAtDepth recursively converts serialisation-safe type descriptor
// data back to the internal form, tracking depth to guard against stack overflow from
// corrupt payloads.
//
// Takes data (descriptor.TypeDescriptorData) which holds the serialised fields.
// Takes depth (int) which is the current recursion depth.
// Takes memo (importDescriptorMemo) which caches already-imported pointers to break
// cycles.
//
// Returns descriptor.TypeDescriptor which is the reconstructed internal descriptor.
//
// Panics when depth reaches MaxTypeDescriptorDepth, indicating a corrupt or tampered
// bytecode payload.
func importTypeDescriptorAtDepth(data descriptor.TypeDescriptorData, depth int, memo importDescriptorMemo) descriptor.TypeDescriptor {
	if depth >= descriptor.MaxTypeDescriptorDepth {
		panic("codec: type descriptor exceeds maximum depth; corrupt or tampered bytecode payload")
	}
	return descriptor.TypeDescriptor{
		PackagePath: data.PackagePath,
		Name:        data.Name,
		BasicKind:   data.BasicKind,
		Length:      int(data.Length),
		Dir:         int(data.Dir),
		Kind:        descriptor.TypeDescriptorKind(data.Kind),
		IsVariadic:  data.IsVariadic,
		Element:     importTypeDescriptorPointer(data.Elem, depth+1, memo),
		Key:         importTypeDescriptorPointer(data.Key, depth+1, memo),
		Value:       importTypeDescriptorPointer(data.Value, depth+1, memo),
		Fields:      importTypeDescriptorFields(data.Fields, depth+1, memo),
		Params:      importTypeDescriptorList(data.Params, depth+1, memo),
		Results:     importTypeDescriptorList(data.Results, depth+1, memo),
	}
}

// importTypeDescriptorPointer imports an optional nested descriptor.
//
// Takes data (*descriptor.TypeDescriptorData) which may be nil.
// Takes depth (int) which is the recursion depth of the nested descriptor.
// Takes memo (importDescriptorMemo) which caches already-imported pointers to break
// cycles.
//
// Returns *descriptor.TypeDescriptor, nil when data is nil.
func importTypeDescriptorPointer(data *descriptor.TypeDescriptorData, depth int, memo importDescriptorMemo) *descriptor.TypeDescriptor {
	if data == nil {
		return nil
	}
	if cached, ok := memo[data]; ok {
		return cached
	}
	imported := new(importTypeDescriptorAtDepth(*data, depth, memo))
	memo[data] = imported
	return imported
}

// importTypeDescriptorList imports a descriptor sequence such as params or results.
//
// Takes items ([]descriptor.TypeDescriptorData) which may be empty.
// Takes depth (int) which is the recursion depth of each element.
// Takes memo (importDescriptorMemo) which caches already-imported pointers to break
// cycles.
//
// Returns []descriptor.TypeDescriptor, nil when items is empty.
func importTypeDescriptorList(items []descriptor.TypeDescriptorData, depth int, memo importDescriptorMemo) []descriptor.TypeDescriptor {
	if len(items) == 0 {
		return nil
	}
	out := make([]descriptor.TypeDescriptor, len(items))
	for i := range items {
		out[i] = *importTypeDescriptorPointer(&items[i], depth, memo)
	}
	return out
}

// importTypeDescriptorFields imports struct field descriptors.
//
// Takes fields ([]descriptor.TypeDescriptorFieldData) which may be empty.
// Takes depth (int) which is the recursion depth of each field type.
// Takes memo (importDescriptorMemo) which caches already-imported pointers to break
// cycles.
//
// Returns []descriptor.TypeDescriptorField, nil when fields is empty.
func importTypeDescriptorFields(fields []descriptor.TypeDescriptorFieldData, depth int, memo importDescriptorMemo) []descriptor.TypeDescriptorField {
	if len(fields) == 0 {
		return nil
	}
	out := make([]descriptor.TypeDescriptorField, len(fields))
	for i := range fields {
		out[i] = descriptor.TypeDescriptorField{
			Name:        fields[i].Name,
			Tag:         fields[i].Tag,
			PackagePath: fields[i].PackagePath,
			Typ:         *importTypeDescriptorPointer(&fields[i].Typ, depth, memo),
		}
	}
	return out
}

// exportTypeDescriptorAtDepth recursively converts an internal type descriptor to
// serialisation-safe form, tracking depth to guard against stack overflow from circular
// type references.
//
// Takes typeDescriptor (descriptor.TypeDescriptor) which is the internal descriptor to
// convert.
// Takes depth (int) which is the current recursion depth.
//
// Returns descriptor.TypeDescriptorData which is the serialisation-safe representation.
//
// Panics when depth reaches MaxTypeDescriptorDepth, indicating a circular reference in
// the type tree.
func exportTypeDescriptorAtDepth(typeDescriptor descriptor.TypeDescriptor, depth int) descriptor.TypeDescriptorData {
	if depth >= descriptor.MaxTypeDescriptorDepth {
		panic("codec: type descriptor exceeds maximum depth; circular reference in type tree")
	}
	return descriptor.TypeDescriptorData{
		PackagePath: typeDescriptor.PackagePath,
		Name:        typeDescriptor.Name,
		BasicKind:   typeDescriptor.BasicKind,
		Length:      safeconv.IntToInt32(typeDescriptor.Length),
		Dir:         safeconv.IntToInt32(typeDescriptor.Dir),
		Kind:        uint8(typeDescriptor.Kind),
		IsVariadic:  typeDescriptor.IsVariadic,
		Elem:        exportTypeDescriptorPointer(typeDescriptor.Element, depth+1),
		Key:         exportTypeDescriptorPointer(typeDescriptor.Key, depth+1),
		Value:       exportTypeDescriptorPointer(typeDescriptor.Value, depth+1),
		Fields:      exportTypeDescriptorFields(typeDescriptor.Fields, depth+1),
		Params:      exportTypeDescriptorList(typeDescriptor.Params, depth+1),
		Results:     exportTypeDescriptorList(typeDescriptor.Results, depth+1),
	}
}

// exportTypeDescriptorPointer exports an optional nested descriptor.
//
// Takes typeDescriptor (*descriptor.TypeDescriptor) which may be nil.
// Takes depth (int) which is the recursion depth of the nested descriptor.
//
// Returns *descriptor.TypeDescriptorData, nil when typeDescriptor is nil.
func exportTypeDescriptorPointer(typeDescriptor *descriptor.TypeDescriptor, depth int) *descriptor.TypeDescriptorData {
	if typeDescriptor == nil {
		return nil
	}
	return new(exportTypeDescriptorAtDepth(*typeDescriptor, depth))
}

// exportTypeDescriptorList exports a descriptor sequence such as params or results.
//
// Takes items ([]descriptor.TypeDescriptor) which may be empty.
// Takes depth (int) which is the recursion depth of each element.
//
// Returns []descriptor.TypeDescriptorData, nil when items is empty.
func exportTypeDescriptorList(items []descriptor.TypeDescriptor, depth int) []descriptor.TypeDescriptorData {
	if len(items) == 0 {
		return nil
	}
	out := make([]descriptor.TypeDescriptorData, len(items))
	for i := range items {
		out[i] = exportTypeDescriptorAtDepth(items[i], depth)
	}
	return out
}

// exportTypeDescriptorFields exports struct field descriptors.
//
// Takes fields ([]descriptor.TypeDescriptorField) which may be empty.
// Takes depth (int) which is the recursion depth of each field type.
//
// Returns []descriptor.TypeDescriptorFieldData, nil when fields is empty.
func exportTypeDescriptorFields(fields []descriptor.TypeDescriptorField, depth int) []descriptor.TypeDescriptorFieldData {
	if len(fields) == 0 {
		return nil
	}
	out := make([]descriptor.TypeDescriptorFieldData, len(fields))
	for i := range fields {
		out[i] = descriptor.TypeDescriptorFieldData{
			Name:        fields[i].Name,
			Tag:         fields[i].Tag,
			PackagePath: fields[i].PackagePath,
			Typ:         exportTypeDescriptorAtDepth(fields[i].Typ, depth),
		}
	}
	return out
}

// exportGeneralConstantDescriptor converts an internal symtab.GeneralConstantDescriptor
// to serialisation-safe data.
//
// Takes constantDescriptor (descriptor.GeneralConstantDescriptor) which is the internal
// descriptor to convert.
//
// Returns GeneralConstantDescriptorData which is the serialisation-safe representation.
func exportGeneralConstantDescriptor(constantDescriptor descriptor.GeneralConstantDescriptor) GeneralConstantDescriptorData {
	return GeneralConstantDescriptorData{
		PackagePath:    constantDescriptor.PackagePath,
		SymbolName:     constantDescriptor.SymbolName,
		TypeDescriptor: ExportTypeDescriptor(constantDescriptor.TypeDescriptor),
		Kind:           uint8(constantDescriptor.Kind),
	}
}

// exportCallSite converts an internal CallSite to serialisation-safe data.
//
// Takes site (CallSite) which is the internal call site to convert.
//
// Returns CallSiteData which is the serialisation-safe representation.
func exportCallSite(site *program.CallSite) CallSiteData {
	return CallSiteData{
		Arguments:              exportVarLocations(site.Arguments),
		Returns:                exportVarLocations(site.Returns),
		FunctionIndex:          site.FunctionIndex,
		ClosureRegister:        site.ClosureRegister,
		NativeRegister:         site.NativeRegister,
		IsClosure:              site.IsClosure,
		IsNative:               site.IsNative,
		IsMethod:               site.IsMethod,
		MethodReceiverRegister: site.MethodReceiverRegister,
		IsEllipsisSpread:       site.IsEllipsisSpread,
		BlocksHostGoroutine:    site.BlocksHostGoroutine,
	}
}

// exportVarLocations converts a slice of internal VarLocation to serialisation-safe data.
//
// Takes locations ([]VarLocation) which is the slice of internal variable locations to
// convert.
//
// Returns []VarLocationData which holds the serialisation-safe representations.
func exportVarLocations(locations []program.VarLocation) []VarLocationData {
	if len(locations) == 0 {
		return nil
	}
	result := make([]VarLocationData, len(locations))
	for i, location := range locations {
		result[i] = VarLocationData{
			UpvalueIndex: safeconv.IntToInt32(location.UpvalueIndex),
			Register:     location.Register,
			Kind:         uint8(location.Kind),
			IsUpvalue:    location.IsUpvalue,
			IsIndirect:   location.IsIndirect,
			OriginalKind: uint8(location.OriginalKind),
			IsSpilled:    location.IsSpilled,
			SpillSlot:    location.SpillSlot,
		}
	}
	return result
}
