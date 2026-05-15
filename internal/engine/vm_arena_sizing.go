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
	"pipit.sh/pipit/internal/engine/program"
	"pipit.sh/pipit/internal/isa"
)

const (
	// arenaDepthEstimateFloor is the smallest call depth the register-bank pre-sizing
	// assumes, so shallow programs still get a few frames of headroom before the growth path
	// runs.
	arenaDepthEstimateFloor = 8

	// arenaDepthEstimateCeiling caps the call depth the register-bank pre-sizing assumes;
	// deeper chains fall into the growth path rather than pre-allocating for an unbounded
	// static estimate.
	arenaDepthEstimateCeiling = 64
)

// sizeArenaForEntry pre-sizes the VM's arena for an execution that starts at entry, using
// the peak register counts of statically reachable functions scaled by call depth.
//
// Takes root (*CompiledFunction) which carries the program's function table.
// Takes entry (*CompiledFunction) which is the function the execution starts in.
func (vm *VM) sizeArenaForEntry(root, entry *program.CompiledFunction) {
	var bytecodeHints arenaBytecodeHints

	allFuncs := append([]*program.CompiledFunction{root}, root.Functions...)
	if root.VariableInitFunction != nil {
		allFuncs = append(allFuncs, root.VariableInitFunction)
	}
	for _, f := range allFuncs {
		accumulateArenaBytecodeHints(&bytecodeHints, f.Body, allFuncs)
	}

	depth := clampedArenaDepth(root)
	totals := peakRegisterCounts(functionsReachableFrom(root, entry, allFuncs))
	scale(&totals, depth)

	const depthEstimate = arenaDepthEstimateCeiling
	totals.Bytes = saturatingProduct(bytecodeHints.Bytes, depthEstimate)
	totals.IntBacking = saturatingProduct3(bytecodeHints.MakeSliceInt, arenaMakeSliceAvgCapacity, depthEstimate)
	totals.FloatBacking = saturatingProduct3(bytecodeHints.MakeSliceFloat, arenaMakeSliceAvgCapacity, depthEstimate)
	totals.StringBacking = saturatingProduct3(bytecodeHints.MakeSliceString, arenaMakeSliceAvgCapacity, depthEstimate)
	totals.BoolBacking = saturatingProduct3(bytecodeHints.MakeSliceBool, arenaMakeSliceAvgCapacity, depthEstimate)
	totals.UintBacking = saturatingProduct3(bytecodeHints.MakeSliceUint, arenaMakeSliceAvgCapacity, depthEstimate)
	totals.GenericBytes = clampAtMost(saturatingProduct(bytecodeHints.genericBytes, depthEstimate), maxGenericBytesPreSize)
	totals.SliceHeaders = clampAtMost(saturatingProduct(bytecodeHints.sliceHeaders, depthEstimate), maxSliceHeaderPreSize)

	totals.FrameStack = vm.frameStackPreSize(root)

	totals.IntBoxes = clampAtMost(saturatingProduct(bytecodeHints.boxInts, boxSlabPerOccurrenceFactor), maxBoxSlabPreSize)
	totals.UintBoxes = clampAtMost(saturatingProduct(bytecodeHints.boxUints, boxSlabPerOccurrenceFactor), maxBoxSlabPreSize)
	totals.FloatBoxes = clampAtMost(saturatingProduct(bytecodeHints.boxFloats, boxSlabPerOccurrenceFactor), maxBoxSlabPreSize)
	totals.StringBoxes = clampAtMost(saturatingProduct(bytecodeHints.boxStrings, boxSlabPerOccurrenceFactor), maxBoxSlabPreSize)
	totals.ComplexBoxes = clampAtMost(saturatingProduct(bytecodeHints.boxComplexes, boxSlabPerOccurrenceFactor), maxBoxSlabPreSize)

	totals.UpvalueCells = clampAtMost(saturatingProduct(bytecodeHints.upvalueCells, depthEstimate), maxUpvalueSlabPreSize)
	totals.UpvalueReferences = clampAtMost(saturatingProduct(bytecodeHints.upvalueReferences, depthEstimate), maxUpvalueSlabPreSize)

	vm.Arena.ensureCapacity(totals)
}

// frameStackPreSize returns the frame count the arena is sized for before root runs.
// Honours the recorded peak from earlier runs to avoid regrowth on recursive workloads.
//
// Takes root (*CompiledFunction) which carries the static estimate and the recorded peak.
//
// Returns int which is the frame-stack capacity to ensure, safety margin included.
func (vm *VM) frameStackPreSize(root *program.CompiledFunction) int {
	frames := clampAtMost(program.EstimateMaxCallDepth(root)+frameStackSafetyMargin, maxFrameStackPreSize)
	if observed := root.ObservedCallDepth(); observed > 0 {
		frames = max(frames, vm.observedFrameStackTarget(observed))
	}
	return frames
}

// observedFrameStackTarget converts a recorded peak frame count into a frame-stack
// capacity: the peak plus the safety margin, with the peak first capped at the call-depth
// limit no run can exceed.
//
// Takes observed (int) which is the recorded peak frame count.
//
// Returns int which is the capacity to size or grow to.
func (vm *VM) observedFrameStackTarget(observed int) int {
	return min(observed, vm.callDepthLimit()) + frameStackSafetyMargin
}

