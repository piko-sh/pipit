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

package adapters

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"pipit.sh/pipit/internal/codec"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab/descriptor"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/symtab"

	"pipit.sh/pipit/internal/safeconv"

	"pipit.sh/pipit/internal/mem"
	"pipit.sh/pipit/internal/schema"
	"pipit.sh/pipit/internal/schema/schemagen"
)

const (
	// maxBytecodeTypeDescriptorDepth caps the recursion depth when reconstructing nested
	// type descriptors from a cached bytecode file. The cap defends against stack exhaustion
	// from a tampered or corrupted on-disk payload.
	maxBytecodeTypeDescriptorDepth = 256

	// maxBytecodeFunctionNestingDepth caps the recursion depth when reconstructing nested
	// compiled functions (closures within closures) from a cached bytecode file.
	maxBytecodeFunctionNestingDepth = 256

	// structFieldLayoutMaxPathDepth mirrors engine's path-depth cap so reconstructed
	// StructFieldLayoutData arrays match the domain type's array size at compile time.
	structFieldLayoutMaxPathDepth = 4

	// slotBankInt is the global-store bank holding int64 slots.
	slotBankInt = 0

	// slotBankFloat is the global-store bank holding float64 slots.
	slotBankFloat = 1

	// slotBankString is the global-store bank holding string slots.
	slotBankString = 2

	// slotBankBool is the global-store bank holding bool slots.
	slotBankBool = 3

	// slotBankUint is the global-store bank holding uint64 slots.
	slotBankUint = 4

	// slotBankComplex is the global-store bank holding complex128 slots.
	slotBankComplex = 5

	// slotBankGeneral is the global-store bank holding reflect.Value slots.
	slotBankGeneral = 6
)

var (
	// errBytecodeRecursionDepthExceeded indicates that the recursion depth bound was reached
	// while unpacking a cached bytecode file. The cache should be considered corrupt or
	// tampered and discarded.
	errBytecodeRecursionDepthExceeded = errors.New("bytecode unpack recursion depth exceeded")

	// errCorruptBytecodePayload signals a tampered bytecode payload.
	errCorruptBytecodePayload = errors.New("corrupt bytecode payload")
)

// LoadCompiledFromBytes deserialises a pipit-packed bytecode payload from memory.
//
// Takes data ([]byte) which carries the packed bytecode payload.
// Takes registry (*symtab.SymbolRegistry) which provides symbol and type lookups for
// runtime reconstruction.
//
// Returns *program.CompiledFileSet which is the reconstructed file set.
// Returns error when the schema header does not match the current binary or the payload
// is corrupt.
func LoadCompiledFromBytes(data []byte, registry *symtab.SymbolRegistry) (*program.CompiledFileSet, error) {
	if len(data) == 0 {
		return nil, errors.New("bytecode payload is empty")
	}
	payload, err := schema.Unpack(data)
	if err != nil {
		return nil, fmt.Errorf("unpacking bytecode header: %w", err)
	}
	return decodeCompiledFileSet(context.Background(), payload, registry)
}

// boundedCount validates a FlatBuffer vector length.
//
// A vector of n elements occupies at least n bytes, so any length greater than the
// payload size proves the payload is truncated or tampered. Returning an error here stops
// a small crafted payload from forcing a multi-gigabyte make.
//
// Takes declared (int) which is the length the FlatBuffer accessor reported.
// Takes payloadLen (int) which is the total payload size in bytes.
// Takes what (string) which names the vector for the error message.
//
// Returns the validated length, or an error when it is implausible.
func boundedCount(declared, payloadLen int, what string) (int, error) {
	if declared < 0 || declared > payloadLen {
		return 0, fmt.Errorf("%w: %s count %d exceeds payload size %d bytes", errCorruptBytecodePayload, what, declared, payloadLen)
	}
	return declared, nil
}

// validatedRegisterKind converts a serialised register-kind byte to a uint8 after
// confirming it names a real register kind. A tampered payload can carry an out-of-range
// byte (including values that would panic safeconv.MustInt8ToUint8); rejecting it keeps
// unpacking from corrupting a register bank or crashing the host.
//
// Takes raw (int8) which is the register-kind value read from the FlatBuffer.
// Takes what (string) which names the field for the error message.
//
// Returns the validated kind byte, or an error when it is out of range.
func validatedRegisterKind(raw int8, what string) (uint8, error) {
	if raw < 0 || int(raw) >= isa.NumRegisterKinds {
		return 0, fmt.Errorf("%w: %s register kind %d out of range [0,%d)", errCorruptBytecodePayload, what, raw, isa.NumRegisterKinds)
	}
	return uint8(raw), nil
}

// decodeCompiledFileSet parses a version-stripped FlatBuffer payload and reconstructs the
// compiled file set it describes.
//
// The generated FlatBuffer accessors trust their offsets, so a truncated or bit-flipped
// payload can index past the buffer and panic. The deferred recover converts such a panic
// into errCorruptBytecodePayload so callers can discard the cache instead of unwinding
// into the host.
//
// Takes payload ([]byte) which is the raw FlatBuffer bytes after Unpack.
// Takes registry (*symtab.SymbolRegistry) which provides symbol and type lookups for
// runtime reconstruction.
//
// Returns *program.CompiledFileSet which is the reconstructed compiled file set.
// Returns error when the root cannot be parsed, reconstruction fails, or the accessors
// panic on a malformed payload.
func decodeCompiledFileSet(ctx context.Context, payload []byte, registry *symtab.SymbolRegistry) (fileSet *program.CompiledFileSet, err error) {
	if len(payload) > schema.MaximumBytecodePayloadBytes {
		return nil, schema.ErrBytecodeTooLarge
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			fileSet = nil
			err = fmt.Errorf("%w: %v", errCorruptBytecodePayload, recovered)
		}
	}()
	fbFileSet := schemagen.GetRootAsCompiledFileSet(payload, 0)
	if fbFileSet == nil {
		return nil, fmt.Errorf("%w: cannot parse FlatBuffer root", errCorruptBytecodePayload)
	}
	if err := validateFunctionExpansion(ctx, fbFileSet, len(payload)); err != nil {
		return nil, err
	}
	return unpackCompiledFileSet(ctx, fbFileSet, registry, len(payload))
}

