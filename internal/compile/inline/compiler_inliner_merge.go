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

package inline

import (
	"math"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/symtab/descriptor"

	"pipit.sh/pipit/internal/isa"
	"pipit.sh/pipit/internal/safeconv"
)

const (
	// byteEncodingLimit is the exclusive upper bound for indices encoded in a single byte of
	// an opcode operand. Pool merges that exceed it under encodingByte must refuse the
	// splice.
	byteEncodingLimit = 256

	// uint16EncodingLimit is the exclusive upper bound for indices encoded in a uint16 pool
	// slot. Pool merges that would push the caller past it refuse the splice.
	uint16EncodingLimit = math.MaxUint16
)

// indexFits returns true when newIndex satisfies the encoding-byte constraint (always
// true when encodingByte is false).
//
// Takes newIndex (uint16) which is the candidate caller-side pool index.
// Takes encodingByte (bool) which is true when the consuming opcode encodes the index in
// a single byte.
//
// Returns true when the index fits the requested encoding.
func indexFits(newIndex uint16, encodingByte bool) bool {
	return !encodingByte || newIndex < byteEncodingLimit
}

// mergePoolIndex dispatches to the per-pool merge helper and returns the new caller-side
// index.
//
// Used by the inliner's operand walker when remapping any per-function table reference.
// The poolCallSites case is delegated to mergeCallSiteForCtx (which needs the
// inlineContext); calling mergePoolIndex with poolCallSites returns (0, false).
//
// On any kind of overflow (e.g. caller's structLayoutTable already holds
// byteEncodingLimit entries when the opcode encodes the index as uint8), returns (0,
// false) so the caller can refuse the splice.
//
// Takes caller (*CompiledFunction) which is the inlining caller whose pool receives the
// merged entry.
// Takes callee (*CompiledFunction) which is the function being spliced.
// Takes pool (inlinePool) which selects which parallel pool is being merged.
// Takes oldIndex (uint16) which is the entry's index in the callee pool.
// Takes encodingByte (bool) which is true when the consuming opcode encodes the result in
// a single byte and the index must fit in 8 bits.
//
// Returns the new caller-side index for the merged entry.
// Returns true when the merge fits; false on overflow or when encodingByte is set and the
// result exceeds the byte limit.
func mergePoolIndex(caller, callee *program.CompiledFunction, pool inlinePool, oldIndex uint16, encodingByte bool) (uint16, bool) {
	switch pool {
	case poolNone:
		return oldIndex, true
	case poolIntConsts:
		return mergeConstantPoolIndex(caller, callee.IntConstants, program.AddIntConstant, oldIndex, encodingByte)
	case poolFloatConsts:
		return mergeConstantPoolIndex(caller, callee.FloatConstants, program.AddFloatConstant, oldIndex, encodingByte)
	case poolStringConsts:
		return mergeConstantPoolIndex(caller, callee.StringConstants, program.AddStringConstant, oldIndex, encodingByte)
	case poolBoolConsts:
		return mergeConstantPoolIndex(caller, callee.BoolConstants, program.AddBoolConstant, oldIndex, encodingByte)
	case poolUintConsts:
		return mergeConstantPoolIndex(caller, callee.UintConstants, program.AddUintConstant, oldIndex, encodingByte)
	case poolComplexConsts:
		return mergeConstantPoolIndex(caller, callee.ComplexConstants, program.AddComplexConstant, oldIndex, encodingByte)
	case poolGeneralConsts:
		return mergeGeneralConstIndex(caller, callee, oldIndex, encodingByte)
	case poolTypeTable:
		return mergeTypeTableIndex(caller, callee, oldIndex, encodingByte)
	case poolStructLayoutTable:
		return mergeStructLayoutIndex(caller, callee, oldIndex, encodingByte)
	case poolCallSites:
		return 0, false
	case poolFunctions:
		return mergeFunctionIndex(caller, callee, oldIndex, encodingByte)
	}
	return 0, false
}

