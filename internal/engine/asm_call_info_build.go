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
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// EnsureASMCallInfoTables returns the asmCallInfoTables map for a root function, building
// it on first use under the function's asmCallInfoTablesOnce. The sync.Once gate makes
// the build race-free across child VMs that share the same root.
//
// Takes rootFunction (*CompiledFunction) whose asmCallInfoTablesOnce guards the build.
//
// Returns the function-pointer-keyed AsmCallInfo table map.
func EnsureASMCallInfoTables(rootFunction *program.CompiledFunction) map[*program.CompiledFunction][]AsmCallInfo {
	rootFunction.ASMCallInfoTablesOnce.Do(func() {
		tables, _ := buildASMCallInfoTables(rootFunction, rootFunction.Functions)
		rootFunction.ASMCallInfoTables = tables
		rootFunction.ASMCallInfoBases = buildASMCallInfoBases(rootFunction.Functions, tables)
	})
	tables, ok := rootFunction.ASMCallInfoTables.(map[*program.CompiledFunction][]AsmCallInfo)
	if !ok {
		return nil
	}
	return tables
}

// EnsureASMCallInfoBases returns the root's shared per-function table base array,
// building the AsmCallInfo tables first when no VM has executed the root yet. A trailing
// zero slot makes a callee from another root resolve to zero and take the Go call path,
// and the returned slice is read-only.
//
// Takes rootFunction (*CompiledFunction) whose function list indexes the array.
//
// Returns the shared array, or nil when the root has no functions.
func EnsureASMCallInfoBases(rootFunction *program.CompiledFunction) []uintptr {
	EnsureASMCallInfoTables(rootFunction)
	return rootFunction.ASMCallInfoBases
}

// buildASMCallInfoBases builds the per-function table base array for functions from the
// tables the same build produced, plus the reserved trailing zero slot.
//
// Takes functions ([]*CompiledFunction) which is the root's function list.
// Takes tables (map[*CompiledFunction][]AsmCallInfo) which are the root's built tables.
//
// Returns the array with len(functions)+1 entries, or nil when functions is empty.
func buildASMCallInfoBases(functions []*program.CompiledFunction, tables map[*program.CompiledFunction][]AsmCallInfo) []uintptr {
	if len(functions) == 0 {
		return nil
	}
	bases := make([]uintptr, len(functions)+1)
	for i, function := range functions {
		if table := tables[function]; len(table) > 0 {
			bases[i] = uintptr(unsafe.Pointer(&table[0]))
		}
	}
	return bases
}

// ConfigureASMReturn validates and configures the return destination for ASM fast-path
// dispatch.
//
// Takes info (*AsmCallInfo) which is the AsmCallInfo entry to populate with return
// metadata.
// Takes site (*CallSite) which is the call site descriptor containing return location
// descriptors.
// Takes callee (*CompiledFunction) which is the target function whose result kinds are
// validated.
//
// Returns true if the return shape is compatible with ASM dispatch, or false if there are
// multiple returns or incompatible register kinds.
func ConfigureASMReturn(info *AsmCallInfo, site *program.CallSite, callee *program.CompiledFunction) bool {
	if len(site.Returns) > asmMaxInlineReturns {
		return false
	}
	if len(site.Returns) == 0 {
		return true
	}
	if len(callee.ResultKinds) < len(site.Returns) {
		return false
	}
	single := len(site.Returns) == 1
	for index, returnLocation := range site.Returns {
		resultKind := callee.ResultKinds[index]
		if returnLocation.IsUpvalue || !asmInlineReturnKind(resultKind, single) || returnLocation.Kind != resultKind {
			return false
		}
	}
	info.ReturnCount = int64(len(site.Returns))
	info.returnDestinationKind = int64(site.Returns[0].Kind)
	info.returnDestinationRegister = int64(site.Returns[0].Register)
	return true
}

// buildASMCallInfoTables pre-computes AsmCallInfo tables for all functions in the
// program.
//
// Callers MUST go through EnsureASMCallInfoTables rather than invoking this directly. The
// function is only exported within the package so the sync.Once helper can call it;
// concurrent direct invocation for the same root races on the shared per-function
// table-slice state populated below.
//
// Takes rootFunction (*CompiledFunction) which is the entry-point function for the
// program.
// Takes functions ([]*CompiledFunction) which is the complete list of compiled functions
// in the program.
//
// Returns a map from function pointer to its AsmCallInfo table, and the root function's
// table for direct use by the dispatch loop.
func buildASMCallInfoTables(rootFunction *program.CompiledFunction, functions []*program.CompiledFunction) (map[*program.CompiledFunction][]AsmCallInfo, []AsmCallInfo) {
	tables := make(map[*program.CompiledFunction][]AsmCallInfo)
	buildASMCallInfoTableFor(rootFunction, functions, tables)
	for _, compiledFunction := range functions {
		if _, ok := tables[compiledFunction]; !ok {
			buildASMCallInfoTableFor(compiledFunction, functions, tables)
		}
	}
	linkCalleeCallInfoPointers(functions, tables)
	publishAsmCallInfoBases(tables)
	return tables, tables[rootFunction]
}