// unpackCompiledFileSet reconstructs a CompiledFileSet from its FlatBuffer
// representation.
//
// Takes fbFileSet (*schemagen.CompiledFileSet) which is the serialised FlatBuffer file
// set.
// Takes registry (*symtab.SymbolRegistry) which provides symbol and type lookups for
// runtime reconstruction.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound every declared vector length.
//
// Returns *program.CompiledFileSet which is the reconstructed compiled file set.
// Returns error when unpacking any function fails or a declared vector length exceeds the
// payload size.
func unpackCompiledFileSet(ctx context.Context, fbFileSet *schemagen.CompiledFileSet, registry *symtab.SymbolRegistry, payloadLen int) (*program.CompiledFileSet, error) {
	values := bytecodeValueBudget{remaining: maximumDecodedValueBytes}
	cache := newDescriptorDecodeCache()
	var root *program.CompiledFunction
	if fbRoot := fbFileSet.Root(nil); fbRoot != nil {
		var err error
		root, err = unpackCompiledFunction(ctx, fbRoot, registry, 0, payloadLen, &values, cache)
		if err != nil {
			return nil, fmt.Errorf("unpacking root function: %w", err)
		}
	}

	var variableInitFunction *program.CompiledFunction
	if fbVarInit := fbFileSet.VariableInitFunction(nil); fbVarInit != nil {
		var err error
		variableInitFunction, err = unpackCompiledFunction(ctx, fbVarInit, registry, 0, payloadLen, &values, cache)
		if err != nil {
			return nil, fmt.Errorf("unpacking variable init function: %w", err)
		}
	}

	entrypoints, err := unpackEntrypoints(fbFileSet, payloadLen)
	if err != nil {
		return nil, err
	}

	initCount, err := boundedCount(fbFileSet.InitialisationFunctionsLength(), payloadLen, "initialisation functions")
	if err != nil {
		return nil, err
	}
	initFunctionIndices := make([]uint16, initCount)
	for i := range initCount {
		initFunctionIndices[i] = fbFileSet.InitialisationFunctions(i)
	}

	slotAllocation := unpackSlotAllocation(fbFileSet)

	packageVariables, err := unpackPackageVariables(fbFileSet, payloadLen, cache)
	if err != nil {
		return nil, err
	}

	return codec.NewCompiledFileSetFromDataWithVars(root, variableInitFunction, entrypoints, initFunctionIndices, slotAllocation, packageVariables), nil
}

// unpackEntrypoints reconstructs the entrypoint name-to-index map from its FlatBuffer
// entries.
//
// Takes fbFileSet (*schemagen.CompiledFileSet) which contains the serialised entrypoint
// entries.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared entry count.
//
// Returns map[string]uint16 which maps entrypoint names to their function indices.
// Returns error when the declared entry count exceeds the payload size.
func unpackEntrypoints(fbFileSet *schemagen.CompiledFileSet, payloadLen int) (map[string]uint16, error) {
	count, err := boundedCount(fbFileSet.EntrypointsLength(), payloadLen, "entrypoints")
	if err != nil {
		return nil, err
	}
	entrypoints := make(map[string]uint16, count)
	var fbEntrypoint schemagen.EntrypointEntry
	for i := range count {
		if fbFileSet.Entrypoints(&fbEntrypoint, i) {
			entrypoints[mem.String(fbEntrypoint.Name())] = fbEntrypoint.FunctionIndex()
		}
	}
	return entrypoints, nil
}

// unpackSlotAllocation reconstructs the per-bank global-store slot reservation counts
// from their FlatBuffer struct. A nil struct (no reservations were serialised) yields the
// zero allocation.
//
// Takes fbFileSet (*schemagen.CompiledFileSet) which contains the serialised slot
// allocation.
//
// Returns program.SlotAllocation with one count per bank.
func unpackSlotAllocation(fbFileSet *schemagen.CompiledFileSet) program.SlotAllocation {
	var slotAllocation program.SlotAllocation
	if alloc := fbFileSet.SlotAllocation(nil); alloc != nil {
		slotAllocation[slotBankInt] = alloc.IntCount()
		slotAllocation[slotBankFloat] = alloc.FloatCount()
		slotAllocation[slotBankString] = alloc.StringCount()
		slotAllocation[slotBankBool] = alloc.BoolCount()
		slotAllocation[slotBankUint] = alloc.UintCount()
		slotAllocation[slotBankComplex] = alloc.ComplexCount()
		slotAllocation[slotBankGeneral] = alloc.GeneralCount()
	}
	return slotAllocation
}

// unpackPackageVariables reconstructs the exported package-variable metadata vector,
// recursively unpacking each entry's optional type descriptor.
//
// Takes fbFileSet (*schemagen.CompiledFileSet) which contains the serialised
// package-variable entries.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared entry count.
// Takes cache (*descriptorDecodeCache) which shares decoded type descriptors across the
// load.
//
// Returns []program.PackageVariableMetadata which holds the reconstructed metadata.
// Returns error when a serialised type descriptor cannot be unpacked, the declared entry
// count exceeds the payload size, or a register kind is out of range.
func unpackPackageVariables(fbFileSet *schemagen.CompiledFileSet, payloadLen int, cache *descriptorDecodeCache) ([]program.PackageVariableMetadata, error) {
	count, err := boundedCount(fbFileSet.PackageVariablesLength(), payloadLen, "package variables")
	if err != nil {
		return nil, err
	}
	packageVariables := make([]program.PackageVariableMetadata, 0, count)
	var fbVar schemagen.PackageVariableEntry
	for i := range count {
		if !fbFileSet.PackageVariables(&fbVar, i) {
			continue
		}
		var typeData *descriptor.TypeDescriptorData
		if td := fbVar.TypeDescriptor(nil); td != nil {
			value, err := unpackTypeDescriptor(td, 0, payloadLen, cache)
			if err != nil {
				return nil, fmt.Errorf("unpacking package-variable type descriptor for %s.%s: %w", mem.String(fbVar.PackagePath()), mem.String(fbVar.Name()), err)
			}
			typeData = &value
		}
		registerKind, err := validatedRegisterKind(int8(fbVar.RegisterKind()), "package variable")
		if err != nil {
			return nil, fmt.Errorf("unpacking package variable %s.%s: %w", mem.String(fbVar.PackagePath()), mem.String(fbVar.Name()), err)
		}
		packageVariables = append(packageVariables, program.PackageVariableMetadata{
			Name:         mem.String(fbVar.Name()),
			PackagePath:  mem.String(fbVar.PackagePath()),
			Type:         typeData,
			RegisterKind: registerKind,
			RelativeSlot: fbVar.RelativeSlot(),
			IsIndirect:   fbVar.IsIndirect(),
		})
	}
	return packageVariables, nil
}

// unpackCompiledFunction reconstructs a CompiledFunction from its FlatBuffer
// representation. All constant pools are read, general constants and types are
// reconstructed via the SymbolRegistry, and child functions are unpacked recursively.
//
// Takes fbFunction (*schemagen.CompiledFunction) which is the serialised FlatBuffer
// function.
// Takes registry (*symtab.SymbolRegistry) which provides symbol and type lookups for
// runtime reconstruction.
// Takes depth (int) which is the current function-nesting recursion depth.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound every declared vector length.
// Takes values (*bytecodeValueBudget) which tracks the remaining general-constant byte
// budget.
// Takes cache (*descriptorDecodeCache) which shares decoded type descriptors across the
// load.
//
// Returns *program.CompiledFunction which is the reconstructed compiled function.
// Returns error when reconstructing general constants, types, or child functions fails, a
// declared vector length exceeds the payload size, or a register kind is out of range.
func unpackCompiledFunction(
	ctx context.Context,
	fbFunction *schemagen.CompiledFunction,
	registry *symtab.SymbolRegistry,
	depth, payloadLen int,
	values *bytecodeValueBudget,
	cache *descriptorDecodeCache,
) (*program.CompiledFunction, error) {
	if depth > maxBytecodeFunctionNestingDepth {
		return nil, fmt.Errorf("%w: function nesting exceeded %d", errBytecodeRecursionDepthExceeded, maxBytecodeFunctionNestingDepth)
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("bytecode unpack cancelled: %w", err)
	}
	data, err := unpackFunctionHeader(fbFunction, registry, payloadLen, cache)
	if err != nil {
		return nil, err
	}
	if err := unpackFunctionRegisters(fbFunction, payloadLen, data); err != nil {
		return nil, err
	}
	if err := unpackFunctionConstantPools(fbFunction, payloadLen, data); err != nil {
		return nil, err
	}
	data.GeneralConstantDescriptors, data.GeneralConstants, err = unpackGeneralConstants(fbFunction, registry, payloadLen, values, cache)
	if err != nil {
		return nil, err
	}
	if err := unpackFunctionTypeTables(fbFunction, registry, payloadLen, cache, data); err != nil {
		return nil, err
	}
	if err := unpackFunctionSiteTables(fbFunction, payloadLen, data); err != nil {
		return nil, err
	}
	unpackChild := func(fbChild *schemagen.CompiledFunction) (*program.CompiledFunction, error) {
		return unpackCompiledFunction(ctx, fbChild, registry, depth+1, payloadLen, values, cache)
	}
	data.Functions, err = unpackChildFunctions(fbFunction, payloadLen, unpackChild)
	if err != nil {
		return nil, err
	}
	if err := unpackFunctionResultAndMethodTables(fbFunction, payloadLen, data); err != nil {
		return nil, err
	}
	if fbVarInit := fbFunction.VariableInitFunction(nil); fbVarInit != nil {
		data.VariableInitFunction, err = unpackChild(fbVarInit)
		if err != nil {
			return nil, fmt.Errorf("unpacking variable init function: %w", err)
		}
	}
	return codec.NewCompiledFunctionFromData(data), nil
}

