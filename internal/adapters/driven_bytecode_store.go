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
	"io/fs"
	"maps"
	"reflect"
	"slices"
	"strings"

	"pipit.sh/pipit/internal/codec"
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab/descriptor"

	flatbuffers "github.com/google/flatbuffers/go"
	"pipit.sh/pipit/internal/fbs"
	"pipit.sh/pipit/internal/rootfs"
	"pipit.sh/pipit/internal/safeconv"
	"pipit.sh/pipit/internal/schema"
	"pipit.sh/pipit/internal/schema/schemagen"
)

const (
	// bytecodeDefaultDirPerm is the default directory permission for the bytecode cache
	// directory.
	bytecodeDefaultDirPerm = 0755

	// bytecodeDefaultFilePerm is the default file permission for cached bytecode files.
	bytecodeDefaultFilePerm = 0644

	// bytecodeInitialBuilder is the initial capacity in bytes for the FlatBuffer builder
	// used during serialisation.
	bytecodeInitialBuilder = 4096

	// bytecodeVectorAlignment is the byte alignment used when building FlatBuffer offset
	// vectors.
	bytecodeVectorAlignment = 4
)

// bytecodeStore provides FlatBuffer-based persistence for compiled bytecode. It
// implements engine.BytecodeStorePort.
type bytecodeStore struct {
	// store confines filesystem access to the bytecode cache directory.
	store rootfs.Store
}

var (
	_ program.BytecodeStorePort = (*bytecodeStore)(nil)
)

// NewBytecodeStore creates a bytecode store backed by the given rootfs store, whose root
// is the bytecode cache directory.
//
// Takes store (rootfs.Store) which confines filesystem access to the cache directory.
//
// Returns *bytecodeStore which is ready for use; callers hold it as a
// program.BytecodeStorePort.
//
//revive:disable-next-line:unexported-return // package-internal type stays unexported.
func NewBytecodeStore(store rootfs.Store) *bytecodeStore {
	return &bytecodeStore{store: store}
}

// Close releases the resources held by the underlying rootfs.Store.
//
// Returns error when the cleanup fails.
func (bytecodeStore *bytecodeStore) Close() error {
	if bytecodeStore.store == nil {
		return nil
	}
	return bytecodeStore.store.Close()
}

// SaveCompiledFileSet serialises and persists a compiled file set under the given key.
// The payload is wrapped with a schema version header and written atomically to prevent
// partial writes.
//
// Takes key (string) which identifies the compiled file set.
// Takes compiledFileSet (*program.CompiledFileSet) which is the compiled file set to
// persist.
//
// Returns error for invalid inputs, cancellation, excessive payload size or storage
// failure.
func (bytecodeStore *bytecodeStore) SaveCompiledFileSet(ctx context.Context, key string, compiledFileSet *program.CompiledFileSet) error {
	if bytecodeStore.store == nil || key == "" {
		return errors.New("bytecode store requires a store and key")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("bytecode save cancelled before serialisation: %w", err)
	}
	if compiledFileSet == nil {
		return errors.New("bytecode store requires a compiled file set")
	}

	builder := flatbuffers.NewBuilder(bytecodeInitialBuilder)
	endSharing := beginDescriptorSharing(builder)
	rootOffset := packCompiledFileSet(builder, compiledFileSet)
	endSharing()
	builder.Finish(rootOffset)

	payload := builder.FinishedBytes()
	if len(payload) > schema.MaximumBytecodePayloadBytes {
		return schema.ErrBytecodeTooLarge
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("bytecode save cancelled after serialisation: %w", err)
	}
	versionedData := make([]byte, fbs.PackedSize(len(payload)))
	schema.PackInto(versionedData, payload)

	if err := bytecodeStore.store.MkdirAll(".", bytecodeDefaultDirPerm); err != nil {
		return fmt.Errorf("failed to create bytecode directory: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("bytecode save cancelled before write: %w", err)
	}
	fileName := fmt.Sprintf("bytecode-%s.bin", key)
	if err := bytecodeStore.store.WriteFileAtomic(fileName, versionedData, bytecodeDefaultFilePerm); err != nil {
		return fmt.Errorf("failed to write bytecode file atomically: %w", err)
	}

	return nil
}

// invalidateCache removes the cached bytecode for the given key. If the file does not
// exist, no error is returned.
//
// Takes key (string) which identifies the cached bytecode to remove.
//
// Returns error when the store is nil, the key is empty, or the removal fails for a
// reason other than the file not existing.
func (bytecodeStore *bytecodeStore) invalidateCache(ctx context.Context, key string) error {
	if bytecodeStore.store == nil || key == "" {
		return errors.New("bytecode store requires a store and key")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("bytecode invalidate cancelled: %w", err)
	}

	fileName := fmt.Sprintf("bytecode-%s.bin", key)
	err := bytecodeStore.store.Remove(fileName)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("failed to remove bytecode file %s: %w", fileName, err)
	}
	return nil
}

// PackCompiledFileSetToBytes serialises a CompiledFileSet into a versioned FlatBuffer
// byte slice ready for writing to disk.
//
// Takes compiledFileSet (*program.CompiledFileSet) which is the compiled file set to
// serialise.
//
// Returns []byte which is the versioned FlatBuffer payload.
func PackCompiledFileSetToBytes(compiledFileSet *program.CompiledFileSet) []byte {
	builder := flatbuffers.NewBuilder(bytecodeInitialBuilder)
	endSharing := beginDescriptorSharing(builder)
	rootOffset := packCompiledFileSet(builder, compiledFileSet)
	endSharing()
	builder.Finish(rootOffset)

	payload := builder.FinishedBytes()
	versionedData := make([]byte, fbs.PackedSize(len(payload)))
	schema.PackInto(versionedData, payload)

	return versionedData
}