// mergeConstantPoolIndex merges one callee constant-pool entry into the caller's pool.
//
// Takes caller (*CompiledFunction) which is the caller whose pool receives the entry.
// Takes pool ([]T) which is the callee pool being read.
// Takes add (func) which appends a value to the caller's matching pool.
// Takes oldIndex (uint16) which is the entry's index in the callee pool.
// Takes encodingByte (bool) which is true when the result must fit in a single byte.
//
// Returns the new caller-side index and true when the merge fits the requested encoding.
func mergeConstantPoolIndex[T any](
	caller *program.CompiledFunction,
	pool []T,
	add func(*program.CompiledFunction, T) (uint16, error),
	oldIndex uint16,
	encodingByte bool,
) (uint16, bool) {
	if int(oldIndex) >= len(pool) {
		return 0, false
	}
	newIndex, err := add(caller, pool[oldIndex])
	if err != nil {
		return 0, false
	}
	return newIndex, indexFits(newIndex, encodingByte)
}

// mergeGeneralConstIndex merges a callee generalConstants entry into the caller's pool.
// AddGeneralConstant takes (v, descriptor); the descriptor is the parallel slice that
// this helper copies alongside the value.
//
// Takes caller (*CompiledFunction) which is the inlining caller whose pool receives the
// merged entry.
// Takes callee (*CompiledFunction) which is the function being spliced.
// Takes oldIndex (uint16) which is the entry's index in the callee pool.
// Takes encodingByte (bool) which is true when the result must fit in a single byte.
//
// Returns the new caller-side index for the merged entry and true when the merge fits the
// requested encoding.
//
// See mergePoolIndex for the broader contract.
func mergeGeneralConstIndex(caller, callee *program.CompiledFunction, oldIndex uint16, encodingByte bool) (uint16, bool) {
	if int(oldIndex) >= len(callee.GeneralConstants) {
		return 0, false
	}
	value := callee.GeneralConstants[oldIndex]
	var constantDescriptor descriptor.GeneralConstantDescriptor
	if int(oldIndex) < len(callee.GeneralConstantDescriptors) {
		constantDescriptor = callee.GeneralConstantDescriptors[oldIndex]
	}
	newIndex, err := program.AddGeneralConstant(caller, value, constantDescriptor)
	if err != nil {
		return 0, false
	}
	return newIndex, indexFits(newIndex, encodingByte)
}

// mergeTypeTableIndex merges a callee typeTable entry into the caller's pool. See
// mergePoolIndex for the broader contract.
//
// Takes caller (*CompiledFunction) which is the inlining caller whose pool receives the
// merged entry.
// Takes callee (*CompiledFunction) which is the function being spliced.
// Takes oldIndex (uint16) which is the entry's index in the callee pool.
// Takes encodingByte (bool) which is true when the result must fit in a single byte.
//
// Returns the new caller-side index for the merged entry and true when the merge fits the
// requested encoding.
func mergeTypeTableIndex(caller, callee *program.CompiledFunction, oldIndex uint16, encodingByte bool) (uint16, bool) {
	if int(oldIndex) >= len(callee.TypeTable) {
		return 0, false
	}
	newIndex, err := program.AddTypeRef(caller, callee.TypeTable[oldIndex])
	if err != nil {
		return 0, false
	}
	return newIndex, indexFits(newIndex, encodingByte)
}

// mergeCallSiteForCtx deep-copies the callee's callSites[oldIndex] entry, remaps
// argument/return register operands via ctx.remap, rebuilds the argCopyProgram against
// the callee's parameterKinds, and appends the result to caller.callSites.
//
// Multi-level inlining path: when the callee's body contains an isa.SubOpCall, the splice
// copies the corresponding CallSite into the caller so the runtime can dispatch through
// it.
//
// Takes ctx (*inlineContext) which carries caller, callee, and the register remap table.
// Takes oldIndex (uint16) which is the callee CallSite index to merge.
// Takes encodingByte (bool) which is true when the result must fit in a single byte.
//
// Returns the new caller-side CallSite index.
// Returns true when the merge succeeded; false on overflow or when any register failed to
// map.
func mergeCallSiteForCtx(ctx *inlineContext, oldIndex uint16, encodingByte bool) (uint16, bool) {
	caller := ctx.caller
	callee := ctx.callee
	if int(oldIndex) >= len(callee.CallSites) {
		return 0, false
	}
	source := callee.CallSites[oldIndex]

	arguments, ok := remapVarLocations(ctx, source.Arguments)
	if !ok {
		return 0, false
	}
	returns, ok := remapVarLocations(ctx, source.Returns)
	if !ok {
		return 0, false
	}

	newSite := buildRemappedCallSite(&source, arguments, returns)
	if !remapSpecialRegisters(ctx, &source, &newSite) {
		return 0, false
	}

	if newSite.CachedCallee != nil && len(newSite.CachedCallee.ParameterKinds) == len(arguments) {
		newSite.ArgCopyProgram = program.BuildCallArgCopyProgram(arguments, newSite.CachedCallee.ParameterKinds, newSite.CachedCallee.ParameterRegisters)
	}

	if len(caller.CallSites) >= uint16EncodingLimit {
		return 0, false
	}
	newIndex := safeconv.IntToUint16(len(caller.CallSites))
	caller.CallSites = append(caller.CallSites, newSite)
	return newIndex, indexFits(newIndex, encodingByte)
}

