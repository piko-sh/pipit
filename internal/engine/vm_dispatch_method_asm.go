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
	"slices"
	"unsafe"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// MaxASMMethodEntries is the number of receiver types an ASM inline method-call site can
	// dispatch in assembly. A site that sees more receiver types keeps serving the first
	// eight from assembly and the rest through the Go path.
	MaxASMMethodEntries = 8
)

// zeroASMCallInfo is the all-zero AsmCallInfo the method dispatcher writes back when a
// call site cannot be served by the inline frame push.
var zeroASMCallInfo AsmCallInfo

// asmMethodEntryTable owns the per-receiver-type AsmCallInfo entries for one method call
// site. The array is allocated once, so the entry addresses handed to the assembly
// dispatcher through methodEntriesPtr never move.
type asmMethodEntryTable struct {
	// entries holds one populated AsmCallInfo per receiver type, in the order the site's
	// methodTypeWords lists them.
	entries [MaxASMMethodEntries]AsmCallInfo

	// count is the number of populated entries, mirroring the site's methodEntryCount for
	// diagnostics and tests.
	count int
}

// buildCallInfoBasesByFunction adopts the shared per-function table base array built once
// for root.
//
// Takes root (*CompiledFunction) whose function list indexes the array.
func (vm *VM) buildCallInfoBasesByFunction(root *program.CompiledFunction) {
	vm.asmCallInfoRoot = root
	vm.asmCallInfoBasesByFunction = EnsureASMCallInfoBases(root)
	vm.asmCallInfoBasesPrivate = false
}

// publishCallInfoBaseFor records a new table base for function in the per-function array,
// so frames pushed in assembly from now on adopt it.
//
// The array starts as the root's shared build, which every VM of the root reads, so the
// first publish clones it into a VM-private copy and repoints the live dispatch context
// at the clone; later publishes write the private copy in place.
//
// Takes function (*CompiledFunction) whose table was replaced.
// Takes base (uintptr) which is the replacement table's first entry address.
func (vm *VM) publishCallInfoBaseFor(function *program.CompiledFunction, base uintptr) {
	index := slices.Index(vm.functions, function)
	if vm.rootFunction != vm.asmCallInfoRoot || index < 0 || index >= len(vm.asmCallInfoBasesByFunction)-1 {
		return
	}
	if !vm.asmCallInfoBasesPrivate {
		vm.asmCallInfoBasesByFunction = slices.Clone(vm.asmCallInfoBasesByFunction)
		vm.asmCallInfoBasesPrivate = true
		if vm.liveCtx != nil {
			vm.publishCallInfoRoot(vm.liveCtx)
		}
	}
	vm.asmCallInfoBasesByFunction[index] = base
}

// calleeBaseSlot returns the per-function array slot for callee: its index in the root's
// function list, or the reserved trailing slot (always zero, forcing the Go path inside
// the callee) when it belongs to another bundle.
//
// Takes callee (*CompiledFunction) which is the resolved callee.
//
// Returns the slot index.
func (vm *VM) calleeBaseSlot(callee *program.CompiledFunction) int64 {
	if vm.rootFunction == vm.asmCallInfoRoot {
		if index := slices.Index(vm.functions, callee); index >= 0 {
			return int64(index)
		}
	}
	return int64(len(vm.asmCallInfoBasesByFunction) - 1)
}

// syncHasGoroutinesFlag mirrors vm.hasGoroutines into the live dispatch context at the
// moment it flips, so the assembly closure and method guards observe the spawn on the
// very next dispatch rather than at the next Go-side frame change.
func (vm *VM) syncHasGoroutinesFlag() {
	if vm.liveCtx == nil {
		return
	}
	if vm.hasGoroutines {
		vm.liveCtx.hasGoroutinesFlag = 1
	} else {
		vm.liveCtx.hasGoroutinesFlag = 0
	}
}

// dropRuntimeCallInfoFills forgets every runtime closure and method fill this VM made in
// its private call-info tables. Called wherever asmCallInfoTables is released, so the
// next execution starts from the shared read-only build and never mistakes it for a
// private copy.
func (vm *VM) dropRuntimeCallInfoFills() {
	vm.PrivateACITables = nil
	vm.AsmMethodEntryTables = nil
	vm.asmCallInfoBasesByFunction = nil
	vm.asmCallInfoBasesPrivate = false
	vm.asmCallInfoRoot = nil
}