// linkCalleeCallInfoPointers fills each AsmCallInfo entry's calleeCallInfo with the base
// pointer of the callee's own table, so the ASM dispatcher can chain into the callee's
// fast-path slots without re-looking up the table.
//
// Takes functions ([]*CompiledFunction) which is the indexed list of compiled functions
// used to resolve callees.
// Takes tables (map[*CompiledFunction][]AsmCallInfo) which is the per-function
// AsmCallInfo tables to link in place.
func linkCalleeCallInfoPointers(functions []*program.CompiledFunction, tables map[*program.CompiledFunction][]AsmCallInfo) {
	for compiledFunction, table := range tables {
		for i := range table {
			if table[i].isFastPath == fastPathIneligible {
				continue
			}
			site := &compiledFunction.CallSites[i]
			callee := functions[site.FunctionIndex]
			if calleeTable, ok := tables[callee]; ok && len(calleeTable) > 0 {
				table[i].calleeCallInfo = uintptr(unsafe.Pointer(&calleeTable[0]))
			}
		}
	}
}

// publishAsmCallInfoBases caches each function's own AsmCallInfo table base on the
// CompiledFunction. Safe because the caller's sync.Once provides the publication barrier.
//
// Takes tables (map[*CompiledFunction][]AsmCallInfo) which is the per-function
// AsmCallInfo tables whose bases are published.
func publishAsmCallInfoBases(tables map[*program.CompiledFunction][]AsmCallInfo) {
	for compiledFunction, table := range tables {
		if len(table) > 0 {
			compiledFunction.ASMCallInfoBase = uintptr(unsafe.Pointer(&table[0]))
		} else {
			compiledFunction.ASMCallInfoBase = 0
		}
	}
}

// buildASMCallInfoTableFor builds the AsmCallInfo table for a single compiled function
// and stores it in the tables map.
//
// Takes function (*CompiledFunction) which is the function to build the table for.
// Takes functions ([]*CompiledFunction) which is the complete list of compiled functions
// for callee resolution.
// Takes tables (map[*CompiledFunction][]AsmCallInfo) which is the map to store the
// resulting table in.
func buildASMCallInfoTableFor(function *program.CompiledFunction, functions []*program.CompiledFunction, tables map[*program.CompiledFunction][]AsmCallInfo) {
	if len(function.CallSites) == 0 {
		tables[function] = nil
		return
	}
	table := make([]AsmCallInfo, len(function.CallSites))
	for i := range function.CallSites {
		buildOneASMCallInfo(&table[i], &function.CallSites[i], functions)
	}
	tables[function] = table
}

// buildOneASMCallInfo populates a single AsmCallInfo entry for a call site if it is
// eligible for ASM fast-path dispatch.
//
// Takes info (*AsmCallInfo) which is the AsmCallInfo entry to populate.
// Takes site (*CallSite) which is the call site descriptor from the caller function.
// Takes functions ([]*CompiledFunction) which is the complete list of compiled functions
// for callee resolution.
func buildOneASMCallInfo(info *AsmCallInfo, site *program.CallSite, functions []*program.CompiledFunction) {
	callee := resolveASMCallee(site, functions)
	if callee == nil {
		return
	}
	if !mapASMArguments(info, site, callee) {
		return
	}
	if !ConfigureASMReturn(info, site, callee) {
		return
	}
	populateASMCalleeFields(info, site, callee)
	info.calleeFunctionIndex = int64(site.FunctionIndex)
}