// remapVarLocations deep-copies a VarLocation slice while remapping each register through
// ctx.lookupRegister.
//
// Takes ctx (*inlineContext) which carries the register remap table.
// Takes source ([]VarLocation) which is the callee-side slice.
//
// Returns the remapped slice and true on success; nil and false when any register fails
// to map.
func remapVarLocations(ctx *inlineContext, source []program.VarLocation) ([]program.VarLocation, bool) {
	out := make([]program.VarLocation, len(source))
	for i, location := range source {
		newReg, ok := ctx.lookupRegister(location.Kind, location.Register)
		if !ok {
			return nil, false
		}
		out[i] = program.VarLocation{
			Register:  newReg,
			Kind:      location.Kind,
			IsSpilled: location.IsSpilled,
			SpillSlot: location.SpillSlot,
		}
	}
	return out, true
}

// buildRemappedCallSite constructs a fresh CallSite from source with register fields
// zeroed and argument/return slices already remapped.
//
// Takes source (*CallSite) which is the original callee-side site.
// Takes arguments ([]VarLocation) which is the remapped argument slice.
// Takes returns ([]VarLocation) which is the remapped return slice.
//
// Returns the constructed CallSite ready for special-register remapping.
func buildRemappedCallSite(source *program.CallSite, arguments, returns []program.VarLocation) program.CallSite {
	return program.CallSite{
		Arguments:                 arguments,
		Returns:                   returns,
		FunctionIndex:             source.FunctionIndex,
		CachedCallee:              source.CachedCallee,
		IsClosure:                 source.IsClosure,
		IsNative:                  source.IsNative,
		IsMethod:                  source.IsMethod,
		ClosureRegister:           0,
		NativeRegister:            0,
		MethodReceiverRegister:    0,
		IsEllipsisSpread:          source.IsEllipsisSpread,
		ArgumentStaticTypeNames:   source.ArgumentStaticTypeNames,
		ArgumentStaticTypeStrings: source.ArgumentStaticTypeStrings,
		ParameterInterfaceFlags:   source.ParameterInterfaceFlags,
		RuntimeVariadicSliceType:  source.RuntimeVariadicSliceType,
		RuntimeVariadicNumFixed:   source.RuntimeVariadicNumFixed,
		LinkedTypeArgs:            source.LinkedTypeArgs,
	}
}

// remapSpecialRegisters maps the closure/native/method-receiver registers from source
// onto destination via ctx. Each special register belongs to the general bank.
//
// Takes ctx (*inlineContext) which carries the register remap table.
// Takes source (*CallSite) which is the original callee-side site.
// Takes destination (*CallSite) which is the freshly-built caller-side site.
//
// Returns true when every special register present in source maps successfully; false
// when any lookup fails.
func remapSpecialRegisters(ctx *inlineContext, source, destination *program.CallSite) bool {
	if source.IsClosure {
		newReg, ok := ctx.lookupRegister(isa.RegisterGeneral, source.ClosureRegister)
		if !ok {
			return false
		}
		destination.ClosureRegister = newReg
	}
	if source.IsNative {
		newReg, ok := ctx.lookupRegister(isa.RegisterGeneral, source.NativeRegister)
		if !ok {
			return false
		}
		destination.NativeRegister = newReg
	}
	if source.IsMethod {
		newReg, ok := ctx.lookupRegister(isa.RegisterGeneral, source.MethodReceiverRegister)
		if !ok {
			return false
		}
		destination.MethodReceiverRegister = newReg
	}
	return true
}