// packCompiledFileSet serialises a CompiledFileSet into a FlatBuffer. The root and
// variable init functions are packed recursively.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes compiledFileSet (*program.CompiledFileSet) which is the compiled file set to
// serialise.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed CompiledFileSet table.
func packCompiledFileSet(builder *flatbuffers.Builder, compiledFileSet *program.CompiledFileSet) flatbuffers.UOffsetT {
	var rootOffset flatbuffers.UOffsetT
	if compiledFileSet.Root() != nil {
		rootOffset = packCompiledFunction(builder, compiledFileSet.Root())
	}

	var varInitOffset flatbuffers.UOffsetT
	if compiledFileSet.VariableInitFunction() != nil {
		varInitOffset = packCompiledFunction(builder, compiledFileSet.VariableInitFunction())
	}

	entrypointsOffset := packEntrypoints(builder, compiledFileSet.Entrypoints())
	initFunctionIndicesOffset := packUint16Slice(builder, compiledFileSet.InitFunctions())
	packageVariablesOffset := packPackageVariables(builder, compiledFileSet.PackageVariables())

	schemagen.CompiledFileSetStart(builder)
	if rootOffset != 0 {
		schemagen.CompiledFileSetAddRoot(builder, rootOffset)
	}
	if varInitOffset != 0 {
		schemagen.CompiledFileSetAddVariableInitFunction(builder, varInitOffset)
	}
	if entrypointsOffset != 0 {
		schemagen.CompiledFileSetAddEntrypoints(builder, entrypointsOffset)
	}
	if initFunctionIndicesOffset != 0 {
		schemagen.CompiledFileSetAddInitialisationFunctions(builder, initFunctionIndicesOffset)
	}
	if alloc := compiledFileSet.SlotAllocation(); !isZeroSlotAllocation(alloc) {
		slotAllocationOffset := schemagen.CreateSlotAllocation(
			builder,
			alloc[0], alloc[1], alloc[2], alloc[3], alloc[4], alloc[5], alloc[6],
		)
		schemagen.CompiledFileSetAddSlotAllocation(builder, slotAllocationOffset)
	}
	if packageVariablesOffset != 0 {
		schemagen.CompiledFileSetAddPackageVariables(builder, packageVariablesOffset)
	}
	return schemagen.CompiledFileSetEnd(builder)
}

// isZeroSlotAllocation reports whether the bundle reserved no global-store slots.
// Serialisation skips the SlotAllocation struct in that case to keep payloads minimal for
// function-only module.
//
// Takes alloc (program.SlotAllocation) which is the bundle's per-bank slot allocation
// tuple.
//
// Returns bool which is true when every entry is zero.
func isZeroSlotAllocation(alloc program.SlotAllocation) bool {
	for _, v := range alloc {
		if v != 0 {
			return false
		}
	}
	return true
}

// packPackageVariables serialises the PackageVariableMetadata vector that the load path
// uses to rebuild settable storage and pendingVarBridge entries.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes vars ([]program.PackageVariableMetadata) which holds the package-variable entries
// to serialise.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// bundle has no exported vars.
func packPackageVariables(builder *flatbuffers.Builder, vars []program.PackageVariableMetadata) flatbuffers.UOffsetT {
	if len(vars) == 0 {
		return 0
	}
	offsets := make([]flatbuffers.UOffsetT, len(vars))
	for i, v := range vars {
		nameOffset := builder.CreateString(v.Name)
		pathOffset := builder.CreateString(v.PackagePath)
		var typeOffset flatbuffers.UOffsetT
		if v.Type != nil {
			typeOffset = packTypeDescriptor(builder, *v.Type)
		}
		schemagen.PackageVariableEntryStart(builder)
		schemagen.PackageVariableEntryAddName(builder, nameOffset)
		schemagen.PackageVariableEntryAddPackagePath(builder, pathOffset)
		if typeOffset != 0 {
			schemagen.PackageVariableEntryAddTypeDescriptor(builder, typeOffset)
		}
		schemagen.PackageVariableEntryAddRegisterKind(builder, schemagen.RegisterKind(safeconv.MustUint8ToInt8(v.RegisterKind)))
		schemagen.PackageVariableEntryAddRelativeSlot(builder, v.RelativeSlot)
		schemagen.PackageVariableEntryAddIsIndirect(builder, v.IsIndirect)
		offsets[i] = schemagen.PackageVariableEntryEnd(builder)
	}
	schemagen.CompiledFileSetStartPackageVariablesVector(builder, len(offsets))
	for _, v := range slices.Backward(offsets) {
		builder.PrependUOffsetT(v)
	}
	return builder.EndVector(len(offsets))
}

// compiledFunctionOffsets holds the FlatBuffer offsets of every child object of one
// CompiledFunction table, built before the table itself is started.
type compiledFunctionOffsets struct {
	// name is the offset of the function's display name string.
	name flatbuffers.UOffsetT

	// sourceFile is the offset of the source file path string.
	sourceFile flatbuffers.UOffsetT

	// registerCounts is the offset of the per-bank register count vector.
	registerCounts flatbuffers.UOffsetT

	// parameterKinds is the offset of the per-parameter register kind vector.
	parameterKinds flatbuffers.UOffsetT

	// parameterRegisters is the offset of the per-parameter register index vector.
	parameterRegisters flatbuffers.UOffsetT

	// resultKinds is the offset of the per-result register kind vector.
	resultKinds flatbuffers.UOffsetT

	// body is the offset of the bytecode instruction sequence.
	body flatbuffers.UOffsetT

	// boolConstants is the offset of the bool constant pool.
	boolConstants flatbuffers.UOffsetT

	// intConstants is the offset of the int64 constant pool.
	intConstants flatbuffers.UOffsetT

	// floatConstants is the offset of the float64 constant pool.
	floatConstants flatbuffers.UOffsetT

	// uintConstants is the offset of the uint64 constant pool.
	uintConstants flatbuffers.UOffsetT

	// complexConstants is the offset of the complex128 constant pool.
	complexConstants flatbuffers.UOffsetT

	// stringConstants is the offset of the string constant pool.
	stringConstants flatbuffers.UOffsetT

	// generalDescriptors is the offset of the general constant descriptor vector.
	generalDescriptors flatbuffers.UOffsetT

	// typeTableDescriptors is the offset of the type table descriptor vector.
	typeTableDescriptors flatbuffers.UOffsetT

	// typeTableInterfaceMethods is the offset of the per-type-table interface method name
	// set vector.
	typeTableInterfaceMethods flatbuffers.UOffsetT

	// typeNames is the offset of the reflect.Type to source-level name mapping.
	typeNames flatbuffers.UOffsetT

	// callSites is the offset of the function call site descriptor vector.
	callSites flatbuffers.UOffsetT

	// upvalueDescriptors is the offset of the closure upvalue descriptor vector.
	upvalueDescriptors flatbuffers.UOffsetT

	// functions is the offset of the nested function literal vector.
	functions flatbuffers.UOffsetT

	// namedResultLocations is the offset of the named return value location vector.
	namedResultLocations flatbuffers.UOffsetT

	// methodTable is the offset of the method name to function index mapping.
	methodTable flatbuffers.UOffsetT

	// structLayoutTable is the offset of the struct field layout table.
	structLayoutTable flatbuffers.UOffsetT

	// variableInitFunction is the offset of the package-level variable init function.
	variableInitFunction flatbuffers.UOffsetT

	// signatureDescriptor is the offset of the function's Go signature type descriptor.
	signatureDescriptor flatbuffers.UOffsetT
}