// resolveASMCallee returns the callee function if the call site is eligible for ASM
// dispatch, or nil if it should be skipped.
//
// Takes site (*CallSite) which is the call site descriptor to evaluate for eligibility.
// Takes functions ([]*CompiledFunction) which is the complete list of compiled functions
// for index lookup.
//
// Returns the resolved callee CompiledFunction, or nil if the call site is ineligible due
// to closures, native calls, variadic signatures, or non-int/float registers.
func resolveASMCallee(site *program.CallSite, functions []*program.CompiledFunction) *program.CompiledFunction {
	if site.IsClosure || site.IsNative {
		return nil
	}
	if int(site.FunctionIndex) >= len(functions) {
		return nil
	}
	callee := functions[site.FunctionIndex]
	if !asmCalleeStaticallyEligible(callee) {
		return nil
	}
	return callee
}

// asmCalleeParameterLayoutPerturbed reports whether the callee's parameter slots are
// scattered relative to the naive per-bank counter. promoteToIndirect can shift same-bank
// parameters off their naive slot, and the ASM copy trampolines are unaware of
// ParameterRegisters, so a mismatch here forces the Go call path.
//
// Takes callee (*CompiledFunction) whose parameter layout is examined.
//
// Returns true when parameterRegisters disagrees with the naive layout.
func asmCalleeParameterLayoutPerturbed(callee *program.CompiledFunction) bool {
	if len(callee.ParameterRegisters) != len(callee.ParameterKinds) {
		return false
	}
	var bankCounter [isa.NumRegisterKinds]uint8
	for i, paramKind := range callee.ParameterKinds {
		expected := bankCounter[paramKind]
		bankCounter[paramKind]++
		if callee.ParameterRegisters[i] != expected {
			return true
		}
	}
	return false
}

// asmArgumentSourceSlice returns the argument source array for the given register kind,
// or nil for unsupported kinds.
//
// Only the scalar banks the assembly call path can address are listed. A typed-slice or
// general argument is not passed through this route, and nil signals that.
//
// Takes info (*AsmCallInfo) which is the call info entry containing the per-kind argument
// source arrays.
// Takes kind (isa.RegisterKind) which selects the register kind whose source array to
// return.
//
// Returns []int64 which is the argument source slice for the given kind, or nil if the
// kind is unsupported.
func asmArgumentSourceSlice(info *AsmCallInfo, kind isa.RegisterKind) []int64 {
	switch kind {
	case isa.RegisterInt:
		return info.intArgumentSources[:]
	case isa.RegisterFloat:
		return info.floatArgumentSources[:]
	case isa.RegisterString:
		return info.stringArgumentSources[:]
	case isa.RegisterBool:
		return info.boolArgumentSources[:]
	case isa.RegisterUint:
		return info.uintArgumentSources[:]
	case isa.RegisterGeneral:
		return info.generalArgumentSources[:]
	case isa.RegisterSliceByte:
		return info.sliceByteArgumentSources[:]
	default:
		return nil
	}
}

// mapASMArguments maps the call site's arguments to the ASM info's source arrays and
// writes the per-kind argument counts into info.
//
// Takes info (*AsmCallInfo) which is the AsmCallInfo entry to populate with argument
// source indices and counts.
// Takes site (*CallSite) which is the call site descriptor containing the argument
// locations.
// Takes callee (*CompiledFunction) which is the target function whose parameter kinds
// must match.
//
// Returns true if all arguments were successfully mapped, or false if any argument kind
// does not match or exceeds the maximum count.
func mapASMArguments(info *AsmCallInfo, site *program.CallSite, callee *program.CompiledFunction) bool {
	var counts [isa.NumRegisterKinds]int
	for argumentIndex, argumentLocation := range site.Arguments {
		if argumentIndex >= len(callee.ParameterKinds) {
			break
		}
		parameterKind := callee.ParameterKinds[argumentIndex]
		if argumentLocation.Kind != parameterKind {
			return false
		}
		sources := asmArgumentSourceSlice(info, parameterKind)
		if sources == nil || counts[parameterKind] >= maxASMArgsByKind[parameterKind] {
			return false
		}
		sources[counts[parameterKind]] = int64(argumentLocation.Register)
		counts[parameterKind]++
	}
	info.intArgumentCount = int64(counts[isa.RegisterInt])
	info.floatArgumentCount = int64(counts[isa.RegisterFloat])
	info.stringArgumentCount = int64(counts[isa.RegisterString])
	info.boolArgumentCount = int64(counts[isa.RegisterBool])
	info.uintArgumentCount = int64(counts[isa.RegisterUint])
	info.generalArgumentCount = int64(counts[isa.RegisterGeneral])
	info.sliceByteArgumentCount = int64(counts[isa.RegisterSliceByte])
	return true
}