// unpackFunctionHeader reads a function's name, source file, flags, and signature type,
// and returns the data record that the later unpack steps fill in.
//
// Takes fbFunction (*schemagen.CompiledFunction) which is the serialised FlatBuffer
// function.
// Takes registry (*symtab.SymbolRegistry) which provides type lookups for the signature.
// Takes payloadLen (int) which is the total untrusted payload size in bytes.
// Takes cache (*descriptorDecodeCache) which shares decoded type descriptors across the
// load.
//
// Returns *codec.CompiledFunctionData which holds the header fields, with every other
// field left at its zero value.
// Returns error when the signature type cannot be reconstructed.
func unpackFunctionHeader(fbFunction *schemagen.CompiledFunction, registry *symtab.SymbolRegistry, payloadLen int, cache *descriptorDecodeCache) (*codec.CompiledFunctionData, error) {
	name := mem.String(fbFunction.Name())
	sourceFile := mem.String(fbFunction.SourceFile())
	isVariadic := fbFunction.IsVariadic()
	isPointerReceiver := fbFunction.IsPointerReceiver()
	hasReceiver := fbFunction.HasReceiver()
	hasRecover := fbFunction.HasRecover()

	signatureReflectType, err := unpackSignatureReflectType(fbFunction, registry, payloadLen, cache)
	if err != nil {
		return nil, err
	}
	return &codec.CompiledFunctionData{
		Name:                       name,
		SourceFile:                 sourceFile,
		IsVariadic:                 isVariadic,
		IsPointerReceiver:          isPointerReceiver,
		HasReceiver:                hasReceiver,
		SignatureReflectType:       signatureReflectType,
		HasRecover:                 hasRecover,
		NumRegisters:               [isa.NumRegisterKinds]uint32{},
		ParamKinds:                 nil,
		ParamRegisters:             nil,
		ResultKinds:                nil,
		Body:                       nil,
		BoolConstants:              nil,
		IntConstants:               nil,
		FloatConstants:             nil,
		UintConstants:              nil,
		ComplexConstants:           nil,
		StringConstants:            nil,
		GeneralConstants:           nil,
		GeneralConstantDescriptors: nil,
		TypeTable:                  nil,
		TypeTableDescriptors:       nil,
		TypeTableInterfaceMethods:  nil,
		TypeNames:                  nil,
		CallSites:                  nil,
		UpvalueDescriptors:         nil,
		Functions:                  nil,
		NamedResultLocations:       nil,
		MethodTable:                nil,
		VariableInitFunction:       nil,
		StructLayoutTable:          nil,
	}, nil
}

// unpackFunctionRegisters reads the register counts and the parameter and result register
// metadata into data.
//
// Takes fbFunction (*schemagen.CompiledFunction) which is the serialised FlatBuffer
// function.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound every declared vector length.
// Takes data (*codec.CompiledFunctionData) which receives the register metadata.
//
// Returns error when a declared vector length exceeds the payload size or a register kind
// is out of range.
func unpackFunctionRegisters(fbFunction *schemagen.CompiledFunction, payloadLen int, data *codec.CompiledFunctionData) error {
	registerCountLength, err := boundedCount(fbFunction.RegisterCountsLength(), payloadLen, "register counts")
	if err != nil {
		return err
	}
	for i := range min(registerCountLength, isa.NumRegisterKinds) {
		data.NumRegisters[i] = fbFunction.RegisterCounts(i)
	}

	data.ParamKinds, err = unpackRegisterKinds(fbFunction.ParameterKindsLength(), fbFunction.ParameterKinds, payloadLen, "parameter kinds", "parameter")
	if err != nil {
		return err
	}

	if declared := fbFunction.ParameterRegistersLength(); declared > 0 {
		data.ParamRegisters, err = unpackScalarVector(declared, fbFunction.ParameterRegisters, payloadLen, "parameter registers")
		if err != nil {
			return err
		}
	}

	data.ResultKinds, err = unpackRegisterKinds(fbFunction.ResultKindsLength(), fbFunction.ResultKinds, payloadLen, "result kinds", "result")
	return err
}

// unpackRegisterKinds reads a vector of register kinds, checking that each one is in
// range.
//
// Takes declared (int) which is the vector length the payload claims.
// Takes kindAt (func(int) schemagen.RegisterKind) which reads the entry at an index.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared length.
// Takes label (string) which names the vector in the length error.
// Takes role (string) which names the register role in the range error.
//
// Returns []codec.RegisterKindValue which holds the validated kinds.
// Returns error when the declared length exceeds the payload size or a kind is out of
// range.
func unpackRegisterKinds(declared int, kindAt func(int) schemagen.RegisterKind, payloadLen int, label, role string) ([]codec.RegisterKindValue, error) {
	count, err := boundedCount(declared, payloadLen, label)
	if err != nil {
		return nil, err
	}
	kinds := make([]codec.RegisterKindValue, count)
	for i := range count {
		kind, err := validatedRegisterKind(int8(kindAt(i)), role)
		if err != nil {
			return nil, err
		}
		kinds[i] = codec.MakeRegisterKind(kind)
	}
	return kinds, nil
}

// unpackScalarVector reads a FlatBuffer vector of scalar entries into a new slice of the
// bounded length.
//
// Takes declared (int) which is the vector length the payload claims.
// Takes at (func(int) T) which reads the entry at an index.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared length.
// Takes label (string) which names the vector in the length error.
//
// Returns []T which holds the entries, empty but not nil when the vector is empty.
// Returns error when the declared length exceeds the payload size.
func unpackScalarVector[T any](declared int, at func(int) T, payloadLen int, label string) ([]T, error) {
	count, err := boundedCount(declared, payloadLen, label)
	if err != nil {
		return nil, err
	}
	entries := make([]T, count)
	for i := range count {
		entries[i] = at(i)
	}
	return entries, nil
}