// clampedArenaDepth returns the static call depth estimate for root plus the frame safety
// margin, clamped to [arenaDepthEstimateFloor, arenaDepthEstimateCeiling].
//
// Takes root (*CompiledFunction) which carries the program's function table.
//
// Returns the clamped depth in frames.
func clampedArenaDepth(root *program.CompiledFunction) int {
	return min(max(program.EstimateMaxCallDepth(root)+frameStackSafetyMargin, arenaDepthEstimateFloor), arenaDepthEstimateCeiling)
}

// functionsReachableFrom returns the functions statically reachable from entry through
// direct call sites, including entry itself. When entry is root, or when any reachable
// function contains a closure or method call site (whose callee cannot be resolved
// statically), every function in allFuncs is returned instead.
//
// Takes root (*CompiledFunction) which carries the program's function table.
// Takes entry (*CompiledFunction) which is the execution's starting function.
// Takes allFuncs ([]*CompiledFunction) which is the complete function set.
//
// Returns the functions whose register counts bound the execution's frames.
func functionsReachableFrom(root, entry *program.CompiledFunction, allFuncs []*program.CompiledFunction) []*program.CompiledFunction {
	if entry == nil || entry == root {
		return allFuncs
	}
	entryIndex := -1
	for i, f := range root.Functions {
		if f == entry {
			entryIndex = i
			break
		}
	}
	if entryIndex < 0 {
		return allFuncs
	}
	reachable, resolvable := walkStaticCallees(root.Functions, entryIndex)
	if !resolvable {
		return allFuncs
	}
	return reachable
}

// walkStaticCallees performs a depth-first walk over the static call graph from
// entryIndex. Returns false when any function contains an unresolvable call.
//
// Takes functions ([]*CompiledFunction) which is the program's function table.
// Takes entryIndex (int) which is the index of the starting function.
//
// Returns the reached functions and whether all callees were statically resolved.
func walkStaticCallees(functions []*program.CompiledFunction, entryIndex int) ([]*program.CompiledFunction, bool) {
	adjacency := program.BuildCallAdjacency(functions)
	visited := make([]bool, len(functions))
	reachable := make([]*program.CompiledFunction, 0, len(functions))
	stack := []int{entryIndex}
	for len(stack) > 0 {
		index := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if visited[index] {
			continue
		}
		visited[index] = true
		function := functions[index]
		if functionHasUnresolvableCalls(function) {
			return nil, false
		}
		reachable = append(reachable, function)
		for _, callee := range adjacency[index] {
			if int(callee) < len(functions) && !visited[callee] {
				stack = append(stack, int(callee))
			}
		}
	}
	return reachable, true
}

// functionHasUnresolvableCalls reports whether function contains a closure or method call
// site, whose callee the static call graph cannot resolve.
//
// Takes function (*CompiledFunction) which is the function to inspect.
//
// Returns true when at least one call site is a closure or method call.
func functionHasUnresolvableCalls(function *program.CompiledFunction) bool {
	for i := range function.CallSites {
		if function.CallSites[i].IsClosure || function.CallSites[i].IsMethod {
			return true
		}
	}
	return false
}

// peakRegisterCounts returns, for every register bank, the largest per-frame register
// count among functions.
//
// Takes functions ([]*CompiledFunction) which are the functions to inspect.
//
// Returns the per-bank maxima as one frame's worth of typedSlabCounts.
func peakRegisterCounts(functions []*program.CompiledFunction) program.TypedSlabCounts {
	var peaks program.TypedSlabCounts
	for _, f := range functions {
		peaks.Ints = max(peaks.Ints, int(f.NumRegisters[isa.RegisterInt]))
		peaks.Floats = max(peaks.Floats, int(f.NumRegisters[isa.RegisterFloat]))
		peaks.Strings = max(peaks.Strings, int(f.NumRegisters[isa.RegisterString]))
		peaks.Generals = max(peaks.Generals, int(f.NumRegisters[isa.RegisterGeneral]))
		peaks.Bools = max(peaks.Bools, int(f.NumRegisters[isa.RegisterBool]))
		peaks.Uints = max(peaks.Uints, int(f.NumRegisters[isa.RegisterUint]))
		peaks.Complexes = max(peaks.Complexes, int(f.NumRegisters[isa.RegisterComplex]))
		peaks.SlicesInts = max(peaks.SlicesInts, int(f.NumRegisters[isa.RegisterSliceInt]))
		peaks.SlicesFloats = max(peaks.SlicesFloats, int(f.NumRegisters[isa.RegisterSliceFloat]))
		peaks.SlicesStrings = max(peaks.SlicesStrings, int(f.NumRegisters[isa.RegisterSliceString]))
		peaks.SlicesBools = max(peaks.SlicesBools, int(f.NumRegisters[isa.RegisterSliceBool]))
		peaks.SlicesUints = max(peaks.SlicesUints, int(f.NumRegisters[isa.RegisterSliceUint]))
	}
	return peaks
}

// scale multiplies every register-bank count by depth, leaving the backing, box and frame
// fields untouched.
//
// Takes c (*TypedSlabCounts) which holds the per-bank counts to multiply.
// Takes depth (int) which is the number of frames to provision.
func scale(c *program.TypedSlabCounts, depth int) {
	c.Ints *= depth
	c.Floats *= depth
	c.Strings *= depth
	c.Generals *= depth
	c.Bools *= depth
	c.Uints *= depth
	c.Complexes *= depth
	c.SlicesInts *= depth
	c.SlicesFloats *= depth
	c.SlicesStrings *= depth
	c.SlicesBools *= depth
	c.SlicesUints *= depth
}