// methodEntryTableFor returns the VM-owned entry table for site, allocating it on first
// use.
//
// Takes site (*CallSite) which keys the table.
//
// Returns the table.
func (vm *VM) methodEntryTableFor(site *program.CallSite) *asmMethodEntryTable {
	if vm.AsmMethodEntryTables == nil {
		vm.AsmMethodEntryTables = make(map[*program.CallSite]*asmMethodEntryTable)
	}
	table := vm.AsmMethodEntryTables[site]
	if table == nil {
		table = &asmMethodEntryTable{entries: [MaxASMMethodEntries]AsmCallInfo{}, count: 0}
		vm.AsmMethodEntryTables[site] = table
	}
	return table
}

// tryPopulateMethodASMInfo adds an ASM inline-call entry for the receiver type the site's
// inline cache just resolved.
//
// Subsequent calls with that receiver type push their frame in assembly instead of
// exiting to Go. The entry is built with the same argument, return and callee population
// steps as a static entry; the receiver arrives as the first general-bank argument
// exactly as pushCompiledFrame() copies it. Nothing is populated while goroutines exist
// (mirroring tryPopulateClosureASMInfo(): the ASM guard re-reads the goroutine flag so an
// existing table is bypassed once a goroutine spawns), when the callee fails a static
// gate, or once the site already holds MaxASMMethodEntries receiver types. The site's
// entry is filled in this VM's private copy of the caller's table, never in the shared
// per-root build.
//
// Takes caller (*CompiledFunction) which owns the call site.
// Takes site (*CallSite) which is the method call site.
// Takes callee (*CompiledFunction) which the inline cache resolved for typeWord.
// Takes typeWord (uintptr) which is the dereferenced receiver type word (the cache key).
func (vm *VM) tryPopulateMethodASMInfo(caller *program.CompiledFunction, site *program.CallSite, callee *program.CompiledFunction, typeWord uintptr) {
	if typeWord == 0 || callee == nil || vm.hasGoroutines || vm.AsmCallInfoTables == nil || vm.rootFunction != vm.asmCallInfoRoot {
		return
	}
	if siteRejectsMethodType(site, typeWord) {
		return
	}
	siteIndex, ok := callSiteIndexWithin(caller, site)
	if !ok {
		return
	}
	table := vm.AsmCallInfoTables[caller]
	if siteIndex >= len(table) {
		return
	}
	if methodTableHoldsType(&table[siteIndex], typeWord) || table[siteIndex].methodEntryCount >= MaxASMMethodEntries {
		return
	}
	fp := vm.FramePointer
	if fp < 0 || fp >= len(vm.asmCallInfoBases) {
		return
	}
	entries := vm.methodEntryTableFor(site)
	slot := entries.count
	if slot >= MaxASMMethodEntries {
		return
	}
	entry := &entries.entries[slot]
	if !asmMethodCalleePrefersInlineFrame(callee) || !buildMethodASMEntry(vm, entry, site, callee) {
		rejectMethodType(site, typeWord)
		return
	}
	table = vm.ensurePrivateACITable(caller, fp, table)
	info := &table[siteIndex]
	info.receiverValueRegister = int64(site.Arguments[0].Register)
	info.methodEntriesPtr = uintptr(unsafe.Pointer(&entries.entries[0]))
	info.methodTypeWords[slot] = typeWord
	entries.count = slot + 1
	info.methodEntryCount = int64(entries.count)
}

// siteRejectsMethodType reports whether typeWord is in the site's negative cache.
//
// Takes site (*CallSite) which owns the cache.
// Takes typeWord (uintptr) which is the dereferenced receiver type word.
//
// Returns true when the word was previously rejected.
func siteRejectsMethodType(site *program.CallSite, typeWord uintptr) bool {
	for _, word := range site.AsmMethodRejectedTypeWords {
		if word == typeWord {
			return true
		}
	}
	return false
}