// unpackFunctionConstantPools reads the function body and the bool, int, float, uint,
// complex, and string constant pools into data.
//
// Takes fbFunction (*schemagen.CompiledFunction) which is the serialised FlatBuffer
// function.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound every declared vector length.
// Takes data (*codec.CompiledFunctionData) which receives the body and the pools.
//
// Returns error when a declared vector length exceeds the payload size.
func unpackFunctionConstantPools(fbFunction *schemagen.CompiledFunction, payloadLen int, data *codec.CompiledFunctionData) error {
	var err error
	if data.Body, err = unpackFunctionBody(fbFunction, payloadLen); err != nil {
		return err
	}
	if data.BoolConstants, err = unpackScalarVector(fbFunction.BoolConstantsLength(), fbFunction.BoolConstants, payloadLen, "bool constants"); err != nil {
		return err
	}
	if data.IntConstants, err = unpackScalarVector(fbFunction.IntConstantsLength(), fbFunction.IntConstants, payloadLen, "int constants"); err != nil {
		return err
	}
	if data.FloatConstants, err = unpackScalarVector(fbFunction.FloatConstantsLength(), fbFunction.FloatConstants, payloadLen, "float constants"); err != nil {
		return err
	}
	if data.UintConstants, err = unpackScalarVector(fbFunction.UintConstantsLength(), fbFunction.UintConstants, payloadLen, "uint constants"); err != nil {
		return err
	}
	if data.ComplexConstants, err = unpackComplexConstants(fbFunction, payloadLen); err != nil {
		return err
	}
	stringAt := func(i int) string { return mem.String(fbFunction.StringConstants(i)) }
	data.StringConstants, err = unpackScalarVector(fbFunction.StringConstantsLength(), stringAt, payloadLen, "string constants")
	return err
}

// unpackFunctionBody reads the function's bytecode instructions.
//
// Takes fbFunction (*schemagen.CompiledFunction) which is the serialised FlatBuffer
// function.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared body length.
//
// Returns []codec.InstructionValue which holds the instructions.
// Returns error when the declared body length exceeds the payload size.
func unpackFunctionBody(fbFunction *schemagen.CompiledFunction, payloadLen int) ([]codec.InstructionValue, error) {
	bodyCount, err := boundedCount(fbFunction.BodyLength(), payloadLen, "function body")
	if err != nil {
		return nil, err
	}
	body := make([]codec.InstructionValue, bodyCount)
	var fbInstruction schemagen.Instruction
	for i := range bodyCount {
		if fbFunction.Body(&fbInstruction, i) {
			body[i] = isa.NewInstruction(isa.Opcode(fbInstruction.Opcode()), fbInstruction.A(), fbInstruction.B(), fbInstruction.C())
		}
	}
	return body, nil
}

// unpackComplexConstants reads the complex128 constant pool.
//
// Takes fbFunction (*schemagen.CompiledFunction) which is the serialised FlatBuffer
// function.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared pool length.
//
// Returns []complex128 which holds the constants.
// Returns error when the declared pool length exceeds the payload size.
func unpackComplexConstants(fbFunction *schemagen.CompiledFunction, payloadLen int) ([]complex128, error) {
	complexConstantCount, err := boundedCount(fbFunction.ComplexConstantsLength(), payloadLen, "complex constants")
	if err != nil {
		return nil, err
	}
	complexConstants := make([]complex128, complexConstantCount)
	var fbComplexValue schemagen.ComplexValue
	for i := range complexConstantCount {
		if fbFunction.ComplexConstants(&fbComplexValue, i) {
			complexConstants[i] = complex(fbComplexValue.Real(), fbComplexValue.Imaginary())
		}
	}
	return complexConstants, nil
}

// unpackFunctionTypeTables reads the type table, the interface method sets, and the type
// names into data.
//
// Takes fbFunction (*schemagen.CompiledFunction) which is the serialised FlatBuffer
// function.
// Takes registry (*symtab.SymbolRegistry) which provides type lookups for runtime
// reconstruction.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound every declared vector length.
// Takes cache (*descriptorDecodeCache) which shares decoded type descriptors across the
// load.
// Takes data (*codec.CompiledFunctionData) which receives the tables.
//
// Returns error when a type cannot be reconstructed or a declared vector length exceeds
// the payload size.
func unpackFunctionTypeTables(fbFunction *schemagen.CompiledFunction, registry *symtab.SymbolRegistry, payloadLen int, cache *descriptorDecodeCache, data *codec.CompiledFunctionData) error {
	var err error
	data.TypeTableDescriptors, data.TypeTable, err = unpackTypeTable(fbFunction, registry, payloadLen, cache)
	if err != nil {
		return err
	}
	if data.TypeTableInterfaceMethods, err = unpackInterfaceMethodSets(fbFunction, payloadLen); err != nil {
		return err
	}
	data.TypeNames, err = unpackTypeNames(fbFunction, registry, payloadLen, cache)
	return err
}

// unpackFunctionSiteTables reads the call sites, the upvalue descriptors, and the struct
// layout table into data.
//
// Takes fbFunction (*schemagen.CompiledFunction) which is the serialised FlatBuffer
// function.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound every declared vector length.
// Takes data (*codec.CompiledFunctionData) which receives the tables.
//
// Returns error when a declared vector length exceeds the payload size or an entry is
// malformed.
func unpackFunctionSiteTables(fbFunction *schemagen.CompiledFunction, payloadLen int, data *codec.CompiledFunctionData) error {
	var err error
	if data.CallSites, err = unpackCallSites(fbFunction, payloadLen); err != nil {
		return err
	}
	if data.UpvalueDescriptors, err = unpackUpvalueDescriptors(fbFunction, payloadLen); err != nil {
		return err
	}
	data.StructLayoutTable, err = unpackStructLayoutTable(fbFunction, payloadLen)
	return err
}

// unpackChildFunctions reads the function's nested child functions.
//
// Takes fbFunction (*schemagen.CompiledFunction) which is the serialised FlatBuffer
// function.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared child count.
// Takes unpackChild (func(*schemagen.CompiledFunction) (*program.CompiledFunction,
// error)) which unpacks one child one nesting level deeper.
//
// Returns []*program.CompiledFunction which holds the children, with a nil entry for any
// slot the payload leaves empty.
// Returns error when the declared child count exceeds the payload size or a child fails
// to unpack.
func unpackChildFunctions(
	fbFunction *schemagen.CompiledFunction,
	payloadLen int,
	unpackChild func(*schemagen.CompiledFunction) (*program.CompiledFunction, error),
) ([]*program.CompiledFunction, error) {
	functionCount, err := boundedCount(fbFunction.FunctionsLength(), payloadLen, "child functions")
	if err != nil {
		return nil, err
	}
	functions := make([]*program.CompiledFunction, functionCount)
	var fbChildFunction schemagen.CompiledFunction
	for i := range functionCount {
		if fbFunction.Functions(&fbChildFunction, i) {
			functions[i], err = unpackChild(&fbChildFunction)
			if err != nil {
				return nil, fmt.Errorf("unpacking child function %d: %w", i, err)
			}
		}
	}
	return functions, nil
}