// packCompiledFunction recursively serialises a CompiledFunction into a FlatBuffer. All
// constant pools, descriptors, call sites, and child functions are packed.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes compiledFunction (*program.CompiledFunction) which is the function to serialise.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed CompiledFunction table.
func packCompiledFunction(builder *flatbuffers.Builder, compiledFunction *program.CompiledFunction) flatbuffers.UOffsetT {
	offsets := packCompiledFunctionChildren(builder, compiledFunction)
	return writeCompiledFunctionTable(builder, compiledFunction, &offsets)
}

// packCompiledFunctionChildren packs every child object of a CompiledFunction table.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes compiledFunction (*program.CompiledFunction) which is the function to serialise.
//
// Returns compiledFunctionOffsets which holds the offset of each packed child object, or
// 0 for an absent one.
func packCompiledFunctionChildren(builder *flatbuffers.Builder, compiledFunction *program.CompiledFunction) compiledFunctionOffsets {
	return compiledFunctionOffsets{
		name:                      builder.CreateString(codec.ExportName(compiledFunction)),
		sourceFile:                builder.CreateString(codec.ExportSourceFile(compiledFunction)),
		registerCounts:            packRegisterCounts(builder, codec.NumRegistersSlice(compiledFunction)),
		parameterKinds:            packRegisterKinds(builder, codec.ParamKinds(compiledFunction)),
		parameterRegisters:        packParameterRegisters(builder, codec.ParamRegisters(compiledFunction)),
		resultKinds:               packRegisterKinds(builder, codec.ResultKinds(compiledFunction)),
		body:                      packInstructions(builder, codec.Body(compiledFunction)),
		boolConstants:             packBoolSlice(builder, codec.BoolConstants(compiledFunction)),
		intConstants:              packInt64Slice(builder, codec.IntConstants(compiledFunction)),
		floatConstants:            packFloat64Slice(builder, codec.FloatConstants(compiledFunction)),
		uintConstants:             packUint64Slice(builder, codec.UintConstants(compiledFunction)),
		complexConstants:          packComplexSlice(builder, codec.ComplexConstants(compiledFunction)),
		stringConstants:           packStringSlice(builder, codec.StringConstants(compiledFunction)),
		generalDescriptors:        packGeneralConstantDescriptors(builder, codec.GeneralConstantDescriptors(compiledFunction)),
		typeTableDescriptors:      packTypeDescriptors(builder, codec.TypeTableDescriptors(compiledFunction)),
		typeTableInterfaceMethods: packInterfaceMethodSets(builder, codec.TypeTableInterfaceMethods(compiledFunction)),
		typeNames:                 packTypeNames(builder, codec.TypeNames(compiledFunction)),
		callSites:                 packCallSites(builder, codec.CallSites(compiledFunction)),
		upvalueDescriptors:        packUpvalueDescriptors(builder, codec.UpvalueDescriptors(compiledFunction)),
		functions:                 packChildFunctions(builder, program.ExportFunctions(compiledFunction)),
		namedResultLocations:      packVarLocations(builder, codec.NamedResultLocations(compiledFunction)),
		methodTable:               packMethodTable(builder, codec.MethodTable(compiledFunction)),
		structLayoutTable:         packStructLayoutTable(builder, codec.StructLayoutTable(compiledFunction)),
		variableInitFunction:      packVariableInitFunction(builder, compiledFunction),
		signatureDescriptor:       packSignatureDescriptor(builder, compiledFunction),
	}
}

// packChildFunctions recursively packs a function's child functions and the vector that
// refers to them.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes childFunctions ([]*program.CompiledFunction) which holds the children to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector.
func packChildFunctions(builder *flatbuffers.Builder, childFunctions []*program.CompiledFunction) flatbuffers.UOffsetT {
	childOffsets := make([]flatbuffers.UOffsetT, len(childFunctions))
	for i, child := range childFunctions {
		childOffsets[i] = packCompiledFunction(builder, child)
	}
	return createVector(builder, childOffsets)
}

// packVariableInitFunction packs the package-level variable initialisation function when
// the function has one.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes compiledFunction (*program.CompiledFunction) which is the function that may own
// the init function.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed init function, or 0 when
// there is none.
func packVariableInitFunction(builder *flatbuffers.Builder, compiledFunction *program.CompiledFunction) flatbuffers.UOffsetT {
	if program.VariableInitFunction(compiledFunction) == nil {
		return 0
	}
	return packCompiledFunction(builder, program.VariableInitFunction(compiledFunction))
}

// packSignatureDescriptor packs the type descriptor of the function's own static Go
// signature when one was recorded.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes compiledFunction (*program.CompiledFunction) which is the function whose
// signature is packed.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed descriptor, or 0 when
// the function has no signature type.
func packSignatureDescriptor(builder *flatbuffers.Builder, compiledFunction *program.CompiledFunction) flatbuffers.UOffsetT {
	signature := codec.ExportSignatureReflectType(compiledFunction)
	if signature == nil {
		return 0
	}
	return packTypeDescriptor(builder, codec.ExportTypeDescriptor(descriptor.ReflectTypeToDescriptor(signature)))
}