// rejectMethodType records typeWord in the site's negative cache, taking the first empty
// slot or evicting the oldest one when the cache is full.
//
// Takes site (*CallSite) which owns the cache.
// Takes typeWord (uintptr) which is the dereferenced receiver type word.
func rejectMethodType(site *program.CallSite, typeWord uintptr) {
	for i, word := range site.AsmMethodRejectedTypeWords {
		if word == 0 {
			site.AsmMethodRejectedTypeWords[i] = typeWord
			return
		}
	}
	copy(site.AsmMethodRejectedTypeWords[:], site.AsmMethodRejectedTypeWords[1:])
	site.AsmMethodRejectedTypeWords[program.AsmMethodRejectedCacheSize-1] = typeWord
}

// asmCalleeStaticallyEligible reports whether a compiled function can be entered through
// the ASM inline-call frame push at all: not variadic, no captured upvalues, no complex
// registers and an unperturbed parameter layout. The per-site argument and return shape
// checks are applied separately by mapASMArguments and ConfigureASMReturn.
//
// Takes callee (*CompiledFunction) which is the candidate callee.
//
// Returns true when the callee passes every static gate.
func asmCalleeStaticallyEligible(callee *program.CompiledFunction) bool {
	if callee.IsVariadic || len(callee.UpvalueDescriptors) > 0 {
		return false
	}
	if callee.NumRegisters[isa.RegisterComplex] > 0 {
		return false
	}
	return !asmCalleeParameterLayoutPerturbed(callee)
}

// asmMethodCalleePrefersInlineFrame reports whether an inline-cache-resolved method body
// should be entered through the ASM inline frame push.
//
// Takes callee (*CompiledFunction) which is the resolved method body.
//
// Returns true when the inline frame push is the faster route.
func asmMethodCalleePrefersInlineFrame(callee *program.CompiledFunction) bool {
	if !asmCalleeStaticallyEligible(callee) || callee.TinyLeafShape != program.TinyLeafNone {
		return false
	}
	classifyFusedEvalShape(callee)
	return callee.EvalShape <= program.FusedEvalNone
}

// callSiteIndexWithin returns the index of site inside function.callSites.
//
// Takes function (*CompiledFunction) which owns the call-site table.
// Takes site (*CallSite) which must point into that table.
//
// Returns the site index and true, or false when site is not an element of the table.
func callSiteIndexWithin(function *program.CompiledFunction, site *program.CallSite) (int, bool) {
	if function == nil || len(function.CallSites) == 0 {
		return 0, false
	}
	base := uintptr(unsafe.Pointer(&function.CallSites[0]))
	address := uintptr(unsafe.Pointer(site))
	if address < base {
		return 0, false
	}
	index := int((address - base) / unsafe.Sizeof(program.CallSite{}))
	if index >= len(function.CallSites) || &function.CallSites[index] != site {
		return 0, false
	}
	return index, true
}

// methodTableHoldsType reports whether info already dispatches typeWord in assembly.
//
// Takes info (*AsmCallInfo) which is the site's call-info entry.
// Takes typeWord (uintptr) which is the dereferenced receiver type word.
//
// Returns true when the word is present.
func methodTableHoldsType(info *AsmCallInfo, typeWord uintptr) bool {
	for i := range int(info.methodEntryCount) {
		if info.methodTypeWords[i] == typeWord {
			return true
		}
	}
	return false
}

// buildMethodASMEntry fills entry for a method callee reached from site, leaving it
// zeroed when the site's argument or return shape cannot be served by the inline frame
// push.
//
// Takes vm (*VM) whose call-info tables supply the callee's own table base.
// Takes entry (*AsmCallInfo) which receives the population.
// Takes site (*CallSite) which describes the argument and return locations.
// Takes callee (*CompiledFunction) which is the resolved method body.
//
// Returns true when the entry is usable by the assembly dispatcher.
func buildMethodASMEntry(vm *VM, entry *AsmCallInfo, site *program.CallSite, callee *program.CompiledFunction) bool {
	*entry = zeroASMCallInfo
	if !mapASMArguments(entry, site, callee) || !ConfigureASMReturn(entry, site, callee) {
		*entry = zeroASMCallInfo
		return false
	}
	populateASMCalleeFields(entry, site, callee)
	if entry.isFastPath == fastPathIneligible {
		*entry = zeroASMCallInfo
		return false
	}
	if calleeTable := vm.AsmCallInfoTables[callee]; len(calleeTable) > 0 {
		entry.calleeCallInfo = uintptr(unsafe.Pointer(&calleeTable[0]))
	}
	entry.calleeFunctionIndex = vm.calleeBaseSlot(callee)
	return true
}
