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

package passes

import (
	"slices"

	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

// InstructionDirectlyMutatesHeap reports whether inst, in isolation (ignoring call
// targets), can mutate heap-resident state.
//
// Derived from the ISA spec table. Fails closed, treating an unresolvable or unclassified
// operation as mutating. Call opcodes are excluded because CallInvalidatesPurity handles
// them via per-callee classification.
//
// Takes inst (instruction) which is the instruction under inspection.
//
// Returns true when the instruction itself performs a heap mutation.
func InstructionDirectlyMutatesHeap(inst isa.Instruction) bool {
	mutates, _ := isa.InstructionMutatesMemory(inst)
	return mutates
}

// IsCallInstruction reports whether inst transfers control to another function.
//
// Takes inst (instruction) which is the instruction under inspection.
//
// Returns true when the instruction is a call variant.
func IsCallInstruction(inst isa.Instruction) bool {
	if inst.Op != isa.OpDrillTier1 {
		return false
	}
	switch isa.SubOpcode(inst.A) {
	case isa.SubOpCall, isa.SubOpCallScalar, isa.SubOpTailCall, isa.SubOpCallMethod,
		isa.SubOpCallMethodInlineable, isa.SubOpCallNative, isa.SubOpCallIIFE:
		return true
	default:
	}
	return false
}

// CallInvalidatesPurity reports whether the call instruction at inst invalidates
// compiledFunction's purity classification.
//
// A call invalidates when the opcode is isa.SubOpCallMethod or isa.SubOpCallNative
// (targets resolved at runtime), isa.SubOpCallIIFE (synthesised inner function),
// isa.SubOpCall, isa.SubOpCallScalar or isa.SubOpTailCall with no cachedCallee (forward
// reference or closure call), or when the cachedCallee's heapMutationClass is
// HeapMutatingCallee.
//
// Takes compiledFunction (*CompiledFunction) which is the function being classified.
// Takes inst (instruction) which is the candidate call instruction.
//
// Returns false only when the callee is statically resolvable and classified as
// HeapPureCallee; true otherwise.
func CallInvalidatesPurity(compiledFunction *program.CompiledFunction, inst isa.Instruction) bool {
	if inst.Op != isa.OpDrillTier1 {
		return true
	}
	switch isa.SubOpcode(inst.A) {
	case isa.SubOpCallMethod, isa.SubOpCallMethodInlineable, isa.SubOpCallNative, isa.SubOpCallIIFE:
		return true
	case isa.SubOpCall, isa.SubOpCallScalar, isa.SubOpTailCall:
		callee := resolveCachedCallee(compiledFunction, inst)
		if callee == nil {
			return true
		}
		return callee.HeapMutationClass != program.HeapPureCallee
	default:
	}
	return true
}

// loopRemainderIsHeapPure reports whether everything that can run between the candidate
// move and the next time it runs is free of heap mutation and impure calls.
//
// The instructions in question are those on some path that leaves the move and comes back
// to it.
//
// Takes compiledFunction (*CompiledFunction) which owns the body and call sites.
// Takes flow (*controlFlowLists) which holds the body's control flow in both directions.
// Takes movePC (int) which is the candidate snapshot's program counter.
//
// Returns true when the move sits in a loop and that set is heap-pure.
func loopRemainderIsHeapPure(compiledFunction *program.CompiledFunction, flow *controlFlowLists, movePC int) bool {
	leaves := reachableFrom(flow.successors, movePC)
	if !leaves[movePC] {
		return false
	}
	returns := reachableFrom(flow.predecessors, movePC)
	for pc, inst := range compiledFunction.Body {
		if pc == movePC || !leaves[pc] || !returns[pc] {
			continue
		}
		if InstructionDirectlyMutatesHeap(inst) {
			return false
		}
		if IsCallInstruction(inst) && CallInvalidatesPurity(compiledFunction, inst) {
			return false
		}
	}
	return true
}

// controlFlowListsOf decodes a body's control flow once, for walks in both directions.
//
// Takes body ([]isa.Instruction) which is the instruction stream.
//
// Returns *controlFlowLists which lists each word's successors and predecessors.
func controlFlowListsOf(body []isa.Instruction) *controlFlowLists {
	edges := controlFlowEdgesOf(body)
	return &controlFlowLists{
		successors:   edges.lists(len(body), func(edge controlFlowEdge) (int, int) { return edge.from, edge.to }),
		predecessors: edges.lists(len(body), func(edge controlFlowEdge) (int, int) { return edge.to, edge.from }),
	}
}

// reachableFrom marks every word that can be reached from start by one or more steps
// along the given edges. start itself is marked only when a path leads back to it.
//
// Takes next ([][]int) which lists, per word, the words one step on.
// Takes start (int) which is the word the walk leaves from.
//
// Returns []bool which marks the reached words.
func reachableFrom(next [][]int, start int) []bool {
	reached := make([]bool, len(next))
	pending := slices.Clone(next[start])
	for len(pending) > 0 {
		pc := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if reached[pc] {
			continue
		}
		reached[pc] = true
		if pc != start {
			pending = append(pending, next[pc]...)
		}
	}
	return reached
}

// resolveCachedCallee returns the statically-resolved callee for the call at inst, or nil
// when the call site has no cachedCallee.
//
// inst's operands B and C encode the call-site index as a uint16 (isa.WideIndex).
// compiledFunction.callSites maps the index to the resolved callee; closure and
// forward-reference sites have a nil cachedCallee.
//
// Takes compiledFunction (*CompiledFunction) whose call sites are consulted.
// Takes inst (instruction) which carries the wide call-site index.
//
// Returns the resolved callee, or nil when no cachedCallee is recorded.
func resolveCachedCallee(compiledFunction *program.CompiledFunction, inst isa.Instruction) *program.CompiledFunction {
	siteIndex := int(inst.WideIndex())
	if siteIndex < 0 || siteIndex >= len(compiledFunction.CallSites) {
		return nil
	}
	return compiledFunction.CallSites[siteIndex].CachedCallee
}