// writeCompiledFunctionTable writes the CompiledFunction table itself from the flags on
// the function and the child offsets packed beforehand.
//
// Fields are added in a fixed order so the packed bytes do not change between runs.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes compiledFunction (*program.CompiledFunction) which supplies the boolean flags.
// Takes offsets (*compiledFunctionOffsets) which holds the packed child objects.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed CompiledFunction table.
func writeCompiledFunctionTable(builder *flatbuffers.Builder, compiledFunction *program.CompiledFunction, offsets *compiledFunctionOffsets) flatbuffers.UOffsetT {
	schemagen.CompiledFunctionStart(builder)
	schemagen.CompiledFunctionAddName(builder, offsets.name)
	schemagen.CompiledFunctionAddSourceFile(builder, offsets.sourceFile)
	schemagen.CompiledFunctionAddIsVariadic(builder, codec.ExportIsVariadic(compiledFunction))
	schemagen.CompiledFunctionAddIsPointerReceiver(builder, codec.ExportIsPointerReceiver(compiledFunction))
	schemagen.CompiledFunctionAddHasRecover(builder, codec.ExportHasRecover(compiledFunction))
	schemagen.CompiledFunctionAddHasReceiver(builder, codec.ExportHasReceiver(compiledFunction))
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddSignatureTypeDescriptor, offsets.signatureDescriptor)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddRegisterCounts, offsets.registerCounts)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddParameterKinds, offsets.parameterKinds)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddParameterRegisters, offsets.parameterRegisters)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddResultKinds, offsets.resultKinds)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddBody, offsets.body)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddBoolConstants, offsets.boolConstants)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddIntConstants, offsets.intConstants)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddFloatConstants, offsets.floatConstants)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddUintConstants, offsets.uintConstants)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddComplexConstants, offsets.complexConstants)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddStringConstants, offsets.stringConstants)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddGeneralConstantDescriptors, offsets.generalDescriptors)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddTypeTableDescriptors, offsets.typeTableDescriptors)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddTypeTableInterfaceMethods, offsets.typeTableInterfaceMethods)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddTypeNames, offsets.typeNames)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddCallSites, offsets.callSites)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddUpvalueDescriptors, offsets.upvalueDescriptors)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddFunctions, offsets.functions)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddNamedResultLocations, offsets.namedResultLocations)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddMethodTable, offsets.methodTable)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddVariableInitFunction, offsets.variableInitFunction)
	addOffsetIfSet(builder, schemagen.CompiledFunctionAddStructLayoutTable, offsets.structLayoutTable)
	return schemagen.CompiledFunctionEnd(builder)
}

// addOffsetIfSet adds an offset field to the table being built, leaving the field out
// when the offset is 0 because the object was never packed.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes add (func(*flatbuffers.Builder, flatbuffers.UOffsetT)) which is the generated
// adder for the field.
// Takes offset (flatbuffers.UOffsetT) which is the offset to add.
func addOffsetIfSet(builder *flatbuffers.Builder, add func(*flatbuffers.Builder, flatbuffers.UOffsetT), offset flatbuffers.UOffsetT) {
	if offset != 0 {
		add(builder, offset)
	}
}

// packStructLayoutTable packs the compile-time-resolved struct field layout table as a
// vector of FlatBuffer structs.
//
// Takes builder (*flatbuffers.Builder) which is the buffer to write into.
// Takes layouts ([]codec.StructFieldLayoutData) which holds the entries to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func packStructLayoutTable(builder *flatbuffers.Builder, layouts []codec.StructFieldLayoutData) flatbuffers.UOffsetT {
	if len(layouts) == 0 {
		return 0
	}
	schemagen.CompiledFunctionStartStructLayoutTableVector(builder, len(layouts))
	for _, layout := range slices.Backward(layouts) {
		schemagen.CreateStructFieldLayout(builder,
			layout.Offset, layout.TypeIndex,
			layout.Path[0], layout.Path[1], layout.Path[2], layout.Path[3],
			layout.PathLength, layout.Kind, layout.RegisterKind, layout.Flags,
			layout.FieldTypeIndex)
	}
	return builder.EndVector(len(layouts))
}

// packInstructions packs a slice of instructions as FlatBuffer structs. Instructions are
// 4-byte fixed-size structs enabling zero-copy reads.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes instructions ([]codec.InstructionData) which holds the bytecode instructions to
// pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed instruction vector, or 0
// when the slice is empty.
func packInstructions(builder *flatbuffers.Builder, instructions []codec.InstructionData) flatbuffers.UOffsetT {
	if len(instructions) == 0 {
		return 0
	}
	schemagen.CompiledFunctionStartBodyVector(builder, len(instructions))
	for _, instruction := range slices.Backward(instructions) {
		schemagen.CreateInstruction(builder, instruction.Operation, instruction.A, instruction.B, instruction.C)
	}
	return builder.EndVector(len(instructions))
}

// packUpvalueDescriptors packs upvalue descriptors as FlatBuffer structs. Each descriptor
// is a 5-byte fixed-size struct.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes descriptors ([]codec.UpvalueDescriptorData) which holds the upvalue descriptors
// to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func packUpvalueDescriptors(builder *flatbuffers.Builder, descriptors []codec.UpvalueDescriptorData) flatbuffers.UOffsetT {
	if len(descriptors) == 0 {
		return 0
	}
	schemagen.CompiledFunctionStartUpvalueDescriptorsVector(builder, len(descriptors))
	for _, upvalueDescriptor := range slices.Backward(descriptors) {
		schemagen.CreateUpvalueDescriptor(
			builder,
			upvalueDescriptor.Index,
			schemagen.RegisterKind(safeconv.MustUint8ToInt8(upvalueDescriptor.Kind)),
			upvalueDescriptor.IsLocal,
			upvalueDescriptor.IsIndirect,
			schemagen.RegisterKind(safeconv.MustUint8ToInt8(upvalueDescriptor.OriginalKind)),
		)
	}
	return builder.EndVector(len(descriptors))
}

// packVarLocations packs variable locations as FlatBuffer tables.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes locations ([]codec.VarLocationData) which holds the variable locations to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func packVarLocations(builder *flatbuffers.Builder, locations []codec.VarLocationData) flatbuffers.UOffsetT {
	if len(locations) == 0 {
		return 0
	}
	offsets := make([]flatbuffers.UOffsetT, len(locations))
	for i, location := range locations {
		offsets[i] = packVarLocation(builder, location)
	}
	return createVector(builder, offsets)
}