// unpackFunctionResultAndMethodTables reads the named result locations and the method
// table into data.
//
// Takes fbFunction (*schemagen.CompiledFunction) which is the serialised FlatBuffer
// function.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound every declared vector length.
// Takes data (*codec.CompiledFunctionData) which receives the tables.
//
// Returns error when a declared vector length exceeds the payload size.
func unpackFunctionResultAndMethodTables(fbFunction *schemagen.CompiledFunction, payloadLen int, data *codec.CompiledFunctionData) error {
	var err error
	data.NamedResultLocations, err = unpackVarLocations(fbFunction.NamedResultLocationsLength(), func(location *schemagen.VarLocation, index int) bool {
		return fbFunction.NamedResultLocations(location, index)
	}, payloadLen)
	if err != nil {
		return fmt.Errorf("unpacking named result locations: %w", err)
	}

	methodTableCount, err := boundedCount(fbFunction.MethodTableLength(), payloadLen, "method table")
	if err != nil {
		return err
	}
	data.MethodTable = make(map[string]uint16, methodTableCount)
	var fbMethodEntry schemagen.MethodTableEntry
	for i := range methodTableCount {
		if fbFunction.MethodTable(&fbMethodEntry, i) {
			data.MethodTable[mem.String(fbMethodEntry.Name())] = fbMethodEntry.FunctionIndex()
		}
	}
	return nil
}

// unpackInterfaceMethodSets reconstructs the per-type-table-entry interface method-name
// sets from their FlatBuffer representation.
//
// Takes fbFunction (*schemagen.CompiledFunction) which contains the serialised sets.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared set and method counts.
//
// Returns [][]string which holds the per-type-table-entry method name lists aligned with
// the type table, or nil when the function had no non-empty interface entries.
// Returns error when a declared count exceeds the payload size.
func unpackInterfaceMethodSets(fbFunction *schemagen.CompiledFunction, payloadLen int) ([][]string, error) {
	count, err := boundedCount(fbFunction.TypeTableInterfaceMethodsLength(), payloadLen, "interface method sets")
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	sets := make([][]string, count)
	var entry schemagen.InterfaceMethodSet
	for i := range count {
		if !fbFunction.TypeTableInterfaceMethods(&entry, i) {
			continue
		}
		methodCount, err := boundedCount(entry.MethodsLength(), payloadLen, "interface methods")
		if err != nil {
			return nil, err
		}
		if methodCount == 0 {
			continue
		}
		methods := make([]string, methodCount)
		for j := range methodCount {
			methods[j] = string(entry.Methods(j))
		}
		sets[i] = methods
	}
	return sets, nil
}

// unpackStructLayoutTable reconstructs the struct-field layout table from FlatBuffer-side
// StructFieldLayout entries. Each entry round trips byte-for-byte: offset, type index,
// path[0..3] + length, kind, register kind, flags.
//
// Takes fbFunction (*schemagen.CompiledFunction) which is the FlatBuffer-side compiled
// function.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared entry count.
//
// Returns []codec.StructFieldLayoutData which holds one entry per table slot, or empty
// when the function had no fast-path field accesses.
// Returns error when the declared entry count exceeds the payload size.
func unpackStructLayoutTable(fbFunction *schemagen.CompiledFunction, payloadLen int) ([]codec.StructFieldLayoutData, error) {
	count, err := boundedCount(fbFunction.StructLayoutTableLength(), payloadLen, "struct field layout")
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	layouts := make([]codec.StructFieldLayoutData, count)
	var entry schemagen.StructFieldLayout
	for i := range count {
		if !fbFunction.StructLayoutTable(&entry, i) {
			continue
		}
		layouts[i] = codec.StructFieldLayoutData{
			Offset:         entry.Offset(),
			TypeIndex:      entry.TypeIndex(),
			Path:           [structFieldLayoutMaxPathDepth]uint8{entry.Path0(), entry.Path1(), entry.Path2(), entry.Path3()},
			PathLength:     entry.PathLength(),
			Kind:           entry.Kind(),
			RegisterKind:   entry.RegisterKind(),
			Flags:          entry.Flags(),
			FieldTypeIndex: entry.FieldTypeIndex(),
		}
	}
	return layouts, nil
}

// unpackGeneralConstants reconstructs general constants from their FlatBuffer
// descriptors. Each descriptor is converted back to an internal descriptor and its
// runtime reflect.Value is reconstructed via the SymbolRegistry.
//
// Takes fbFunction (*schemagen.CompiledFunction) which contains the serialised general
// constant descriptors.
// Takes registry (*symtab.SymbolRegistry) which provides symbol lookups for runtime value
// reconstruction.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared descriptor count.
// Takes budget (*bytecodeValueBudget) which tracks the remaining general-constant byte
// budget.
// Takes cache (*descriptorDecodeCache) which shares decoded type descriptors across the
// load.
//
// Returns []codec.GeneralConstantDescriptorInternal which holds the reconstructed
// internal descriptors.
// Returns []reflect.Value which holds the reconstructed runtime values.
// Returns error when any constant cannot be reconstructed or the declared count exceeds
// the payload size.
func unpackGeneralConstants(
	fbFunction *schemagen.CompiledFunction,
	registry *symtab.SymbolRegistry,
	payloadLen int,
	budget *bytecodeValueBudget,
	cache *descriptorDecodeCache,
) ([]codec.GeneralConstantDescriptorInternal, []reflect.Value, error) {
	count, err := boundedCount(fbFunction.GeneralConstantDescriptorsLength(), payloadLen, "general constants")
	if err != nil {
		return nil, nil, err
	}
	if count == 0 {
		return nil, nil, nil
	}
	descriptors := make([]codec.GeneralConstantDescriptorInternal, count)
	values := make([]reflect.Value, count)
	var fbDescriptor schemagen.GeneralConstantDescriptor
	for i := range count {
		if !fbFunction.GeneralConstantDescriptors(&fbDescriptor, i) {
			continue
		}
		data, err := unpackGeneralConstantDescriptor(&fbDescriptor, payloadLen, cache)
		if err != nil {
			return nil, nil, fmt.Errorf("unpacking general constant descriptor %d: %w", i, err)
		}
		descriptors[i] = codec.ImportGeneralConstantDescriptor(data)
		value, err := budget.reconstruct(data, registry)
		if err != nil {
			return nil, nil, fmt.Errorf("reconstructing general constant %d: %w", i, err)
		}
		values[i] = value
	}
	return descriptors, values, nil
}

// unpackTypeTable reconstructs the type table from its FlatBuffer descriptors. Each
// descriptor is converted back to an internal descriptor and its runtime reflect.Type is
// reconstructed via the SymbolRegistry.
//
// Takes fbFunction (*schemagen.CompiledFunction) which contains the serialised type table
// descriptors.
// Takes registry (*symtab.SymbolRegistry) which provides named type lookups for runtime
// reconstruction.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared descriptor count.
// Takes cache (*descriptorDecodeCache) which shares decoded type descriptors across the
// load.
//
// Returns []codec.TypeDescriptorInternal which holds the reconstructed internal
// descriptors.
// Returns []reflect.Type which holds the reconstructed runtime types.
// Returns error when any type cannot be reconstructed or the declared count exceeds the
// payload size.
func unpackTypeTable(
	fbFunction *schemagen.CompiledFunction,
	registry *symtab.SymbolRegistry,
	payloadLen int,
	cache *descriptorDecodeCache,
) ([]codec.TypeDescriptorInternal, []reflect.Type, error) {
	count, err := boundedCount(fbFunction.TypeTableDescriptorsLength(), payloadLen, "type table")
	if err != nil {
		return nil, nil, err
	}
	if count == 0 {
		return nil, nil, nil
	}
	descriptors := make([]codec.TypeDescriptorInternal, count)
	types := make([]reflect.Type, count)
	var fbDescriptor schemagen.TypeDescriptor
	for i := range count {
		if !fbFunction.TypeTableDescriptors(&fbDescriptor, i) {
			continue
		}
		data, err := unpackTypeDescriptor(&fbDescriptor, 0, payloadLen, cache)
		if err != nil {
			return nil, nil, fmt.Errorf("unpacking type descriptor %d: %w", i, err)
		}
		descriptors[i] = codec.ImportTypeDescriptor(data)
		reconstructedType, err := reflectTypeForTable(cache, fbDescriptor.Table().Pos, data, registry)
		if err != nil {
			return nil, nil, fmt.Errorf("reconstructing type %d: %w", i, err)
		}
		types[i] = reconstructedType
	}
	return descriptors, types, nil
}