// asmInlineReturnKind reports whether the inline return handlers can copy a result of the
// given register kind. The general bank is supported only for single-result sites.
//
// Takes kind (isa.RegisterKind) which is the callee result kind.
// Takes single (bool) which is true when the site has exactly one result.
//
// Returns bool which is true when the inline return can carry the result.
func asmInlineReturnKind(kind isa.RegisterKind, single bool) bool {
	switch kind {
	case isa.RegisterInt, isa.RegisterFloat, isa.RegisterString, isa.RegisterBool, isa.RegisterUint:
		return true
	case isa.RegisterGeneral:
		return single
	default:
		return false
	}
}

// populateASMCalleeFields fills the callee-specific pointer and size fields that the ASM
// dispatch loop needs to set up an inline call frame. Argument counts must already be
// written to info by mapASMArguments.
//
// Takes info (*AsmCallInfo) which is the AsmCallInfo entry to populate with callee
// pointers and sizes.
// Takes site (*CallSite) which is the call site descriptor containing return location
// data.
// Takes callee (*CompiledFunction) which is the target compiled function providing body
// and constant pointers.
func populateASMCalleeFields(info *AsmCallInfo, site *program.CallSite, callee *program.CompiledFunction) {
	info.calleeFunction = uintptr(unsafe.Pointer(callee))
	if len(callee.Body) > 0 {
		info.calleeBody = uintptr(unsafe.Pointer(&callee.Body[0]))
	}
	info.calleeBodyLength = int64(len(callee.Body))
	if len(callee.IntConstants) > 0 {
		info.calleeIntConstants = uintptr(unsafe.Pointer(&callee.IntConstants[0]))
	}
	if len(callee.FloatConstants) > 0 {
		info.calleeFloatConstants = uintptr(unsafe.Pointer(&callee.FloatConstants[0]))
	}
	if len(callee.StringConstants) > 0 {
		info.calleeStringConstants = uintptr(unsafe.Pointer(&callee.StringConstants[0]))
	}
	if len(callee.BoolConstants) > 0 {
		info.calleeBoolConstants = uintptr(unsafe.Pointer(&callee.BoolConstants[0]))
	}
	if len(callee.UintConstants) > 0 {
		info.calleeUintConstants = uintptr(unsafe.Pointer(&callee.UintConstants[0]))
	}
	info.calleeIntCount = int64(callee.NumRegisters[isa.RegisterInt])
	info.calleeFloatCount = int64(callee.NumRegisters[isa.RegisterFloat])
	info.calleeStringCount = int64(callee.NumRegisters[isa.RegisterString])
	info.calleeBoolCount = int64(callee.NumRegisters[isa.RegisterBool])
	info.calleeUintCount = int64(callee.NumRegisters[isa.RegisterUint])
	info.calleeGeneralCount = int64(callee.NumRegisters[isa.RegisterGeneral])
	info.calleeSliceByteCount = int64(callee.NumRegisters[isa.RegisterSliceByte])
	if len(site.Returns) > 0 {
		info.returnDestinationPointer = uintptr(unsafe.Pointer(&site.Returns[0]))
		info.returnDestinationLen = int64(len(site.Returns))
	}
	if calleeUsesTypedSliceBanks(callee) {
		info.isFastPath = fastPathIneligible
		return
	}
	switch {
	case callee.NumRegisters[isa.RegisterGeneral] > 0:
		info.isFastPath = fastPathGeneralBank
	case callee.NumRegisters[isa.RegisterString] == 0 &&
		callee.NumRegisters[isa.RegisterBool] == 0 &&
		callee.NumRegisters[isa.RegisterUint] == 0 &&
		callee.NumRegisters[isa.RegisterSliceByte] == 0:
		info.isFastPath = fastPathPureIntFloat
	default:
		info.isFastPath = fastPathExtendedBanks
	}
}

// calleeUsesTypedSliceBanks reports whether callee uses an unsupported typed-slice bank.
// RegisterSliceByte is supported by the ASM path and is not checked here.
//
// Takes callee (*CompiledFunction) whose register watermark is inspected.
//
// Returns true when at least one unsupported typed-slice bank has nonzero register count.
func calleeUsesTypedSliceBanks(callee *program.CompiledFunction) bool {
	return callee.NumRegisters[isa.RegisterSliceInt] > 0 ||
		callee.NumRegisters[isa.RegisterSliceFloat] > 0 ||
		callee.NumRegisters[isa.RegisterSliceString] > 0 ||
		callee.NumRegisters[isa.RegisterSliceBool] > 0 ||
		callee.NumRegisters[isa.RegisterSliceUint] > 0
}