// packVarLocation packs a single variable location as a FlatBuffer table.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes location (codec.VarLocationData) which holds the variable location fields.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed VarLocation table.
func packVarLocation(builder *flatbuffers.Builder, location codec.VarLocationData) flatbuffers.UOffsetT {
	schemagen.VarLocationStart(builder)
	schemagen.VarLocationAddUpvalueIndex(builder, location.UpvalueIndex)
	schemagen.VarLocationAddRegister(builder, location.Register)
	schemagen.VarLocationAddKind(builder, schemagen.RegisterKind(safeconv.MustUint8ToInt8(location.Kind)))
	schemagen.VarLocationAddIsUpvalue(builder, location.IsUpvalue)
	schemagen.VarLocationAddIsIndirect(builder, location.IsIndirect)
	schemagen.VarLocationAddOriginalKind(builder, schemagen.RegisterKind(safeconv.MustUint8ToInt8(location.OriginalKind)))
	schemagen.VarLocationAddIsSpilled(builder, location.IsSpilled)
	schemagen.VarLocationAddSpillSlot(builder, location.SpillSlot)
	return schemagen.VarLocationEnd(builder)
}

// packCallSites packs call site descriptors as FlatBuffer tables.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes callSites ([]codec.CallSiteData) which holds the call site descriptors to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func packCallSites(builder *flatbuffers.Builder, callSites []codec.CallSiteData) flatbuffers.UOffsetT {
	if len(callSites) == 0 {
		return 0
	}
	offsets := make([]flatbuffers.UOffsetT, len(callSites))
	for i, site := range callSites {
		offsets[i] = packCallSite(builder, site)
	}
	return createVector(builder, offsets)
}

// packCallSite packs a single call site as a FlatBuffer table. Only static metadata is
// serialised; runtime caches are reconstructed on load.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes site (codec.CallSiteData) which holds the call site fields.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed CallSite table.
func packCallSite(builder *flatbuffers.Builder, site codec.CallSiteData) flatbuffers.UOffsetT {
	argumentsOffset := packVarLocations(builder, site.Arguments)
	returnsOffset := packVarLocations(builder, site.Returns)

	schemagen.CallSiteStart(builder)
	schemagen.CallSiteAddFunctionIndex(builder, site.FunctionIndex)
	schemagen.CallSiteAddClosureRegister(builder, site.ClosureRegister)
	schemagen.CallSiteAddNativeRegister(builder, site.NativeRegister)
	schemagen.CallSiteAddIsClosure(builder, site.IsClosure)
	schemagen.CallSiteAddIsNative(builder, site.IsNative)
	schemagen.CallSiteAddIsMethod(builder, site.IsMethod)
	schemagen.CallSiteAddMethodReceiverRegister(builder, site.MethodReceiverRegister)
	if argumentsOffset != 0 {
		schemagen.CallSiteAddArguments(builder, argumentsOffset)
	}
	if returnsOffset != 0 {
		schemagen.CallSiteAddReturns(builder, returnsOffset)
	}
	if site.BlocksHostGoroutine {
		schemagen.CallSiteAddBlocksHostGoroutine(builder, site.BlocksHostGoroutine)
	}
	if site.IsEllipsisSpread {
		schemagen.CallSiteAddIsEllipsisSpread(builder, site.IsEllipsisSpread)
	}
	return schemagen.CallSiteEnd(builder)
}

// packTypeDescriptor packs a type descriptor into a FlatBuffer table. Recursive fields
// (elem, key, value, fields, params, results) are packed depth-first.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes typeDescriptorData (descriptor.TypeDescriptorData) which holds the type
// descriptor to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed TypeDescriptor table.
func packTypeDescriptor(builder *flatbuffers.Builder, typeDescriptorData descriptor.TypeDescriptorData) flatbuffers.UOffsetT {
	offset, _ := packTypeDescriptorShared(builder, typeDescriptorData)
	return offset
}

// packTypeDescriptorShared is packTypeDescriptor that also reports the subtree's
// structural key, so an identical subtree already written into this builder is referenced
// again rather than written a second time.
//
// Children are packed first because a parent's key is built from theirs, which keeps the
// whole keying linear in the tree rather than quadratic. The strings are created after
// the memo check so a shared subtree costs nothing at all.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes typeDescriptorData (descriptor.TypeDescriptorData) which holds the type
// descriptor to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed TypeDescriptor table.
// Returns string which is the subtree's structural key.
func packTypeDescriptorShared(builder *flatbuffers.Builder, typeDescriptorData descriptor.TypeDescriptorData) (flatbuffers.UOffsetT, string) {
	var elemOffset, keyOffset, valueOffset flatbuffers.UOffsetT
	var elemKey, keyKey, valueKey string
	if typeDescriptorData.Elem != nil {
		elemOffset, elemKey = packTypeDescriptorShared(builder, *typeDescriptorData.Elem)
	}
	if typeDescriptorData.Key != nil {
		keyOffset, keyKey = packTypeDescriptorShared(builder, *typeDescriptorData.Key)
	}
	if typeDescriptorData.Value != nil {
		valueOffset, valueKey = packTypeDescriptorShared(builder, *typeDescriptorData.Value)
	}

	fieldsOffset, fieldsKey := packTypeDescFieldsShared(builder, typeDescriptorData.Fields)
	paramsOffset, paramsKey := packTypeDescriptorsShared(builder, typeDescriptorData.Params)
	resultsOffset, resultsKey := packTypeDescriptorsShared(builder, typeDescriptorData.Results)

	key := typeDescriptorStructuralKey(typeDescriptorData, elemKey, keyKey, valueKey, fieldsKey, paramsKey, resultsKey)

	memo := descriptorMemoFor(builder)
	if existing, ok := memo.lookup(key); ok {
		return existing, key
	}

	packagePathOffset := builder.CreateString(typeDescriptorData.PackagePath)
	nameOffset := builder.CreateString(typeDescriptorData.Name)

	schemagen.TypeDescriptorStart(builder)
	schemagen.TypeDescriptorAddKind(builder, schemagen.TypeDescKind(safeconv.MustUint8ToInt8(typeDescriptorData.Kind)))
	schemagen.TypeDescriptorAddPackagePath(builder, packagePathOffset)
	schemagen.TypeDescriptorAddName(builder, nameOffset)
	schemagen.TypeDescriptorAddBasicKind(builder, typeDescriptorData.BasicKind)
	if elemOffset != 0 {
		schemagen.TypeDescriptorAddElement(builder, elemOffset)
	}
	if keyOffset != 0 {
		schemagen.TypeDescriptorAddKey(builder, keyOffset)
	}
	if valueOffset != 0 {
		schemagen.TypeDescriptorAddValue(builder, valueOffset)
	}
	schemagen.TypeDescriptorAddLength(builder, typeDescriptorData.Length)
	schemagen.TypeDescriptorAddDirection(builder, typeDescriptorData.Dir)
	if fieldsOffset != 0 {
		schemagen.TypeDescriptorAddFields(builder, fieldsOffset)
	}
	if paramsOffset != 0 {
		schemagen.TypeDescriptorAddParams(builder, paramsOffset)
	}
	if resultsOffset != 0 {
		schemagen.TypeDescriptorAddResults(builder, resultsOffset)
	}
	schemagen.TypeDescriptorAddIsVariadic(builder, typeDescriptorData.IsVariadic)
	offset := schemagen.TypeDescriptorEnd(builder)
	memo.remember(key, offset)
	return offset, key
}