// unpackSignatureReflectType rebuilds a function's own signature type from its
// descriptor.
//
// A bundle written before the field existed, and an erased generic body, both carry no
// descriptor; the function then keeps the nil signature it had before the field was
// added.
//
// Takes fbFunction (*schemagen.CompiledFunction) which is the function being unpacked.
// Takes registry (*symtab.SymbolRegistry) which resolves registered types.
// Takes payloadLen (int) which bounds the descriptor walk.
// Takes cache (*descriptorDecodeCache) which shares descriptors across the load.
//
// Returns reflect.Type which is the signature type, or nil when unrecorded.
// Returns error when the recorded descriptor cannot be rebuilt.
func unpackSignatureReflectType(fbFunction *schemagen.CompiledFunction, registry *symtab.SymbolRegistry, payloadLen int, cache *descriptorDecodeCache) (reflect.Type, error) {
	var fbDescriptor schemagen.TypeDescriptor
	if fbFunction.SignatureTypeDescriptor(&fbDescriptor) == nil {
		return nil, nil
	}
	data, err := unpackTypeDescriptor(&fbDescriptor, 0, payloadLen, cache)
	if err != nil {
		return nil, fmt.Errorf("unpacking signature descriptor: %w", err)
	}
	signature, err := reflectTypeForTable(cache, fbDescriptor.Table().Pos, data, registry)
	if err != nil {
		return nil, fmt.Errorf("reconstructing signature type: %w", err)
	}
	return signature, nil
}

// unpackTypeNames reconstructs the type names map from FlatBuffer entries. Each entry's
// type descriptor is resolved to a reflect.Type and paired with its string name.
//
// Takes fbFunction (*schemagen.CompiledFunction) which contains the serialised type name
// entries.
// Takes registry (*symtab.SymbolRegistry) which provides named type lookups for runtime
// reconstruction.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared entry count.
// Takes cache (*descriptorDecodeCache) which shares decoded type descriptors across the
// load.
//
// Returns map[reflect.Type]string which maps runtime types to their string names.
// Returns error when a serialised type descriptor exceeds the recursion depth bound or
// the declared entry count exceeds the payload size.
func unpackTypeNames(
	fbFunction *schemagen.CompiledFunction,
	registry *symtab.SymbolRegistry,
	payloadLen int,
	cache *descriptorDecodeCache,
) (map[reflect.Type]string, error) {
	count, err := boundedCount(fbFunction.TypeNamesLength(), payloadLen, "type names")
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	result := make(map[reflect.Type]string, count)
	var fbEntry schemagen.TypeNameEntry
	for i := range count {
		if !fbFunction.TypeNames(&fbEntry, i) {
			continue
		}
		name := mem.String(fbEntry.Name())
		fbTypeDescriptor := fbEntry.TypeDescriptor(nil)
		if fbTypeDescriptor == nil {
			continue
		}
		data, err := unpackTypeDescriptor(fbTypeDescriptor, 0, payloadLen, cache)
		if err != nil {
			return nil, fmt.Errorf("unpacking type name descriptor %d: %w", i, err)
		}
		reconstructedType, err := codec.DescriptorToReflectType(data, registry)
		if err == nil {
			result[reconstructedType] = name
		}
	}
	return result, nil
}

// unpackCallSites reconstructs call sites from their FlatBuffer representation. Only
// static metadata is deserialised; runtime caches are initialised to their zero values.
//
// Takes fbFunction (*schemagen.CompiledFunction) which contains the serialised call
// sites.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared call-site and argument counts.
//
// Returns []codec.CallSiteInternal which holds the reconstructed call sites.
// Returns error when a declared count exceeds the payload size or a register kind is out
// of range.
func unpackCallSites(fbFunction *schemagen.CompiledFunction, payloadLen int) ([]codec.CallSiteInternal, error) {
	count, err := boundedCount(fbFunction.CallSitesLength(), payloadLen, "call sites")
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	sites := make([]codec.CallSiteInternal, count)
	var fbCallSite schemagen.CallSite
	for i := range count {
		if !fbFunction.CallSites(&fbCallSite, i) {
			continue
		}
		arguments, err := unpackVarLocationData(fbCallSite.ArgumentsLength(), func(location *schemagen.VarLocation, index int) bool {
			return fbCallSite.Arguments(location, index)
		}, payloadLen)
		if err != nil {
			return nil, fmt.Errorf("unpacking call site %d arguments: %w", i, err)
		}
		returns, err := unpackVarLocationData(fbCallSite.ReturnsLength(), func(location *schemagen.VarLocation, index int) bool {
			return fbCallSite.Returns(location, index)
		}, payloadLen)
		if err != nil {
			return nil, fmt.Errorf("unpacking call site %d returns: %w", i, err)
		}
		data := codec.CallSiteData{
			FunctionIndex:          fbCallSite.FunctionIndex(),
			ClosureRegister:        fbCallSite.ClosureRegister(),
			NativeRegister:         fbCallSite.NativeRegister(),
			IsClosure:              fbCallSite.IsClosure(),
			IsNative:               fbCallSite.IsNative(),
			IsMethod:               fbCallSite.IsMethod(),
			MethodReceiverRegister: fbCallSite.MethodReceiverRegister(),
			IsEllipsisSpread:       fbCallSite.IsEllipsisSpread(),
			BlocksHostGoroutine:    fbCallSite.BlocksHostGoroutine(),
			Arguments:              arguments,
			Returns:                returns,
		}
		sites[i] = codec.MakeCallSite(data)
	}
	return sites, nil
}

// unpackUpvalueDescriptors reconstructs upvalue descriptors from their FlatBuffer
// representation.
//
// Takes fbFunction (*schemagen.CompiledFunction) which contains the serialised upvalue
// descriptors.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared descriptor count.
//
// Returns []program.UpvalueDescriptor which holds the reconstructed upvalue descriptors.
// Returns error when the declared count exceeds the payload size or a register kind is
// out of range.
func unpackUpvalueDescriptors(fbFunction *schemagen.CompiledFunction, payloadLen int) ([]program.UpvalueDescriptor, error) {
	count, err := boundedCount(fbFunction.UpvalueDescriptorsLength(), payloadLen, "upvalue descriptors")
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	descriptors := make([]program.UpvalueDescriptor, count)
	var fbDescriptor schemagen.UpvalueDescriptor
	for i := range count {
		if !fbFunction.UpvalueDescriptors(&fbDescriptor, i) {
			continue
		}
		kind, err := validatedRegisterKind(int8(fbDescriptor.Kind()), "upvalue")
		if err != nil {
			return nil, err
		}
		originalKind, err := validatedRegisterKind(int8(fbDescriptor.OriginalKind()), "upvalue original")
		if err != nil {
			return nil, err
		}
		descriptors[i] = codec.MakeUpvalueDescriptor(codec.UpvalueDescriptorData{
			Index:        fbDescriptor.Index(),
			Kind:         kind,
			OriginalKind: originalKind,
			IsLocal:      fbDescriptor.IsLocal(),
			IsIndirect:   fbDescriptor.IsIndirect(),
		})
	}
	return descriptors, nil
}