// mergeFunctionIndex appends callee.functions[oldIndex] to caller.functions and returns
// the new index.
//
// Dedup uses pointer identity since *CompiledFunction is the key. When oldIndex is out of
// range (sites reference functionIndex = 0 even when no nested function is referenced,
// such as closures or native calls), returns (0, true) so the runtime passes through
// without reading.
//
// Linear-scan dedup: functions slices are small, so the scan is cheap compared to a hash
// map.
//
// Takes caller (*CompiledFunction) which receives the merged entry.
// Takes callee (*CompiledFunction) which supplies the source function.
// Takes oldIndex (uint16) which is the source functions slice index.
// Takes encodingByte (bool) which is true when the result must fit in a single byte.
//
// Returns the caller-side functions index.
// Returns true on success; false on overflow or when encodingByte is set and the result
// exceeds the byte limit.
func mergeFunctionIndex(caller, callee *program.CompiledFunction, oldIndex uint16, encodingByte bool) (uint16, bool) {
	if int(oldIndex) >= len(callee.Functions) {
		return 0, true
	}
	target := callee.Functions[oldIndex]
	for i, existing := range caller.Functions {
		if existing == target {
			index := safeconv.IntToUint16(i)
			return index, indexFits(index, encodingByte)
		}
	}
	if len(caller.Functions) >= uint16EncodingLimit {
		return 0, false
	}
	index := safeconv.IntToUint16(len(caller.Functions))
	caller.Functions = append(caller.Functions, target)
	return index, indexFits(index, encodingByte)
}

// mergeStructLayoutIndex merges a callee's structLayoutTable entry into the caller's
// table.
//
// The layout's TypeIndex (and FieldTypeIndex, when populated) reference the callee's
// typeTable, so those are remapped first before searching for an existing equivalent
// entry in the caller. Dedup uses a linear scan: callee tables are small, so the scan is
// amortised O(n) per splice.
//
// Takes caller (*CompiledFunction) which receives the merged layout.
// Takes callee (*CompiledFunction) which supplies the source layout.
// Takes oldIndex (uint16) which is the source structLayoutTable index.
// Takes encodingByte (bool) which is true when the result must fit in a single byte.
//
// Returns the caller-side structLayoutTable index.
// Returns true on success; false on overflow or unresolved type-table remap.
func mergeStructLayoutIndex(caller, callee *program.CompiledFunction, oldIndex uint16, encodingByte bool) (uint16, bool) {
	if int(oldIndex) >= len(callee.StructLayoutTable) {
		return 0, false
	}
	source := callee.StructLayoutTable[oldIndex]
	newTypeIndex, ok := mergePoolIndex(caller, callee, poolTypeTable, source.TypeIndex, false)
	if !ok {
		return 0, false
	}
	source.TypeIndex = newTypeIndex
	if source.FieldTypeIndex != 0 || (source.RegisterKind == uint8(isa.RegisterGeneral)) {
		newFieldTypeIndex, ok := mergePoolIndex(caller, callee, poolTypeTable, source.FieldTypeIndex, false)
		if !ok {
			return 0, false
		}
		source.FieldTypeIndex = newFieldTypeIndex
	}
	for i := range caller.StructLayoutTable {
		if structLayoutEqual(caller.StructLayoutTable[i], source) {
			index := safeconv.IntToUint16(i)
			return index, indexFits(index, encodingByte)
		}
	}
	index := safeconv.IntToUint16(len(caller.StructLayoutTable))
	caller.StructLayoutTable = append(caller.StructLayoutTable, source)
	return index, indexFits(index, encodingByte)
}

// structLayoutEqual compares two StructFieldLayout entries field by field. Used by
// mergeStructLayoutIndex's dedup search.
//
// Takes a (StructFieldLayout) which is the first layout.
// Takes b (StructFieldLayout) which is the second layout.
//
// Returns true when every scalar field and every path entry matches.
func structLayoutEqual(a, b program.StructFieldLayout) bool {
	if a.Offset != b.Offset ||
		a.TypeIndex != b.TypeIndex ||
		a.PathLength != b.PathLength ||
		a.Kind != b.Kind ||
		a.RegisterKind != b.RegisterKind ||
		a.Flags != b.Flags ||
		a.FieldTypeIndex != b.FieldTypeIndex {
		return false
	}
	for i := range a.Path {
		if a.Path[i] != b.Path[i] {
			return false
		}
	}
	return true
}