// typeDescriptorStructuralKey renders the key two descriptors share exactly when they
// would pack to identical bytes.
//
// Takes typeDescriptorData (descriptor.TypeDescriptorData) which supplies the node's own
// fields.
// Takes elemKey (string) which is the elem pointer child's key, empty when absent.
// Takes keyKey (string) which is the key pointer child's key, empty when absent.
// Takes valueKey (string) which is the value pointer child's key, empty when absent.
// Takes fieldsKey (string) which is the fields vector child's key, empty when absent.
// Takes paramsKey (string) which is the params vector child's key, empty when absent.
// Takes resultsKey (string) which is the results vector child's key, empty when absent.
//
// Returns string which is the node's structural key.
func typeDescriptorStructuralKey(
	typeDescriptorData descriptor.TypeDescriptorData,
	elemKey, keyKey, valueKey, fieldsKey, paramsKey, resultsKey string,
) string {
	var keyBuilder strings.Builder
	_, _ = keyBuilder.WriteString("d")
	appendKeyInt(&keyBuilder, int64(typeDescriptorData.Kind))
	appendKeyInt(&keyBuilder, int64(typeDescriptorData.BasicKind))
	appendKeyInt(&keyBuilder, int64(typeDescriptorData.Length))
	appendKeyInt(&keyBuilder, int64(typeDescriptorData.Dir))
	appendKeyInt(&keyBuilder, boolKeyValue(typeDescriptorData.IsVariadic))
	appendKeyPart(&keyBuilder, typeDescriptorData.PackagePath)
	appendKeyPart(&keyBuilder, typeDescriptorData.Name)
	appendKeyPart(&keyBuilder, elemKey)
	appendKeyPart(&keyBuilder, keyKey)
	appendKeyPart(&keyBuilder, valueKey)
	appendKeyPart(&keyBuilder, fieldsKey)
	appendKeyPart(&keyBuilder, paramsKey)
	appendKeyPart(&keyBuilder, resultsKey)
	return keyBuilder.String()
}

// packTypeDescriptors packs a slice of type descriptors as a FlatBuffer vector.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes descriptors ([]descriptor.TypeDescriptorData) which holds the type descriptors to
// pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func packTypeDescriptors(builder *flatbuffers.Builder, descriptors []descriptor.TypeDescriptorData) flatbuffers.UOffsetT {
	offset, _ := packTypeDescriptorsShared(builder, descriptors)
	return offset
}

// packTypeDescriptorsShared is packTypeDescriptors that also reports the vector's
// structural key, and shares the vector itself when an identical one was already written.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes descriptors ([]descriptor.TypeDescriptorData) which holds the type descriptors to
// pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
// Returns string which is the vector's structural key.
func packTypeDescriptorsShared(builder *flatbuffers.Builder, descriptors []descriptor.TypeDescriptorData) (flatbuffers.UOffsetT, string) {
	if len(descriptors) == 0 {
		return 0, ""
	}
	offsets := make([]flatbuffers.UOffsetT, len(descriptors))
	var keyBuilder strings.Builder
	_, _ = keyBuilder.WriteString("v")
	for i := range descriptors {
		var elementKey string
		offsets[i], elementKey = packTypeDescriptorShared(builder, descriptors[i])
		appendKeyPart(&keyBuilder, elementKey)
	}
	key := keyBuilder.String()

	memo := descriptorMemoFor(builder)
	if existing, ok := memo.lookup(key); ok {
		return existing, key
	}
	offset := createVector(builder, offsets)
	memo.remember(key, offset)
	return offset, key
}

// packTypeDescFieldsShared packs struct field descriptors as FlatBuffer tables and
// reports the vector's structural key, sharing both the field tables and the vector when
// identical ones were already written.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes fields ([]descriptor.TypeDescriptorFieldData) which holds the fields to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
// Returns string which is the vector's structural key.
func packTypeDescFieldsShared(builder *flatbuffers.Builder, fields []descriptor.TypeDescriptorFieldData) (flatbuffers.UOffsetT, string) {
	if len(fields) == 0 {
		return 0, ""
	}
	offsets := make([]flatbuffers.UOffsetT, len(fields))
	var vectorKeyBuilder strings.Builder
	_, _ = vectorKeyBuilder.WriteString("fv")
	memo := descriptorMemoFor(builder)
	for i := range fields {
		typeOffset, typeKey := packTypeDescriptorShared(builder, fields[i].Typ)

		var fieldKeyBuilder strings.Builder
		_, _ = fieldKeyBuilder.WriteString("f")
		appendKeyPart(&fieldKeyBuilder, fields[i].Name)
		appendKeyPart(&fieldKeyBuilder, fields[i].Tag)
		appendKeyPart(&fieldKeyBuilder, fields[i].PackagePath)
		appendKeyPart(&fieldKeyBuilder, typeKey)
		fieldKey := fieldKeyBuilder.String()
		appendKeyPart(&vectorKeyBuilder, fieldKey)

		if existing, ok := memo.lookup(fieldKey); ok {
			offsets[i] = existing
			continue
		}
		nameOffset := builder.CreateString(fields[i].Name)
		tagOffset := builder.CreateString(fields[i].Tag)
		packagePathOffset := builder.CreateString(fields[i].PackagePath)

		schemagen.TypeDescFieldStart(builder)
		schemagen.TypeDescFieldAddName(builder, nameOffset)
		schemagen.TypeDescFieldAddTag(builder, tagOffset)
		schemagen.TypeDescFieldAddPackagePath(builder, packagePathOffset)
		schemagen.TypeDescFieldAddTypeDescriptor(builder, typeOffset)
		offsets[i] = schemagen.TypeDescFieldEnd(builder)
		memo.remember(fieldKey, offsets[i])
	}
	key := vectorKeyBuilder.String()
	if existing, ok := memo.lookup(key); ok {
		return existing, key
	}
	offset := createVector(builder, offsets)
	memo.remember(key, offset)
	return offset, key
}