// unpackVarLocations reconstructs variable locations as internal varLocation values from
// a FlatBuffer vector accessed via the getter function.
//
// Takes length (int) which is the number of variable locations in the vector.
// Takes getter (func) which retrieves each VarLocation by index from the FlatBuffer.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared length.
//
// Returns []codec.VarLocationInternal which holds the reconstructed variable locations.
// Returns error when the declared length exceeds the payload size or a register kind is
// out of range.
func unpackVarLocations(length int, getter func(*schemagen.VarLocation, int) bool, payloadLen int) ([]codec.VarLocationInternal, error) {
	count, err := boundedCount(length, payloadLen, "variable locations")
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	locations := make([]codec.VarLocationInternal, count)
	var fbLocation schemagen.VarLocation
	for i := range count {
		if !getter(&fbLocation, i) {
			continue
		}
		kind, err := validatedRegisterKind(int8(fbLocation.Kind()), "variable location")
		if err != nil {
			return nil, err
		}
		originalKind, err := validatedRegisterKind(int8(fbLocation.OriginalKind()), "variable location original")
		if err != nil {
			return nil, err
		}
		locations[i] = codec.MakeVarLocation(codec.VarLocationData{
			UpvalueIndex: fbLocation.UpvalueIndex(),
			Register:     fbLocation.Register(),
			Kind:         kind,
			IsUpvalue:    fbLocation.IsUpvalue(),
			IsIndirect:   fbLocation.IsIndirect(),
			OriginalKind: originalKind,
			IsSpilled:    fbLocation.IsSpilled(),
			SpillSlot:    fbLocation.SpillSlot(),
		})
	}
	return locations, nil
}

// unpackVarLocationData reconstructs variable locations as serialisation-safe
// VarLocationData values from a FlatBuffer vector accessed via the getter function.
//
// Takes length (int) which is the number of variable locations in the vector.
// Takes getter (func) which retrieves each VarLocation by index from the FlatBuffer.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared length.
//
// Returns []codec.VarLocationData which holds the reconstructed variable location data.
// Returns error when the declared length exceeds the payload size or a register kind is
// out of range.
func unpackVarLocationData(length int, getter func(*schemagen.VarLocation, int) bool, payloadLen int) ([]codec.VarLocationData, error) {
	count, err := boundedCount(length, payloadLen, "variable location data")
	if err != nil {
		return nil, err
	}
	if count == 0 {
		return nil, nil
	}
	locations := make([]codec.VarLocationData, count)
	var fbLocation schemagen.VarLocation
	for i := range count {
		if !getter(&fbLocation, i) {
			continue
		}
		kind, err := validatedRegisterKind(int8(fbLocation.Kind()), "variable location data")
		if err != nil {
			return nil, err
		}
		originalKind, err := validatedRegisterKind(int8(fbLocation.OriginalKind()), "variable location data original")
		if err != nil {
			return nil, err
		}
		locations[i] = codec.VarLocationData{
			UpvalueIndex: fbLocation.UpvalueIndex(),
			Register:     fbLocation.Register(),
			Kind:         kind,
			IsUpvalue:    fbLocation.IsUpvalue(),
			IsIndirect:   fbLocation.IsIndirect(),
			OriginalKind: originalKind,
			IsSpilled:    fbLocation.IsSpilled(),
			SpillSlot:    fbLocation.SpillSlot(),
		}
	}
	return locations, nil
}

// unpackGeneralConstantDescriptor reconstructs a general constant descriptor from its
// FlatBuffer representation.
//
// Takes fbDescriptor (*schemagen.GeneralConstantDescriptor) which is the serialised
// FlatBuffer descriptor.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the embedded type descriptor's vector lengths.
// Takes cache (*descriptorDecodeCache) which shares decoded type descriptors across the
// load.
//
// Returns codec.GeneralConstantDescriptorData which holds the reconstructed descriptor
// data.
// Returns error when the embedded type descriptor exceeds the recursion depth bound or
// declares a vector length larger than the payload size.
func unpackGeneralConstantDescriptor(fbDescriptor *schemagen.GeneralConstantDescriptor, payloadLen int, cache *descriptorDecodeCache) (codec.GeneralConstantDescriptorData, error) {
	data := codec.GeneralConstantDescriptorData{
		Kind:        safeconv.MustInt8ToUint8(int8(fbDescriptor.Kind())),
		PackagePath: mem.String(fbDescriptor.PackagePath()),

		TypeDescriptor: descriptor.TypeDescriptorData{},
		SymbolName:     mem.String(fbDescriptor.SymbolName()),
	}
	if fbTypeDescriptor := fbDescriptor.TypeDescriptor(nil); fbTypeDescriptor != nil {
		typeDescriptor, err := unpackTypeDescriptor(fbTypeDescriptor, 0, payloadLen, cache)
		if err != nil {
			return codec.GeneralConstantDescriptorData{}, err
		}
		data.TypeDescriptor = typeDescriptor
	}
	return data, nil
}

// unpackTypeDescriptor recursively reconstructs a type descriptor from FlatBuffer form.
//
// Recursive fields (elem, key, value, fields, params, results) are unpacked depth-first.
// The depth parameter caps recursion to defend against tampered or corrupted on-disk
// payloads.
//
// Takes fbDescriptor (*schemagen.TypeDescriptor) which is the serialised FlatBuffer type
// descriptor.
// Takes depth (int) which is the current recursion depth.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the declared field, parameter, and result counts.
// Takes cache (*descriptorDecodeCache) which shares decoded type descriptors across the
// load.
//
// Returns descriptor.TypeDescriptorData which holds the reconstructed type descriptor
// data.
// Returns error when the recursion depth bound is exceeded or a declared count exceeds
// the payload size.
func unpackTypeDescriptor(fbDescriptor *schemagen.TypeDescriptor, depth, payloadLen int, cache *descriptorDecodeCache) (descriptor.TypeDescriptorData, error) {
	decoded, err := unpackTypeDescriptorShared(fbDescriptor, depth, payloadLen, cache)
	if err != nil {
		return descriptor.TypeDescriptorData{}, err
	}
	return *decoded, nil
}