// packGeneralConstantDescriptors packs general constant descriptors as FlatBuffer tables.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes descriptors ([]codec.GeneralConstantDescriptorData) which holds the descriptors
// to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func packGeneralConstantDescriptors(builder *flatbuffers.Builder, descriptors []codec.GeneralConstantDescriptorData) flatbuffers.UOffsetT {
	if len(descriptors) == 0 {
		return 0
	}
	offsets := make([]flatbuffers.UOffsetT, len(descriptors))
	for i := range descriptors {
		packagePathOffset := builder.CreateString(descriptors[i].PackagePath)
		symbolNameOffset := builder.CreateString(descriptors[i].SymbolName)
		typeDescOffset := packTypeDescriptor(builder, descriptors[i].TypeDescriptor)

		schemagen.GeneralConstantDescriptorStart(builder)
		schemagen.GeneralConstantDescriptorAddKind(builder, schemagen.GeneralConstantKind(safeconv.MustUint8ToInt8(descriptors[i].Kind)))
		schemagen.GeneralConstantDescriptorAddPackagePath(builder, packagePathOffset)
		schemagen.GeneralConstantDescriptorAddSymbolName(builder, symbolNameOffset)
		schemagen.GeneralConstantDescriptorAddTypeDescriptor(builder, typeDescOffset)
		offsets[i] = schemagen.GeneralConstantDescriptorEnd(builder)
	}
	return createVector(builder, offsets)
}

// packTypeNames packs the type names map as a vector of entries. Each entry pairs a type
// descriptor with its string name.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes typeNames (map[reflect.Type]codec.TypeNameData) which maps runtime types to their
// serialisable name entries.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// map is empty.
func packTypeNames(builder *flatbuffers.Builder, typeNames map[reflect.Type]codec.TypeNameData) flatbuffers.UOffsetT {
	if len(typeNames) == 0 {
		return 0
	}

	sortedData := slices.Collect(maps.Values(typeNames))
	slices.SortFunc(sortedData, func(a, b codec.TypeNameData) int {
		return strings.Compare(a.Name, b.Name)
	})

	offsets := make([]flatbuffers.UOffsetT, 0, len(sortedData))
	for i := range sortedData {
		data := &sortedData[i]
		nameOffset := builder.CreateString(data.Name)
		typeDescOffset := packTypeDescriptor(builder, data.TypeDescriptor)

		schemagen.TypeNameEntryStart(builder)
		schemagen.TypeNameEntryAddName(builder, nameOffset)
		schemagen.TypeNameEntryAddTypeDescriptor(builder, typeDescOffset)
		offsets = append(offsets, schemagen.TypeNameEntryEnd(builder))
	}
	return createVector(builder, offsets)
}

// packEntrypoints packs the entrypoints map as a sorted vector of entrypoint entries.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes entrypoints (map[string]uint16) which maps function names to their indices.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// map is empty.
func packEntrypoints(builder *flatbuffers.Builder, entrypoints map[string]uint16) flatbuffers.UOffsetT {
	return packStringUint16Map(builder, entrypoints, func(mapBuilder *flatbuffers.Builder, nameOffset flatbuffers.UOffsetT, index uint16) flatbuffers.UOffsetT {
		schemagen.EntrypointEntryStart(mapBuilder)
		schemagen.EntrypointEntryAddName(mapBuilder, nameOffset)
		schemagen.EntrypointEntryAddFunctionIndex(mapBuilder, index)
		return schemagen.EntrypointEntryEnd(mapBuilder)
	})
}

// packMethodTable packs the method table map as a sorted vector of method table entries.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes methodTable (map[string]uint16) which maps method names to their function
// indices.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// map is empty.
func packMethodTable(builder *flatbuffers.Builder, methodTable map[string]uint16) flatbuffers.UOffsetT {
	return packStringUint16Map(builder, methodTable, func(mapBuilder *flatbuffers.Builder, nameOffset flatbuffers.UOffsetT, index uint16) flatbuffers.UOffsetT {
		schemagen.MethodTableEntryStart(mapBuilder)
		schemagen.MethodTableEntryAddName(mapBuilder, nameOffset)
		schemagen.MethodTableEntryAddFunctionIndex(mapBuilder, index)
		return schemagen.MethodTableEntryEnd(mapBuilder)
	})
}

// packStringUint16Map packs a map[string]uint16 as a sorted vector of FlatBuffer entries
// using the provided entry builder function. Keys are sorted for deterministic output.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes entries (map[string]uint16) which is the map to serialise.
// Takes buildEntry (func) which creates each entry from a name offset and uint16 value.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// map is empty.
func packStringUint16Map(
	builder *flatbuffers.Builder,
	entries map[string]uint16,
	buildEntry func(*flatbuffers.Builder, flatbuffers.UOffsetT, uint16) flatbuffers.UOffsetT,
) flatbuffers.UOffsetT {
	if len(entries) == 0 {
		return 0
	}
	keys := slices.Sorted(maps.Keys(entries))
	offsets := make([]flatbuffers.UOffsetT, len(keys))
	for i, key := range keys {
		nameOffset := builder.CreateString(key)
		offsets[i] = buildEntry(builder, nameOffset, entries[key])
	}
	return createVector(builder, offsets)
}

// packComplexSlice packs complex128 constants as FlatBuffer ComplexValue structs. Each
// struct is 16 bytes (two float64).
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes values ([]complex128) which holds the complex constants to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func packComplexSlice(builder *flatbuffers.Builder, values []complex128) flatbuffers.UOffsetT {
	if len(values) == 0 {
		return 0
	}
	schemagen.CompiledFunctionStartComplexConstantsVector(builder, len(values))
	for _, value := range slices.Backward(values) {
		schemagen.CreateComplexValue(builder, real(value), imag(value))
	}
	return builder.EndVector(len(values))
}

// packStringSlice packs string constants as a FlatBuffer string vector.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes values ([]string) which holds the string constants to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func packStringSlice(builder *flatbuffers.Builder, values []string) flatbuffers.UOffsetT {
	if len(values) == 0 {
		return 0
	}
	offsets := make([]flatbuffers.UOffsetT, len(values))
	for i, value := range values {
		offsets[i] = builder.CreateString(value)
	}
	return createVector(builder, offsets)
}

// packBoolSlice packs bool constants as a FlatBuffer boolean vector.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes values ([]bool) which holds the boolean constants to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func packBoolSlice(builder *flatbuffers.Builder, values []bool) flatbuffers.UOffsetT {
	if len(values) == 0 {
		return 0
	}
	schemagen.CompiledFunctionStartBoolConstantsVector(builder, len(values))
	for _, value := range slices.Backward(values) {
		builder.PrependBool(value)
	}
	return builder.EndVector(len(values))
}

// packInt64Slice packs int64 constants as a FlatBuffer int64 vector.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes values ([]int64) which holds the integer constants to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func packInt64Slice(builder *flatbuffers.Builder, values []int64) flatbuffers.UOffsetT {
	if len(values) == 0 {
		return 0
	}
	schemagen.CompiledFunctionStartIntConstantsVector(builder, len(values))
	for _, value := range slices.Backward(values) {
		builder.PrependInt64(value)
	}
	return builder.EndVector(len(values))
}

// packFloat64Slice packs float64 constants as a FlatBuffer float64 vector.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes values ([]float64) which holds the floating-point constants to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func packFloat64Slice(builder *flatbuffers.Builder, values []float64) flatbuffers.UOffsetT {
	if len(values) == 0 {
		return 0
	}
	schemagen.CompiledFunctionStartFloatConstantsVector(builder, len(values))
	for _, value := range slices.Backward(values) {
		builder.PrependFloat64(value)
	}
	return builder.EndVector(len(values))
}

// packUint64Slice packs uint64 constants as a FlatBuffer uint64 vector.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes values ([]uint64) which holds the unsigned integer constants to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func packUint64Slice(builder *flatbuffers.Builder, values []uint64) flatbuffers.UOffsetT {
	if len(values) == 0 {
		return 0
	}
	schemagen.CompiledFunctionStartUintConstantsVector(builder, len(values))
	for _, value := range slices.Backward(values) {
		builder.PrependUint64(value)
	}
	return builder.EndVector(len(values))
}

// packUint16Slice packs uint16 init function indices as a FlatBuffer uint16 vector.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes values ([]uint16) which holds the init function indices to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func packUint16Slice(builder *flatbuffers.Builder, values []uint16) flatbuffers.UOffsetT {
	if len(values) == 0 {
		return 0
	}
	schemagen.CompiledFileSetStartInitialisationFunctionsVector(builder, len(values))
	for _, value := range slices.Backward(values) {
		builder.PrependUint16(value)
	}
	return builder.EndVector(len(values))
}

// packRegisterCounts packs per-bank register counts as a uint32 FlatBuffer vector.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes values ([]uint32) which holds the register counts to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func packRegisterCounts(builder *flatbuffers.Builder, values []uint32) flatbuffers.UOffsetT {
	if len(values) == 0 {
		return 0
	}
	schemagen.CompiledFunctionStartRegisterCountsVector(builder, len(values))
	for _, value := range slices.Backward(values) {
		builder.PrependUint32(value)
	}
	return builder.EndVector(len(values))
}

// packRegisterKinds packs register kind values as a FlatBuffer int8 vector.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes values ([]uint8) which holds the register kind values to pack.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func packRegisterKinds(builder *flatbuffers.Builder, values []uint8) flatbuffers.UOffsetT {
	if len(values) == 0 {
		return 0
	}
	schemagen.CompiledFunctionStartParameterKindsVector(builder, len(values))
	for _, value := range slices.Backward(values) {
		builder.PrependInt8(safeconv.MustUint8ToInt8(value))
	}
	return builder.EndVector(len(values))
}

// packInterfaceMethodSets packs the per-type-table-entry interface method-name sets as a
// FlatBuffer vector of InterfaceMethodSet tables. Empty entries are preserved by emitting
// an empty InterfaceMethodSet so that the unpacker can recover the original alignment
// with the type table.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes sets ([][]string) which holds the per-slot method-name lists.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// input is empty.
func packInterfaceMethodSets(builder *flatbuffers.Builder, sets [][]string) flatbuffers.UOffsetT {
	if len(sets) == 0 {
		return 0
	}
	entryOffsets := make([]flatbuffers.UOffsetT, len(sets))
	for i, methods := range sets {
		var methodsVectorOffset flatbuffers.UOffsetT
		if len(methods) > 0 {
			methodOffsets := make([]flatbuffers.UOffsetT, len(methods))
			for j, methodName := range methods {
				methodOffsets[j] = builder.CreateString(methodName)
			}
			methodsVectorOffset = createVector(builder, methodOffsets)
		}
		schemagen.InterfaceMethodSetStart(builder)
		if methodsVectorOffset != 0 {
			schemagen.InterfaceMethodSetAddMethods(builder, methodsVectorOffset)
		}
		entryOffsets[i] = schemagen.InterfaceMethodSetEnd(builder)
	}
	return createVector(builder, entryOffsets)
}

// packParameterRegisters packs the per-parameter destination register slot table as a
// FlatBuffers vector of uint8 values.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes values ([]uint8) which holds the parameter register slot assignments.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func packParameterRegisters(builder *flatbuffers.Builder, values []uint8) flatbuffers.UOffsetT {
	if len(values) == 0 {
		return 0
	}
	schemagen.CompiledFunctionStartParameterRegistersVector(builder, len(values))
	for _, value := range slices.Backward(values) {
		builder.PrependByte(value)
	}
	return builder.EndVector(len(values))
}

// createVector builds a FlatBuffers vector from pre-built table offsets.
//
// Takes builder (*flatbuffers.Builder) which is the FlatBuffer builder to write into.
// Takes offsets ([]flatbuffers.UOffsetT) which holds the table offsets to include in the
// vector.
//
// Returns flatbuffers.UOffsetT which is the offset of the packed vector, or 0 when the
// slice is empty.
func createVector(builder *flatbuffers.Builder, offsets []flatbuffers.UOffsetT) flatbuffers.UOffsetT {
	if len(offsets) == 0 {
		return 0
	}
	builder.StartVector(bytecodeVectorAlignment, len(offsets), bytecodeVectorAlignment)
	for _, offset := range slices.Backward(offsets) {
		builder.PrependUOffsetT(offset)
	}
	return builder.EndVector(len(offsets))
}