// unpackTypeDescriptorShared is unpackTypeDescriptor returning the shared descriptor for
// a table, decoding it only the first time the table is reached.
//
// The packer writes an identical subtree once and references it from every parent.
// Handing back the same pointer keeps the decode linear in the number of distinct tables
// and gives the reconstruction downstream a graph it can memoise on, instead of
// re-expanding the subtree once per path.
//
// Takes fbDescriptor (*schemagen.TypeDescriptor) which is the table to decode.
// Takes depth (int) which is the current nesting depth.
// Takes payloadLen (int) which bounds the counts read from the payload.
// Takes cache (*descriptorDecodeCache) which holds this load's decoded tables; may be
// nil.
//
// Returns *TypeDescriptorData which is the shared descriptor for the table.
// Returns error when the payload is corrupt or nests too deeply.
func unpackTypeDescriptorShared(fbDescriptor *schemagen.TypeDescriptor, depth, payloadLen int, cache *descriptorDecodeCache) (*descriptor.TypeDescriptorData, error) {
	if depth > maxBytecodeTypeDescriptorDepth {
		return nil, fmt.Errorf("%w: type descriptor depth exceeded %d", errBytecodeRecursionDepthExceeded, maxBytecodeTypeDescriptorDepth)
	}
	tableOffset := fbDescriptor.Table().Pos
	if shared, ok := cache.lookupDecoded(tableOffset); ok {
		return shared, nil
	}
	data := descriptor.TypeDescriptorData{
		Kind:        safeconv.MustInt8ToUint8(int8(fbDescriptor.Kind())),
		PackagePath: mem.String(fbDescriptor.PackagePath()),
		Name:        mem.String(fbDescriptor.Name()),
		BasicKind:   fbDescriptor.BasicKind(),
		Length:      fbDescriptor.Length(),
		Dir:         fbDescriptor.Direction(),
		IsVariadic:  fbDescriptor.IsVariadic(),
	}
	var err error
	if data.Elem, err = unpackChildTypeDescriptor(fbDescriptor.Element(nil), depth, payloadLen, cache); err != nil {
		return nil, err
	}
	if data.Key, err = unpackChildTypeDescriptor(fbDescriptor.Key(nil), depth, payloadLen, cache); err != nil {
		return nil, err
	}
	if data.Value, err = unpackChildTypeDescriptor(fbDescriptor.Value(nil), depth, payloadLen, cache); err != nil {
		return nil, err
	}
	if data.Fields, err = unpackTypeDescriptorFields(fbDescriptor, depth, payloadLen, cache); err != nil {
		return nil, err
	}
	data.Params, err = unpackTypeDescriptorList(fbDescriptor.ParamsLength(), fbDescriptor.Params, "type descriptor params", depth, payloadLen, cache)
	if err != nil {
		return nil, err
	}
	data.Results, err = unpackTypeDescriptorList(fbDescriptor.ResultsLength(), fbDescriptor.Results, "type descriptor results", depth, payloadLen, cache)
	if err != nil {
		return nil, err
	}
	cache.rememberDecoded(tableOffset, &data)
	return &data, nil
}

// unpackChildTypeDescriptor decodes an optional element, key, or value descriptor one
// level below its parent.
//
// Takes fbChild (*schemagen.TypeDescriptor) which is the child table, or nil when the
// parent has none.
// Takes depth (int) which is the parent's nesting depth.
// Takes payloadLen (int) which bounds the counts read from the payload.
// Takes cache (*descriptorDecodeCache) which holds this load's decoded tables; may be
// nil.
//
// Returns *descriptor.TypeDescriptorData which is the shared child descriptor, or nil
// when fbChild is nil.
// Returns error when the payload is corrupt or nests too deeply.
func unpackChildTypeDescriptor(fbChild *schemagen.TypeDescriptor, depth, payloadLen int, cache *descriptorDecodeCache) (*descriptor.TypeDescriptorData, error) {
	if fbChild == nil {
		return nil, nil
	}
	return unpackTypeDescriptorShared(fbChild, depth+1, payloadLen, cache)
}

// unpackTypeDescriptorFields decodes the struct fields of a type descriptor.
//
// Takes fbDescriptor (*schemagen.TypeDescriptor) which is the table that owns the fields.
// Takes depth (int) which is the descriptor's nesting depth.
// Takes payloadLen (int) which bounds the declared field count.
// Takes cache (*descriptorDecodeCache) which holds this load's decoded tables; may be
// nil.
//
// Returns []descriptor.TypeDescriptorFieldData which holds the fields, or nil when the
// descriptor declares none.
// Returns error when the declared count exceeds the payload size or a field type fails to
// decode.
func unpackTypeDescriptorFields(fbDescriptor *schemagen.TypeDescriptor, depth, payloadLen int, cache *descriptorDecodeCache) ([]descriptor.TypeDescriptorFieldData, error) {
	declared := fbDescriptor.FieldsLength()
	if declared <= 0 {
		return nil, nil
	}
	fieldCount, err := boundedCount(declared, payloadLen, "type descriptor fields")
	if err != nil {
		return nil, err
	}
	fields := make([]descriptor.TypeDescriptorFieldData, fieldCount)
	var fbField schemagen.TypeDescField
	for i := range fieldCount {
		if !fbDescriptor.Fields(&fbField, i) {
			continue
		}
		fieldData, err := unpackTypeDescriptorFieldData(&fbField, depth, payloadLen, cache)
		if err != nil {
			return nil, err
		}
		fields[i] = fieldData
	}
	return fields, nil
}

// unpackTypeDescriptorList decodes the parameter or result types of a function type
// descriptor one level below it.
//
// Takes declared (int) which is the list length the payload claims.
// Takes at (func(*schemagen.TypeDescriptor, int) bool) which reads the entry at an index.
// Takes label (string) which names the list in the length error.
// Takes depth (int) which is the owning descriptor's nesting depth.
// Takes payloadLen (int) which bounds the declared length.
// Takes cache (*descriptorDecodeCache) which holds this load's decoded tables; may be
// nil.
//
// Returns []descriptor.TypeDescriptorData which holds the decoded types, or nil when the
// list is empty.
// Returns error when the declared length exceeds the payload size or an entry fails to
// decode.
func unpackTypeDescriptorList(declared int, at func(*schemagen.TypeDescriptor, int) bool, label string, depth, payloadLen int, cache *descriptorDecodeCache) ([]descriptor.TypeDescriptorData, error) {
	if declared <= 0 {
		return nil, nil
	}
	count, err := boundedCount(declared, payloadLen, label)
	if err != nil {
		return nil, err
	}
	entries := make([]descriptor.TypeDescriptorData, count)
	var fbEntry schemagen.TypeDescriptor
	for i := range count {
		if at(&fbEntry, i) {
			entry, err := unpackTypeDescriptor(&fbEntry, depth+1, payloadLen, cache)
			if err != nil {
				return nil, err
			}
			entries[i] = entry
		}
	}
	return entries, nil
}

// unpackTypeDescriptorFieldData materialises a single TypeDescriptorFieldData entry from
// a FlatBuffer field descriptor, recursively unpacking its type descriptor when present.
//
// Takes fbField (*schemagen.TypeDescField) which is the FlatBuffer field descriptor to
// materialise.
// Takes depth (int) which is the current recursion depth used to bound nested
// descriptors.
// Takes payloadLen (int) which is the total untrusted payload size in bytes, used to
// bound the nested descriptor's vector lengths.
// Takes cache (*descriptorDecodeCache) which shares decoded type descriptors across the
// load.
//
// Returns the materialised TypeDescriptorFieldData entry, and an error that is non-nil
// when descriptor unpacking fails.
func unpackTypeDescriptorFieldData(fbField *schemagen.TypeDescField, depth, payloadLen int, cache *descriptorDecodeCache) (descriptor.TypeDescriptorFieldData, error) {
	fieldData := descriptor.TypeDescriptorFieldData{
		Name:        mem.String(fbField.Name()),
		Tag:         mem.String(fbField.Tag()),
		PackagePath: mem.String(fbField.PackagePath()),
	}
	fbFieldType := fbField.TypeDescriptor(nil)
	if fbFieldType == nil {
		return fieldData, nil
	}
	fieldType, err := unpackTypeDescriptor(fbFieldType, depth+1, payloadLen, cache)
	if err != nil {
		return descriptor.TypeDescriptorFieldData{}, err
	}
	fieldData.Typ = fieldType
	return fieldData, nil
}
